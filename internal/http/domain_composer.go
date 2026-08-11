package httpx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"flowpanel/internal/domain"
	"flowpanel/internal/executil"
	"flowpanel/internal/phpenv"
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
	php phpenv.Manager,
	hostname string,
	action string,
) (domain.Record, bool, error) {
	record, ok := domains.FindByHostname(hostname)
	if !ok {
		return domain.Record{}, false, domain.ErrNotFound
	}
	if !domain.SupportsManagedDocumentRoot(record.Kind) {
		return domain.Record{}, false, errComposerUnsupportedDomain
	}
	if action != "install" && action != "update" {
		return domain.Record{}, false, fmt.Errorf("unsupported composer action %q", action)
	}

	targetPath, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return domain.Record{}, false, fmt.Errorf("resolve domain document root: %w", err)
	}

	manifestPath := filepath.Join(targetPath, "composer.json")
	if _, err := os.Stat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Record{}, false, errComposerMissingManifest
		}
		return domain.Record{}, false, fmt.Errorf("inspect composer manifest: %w", err)
	}

	composerPath, err := composerExecutablePath()
	if err != nil {
		return domain.Record{}, false, errComposerUnavailable
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

	runComposer := func(useWorker bool) (bool, string, error) {
		cmd := exec.CommandContext(runCtx, composerPath, action, "--no-interaction", "--no-progress")
		cmd.Dir = targetPath
		cmd.Env = composerCommandEnvironment()
		executedAsWorker := false
		if useWorker {
			var err error
			executedAsWorker, err = configureCommandForPHPWorker(runCtx, php, record.ID, record.PHPVersion, cmd)
			if err != nil {
				return false, "", err
			}
		}

		output := executil.NewTailBuffer(executil.DefaultOutputLimit)
		cmd.Stdout, cmd.Stderr = output, output

		err := cmd.Run()
		return executedAsWorker, strings.TrimSpace(output.String()), err
	}

	executedAsWorker, message, err := runComposer(true)
	if err != nil && executedAsWorker && shouldRetryWithoutPHPWorker(err) {
		executedAsWorker, message, err = runComposer(false)
	}
	if err != nil {
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return domain.Record{}, false, fmt.Errorf("composer %s timed out", action)
		case errors.Is(runCtx.Err(), context.Canceled):
			return domain.Record{}, false, fmt.Errorf("composer %s was canceled", action)
		case message != "":
			return domain.Record{}, false, fmt.Errorf("composer %s failed: %s", action, message)
		default:
			return domain.Record{}, false, fmt.Errorf("composer %s failed: %w", action, err)
		}
	}

	return record, executedAsWorker, nil
}

func composerCommandEnvironment() []string {
	env := os.Environ()
	pathValue := strings.TrimSpace(os.Getenv("PATH"))
	if pathValue == "" {
		pathValue = "/usr/local/bin:/usr/bin:/bin"
	} else if !pathListContains(pathValue, "/usr/local/bin") {
		pathValue = "/usr/local/bin" + string(os.PathListSeparator) + pathValue
	}
	env = setCommandEnvironmentValue(env, "PATH", pathValue)
	env = setCommandEnvironmentValue(env, "COMPOSER_ALLOW_SUPERUSER", "1")
	if strings.TrimSpace(os.Getenv("HOME")) == "" {
		if currentUser, err := user.Current(); err == nil && strings.TrimSpace(currentUser.HomeDir) != "" {
			env = setCommandEnvironmentValue(env, "HOME", strings.TrimSpace(currentUser.HomeDir))
		}
	}
	return env
}

func composerExecutablePath() (string, error) {
	const managedPath = "/usr/local/bin/composer"
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
		return managedPath, nil
	}
	return exec.LookPath("composer")
}

func pathListContains(list string, target string) bool {
	for _, entry := range filepath.SplitList(list) {
		if filepath.Clean(entry) == target {
			return true
		}
	}
	return false
}

func setCommandEnvironmentValue(env []string, key string, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}
