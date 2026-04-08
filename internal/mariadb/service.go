package mariadb

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/config"

	"go.uber.org/zap"
)

const (
	statusCommandTimeout = 3 * time.Second
	sqlCommandTimeout    = 8 * time.Second
	dumpCommandTimeout   = 2 * time.Minute
	dialTimeout          = 500 * time.Millisecond
	defaultTCPHost       = "127.0.0.1"
	defaultTCPPort       = "3306"
	defaultDBUser        = "root"
	localhostHost        = "localhost"
	defaultPasswordFile  = "mariadb-root-password"
	passwordBytesLength  = 24
)

var (
	serverBinaryCandidates = []string{
		"mariadbd",
		"mysqld",
	}
	clientBinaryCandidates = []string{
		"mariadb",
		"mysql",
	}
	dumpBinaryCandidates = []string{
		"mariadb-dump",
		"mysqldump",
	}
	socketCandidates = []string{
		"/run/mysqld/mysqld.sock",
		"/var/run/mysqld/mysqld.sock",
		"/run/mysql/mysql.sock",
		"/var/run/mysql/mysql.sock",
		"/run/mariadb/mariadb.sock",
		"/var/run/mariadb/mariadb.sock",
		"/tmp/mysql.sock",
		"/tmp/mysqld.sock",
	}
	systemDatabases = map[string]struct{}{
		"information_schema": {},
		"mysql":              {},
		"performance_schema": {},
		"sys":                {},
	}
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

	ErrDatabaseNotFound      = errors.New("database not found")
	ErrDatabaseAlreadyExists = errors.New("database already exists")
)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
	Remove(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Restart(context.Context) error
	RootPassword(context.Context) (string, bool, error)
	SetRootPassword(context.Context, string) error
	ListDatabases(context.Context) ([]DatabaseRecord, error)
	DumpDatabase(context.Context, string) ([]byte, error)
	RestoreDatabase(context.Context, string, []byte) error
	CreateDatabase(context.Context, CreateDatabaseInput) (DatabaseRecord, error)
	UpdateDatabase(context.Context, string, UpdateDatabaseInput) (DatabaseRecord, error)
	DeleteDatabase(context.Context, string, DeleteDatabaseInput) error
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Product          string   `json:"product,omitempty"`
	ServerInstalled  bool     `json:"server_installed"`
	ServerPath       string   `json:"server_path,omitempty"`
	ClientInstalled  bool     `json:"client_installed"`
	ClientPath       string   `json:"client_path,omitempty"`
	Version          string   `json:"version,omitempty"`
	ListenAddress    string   `json:"listen_address,omitempty"`
	ServiceRunning   bool     `json:"service_running"`
	Ready            bool     `json:"ready"`
	State            string   `json:"state"`
	Message          string   `json:"message"`
	Issues           []string `json:"issues,omitempty"`
	InstallAvailable bool     `json:"install_available"`
	InstallLabel     string   `json:"install_label,omitempty"`
	RemoveAvailable  bool     `json:"remove_available"`
	RemoveLabel      string   `json:"remove_label,omitempty"`
	StartAvailable   bool     `json:"start_available"`
	StartLabel       string   `json:"start_label,omitempty"`
	StopAvailable    bool     `json:"stop_available"`
	StopLabel        string   `json:"stop_label,omitempty"`
	RestartAvailable bool     `json:"restart_available"`
	RestartLabel     string   `json:"restart_label,omitempty"`
}

type DatabaseRecord struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Host     string `json:"host"`
	Domain   string `json:"domain,omitempty"`
	Password string `json:"password,omitempty"`
}

type CreateDatabaseInput struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	Domain   string `json:"domain,omitempty"`
}

type UpdateDatabaseInput struct {
	CurrentUsername string `json:"current_username"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	Domain          string `json:"domain,omitempty"`
}

type DeleteDatabaseInput struct {
	Username string `json:"username"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "validation failed"
}

type Service struct {
	logger           *zap.Logger
	rootPasswordPath string
	store            *Store
}

type actionPlan struct {
	packageManager string
	installLabel   string
	removeLabel    string
	startLabel     string
	stopLabel      string
	restartLabel   string
	installCmds    [][]string
	removeCmds     [][]string
	startCmds      [][]string
	stopCmds       [][]string
	restartCmds    [][]string
}

func NewService(logger *zap.Logger, store *Store) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger:           logger,
		rootPasswordPath: resolvePasswordFilePath(),
		store:            store,
	}
}

