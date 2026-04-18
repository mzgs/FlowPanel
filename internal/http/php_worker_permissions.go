package httpx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"flowpanel/internal/domain"
	"flowpanel/internal/phpenv"
)

func ensurePHPDocumentRootWorkerOwnership(
	ctx context.Context,
	php phpenv.Manager,
	domains *domain.Service,
	record domain.Record,
) error {
	if record.Kind != domain.KindPHP {
		return nil
	}

	documentRoot, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
	if err != nil {
		return fmt.Errorf("resolve domain document root: %w", err)
	}

	return ensurePHPWorkerOwnership(ctx, php, record.PHPVersion, true, documentRoot)
}

func ensurePHPUploadWorkerOwnership(
	ctx context.Context,
	php phpenv.Manager,
	domains *domain.Service,
	uploadDirectory string,
	uploadedPaths []string,
) error {
	record, ok := resolvePHPDomainRecordForPath(domains, uploadDirectory)
	if !ok {
		return nil
	}

	paths := make([]string, 0, len(uploadedPaths)+1)
	paths = append(paths, uploadDirectory)
	paths = append(paths, uploadedPaths...)

	return ensurePHPWorkerOwnership(ctx, php, record.PHPVersion, false, paths...)
}

func ensurePHPWorkerOwnership(
	ctx context.Context,
	php phpenv.Manager,
	version string,
	recursive bool,
	paths ...string,
) error {
	if runtime.GOOS != "linux" || php == nil || len(paths) == 0 {
		return nil
	}

	identity, err := php.WorkerIdentity(ctx, version)
	if err != nil {
		return err
	}

	worker, err := resolvePHPWorkerRuntime(identity)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}

		if recursive {
			if err := chownPathTree(path, worker.uid, worker.gid); err != nil {
				return err
			}
			continue
		}

		if err := chownOnePath(path, worker.uid, worker.gid); err != nil {
			return err
		}
	}

	return nil
}

func resolvePHPDomainRecordForPath(domains *domain.Service, absolutePath string) (domain.Record, bool) {
	if domains == nil {
		return domain.Record{}, false
	}

	absolutePath = filepath.Clean(strings.TrimSpace(absolutePath))
	if absolutePath == "" {
		return domain.Record{}, false
	}

	var matched domain.Record
	longestMatch := -1
	for _, record := range domains.List() {
		if record.Kind != domain.KindPHP {
			continue
		}

		documentRoot, err := domain.ResolveDocumentRoot(domains.BasePath(), record)
		if err != nil {
			continue
		}
		documentRoot = filepath.Clean(documentRoot)
		if !pathWithin(documentRoot, absolutePath) {
			continue
		}
		if len(documentRoot) <= longestMatch {
			continue
		}

		matched = record
		longestMatch = len(documentRoot)
	}

	return matched, longestMatch >= 0
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}

	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

type phpWorkerRuntime struct {
	name    string
	group   string
	homeDir string
	uid     int
	gid     int
}

func resolvePHPWorkerRuntime(identity phpenv.WorkerIdentity) (phpWorkerRuntime, error) {
	account, err := user.Lookup(strings.TrimSpace(identity.User))
	if err != nil {
		return phpWorkerRuntime{}, fmt.Errorf("look up php-fpm worker user %q: %w", identity.User, err)
	}

	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return phpWorkerRuntime{}, fmt.Errorf("parse php-fpm worker uid %q: %w", account.Uid, err)
	}

	groupName := strings.TrimSpace(identity.Group)
	groupID := strings.TrimSpace(account.Gid)
	if groupName != "" {
		group, err := user.LookupGroup(groupName)
		if err != nil {
			return phpWorkerRuntime{}, fmt.Errorf("look up php-fpm worker group %q: %w", groupName, err)
		}
		groupID = group.Gid
	} else {
		groupName = groupID
	}

	gid, err := strconv.Atoi(groupID)
	if err != nil {
		return phpWorkerRuntime{}, fmt.Errorf("parse php-fpm worker gid %q: %w", groupID, err)
	}

	return phpWorkerRuntime{
		name:    strings.TrimSpace(identity.User),
		group:   groupName,
		homeDir: strings.TrimSpace(account.HomeDir),
		uid:     uid,
		gid:     gid,
	}, nil
}

func configureCommandForPHPWorker(
	ctx context.Context,
	php phpenv.Manager,
	version string,
	cmd *exec.Cmd,
) (bool, error) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 || php == nil || cmd == nil {
		return false, nil
	}

	identity, err := php.WorkerIdentity(ctx, version)
	if err != nil {
		return false, err
	}
	worker, err := resolvePHPWorkerRuntime(identity)
	if err != nil {
		return false, err
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(worker.uid),
			Gid: uint32(worker.gid),
		},
	}
	cmd.Env = withCommandIdentityEnv(cmd.Env, worker)
	return true, nil
}

func shouldRetryWithoutPHPWorker(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

func withCommandIdentityEnv(env []string, worker phpWorkerRuntime) []string {
	if len(env) == 0 {
		env = os.Environ()
	}

	pairs := make([]string, 0, len(env)+3)
	skip := map[string]struct{}{
		"HOME":    {},
		"USER":    {},
		"LOGNAME": {},
	}
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, exists := skip[key]; exists {
				continue
			}
		}
		pairs = append(pairs, entry)
	}

	if worker.homeDir != "" {
		pairs = append(pairs, "HOME="+worker.homeDir)
	}
	if worker.name != "" {
		pairs = append(pairs, "USER="+worker.name, "LOGNAME="+worker.name)
	}

	return pairs
}

func chownPathTree(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chown(currentPath, uid, gid)
	})
}

func chownOnePath(path string, uid, gid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	return os.Chown(path, uid, gid)
}
