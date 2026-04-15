package httpx

import (
	"fmt"
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerFileRoutes(r chi.Router) {
	filesListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		listing, err := a.app.Files.List(r.URL.Query().Get("path"))
		if err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, listing)
	})

	filesCreateDirectoryHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if err := a.app.Files.CreateDirectory(input.Path, input.Name); err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusCreated, map[string]any{"ok": true})
	})

	filesCreateFileHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if err := a.app.Files.CreateFile(input.Path, input.Name); err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusCreated, map[string]any{"ok": true})
	})

	filesRenameHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		newPath, err := a.app.Files.Rename(input.Path, input.Name)
		if err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"path": newPath})
	})

	filesDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		if err := a.app.Files.Delete(trimmedQuery(r, "path")); err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})

	filesContentHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		content, err := a.app.Files.ReadTextFile(r.URL.Query().Get("path"))
		if err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, content)
	})

	filesUpdateContentHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if err := a.app.Files.WriteTextFile(input.Path, input.Content); err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})

	filesUpdatePermissionsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Path        string `json:"path"`
			Permissions string `json:"permissions"`
			Recursive   bool   `json:"recursive"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if err := a.app.Files.SetPermissions(input.Path, input.Permissions, input.Recursive); err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})

	filesUploadHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid upload payload"})
			return
		}

		createdPaths, err := a.app.Files.Upload(r.FormValue("path"), r.MultipartForm.File["files"])
		if err != nil {
			writeFileError(w, err)
			return
		}
		uploadDirectory, _, err := a.app.Files.ResolveDirectory(r.FormValue("path"))
		if err != nil {
			writeFileError(w, err)
			return
		}
		if err := ensurePHPUploadWorkerOwnership(r.Context(), a.app.PHP, a.app.Domains, uploadDirectory, createdPaths); err != nil {
			a.app.Logger.Error("apply php worker ownership after upload failed", zap.String("path", uploadDirectory), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "files uploaded but php-fpm ownership could not be updated"})
			return
		}

		writeJSON(w, stdhttp.StatusCreated, map[string]any{"ok": true})
	})

	filesDownloadHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		absolutePath, name, cleanup, err := a.app.Files.DownloadPath(r.URL.Query().Get("path"))
		if err != nil {
			writeFileError(w, err)
			return
		}
		defer cleanup()

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		stdhttp.ServeFile(w, r, absolutePath)
	})

	filesDownloadArchiveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Paths []string `json:"paths"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		name, writeArchive, err := a.app.Files.PrepareDownloadPaths(input.Paths)
		if err != nil {
			writeFileError(w, err)
			return
		}

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		w.Header().Set("Content-Type", "application/gzip")
		if err := writeArchive(w); err != nil {
			a.app.Logger.Error("stream file archive failed", zap.Error(err))
		}
	})

	filesCreateArchiveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Paths       []string `json:"paths"`
			Destination string   `json:"destination"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		archivePath, err := a.app.Files.CreateArchive(input.Paths, input.Destination)
		if err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusCreated, map[string]any{"path": archivePath})
	})

	filesExtractArchiveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Path string `json:"path"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if err := a.app.Files.ExtractArchive(input.Path); err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})

	filesTransferHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Files == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "file manager is not configured"})
			return
		}

		var input struct {
			Mode   string   `json:"mode"`
			Paths  []string `json:"paths"`
			Target string   `json:"target"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if err := a.app.Files.Transfer(input.Mode, input.Paths, input.Target); err != nil {
			writeFileError(w, err)
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})

	r.Method(stdhttp.MethodGet, "/files", filesListHandler)
	r.Method(stdhttp.MethodPost, "/files/directories", filesCreateDirectoryHandler)
	r.Method(stdhttp.MethodPost, "/files/documents", filesCreateFileHandler)
	r.Method(stdhttp.MethodPost, "/files/rename", filesRenameHandler)
	r.Method(stdhttp.MethodDelete, "/files", filesDeleteHandler)
	r.Method(stdhttp.MethodGet, "/files/content", filesContentHandler)
	r.Method(stdhttp.MethodPut, "/files/content", filesUpdateContentHandler)
	r.Method(stdhttp.MethodPut, "/files/permissions", filesUpdatePermissionsHandler)
	r.Method(stdhttp.MethodPost, "/files/upload", filesUploadHandler)
	r.Method(stdhttp.MethodGet, "/files/download", filesDownloadHandler)
	r.Method(stdhttp.MethodPost, "/files/download", filesDownloadArchiveHandler)
	r.Method(stdhttp.MethodPost, "/files/archive", filesCreateArchiveHandler)
	r.Method(stdhttp.MethodPost, "/files/extract", filesExtractArchiveHandler)
	r.Method(stdhttp.MethodPost, "/files/transfer", filesTransferHandler)
}