func (s *Service) Status(ctx context.Context) Status {
	status := Status{
		Platform: runtime.GOOS,
		Product:  "MariaDB",
	}
	plan := detectActionPlan()
	status.PackageManager = plan.packageManager

	serverPath, serverInstalled := lookupFirstCommand(serverBinaryCandidates...)
	if serverInstalled {
		status.ServerInstalled = true
		status.ServerPath = serverPath
	}

	clientPath, clientInstalled := lookupFirstCommand(clientBinaryCandidates...)
	if clientInstalled {
		status.ClientInstalled = true
		status.ClientPath = clientPath
	}

	if output, err := inspectVersion(ctx, clientPath, clientInstalled, serverPath, serverInstalled); err == nil {
		status.Product, status.Version = parseVersion(output)
		if status.Product == "" {
			status.Product = "MariaDB"
		}
	} else if err != nil {
		status.Issues = append(status.Issues, err.Error())
	}

	status.ListenAddress, status.ServiceRunning = detectReachableAddress()
	status.InstallAvailable = len(plan.installCmds) > 0 && !status.ServerInstalled
	status.InstallLabel = plan.installLabel
	status.RemoveAvailable = len(plan.removeCmds) > 0 && (status.ServerInstalled || status.ClientInstalled)
	status.RemoveLabel = plan.removeLabel
	status.StartAvailable = len(plan.startCmds) > 0 && status.ServerInstalled && !status.ServiceRunning
	status.StartLabel = plan.startLabel
	status.StopAvailable = len(plan.stopCmds) > 0 && status.ServerInstalled && status.ServiceRunning
	status.StopLabel = plan.stopLabel
	status.RestartAvailable = len(plan.restartCmds) > 0 && status.ServerInstalled && status.ServiceRunning
	status.RestartLabel = plan.restartLabel

	switch {
	case status.ServiceRunning:
		status.Ready = true
		status.State = "ready"
		if status.ListenAddress != "" {
			status.Message = fmt.Sprintf("%s is accepting local connections on %s.", status.Product, status.ListenAddress)
		} else {
			status.Message = fmt.Sprintf("%s is accepting local connections.", status.Product)
		}
	case status.ServerInstalled:
		status.State = "stopped"
		status.Message = fmt.Sprintf("%s appears installed, but no local socket or TCP listener responded.", status.Product)
	case status.ClientInstalled:
		status.State = "client-only"
		status.Message = fmt.Sprintf("%s client tools are installed, but no local server binary was found.", status.Product)
	default:
		status.State = "missing"
		status.Message = fmt.Sprintf("%s was not detected on this server.", status.Product)
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic MariaDB installation is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("installing mariadb runtime",
		zap.String("package_manager", plan.packageManager),
	)
	if err := runCommands(ctx, plan.installCmds...); err != nil {
		return err
	}

	if err := s.ensureRootPassword(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Service) Remove(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.removeCmds) == 0 {
		return fmt.Errorf("automatic MariaDB removal is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("removing mariadb runtime",
		zap.String("package_manager", plan.packageManager),
	)
	commands := make([][]string, 0, len(plan.stopCmds)+len(plan.removeCmds))
	if status := s.Status(ctx); status.ServiceRunning {
		commands = append(commands, plan.stopCmds...)
	}
	commands = append(commands, plan.removeCmds...)
	if err := runCommands(ctx, commands...); err != nil {
		return err
	}

	if s.rootPasswordPath != "" {
		if err := os.Remove(s.rootPasswordPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove mariadb root password file: %w", err)
		}
	}
	_ = os.Unsetenv("FLOWPANEL_MARIADB_PASSWORD")

	return nil
}

func (s *Service) Start(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.startCmds) == 0 {
		return fmt.Errorf("automatic MariaDB startup is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("starting mariadb service",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.startCmds...)
}

func (s *Service) Stop(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.stopCmds) == 0 {
		return fmt.Errorf("automatic MariaDB shutdown is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("stopping mariadb service",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.stopCmds...)
}

func (s *Service) Restart(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.restartCmds) == 0 {
		return fmt.Errorf("automatic MariaDB restart is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("restarting mariadb service",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.restartCmds...)
}

func (s *Service) RootPassword(context.Context) (string, bool, error) {
	if password, configured := rootPasswordFromEnv(); configured {
		return password, true, nil
	}

	password, configured, err := readPasswordFile(s.rootPasswordPath)
	if err != nil {
		return "", false, fmt.Errorf("read mariadb root password file: %w", err)
	}

	return password, configured, nil
}

func (s *Service) SetRootPassword(ctx context.Context, password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return ValidationErrors{
			"password": "Password must be at least 8 characters.",
		}
	}

	rootUserLiteral := quoteLiteral(defaultDBUser)
	passwordLiteral := quoteLiteral(password)
	statements := []string{
		fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s", rootUserLiteral, passwordLiteral),
		fmt.Sprintf("ALTER USER %s@'localhost' IDENTIFIED BY %s", rootUserLiteral, passwordLiteral),
		"FLUSH PRIVILEGES",
	}

	if _, err := s.runSQL(ctx, strings.Join(statements, "; ")); err != nil {
		return fmt.Errorf("configure mariadb root user: %w", err)
	}

	if err := writePasswordFile(s.rootPasswordPath, password); err != nil {
		return fmt.Errorf("write mariadb root password file: %w", err)
	}

	_ = os.Setenv("FLOWPANEL_MARIADB_PASSWORD", password)
	return nil
}

func (s *Service) ListDatabases(ctx context.Context) ([]DatabaseRecord, error) {
	databaseRows, err := s.queryRows(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME ASC")
	if err != nil {
		return nil, err
	}

	metadata, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}

	recordsByName := make(map[string]DatabaseRecord, len(databaseRows))
	for _, row := range databaseRows {
		if len(row) == 0 {
			continue
		}

		name := strings.TrimSpace(row[0])
		if name == "" || isSystemDatabase(name) {
			continue
		}

		recordsByName[name] = DatabaseRecord{
			Name: name,
			Host: localhostHost,
		}
	}

	for name, credential := range metadata {
		if name == "" || isSystemDatabase(name) {
			continue
		}

		record, exists := recordsByName[name]
		if !exists {
			continue
		}

		if record.Username == "" && credential.Username != "" {
			record.Username = credential.Username
		}
		if record.Host == "" && credential.Host != "" {
			record.Host = credential.Host
		}
		if credential.Password != "" && (credential.Username == "" || credential.Username == record.Username) {
			record.Password = credential.Password
		}
		if credential.Domain != "" {
			record.Domain = credential.Domain
		}
		if record.Host == "" {
			record.Host = localhostHost
		}

		recordsByName[name] = record
	}

	records := make([]DatabaseRecord, 0, len(recordsByName))
	for _, record := range recordsByName {
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Name < records[j].Name
	})

	return records, nil
}

func (s *Service) CreateDatabase(ctx context.Context, input CreateDatabaseInput) (DatabaseRecord, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Username = strings.TrimSpace(input.Username)
	input.Password = strings.TrimSpace(input.Password)
	input.Domain = strings.TrimSpace(input.Domain)

	if validation := validateCreateInput(input); len(validation) > 0 {
		return DatabaseRecord{}, validation
	}

	exists, err := s.databaseExists(ctx, input.Name)
	if err != nil {
		return DatabaseRecord{}, err
	}
	if exists {
		return DatabaseRecord{}, ErrDatabaseAlreadyExists
	}

	databaseIdentifier := quoteIdentifier(input.Name)
	userLiteral := quoteLiteral(input.Username)
	passwordLiteral := quoteLiteral(input.Password)
	statements := []string{
		fmt.Sprintf("CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", databaseIdentifier),
		fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s", userLiteral, passwordLiteral),
		fmt.Sprintf("ALTER USER %s@'localhost' IDENTIFIED BY %s", userLiteral, passwordLiteral),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s@'localhost'", databaseIdentifier, userLiteral),
		"FLUSH PRIVILEGES",
	}

	if _, err := s.runSQL(ctx, strings.Join(statements, "; ")); err != nil {
		return DatabaseRecord{}, err
	}

	record := DatabaseRecord{
		Name:     input.Name,
		Username: input.Username,
		Host:     localhostHost,
		Domain:   input.Domain,
		Password: input.Password,
	}
	if err := s.store.Upsert(ctx, record); err != nil {
		return DatabaseRecord{}, err
	}

	return record, nil
}

func (s *Service) DumpDatabase(ctx context.Context, databaseName string) ([]byte, error) {
	databaseName = strings.TrimSpace(databaseName)
	if message := validateIdentifier(databaseName, "Database name"); message != "" {
		return nil, ValidationErrors{
			"name": message,
		}
	}
	if isSystemDatabase(databaseName) {
		return nil, ValidationErrors{
			"name": "System databases cannot be exported.",
		}
	}

	dumpPath, ok := lookupFirstCommand(dumpBinaryCandidates...)
	if !ok {
		return nil, errors.New("mariadb dump client is not installed")
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, dumpCommandTimeout)
		defer cancel()
	}

	config, err := s.resolveSQLClientConfig()
	if err != nil {
		return nil, err
	}

	args := []string{
		"--single-transaction",
		"--routines",
		"--events",
		"--triggers",
		"--default-character-set=utf8mb4",
		fmt.Sprintf("--user=%s", config.user),
	}
	if config.socket != "" {
		args = append(args, "--protocol=socket", fmt.Sprintf("--socket=%s", config.socket))
	} else {
		args = append(args,
			"--protocol=tcp",
			fmt.Sprintf("--host=%s", config.host),
			fmt.Sprintf("--port=%s", config.port),
		)
	}
	args = append(args, "--databases", databaseName)

	cmd := exec.CommandContext(runCtx, dumpPath, args...)
	if config.password != "" {
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+config.password)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%s timed out", dumpPath)
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return nil, fmt.Errorf("%s was canceled", dumpPath)
		}

		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			return nil, fmt.Errorf("%s failed: %w", dumpPath, err)
		}

		return nil, fmt.Errorf("%s failed: %s", dumpPath, message)
	}

	return stdout.Bytes(), nil
}

