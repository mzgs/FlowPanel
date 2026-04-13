package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	nethttp "net/http"
	"net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/config"
	"flowpanel/internal/domain"
	"flowpanel/internal/mariadb"

	"golang.org/x/net/publicsuffix"
)

const wordPressActionTimeout = 10 * time.Minute
const wordPressCLIPharURL = "https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar"
const wordPressDatabasePasswordBytes = 24
const wordPressDatabaseUsernameBytes = 6

var (
	errWordPressUnsupportedDomain     = errors.New("WordPress toolkit is available only for PHP site domains")
	errWordPressCLIUnavailable        = errors.New("wp-cli is not installed on this server")
	errWordPressDatabaseUnavailable   = errors.New("MariaDB is not configured on this server")
	errWordPressNotInstalled          = errors.New("WordPress is not installed for this domain")
	errWordPressAlreadyInstalled      = errors.New("WordPress is already installed for this domain")
	errWordPressInstallDirectoryDirty = errors.New("document root is not empty, so WordPress installation was refused")

	wordPressIdentifierPattern  = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	wordPressTablePrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	wordPressUserPattern        = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	wordPressHTMLTagPattern     = regexp.MustCompile(`<[^>]+>`)
	wordPressCLIMu              sync.Mutex
)

type wordPressStatus struct {
	CLIAvailable     bool                 `json:"cli_available"`
	CLIPath          string               `json:"cli_path,omitempty"`
	DocumentRoot     string               `json:"document_root"`
	SuggestedDBName  string               `json:"suggested_database_name,omitempty"`
	ConfigPresent    bool                 `json:"config_present"`
	CoreFilesPresent bool                 `json:"core_files_present"`
	Installed        bool                 `json:"installed"`
	InspectError     string               `json:"inspect_error,omitempty"`
	Version          string               `json:"version,omitempty"`
	SiteURL          string               `json:"site_url,omitempty"`
	SiteTitle        string               `json:"site_title,omitempty"`
	CoreUpdate       *wordPressCoreUpdate `json:"core_update,omitempty"`
	Plugins          []wordPressExtension `json:"plugins"`
	Themes           []wordPressExtension `json:"themes"`
	Databases        []wordPressDatabase  `json:"databases"`
}

type wordPressSummary struct {
	CLIAvailable bool   `json:"cli_available"`
	CLIPath      string `json:"cli_path,omitempty"`
	Installed    bool   `json:"installed"`
	InspectError string `json:"inspect_error,omitempty"`
	Version      string `json:"version,omitempty"`
}

type wordPressCoreUpdate struct {
	Version    string `json:"version,omitempty"`
	UpdateType string `json:"update_type,omitempty"`
	PackageURL string `json:"package_url,omitempty"`
}

type wordPressExtension struct {
	Name          string `json:"name"`
	Title         string `json:"title,omitempty"`
	Status        string `json:"status,omitempty"`
	Version       string `json:"version,omitempty"`
	Update        string `json:"update,omitempty"`
	UpdateVersion string `json:"update_version,omitempty"`
	AutoUpdate    string `json:"auto_update,omitempty"`
}

type wordPressDatabase struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Host     string `json:"host"`
}

type wordPressDatabaseConfig struct {
	Name     string
	Username string
	Host     string
}

type wordPressInstallInput struct {
	DatabaseName      string `json:"database_name"`
	SiteURL           string `json:"site_url"`
	SiteTitle         string `json:"site_title"`
	AdminUsername     string `json:"admin_username"`
	AdminEmail        string `json:"admin_email"`
	AdminPassword     string `json:"admin_password"`
	TablePrefix       string `json:"table_prefix"`
	ClearDocumentRoot bool   `json:"clear_document_root"`
}

type wordPressExtensionActionInput struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type wordPressExtensionInstallInput struct {
	Slug string `json:"slug"`
}

