package httpx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"flowpanel/internal/config"
	"flowpanel/internal/domain"
	"flowpanel/internal/executil"
	"flowpanel/internal/pm2"
)

const domainApplicationBuildTimeout = 15 * time.Minute

func (a *apiRoutes) ensureDomainApplicationPrerequisites(ctx context.Context) error {
	if a.app.PM2 == nil {
		return errors.New("PM2 runtime is not configured")
	}
	if a.app.PM2.Status(ctx).Installed {
		return nil
	}
	if a.app.NodeJS != nil && !a.app.NodeJS.Status(ctx).Installed {
		if err := a.app.NodeJS.Install(ctx); err != nil {
			return fmt.Errorf("install Node.js for PM2: %w", err)
		}
	}
	if err := a.app.PM2.Install(ctx); err != nil {
		return fmt.Errorf("install PM2: %w", err)
	}
	return nil
}

func (a *apiRoutes) ensureDomainApplicationBinary(ctx context.Context, record domain.Record) error {
	if record.Kind != domain.KindApplication {
		return nil
	}
	if err := a.ensureDomainApplicationPrerequisites(ctx); err != nil {
		return err
	}

	root, err := domain.ResolveDocumentRoot(a.app.Domains.BasePath(), record)
	if err != nil {
		return err
	}
	binaryPath, err := domain.ResolveAppBinaryPath(a.app.Domains.BasePath(), record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return fmt.Errorf("create executable directory: %w", err)
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, domainApplicationBuildTimeout)
		defer cancel()
	}

	commandName, commandArgs := gitHubShellCommand(record.AppBuildCommand)
	cmd := exec.CommandContext(runCtx, commandName, commandArgs...)
	cmd.Dir = root
	buildCachePath := filepath.Join(config.CachePath(), "applications")
	env := os.Environ()
	for key, path := range map[string]string{
		"HOME":                 filepath.Join(buildCachePath, "home"),
		"XDG_CACHE_HOME":       filepath.Join(buildCachePath, "cache"),
		"GOPATH":               filepath.Join(buildCachePath, "go"),
		"GOMODCACHE":           filepath.Join(buildCachePath, "go", "pkg", "mod"),
		"GOCACHE":              filepath.Join(buildCachePath, "go-build"),
		"CARGO_HOME":           filepath.Join(buildCachePath, "cargo"),
		"ZIG_GLOBAL_CACHE_DIR": filepath.Join(buildCachePath, "zig"),
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create application build cache %q: %w", path, err)
		}
		env = setCommandEnvironmentValue(env, key, path)
	}
	cmd.Env = env
	output := executil.NewTailBuffer(executil.DefaultOutputLimit)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return errors.New("application build timed out")
		case errors.Is(runCtx.Err(), context.Canceled):
			return errors.New("application build was canceled")
		case message != "":
			return fmt.Errorf("application build failed: %s", message)
		default:
			return fmt.Errorf("application build failed: %w", err)
		}
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("build completed but executable %q was not created", record.AppBinaryPath)
		}
		return fmt.Errorf("inspect built executable: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("configured executable %q is a directory", record.AppBinaryPath)
	}
	if err := os.Chmod(binaryPath, info.Mode().Perm()|0o755); err != nil {
		return fmt.Errorf("make application executable: %w", err)
	}
	return nil
}

func (a *apiRoutes) deployDomainApplication(ctx context.Context, record domain.Record) error {
	if record.Kind != domain.KindApplication {
		return nil
	}
	if err := a.ensureDomainApplicationBinary(ctx, record); err != nil {
		return err
	}
	config, err := resolveDomainRuntimeProcessConfig(a.app.Domains.BasePath(), record)
	if err != nil {
		return err
	}
	processes, err := a.app.PM2.List(ctx)
	if err != nil {
		return fmt.Errorf("list PM2 processes: %w", err)
	}
	if process, ok := matchDomainNodeJSProcess(processes, config); ok {
		if canStopDomainNodeJSProcess(process) {
			_, err = a.app.PM2.RestartProcess(ctx, process.ID)
		}
		return err
	}
	_, err = a.app.PM2.CreateProcess(ctx, pm2.CreateProcessInput{
		Name:             record.Hostname,
		ScriptPath:       config.ScriptPath,
		WorkingDirectory: config.WorkingDirectory,
		Interpreter:      config.InterpreterPath,
		Environment:      domainRuntimeEnvironment(record),
	})
	return err
}