func (s *Service) RestoreDatabase(ctx context.Context, databaseName string, dump []byte) error {
	databaseName = strings.TrimSpace(databaseName)
	if message := validateIdentifier(databaseName, "Database name"); message != "" {
		return ValidationErrors{
			"name": message,
		}
	}
	if isSystemDatabase(databaseName) {
		return ValidationErrors{
			"name": "System databases cannot be restored.",
		}
	}
	if len(bytes.TrimSpace(dump)) == 0 {
		return errors.New("database dump is empty")
	}

	if _, err := s.runSQL(ctx, fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		quoteIdentifier(databaseName),
	)); err != nil {
		return err
	}

	clientPath, ok := lookupFirstCommand(clientBinaryCandidates...)
	if !ok {
		return errors.New("mariadb/mysql client is not installed")
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, dumpCommandTimeout)
		defer cancel()
	}

	config, err := s.resolveSQLClientConfig()
	if err != nil {
		return err
	}

	args := []string{
		fmt.Sprintf("--user=%s", config.user),
	}
	if config.socket != "" {
		args = append(args, "--protocol=socket", fmt.Sprintf("--socket=%s", config.socket))
	} else {
		args = append(args,
			"--protocol=tcp",
			fmt.Sprintf("--host=%s", config.host),
			fmt.Sprintf("--port=%s", config.port),
		)
	}

	cmd := exec.CommandContext(runCtx, clientPath, args...)
	cmd.Stdin = bytes.NewReader(dump)
	if config.password != "" {
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+config.password)
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out", clientPath)
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		return fmt.Errorf("%s was canceled", clientPath)
	}
	if combinedOutput == "" {
		return fmt.Errorf("%s failed: %w", clientPath, err)
	}

	return fmt.Errorf("%s failed: %s", clientPath, combinedOutput)
}

