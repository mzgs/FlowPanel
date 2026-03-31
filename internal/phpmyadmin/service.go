package phpmyadmin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"go.uber.org/zap"
)

var cellarVersionPattern = regexp.MustCompile(`/phpmyadmin/([^/]+)/`)

const (
	debianConfigUserPath       = "/etc/phpmyadmin/conf.d/zz-flowpanel-phpmyadmin.php"
	debianCreateTablesSQLPath  = "/usr/share/phpmyadmin/sql/create_tables.sql"
	rpmConfigUserPath          = "/etc/phpMyAdmin/config.inc.php"
	rpmCreateTablesSQLPath     = "/usr/share/phpMyAdmin/sql/create_tables.sql"
	mariaDBRootUser            = "root"
	defaultMariaDBPasswordFile = "mariadb-root-password"
)

var (
	phpMyAdminStorageTablePattern = regexp.MustCompile("(?im)^\\s*CREATE\\s+TABLE\\s+IF\\s+NOT\\s+EXISTS\\s+`([^`]+)`")
	phpMyAdminConfigStartMarker   = "// BEGIN FlowPanel phpMyAdmin configuration storage"
	phpMyAdminConfigEndMarker     = "// END FlowPanel phpMyAdmin configuration storage"
	phpMyAdminStorageTableConfig  = map[string]string{
		"pma__bookmark":          "bookmarktable",
		"pma__central_columns":   "central_columns",
		"pma__column_info":       "column_info",
		"pma__designer_settings": "designer_settings",
		"pma__export_templates":  "export_templates",
		"pma__favorite":          "favorite",
		"pma__history":           "history",
		"pma__navigationhiding":  "navigationhiding",
		"pma__pdf_pages":         "pdf_pages",
		"pma__recent":            "recent",
		"pma__relation":          "relation",
		"pma__savedsearches":     "savedsearches",
		"pma__table_coords":      "table_coords",
		"pma__table_info":        "table_info",
		"pma__table_uiprefs":     "table_uiprefs",
		"pma__tracking":          "tracking",
		"pma__userconfig":        "userconfig",
		"pma__usergroups":        "usergroups",
		"pma__users":             "users",
	}
	mariaDBSocketCandidates = []string{
		"/run/mysqld/mysqld.sock",
		"/var/run/mysqld/mysqld.sock",
		"/run/mysql/mysql.sock",
		"/var/run/mysql/mysql.sock",
		"/run/mariadb/mariadb.sock",
		"/var/run/mariadb/mariadb.sock",
		"/tmp/mysql.sock",
		"/tmp/mysqld.sock",
	}
)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Installed        bool     `json:"installed"`
	InstallPath      string   `json:"install_path,omitempty"`
	Version          string   `json:"version,omitempty"`
	State            string   `json:"state"`
	Message          string   `json:"message"`
	Issues           []string `json:"issues,omitempty"`
	InstallAvailable bool     `json:"install_available"`
	InstallLabel     string   `json:"install_label,omitempty"`
}

type Service struct {
	logger *zap.Logger
}

type actionPlan struct {
	packageManager string
	installLabel   string
	installEnv     map[string]string
	installCmds    [][]string
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
	}
}

