package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"flowpanel/internal/domain"
	"flowpanel/internal/nodejs"
)

const domainNPMInstallTimeout = 15 * time.Minute

var (
	errDomainNPMUnsupportedDomain = errors.New("npm install is available only for Node.js domains")
	errDomainNPMMissingManifest   = errors.New("package.json was not found for this domain")
	errDomainNPMUnavailable       = errors.New("npm is not installed on this server")
)

func runDomainNPMInstall(
	ctx context.Context,
	domains *domain.Service,
	nodeJS nodejs.Manager,
	hostname string,
) (domain.Record, error) {
	record, ok := domains.FindByHostname(hostname)
	if !ok {
		return domain.Record{}, domain.ErrNotFound
	}
	if record.Kind != domain.KindNodeJS {
		return domain.Record{}, errDomainNPMUnsupportedDomain
	}

	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return domain.Record{}, fmt.Errorf("resolve domain document root: %w", err)
	}

	manifestPath := filepath.Join(targetPath, "package.json")
	if _, err := os.Stat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Record{}, errDomainNPMMissingManifest
		}
		return domain.Record{}, fmt.Errorf("inspect package.json: %w", err)
	}

	if nodeJS == nil {
		return domain.Record{}, errDomainNPMUnavailable
	}
	npmPath := strings.TrimSpace(nodeJS.Status(ctx).NPMPath)
	if npmPath == "" {
		return domain.Record{}, errDomainNPMUnavailable
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, domainNPMInstallTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, npmPath, "install")
	cmd.Dir = targetPath
	cmd.Env = os.Environ()

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return domain.Record{}, errors.New("npm install timed out")
		case errors.Is(runCtx.Err(), context.Canceled):
			return domain.Record{}, errors.New("npm install was canceled")
		case message != "":
			return domain.Record{}, fmt.Errorf("npm install failed: %s", message)
		default:
			return domain.Record{}, fmt.Errorf("npm install failed: %w", err)
		}
	}

	return record, nil
}

func ensureDomainNodeModules(
	ctx context.Context,
	domains *domain.Service,
	nodeJS nodejs.Manager,
	record domain.Record,
) error {
	if record.Kind != domain.KindNodeJS {
		return nil
	}

	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return fmt.Errorf("resolve domain document root: %w", err)
	}

	nodeModulesPath := filepath.Join(targetPath, "node_modules")
	if _, err := os.Stat(nodeModulesPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect node_modules: %w", err)
	}

	manifestPath := filepath.Join(targetPath, "package.json")
	if _, err := os.Stat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect package.json: %w", err)
	}

	_, err = runDomainNPMInstall(ctx, domains, nodeJS, record.Hostname)
	return err
}
