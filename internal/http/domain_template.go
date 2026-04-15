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
	"flowpanel/internal/phpenv"
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
	php phpenv.Manager,
	hostname string,
	input domainTemplateInstallInput,
) (domainTemplateInstallResult, domain.Record, bool, error) {
	templateKey := strings.TrimSpace(strings.ToLower(input.Template))
	if templateKey == "wordpress" {
		status, record, executedAsWorker, err := installWordPress(ctx, domains, mariadbManager, php, hostname, wordPressInstallInput{
			DatabaseName:      input.DatabaseName,
			SiteTitle:         input.SiteTitle,
			AdminUsername:     input.AdminUsername,
			AdminEmail:        input.AdminEmail,
			AdminPassword:     input.AdminPassword,
			TablePrefix:       input.TablePrefix,
			ClearDocumentRoot: input.ClearDocumentRoot,
		})
		if err != nil {
			return domainTemplateInstallResult{}, record, false, err
		}

		return domainTemplateInstallResult{
			Template:  templateKey,
			WordPress: &status,
		}, record, executedAsWorker, nil
	}

	record, targetPath, err := resolveDomainTemplateDomain(domains, hostname)
	if err != nil {
		return domainTemplateInstallResult{}, domain.Record{}, false, err
	}

	validation := validateDomainTemplateInstallInput(templateKey, input)
	if len(validation) > 0 {
		return domainTemplateInstallResult{}, record, false, validation
	}

	definition := domainTemplateDefinitions[templateKey]
	executedAsWorker, err := installComposerTemplate(ctx, mariadbManager, php, record.PHPVersion, targetPath, hostname, templateKey, definition, input)
	if err != nil {
		return domainTemplateInstallResult{}, record, false, err
	}

	return domainTemplateInstallResult{Template: templateKey}, record, executedAsWorker, nil
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
	php phpenv.Manager,
	phpVersion string,
	targetPath string,
	hostname string,
	templateKey string,
	definition domainTemplateDefinition,
	input domainTemplateInstallInput,
) (bool, error) {
	composerPath, err := exec.LookPath("composer")
	if err != nil {
		return false, errComposerUnavailable
	}

	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return false, fmt.Errorf("ensure document root: %w", err)
	}

	empty, err := directoryIsEmpty(targetPath)
	if err != nil {
		return false, fmt.Errorf("inspect document root: %w", err)
	}
	if !empty && !input.ClearDocumentRoot {
		return false, errDomainTemplateInstallDirectoryDirty
	}

	stageRoot, err := os.MkdirTemp("", "flowpanel-template-*")
	if err != nil {
		return false, fmt.Errorf("create template staging directory: %w", err)
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
	executedAsWorker, err := runTemplateCommand(
		runCtx,
		php,
		phpVersion,
		stageRoot,
		composerEnv,
		"template installation",
		composerPath,
		"create-project",
		"--no-interaction",
		"--no-progress",
		definition.packageName,
		stagePath,
	)
	if err != nil {
		return false, err
	}

	if err := finalizeComposerTemplateInstall(runCtx, mariadbManager, php, phpVersion, hostname, templateKey, stagePath, input); err != nil {
		return false, err
	}

	if input.ClearDocumentRoot {
		if err := clearDocumentRootContents(targetPath); err != nil {
			return false, err
		}
	}

	entries, err := os.ReadDir(stagePath)
	if err != nil {
		return false, fmt.Errorf("read staged template files: %w", err)
	}
	copiedPaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		sourcePath := filepath.Join(stagePath, entry.Name())
		destinationPath := filepath.Join(targetPath, entry.Name())
		if err := filesvc.CopyPath(sourcePath, destinationPath); err != nil {
			return false, fmt.Errorf("copy template file %q: %w", entry.Name(), err)
		}
		copiedPaths = append(copiedPaths, destinationPath)
	}
	if executedAsWorker && len(copiedPaths) > 0 {
		if err := ensurePHPWorkerOwnership(runCtx, php, phpVersion, true, copiedPaths...); err != nil {
			return false, err
		}
	}

	return executedAsWorker, nil
}

