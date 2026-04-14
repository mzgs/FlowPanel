package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"flowpanel/internal/domain"
	filesvc "flowpanel/internal/files"
	"flowpanel/internal/mariadb"
)

const domainTemplateActionTimeout = 10 * time.Minute

var (
	errDomainTemplateUnsupportedDomain     = errors.New("PHP app installation is available only for PHP site domains")
	errDomainTemplateInstallDirectoryDirty = errors.New("document root is not empty, so installation was refused")
	errDomainTemplateDatabaseUnavailable   = errors.New("MariaDB is not configured on this server")
)

type domainTemplateInstallInput struct {
	Template          string `json:"template"`
	ClearDocumentRoot bool   `json:"clear_document_root"`
	AppName           string `json:"app_name"`
	DatabaseName      string `json:"database_name"`
	SiteTitle         string `json:"site_title"`
	AdminUsername     string `json:"admin_username"`
	AdminEmail        string `json:"admin_email"`
	AdminPassword     string `json:"admin_password"`
	TablePrefix       string `json:"table_prefix"`
}

type domainTemplateInstallResult struct {
	Template  string           `json:"template"`
	WordPress *wordPressStatus `json:"wordpress,omitempty"`
}

type domainTemplateDefinition struct {
	packageName string
}

var domainTemplateDefinitions = map[string]domainTemplateDefinition{
	"symfony": {
		packageName: "symfony/skeleton",
	},
	"laravel": {
		packageName: "laravel/laravel",
	},
	"octobercms": {
		packageName: "october/october",
	},
	"cakephp": {
		packageName: "cakephp/app",
	},
	"codeigniter": {
		packageName: "codeigniter4/appstarter",
	},
	"slim": {
		packageName: "slim/slim-skeleton",
	},
}

func installDomainTemplate(
	ctx context.Context,
	domains *domain.Service,
	mariadbManager mariadb.Manager,
	hostname string,
	input domainTemplateInstallInput,
) (domainTemplateInstallResult, domain.Record, error) {
	templateKey := strings.TrimSpace(strings.ToLower(input.Template))
	if templateKey == "wordpress" {
		status, record, err := installWordPress(ctx, domains, mariadbManager, hostname, wordPressInstallInput{
			DatabaseName:      input.DatabaseName,
			SiteTitle:         input.SiteTitle,
			AdminUsername:     input.AdminUsername,
			AdminEmail:        input.AdminEmail,
			AdminPassword:     input.AdminPassword,
			TablePrefix:       input.TablePrefix,
			ClearDocumentRoot: input.ClearDocumentRoot,
		})
		if err != nil {
			return domainTemplateInstallResult{}, record, err
		}

		return domainTemplateInstallResult{
			Template:  templateKey,
			WordPress: &status,
		}, record, nil
	}

	record, targetPath, err := resolveDomainTemplateDomain(domains, hostname)
	if err != nil {
		return domainTemplateInstallResult{}, domain.Record{}, err
	}

	validation := validateDomainTemplateInstallInput(templateKey, input)
	if len(validation) > 0 {
		return domainTemplateInstallResult{}, record, validation
	}

	definition := domainTemplateDefinitions[templateKey]
	if err := installComposerTemplate(ctx, mariadbManager, targetPath, hostname, templateKey, definition, input); err != nil {
		return domainTemplateInstallResult{}, record, err
	}

	return domainTemplateInstallResult{Template: templateKey}, record, nil
}

func resolveDomainTemplateDomain(domains *domain.Service, hostname string) (domain.Record, string, error) {
	record, ok := domains.FindByHostname(hostname)
	if !ok {
		return domain.Record{}, "", domain.ErrNotFound
	}
	if record.Kind != domain.KindPHP {
		return domain.Record{}, "", errDomainTemplateUnsupportedDomain
	}

	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return domain.Record{}, "", fmt.Errorf("resolve domain document root: %w", err)
	}

	return record, targetPath, nil
}