func (s *Service) UpdateDatabase(ctx context.Context, databaseName string, input UpdateDatabaseInput) (DatabaseRecord, error) {
	databaseName = strings.TrimSpace(databaseName)
	input.CurrentUsername = strings.TrimSpace(input.CurrentUsername)
	input.Username = strings.TrimSpace(input.Username)
	input.Password = strings.TrimSpace(input.Password)
	input.Domain = strings.TrimSpace(input.Domain)
	if input.CurrentUsername == "" {
		input.CurrentUsername = input.Username
	}

	if validation := validateUpdateInput(databaseName, input); len(validation) > 0 {
		return DatabaseRecord{}, validation
	}

	exists, err := s.databaseExists(ctx, databaseName)
	if err != nil {
		return DatabaseRecord{}, err
	}
	if !exists {
		return DatabaseRecord{}, ErrDatabaseNotFound
	}

	hasGrant, err := s.userHasGrant(ctx, databaseName, input.CurrentUsername, localhostHost)
	if err != nil {
		return DatabaseRecord{}, err
	}
	if !hasGrant {
		return DatabaseRecord{}, ValidationErrors{
			"current_username": "Current username does not have access to this database.",
		}
	}

	metadata, err := s.store.List(ctx)
	if err != nil {
		return DatabaseRecord{}, err
	}
	storedRecord := metadata[databaseName]

	databaseIdentifier := quoteIdentifier(databaseName)
	currentUserLiteral := quoteLiteral(input.CurrentUsername)
	nextUserLiteral := quoteLiteral(input.Username)
	statements := []string{
		fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s@'localhost'", databaseIdentifier, nextUserLiteral),
	}
	effectivePassword := storedRecord.Password

	if input.Password != "" {
		passwordLiteral := quoteLiteral(input.Password)
		effectivePassword = input.Password
		statements = append([]string{
			fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s", nextUserLiteral, passwordLiteral),
			fmt.Sprintf("ALTER USER %s@'localhost' IDENTIFIED BY %s", nextUserLiteral, passwordLiteral),
		}, statements...)
	}

	if input.CurrentUsername != input.Username {
		statements = append(statements, fmt.Sprintf(
			"REVOKE ALL PRIVILEGES, GRANT OPTION ON %s.* FROM %s@'localhost'",
			databaseIdentifier,
			currentUserLiteral,
		))
	}

	statements = append(statements, "FLUSH PRIVILEGES")
	if _, err := s.runSQL(ctx, strings.Join(statements, "; ")); err != nil {
		return DatabaseRecord{}, err
	}

	if input.CurrentUsername != input.Username {
		if err := s.dropUserIfUnused(ctx, input.CurrentUsername, localhostHost); err != nil {
			s.logger.Warn("failed to drop unused mariadb user",
				zap.String("username", input.CurrentUsername),
				zap.Error(err),
			)
		}
	}

	record := DatabaseRecord{
		Name:     databaseName,
		Username: input.Username,
		Host:     localhostHost,
		Domain:   input.Domain,
		Password: effectivePassword,
	}
	if err := s.store.Upsert(ctx, record); err != nil {
		return DatabaseRecord{}, err
	}

	return record, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, databaseName string, input DeleteDatabaseInput) error {
	databaseName = strings.TrimSpace(databaseName)
	input.Username = strings.TrimSpace(input.Username)

	if validation := validateDeleteInput(databaseName, input); len(validation) > 0 {
		return validation
	}

	exists, err := s.databaseExists(ctx, databaseName)
	if err != nil {
		return err
	}
	if !exists {
		return ErrDatabaseNotFound
	}

	if _, err := s.runSQL(ctx, fmt.Sprintf("DROP DATABASE %s", quoteIdentifier(databaseName))); err != nil {
		return err
	}

	if input.Username != "" {
		if err := s.dropUserIfUnused(ctx, input.Username, localhostHost); err != nil {
			s.logger.Warn("failed to drop unused mariadb user",
				zap.String("username", input.Username),
				zap.Error(err),
			)
		}
	}

	if err := s.store.Delete(ctx, databaseName); err != nil {
		return err
	}

	return nil
}