func finalizeComposerTemplateInstall(
	ctx context.Context,
	mariadbManager mariadb.Manager,
	php phpenv.Manager,
	phpVersion string,
	hostname string,
	templateKey string,
	stagePath string,
	input domainTemplateInstallInput,
) error {
	switch templateKey {
	case "symfony":
		return finalizeSymfonyInstall(ctx, php, phpVersion, stagePath)
	case "laravel":
		return finalizeLaravelInstall(ctx, php, phpVersion, hostname, stagePath, input)
	case "octobercms":
		return finalizeOctoberCMSInstall(ctx, mariadbManager, php, phpVersion, hostname, stagePath, input)
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

func finalizeSymfonyInstall(ctx context.Context, php phpenv.Manager, phpVersion string, stagePath string) error {
	composerPath, err := exec.LookPath("composer")
	if err != nil {
		return errComposerUnavailable
	}

	_, err = runTemplateCommand(
		ctx,
		php,
		phpVersion,
		stagePath,
		append(os.Environ(), "COMPOSER_ALLOW_SUPERUSER=1"),
		"symfony setup",
		composerPath,
		"require",
		"--no-interaction",
		"--no-progress",
		"webapp",
	)
	return err
}

func finalizeLaravelInstall(
	ctx context.Context,
	php phpenv.Manager,
	phpVersion string,
	hostname string,
	stagePath string,
	input domainTemplateInstallInput,
) error {
	if err := ensureTemplatePHPExtensions(ctx, php, phpVersion, "pdo_sqlite", "sqlite3"); err != nil {
		return fmt.Errorf("prepare Laravel PHP extensions: %w", err)
	}

	envPath := filepath.Join(stagePath, ".env")
	if err := ensureTemplateFile(envPath, filepath.Join(stagePath, ".env.example")); err != nil {
		return err
	}
	if err := ensureLaravelSQLiteDatabase(stagePath); err != nil {
		return err
	}
	if fileExists(envPath) {
		updates := map[string]string{
			"APP_NAME":         quoteEnvValue(input.AppName),
			"APP_URL":          quoteEnvValue(defaultTemplateSiteURL(hostname)),
			"DB_CONNECTION":    quoteEnvValue("sqlite"),
			"SESSION_DRIVER":   quoteEnvValue("file"),
			"CACHE_STORE":      quoteEnvValue("file"),
			"QUEUE_CONNECTION": quoteEnvValue("sync"),
		}
		for key, value := range updates {
			if err := upsertEnvValue(envPath, key, value); err != nil {
				return fmt.Errorf("update Laravel %s: %w", strings.ToLower(key), err)
			}
		}
	}

	phpPath, err := exec.LookPath("php")
	if err != nil {
		return fmt.Errorf("php is required to finish Laravel setup")
	}

	_, err = runTemplateCommand(
		ctx,
		php,
		phpVersion,
		stagePath,
		os.Environ(),
		"laravel setup",
		phpPath,
		"artisan",
		"key:generate",
		"--force",
	)
	return err
}

func ensureLaravelSQLiteDatabase(stagePath string) error {
	databaseDir := filepath.Join(stagePath, "database")
	if err := os.MkdirAll(databaseDir, 0o755); err != nil {
		return fmt.Errorf("create Laravel database directory: %w", err)
	}

	databasePath := filepath.Join(databaseDir, "database.sqlite")
	if fileExists(databasePath) {
		return nil
	}
	if err := os.WriteFile(databasePath, []byte{}, 0o644); err != nil {
		return fmt.Errorf("create Laravel sqlite database: %w", err)
	}

	return nil
}

func ensureTemplatePHPExtensions(ctx context.Context, php phpenv.Manager, version string, extensions ...string) error {
	if php == nil {
		return nil
	}

	status := php.StatusForVersion(ctx, version)
	for _, extension := range extensions {
		extension = strings.TrimSpace(extension)
		if extension == "" || extensionLoaded(status.Extensions, findTemplateExtensionAliases(extension)...) {
			continue
		}

		nextStatus, err := php.InstallExtensionForVersion(ctx, version, extension)
		if err != nil {
			return err
		}
		status = nextStatus
	}

	return nil
}

func findTemplateExtensionAliases(extension string) []string {
	switch extension {
	case "pdo_sqlite":
		return []string{"pdo_sqlite", "sqlite3"}
	case "sqlite3":
		return []string{"sqlite3", "pdo_sqlite"}
	default:
		return []string{extension}
	}
}

func extensionLoaded(loaded []string, names ...string) bool {
	if len(loaded) == 0 || len(names) == 0 {
		return false
	}

	seen := make(map[string]struct{}, len(loaded))
	for _, extension := range loaded {
		seen[strings.ToLower(strings.TrimSpace(extension))] = struct{}{}
	}
	for _, name := range names {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(name))]; ok {
			return true
		}
	}

	return false
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
	php phpenv.Manager,
	phpVersion string,
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

	runOctober := func(args ...string) error {
		commandArgs := append([]string{"artisan"}, args...)
		_, err := runTemplateCommand(
			ctx,
			php,
			phpVersion,
			stagePath,
			os.Environ(),
			"october cms setup",
			phpPath,
			commandArgs...,
		)
		return err
	}

	for _, args := range [][]string{
		{"key:generate", "--force"},
		{"october:migrate", "--force"},
	} {
		if err := runOctober(args...); err != nil {
			return err
		}
	}

	if fileExists(filepath.Join(stagePath, "themes", "demo")) {
		if err := runOctober("theme:seed", "demo", "--root"); err != nil {
			return err
		}
	}

	for _, args := range [][]string{
		{"tailor:migrate"},
		{"optimize:clear"},
	} {
		if err := runOctober(args...); err != nil {
			return err
		}
	}

	return nil
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
	php phpenv.Manager,
	phpVersion string,
	dir string,
	env []string,
	failureLabel string,
	command string,
	args ...string,
) (bool, error) {
	run := func(useWorker bool) (bool, string, error) {
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir = dir
		cmd.Env = env
		executedAsWorker := false
		if useWorker {
			var err error
			executedAsWorker, err = configureCommandForPHPWorker(ctx, php, phpVersion, cmd)
			if err != nil {
				return false, "", err
			}
		}

		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output

		err := cmd.Run()
		return executedAsWorker, strings.TrimSpace(output.String()), err
	}

	executedAsWorker, message, err := run(true)
	if err != nil && executedAsWorker && shouldRetryWithoutPHPWorker(err) {
		executedAsWorker, message, err = run(false)
	}
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return false, fmt.Errorf("%s timed out", failureLabel)
		case errors.Is(ctx.Err(), context.Canceled):
			return false, fmt.Errorf("%s was canceled", failureLabel)
		case message != "":
			return false, fmt.Errorf("%s failed: %s", failureLabel, message)
		default:
			return false, fmt.Errorf("%s failed: %w", failureLabel, err)
		}
	}

	return executedAsWorker, nil
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
