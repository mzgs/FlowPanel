package phpenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var phpSizeSettingPattern = regexp.MustCompile(`^(?:\d+[KMGkmg]?|-1)$`)

const phpErrorAllMask = 32767

var phpErrorReportingConstants = []struct {
	name  string
	value int
}{
	{name: "E_ERROR", value: 1},
	{name: "E_WARNING", value: 2},
	{name: "E_PARSE", value: 4},
	{name: "E_NOTICE", value: 8},
	{name: "E_CORE_ERROR", value: 16},
	{name: "E_CORE_WARNING", value: 32},
	{name: "E_COMPILE_ERROR", value: 64},
	{name: "E_COMPILE_WARNING", value: 128},
	{name: "E_USER_ERROR", value: 256},
	{name: "E_USER_WARNING", value: 512},
	{name: "E_USER_NOTICE", value: 1024},
	{name: "E_STRICT", value: 2048},
	{name: "E_RECOVERABLE_ERROR", value: 4096},
	{name: "E_DEPRECATED", value: 8192},
	{name: "E_USER_DEPRECATED", value: 16384},
}

type phpConfigInfo struct {
	loadedConfigFile  string
	scanDir           string
	managedConfigFile string
	extensionDir      string
	settings          Settings
}

func (s *Service) UpdateSettings(ctx context.Context, input UpdateSettingsInput) (Status, error) {
	runtimeStatus, err := s.UpdateSettingsForVersion(ctx, "", input)
	if err != nil {
		return Status{}, err
	}

	status := s.Status(ctx)
	if status.ManagedConfigFile == "" {
		status.ManagedConfigFile = runtimeStatus.ManagedConfigFile
	}
	return status, nil
}

func (s *Service) UpdateSettingsForVersion(ctx context.Context, version string, input UpdateSettingsInput) (RuntimeStatus, error) {
	validation := ValidateUpdateSettingsInput(input)
	if len(validation) > 0 {
		return RuntimeStatus{}, validation
	}

	status := s.StatusForVersion(ctx, version)
	if !status.PHPInstalled || status.PHPPath == "" {
		return RuntimeStatus{}, fmt.Errorf("php %s is not installed", status.Version)
	}

	configInfo, err := inspectPHPConfig(ctx, status.PHPPath)
	if err != nil {
		return RuntimeStatus{}, err
	}

	if configInfo.managedConfigFile == "" {
		return RuntimeStatus{}, fmt.Errorf("flowpanel could not determine where to write PHP settings")
	}

	normalized := NormalizeUpdateSettingsInput(input)
	if err := os.MkdirAll(filepath.Dir(configInfo.managedConfigFile), 0o755); err != nil {
		return RuntimeStatus{}, fmt.Errorf("create php config directory: %w", err)
	}
	if err := os.WriteFile(configInfo.managedConfigFile, []byte(renderManagedPHPConfig(normalized)), 0o644); err != nil {
		return RuntimeStatus{}, fmt.Errorf("write php settings: %w", err)
	}

	if status.ServiceRunning {
		if err := s.RestartVersion(ctx, status.Version); err != nil {
			if status.FPMPath == "" {
				return RuntimeStatus{}, fmt.Errorf("php settings saved but failed to restart php-fpm: %w", err)
			}
			if fallbackErr := restartPHPFPM(ctx, status.FPMPath); fallbackErr != nil {
				return RuntimeStatus{}, fmt.Errorf("php settings saved but failed to restart php-fpm: %w", err)
			}
		}
	}

	nextStatus := s.StatusForVersion(ctx, status.Version)
	if nextStatus.ManagedConfigFile == "" {
		nextStatus.ManagedConfigFile = configInfo.managedConfigFile
	}
	return nextStatus, nil
}