type wordPressExtensionSearchResult struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Version      string `json:"version,omitempty"`
	Author       string `json:"author,omitempty"`
	LastUpdated  string `json:"last_updated,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

type wordPressStatusSection string

const (
	wordPressStatusSectionAll      wordPressStatusSection = "all"
	wordPressStatusSectionPlugins  wordPressStatusSection = "plugins"
	wordPressStatusSectionThemes   wordPressStatusSection = "themes"
	wordPressStatusSectionDatabase wordPressStatusSection = "database"
	wordPressSearchResultsPerPage                         = "20"
	wordPressPluginSearchURL                              = "https://api.wordpress.org/plugins/info/1.2/"
	wordPressThemeSearchURL                               = "https://api.wordpress.org/themes/info/1.2/"
)

func parseWordPressStatusSection(value string) (wordPressStatusSection, error) {
	switch strings.TrimSpace(value) {
	case "", string(wordPressStatusSectionAll):
		return wordPressStatusSectionAll, nil
	case string(wordPressStatusSectionPlugins):
		return wordPressStatusSectionPlugins, nil
	case string(wordPressStatusSectionThemes):
		return wordPressStatusSectionThemes, nil
	case string(wordPressStatusSectionDatabase):
		return wordPressStatusSectionDatabase, nil
	default:
		return "", fmt.Errorf("invalid wordpress section %q", value)
	}
}

func loadWordPressStatus(
	ctx context.Context,
	domains *domain.Service,
	mariadbManager mariadb.Manager,
	hostname string,
) (wordPressStatus, domain.Record, error) {
	return loadWordPressStatusSection(ctx, domains, mariadbManager, hostname, wordPressStatusSectionAll)
}

func loadWordPressStatusSection(
	ctx context.Context,
	domains *domain.Service,
	mariadbManager mariadb.Manager,
	hostname string,
	section wordPressStatusSection,
) (wordPressStatus, domain.Record, error) {
	record, targetPath, err := resolveWordPressDomain(domains, hostname)
	if err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}

	status := wordPressStatus{
		DocumentRoot:    targetPath,
		SuggestedDBName: suggestedWordPressDatabaseName(hostname),
		Plugins:         []wordPressExtension{},
		Themes:          []wordPressExtension{},
		Databases:       []wordPressDatabase{},
	}
	status.ConfigPresent = fileExists(filepath.Join(targetPath, "wp-config.php"))
	status.CoreFilesPresent = wordPressCoreFilesPresent(targetPath)

	cli, err := resolveWordPressCLI(ctx)
	if err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}

	status.CLIAvailable = true
	status.CLIPath = cli.path

	installed, inspectError := inspectWordPressInstallation(ctx, targetPath)
	status.Installed = installed
	status.InspectError = inspectError
	if !installed || inspectError != "" {
		return status, record, nil
	}

	if section == wordPressStatusSectionAll || section == wordPressStatusSectionDatabase {
		status.Databases = listWordPressDatabases(ctx, mariadbManager, targetPath)
	}

	if section == wordPressStatusSectionAll {
		if version, err := runWordPressValueCommand(ctx, targetPath, "core", "version"); err == nil {
			status.Version = version
		} else if status.InspectError == "" {
			status.InspectError = err.Error()
		}

		if siteURL, err := runWordPressValueCommand(ctx, targetPath, "option", "get", "siteurl"); err == nil {
			status.SiteURL = siteURL
		} else if status.InspectError == "" {
			status.InspectError = err.Error()
		}

		if siteTitle, err := runWordPressValueCommand(ctx, targetPath, "option", "get", "blogname"); err == nil {
			status.SiteTitle = siteTitle
		} else if status.InspectError == "" {
			status.InspectError = err.Error()
		}

		if update, err := loadWordPressCoreUpdate(ctx, targetPath); err == nil {
			status.CoreUpdate = update
		} else if status.InspectError == "" {
			status.InspectError = err.Error()
		}
	}

	if section == wordPressStatusSectionAll || section == wordPressStatusSectionPlugins {
		if plugins, err := loadWordPressExtensions(ctx, targetPath, "plugin"); err == nil {
			status.Plugins = plugins
		} else if status.InspectError == "" {
			status.InspectError = err.Error()
		}
	}

	if section == wordPressStatusSectionAll || section == wordPressStatusSectionThemes {
		if themes, err := loadWordPressExtensions(ctx, targetPath, "theme"); err == nil {
			status.Themes = themes
		} else if status.InspectError == "" {
			status.InspectError = err.Error()
		}
	}

	return status, record, nil
}

func loadWordPressSummary(
	ctx context.Context,
	domains *domain.Service,
	hostname string,
) (wordPressSummary, domain.Record, error) {
	record, targetPath, err := resolveWordPressDomain(domains, hostname)
	if err != nil {
		return wordPressSummary{}, domain.Record{}, err
	}

	cli, err := resolveWordPressCLI(ctx)
	if err != nil {
		return wordPressSummary{}, domain.Record{}, err
	}

	summary := wordPressSummary{
		CLIAvailable: true,
		CLIPath:      cli.path,
	}

	installed, inspectError := inspectWordPressInstallation(ctx, targetPath)
	summary.Installed = installed
	summary.InspectError = inspectError
	if !installed || inspectError != "" {
		return summary, record, nil
	}

	if version, err := runWordPressValueCommand(ctx, targetPath, "core", "version"); err == nil {
		summary.Version = version
	} else if summary.InspectError == "" {
		summary.InspectError = err.Error()
	}

	return summary, record, nil
}

func installWordPress(
	ctx context.Context,
	domains *domain.Service,
	mariadbManager mariadb.Manager,
	hostname string,
	input wordPressInstallInput,
) (wordPressStatus, domain.Record, error) {
	record, targetPath, err := resolveWordPressDomain(domains, hostname)
	if err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}
	if _, err := resolveWordPressCLI(ctx); err != nil {
		return wordPressStatus{}, record, err
	}

	configPresent := fileExists(filepath.Join(targetPath, "wp-config.php"))
	validation := validateWordPressInstallInput(input, configPresent)
	if len(validation) > 0 {
		return wordPressStatus{}, record, validation
	}

	installed, inspectError := inspectWordPressInstallation(ctx, targetPath)
	if installed {
		return wordPressStatus{}, record, errWordPressAlreadyInstalled
	}
	if inspectError != "" {
		return wordPressStatus{}, record, errors.New(inspectError)
	}

	if !wordPressCoreFilesPresent(targetPath) {
		empty, err := directoryIsEmpty(targetPath)
		if err != nil {
			return wordPressStatus{}, record, fmt.Errorf("inspect document root: %w", err)
		}
		if !empty {
			if !input.ClearDocumentRoot {
				return wordPressStatus{}, record, errWordPressInstallDirectoryDirty
			}
			if err := clearDocumentRootContents(targetPath); err != nil {
				return wordPressStatus{}, record, fmt.Errorf("clear document root: %w", err)
			}
		}
		if _, _, err := runWordPressCommand(ctx, targetPath, "core", "download"); err != nil {
			return wordPressStatus{}, record, err
		}
	}

	var database mariadb.DatabaseRecord
	if !configPresent {
		database, err = createWordPressDatabase(ctx, mariadbManager, hostname, strings.TrimSpace(input.DatabaseName))
		if err != nil {
			return wordPressStatus{}, record, err
		}
	}

	if !configPresent {
		dbHost := strings.TrimSpace(database.Host)
		if dbHost == "" {
			dbHost = "localhost"
		}
		args := []string{
			"config",
			"create",
			"--skip-check",
			"--dbname=" + database.Name,
			"--dbuser=" + database.Username,
			"--dbhost=" + dbHost,
			"--dbprefix=" + normalizeWordPressTablePrefix(input.TablePrefix),
		}
		if database.Password != "" {
			args = append(args, "--dbpass="+database.Password)
		}
		if _, _, err := runWordPressCommand(ctx, targetPath, args...); err != nil {
			return wordPressStatus{}, record, err
		}
	}

	if _, _, err := runWordPressCommand(
		ctx,
		targetPath,
		"core",
		"install",
		"--url="+strings.TrimSpace(input.SiteURL),
		"--title="+strings.TrimSpace(input.SiteTitle),
		"--admin_user="+strings.TrimSpace(input.AdminUsername),
		"--admin_email="+strings.TrimSpace(input.AdminEmail),
		"--admin_password="+input.AdminPassword,
	); err != nil {
		return wordPressStatus{}, record, err
	}

	return loadWordPressStatus(ctx, domains, mariadbManager, hostname)
}

func runWordPressExtensionAction(
	ctx context.Context,
	domains *domain.Service,
	mariadbManager mariadb.Manager,
	hostname string,
	resource string,
	input wordPressExtensionActionInput,
) (wordPressStatus, domain.Record, error) {
	_, targetPath, err := resolveWordPressDomain(domains, hostname)
	if err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}
	if _, err := resolveWordPressCLI(ctx); err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}
	if err := ensureWordPressInstalled(ctx, targetPath); err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}

	validation := domain.ValidationErrors{}
	name := strings.TrimSpace(input.Name)
	action := strings.TrimSpace(input.Action)
	if name == "" {
		validation["name"] = fmt.Sprintf("Select a %s.", resource)
	} else if !wordPressIdentifierPattern.MatchString(name) {
		validation["name"] = fmt.Sprintf("Select a valid %s.", resource)
	}
	switch action {
	case "activate", "deactivate", "delete", "update":
	default:
		validation["action"] = "Select a valid action."
	}
	if len(validation) > 0 {
		return wordPressStatus{}, domain.Record{}, validation
	}

	if _, _, err := runWordPressCommand(ctx, targetPath, resource, action, name); err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}

	section := wordPressStatusSectionAll
	switch resource {
	case "plugin":
		section = wordPressStatusSectionPlugins
	case "theme":
		section = wordPressStatusSectionThemes
	}

	return loadWordPressStatusSection(ctx, domains, mariadbManager, hostname, section)
}

func searchWordPressExtensions(
	ctx context.Context,
	domains *domain.Service,
	hostname string,
	resource string,
	query string,
) ([]wordPressExtensionSearchResult, domain.Record, error) {
	record, targetPath, err := resolveWordPressDomain(domains, hostname)
	if err != nil {
		return nil, domain.Record{}, err
	}
	if _, err := resolveWordPressCLI(ctx); err != nil {
		return nil, record, err
	}
	if err := ensureWordPressInstalled(ctx, targetPath); err != nil {
		return nil, record, err
	}

	normalizedQuery := strings.TrimSpace(query)
	validation := domain.ValidationErrors{}
	if normalizedQuery == "" {
		validation["q"] = "Enter a search term."
	} else if len([]rune(normalizedQuery)) < 2 {
		validation["q"] = "Enter at least 2 characters."
	}
	if len(validation) > 0 {
		return nil, record, validation
	}

	var results []wordPressExtensionSearchResult
	switch resource {
	case "plugin":
		results, err = loadWordPressPluginSearchResults(ctx, normalizedQuery)
	case "theme":
		results, err = loadWordPressThemeSearchResults(ctx, normalizedQuery)
	default:
		err = fmt.Errorf("unsupported WordPress resource %q", resource)
	}
	if err != nil {
		return nil, record, err
	}

	return results, record, nil
}

func installWordPressExtension(
	ctx context.Context,
	domains *domain.Service,
	mariadbManager mariadb.Manager,
	hostname string,
	resource string,
	input wordPressExtensionInstallInput,
) (wordPressStatus, domain.Record, error) {
	record, targetPath, err := resolveWordPressDomain(domains, hostname)
	if err != nil {
		return wordPressStatus{}, domain.Record{}, err
	}
	if _, err := resolveWordPressCLI(ctx); err != nil {
		return wordPressStatus{}, record, err
	}
	if err := ensureWordPressInstalled(ctx, targetPath); err != nil {
		return wordPressStatus{}, record, err
	}

	slug := strings.TrimSpace(input.Slug)
	validation := domain.ValidationErrors{}
	if slug == "" {
		validation["slug"] = fmt.Sprintf("Select a %s to install.", resource)
	} else if !wordPressIdentifierPattern.MatchString(slug) {
		validation["slug"] = fmt.Sprintf("Select a valid %s slug.", resource)
	}
	if len(validation) > 0 {
		return wordPressStatus{}, record, validation
	}

	if _, _, err := runWordPressCommand(ctx, targetPath, resource, "install", slug); err != nil {
		return wordPressStatus{}, record, err
	}

	section := wordPressStatusSectionThemes
	if resource == "plugin" {
		section = wordPressStatusSectionPlugins
	}

	return loadWordPressStatusSection(ctx, domains, mariadbManager, hostname, section)
}

type wordPressPluginSearchResponse struct {
	Plugins []struct {
		Name        string            `json:"name"`
		Slug        string            `json:"slug"`
		Version     string            `json:"version"`
		Author      string            `json:"author"`
		LastUpdated string            `json:"last_updated"`
		Icons       map[string]string `json:"icons"`
	} `json:"plugins"`
}

type wordPressThemeSearchResponse struct {
	Themes []struct {
		Name          string `json:"name"`
		Slug          string `json:"slug"`
		Version       string `json:"version"`
		LastUpdated   string `json:"last_updated"`
		ScreenshotURL string `json:"screenshot_url"`
		Author        struct {
			DisplayName string `json:"display_name"`
			Author      string `json:"author"`
		} `json:"author"`
	} `json:"themes"`
}

func loadWordPressPluginSearchResults(
	ctx context.Context,
	query string,
) ([]wordPressExtensionSearchResult, error) {
	searchURL, err := url.Parse(wordPressPluginSearchURL)
	if err != nil {
		return nil, fmt.Errorf("parse WordPress plugin search URL: %w", err)
	}

	params := searchURL.Query()
	params.Set("action", "query_plugins")
	params.Set("request[search]", query)
	params.Set("request[per_page]", wordPressSearchResultsPerPage)
	params.Set("request[fields][icons]", "1")
	searchURL.RawQuery = params.Encode()

	var response wordPressPluginSearchResponse
	if err := fetchWordPressSearchResponse(ctx, searchURL.String(), &response); err != nil {
		return nil, err
	}

	results := make([]wordPressExtensionSearchResult, 0, len(response.Plugins))
	for _, plugin := range response.Plugins {
		results = append(results, wordPressExtensionSearchResult{
			Name:         strings.TrimSpace(plugin.Name),
			Slug:         strings.TrimSpace(plugin.Slug),
			Version:      strings.TrimSpace(plugin.Version),
			Author:       normalizeWordPressAuthor(plugin.Author),
			LastUpdated:  strings.TrimSpace(plugin.LastUpdated),
			ThumbnailURL: pickWordPressPluginIcon(plugin.Icons),
		})
	}

	return results, nil
}

func loadWordPressThemeSearchResults(
	ctx context.Context,
	query string,
) ([]wordPressExtensionSearchResult, error) {
	searchURL, err := url.Parse(wordPressThemeSearchURL)
	if err != nil {
		return nil, fmt.Errorf("parse WordPress theme search URL: %w", err)
	}

	params := searchURL.Query()
	params.Set("action", "query_themes")
	params.Set("request[search]", query)
	params.Set("request[per_page]", wordPressSearchResultsPerPage)
	searchURL.RawQuery = params.Encode()

	var response wordPressThemeSearchResponse
	if err := fetchWordPressSearchResponse(ctx, searchURL.String(), &response); err != nil {
		return nil, err
	}

	results := make([]wordPressExtensionSearchResult, 0, len(response.Themes))
	for _, theme := range response.Themes {
		results = append(results, wordPressExtensionSearchResult{
			Name:         strings.TrimSpace(theme.Name),
			Slug:         strings.TrimSpace(theme.Slug),
			Version:      strings.TrimSpace(theme.Version),
			Author:       firstNonEmpty(strings.TrimSpace(theme.Author.DisplayName), strings.TrimSpace(theme.Author.Author)),
			LastUpdated:  strings.TrimSpace(theme.LastUpdated),
			ThumbnailURL: normalizeWordPressAssetURL(theme.ScreenshotURL),
		})
	}

	return results, nil
}

func fetchWordPressSearchResponse(ctx context.Context, requestURL string, target any) error {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, 30*time.Second)
		defer cancel()
	}

	request, err := nethttp.NewRequestWithContext(runCtx, nethttp.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("create WordPress search request: %w", err)
	}

	response, err := nethttp.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("request WordPress search results: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != nethttp.StatusOK {
		return fmt.Errorf("request WordPress search results failed with status %d", response.StatusCode)
	}

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode WordPress search results: %w", err)
	}

	return nil
}

func pickWordPressPluginIcon(icons map[string]string) string {
	for _, key := range []string{"2x", "1x", "svg", "default"} {
		if value := normalizeWordPressAssetURL(icons[key]); value != "" {
			return value
		}
	}

	return ""
}

func normalizeWordPressAuthor(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	return strings.TrimSpace(html.UnescapeString(wordPressHTMLTagPattern.ReplaceAllString(trimmed, "")))
}

func normalizeWordPressAssetURL(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return ""
	case strings.HasPrefix(trimmed, "//"):
		return "https:" + trimmed
	default:
		return trimmed
	}
}

func resolveWordPressDomain(domains *domain.Service, hostname string) (domain.Record, string, error) {
	record, ok := domains.FindByHostname(hostname)
	if !ok {
		return domain.Record{}, "", domain.ErrNotFound
	}
	if record.Kind != domain.KindPHP {
		return domain.Record{}, "", errWordPressUnsupportedDomain
	}

	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return domain.Record{}, "", fmt.Errorf("resolve domain document root: %w", err)
	}

	return record, targetPath, nil
}

func listWordPressDatabases(
	ctx context.Context,
	mariadbManager mariadb.Manager,
	targetPath string,
) []wordPressDatabase {
	config, err := loadWordPressDatabaseConfig(ctx, targetPath)
	if err != nil || strings.TrimSpace(config.Name) == "" {
		return []wordPressDatabase{}
	}

	database := wordPressDatabase{
		Name:     config.Name,
		Username: config.Username,
		Host:     config.Host,
	}

	if mariadbManager != nil {
		records, err := mariadbManager.ListDatabases(ctx)
		if err == nil {
			for _, record := range records {
				if record.Name != config.Name {
					continue
				}
				database = wordPressDatabase{
					Name:     record.Name,
					Username: firstNonEmpty(record.Username, config.Username),
					Host:     firstNonEmpty(record.Host, config.Host),
				}
				return []wordPressDatabase{database}
			}
		}
	}

	return []wordPressDatabase{database}
}

func loadWordPressDatabaseConfig(ctx context.Context, targetPath string) (wordPressDatabaseConfig, error) {
	name, err := runWordPressValueCommand(ctx, targetPath, "config", "get", "DB_NAME", "--type=constant")
	if err != nil {
		return wordPressDatabaseConfig{}, err
	}

	username, err := runWordPressValueCommand(ctx, targetPath, "config", "get", "DB_USER", "--type=constant")
	if err != nil {
		return wordPressDatabaseConfig{}, err
	}

	host, err := runWordPressValueCommand(ctx, targetPath, "config", "get", "DB_HOST", "--type=constant")
	if err != nil {
		return wordPressDatabaseConfig{}, err
	}

	return wordPressDatabaseConfig{
		Name:     strings.TrimSpace(name),
		Username: strings.TrimSpace(username),
		Host:     strings.TrimSpace(host),
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func validateWordPressInstallInput(input wordPressInstallInput, configPresent bool) domain.ValidationErrors {
	validation := domain.ValidationErrors{}

	databaseName := strings.TrimSpace(input.DatabaseName)
	siteURL := strings.TrimSpace(input.SiteURL)
	siteTitle := strings.TrimSpace(input.SiteTitle)
	adminUsername := strings.TrimSpace(input.AdminUsername)
	adminEmail := strings.TrimSpace(input.AdminEmail)

	if !configPresent {
		if databaseName == "" {
			validation["database_name"] = "Database name is required."
		} else if !wordPressTablePrefixPattern.MatchString(databaseName) {
			validation["database_name"] = "Database name can contain only letters, numbers, and underscores."
		} else if isReservedWordPressDatabaseName(databaseName) {
			validation["database_name"] = "Choose a different database name."
		}
	}

	if siteURL == "" {
		validation["site_url"] = "Site URL is required."
	} else if parsedURL, err := url.Parse(siteURL); err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		validation["site_url"] = "Enter a full site URL starting with http:// or https://."
	} else if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		validation["site_url"] = "Enter a full site URL starting with http:// or https://."
	}

	if siteTitle == "" {
		validation["site_title"] = "Site title is required."
	}

	if adminUsername == "" {
		validation["admin_username"] = "Admin username is required."
	} else if !wordPressUserPattern.MatchString(adminUsername) {
		validation["admin_username"] = "Use letters, numbers, dots, dashes, or underscores."
	}

	if adminEmail == "" {
		validation["admin_email"] = "Admin email is required."
	} else if _, err := mail.ParseAddress(adminEmail); err != nil {
		validation["admin_email"] = "Enter a valid email address."
	}

	if len(input.AdminPassword) < 8 {
		validation["admin_password"] = "Admin password must be at least 8 characters."
	}

	tablePrefix := normalizeWordPressTablePrefix(input.TablePrefix)
	if tablePrefix == "" {
		validation["table_prefix"] = "Table prefix is required."
	} else if !wordPressTablePrefixPattern.MatchString(tablePrefix) {
		validation["table_prefix"] = "Use only letters, numbers, and underscores."
	}

	return validation
}

func createWordPressDatabase(
	ctx context.Context,
	mariadbManager mariadb.Manager,
	hostname string,
	name string,
) (mariadb.DatabaseRecord, error) {
	if mariadbManager == nil {
		return mariadb.DatabaseRecord{}, errWordPressDatabaseUnavailable
	}

	username, err := generateWordPressDatabaseUsername(name)
	if err != nil {
		return mariadb.DatabaseRecord{}, fmt.Errorf("generate WordPress database username: %w", err)
	}
	password, err := generateWordPressDatabasePassword()
	if err != nil {
		return mariadb.DatabaseRecord{}, fmt.Errorf("generate WordPress database password: %w", err)
	}

	record, err := mariadbManager.CreateDatabase(ctx, mariadb.CreateDatabaseInput{
		Name:     name,
		Username: username,
		Password: password,
		Domain:   hostname,
	})
	if err == nil {
		return record, nil
	}

	var validation mariadb.ValidationErrors
	switch {
	case errors.As(err, &validation):
		wordPressValidation := domain.ValidationErrors{}
		if message := strings.TrimSpace(validation["name"]); message != "" {
			wordPressValidation["database_name"] = message
		}
		if len(wordPressValidation) > 0 {
			return mariadb.DatabaseRecord{}, wordPressValidation
		}
	case errors.Is(err, mariadb.ErrDatabaseAlreadyExists):
		return mariadb.DatabaseRecord{}, domain.ValidationErrors{
			"database_name": "This database already exists.",
		}
	}

	return mariadb.DatabaseRecord{}, err
}

func generateWordPressDatabaseUsername(databaseName string) (string, error) {
	base := sanitizeWordPressIdentifier(strings.TrimPrefix(databaseName, "wp_"))
	if base == "" {
		base = "site"
	}
	if len(base) > 12 {
		base = base[:12]
	}

	suffix, err := generateWordPressRandomString(wordPressDatabaseUsernameBytes)
	if err != nil {
		return "", err
	}

	return "wpu_" + base + "_" + strings.ToLower(suffix), nil
}

func generateWordPressDatabasePassword() (string, error) {
	randomBytes := make([]byte, wordPressDatabasePasswordBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func generateWordPressRandomString(byteLength int) (string, error) {
	randomBytes := make([]byte, byteLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(randomBytes), nil
}

func suggestedWordPressDatabaseName(hostname string) string {
	normalized := strings.TrimSpace(strings.ToLower(hostname))
	normalized = strings.TrimSuffix(normalized, ".")
	normalized = strings.TrimPrefix(normalized, "www.")
	if normalized == "" {
		return "wp_site"
	}

	if suffix, _ := publicsuffix.PublicSuffix(normalized); suffix != "" {
		trimmed := strings.TrimSuffix(normalized, "."+suffix)
		if trimmed != "" && trimmed != normalized {
			normalized = trimmed
		}
	}

	sanitized := sanitizeWordPressIdentifier(normalized)
	if sanitized == "" {
		sanitized = "site"
	}

	return "wp_" + sanitized
}

func sanitizeWordPressIdentifier(value string) string {
	replacer := strings.NewReplacer(".", "_", "-", "_")
	sanitized := replacer.Replace(strings.TrimSpace(strings.ToLower(value)))
	sanitized = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		default:
			return '_'
		}
	}, sanitized)

	return strings.Trim(squeezeUnderscores(sanitized), "_")
}

func squeezeUnderscores(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		if r == '_' {
			if lastUnderscore {
				continue
			}
			lastUnderscore = true
		} else {
			lastUnderscore = false
		}
		builder.WriteRune(r)
	}

	return builder.String()
}

func isReservedWordPressDatabaseName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "information_schema", "mysql", "performance_schema", "sys":
		return true
	default:
		return false
	}
}

func ensureWordPressInstalled(ctx context.Context, targetPath string) error {
	installed, inspectError := inspectWordPressInstallation(ctx, targetPath)
	if installed {
		return nil
	}
	if inspectError != "" {
		return errors.New(inspectError)
	}
	return errWordPressNotInstalled
}

func inspectWordPressInstallation(ctx context.Context, targetPath string) (bool, string) {
	_, output, err := runWordPressCommand(ctx, targetPath, "core", "is-installed")
	if err == nil {
		return true, ""
	}
	if isExpectedWordPressMissingState(output) {
		return false, ""
	}
	return false, strings.TrimSpace(output)
}

func isExpectedWordPressMissingState(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	switch {
	case normalized == "":
		return false
	case strings.Contains(normalized, "wordpress is not installed"):
		return true
	case strings.Contains(normalized, "this does not seem to be a wordpress installation"):
		return true
	case strings.Contains(normalized, "the site you have requested is not installed properly"):
		return true
	case strings.Contains(normalized, "wp-config.php"):
		return true
	default:
		return false
	}
}

func loadWordPressCoreUpdate(ctx context.Context, targetPath string) (*wordPressCoreUpdate, error) {
	output, _, err := runWordPressCommand(ctx, targetPath, "core", "check-update", "--format=json")
	if err != nil {
		return nil, err
	}

	var updates []wordPressCoreUpdate
	if err := json.Unmarshal(output, &updates); err != nil {
		return nil, fmt.Errorf("parse WordPress core updates: %w", err)
	}
	if len(updates) == 0 {
		return nil, nil
	}

	return &updates[0], nil
}

func loadWordPressExtensions(
	ctx context.Context,
	targetPath string,
	resource string,
) ([]wordPressExtension, error) {
	output, _, err := runWordPressCommand(ctx, targetPath, resource, "list", "--format=json")
	if err != nil {
		return nil, err
	}

	var extensions []wordPressExtension
	if err := json.Unmarshal(output, &extensions); err != nil {
		return nil, fmt.Errorf("parse WordPress %s list: %w", resource, err)
	}

	sort.Slice(extensions, func(i, j int) bool {
		leftStatus := extensions[i].Status == "active" || extensions[i].Status == "active-network"
		rightStatus := extensions[j].Status == "active" || extensions[j].Status == "active-network"
		if leftStatus != rightStatus {
			return leftStatus
		}
		return extensions[i].Name < extensions[j].Name
	})

	return extensions, nil
}

func runWordPressValueCommand(ctx context.Context, targetPath string, args ...string) (string, error) {
	output, _, err := runWordPressCommand(ctx, targetPath, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func runWordPressCommand(
	ctx context.Context,
	targetPath string,
	args ...string,
) ([]byte, string, error) {
	cli, err := resolveWordPressCLI(ctx)
	if err != nil {
		return nil, "", err
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, wordPressActionTimeout)
		defer cancel()
	}

	commandArgs := append([]string{"--allow-root", "--path=" + targetPath}, args...)
	cmd := exec.CommandContext(runCtx, cli.execPath, commandArgs...)
	cmd.Dir = targetPath
	cmd.Env = os.Environ()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	if err != nil {
		return stdout.Bytes(), output, formatWordPressCommandError(runCtx, args, output, err)
	}

	return stdout.Bytes(), output, nil
}

type wordPressCLI struct {
	execPath string
	path     string
}

func resolveWordPressCLI(ctx context.Context) (wordPressCLI, error) {
	if cli, ok := localWordPressCLI(); ok {
		return cli, nil
	}

	wordPressCLIMu.Lock()
	defer wordPressCLIMu.Unlock()

	if cli, ok := localWordPressCLI(); ok {
		return cli, nil
	}

	cli, err := installLocalWordPressCLI(ctx)
	if err == nil {
		return cli, nil
	}

	if systemPath, systemErr := exec.LookPath("wp"); systemErr == nil {
		return wordPressCLI{execPath: systemPath, path: systemPath}, nil
	}

	return wordPressCLI{}, fmt.Errorf("%w: %v", errWordPressCLIUnavailable, err)
}

func localWordPressCLI() (wordPressCLI, bool) {
	executablePath, pharPath := localWordPressCLIPaths()
	if !fileExists(executablePath) || !fileExists(pharPath) {
		return wordPressCLI{}, false
	}

	return wordPressCLI{
		execPath: executablePath,
		path:     executablePath,
	}, true
}

func installLocalWordPressCLI(ctx context.Context) (wordPressCLI, error) {
	if err := config.EnsureBinPath(); err != nil {
		return wordPressCLI{}, err
	}

	executablePath, pharPath := localWordPressCLIPaths()
	phpPath, err := exec.LookPath("php")
	if err != nil {
		return wordPressCLI{}, fmt.Errorf("php is required to run wp-cli")
	}

	if !fileExists(pharPath) {
		if err := downloadWordPressCLIPhar(ctx, pharPath); err != nil {
			return wordPressCLI{}, err
		}
	}

	if err := writeWordPressCLIWrapper(executablePath, pharPath, phpPath); err != nil {
		return wordPressCLI{}, err
	}

	return wordPressCLI{
		execPath: executablePath,
		path:     executablePath,
	}, nil
}

func localWordPressCLIPaths() (string, string) {
	binPath := config.BinPath()
	pharPath := filepath.Join(binPath, "wp-cli.phar")
	executableName := "wp"
	if runtime.GOOS == "windows" {
		executableName = "wp.cmd"
	}

	return filepath.Join(binPath, executableName), pharPath
}

func downloadWordPressCLIPhar(ctx context.Context, destinationPath string) error {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, 2*time.Minute)
		defer cancel()
	}

	request, err := nethttp.NewRequestWithContext(runCtx, nethttp.MethodGet, wordPressCLIPharURL, nil)
	if err != nil {
		return fmt.Errorf("create wp-cli download request: %w", err)
	}

	response, err := nethttp.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download wp-cli: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != nethttp.StatusOK {
		return fmt.Errorf("download wp-cli: unexpected status %d", response.StatusCode)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(destinationPath), "wp-cli-*.phar")
	if err != nil {
		return fmt.Errorf("create temporary wp-cli file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := io.Copy(tempFile, response.Body); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write wp-cli archive: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close wp-cli archive: %w", err)
	}
	if err := os.Chmod(tempPath, 0o755); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("make wp-cli archive executable: %w", err)
	}
	if err := os.Rename(tempPath, destinationPath); err != nil {
		return fmt.Errorf("install wp-cli archive: %w", err)
	}

	return nil
}

func writeWordPressCLIWrapper(executablePath string, pharPath string, phpPath string) error {
	var content string
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		content = fmt.Sprintf("@echo off\r\n\"%s\" \"%s\" %%*\r\n", phpPath, pharPath)
		mode = 0o644
	} else {
		content = fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"%s\" \"$@\"\n", phpPath, pharPath)
	}

	if err := os.WriteFile(executablePath, []byte(content), mode); err != nil {
		return fmt.Errorf("write wp-cli launcher: %w", err)
	}

	return nil
}

func formatWordPressCommandError(
	ctx context.Context,
	args []string,
	output string,
	err error,
) error {
	command := "wp " + strings.Join(args, " ")
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s timed out", command)
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf("%s was canceled", command)
	case output != "":
		return fmt.Errorf("%s failed: %s", command, output)
	default:
		return fmt.Errorf("%s failed: %w", command, err)
	}
}

func wordPressCoreFilesPresent(targetPath string) bool {
	return fileExists(filepath.Join(targetPath, "wp-load.php")) &&
		fileExists(filepath.Join(targetPath, "wp-settings.php"))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func directoryIsEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func normalizeWordPressTablePrefix(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "wp_"
	}
	return normalized
}