func (s *Service) Status(context.Context) Status {
	status := Status{
		Platform: runtime.GOOS,
	}

	plan := detectActionPlan()
	status.PackageManager = plan.packageManager
	status.InstallLabel = plan.installLabel

	installPath, installed := detectInstallPath()
	status.Installed = installed
	status.InstallPath = installPath
	status.Version = detectVersion(installPath)
	status.InstallAvailable = len(plan.installCmds) > 0 && !status.Installed

	switch {
	case status.Installed:
		status.State = "installed"
		switch {
		case status.Version != "" && status.InstallPath != "":
			status.Message = fmt.Sprintf("phpMyAdmin %s is installed at %s.", status.Version, status.InstallPath)
		case status.Version != "":
			status.Message = fmt.Sprintf("phpMyAdmin %s is installed.", status.Version)
		case status.InstallPath != "":
			status.Message = fmt.Sprintf("phpMyAdmin is installed at %s.", status.InstallPath)
		default:
			status.Message = "phpMyAdmin is installed."
		}
	case status.InstallAvailable:
		status.State = "missing"
		status.Message = "phpMyAdmin is not installed. Install it here to add a browser-based MariaDB client."
	default:
		status.State = "missing"
		status.Message = "phpMyAdmin is not installed on this server."
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic phpMyAdmin installation is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("installing phpmyadmin",
		zap.String("package_manager", plan.packageManager),
	)

	if err := runCommands(ctx, plan.installEnv, plan.installCmds...); err != nil {
		return err
	}
	if setup, ok := detectLinuxSetup(plan.packageManager); ok {
		if err := s.finalizeLinuxInstall(ctx, setup); err != nil {
			return err
		}
	}

	return nil
}

func detectActionPlan() actionPlan {
	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			return actionPlan{
				packageManager: "homebrew",
				installLabel:   "Install phpMyAdmin",
				installCmds: [][]string{
					{brewPath, "install", "phpmyadmin"},
				},
			}
		}
	case "linux":
		if os.Geteuid() == 0 {
			if aptPath, ok := lookupCommand("apt-get"); ok {
				return actionPlan{
					packageManager: "apt",
					installLabel:   "Install phpMyAdmin",
					installEnv: map[string]string{
						"DEBIAN_FRONTEND": "noninteractive",
					},
					installCmds: [][]string{
						{aptPath, "update"},
						{aptPath, "install", "-y", "phpmyadmin"},
					},
				}
			}
			if dnfPath, ok := lookupCommand("dnf"); ok {
				return actionPlan{
					packageManager: "dnf",
					installLabel:   "Install phpMyAdmin",
					installCmds: [][]string{
						{dnfPath, "install", "-y", "phpMyAdmin"},
					},
				}
			}
			if yumPath, ok := lookupCommand("yum"); ok {
				return actionPlan{
					packageManager: "yum",
					installLabel:   "Install phpMyAdmin",
					installCmds: [][]string{
						{yumPath, "install", "-y", "phpMyAdmin"},
					},
				}
			}
			if pacmanPath, ok := lookupCommand("pacman"); ok {
				return actionPlan{
					packageManager: "pacman",
					installLabel:   "Install phpMyAdmin",
					installCmds: [][]string{
						{pacmanPath, "-Sy", "--noconfirm", "phpmyadmin"},
					},
				}
			}
		}
	}

	return actionPlan{}
}

func detectInstallPath() (string, bool) {
	for _, candidate := range installPathCandidates() {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}

		return candidate, true
	}

	return "", false
}

func detectVersion(installPath string) string {
	installPath = strings.TrimSpace(installPath)
	if installPath == "" {
		return ""
	}

	if resolvedPath, err := filepath.EvalSymlinks(installPath); err == nil {
		if version := extractVersionFromPath(resolvedPath); version != "" {
			return version
		}
	}

	if version := extractVersionFromPath(installPath); version != "" {
		return version
	}

	for _, name := range []string{"composer.json", "package.json"} {
		version, err := readVersionFromJSON(filepath.Join(installPath, name))
		if err == nil && version != "" {
			return version
		}
	}

	return ""
}

func readVersionFromJSON(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return "", err
	}

	return strings.TrimSpace(payload.Version), nil
}

func extractVersionFromPath(path string) string {
	match := cellarVersionPattern.FindStringSubmatch(filepath.ToSlash(path))
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}

	return ""
}

func installPathCandidates() []string {
	if override := strings.TrimSpace(os.Getenv("FLOWPANEL_PHPMYADMIN_PATH")); override != "" {
		return []string{override}
	}

	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/share/phpmyadmin",
			"/usr/local/share/phpmyadmin",
		}
	default:
		return []string{
			"/usr/share/phpmyadmin",
			"/usr/share/phpMyAdmin",
			"/usr/share/webapps/phpmyadmin",
			"/usr/share/webapps/phpMyAdmin",
		}
	}
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

func runCommands(ctx context.Context, env map[string]string, commands ...[]string) error {
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}

		if _, err := runCommand(ctx, env, command[0], command[1:]...); err != nil {
			return err
		}
	}

	return nil
}

func runCommand(ctx context.Context, env map[string]string, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}

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

type linuxSetup struct {
	configPath string
	schemaPath string
}

