package httpx

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/domain"
	filesvc "flowpanel/internal/files"
	"flowpanel/internal/ftp"
	"flowpanel/internal/mariadb"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerDomainRoutes(r chi.Router) {
	ftpAccountsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.FTPAccounts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "ftp accounts are not configured"})
			return
		}

		records, err := a.app.FTPAccounts.ListAccounts(r.Context())
		if err != nil {
			a.app.Logger.Error("list ftp accounts failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to list ftp accounts"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"accounts": records})
	})

	ftpAccountsCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.FTPAccounts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "ftp accounts are not configured"})
			return
		}

		var input ftp.CreateAccountInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		record, err := a.app.FTPAccounts.CreateAccount(r.Context(), input)
		if err != nil {
			var validation ftp.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}

			a.app.Logger.Error("create ftp account failed", zap.Error(err))
			a.mutationEvent(r.Context(), "ftp", "create", "ftp_account", input.Username, input.Username, "failed", "Failed to create the FTP account.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to create ftp account"})
			return
		}

		a.mutationEvent(r.Context(), "ftp", "create", "ftp_account", record.ID, record.Username, "succeeded", fmt.Sprintf("Created FTP account %q.", record.Username))
		writeJSON(w, stdhttp.StatusCreated, map[string]any{"account": record})
	})

	ftpAccountsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.FTPAccounts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "ftp accounts are not configured"})
			return
		}

		var input ftp.UpdateAccountInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		accountID := chi.URLParam(r, "accountID")
		record, err := a.app.FTPAccounts.UpdateAccount(r.Context(), accountID, input)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "ftp account not found"})
				return
			}

			var validation ftp.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}

			a.app.Logger.Error("update ftp account failed", zap.String("account_id", accountID), zap.Error(err))
			a.mutationEvent(r.Context(), "ftp", "update", "ftp_account", accountID, accountID, "failed", "Failed to update the FTP account.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update ftp account"})
			return
		}

		a.mutationEvent(r.Context(), "ftp", "update", "ftp_account", record.ID, record.Username, "succeeded", fmt.Sprintf("Updated FTP account %q.", record.Username))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"account": record})
	})

	ftpAccountsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.FTPAccounts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "ftp accounts are not configured"})
			return
		}

		accountID := chi.URLParam(r, "accountID")
		if err := a.app.FTPAccounts.DeleteAccount(r.Context(), accountID); err != nil {
			a.app.Logger.Error("delete ftp account failed", zap.String("account_id", accountID), zap.Error(err))
			a.mutationEvent(r.Context(), "ftp", "delete", "ftp_account", accountID, accountID, "failed", "Failed to delete the FTP account.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to delete ftp account"})
			return
		}

		a.mutationEvent(r.Context(), "ftp", "delete", "ftp_account", accountID, accountID, "succeeded", "Deleted the FTP account.")
		writeJSON(w, stdhttp.StatusOK, map[string]any{"deleted": true})
	})

	domainsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"sites_base_path": a.app.Domains.BasePath(),
			"domains":         a.app.Domains.List(),
		})
	})

	domainsLogsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostnameFilter := normalizeDomainLogHostname(r.URL.Query().Get("hostname"))
		typeFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
		if typeFilter == "" {
			typeFilter = "all"
		}
		switch typeFilter {
		case "all", "access", "error":
		default:
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "type must be one of all, access, or error"})
			return
		}

		limit := 200
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			parsedLimit, err := strconv.Atoi(rawLimit)
			if err != nil || parsedLimit < 1 || parsedLimit > 1000 {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "limit must be an integer between 1 and 1000"})
				return
			}
			limit = parsedLimit
		}

		search := strings.TrimSpace(r.URL.Query().Get("search"))
		records := a.app.Domains.List()
		hostnames := make([]string, 0, len(records))
		logs := make([]domainLogResponse, 0, len(records)*2)
		for _, record := range records {
			hostnames = append(hostnames, record.Hostname)
			if hostnameFilter != "" && record.Hostname != hostnameFilter {
				continue
			}

			if typeFilter == "all" || typeFilter == "access" {
				logs = append(logs, readDomainLog(record.Hostname, "access", record.Logs.Access, search, limit))
			}
			if typeFilter == "all" || typeFilter == "error" {
				logs = append(logs, readDomainLog(record.Hostname, "error", record.Logs.Error, search, limit))
			}
		}
		sort.Strings(hostnames)

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"hostnames": hostnames,
			"filters": map[string]any{
				"hostname": hostnameFilter,
				"type":     typeFilter,
				"search":   search,
				"limit":    limit,
			},
			"logs": logs,
		})
	})

	domainsPreviewHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		refreshRequested := queryEnabled(r, "refresh")

		var (
			previewPath string
			err         error
		)
		if refreshRequested {
			previewPath, err = a.app.Domains.RefreshPreview(r.Context(), hostname)
		} else {
			previewPath, err = a.app.Domains.EnsurePreview(r.Context(), hostname)
		}
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			} else {
				a.app.Logger.Error("load domain preview failed", zap.String("hostname", hostname), zap.Error(err))
				writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": "failed to load domain preview"})
			}
			return
		}

		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Header().Set("Content-Type", "image/png")
		stdhttp.ServeFile(w, r, previewPath)
	})

	domainsWebsiteCopyHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		sourceRecord, ok := a.app.Domains.FindByHostname(hostname)
		if !ok {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			return
		}

		var input struct {
			TargetHostname     string `json:"target_hostname"`
			ReplaceTargetFiles bool   `json:"replace_target_files"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		validation := domain.ValidationErrors{}
		if !isSiteBackedDomainRecord(sourceRecord) {
			validation["kind"] = "Website copying is not available for this domain."
		}

		targetHostname := strings.TrimSpace(input.TargetHostname)
		var targetRecord domain.Record
		if targetHostname == "" {
			validation["target_hostname"] = "Select a destination domain."
		} else {
			record, exists := a.app.Domains.FindByHostname(targetHostname)
			if !exists {
				validation["target_hostname"] = "Select a valid destination domain."
			} else {
				targetRecord = record
				if !isSiteBackedDomainRecord(targetRecord) {
					validation["target_hostname"] = "Destination domain is not available for website copying."
				}
				if targetRecord.Hostname == sourceRecord.Hostname {
					validation["target_hostname"] = "Choose a different destination domain."
				}
			}
		}

		if len(validation) > 0 {
			writeValidationFailed(w, map[string]string(validation))
			return
		}

		if err := copyDomainDocumentRoot(sourceRecord, targetRecord, a.app.Domains.BasePath(), input.ReplaceTargetFiles); err != nil {
			switch {
			case errors.Is(err, errDomainCopyConflict):
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": "target directory already contains files that would be replaced"})
			case errors.Is(err, errDomainCopyInvalidTarget):
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "validation failed",
					"field_errors": map[string]string{
						"target_hostname": "Destination domain must use a different document root.",
					},
				})
			default:
				a.app.Logger.Error("copy website failed", zap.String("source_hostname", sourceRecord.Hostname), zap.String("target_hostname", targetRecord.Hostname), zap.Error(err))
				a.mutationEvent(r.Context(), "domains", "copy", "website", sourceRecord.Hostname, sourceRecord.Hostname, "failed", "Failed to copy website files.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to copy website"})
			}
			return
		}

		if err := a.app.Domains.InvalidatePreview(targetRecord.Hostname); err != nil {
			a.app.Logger.Warn("invalidate copied domain preview failed", zap.String("hostname", targetRecord.Hostname), zap.Error(err))
		}

		a.mutationEvent(r.Context(), "domains", "copy", "website", sourceRecord.Hostname, sourceRecord.Hostname, "succeeded", fmt.Sprintf(`Copied website files from %q to %q.`, sourceRecord.Hostname, targetRecord.Hostname))
		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"ok":              true,
			"source_hostname": sourceRecord.Hostname,
			"target_hostname": targetRecord.Hostname,
		})
	})

	domainsComposerActionHandler := func(action string) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			record, err := runDomainComposerAction(r.Context(), a.app.Domains, hostname, action)
			if err != nil {
				switch {
				case errors.Is(err, domain.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
				case errors.Is(err, errComposerUnsupportedDomain), errors.Is(err, errComposerMissingManifest):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				case errors.Is(err, errComposerUnavailable):
					writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
				default:
					a.app.Logger.Error("run composer command failed", zap.String("hostname", hostname), zap.String("action", action), zap.Error(err))
					a.mutationEvent(r.Context(), "domains", "composer_"+action, "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Failed to run composer %s for %q.", action, record.Hostname))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				}
				return
			}

			a.mutationEvent(r.Context(), "domains", "composer_"+action, "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Ran composer %s for %q.", action, record.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
		}
	}

	domainsTemplateInstallHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		var input domainTemplateInstallInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		result, record, err := installDomainTemplate(r.Context(), a.app.Domains, a.app.MariaDB, hostname, input)
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			case errors.Is(err, errDomainTemplateUnsupportedDomain), errors.Is(err, errDomainTemplateInstallDirectoryDirty), errors.Is(err, errWordPressAlreadyInstalled), errors.Is(err, errWordPressInstallDirectoryDirty):
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			case errors.Is(err, errComposerUnavailable), errors.Is(err, errWordPressCLIUnavailable), errors.Is(err, errWordPressDatabaseUnavailable), errors.Is(err, errDomainTemplateDatabaseUnavailable):
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			default:
				var validation domain.ValidationErrors
				if errors.As(err, &validation) {
					writeValidationFailed(w, map[string]string(validation))
					return
				}

				a.app.Logger.Error("install domain template failed", zap.String("hostname", hostname), zap.String("template", input.Template), zap.Error(err))
				a.mutationEvent(r.Context(), "domains", "template_install", "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Failed to install %q for %q.", input.Template, hostname))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			}
			return
		}

		if err := a.app.Domains.InvalidatePreview(record.Hostname); err != nil {
			a.app.Logger.Warn("invalidate domain preview after template install failed", zap.String("hostname", record.Hostname), zap.Error(err))
		}
		a.mutationEvent(r.Context(), "domains", "template_install", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Installed %q for %q.", result.Template, record.Hostname))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"result": result})
	})

	domainsWordPressStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		section, err := parseWordPressStatusSection(r.URL.Query().Get("section"))
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "section must be one of plugins, themes, or database"})
			return
		}

		status, _, err := loadWordPressStatusSection(
			r.Context(),
			a.app.Domains,
			a.app.MariaDB,
			hostname,
			section,
		)
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			case errors.Is(err, errWordPressUnsupportedDomain):
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			default:
				a.app.Logger.Error("load wordpress status failed", zap.String("hostname", hostname), zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to inspect wordpress toolkit"})
			}
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"wordpress": status})
	})

	domainsWordPressSummaryHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		summary, _, err := loadWordPressSummary(r.Context(), a.app.Domains, hostname)
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			case errors.Is(err, errWordPressUnsupportedDomain):
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			default:
				a.app.Logger.Error("load wordpress summary failed", zap.String("hostname", hostname), zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to inspect wordpress summary"})
			}
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"wordpress": summary})
	})

	domainsWordPressExtensionActionHandler := func(resource string) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			var input wordPressExtensionActionInput
			if err := decodeJSON(r, &input); err != nil {
				writeInvalidRequestBody(w)
				return
			}

			status, record, err := runWordPressExtensionAction(r.Context(), a.app.Domains, a.app.MariaDB, hostname, resource, input)
			if err != nil {
				switch {
				case errors.Is(err, domain.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
				case errors.Is(err, errWordPressUnsupportedDomain), errors.Is(err, errWordPressNotInstalled):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				case errors.Is(err, errWordPressCLIUnavailable):
					writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
				default:
					var validation domain.ValidationErrors
					if errors.As(err, &validation) {
						writeValidationFailed(w, map[string]string(validation))
						return
					}

					a.app.Logger.Error("run wordpress extension action failed", zap.String("hostname", hostname), zap.String("resource", resource), zap.String("action", input.Action), zap.Error(err))
					a.mutationEvent(r.Context(), "domains", "wordpress_"+resource+"_"+input.Action, "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Failed to %s WordPress %s for %q.", input.Action, resource, hostname))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				}
				return
			}

			if err := a.app.Domains.InvalidatePreview(record.Hostname); err != nil {
				a.app.Logger.Warn("invalidate wordpress preview failed", zap.String("hostname", record.Hostname), zap.Error(err))
			}
			a.mutationEvent(r.Context(), "domains", "wordpress_"+resource+"_"+input.Action, "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Ran WordPress %s %s for %q.", resource, input.Action, record.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{"wordpress": status})
		}
	}

	domainsWordPressExtensionSearchHandler := func(resource string) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			results, _, err := searchWordPressExtensions(
				r.Context(),
				a.app.Domains,
				hostname,
				resource,
				r.URL.Query().Get("q"),
			)
			if err != nil {
				switch {
				case errors.Is(err, domain.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
				case errors.Is(err, errWordPressUnsupportedDomain), errors.Is(err, errWordPressNotInstalled):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				case errors.Is(err, errWordPressCLIUnavailable):
					writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
				default:
					var validation domain.ValidationErrors
					if errors.As(err, &validation) {
						writeValidationFailed(w, map[string]string(validation))
						return
					}

					a.app.Logger.Error("search wordpress extension failed", zap.String("hostname", hostname), zap.String("resource", resource), zap.Error(err))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				}
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{"results": results})
		}
	}

	domainsWordPressExtensionInstallHandler := func(resource string) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			var input wordPressExtensionInstallInput
			if err := decodeJSON(r, &input); err != nil {
				writeInvalidRequestBody(w)
				return
			}

			status, record, err := installWordPressExtension(r.Context(), a.app.Domains, a.app.MariaDB, hostname, resource, input)
			if err != nil {
				switch {
				case errors.Is(err, domain.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
				case errors.Is(err, errWordPressUnsupportedDomain), errors.Is(err, errWordPressNotInstalled):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				case errors.Is(err, errWordPressCLIUnavailable):
					writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
				default:
					var validation domain.ValidationErrors
					if errors.As(err, &validation) {
						writeValidationFailed(w, map[string]string(validation))
						return
					}

					a.app.Logger.Error("install wordpress extension failed", zap.String("hostname", hostname), zap.String("resource", resource), zap.String("slug", input.Slug), zap.Error(err))
					a.mutationEvent(r.Context(), "domains", "wordpress_"+resource+"_install", "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Failed to install WordPress %s %q for %q.", resource, input.Slug, hostname))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				}
				return
			}

			if err := a.app.Domains.InvalidatePreview(record.Hostname); err != nil {
				a.app.Logger.Warn("invalidate wordpress preview failed", zap.String("hostname", record.Hostname), zap.Error(err))
			}
			a.mutationEvent(r.Context(), "domains", "wordpress_"+resource+"_install", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Installed WordPress %s %q for %q.", resource, input.Slug, record.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{"wordpress": status})
		}
	}

	domainsGitHubUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		record, ok := a.app.Domains.FindByHostname(hostname)
		if !ok {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			return
		}
		if err := ensureGitHubIntegrationSupported(record); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		var input domainGitHubIntegrationInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		repositoryURL := strings.TrimSpace(input.RepositoryURL)
		postFetchScript := strings.TrimSpace(input.PostFetchScript)
		existingIntegration := record.GitHub
		if repositoryURL == "" {
			if existingIntegration != nil && existingIntegration.WebhookID > 0 {
				if token, err := getGitHubToken(r.Context(), a.app.Settings); err == nil {
					if ref, refErr := parseGitHubRepositoryURL(existingIntegration.RepositoryURL); refErr == nil {
						if err := deleteGitHubWebhook(r.Context(), token, ref, existingIntegration.WebhookID); err != nil {
							a.app.Logger.Warn("delete github webhook failed", zap.String("hostname", record.Hostname), zap.Error(err))
						}
					}
				}
			}

			updatedRecord, err := a.app.Domains.DeleteGitHubIntegration(r.Context(), hostname)
			if err != nil {
				a.app.Logger.Error("delete github integration failed", zap.String("hostname", hostname), zap.Error(err))
				a.mutationEvent(r.Context(), "domains", "github_disconnect", "domain", record.ID, record.Hostname, "failed", "Failed to remove the GitHub integration.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to remove github integration"})
				return
			}

			a.mutationEvent(r.Context(), "domains", "github_disconnect", "domain", updatedRecord.ID, updatedRecord.Hostname, "succeeded", fmt.Sprintf("Removed the GitHub integration for %q.", updatedRecord.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{"domain": updatedRecord})
			return
		}

		token, err := getGitHubToken(r.Context(), a.app.Settings)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
				"error": err.Error(),
				"field_errors": map[string]string{
					"repository_url": "Add a GitHub token in Settings first.",
				},
			})
			return
		}

		repoRef, err := parseGitHubRepositoryURL(repositoryURL)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
				"error": err.Error(),
				"field_errors": map[string]string{
					"repository_url": err.Error(),
				},
			})
			return
		}

		metadata, err := loadGitHubRepositoryMetadata(r.Context(), token, repoRef)
		if err != nil {
			a.app.Logger.Error("load github repository metadata failed", zap.String("hostname", hostname), zap.String("repository", repositoryURL), zap.Error(err))
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}

		now := time.Now().UTC()
		createdAt := now
		webhookID := int64(0)
		webhookSecret := ""
		if existingIntegration != nil {
			createdAt = existingIntegration.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			webhookID = existingIntegration.WebhookID
			webhookSecret = existingIntegration.WebhookSecret
		}

		if existingIntegration != nil && existingIntegration.WebhookID > 0 && !sameGitHubRepository(existingIntegration.RepositoryURL, metadata.CloneURL) {
			if previousRef, refErr := parseGitHubRepositoryURL(existingIntegration.RepositoryURL); refErr == nil {
				if err := deleteGitHubWebhook(r.Context(), token, previousRef, existingIntegration.WebhookID); err != nil {
					a.app.Logger.Error("delete previous github webhook failed", zap.String("hostname", hostname), zap.Error(err))
					writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
					return
				}
			}
			webhookID = 0
			webhookSecret = ""
		}

		if input.AutoDeployOnPush {
			if webhookSecret == "" {
				webhookSecret, err = generateGitHubWebhookSecret()
				if err != nil {
					a.app.Logger.Error("generate github webhook secret failed", zap.String("hostname", hostname), zap.Error(err))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to generate webhook secret"})
					return
				}
			}

			panelURL, panelURLErr := currentPanelURL(r.Context(), a.app)
			if panelURLErr != nil {
				a.app.Logger.Error("load panel url for github webhook failed", zap.String("hostname", hostname), zap.Error(panelURLErr))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load panel url"})
				return
			}

			webhookURL, err := buildGitHubWebhookURL(r, record.Hostname, panelURL)
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}

			webhookID, err = upsertGitHubWebhook(r.Context(), token, repoRef, webhookID, webhookURL, webhookSecret)
			if err != nil {
				a.app.Logger.Error("configure github webhook failed", zap.String("hostname", hostname), zap.String("repository", metadata.CloneURL), zap.Error(err))
				writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
				return
			}
		} else if existingIntegration != nil && existingIntegration.WebhookID > 0 {
			if existingRef, refErr := parseGitHubRepositoryURL(existingIntegration.RepositoryURL); refErr == nil {
				if err := deleteGitHubWebhook(r.Context(), token, existingRef, existingIntegration.WebhookID); err != nil {
					a.app.Logger.Error("delete github webhook failed", zap.String("hostname", hostname), zap.Error(err))
					writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
					return
				}
			}
			webhookID = 0
			webhookSecret = ""
		}

		integration := domain.GitHubIntegration{
			RepositoryURL:    strings.TrimSpace(metadata.CloneURL),
			AutoDeployOnPush: input.AutoDeployOnPush,
			DefaultBranch:    strings.TrimSpace(metadata.DefaultBranch),
			PostFetchScript:  postFetchScript,
			WebhookSecret:    webhookSecret,
			WebhookID:        webhookID,
			CreatedAt:        createdAt,
			UpdatedAt:        now,
		}

		updatedRecord, err := a.app.Domains.UpsertGitHubIntegration(r.Context(), hostname, integration)
		if err != nil {
			a.app.Logger.Error("save github integration failed", zap.String("hostname", hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "github_update", "domain", record.ID, record.Hostname, "failed", "Failed to save the GitHub integration.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to save github integration"})
			return
		}

		a.mutationEvent(r.Context(), "domains", "github_update", "domain", updatedRecord.ID, updatedRecord.Hostname, "succeeded", fmt.Sprintf("Updated the GitHub integration for %q.", updatedRecord.Hostname))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"domain": updatedRecord})
	})

	domainsGitHubDeployHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		record, ok := a.app.Domains.FindByHostname(hostname)
		if !ok {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			return
		}
		if err := ensureGitHubIntegrationSupported(record); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if record.GitHub == nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": errGitHubIntegrationNotConfigured.Error()})
			return
		}

		token, err := getGitHubToken(r.Context(), a.app.Settings)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		result, err := runDomainGitHubDeploy(r.Context(), a.app.Domains.BasePath(), record, *record.GitHub, token)
		if err != nil {
			a.app.Logger.Error("github deploy failed", zap.String("hostname", hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "github_deploy", "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Failed to deploy %q from GitHub.", record.Hostname))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		a.mutationEvent(r.Context(), "domains", "github_deploy", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Deployed %q from GitHub.", record.Hostname))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true, "action": result.Action})
	})

	domainsGitHubWebhookHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")
		record, ok := a.app.Domains.FindByHostname(hostname)
		if !ok || record.GitHub == nil || !record.GitHub.AutoDeployOnPush || strings.TrimSpace(record.GitHub.WebhookSecret) == "" {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "github webhook not configured"})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "failed to read webhook payload"})
			return
		}

		if !verifyGitHubWebhookSignature(record.GitHub.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
			writeJSON(w, stdhttp.StatusUnauthorized, map[string]any{"error": errGitHubInvalidWebhookSignature.Error()})
			return
		}

		eventName := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
		switch eventName {
		case "ping":
			writeJSON(w, stdhttp.StatusAccepted, map[string]any{"ok": true})
			return
		case "push":
		default:
			writeJSON(w, stdhttp.StatusAccepted, map[string]any{"ok": true})
			return
		}

		var payload gitHubWebhookPushPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid webhook payload"})
			return
		}

		if payload.Repository.CloneURL != "" && !sameGitHubRepository(payload.Repository.CloneURL, record.GitHub.RepositoryURL) {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "webhook repository does not match this domain integration"})
			return
		}

		defaultBranch := strings.TrimSpace(record.GitHub.DefaultBranch)
		if defaultBranch == "" {
			defaultBranch = strings.TrimSpace(payload.Repository.DefaultBranch)
		}
		if defaultBranch != "" && payload.Ref != "refs/heads/"+defaultBranch {
			writeJSON(w, stdhttp.StatusAccepted, map[string]any{"ok": true})
			return
		}

		token, err := getGitHubToken(r.Context(), a.app.Settings)
		if err != nil {
			a.app.Logger.Error("github webhook deploy blocked by missing token", zap.String("hostname", hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "github_webhook_deploy", "domain", record.ID, record.Hostname, "failed", "GitHub webhook was received but no GitHub token is configured.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		result, err := runDomainGitHubDeploy(r.Context(), a.app.Domains.BasePath(), record, *record.GitHub, token)
		if err != nil {
			a.app.Logger.Error("github webhook deploy failed", zap.String("hostname", hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "github_webhook_deploy", "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Push webhook deployment failed for %q.", record.Hostname))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		a.mutationEvent(r.Context(), "domains", "github_webhook_deploy", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Push webhook deployed %q.", record.Hostname))
		writeJSON(w, stdhttp.StatusAccepted, map[string]any{"ok": true, "action": result.Action})
	})

	domainsPHPSettingsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := chi.URLParam(r, "hostname")

		var input domain.UpdatePHPInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		record, err := a.app.Domains.UpdatePHPSettings(r.Context(), hostname, input)
		if err != nil {
			var validation domain.ValidationErrors
			switch {
			case errors.As(err, &validation):
				writeValidationFailed(w, map[string]string(validation))
			case errors.Is(err, domain.ErrNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			default:
				a.app.Logger.Error("update domain php settings failed", zap.String("hostname", hostname), zap.Error(err))
				a.mutationEvent(r.Context(), "domains", "update_php_settings", "domain", hostname, hostname, "failed", "Failed to update domain PHP settings.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update domain php settings"})
			}
			return
		}

		if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
			a.app.Logger.Error("sync domains after php settings update failed", zap.String("hostname", hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "update_php_settings", "domain", record.ID, record.Hostname, "failed", eventErrorMessage("Saved domain PHP settings but failed to republish routes.", err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "domain php settings saved but routes could not be refreshed"})
			return
		}

		a.mutationEvent(r.Context(), "domains", "update_php_settings", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Updated PHP settings for %q.", record.Hostname))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"domain": record})
	})

	domainsCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input domain.CreateInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if input.Kind == domain.KindPHP {
			if a.app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
				return
			}

			phpStatus := a.app.PHP.Status(r.Context())
			if !phpStatus.Ready {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "php runtime is not ready",
					"field_errors": map[string]string{
						"kind": phpStatus.Message,
					},
				})
				return
			}
		}

		record, err := a.app.Domains.Create(r.Context(), input)
		if err != nil {
			var validation domain.ValidationErrors
			switch {
			case errors.As(err, &validation):
				writeValidationFailed(w, map[string]string(validation))
			case errors.Is(err, domain.ErrDuplicateHostname):
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": "domain already exists",
					"field_errors": map[string]string{
						"hostname": "This domain already exists.",
					},
				})
			default:
				a.app.Logger.Error("create domain failed", zap.Error(err))
				a.mutationEvent(r.Context(), "domains", "create", "domain", strings.TrimSpace(input.Hostname), strings.TrimSpace(input.Hostname), "failed", "Failed to create domain.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to create domain"})
			}
			return
		}

		if a.app.FTPAccounts != nil {
			if err := a.app.FTPAccounts.ReconcileDomain(r.Context(), record); err != nil {
				_, removed, rollbackErr := a.app.Domains.Delete(r.Context(), record.ID)
				if rollbackErr != nil {
					a.app.Logger.Error("rollback created domain after ftp setup failed", zap.String("domain_id", record.ID), zap.Error(rollbackErr))
				} else if !removed {
					a.app.Logger.Error("rollback created domain after ftp setup missing", zap.String("domain_id", record.ID))
				}
				a.app.Logger.Error("create default ftp account failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(err))
				a.mutationEvent(r.Context(), "domains", "create", "domain", record.ID, record.Hostname, "failed", "Created domain record but failed to provision its FTP account.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to provision ftp account"})
				return
			}
		}

		if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
			_, removed, rollbackErr := a.app.Domains.Delete(r.Context(), record.ID)
			if rollbackErr != nil {
				a.app.Logger.Error("rollback created domain failed", zap.String("domain_id", record.ID), zap.Error(rollbackErr))
			} else if !removed {
				a.app.Logger.Error("rollback created domain missing", zap.String("domain_id", record.ID))
			}
			if a.app.FTPAccounts != nil {
				if cleanupErr := a.app.FTPAccounts.DeleteDomain(r.Context(), record.ID); cleanupErr != nil {
					a.app.Logger.Warn("cleanup ftp account after domain publish failure failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(cleanupErr))
				}
			}
			a.app.Logger.Error("publish domain failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "create", "domain", record.ID, record.Hostname, "failed", eventErrorMessage("Created domain record but failed to publish it.", err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to publish domain"})
			return
		}

		a.mutationEvent(r.Context(), "domains", "create", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Created domain %q.", record.Hostname))
		writeJSON(w, stdhttp.StatusCreated, map[string]any{"domain": record})
	})

	domainsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var input domain.UpdateInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if input.Kind == domain.KindPHP {
			if a.app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
				return
			}

			phpStatus := a.app.PHP.Status(r.Context())
			if !phpStatus.Ready {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "php runtime is not ready",
					"field_errors": map[string]string{
						"kind": phpStatus.Message,
					},
				})
				return
			}
		}

		domainID := chi.URLParam(r, "domainID")
		record, previous, err := a.app.Domains.Update(r.Context(), domainID, input)
		if err != nil {
			var validation domain.ValidationErrors
			switch {
			case errors.As(err, &validation):
				writeValidationFailed(w, map[string]string(validation))
			case errors.Is(err, domain.ErrDuplicateHostname):
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": "domain already exists",
					"field_errors": map[string]string{
						"hostname": "This domain already exists.",
					},
				})
			case errors.Is(err, domain.ErrNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			default:
				a.app.Logger.Error("update domain failed", zap.String("domain_id", domainID), zap.Error(err))
				a.mutationEvent(r.Context(), "domains", "update", "domain", domainID, domainID, "failed", "Failed to update domain.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update domain"})
			}
			return
		}

		if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
			if rollbackErr := a.app.Domains.Restore(r.Context(), previous); rollbackErr != nil {
				a.app.Logger.Error("rollback updated domain failed", zap.String("domain_id", previous.ID), zap.Error(rollbackErr))
			}
			a.app.Logger.Error("publish updated domain failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "update", "domain", record.ID, record.Hostname, "failed", eventErrorMessage("Updated domain record but failed to publish it.", err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update domain"})
			return
		}

		if a.app.FTPAccounts != nil {
			if err := a.app.FTPAccounts.ReconcileDomain(r.Context(), record); err != nil {
				a.app.Logger.Error("reconcile ftp account after domain update failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "domain updated but ftp account could not be reconciled"})
				return
			}
		}

		a.mutationEvent(r.Context(), "domains", "update", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Updated domain %q.", record.Hostname))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"domain": record})
	})

	domainsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		domainID := chi.URLParam(r, "domainID")
		deleteDatabase := queryEnabled(r, "delete_database")
		deleteDocumentRoot := queryEnabled(r, "delete_document_root")
		record, removed, err := a.app.Domains.Delete(r.Context(), domainID)
		if err != nil {
			a.app.Logger.Error("delete domain failed", zap.String("domain_id", domainID), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "delete", "domain", domainID, domainID, "failed", "Failed to delete domain.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to delete domain"})
			return
		}
		if !removed {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			return
		}

		if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
			if rollbackErr := a.app.Domains.Restore(r.Context(), record); rollbackErr != nil {
				a.app.Logger.Error("rollback deleted domain failed", zap.String("domain_id", record.ID), zap.Error(rollbackErr))
			}
			a.app.Logger.Error("publish deleted domain failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "delete", "domain", record.ID, record.Hostname, "failed", eventErrorMessage("Deleted domain record but failed to republish routes.", err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to delete domain"})
			return
		}

		warnings := make([]string, 0, 2)
		if deleteDatabase {
			cleanupWarnings, cleanupErr := deleteLinkedDomainDatabases(r.Context(), a.app.MariaDB, record.Hostname)
			warnings = append(warnings, cleanupWarnings...)
			if cleanupErr != nil {
				a.app.Logger.Warn("delete linked databases failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(cleanupErr))
			}
		}
		if deleteDocumentRoot {
			if warning, cleanupErr := deleteDomainDocumentRoot(record, a.app.Domains.BasePath()); cleanupErr != nil {
				warnings = append(warnings, warning)
				a.app.Logger.Warn("delete domain document root failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(cleanupErr))
			}
		}

		if a.app.FTPAccounts != nil {
			if cleanupErr := a.app.FTPAccounts.DeleteDomain(r.Context(), record.ID); cleanupErr != nil {
				warnings = append(warnings, "The FTP account could not be removed.")
				a.app.Logger.Warn("delete domain ftp account failed", zap.String("domain_id", record.ID), zap.String("hostname", record.Hostname), zap.Error(cleanupErr))
			}
		}

		message := fmt.Sprintf("Deleted domain %q.", record.Hostname)
		if len(warnings) > 0 {
			message = fmt.Sprintf(`Deleted domain %q with cleanup warnings.`, record.Hostname)
		}
		a.mutationEvent(r.Context(), "domains", "delete", "domain", record.ID, record.Hostname, "succeeded", message)
		writeJSON(w, stdhttp.StatusOK, map[string]any{"domain": record, "warnings": warnings})
	})

	domainFTPGetHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.FTPAccounts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "ftp accounts are not configured"})
			return
		}

		domainID := chi.URLParam(r, "domainID")
		status, err := a.app.FTPAccounts.GetDomainStatus(r.Context(), domainID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
				return
			}

			a.app.Logger.Error("load domain ftp status failed", zap.String("domain_id", domainID), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load ftp account"})
			return
		}

		if err := writeDomainFTPResponse(w, stdhttp.StatusOK, a.app, r, status); err != nil {
			a.app.Logger.Error("load ftp connection settings failed", zap.String("domain_id", domainID), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load ftp connection settings"})
			return
		}
	})

	domainFTPUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.FTPAccounts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "ftp accounts are not configured"})
			return
		}

		var input ftp.UpdateInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		domainID := chi.URLParam(r, "domainID")
		status, err := a.app.FTPAccounts.UpdateDomain(r.Context(), domainID, input)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
				return
			}

			var validation ftp.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}

			a.app.Logger.Error("update domain ftp account failed", zap.String("domain_id", domainID), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "update_ftp", "domain", domainID, domainID, "failed", "Failed to update the domain FTP account.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update ftp account"})
			return
		}

		a.mutationEvent(r.Context(), "domains", "update_ftp", "domain", domainID, status.Username, "succeeded", "Updated the domain FTP account.")
		if err := writeDomainFTPResponse(w, stdhttp.StatusOK, a.app, r, status); err != nil {
			a.app.Logger.Error("load ftp connection settings after update failed", zap.String("domain_id", domainID), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "ftp account updated but connection settings could not be loaded"})
		}
	})

	domainFTPResetPasswordHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.FTPAccounts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "ftp accounts are not configured"})
			return
		}

		domainID := chi.URLParam(r, "domainID")
		status, password, err := a.app.FTPAccounts.ResetPassword(r.Context(), domainID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
				return
			}

			var validation ftp.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}

			a.app.Logger.Error("reset domain ftp password failed", zap.String("domain_id", domainID), zap.Error(err))
			a.mutationEvent(r.Context(), "domains", "reset_ftp_password", "domain", domainID, domainID, "failed", "Failed to reset the domain FTP password.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to reset ftp password"})
			return
		}

		a.mutationEvent(r.Context(), "domains", "reset_ftp_password", "domain", domainID, status.Username, "succeeded", "Reset the domain FTP password.")
		payload, err := domainFTPResponsePayload(r, a.app, status)
		if err != nil {
			a.app.Logger.Error("load ftp connection settings after password reset failed", zap.String("domain_id", domainID), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "ftp password reset but connection settings could not be loaded"})
			return
		}
		payload["password"] = password
		writeJSON(w, stdhttp.StatusOK, payload)
	})

	r.Method(stdhttp.MethodGet, "/domains", domainsListHandler)
	r.Method(stdhttp.MethodHead, "/domains", domainsListHandler)
	r.Method(stdhttp.MethodGet, "/domains/logs", domainsLogsHandler)
	r.Method(stdhttp.MethodHead, "/domains/logs", domainsLogsHandler)
	r.Method(stdhttp.MethodGet, "/domains/{hostname}/preview", domainsPreviewHandler)
	r.Method(stdhttp.MethodHead, "/domains/{hostname}/preview", domainsPreviewHandler)
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/copy", domainsWebsiteCopyHandler)
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/templates/install", domainsTemplateInstallHandler)
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/composer/install", domainsComposerActionHandler("install"))
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/composer/update", domainsComposerActionHandler("update"))
	r.Method(stdhttp.MethodGet, "/domains/{hostname}/wordpress/summary", domainsWordPressSummaryHandler)
	r.Method(stdhttp.MethodHead, "/domains/{hostname}/wordpress/summary", domainsWordPressSummaryHandler)
	r.Method(stdhttp.MethodGet, "/domains/{hostname}/wordpress", domainsWordPressStatusHandler)
	r.Method(stdhttp.MethodHead, "/domains/{hostname}/wordpress", domainsWordPressStatusHandler)
	r.Method(stdhttp.MethodGet, "/domains/{hostname}/wordpress/plugins/search", domainsWordPressExtensionSearchHandler("plugin"))
	r.Method(stdhttp.MethodGet, "/domains/{hostname}/wordpress/themes/search", domainsWordPressExtensionSearchHandler("theme"))
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/wordpress/plugins/install", domainsWordPressExtensionInstallHandler("plugin"))
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/wordpress/themes/install", domainsWordPressExtensionInstallHandler("theme"))
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/wordpress/plugins/action", domainsWordPressExtensionActionHandler("plugin"))
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/wordpress/themes/action", domainsWordPressExtensionActionHandler("theme"))
	r.Method(stdhttp.MethodPut, "/domains/{hostname}/php-settings", domainsPHPSettingsUpdateHandler)
	r.Method(stdhttp.MethodPut, "/domains/{hostname}/github", domainsGitHubUpdateHandler)
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/github/deploy", domainsGitHubDeployHandler)
	r.Method(stdhttp.MethodPost, "/domains/{hostname}/github/webhook", domainsGitHubWebhookHandler)
	r.Method(stdhttp.MethodPost, "/domains", domainsCreateHandler)
	r.Method(stdhttp.MethodPut, "/domains/{domainID}", domainsUpdateHandler)
	r.Method(stdhttp.MethodDelete, "/domains/{domainID}", domainsDeleteHandler)
	r.Method(stdhttp.MethodGet, "/domains/{domainID}/ftp", domainFTPGetHandler)
	r.Method(stdhttp.MethodPut, "/domains/{domainID}/ftp", domainFTPUpdateHandler)
	r.Method(stdhttp.MethodPost, "/domains/{domainID}/ftp/reset-password", domainFTPResetPasswordHandler)
	r.Method(stdhttp.MethodGet, "/ftp/accounts", ftpAccountsListHandler)
	r.Method(stdhttp.MethodHead, "/ftp/accounts", ftpAccountsListHandler)
	r.Method(stdhttp.MethodPost, "/ftp/accounts", ftpAccountsCreateHandler)
	r.Method(stdhttp.MethodPut, "/ftp/accounts/{accountID}", ftpAccountsUpdateHandler)
	r.Method(stdhttp.MethodDelete, "/ftp/accounts/{accountID}", ftpAccountsDeleteHandler)
}

type domainLogResponse struct {
	Hostname     string     `json:"hostname"`
	Type         string     `json:"type"`
	Path         string     `json:"path"`
	Available    bool       `json:"available"`
	ModifiedAt   *time.Time `json:"modified_at,omitempty"`
	SizeBytes    int64      `json:"size_bytes"`
	TotalMatches int        `json:"total_matches"`
	Truncated    bool       `json:"truncated"`
	ReadError    string     `json:"read_error,omitempty"`
	Lines        []string   `json:"lines"`
}

func normalizeDomainLogHostname(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func readDomainLog(hostname string, logType string, filePath string, search string, limit int) domainLogResponse {
	response := domainLogResponse{
		Hostname: hostname,
		Type:     logType,
		Path:     filePath,
		Lines:    []string{},
	}

	if strings.TrimSpace(filePath) == "" {
		return response
	}

	info, err := os.Stat(filePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			response.ReadError = err.Error()
		}
		return response
	}

	response.Available = true
	response.SizeBytes = info.Size()
	modifiedAt := info.ModTime().UTC()
	response.ModifiedAt = &modifiedAt

	lines, totalMatches, truncated, err := tailMatchingLogLines(filePath, search, limit)
	if err != nil {
		response.ReadError = err.Error()
		return response
	}

	response.TotalMatches = totalMatches
	response.Truncated = truncated
	response.Lines = lines
	return response
}

func tailMatchingLogLines(filePath string, search string, limit int) ([]string, int, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, false, err
	}
	defer file.Close()

	search = strings.ToLower(strings.TrimSpace(search))
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lines := make([]string, 0, limit)
	totalMatches := 0
	for scanner.Scan() {
		line := scanner.Text()
		if search != "" && !strings.Contains(strings.ToLower(line), search) {
			continue
		}

		totalMatches++
		if len(lines) < limit {
			lines = append(lines, line)
			continue
		}

		copy(lines, lines[1:])
		lines[len(lines)-1] = line
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, false, err
	}

	return lines, totalMatches, totalMatches > limit, nil
}

func deleteLinkedDomainDatabases(ctx context.Context, manager mariadb.Manager, hostname string) ([]string, error) {
	if manager == nil {
		err := errors.New("mariadb runtime is not configured")
		return []string{"MariaDB runtime is not configured, so linked databases were not deleted."}, err
	}

	databases, err := manager.ListDatabases(ctx)
	if err != nil {
		return []string{"Failed to load linked databases for deletion."}, err
	}

	var warnings []string
	for _, database := range databases {
		if strings.TrimSpace(database.Domain) != hostname {
			continue
		}

		if err := manager.DeleteDatabase(ctx, database.Name, mariadb.DeleteDatabaseInput{Username: database.Username}); err != nil {
			warnings = append(warnings, fmt.Sprintf(`Failed to delete linked database %q.`, database.Name))
		}
	}

	if len(warnings) > 0 {
		return warnings, errors.New(strings.Join(warnings, " "))
	}

	return nil, nil
}

var (
	errDomainCopyConflict      = errors.New("target directory already contains conflicting files")
	errDomainCopyInvalidTarget = errors.New("source and destination domains share the same document root")
)

func isSiteBackedDomainRecord(record domain.Record) bool {
	return domain.SupportsManagedDocumentRoot(record.Kind)
}

func resolveDomainDocumentRoot(record domain.Record, basePath string) (string, error) {
	return domain.ResolveDocumentRoot(basePath, record)
}

func copyDomainDocumentRoot(source, target domain.Record, basePath string, replaceTargetFiles bool) error {
	sourcePath, err := resolveDomainDocumentRoot(source, basePath)
	if err != nil {
		return err
	}

	targetPath, err := resolveDomainDocumentRoot(target, basePath)
	if err != nil {
		return err
	}

	if sourcePath == targetPath {
		return errDomainCopyInvalidTarget
	}

	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return fmt.Errorf("ensure target document root: %w", err)
	}
	if err := os.MkdirAll(sourcePath, 0o755); err != nil {
		return fmt.Errorf("ensure source document root: %w", err)
	}
	if replaceTargetFiles {
		if err := clearDocumentRootContents(targetPath); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return fmt.Errorf("read source document root: %w", err)
	}

	for _, entry := range entries {
		sourceEntryPath := filepath.Join(sourcePath, entry.Name())
		targetEntryPath := filepath.Join(targetPath, entry.Name())
		if err := filesvc.CopyPath(sourceEntryPath, targetEntryPath); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return errDomainCopyConflict
			}
			return fmt.Errorf("copy document root entry %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func clearDocumentRootContents(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(path, 0o755)
		}
		return fmt.Errorf("read target document root: %w", err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			return fmt.Errorf("clear target document root entry %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func deleteDomainDocumentRoot(record domain.Record, basePath string) (string, error) {
	if !isSiteBackedDomainRecord(record) {
		return "", nil
	}

	targetPath, err := resolveDomainDocumentRoot(record, basePath)
	if err != nil {
		return "The domain document root could not be deleted.", err
	}

	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Sprintf(`Failed to delete the document root for %q.`, record.Hostname), err
	}

	return "", nil
}
