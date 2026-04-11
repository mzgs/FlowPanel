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
)

const composerActionTimeout = 10 * time.Minute

var (
	errComposerUnsupportedDomain = errors.New("composer is not available for this domain")
	errComposerMissingManifest   = errors.New("composer.json was not found for this domain")
	errComposerUnavailable       = errors.New("composer is not installed on this server")
)

func runDomainComposerAction(
	ctx context.Context,
	domains *domain.Service,
	hostname string,
	action string,
) (domain.Record, error) {
	record, ok := domains.FindByHostname(hostname)
	if !ok {
		return domain.Record{}, domain.ErrNotFound
	}
	if !domain.SupportsManagedDocumentRoot(record.Kind) {
		return domain.Record{}, errComposerUnsupportedDomain
	}
	if action != "install" && action != "update" {
		return domain.Record{}, fmt.Errorf("unsupported composer action %q", action)
	}

	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return domain.Record{}, fmt.Errorf("resolve domain document root: %w", err)
	}

	manifestPath := filepath.Join(targetPath, "composer.json")
	if _, err := os.Stat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Record{}, errComposerMissingManifest
		}
		return domain.Record{}, fmt.Errorf("inspect composer manifest: %w", err)
	}

	composerPath, err := exec.LookPath("composer")
	if err != nil {
		return domain.Record{}, errComposerUnavailable
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, composerActionTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, composerPath, action, "--no-interaction", "--no-progress")
	cmd.Dir = targetPath
	cmd.Env = append(os.Environ(), "COMPOSER_ALLOW_SUPERUSER=1")

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return domain.Record{}, fmt.Errorf("composer %s timed out", action)
		case errors.Is(runCtx.Err(), context.Canceled):
			return domain.Record{}, fmt.Errorf("composer %s was canceled", action)
		case message != "":
			return domain.Record{}, fmt.Errorf("composer %s failed: %s", action, message)
		default:
			return domain.Record{}, fmt.Errorf("composer %s failed: %w", action, err)
		}
	}

	return record, nil
}