type sqlClientConfig struct {
	user     string
	password string
	host     string
	port     string
	socket   string
}

func (s *Service) runSQL(ctx context.Context, query string) (string, error) {
	clientPath, ok := lookupFirstCommand(clientBinaryCandidates...)
	if !ok {
		return "", errors.New("mariadb/mysql client is not installed")
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, sqlCommandTimeout)
		defer cancel()
	}

	config, err := s.resolveSQLClientConfig()
	if err != nil {
		return "", err
	}
	args := []string{
		"--batch",
		"--raw",
		"--skip-column-names",
		fmt.Sprintf("--user=%s", config.user),
	}

	if config.socket != "" {
		args = append(args, "--protocol=socket", fmt.Sprintf("--socket=%s", config.socket))
	} else {
		args = append(args,
			"--protocol=tcp",
			fmt.Sprintf("--host=%s", config.host),
			fmt.Sprintf("--port=%s", config.port),
		)
	}

	args = append(args, "--execute", query)

	cmd := exec.CommandContext(runCtx, clientPath, args...)
	if config.password != "" {
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+config.password)
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return combinedOutput, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return combinedOutput, fmt.Errorf("%s timed out", clientPath)
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		return combinedOutput, fmt.Errorf("%s was canceled", clientPath)
	}
	if combinedOutput == "" {
		return combinedOutput, fmt.Errorf("%s failed: %w", clientPath, err)
	}

	return combinedOutput, fmt.Errorf("%s failed: %s", clientPath, combinedOutput)
}

func (s *Service) resolveSQLClientConfig() (sqlClientConfig, error) {
	config := sqlClientConfig{
		user:   strings.TrimSpace(envWithDefault("FLOWPANEL_MARIADB_USER", defaultDBUser)),
		host:   strings.TrimSpace(os.Getenv("FLOWPANEL_MARIADB_HOST")),
		port:   strings.TrimSpace(os.Getenv("FLOWPANEL_MARIADB_PORT")),
		socket: strings.TrimSpace(os.Getenv("FLOWPANEL_MARIADB_SOCKET")),
	}

	if config.user == "" {
		config.user = defaultDBUser
	}

	if password, ok := rootPasswordFromEnv(); ok {
		config.password = password
	} else {
		password, configured, err := readPasswordFile(s.rootPasswordPath)
		if err != nil {
			return sqlClientConfig{}, fmt.Errorf("read mariadb root password file: %w", err)
		}
		if configured {
			config.password = password
		}
	}

	if config.socket == "" && config.host == "" {
		if address, reachable := detectReachableAddress(); reachable {
			if strings.HasPrefix(address, "/") {
				config.socket = address
			} else {
				if host, port, err := net.SplitHostPort(address); err == nil {
					config.host = host
					config.port = port
				}
			}
		}
	}

	if config.socket == "" {
		if config.host == "" {
			config.host = defaultTCPHost
		}
		if config.port == "" {
			config.port = defaultTCPPort
		}
	}

	return config, nil
}