func ValidateUpdateSettingsInput(input UpdateSettingsInput) ValidationErrors {
	validation := ValidationErrors{}

	if !isValidPHPInteger(input.MaxExecutionTime, false) {
		validation["max_execution_time"] = "Enter a whole number. Use 0 for unlimited."
	}
	if !isValidPHPInteger(input.MaxInputTime, true) {
		validation["max_input_time"] = "Enter a whole number. Use -1 for unlimited."
	}
	if !isValidPHPSize(input.MemoryLimit, true) {
		validation["memory_limit"] = "Use a value like 256M, 1G, or -1."
	}
	if !isValidPHPSize(input.PostMaxSize, false) {
		validation["post_max_size"] = "Use a value like 64M or 1G."
	}
	if !isValidPHPOnOff(input.FileUploads) {
		validation["file_uploads"] = "Choose On or Off."
	}
	if !isValidPHPSize(input.UploadMaxFilesize, false) {
		validation["upload_max_filesize"] = "Use a value like 64M or 1G."
	}
	if !isValidPHPInteger(input.MaxFileUploads, false) {
		validation["max_file_uploads"] = "Enter a whole number."
	}
	if !isValidPHPInteger(input.DefaultSocketTimeout, false) {
		validation["default_socket_timeout"] = "Enter a whole number."
	}
	if strings.TrimSpace(input.ErrorReporting) == "" {
		validation["error_reporting"] = "Error reporting is required."
	}
	if !isValidPHPOnOff(input.DisplayErrors) {
		validation["display_errors"] = "Choose On or Off."
	}

	return validation
}

func NormalizeUpdateSettingsInput(input UpdateSettingsInput) Settings {
	return Settings{
		MaxExecutionTime:     normalizePHPInteger(input.MaxExecutionTime),
		MaxInputTime:         normalizePHPInteger(input.MaxInputTime),
		MemoryLimit:          normalizePHPSize(input.MemoryLimit),
		PostMaxSize:          normalizePHPSize(input.PostMaxSize),
		FileUploads:          normalizePHPOnOff(input.FileUploads),
		UploadMaxFilesize:    normalizePHPSize(input.UploadMaxFilesize),
		MaxFileUploads:       normalizePHPInteger(input.MaxFileUploads),
		DefaultSocketTimeout: normalizePHPInteger(input.DefaultSocketTimeout),
		ErrorReporting:       strings.TrimSpace(input.ErrorReporting),
		DisplayErrors:        normalizePHPOnOff(input.DisplayErrors),
	}
}

func isValidPHPInteger(value string, allowNegativeOne bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if value == "-1" {
		return allowNegativeOne
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func isValidPHPSize(value string, allowNegativeOne bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if value == "-1" {
		return allowNegativeOne
	}
	return phpSizeSettingPattern.MatchString(value)
}

func normalizePHPInteger(value string) string {
	return strings.TrimSpace(value)
}

func normalizePHPSize(value string) string {
	value = strings.TrimSpace(value)
	if value == "-1" {
		return value
	}
	if len(value) <= 1 {
		return strings.ToUpper(value)
	}
	return value[:len(value)-1] + strings.ToUpper(value[len(value)-1:])
}

func isValidPHPOnOff(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "off", "1", "0", "true", "false":
		return true
	default:
		return false
	}
}

func normalizePHPOnOff(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "1", "true":
		return "On"
	default:
		return "Off"
	}
}

func renderManagedPHPConfig(settings Settings) string {
	var builder strings.Builder
	builder.WriteString("; Managed by FlowPanel.\n")
	builder.WriteString("; Manual edits may be overwritten.\n")
	builder.WriteString(fmt.Sprintf("max_execution_time = %s\n", settings.MaxExecutionTime))
	builder.WriteString(fmt.Sprintf("max_input_time = %s\n", settings.MaxInputTime))
	builder.WriteString(fmt.Sprintf("memory_limit = %s\n", settings.MemoryLimit))
	builder.WriteString(fmt.Sprintf("post_max_size = %s\n", settings.PostMaxSize))
	builder.WriteString(fmt.Sprintf("file_uploads = %s\n", settings.FileUploads))
	builder.WriteString(fmt.Sprintf("upload_max_filesize = %s\n", settings.UploadMaxFilesize))
	builder.WriteString(fmt.Sprintf("max_file_uploads = %s\n", settings.MaxFileUploads))
	builder.WriteString(fmt.Sprintf("default_socket_timeout = %s\n", settings.DefaultSocketTimeout))
	builder.WriteString(fmt.Sprintf("error_reporting = %s\n", settings.ErrorReporting))
	builder.WriteString(fmt.Sprintf("display_errors = %s\n", settings.DisplayErrors))
	return builder.String()
}