func detectLinuxSetup(packageManager string) (linuxSetup, bool) {
	switch packageManager {
	case "apt":
		return linuxSetup{
			configPath: debianConfigUserPath,
			schemaPath: debianCreateTablesSQLPath,
		}, true
	case "dnf", "yum":
		return linuxSetup{
			configPath: rpmConfigUserPath,
			schemaPath: rpmCreateTablesSQLPath,
		}, true
	default:
		return linuxSetup{}, false
	}
}

func (s *Service) finalizeLinuxInstall(ctx context.Context, setup linuxSetup) error {
	tableConfig, err := readPHPMyAdminStorageTableConfig(setup.schemaPath)
	if err != nil {
		return fmt.Errorf("read phpMyAdmin storage table definitions: %w", err)
	}

	createTablesSQL, err := os.ReadFile(setup.schemaPath)
	if err != nil {
		return fmt.Errorf("read phpMyAdmin schema SQL: %w", err)
	}
	if _, err := runMariaDBCommand(ctx, string(createTablesSQL)); err != nil {
		return fmt.Errorf("create phpMyAdmin storage tables: %w", err)
	}

	controlPass, err := generateControlPassword()
	if err != nil {
		return fmt.Errorf("generate phpMyAdmin control password: %w", err)
	}
	if _, err := runMariaDBCommand(ctx, buildControlUserSQL(controlPass)); err != nil {
		return fmt.Errorf("configure phpMyAdmin control user: %w", err)
	}

	if err := writeManagedPHPMyAdminConfig(setup.configPath, renderManagedPHPMyAdminConfig(controlPass, tableConfig)); err != nil {
		return fmt.Errorf("write phpMyAdmin config: %w", err)
	}

	return nil
}

func readPHPMyAdminStorageTableConfig(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	matches := phpMyAdminStorageTablePattern.FindAllStringSubmatch(string(content), -1)
	config := make(map[string]string, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}

		tableName := strings.TrimSpace(match[1])
		optionName, ok := phpMyAdminStorageTableConfig[tableName]
		if !ok {
			continue
		}
		config[optionName] = tableName
	}

	return config, nil
}

func buildControlUserSQL(controlPass string) string {
	controlUserLiteral := quoteSQLLiteral("phpmyadmin")
	controlPassLiteral := quoteSQLLiteral(controlPass)

	return strings.Join([]string{
		fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s", controlUserLiteral, controlPassLiteral),
		fmt.Sprintf("ALTER USER %s@'localhost' IDENTIFIED BY %s", controlUserLiteral, controlPassLiteral),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `phpmyadmin`.* TO %s@'localhost'", controlUserLiteral),
		"FLUSH PRIVILEGES",
	}, "; ")
}

func renderManagedPHPMyAdminConfig(controlPass string, tableConfig map[string]string) string {
	var builder strings.Builder
	builder.WriteString(phpMyAdminConfigStartMarker)
	builder.WriteString("\n")
	builder.WriteString("if (!isset($cfg, $i) || !isset($cfg['Servers'][$i])) {\n")
	builder.WriteString("    return;\n")
	builder.WriteString("}\n")
	builder.WriteString("$cfg['Servers'][$i]['pmadb'] = 'phpmyadmin';\n")
	builder.WriteString("$cfg['Servers'][$i]['controluser'] = 'phpmyadmin';\n")
	builder.WriteString("$cfg['Servers'][$i]['controlpass'] = ")
	builder.WriteString(quotePHPString(controlPass))
	builder.WriteString(";\n")

	keys := make([]string, 0, len(tableConfig))
	for key := range tableConfig {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString("$cfg['Servers'][$i]['")
		builder.WriteString(key)
		builder.WriteString("'] = '")
		builder.WriteString(tableConfig[key])
		builder.WriteString("';\n")
	}

	builder.WriteString(phpMyAdminConfigEndMarker)
	builder.WriteString("\n")

	return builder.String()
}