func (s *Service) ensureRootPassword(ctx context.Context) error {
	password, configured, err := s.RootPassword(ctx)
	if err != nil {
		return err
	}

	if !configured {
		password, err = generatePassword()
		if err != nil {
			return fmt.Errorf("generate mariadb root password: %w", err)
		}
	}

	return s.SetRootPassword(ctx, password)
}

func resolvePasswordFilePath() string {
	if value := strings.TrimSpace(os.Getenv("FLOWPANEL_MARIADB_PASSWORD_FILE")); value != "" {
		return value
	}

	if dbPath := strings.TrimSpace(os.Getenv("FLOWPANEL_DB_PATH")); dbPath != "" && dbPath != ":memory:" {
		return filepath.Join(filepath.Dir(dbPath), defaultPasswordFile)
	}

	return filepath.Join(config.FlowPanelDataPath(), defaultPasswordFile)
}

func rootPasswordFromEnv() (string, bool) {
	password, configured := os.LookupEnv("FLOWPANEL_MARIADB_PASSWORD")
	if !configured {
		return "", false
	}

	password = strings.TrimSpace(password)
	if password == "" {
		return "", false
	}

	return password, true
}

func readPasswordFile(path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" {
		return "", false, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}

	password := strings.TrimSpace(string(content))
	if password == "" {
		return "", false, nil
	}

	return password, true, nil
}

func writePasswordFile(path, password string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("mariadb root password file path is empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(password+"\n"), 0o600)
}

func generatePassword() (string, error) {
	randomBytes := make([]byte, passwordBytesLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func (s *Service) queryRows(ctx context.Context, query string) ([][]string, error) {
	output, err := s.runSQL(ctx, query)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(output) == "" {
		return make([][]string, 0), nil
	}

	lines := strings.Split(output, "\n")
	rows := make([][]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}

	return rows, nil
}

func (s *Service) queryCount(ctx context.Context, query string) (int, error) {
	output, err := s.runSQL(ctx, query)
	if err != nil {
		return 0, err
	}

	firstLine := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			firstLine = line
			break
		}
	}

	if firstLine == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(firstLine)
	if err != nil {
		return 0, fmt.Errorf("unexpected SQL count result: %q", firstLine)
	}

	return value, nil
}

func (s *Service) databaseExists(ctx context.Context, databaseName string) (bool, error) {
	count, err := s.queryCount(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = %s",
		quoteLiteral(databaseName),
	))
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (s *Service) userHasGrant(ctx context.Context, databaseName, username, host string) (bool, error) {
	count, err := s.queryCount(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM mysql.db WHERE Db = %s AND User = %s AND Host = %s",
		quoteLiteral(databaseName),
		quoteLiteral(username),
		quoteLiteral(host),
	))
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (s *Service) dropUserIfUnused(ctx context.Context, username, host string) error {
	grantCount, err := s.queryCount(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM mysql.db WHERE User = %s AND Host = %s",
		quoteLiteral(username),
		quoteLiteral(host),
	))
	if err != nil {
		return err
	}
	if grantCount > 0 {
		return nil
	}

	_, err = s.runSQL(ctx, fmt.Sprintf("DROP USER IF EXISTS %s@%s", quoteLiteral(username), quoteLiteral(host)))
	return err
}

func validateCreateInput(input CreateDatabaseInput) ValidationErrors {
	validation := ValidationErrors{}
	if message := validateIdentifier(input.Name, "Database name"); message != "" {
		validation["name"] = message
	} else if isSystemDatabase(input.Name) {
		validation["name"] = "Choose a different database name."
	}

	if message := validateIdentifier(input.Username, "Username"); message != "" {
		validation["username"] = message
	}

	if len(input.Password) < 8 {
		validation["password"] = "Password must be at least 8 characters."
	}

	return validation
}

func validateUpdateInput(databaseName string, input UpdateDatabaseInput) ValidationErrors {
	validation := ValidationErrors{}
	if message := validateIdentifier(databaseName, "Database name"); message != "" {
		validation["name"] = message
	} else if isSystemDatabase(databaseName) {
		validation["name"] = "System databases cannot be modified."
	}

	if message := validateIdentifier(input.CurrentUsername, "Current username"); message != "" {
		validation["current_username"] = message
	}

	if message := validateIdentifier(input.Username, "Username"); message != "" {
		validation["username"] = message
	}

	if input.Password == "" {
		if input.CurrentUsername != "" && input.Username != "" && input.CurrentUsername != input.Username {
			validation["password"] = "Password is required when changing username."
		}
	} else if len(input.Password) < 8 {
		validation["password"] = "Password must be at least 8 characters."
	}

	return validation
}