func inspectPHPConfig(ctx context.Context, phpPath string) (phpConfigInfo, error) {
	info := phpConfigInfo{}

	iniOutput, err := runInspectCommand(ctx, phpPath, "--ini")
	if err != nil {
		return info, fmt.Errorf("inspect php config: %w", err)
	}

	info.loadedConfigFile = parsePHPIniOutputValue(iniOutput, "Loaded Configuration File")
	info.scanDir = parsePHPIniOutputValue(iniOutput, "Scan for additional .ini files in")
	info.managedConfigFile = determineManagedPHPConfigFile(info.loadedConfigFile, info.scanDir)

	extensionDir, err := runInspectCommand(ctx, phpPath, "-n", "-r", `echo ini_get("extension_dir");`)
	if err != nil {
		return info, fmt.Errorf("inspect php extension_dir: %w", err)
	}
	info.extensionDir = strings.TrimSpace(extensionDir)

	settingsOutput, err := runInspectCommand(ctx, phpPath, "-r", `echo json_encode([
  "max_execution_time" => ini_get("max_execution_time"),
  "max_input_time" => ini_get("max_input_time"),
  "memory_limit" => ini_get("memory_limit"),
  "post_max_size" => ini_get("post_max_size"),
  "file_uploads" => filter_var(ini_get("file_uploads"), FILTER_VALIDATE_BOOLEAN) ? "On" : "Off",
  "upload_max_filesize" => ini_get("upload_max_filesize"),
  "max_file_uploads" => ini_get("max_file_uploads"),
  "default_socket_timeout" => ini_get("default_socket_timeout"),
  "error_reporting" => ini_get("error_reporting"),
  "display_errors" => filter_var(ini_get("display_errors"), FILTER_VALIDATE_BOOLEAN) ? "On" : "Off",
], JSON_UNESCAPED_SLASHES);`)
	if err != nil {
		return info, fmt.Errorf("inspect php ini settings: %w", err)
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(settingsOutput), &payload); err != nil {
		return info, fmt.Errorf("decode php ini settings: %w", err)
	}

	info.settings = Settings{
		MaxExecutionTime:     payload["max_execution_time"],
		MaxInputTime:         payload["max_input_time"],
		MemoryLimit:          payload["memory_limit"],
		PostMaxSize:          payload["post_max_size"],
		FileUploads:          payload["file_uploads"],
		UploadMaxFilesize:    payload["upload_max_filesize"],
		MaxFileUploads:       payload["max_file_uploads"],
		DefaultSocketTimeout: payload["default_socket_timeout"],
		ErrorReporting:       normalizePHPErrorReportingValue(payload["error_reporting"]),
		DisplayErrors:        payload["display_errors"],
	}

	if managed, err := parseManagedPHPConfig(info.managedConfigFile); err == nil {
		info.settings = mergeManagedPHPSettings(info.settings, managed)
	}

	return info, nil
}

func parseManagedPHPConfig(path string) (Settings, error) {
	if strings.TrimSpace(path) == "" {
		return Settings{}, os.ErrNotExist
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}

	settings := Settings{}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "max_execution_time":
			settings.MaxExecutionTime = value
		case "max_input_time":
			settings.MaxInputTime = value
		case "memory_limit":
			settings.MemoryLimit = value
		case "post_max_size":
			settings.PostMaxSize = value
		case "file_uploads":
			settings.FileUploads = value
		case "upload_max_filesize":
			settings.UploadMaxFilesize = value
		case "max_file_uploads":
			settings.MaxFileUploads = value
		case "default_socket_timeout":
			settings.DefaultSocketTimeout = value
		case "error_reporting":
			settings.ErrorReporting = value
		case "display_errors":
			settings.DisplayErrors = value
		}
	}

	return settings, nil
}

func mergeManagedPHPSettings(base, managed Settings) Settings {
	if managed.MaxExecutionTime != "" {
		base.MaxExecutionTime = managed.MaxExecutionTime
	}
	if managed.MaxInputTime != "" {
		base.MaxInputTime = managed.MaxInputTime
	}
	if managed.MemoryLimit != "" {
		base.MemoryLimit = managed.MemoryLimit
	}
	if managed.PostMaxSize != "" {
		base.PostMaxSize = managed.PostMaxSize
	}
	if managed.FileUploads != "" {
		base.FileUploads = managed.FileUploads
	}
	if managed.UploadMaxFilesize != "" {
		base.UploadMaxFilesize = managed.UploadMaxFilesize
	}
	if managed.MaxFileUploads != "" {
		base.MaxFileUploads = managed.MaxFileUploads
	}
	if managed.DefaultSocketTimeout != "" {
		base.DefaultSocketTimeout = managed.DefaultSocketTimeout
	}
	if managed.ErrorReporting != "" {
		base.ErrorReporting = managed.ErrorReporting
	}
	if managed.DisplayErrors != "" {
		base.DisplayErrors = managed.DisplayErrors
	}

	return base
}

func normalizePHPErrorReportingValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}

	mask, err := strconv.Atoi(value)
	if err != nil {
		return value
	}

	if mask == 0 {
		return "0"
	}
	if mask == phpErrorAllMask {
		return "E_ALL"
	}
	if mask < 0 || mask > phpErrorAllMask {
		return value
	}

	included := make([]string, 0, len(phpErrorReportingConstants))
	excluded := make([]string, 0, len(phpErrorReportingConstants))
	includedMask := 0

	for _, item := range phpErrorReportingConstants {
		if mask&item.value == item.value {
			included = append(included, item.name)
			includedMask |= item.value
		} else if phpErrorAllMask&item.value == item.value {
			excluded = append(excluded, item.name)
		}
	}

	if includedMask != mask {
		return value
	}
	if len(excluded) > 0 && len(excluded) <= len(included) {
		return "E_ALL & ~" + strings.Join(excluded, " & ~")
	}
	if len(included) > 0 {
		return strings.Join(included, " | ")
	}

	return value
}

func parsePHPIniOutputValue(output, label string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, label+":") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, label+":"))
		switch value {
		case "", "(none)":
			return ""
		default:
			return value
		}
	}

	return ""
}

func determineManagedPHPConfigFile(loadedConfigFile, scanDir string) string {
	if scanDir != "" {
		return filepath.Join(scanDir, "99-flowpanel.ini")
	}
	if loadedConfigFile != "" {
		return filepath.Join(filepath.Dir(loadedConfigFile), "99-flowpanel.ini")
	}
	return ""
}

func restartPHPFPM(ctx context.Context, fpmPath string) error {
	return runPHPFPMServiceCommand(ctx, fpmPath, "restart")
}

func runPHPFPMServiceCommand(ctx context.Context, fpmPath, action string) error {
	action = strings.TrimSpace(action)
	if action == "" {
		return fmt.Errorf("php-fpm service action is required")
	}

	candidates := fpmServiceCandidates(fpmPath)
	if systemctlPath, ok := lookupCommand("systemctl"); ok {
		for _, candidate := range candidates {
			if _, err := runCommand(ctx, systemctlPath, action, candidate); err == nil {
				return nil
			}
		}
	}
	if servicePath, ok := lookupCommand("service"); ok {
		for _, candidate := range candidates {
			if _, err := runCommand(ctx, servicePath, candidate, action); err == nil {
				return nil
			}
		}
	}

	if len(candidates) == 0 {
		return fmt.Errorf("no php-fpm service name could be determined")
	}
	return fmt.Errorf("automatic php-fpm %s is not supported for %s", action, strings.Join(candidates, ", "))
}

func fpmServiceCandidates(fpmPath string) []string {
	base := strings.TrimSpace(filepath.Base(fpmPath))
	candidates := []string{}
	hasVersionSpecificCandidate := false
	if base != "" {
		candidates = append(candidates, base)
	}
	if matches := regexp.MustCompile(`^php-fpm(\d+(?:\.\d+)?)$`).FindStringSubmatch(base); len(matches) == 2 {
		candidates = append([]string{"php" + matches[1] + "-fpm"}, candidates...)
		hasVersionSpecificCandidate = true
	}
	if matches := regexp.MustCompile(`^php(\d+(?:\.\d+)?)\-fpm$`).FindStringSubmatch(base); len(matches) == 2 {
		candidates = append(candidates, "php-fpm"+matches[1])
		hasVersionSpecificCandidate = true
	}
	if matches := regexp.MustCompile(`/php(\d+)/root/usr/sbin/php-fpm$`).FindStringSubmatch(filepath.ToSlash(strings.TrimSpace(fpmPath))); len(matches) == 2 {
		candidates = append([]string{"php" + matches[1] + "-php-fpm"}, candidates...)
		hasVersionSpecificCandidate = true
	}
	if matches := regexp.MustCompile(`/php@(\d+(?:\.\d+)?)/`).FindStringSubmatch(filepath.ToSlash(strings.TrimSpace(fpmPath))); len(matches) == 2 {
		candidates = append(candidates, "php@"+matches[1])
		hasVersionSpecificCandidate = true
	}
	if !hasVersionSpecificCandidate && len(candidates) == 0 {
		candidates = append(candidates, "php-fpm", "php")
	} else if !hasVersionSpecificCandidate && len(candidates) > 0 {
		candidates = append(candidates, "php")
	}

	deduped := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		deduped = append(deduped, candidate)
	}

	return deduped
}
