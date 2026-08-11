package httpx

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	stdhttp "net/http"
	"path"
	"strings"

	"flowpanel/internal/mariadb"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerMariaDBDatabaseRestoreRoute(r chi.Router) {
	r.Method(stdhttp.MethodPost, "/mariadb/databases/{databaseName}/restore", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}
		databaseName := strings.TrimSpace(chi.URLParam(r, "databaseName"))
		databases, err := a.app.MariaDB.ListDatabases(r.Context())
		if err != nil {
			a.app.Logger.Error("list mariadb databases before restore failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to verify database"})
			return
		}
		found := false
		for _, database := range databases {
			if database.Name == databaseName {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "database not found"})
			return
		}

		r.Body = stdhttp.MaxBytesReader(w, r.Body, maxFileUploadBytes)
		parseErr := r.ParseMultipartForm(multipartFormMemoryMax)
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		if parseErr != nil {
			var maxBytesError *stdhttp.MaxBytesError
			if errors.As(parseErr, &maxBytesError) {
				writeJSON(w, stdhttp.StatusRequestEntityTooLarge, map[string]any{"error": "upload exceeds the 8 GB limit"})
				return
			}
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid database backup upload"})
			return
		}
		file, header, err := r.FormFile("backup")
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "upload one .sql, .zip, or .tar.gz file in the backup field"})
			return
		}
		defer file.Close()

		if err := restoreUploadedDatabase(r.Context(), a.app.MariaDB, databaseName, header.Filename, header.Size, file); err != nil {
			var uploadError databaseRestoreUploadError
			if errors.As(err, &uploadError) {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": uploadError.Error()})
				return
			}
			a.app.Logger.Error("restore uploaded mariadb database failed", zap.String("database_name", databaseName), zap.String("file_name", header.Filename), zap.Error(err))
			a.mutationEvent(r.Context(), "database", "restore", "database", databaseName, databaseName, "failed", "Failed to restore the uploaded database backup.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to restore database backup"})
			return
		}

		a.mutationEvent(r.Context(), "database", "restore", "database", databaseName, databaseName, "succeeded", fmt.Sprintf("Restored database %q from uploaded backup %q.", databaseName, header.Filename))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"restored": true})
	}))
}

type databaseRestoreUploadError string

func (e databaseRestoreUploadError) Error() string { return string(e) }

const maxDatabaseRestoreArchiveFiles = 100_000

func restoreUploadedDatabase(ctx context.Context, manager mariadb.Manager, databaseName, fileName string, size int64, file multipart.File) error {
	lowerName := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(lowerName, ".sql"):
		return manager.RestoreDatabase(ctx, databaseName, file)
	case strings.HasSuffix(lowerName, ".zip"):
		entryCount, directorySize, err := inspectZipReaderDirectory(file, size)
		if err != nil || entryCount > maxDatabaseRestoreArchiveFiles || directorySize > dockerVolumeDirectoryMaxSize {
			return databaseRestoreUploadError("ZIP archive directory exceeds safety limits")
		}
		reader, err := zip.NewReader(file, size)
		if err != nil {
			return databaseRestoreUploadError("uploaded file is not a valid ZIP archive")
		}
		if len(reader.File) > maxDatabaseRestoreArchiveFiles {
			return databaseRestoreUploadError("ZIP archive contains too many files")
		}
		names := make([]string, 0, len(reader.File))
		entries := make(map[string]*zip.File)
		for _, entry := range reader.File {
			if !entry.FileInfo().IsDir() && strings.HasSuffix(strings.ToLower(entry.Name), ".sql") {
				if entry.UncompressedSize64 > uint64(maxFileUploadBytes) {
					return databaseRestoreUploadError("SQL dump in the ZIP archive exceeds the 8 GB limit")
				}
				names = append(names, entry.Name)
				entries[entry.Name] = entry
			}
		}
		selected, err := selectDatabaseDump(databaseName, names)
		if err != nil {
			return err
		}
		dump, err := entries[selected].Open()
		if err != nil {
			return databaseRestoreUploadError("failed to read the SQL dump from the ZIP archive")
		}
		defer dump.Close()
		return manager.RestoreDatabase(ctx, databaseName, dump)
	case strings.HasSuffix(lowerName, ".tar.gz"), strings.HasSuffix(lowerName, ".tgz"):
		names, err := tarGzipSQLNames(file)
		if err != nil {
			return err
		}
		selected, err := selectDatabaseDump(databaseName, names)
		if err != nil {
			return err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return databaseRestoreUploadError("failed to read the TAR.GZ archive")
		}
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return databaseRestoreUploadError("uploaded file is not a valid TAR.GZ archive")
		}
		defer gzipReader.Close()
		tarReader := tar.NewReader(gzipReader)
		for {
			header, err := tarReader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return databaseRestoreUploadError("uploaded file is not a valid TAR.GZ archive")
			}
			if header.Name == selected && header.FileInfo().Mode().IsRegular() {
				return manager.RestoreDatabase(ctx, databaseName, io.LimitReader(tarReader, header.Size))
			}
		}
		return databaseRestoreUploadError("the selected SQL dump could not be read from the TAR.GZ archive")
	default:
		return databaseRestoreUploadError("database backup must be a .sql, .zip, or .tar.gz file")
	}
}

func tarGzipSQLNames(file multipart.File) ([]string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, databaseRestoreUploadError("failed to read the TAR.GZ archive")
	}
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, databaseRestoreUploadError("uploaded file is not a valid TAR.GZ archive")
	}
	defer gzipReader.Close()

	var names []string
	tarReader := tar.NewReader(gzipReader)
	entryCount := 0
	metadataSize := 0
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, databaseRestoreUploadError("uploaded file is not a valid TAR.GZ archive")
		}
		entryCount++
		metadataSize += len(header.Name)
		if entryCount > maxDatabaseRestoreArchiveFiles || len(header.Name) > 4096 || metadataSize > dockerVolumeDirectoryMaxSize {
			return nil, databaseRestoreUploadError("TAR.GZ archive contains too many files")
		}
		if header.FileInfo().Mode().IsRegular() && strings.HasSuffix(strings.ToLower(header.Name), ".sql") {
			if header.Size > maxFileUploadBytes {
				return nil, databaseRestoreUploadError("SQL dump in the TAR.GZ archive exceeds the 8 GB limit")
			}
			names = append(names, header.Name)
		}
	}
}

func selectDatabaseDump(databaseName string, names []string) (string, error) {
	wanted := databaseName + ".sql"
	var matches []string
	for _, name := range names {
		if path.Base(name) == wanted {
			matches = append(matches, name)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", databaseRestoreUploadError(fmt.Sprintf("archive contains multiple %s files", wanted))
	}
	if len(names) == 1 {
		return names[0], nil
	}
	if len(names) == 0 {
		return "", databaseRestoreUploadError("archive does not contain an SQL dump")
	}
	return "", databaseRestoreUploadError(fmt.Sprintf("archive contains multiple SQL dumps and none is named %s", wanted))
}
