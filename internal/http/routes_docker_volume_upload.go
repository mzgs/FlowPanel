package httpx

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/dockercontainer"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const (
	dockerVolumeArchiveMaxFiles       = 100_000
	dockerVolumeArchiveMaxSize  int64 = 16 << 30
)

var dockerVolumeUploadMu sync.Mutex

func (a *apiRoutes) uploadDockerVolumeData(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if a.app.Docker == nil {
		writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
		return
	}

	containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
	if containerID == "" {
		writeValidationFailed(w, map[string]string{"container_id": "Container ID is required."})
		return
	}

	r.Body = stdhttp.MaxBytesReader(w, r.Body, maxFileUploadBytes)
	if err := r.ParseMultipartForm(multipartFormMemoryMax); err != nil {
		var maxBytesError *stdhttp.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, stdhttp.StatusRequestEntityTooLarge, map[string]any{"error": "upload exceeds the 8 GB limit"})
			return
		}
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid volume data upload"})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	archive, header, err := r.FormFile("archive")
	if err != nil {
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "upload a ZIP file in the archive field"})
		return
	}
	defer archive.Close()
	if !strings.EqualFold(filepath.Ext(header.Filename), ".zip") {
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "volume data must be a ZIP file"})
		return
	}

	dockerVolumeUploadMu.Lock()
	defer dockerVolumeUploadMu.Unlock()
	commandCtx, cancel := context.WithTimeout(backgroundRequestContext(r.Context()), 10*time.Minute)
	defer cancel()
	record, err := inspectDockerContainerConfig(commandCtx, containerID)
	if err != nil {
		writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}

	source, ok := dockerBindVolumeSource(record, r.FormValue("source"))
	if !ok {
		writeValidationFailed(w, map[string]string{"source": "Choose an existing bind-mounted source directory for this container."})
		return
	}
	reader, err := zip.NewReader(archive, header.Size)
	if err != nil || validateDockerVolumeArchive(reader) != nil {
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "uploaded file is not a safe, valid ZIP archive"})
		return
	}

	container, err := replaceDockerVolumeData(commandCtx, containerID, record, source, reader)
	if err != nil {
		a.app.Logger.Error("replace docker volume data failed", zap.String("container_id", containerID), zap.String("source", source), zap.Error(err))
		a.mutationEvent(commandCtx, "runtime", "upload", "docker_volume", containerID, source, "failed", err.Error())
		writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}

	a.mutationEvent(commandCtx, "runtime", "upload", "docker_volume", container.ID, source, "succeeded", fmt.Sprintf("Replaced Docker volume data in %q and restarted the container.", source))
	writeJSON(w, stdhttp.StatusOK, map[string]any{"container": container})
}

func dockerBindVolumeSource(record dockerInspectRecord, requested string) (string, bool) {
	requested = filepath.Clean(strings.TrimSpace(requested))
	if !filepath.IsAbs(requested) {
		return "", false
	}
	for _, mount := range record.Mounts {
		source := filepath.Clean(strings.TrimSpace(mount.Source))
		if mount.Type == "bind" && source == requested {
			info, err := os.Stat(source)
			return source, err == nil && info.IsDir()
		}
	}
	return "", false
}

func validateDockerVolumeArchive(reader *zip.Reader) error {
	if len(reader.File) == 0 || len(reader.File) > dockerVolumeArchiveMaxFiles {
		return errors.New("invalid archive entry count")
	}
	var total uint64
	for _, file := range reader.File {
		if _, ok := dockerVolumeArchivePath(file.Name); !ok || file.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe archive entry")
		}
		total += file.UncompressedSize64
		if total > uint64(dockerVolumeArchiveMaxSize) {
			return errors.New("archive expands beyond the 16 GB limit")
		}
	}
	return nil
}

func dockerVolumeArchivePath(name string) (string, bool) {
	if strings.ContainsRune(name, '\x00') || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return "", false
	}
	relative := filepath.Clean(filepath.FromSlash(name))
	if relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

func replaceDockerVolumeData(ctx context.Context, containerID string, record dockerInspectRecord, source string, reader *zip.Reader) (container dockerContainerListItem, resultErr error) {
	if record.State.Running {
		if _, err := runDockerContainerAction(ctx, containerID, "stop"); err != nil {
			return container, err
		}
	}
	defer func() {
		if resultErr != nil {
			_, _ = runDockerContainerAction(context.WithoutCancel(ctx), containerID, "start")
		}
	}()

	entries, err := os.ReadDir(source)
	if err != nil {
		return container, fmt.Errorf("read Docker volume source %q: %w", source, err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(source, entry.Name())); err != nil {
			return container, fmt.Errorf("clear Docker volume source %q: %w", source, err)
		}
	}
	if err := extractDockerVolumeArchive(reader, source); err != nil {
		return container, err
	}
	if err := dockercontainer.PrepareVolumePermissions(ctx, record, source); err != nil {
		return container, err
	}
	return runDockerContainerAction(ctx, containerID, "restart")
}

func extractDockerVolumeArchive(reader *zip.Reader, source string) error {
	for _, file := range reader.File {
		relative, _ := dockerVolumeArchivePath(file.Name)
		target := filepath.Join(source, relative)
		mode := file.Mode().Perm()
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, mode|0o700); err != nil {
				return fmt.Errorf("create volume directory: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create volume directory: %w", err)
		}
		input, err := file.Open()
		if err != nil {
			return fmt.Errorf("open ZIP entry: %w", err)
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode|0o600)
		if err == nil {
			_, err = io.Copy(output, input)
		}
		input.Close()
		if output != nil {
			if closeErr := output.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			return fmt.Errorf("extract ZIP entry %q: %w", file.Name, err)
		}
	}
	return nil
}