func writeManagedPHPMyAdminConfig(path string, managedBlock string) error {
	var existing string
	mode := os.FileMode(0o644)
	hadClosingTag := false

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	info, err := os.Stat(path)
	switch {
	case err == nil:
		mode = info.Mode().Perm()
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		existing = string(content)
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}

	trimmedExisting := strings.TrimSpace(existing)
	if strings.HasSuffix(trimmedExisting, "?>") {
		hadClosingTag = true
		existing = strings.TrimRight(strings.TrimSuffix(existing, "?>"), "\n")
	}

	updated := upsertManagedPHPMyAdminConfig(existing, managedBlock)
	if !strings.Contains(updated, "<?php") {
		updated = "<?php\n" + strings.TrimLeft(updated, "\n")
	}
	if hadClosingTag {
		updated = strings.TrimRight(updated, "\n") + "\n?>\n"
	}

	return os.WriteFile(path, []byte(updated), mode)
}

func upsertManagedPHPMyAdminConfig(existing, managedBlock string) string {
	managedBlock = strings.TrimRight(managedBlock, "\n") + "\n"

	start := strings.Index(existing, phpMyAdminConfigStartMarker)
	end := strings.Index(existing, phpMyAdminConfigEndMarker)
	if start >= 0 && end >= start {
		end += len(phpMyAdminConfigEndMarker)
		prefix := strings.TrimRight(existing[:start], "\n")
		suffix := strings.TrimLeft(existing[end:], "\n")

		switch {
		case prefix == "" && suffix == "":
			return managedBlock
		case prefix == "":
			return managedBlock + "\n" + suffix
		case suffix == "":
			return prefix + "\n\n" + managedBlock
		default:
			return prefix + "\n\n" + managedBlock + "\n" + suffix
		}
	}

	existing = strings.TrimRight(existing, "\n")
	if existing == "" {
		return managedBlock
	}

	return existing + "\n\n" + managedBlock
}

func runMariaDBCommand(ctx context.Context, query string) (string, error) {
	clientPath, ok := lookupCommand("mariadb")
	if !ok {
		clientPath, ok = lookupCommand("mysql")
	}
	if !ok {
		return "", errors.New("mariadb/mysql client is not installed")
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	args := []string{
		"--batch",
		"--raw",
		"--skip-column-names",
		fmt.Sprintf("--user=%s", strings.TrimSpace(envWithDefault("FLOWPANEL_MARIADB_USER", mariaDBRootUser))),
	}
	if socket := resolveMariaDBSocket(); socket != "" {
		args = append(args, "--protocol=socket", fmt.Sprintf("--socket=%s", socket))
	} else {
		host := strings.TrimSpace(envWithDefault("FLOWPANEL_MARIADB_HOST", "127.0.0.1"))
		port := strings.TrimSpace(envWithDefault("FLOWPANEL_MARIADB_PORT", "3306"))
		args = append(args, "--protocol=tcp", fmt.Sprintf("--host=%s", host), fmt.Sprintf("--port=%s", port))
	}
	args = append(args, "--execute", query)

	env := map[string]string{}
	if password, ok := resolveMariaDBPassword(); ok {
		env["MYSQL_PWD"] = password
	}

	return runCommand(runCtx, env, clientPath, args...)
}

func resolveMariaDBSocket() string {
	if socket := strings.TrimSpace(os.Getenv("FLOWPANEL_MARIADB_SOCKET")); socket != "" {
		return socket
	}

	for _, candidate := range mariaDBSocketCandidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		return candidate
	}

	return ""
}

func resolveMariaDBPassword() (string, bool) {
	if password, ok := os.LookupEnv("FLOWPANEL_MARIADB_PASSWORD"); ok {
		password = strings.TrimSpace(password)
		if password != "" {
			return password, true
		}
	}

	path := strings.TrimSpace(os.Getenv("FLOWPANEL_MARIADB_PASSWORD_FILE"))
	if path == "" {
		if dbPath := strings.TrimSpace(os.Getenv("FLOWPANEL_DB_PATH")); dbPath != "" && dbPath != ":memory:" {
			path = filepath.Join(filepath.Dir(dbPath), defaultMariaDBPasswordFile)
		} else {
			path = filepath.Join(".", "data", defaultMariaDBPasswordFile)
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	password := strings.TrimSpace(string(content))
	if password == "" {
		return "", false
	}

	return password, true
}

func generateControlPassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func quoteSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quotePHPString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "'", "\\'")
	return "'" + value + "'"
}

func envWithDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