func validateDomainTemplateInstallInput(
	templateKey string,
	input domainTemplateInstallInput,
) domain.ValidationErrors {
	switch templateKey {
	case "wordpress":
		return validateWordPressInstallInput(wordPressInstallInput{
			DatabaseName:  input.DatabaseName,
			SiteTitle:     input.SiteTitle,
			AdminUsername: input.AdminUsername,
			AdminEmail:    input.AdminEmail,
			AdminPassword: input.AdminPassword,
			TablePrefix:   input.TablePrefix,
		}, false)
	case "symfony":
		return nil
	case "laravel":
		return validateTemplateInstallFields(input, true, false)
	case "octobercms":
		return validateTemplateInstallFields(input, true, true)
	case "cakephp":
		return nil
	case "codeigniter":
		return nil
	case "slim":
		return validateTemplateInstallFields(input, true, false)
	default:
		return domain.ValidationErrors{
			"template": "Select a supported application.",
		}
	}
}

func validateTemplateInstallFields(
	input domainTemplateInstallInput,
	requireAppName bool,
	requireDatabase bool,
) domain.ValidationErrors {
	validation := domain.ValidationErrors{}

	if requireAppName && strings.TrimSpace(input.AppName) == "" {
		validation["app_name"] = "Application name is required."
	}
	if requireDatabase && strings.TrimSpace(input.DatabaseName) == "" {
		validation["database_name"] = "Database name is required."
	}

	return validation
}