func validateDeleteInput(databaseName string, input DeleteDatabaseInput) ValidationErrors {
	validation := ValidationErrors{}
	if message := validateIdentifier(databaseName, "Database name"); message != "" {
		validation["name"] = message
	} else if isSystemDatabase(databaseName) {
		validation["name"] = "System databases cannot be deleted."
	}

	if input.Username != "" {
		if message := validateIdentifier(input.Username, "Username"); message != "" {
			validation["username"] = message
		}
	}

	return validation
}

func validateIdentifier(value, label string) string {
	if value == "" {
		return fmt.Sprintf("%s is required.", label)
	}
	if !identifierPattern.MatchString(value) {
		return fmt.Sprintf("%s can contain only letters, numbers, and underscores.", label)
	}

	return ""
}

func isSystemDatabase(name string) bool {
	_, ok := systemDatabases[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func quoteIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func envWithDefault(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}

	return trimmed
}

func detectActionPlan() actionPlan {
	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			return actionPlan{
				packageManager: "homebrew",
				installLabel:   "Install MariaDB",
				removeLabel:    "Remove MariaDB",
				startLabel:     "Start MariaDB",
				stopLabel:      "Stop MariaDB",
				restartLabel:   "Restart MariaDB",
				installCmds: [][]string{
					{brewPath, "install", "mariadb"},
				},
				removeCmds: [][]string{
					{brewPath, "uninstall", "mariadb"},
				},
				startCmds: [][]string{
					{brewPath, "services", "start", "mariadb"},
				},
				stopCmds: [][]string{
					{brewPath, "services", "stop", "mariadb"},
				},
				restartCmds: [][]string{
					{brewPath, "services", "restart", "mariadb"},
				},
			}
		}
	case "linux":
		if os.Geteuid() == 0 {
			if aptPath, ok := lookupCommand("apt-get"); ok {
				if systemctlPath, ok := lookupCommand("systemctl"); ok {
					return actionPlan{
						packageManager: "apt",
						installLabel:   "Install MariaDB",
						removeLabel:    "Remove MariaDB",
						startLabel:     "Start MariaDB",
						stopLabel:      "Stop MariaDB",
						restartLabel:   "Restart MariaDB",
						installCmds: [][]string{
							{aptPath, "update"},
							{aptPath, "install", "-y", "mariadb-server", "mariadb-client"},
						},
						removeCmds: [][]string{
							{aptPath, "remove", "-y", "mariadb-server", "mariadb-client"},
						},
						startCmds: [][]string{
							{systemctlPath, "start", "mariadb"},
						},
						stopCmds: [][]string{
							{systemctlPath, "stop", "mariadb"},
						},
						restartCmds: [][]string{
							{systemctlPath, "restart", "mariadb"},
						},
					}
				}
				return actionPlan{
					packageManager: "apt",
					installLabel:   "Install MariaDB",
					removeLabel:    "Remove MariaDB",
					installCmds: [][]string{
						{aptPath, "update"},
						{aptPath, "install", "-y", "mariadb-server", "mariadb-client"},
					},
					removeCmds: [][]string{
						{aptPath, "remove", "-y", "mariadb-server", "mariadb-client"},
					},
				}
			}
			if dnfPath, ok := lookupCommand("dnf"); ok {
				if systemctlPath, ok := lookupCommand("systemctl"); ok {
					return actionPlan{
						packageManager: "dnf",
						installLabel:   "Install MariaDB",
						removeLabel:    "Remove MariaDB",
						startLabel:     "Start MariaDB",
						stopLabel:      "Stop MariaDB",
						restartLabel:   "Restart MariaDB",
						installCmds: [][]string{
							{dnfPath, "install", "-y", "mariadb-server", "mariadb"},
						},
						removeCmds: [][]string{
							{dnfPath, "remove", "-y", "mariadb-server", "mariadb"},
						},
						startCmds: [][]string{
							{systemctlPath, "start", "mariadb"},
						},
						stopCmds: [][]string{
							{systemctlPath, "stop", "mariadb"},
						},
						restartCmds: [][]string{
							{systemctlPath, "restart", "mariadb"},
						},
					}
				}
				return actionPlan{
					packageManager: "dnf",
					installLabel:   "Install MariaDB",
					removeLabel:    "Remove MariaDB",
					installCmds: [][]string{
						{dnfPath, "install", "-y", "mariadb-server", "mariadb"},
					},
					removeCmds: [][]string{
						{dnfPath, "remove", "-y", "mariadb-server", "mariadb"},
					},
				}
			}
			if yumPath, ok := lookupCommand("yum"); ok {
				if systemctlPath, ok := lookupCommand("systemctl"); ok {
					return actionPlan{
						packageManager: "yum",
						installLabel:   "Install MariaDB",
						removeLabel:    "Remove MariaDB",
						startLabel:     "Start MariaDB",
						stopLabel:      "Stop MariaDB",
						restartLabel:   "Restart MariaDB",
						installCmds: [][]string{
							{yumPath, "install", "-y", "mariadb-server", "mariadb"},
						},
						removeCmds: [][]string{
							{yumPath, "remove", "-y", "mariadb-server", "mariadb"},
						},
						startCmds: [][]string{
							{systemctlPath, "start", "mariadb"},
						},
						stopCmds: [][]string{
							{systemctlPath, "stop", "mariadb"},
						},
						restartCmds: [][]string{
							{systemctlPath, "restart", "mariadb"},
						},
					}
				}
				return actionPlan{
					packageManager: "yum",
					installLabel:   "Install MariaDB",
					removeLabel:    "Remove MariaDB",
					installCmds: [][]string{
						{yumPath, "install", "-y", "mariadb-server", "mariadb"},
					},
					removeCmds: [][]string{
						{yumPath, "remove", "-y", "mariadb-server", "mariadb"},
					},
				}
			}
			if pacmanPath, ok := lookupCommand("pacman"); ok {
				if systemctlPath, ok := lookupCommand("systemctl"); ok {
					return actionPlan{
						packageManager: "pacman",
						installLabel:   "Install MariaDB",
						removeLabel:    "Remove MariaDB",
						startLabel:     "Start MariaDB",
						stopLabel:      "Stop MariaDB",
						restartLabel:   "Restart MariaDB",
						installCmds: [][]string{
							{pacmanPath, "-Sy", "--noconfirm", "mariadb"},
						},
						removeCmds: [][]string{
							{pacmanPath, "-Rns", "--noconfirm", "mariadb"},
						},
						startCmds: [][]string{
							{systemctlPath, "start", "mariadb"},
						},
						stopCmds: [][]string{
							{systemctlPath, "stop", "mariadb"},
						},
						restartCmds: [][]string{
							{systemctlPath, "restart", "mariadb"},
						},
					}
				}
				return actionPlan{
					packageManager: "pacman",
					installLabel:   "Install MariaDB",
					removeLabel:    "Remove MariaDB",
					installCmds: [][]string{
						{pacmanPath, "-Sy", "--noconfirm", "mariadb"},
					},
					removeCmds: [][]string{
						{pacmanPath, "-Rns", "--noconfirm", "mariadb"},
					},
				}
			}
		}
	}

	return actionPlan{}
}

