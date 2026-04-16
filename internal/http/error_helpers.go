package httpx

import (
	"errors"
	"io/fs"
	stdhttp "net/http"
	"regexp"
	"strings"

	"flowpanel/internal/backup"
	filesvc "flowpanel/internal/files"
)

var listenerConflictPattern = regexp.MustCompile(`listen tcp (?P<address>\[[^\]]+\]:\d+|[^:[:space:]]*:\d+)`)

func eventErrorMessage(message string, err error) string {
	message = strings.TrimSpace(message)
	if err == nil {
		return message
	}

	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		return message
	}
	if message == "" {
		return detail
	}

	return message + "\n\nError: " + detail
}

func syncDomainsErrorResponse(err error, fallback string) (int, string) {
	message := strings.TrimSpace(fallback)
	if message == "" {
		message = "failed to refresh domain routing"
	}
	if err == nil {
		return stdhttp.StatusInternalServerError, message
	}

	detail := strings.ToLower(strings.TrimSpace(err.Error()))
	if !strings.Contains(detail, "address already in use") {
		return stdhttp.StatusInternalServerError, message
	}

	address := listenerConflictAddress(err.Error())
	switch {
	case strings.HasSuffix(address, ":443"):
		return stdhttp.StatusConflict, "FlowPanel could not publish the domain because another process is already listening on HTTPS port 443. Stop the service or Docker container using port 443, then retry."
	case strings.HasSuffix(address, ":80"):
		return stdhttp.StatusConflict, "FlowPanel could not publish the domain because another process is already listening on HTTP port 80. Stop the service or Docker container using port 80, then retry."
	default:
		return stdhttp.StatusConflict, "FlowPanel could not publish the domain because one of its public ports is already in use. Stop the conflicting service or Docker container, then retry."
	}
}

func listenerConflictAddress(message string) string {
	match := listenerConflictPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(message)))
	if len(match) < 2 {
		return ""
	}

	return strings.TrimSpace(match[1])
}

func writeFileError(w stdhttp.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filesvc.ErrNotFound):
		writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "file or directory not found"})
	case errors.Is(err, filesvc.ErrInvalidPath):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid file path"})
	case errors.Is(err, filesvc.ErrDirectoryExpected):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "directory expected"})
	case errors.Is(err, filesvc.ErrFileExpected):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "file expected"})
	case errors.Is(err, filesvc.ErrUnsupportedEntry):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "symlinks are not supported"})
	case errors.Is(err, filesvc.ErrBinaryFile):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "file is not editable as text"})
	case errors.Is(err, filesvc.ErrEditableFileTooBig):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "file is too large to edit in the panel"})
	case errors.Is(err, filesvc.ErrInvalidTransfer):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid move or copy operation"})
	case errors.Is(err, filesvc.ErrInvalidPermissions):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid permissions value"})
	case errors.Is(err, filesvc.ErrUnsupportedArchive):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "unsupported archive format"})
	case errors.Is(err, filesvc.ErrInvalidArchive):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid archive contents"})
	case errors.Is(err, fs.ErrExist):
		writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": "file already exists"})
	default:
		writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "file operation failed"})
	}
}

func writeBackupError(w stdhttp.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "backup not found"})
	case errors.Is(err, backup.ErrInvalidName):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup name"})
	case errors.Is(err, backup.ErrInvalidArchive):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup archive"})
	case errors.Is(err, backup.ErrAlreadyExists):
		writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": "backup already exists"})
	case errors.Is(err, backup.ErrInvalidLocation):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup location"})
	default:
		writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
}
