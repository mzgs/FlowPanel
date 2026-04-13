package httpx

import (
	"bytes"
	"context"
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
	"laravel": {
		packageName: "laravel/laravel",
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
	if err := installComposerTemplate(ctx, targetPath, hostname, templateKey, definition, input); err != nil {
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
	case "laravel":
		return validateTemplateAppInput(input, true)
	case "codeigniter":
		return nil
	case "slim":
		return validateTemplateAppInput(input, true)
	default:
		return domain.ValidationErrors{
			"template": "Select a supported application.",
		}
	}
}

func validateTemplateAppInput(
	input domainTemplateInstallInput,
	requireAppName bool,
) domain.ValidationErrors {
	validation := domain.ValidationErrors{}

	if requireAppName && strings.TrimSpace(input.AppName) == "" {
		validation["app_name"] = "Application name is required."
	}

	return validation
}

func installComposerTemplate(
	ctx context.Context,
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

	cmd := exec.CommandContext(
		runCtx,
		composerPath,
		"create-project",
		"--no-interaction",
		"--no-progress",
		definition.packageName,
		stagePath,
	)
	cmd.Env = append(os.Environ(), "COMPOSER_ALLOW_SUPERUSER=1")

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return fmt.Errorf("template installation timed out")
		case errors.Is(runCtx.Err(), context.Canceled):
			return fmt.Errorf("template installation was canceled")
		case message != "":
			return fmt.Errorf("template installation failed: %s", message)
		default:
			return fmt.Errorf("template installation failed: %w", err)
		}
	}

	if err := finalizeComposerTemplateInstall(runCtx, hostname, templateKey, stagePath, input); err != nil {
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
	hostname string,
	templateKey string,
	stagePath string,
	input domainTemplateInstallInput,
) error {
	switch templateKey {
	case "laravel":
		return finalizeLaravelInstall(ctx, hostname, stagePath, input)
	case "codeigniter":
		return finalizeCodeIgniterInstall(hostname, stagePath)
	case "slim":
		return finalizeSlimInstall(stagePath, input)
	default:
		return nil
	}
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

	cmd := exec.CommandContext(ctx, phpPath, "artisan", "key:generate", "--force")
	cmd.Dir = stagePath
	cmd.Env = os.Environ()

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		if message != "" {
			return fmt.Errorf("laravel setup failed: %s", message)
		}
		return fmt.Errorf("laravel setup failed: %w", err)
	}

	return nil
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

func defaultTemplateSiteURL(hostname string) string {
	trimmed := strings.TrimSpace(hostname)
	if trimmed == "" {
		return ""
	}
	return "https://" + trimmed
}
