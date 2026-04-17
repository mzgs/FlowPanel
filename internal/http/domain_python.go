package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"flowpanel/internal/domain"
)

const domainPythonRequirementsInstallTimeout = 15 * time.Minute
const domainPythonEnvironmentPrepareTimeout = 20 * time.Minute
const domainPythonDefaultVirtualEnvName = ".venv"

var (
	errDomainPythonRequirementsUnsupportedDomain = errors.New("install requirements is available only for Python domains")
	errDomainPythonRequirementsMissingFile       = errors.New("requirements.txt was not found for this domain")
)

var domainPythonVirtualEnvNames = []string{".venv", "venv", "env"}

func runDomainPythonRequirementsInstall(
	ctx context.Context,
	domains *domain.Service,
	hostname string,
) (domain.Record, error) {
	record, targetPath, err := loadPythonDomainInstallTarget(domains, hostname)
	if err != nil {
		return domain.Record{}, err
	}

	interpreterPath, err := ensureDomainPythonVirtualEnv(ctx, domains, record, targetPath)
	if err != nil {
		return domain.Record{}, err
	}
	if err := installDomainPythonRequirements(ctx, interpreterPath, targetPath); err != nil {
		return domain.Record{}, err
	}

	return record, nil
}

func ensureDomainPythonEnvironment(
	ctx context.Context,
	domains *domain.Service,
	record domain.Record,
) error {
	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return fmt.Errorf("resolve domain document root: %w", err)
	}

	interpreterPath, err := ensureDomainPythonVirtualEnv(ctx, domains, record, targetPath)
	if err != nil {
		return err
	}
	if !domainPythonRequirementsFileExists(targetPath) {
		return nil
	}

	return installDomainPythonRequirements(ctx, interpreterPath, targetPath)
}

func loadPythonDomainInstallTarget(domains *domain.Service, hostname string) (domain.Record, string, error) {
	record, ok := domains.FindByHostname(hostname)
	if !ok {
		return domain.Record{}, "", domain.ErrNotFound
	}
	if record.Kind != domain.KindPython {
		return domain.Record{}, "", errDomainPythonRequirementsUnsupportedDomain
	}

	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return domain.Record{}, "", fmt.Errorf("resolve domain document root: %w", err)
	}

	return record, targetPath, nil
}

func ensureDomainPythonVirtualEnv(
	ctx context.Context,
	domains *domain.Service,
	record domain.Record,
	targetPath string,
) (string, error) {
	if interpreterPath, ok := findExistingDomainPythonVirtualEnvInterpreter(targetPath); ok {
		return interpreterPath, nil
	}

	baseInterpreterPath, err := domain.ResolvePythonInterpreter(domains.BasePath(), record)
	if err != nil {
		return "", fmt.Errorf("resolve python interpreter: %w", err)
	}

	runCtx, cancel := prepareDomainPythonContext(ctx, domainPythonEnvironmentPrepareTimeout)
	defer cancel()

	if _, err := runDomainPythonCommand(
		runCtx,
		targetPath,
		baseInterpreterPath,
		"create virtual environment",
		"create virtual environment timed out",
		"create virtual environment was canceled",
		"-m",
		"venv",
		domainPythonDefaultVirtualEnvName,
	); err != nil {
		return "", err
	}

	interpreterPath := domainPythonVirtualEnvInterpreterPath(targetPath, domainPythonDefaultVirtualEnvName)
	if _, err := os.Stat(interpreterPath); err != nil {
		return "", fmt.Errorf("inspect created python virtual environment: %w", err)
	}

	return interpreterPath, nil
}

func installDomainPythonRequirements(
	ctx context.Context,
	interpreterPath string,
	targetPath string,
) error {
	if !domainPythonRequirementsFileExists(targetPath) {
		return errDomainPythonRequirementsMissingFile
	}

	runCtx, cancel := prepareDomainPythonContext(ctx, domainPythonRequirementsInstallTimeout)
	defer cancel()

	_, err := runDomainPythonCommand(
		runCtx,
		targetPath,
		interpreterPath,
		"install requirements",
		"install requirements timed out",
		"install requirements was canceled",
		"-m",
		"pip",
		"install",
		"-r",
		"requirements.txt",
	)
	return err
}

func domainPythonRequirementsFileExists(targetPath string) bool {
	info, err := os.Stat(filepath.Join(targetPath, "requirements.txt"))
	return err == nil && !info.IsDir()
}

func findExistingDomainPythonVirtualEnvInterpreter(targetPath string) (string, bool) {
	for _, name := range domainPythonVirtualEnvNames {
		interpreterPath := domainPythonVirtualEnvInterpreterPath(targetPath, name)
		info, err := os.Stat(interpreterPath)
		if err == nil && !info.IsDir() {
			return interpreterPath, true
		}
	}

	return "", false
}

func domainPythonVirtualEnvInterpreterPath(targetPath string, name string) string {
	parts := []string{targetPath, name}
	if runtime.GOOS == "windows" {
		parts = append(parts, "Scripts", "python.exe")
	} else {
		parts = append(parts, "bin", "python")
	}

	return filepath.Join(parts...)
}

func prepareDomainPythonContext(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); ok {
		return runCtx, func() {}
	}

	return context.WithTimeout(runCtx, timeout)
}

func runDomainPythonCommand(
	ctx context.Context,
	targetPath string,
	interpreterPath string,
	action string,
	timeoutMessage string,
	canceledMessage string,
	args ...string,
) (string, error) {
	cmd := exec.CommandContext(ctx, interpreterPath, args...)
	cmd.Dir = targetPath
	cmd.Env = os.Environ()

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return "", errors.New(timeoutMessage)
		case errors.Is(ctx.Err(), context.Canceled):
			return "", errors.New(canceledMessage)
		case message != "":
			return "", fmt.Errorf("%s failed: %s", action, message)
		default:
			return "", fmt.Errorf("%s failed: %w", action, err)
		}
	}

	return strings.TrimSpace(output.String()), nil
}
