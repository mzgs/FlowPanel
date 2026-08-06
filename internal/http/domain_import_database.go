package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/mariadb"
)

const websiteImportMaxDatabaseBytes = int64(1 << 30)

var importDatabaseIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type websiteImportDatabaseInput struct {
	SourceName          string `json:"source_name"`
	SourceHost          string `json:"source_host"`
	SourcePort          int    `json:"source_port"`
	SourceUsername      string `json:"source_username"`
	SourcePassword      string `json:"source_password"`
	DestinationName     string `json:"destination_name"`
	DestinationUsername string `json:"destination_username"`
	DestinationPassword string `json:"destination_password"`
}

func (input *websiteImportDatabaseInput) normalize() {
	input.SourceName = strings.TrimSpace(input.SourceName)
	input.SourceHost = strings.Trim(strings.TrimSpace(input.SourceHost), "[]")
	input.SourceUsername = strings.TrimSpace(input.SourceUsername)
	input.DestinationName = strings.TrimSpace(input.DestinationName)
	input.DestinationUsername = strings.TrimSpace(input.DestinationUsername)
}

func (input websiteImportDatabaseInput) validate() map[string]string {
	validation := input.validateDestination()
	if input.SourceName == "" {
		validation["database.source_name"] = "Select a source database."
	} else if strings.HasPrefix(input.SourceName, "-") || strings.ContainsAny(input.SourceName, "\x00\r\n") {
		validation["database.source_name"] = "The source database name is invalid."
	}
	if input.SourceHost == "" || strings.ContainsAny(input.SourceHost, "/?#@") {
		validation["database.source_host"] = "Enter a valid database hostname or IP address."
	}
	if input.SourcePort < 1 || input.SourcePort > 65535 {
		validation["database.source_port"] = "Database port must be between 1 and 65535."
	}
	if input.SourceUsername == "" {
		validation["database.source_username"] = "Enter the source database username."
	}
	if input.SourcePassword == "" {
		validation["database.source_password"] = "Enter the source database password."
	}
	return validation
}

func (input websiteImportDatabaseInput) validateDestination() map[string]string {
	validation := map[string]string{}
	if input.SourceName == "" {
		validation["database.source_name"] = "Select a source database."
	} else if strings.HasPrefix(input.SourceName, "-") || strings.ContainsAny(input.SourceName, "\x00\r\n") {
		validation["database.source_name"] = "The source database name is invalid."
	}
	for field, value := range map[string]string{
		"database.destination_name":     input.DestinationName,
		"database.destination_username": input.DestinationUsername,
	} {
		if value == "" || !importDatabaseIdentifierPattern.MatchString(value) {
			validation[field] = "Use only letters, numbers, and underscores."
		}
	}
	if input.DestinationPassword == "" {
		validation["database.destination_password"] = "Enter a destination database password."
	}
	return validation
}

func importWebsiteDatabase(ctx context.Context, manager mariadb.Manager, domainName string, input websiteImportDatabaseInput) (string, error) {
	if manager == nil {
		return "", errors.New("MariaDB is not configured on this server")
	}
	dump, err := dumpRemoteDatabase(ctx, input)
	if err != nil {
		return "", err
	}

	return restoreImportedWebsiteDatabase(ctx, manager, domainName, input, dump)
}

func restoreImportedWebsiteDatabase(ctx context.Context, manager mariadb.Manager, domainName string, input websiteImportDatabaseInput, dump []byte) (string, error) {
	record, err := manager.CreateDatabase(ctx, mariadb.CreateDatabaseInput{
		Name:     input.DestinationName,
		Username: input.DestinationUsername,
		Password: input.DestinationPassword,
		Domain:   domainName,
	})
	if err != nil {
		return "", fmt.Errorf("create destination database: %w", err)
	}
	restoreCtx, restoreCancel := context.WithTimeout(ctx, 30*time.Minute)
	defer restoreCancel()
	if err := manager.RestoreDatabase(restoreCtx, record.Name, dump); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleanupErr := manager.DeleteDatabase(cleanupCtx, record.Name, mariadb.DeleteDatabaseInput{Username: record.Username})
		return "", fmt.Errorf("restore destination database: %w", errors.Join(err, cleanupErr))
	}
	return record.Name, nil
}

func dumpRemoteDatabase(ctx context.Context, input websiteImportDatabaseInput) ([]byte, error) {
	dumpBinary, err := findCommand("mariadb-dump", "mysqldump")
	if err != nil {
		return nil, errors.New("mariadb-dump or mysqldump is required to import a remote database")
	}
	dumpFile, err := os.CreateTemp("", "flowpanel-remote-database-*.sql")
	if err != nil {
		return nil, fmt.Errorf("create database import staging file: %w", err)
	}
	dumpPath := dumpFile.Name()
	defer os.Remove(dumpPath)

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(runCtx, dumpBinary,
		"--single-transaction",
		"--quick",
		"--skip-lock-tables",
		"--hex-blob",
		"--default-character-set=utf8mb4",
		"--host="+input.SourceHost,
		"--port="+strconv.Itoa(input.SourcePort),
		"--user="+input.SourceUsername,
		input.SourceName,
	)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+input.SourcePassword)
	cmd.Stdout = &limitedFileWriter{file: dumpFile, remaining: websiteImportMaxDatabaseBytes}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := dumpFile.Close()
	if runErr != nil || closeErr != nil {
		message := strings.TrimSpace(stderr.String())
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("source database export timed out")
		}
		if message != "" {
			return nil, fmt.Errorf("source database export failed: %s", message)
		}
		return nil, fmt.Errorf("source database export failed: %w", errors.Join(runErr, closeErr))
	}
	dump, err := os.ReadFile(dumpPath)
	if err != nil {
		return nil, fmt.Errorf("read source database export: %w", err)
	}
	if len(bytes.TrimSpace(dump)) == 0 {
		return nil, errors.New("source database export was empty")
	}
	return dump, nil
}

type limitedFileWriter struct {
	file      *os.File
	remaining int64
}

func (writer *limitedFileWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > writer.remaining {
		return 0, fmt.Errorf("database export exceeds the %d GiB import limit", websiteImportMaxDatabaseBytes>>30)
	}
	written, err := writer.file.Write(value)
	writer.remaining -= int64(written)
	return written, err
}

func findCommand(names ...string) (string, error) {
	for _, name := range names {
		if command, err := exec.LookPath(name); err == nil {
			return command, nil
		}
	}
	return "", io.EOF
}