func installComposerTemplate(
	ctx context.Context,
	mariadbManager mariadb.Manager,
	targetPath string,
	hostname string,
	templateKey string,
	definition domainTemplateDefinition,
	input domainTemplateInstallInput,
) error {
	composerPath, err := exec.LookPath("composer")
	if err != nil {
		return errComposerUnavailable
	}

	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return fmt.Errorf("ensure document root: %w", err)
	}

	empty, err := directoryIsEmpty(targetPath)
	if err != nil {
		return fmt.Errorf("inspect document root: %w", err)
	}
	if !empty && !input.ClearDocumentRoot {
		return errDomainTemplateInstallDirectoryDirty
	}

	stageRoot, err := os.MkdirTemp("", "flowpanel-template-*")
	if err != nil {
		return fmt.Errorf("create template staging directory: %w", err)
	}
	defer os.RemoveAll(stageRoot)

	stagePath := filepath.Join(stageRoot, "app")
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, domainTemplateActionTimeout)
		defer cancel()
	}

	composerEnv := append(os.Environ(), "COMPOSER_ALLOW_SUPERUSER=1")
	if err := runTemplateCommand(
		runCtx,
		stageRoot,
		composerEnv,
		"template installation",
		composerPath,
		"create-project",
		"--no-interaction",
		"--no-progress",
		definition.packageName,
		stagePath,
	); err != nil {
		return err
	}

	if err := finalizeComposerTemplateInstall(runCtx, mariadbManager, hostname, templateKey, stagePath, input); err != nil {
		return err
	}

	if input.ClearDocumentRoot {
		if err := clearDocumentRootContents(targetPath); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(stagePath)
	if err != nil {
		return fmt.Errorf("read staged template files: %w", err)
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(stagePath, entry.Name())
		destinationPath := filepath.Join(targetPath, entry.Name())
		if err := filesvc.CopyPath(sourcePath, destinationPath); err != nil {
			return fmt.Errorf("copy template file %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func finalizeComposerTemplateInstall(
	ctx context.Context,
	mariadbManager mariadb.Manager,
	hostname string,
	templateKey string,
	stagePath string,
	input domainTemplateInstallInput,
) error {
	switch templateKey {
	case "symfony":
		return finalizeSymfonyInstall(ctx, stagePath)
	case "laravel":
		return finalizeLaravelInstall(ctx, hostname, stagePath, input)
	case "octobercms":
		return finalizeOctoberCMSInstall(ctx, mariadbManager, hostname, stagePath, input)
	case "cakephp":
		return nil
	case "codeigniter":
		return finalizeCodeIgniterInstall(hostname, stagePath)
	case "slim":
		return finalizeSlimInstall(stagePath, input)
	default:
		return nil
	}
}

func finalizeSymfonyInstall(ctx context.Context, stagePath string) error {
	composerPath, err := exec.LookPath("composer")
	if err != nil {
		return errComposerUnavailable
	}

	return runTemplateCommand(
		ctx,
		stagePath,
		append(os.Environ(), "COMPOSER_ALLOW_SUPERUSER=1"),
		"symfony setup",
		composerPath,
		"require",
		"--no-interaction",
		"--no-progress",
		"webapp",
	)
}

func finalizeLaravelInstall(
	ctx context.Context,
	hostname string,
	stagePath string,
	input domainTemplateInstallInput,
) error {
	envPath := filepath.Join(stagePath, ".env")
	if err := ensureTemplateFile(envPath, filepath.Join(stagePath, ".env.example")); err != nil {
		return err
	}
	if fileExists(envPath) {
		if err := upsertEnvValue(envPath, "APP_NAME", quoteEnvValue(input.AppName)); err != nil {
			return fmt.Errorf("update Laravel app name: %w", err)
		}
		if err := upsertEnvValue(envPath, "APP_URL", quoteEnvValue(defaultTemplateSiteURL(hostname))); err != nil {
			return fmt.Errorf("update Laravel app url: %w", err)
		}
	}

	phpPath, err := exec.LookPath("php")
	if err != nil {
		return fmt.Errorf("php is required to finish Laravel setup")
	}

	return runTemplateCommand(
		ctx,
		stagePath,
		os.Environ(),
		"laravel setup",
		phpPath,
		"artisan",
		"key:generate",
		"--force",
	)
}

func finalizeCodeIgniterInstall(hostname string, stagePath string) error {
	envPath := filepath.Join(stagePath, ".env")
	if err := ensureTemplateFile(envPath, filepath.Join(stagePath, "env")); err != nil {
		return err
	}
	if !fileExists(envPath) {
		return nil
	}

	return upsertINIValue(envPath, "app.baseURL", quoteINIValue(ensureTrailingSlash(defaultTemplateSiteURL(hostname))))
}

func finalizeSlimInstall(stagePath string, input domainTemplateInstallInput) error {
	envPath := filepath.Join(stagePath, ".env")
	if err := ensureTemplateFile(envPath, filepath.Join(stagePath, ".env.example")); err != nil {
		return err
	}
	if !fileExists(envPath) {
		return nil
	}

	if strings.TrimSpace(input.AppName) != "" {
		if err := upsertEnvValue(envPath, "APP_NAME", quoteEnvValue(input.AppName)); err != nil {
			return fmt.Errorf("update Slim app name: %w", err)
		}
	}

	return nil
}

func finalizeOctoberCMSInstall(
	ctx context.Context,
	mariadbManager mariadb.Manager,
	hostname string,
	stagePath string,
	input domainTemplateInstallInput,
) error {
	envPath := filepath.Join(stagePath, ".env")
	if err := ensureTemplateFile(envPath, filepath.Join(stagePath, ".env.example")); err != nil {
		return err
	}

	database, err := createDomainTemplateDatabase(
		ctx,
		mariadbManager,
		hostname,
		strings.TrimSpace(input.DatabaseName),
		"ocu",
	)
	if err != nil {
		return err
	}

	if fileExists(envPath) {
		updates := map[string]string{
			"APP_NAME":      quoteEnvValue(input.AppName),
			"APP_URL":       quoteEnvValue(defaultTemplateSiteURL(hostname)),
			"DB_CONNECTION": quoteEnvValue("mysql"),
			"DB_HOST":       quoteEnvValue(defaultTemplateDatabaseHost(database.Host)),
			"DB_PORT":       quoteEnvValue("3306"),
			"DB_DATABASE":   quoteEnvValue(database.Name),
			"DB_USERNAME":   quoteEnvValue(database.Username),
			"DB_PASSWORD":   quoteEnvValue(database.Password),
		}

		for key, value := range updates {
			if err := upsertEnvValue(envPath, key, value); err != nil {
				return fmt.Errorf("update October CMS %s: %w", strings.ToLower(key), err)
			}
		}
	}

	phpPath, err := exec.LookPath("php")
	if err != nil {
		return fmt.Errorf("php is required to finish October CMS setup")
	}

	if err := runTemplateCommand(
		ctx,
		stagePath,
		os.Environ(),
		"october cms setup",
		phpPath,
		"artisan",
		"key:generate",
		"--force",
	); err != nil {
		return err
	}

	return runTemplateCommand(
		ctx,
		stagePath,
		os.Environ(),
		"october cms setup",
		phpPath,
		"artisan",
		"october:migrate",
		"--force",
	)
}

func ensureTemplateFile(destinationPath string, sourcePath string) error {
	if fileExists(destinationPath) || !fileExists(sourcePath) {
		return nil
	}

	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read template file: %w", err)
	}

	if err := os.WriteFile(destinationPath, content, 0o644); err != nil {
		return fmt.Errorf("write template file: %w", err)
	}

	return nil
}

func upsertEnvValue(path string, key string, value string) error {
	return upsertTemplateValue(path, regexp.MustCompile(`^\s*#?\s*`+regexp.QuoteMeta(key)+`\s*=`), fmt.Sprintf("%s=%s", key, value))
}

func upsertINIValue(path string, key string, value string) error {
	return upsertTemplateValue(path, regexp.MustCompile(`^\s*#?\s*`+regexp.QuoteMeta(key)+`\s*=`), fmt.Sprintf("%s = %s", key, value))
}

func upsertTemplateValue(path string, pattern *regexp.Regexp, line string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	replaced := false
	for index, current := range lines {
		if !pattern.MatchString(current) {
			continue
		}
		lines[index] = line
		replaced = true
		break
	}
	if !replaced {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, line)
	}

	nextContent := strings.Join(lines, "\n")
	if !strings.HasSuffix(nextContent, "\n") {
		nextContent += "\n"
	}

	return os.WriteFile(path, []byte(nextContent), 0o644)
}

func quoteEnvValue(value string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(value), `"`, `\"`) + `"`
}