func inspectVersion(ctx context.Context, clientPath string, clientInstalled bool, serverPath string, serverInstalled bool) (string, error) {
	switch {
	case clientInstalled:
		return runInspectCommand(ctx, clientPath, "--version")
	case serverInstalled:
		return runInspectCommand(ctx, serverPath, "--version")
	default:
		return "", nil
	}
}

func lookupFirstCommand(candidates ...string) (string, bool) {
	for _, candidate := range candidates {
		if path, ok := lookupCommand(candidate); ok {
			return path, true
		}
	}

	return "", false
}

func lookupCommand(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	for _, dir := range []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/usr/sbin",
	} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, true
	}

	return "", false
}

func runInspectCommand(ctx context.Context, name string, args ...string) (string, error) {
	inspectCtx := ctx
	if inspectCtx == nil {
		inspectCtx = context.Background()
	}

	if _, ok := inspectCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		inspectCtx, cancel = context.WithTimeout(inspectCtx, statusCommandTimeout)
		defer cancel()
	}

	return runCommand(inspectCtx, name, args...)
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return combinedOutput, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return combinedOutput, fmt.Errorf("%s timed out", name)
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		return combinedOutput, fmt.Errorf("%s was canceled", name)
	}

	if combinedOutput == "" {
		return combinedOutput, fmt.Errorf("%s failed: %w", name, err)
	}

	return combinedOutput, fmt.Errorf("%s failed: %s", name, combinedOutput)
}

func runCommands(ctx context.Context, commands ...[]string) error {
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if _, err := runCommand(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}

	return nil
}

func detectReachableAddress() (string, bool) {
	for _, socketPath := range socketCandidates {
		if !pathExists(socketPath) {
			continue
		}

		if canDial("unix", socketPath) {
			return socketPath, true
		}
	}

	if canDial("tcp", "127.0.0.1:3306") {
		return "127.0.0.1:3306", true
	}

	return "", false
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func canDial(network, address string) bool {
	conn, err := net.DialTimeout(network, address, dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()

	return true
}

func parseVersion(output string) (string, string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		lowerLine := strings.ToLower(line)
		switch {
		case strings.Contains(lowerLine, "mariadb"):
			return "MariaDB", line
		case strings.Contains(lowerLine, "mysql"):
			return "MySQL", line
		default:
			return "", line
		}
	}

	return "", ""
}