func quoteINIValue(value string) string {
	return `'` + strings.ReplaceAll(strings.TrimSpace(value), `'`, `\'`) + `'`
}

func ensureTrailingSlash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasSuffix(trimmed, "/") {
		return trimmed
	}
	return trimmed + "/"
}

func defaultTemplateDatabaseHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return "localhost"
	}

	return host
}

func defaultTemplateSiteURL(hostname string) string {
	trimmed := strings.TrimSpace(hostname)
	if trimmed == "" {
		return ""
	}
	return "https://" + trimmed
}

func runTemplateCommand(
	ctx context.Context,
	dir string,
	env []string,
	failureLabel string,
	command string,
	args ...string,
) error {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	cmd.Env = env

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return fmt.Errorf("%s timed out", failureLabel)
		case errors.Is(ctx.Err(), context.Canceled):
			return fmt.Errorf("%s was canceled", failureLabel)
		case message != "":
			return fmt.Errorf("%s failed: %s", failureLabel, message)
		default:
			return fmt.Errorf("%s failed: %w", failureLabel, err)
		}
	}

	return nil
}

func createDomainTemplateDatabase(
	ctx context.Context,
	mariadbManager mariadb.Manager,
	hostname string,
	name string,
	usernamePrefix string,
) (mariadb.DatabaseRecord, error) {
	if mariadbManager == nil {
		return mariadb.DatabaseRecord{}, errDomainTemplateDatabaseUnavailable
	}

	username, err := generateTemplateDatabaseUsername(name, usernamePrefix)
	if err != nil {
		return mariadb.DatabaseRecord{}, fmt.Errorf("generate database username: %w", err)
	}
	password, err := generateTemplateDatabasePassword()
	if err != nil {
		return mariadb.DatabaseRecord{}, fmt.Errorf("generate database password: %w", err)
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
		templateValidation := domain.ValidationErrors{}
		if message := strings.TrimSpace(validation["name"]); message != "" {
			templateValidation["database_name"] = message
		}
		if len(templateValidation) > 0 {
			return mariadb.DatabaseRecord{}, templateValidation
		}
	case errors.Is(err, mariadb.ErrDatabaseAlreadyExists):
		return mariadb.DatabaseRecord{}, domain.ValidationErrors{
			"database_name": "This database already exists.",
		}
	}

	return mariadb.DatabaseRecord{}, err
}

func generateTemplateDatabaseUsername(databaseName string, usernamePrefix string) (string, error) {
	base := sanitizeTemplateIdentifier(databaseName)
	if base == "" {
		base = "site"
	}
	if len(base) > 12 {
		base = base[:12]
	}

	suffix, err := generateTemplateRandomString(wordPressDatabaseUsernameBytes)
	if err != nil {
		return "", err
	}

	return usernamePrefix + "_" + base + "_" + strings.ToLower(suffix), nil
}

func generateTemplateDatabasePassword() (string, error) {
	randomBytes := make([]byte, wordPressDatabasePasswordBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func generateTemplateRandomString(byteLength int) (string, error) {
	randomBytes := make([]byte, byteLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(randomBytes), nil
}

func sanitizeTemplateIdentifier(value string) string {
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
