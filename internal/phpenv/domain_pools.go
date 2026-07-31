package phpenv

import (
	"context"
	"crypto/sha256"
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
	"time"

	"go.uber.org/zap"
)

const domainPoolFilePrefix = "zz-flowpanel-"

type domainPoolRuntimeConfig struct {
	status RuntimeStatus
	dir    string
}

type domainPoolFileSnapshot struct {
	content []byte
	mode    fs.FileMode
	exists  bool
}

type domainRootSnapshot struct {
	uid  int
	gid  int
	mode fs.FileMode
}

func (s *Service) ReconcileDomainPools(ctx context.Context, inputs []DomainPoolInput) (pools map[string]DomainPool, reconcileErr error) {
	pools = make(map[string]DomainPool, len(inputs))
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return pools, nil
	}

	runtimes := map[string]domainPoolRuntimeConfig{}
	desiredFiles := map[string]struct{}{}
	desiredUsers := map[string]struct{}{}
	staleUsers := map[string]struct{}{}
	changedVersions := map[string]domainPoolRuntimeConfig{}
	snapshots := map[string]domainPoolFileSnapshot{}
	rootSnapshots := map[string]domainRootSnapshot{}
	createdUsers := map[string]struct{}{}
	committed := false
	defer func() {
		if committed || reconcileErr == nil {
			return
		}
		if err := rollbackDomainPoolChanges(ctx, snapshots, rootSnapshots, changedVersions, createdUsers); err != nil {
			reconcileErr = fmt.Errorf("%w; rollback PHP pools: %v", reconcileErr, err)
		}
	}()

	for _, input := range inputs {
		version := NormalizeVersion(input.Version)
		if version == "" {
			return nil, fmt.Errorf("resolve PHP version for %q", input.Hostname)
		}
		runtimeConfig, ok := runtimes[version]
		if !ok {
			status := s.StatusForVersion(ctx, version)
			if !status.Ready {
				return nil, fmt.Errorf("php-fpm %s is not ready: %s", version, status.Message)
			}
			dir, err := domainPoolConfigDirectory(version, status.FPMPath)
			if err != nil {
				return nil, err
			}
			runtimeConfig = domainPoolRuntimeConfig{status: status, dir: dir}
			runtimes[version] = runtimeConfig
		}

		root := filepath.Clean(strings.TrimSpace(input.DocumentRoot))
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("domain root for %q must be absolute", input.Hostname)
		}
		name := DomainUserName(input.DomainID)
		desiredUsers[name] = struct{}{}
		created, err := ensureDomainUser(ctx, name, root)
		if err != nil {
			return nil, fmt.Errorf("ensure PHP user for %q: %w", input.Hostname, err)
		}
		if created {
			createdUsers[name] = struct{}{}
		}
		if err := snapshotDomainRoot(root, rootSnapshots); err != nil {
			return nil, fmt.Errorf("snapshot PHP root ownership for %q: %w", input.Hostname, err)
		}
		if err := ensureDomainRootOwnership(root, name, created); err != nil {
			return nil, fmt.Errorf("isolate PHP files for %q: %w", input.Hostname, err)
		}

		socket := domainPoolSocket(runtimeConfig.status.ListenAddress, name)
		pool := DomainPool{User: name, Group: name, ListenAddress: socket}
		configPath := filepath.Join(runtimeConfig.dir, domainPoolFilePrefix+strings.TrimPrefix(name, "fp_")+".conf")
		desiredFiles[configPath] = struct{}{}
		if err := snapshotDomainPoolFile(configPath, snapshots); err != nil {
			return nil, fmt.Errorf("snapshot PHP pool for %q: %w", input.Hostname, err)
		}
		changed, err := writeFileIfChanged(configPath, renderDomainPool(input, pool))
		if err != nil {
			return nil, fmt.Errorf("write PHP pool for %q: %w", input.Hostname, err)
		}
		if changed {
			changedVersions[version] = runtimeConfig
		}
		pools[input.DomainID] = pool
	}

	for _, version := range SupportedVersions() {
		status := s.StatusForVersion(ctx, version)
		if !status.FPMInstalled || status.FPMPath == "" {
			continue
		}
		dir, err := domainPoolConfigDirectory(version, status.FPMPath)
		if err != nil {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(dir, domainPoolFilePrefix+"*.conf"))
		for _, path := range matches {
			if _, keep := desiredFiles[path]; keep {
				continue
			}
			if err := snapshotDomainPoolFile(path, snapshots); err != nil {
				return nil, fmt.Errorf("snapshot stale PHP pool %q: %w", path, err)
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("remove stale PHP pool %q: %w", path, err)
			}
			suffix := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), domainPoolFilePrefix), ".conf")
			staleUsers["fp_"+suffix] = struct{}{}
			changedVersions[version] = domainPoolRuntimeConfig{status: status, dir: dir}
		}
	}

	for version, config := range changedVersions {
		if output, err := exec.CommandContext(ctx, config.status.FPMPath, "-tt").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("validate PHP %s pools: %w: %s", version, err, strings.TrimSpace(string(output)))
		}
		if config.status.ServiceRunning {
			if err := runPHPFPMServiceCommand(ctx, config.status.FPMPath, "reload"); err != nil {
				return nil, fmt.Errorf("reload PHP %s pools: %w", version, err)
			}
		}
	}

	for _, pool := range pools {
		if _, changed := changedVersions[poolVersion(inputs, pool.User)]; !changed {
			continue
		}
		if err := waitForDomainPool(ctx, pool.ListenAddress); err != nil {
			return nil, err
		}
	}
	for name := range staleUsers {
		if _, keep := desiredUsers[name]; keep {
			continue
		}
		if err := deleteDomainUser(ctx, name); err != nil {
			s.logger.Warn("remove stale domain PHP user failed", zap.String("user", name), zap.Error(err))
		}
	}

	committed = true
	return pools, nil
}
func poolVersion(inputs []DomainPoolInput, userName string) string {
	for _, input := range inputs {
		if DomainUserName(input.DomainID) == userName {
			return NormalizeVersion(input.Version)
		}
	}
	return ""
}

func DomainUserName(domainID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(domainID)))
	return fmt.Sprintf("fp_%x", sum[:6])
}

func (s *Service) DomainWorkerIdentity(ctx context.Context, domainID, version string) (WorkerIdentity, error) {
	if strings.TrimSpace(domainID) == "" || runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return s.WorkerIdentity(ctx, version)
	}
	name := DomainUserName(domainID)
	if _, err := user.Lookup(name); err != nil {
		if _, ok := err.(user.UnknownUserError); ok {
			return s.WorkerIdentity(ctx, version)
		}
		return WorkerIdentity{}, fmt.Errorf("look up domain PHP user %q: %w", name, err)
	}
	return WorkerIdentity{User: name, Group: name}, nil
}

func domainPoolConfigDirectory(version, fpmPath string) (string, error) {
	digits := strings.ReplaceAll(version, ".", "")
	candidates := []string{
		filepath.Join("/etc/php", version, "fpm", "pool.d"),
		filepath.Join("/etc/opt/remi", "php"+digits, "php-fpm.d"),
		filepath.Join("/opt/remi", "php"+digits, "root", "etc", "php-fpm.d"),
		"/etc/php-fpm.d",
	}
	if strings.Contains(filepath.ToSlash(fpmPath), "/opt/remi/") {
		candidates[0], candidates[2] = candidates[2], candidates[0]
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not locate the PHP %s FPM pool directory", version)
}

func domainPoolSocket(sharedAddress, userName string) string {
	sharedAddress = strings.TrimPrefix(strings.TrimSpace(sharedAddress), "unix:")
	dir := "/run/php"
	if filepath.IsAbs(sharedAddress) {
		dir = filepath.Dir(sharedAddress)
	} else if info, err := os.Stat("/run/php-fpm"); err == nil && info.IsDir() {
		dir = "/run/php-fpm"
	}
	return filepath.Join(dir, userName+".sock")
}

func ensureDomainUser(ctx context.Context, name, home string) (bool, error) {
	if _, err := user.Lookup(name); err == nil {
		return false, nil
	} else if _, ok := err.(user.UnknownUserError); !ok {
		return false, err
	}
	useradd, err := exec.LookPath("useradd")
	if err != nil {
		return false, errors.New("useradd is not installed")
	}
	shell := "/usr/sbin/nologin"
	if _, err := os.Stat(shell); err != nil {
		shell = "/sbin/nologin"
	}
	args := []string{"--system", "--no-create-home", "--home-dir", home, "--shell", shell}
	if _, err := user.LookupGroup(name); err == nil {
		args = append(args, "--gid", name)
	} else {
		args = append(args, "--user-group")
	}
	output, err := exec.CommandContext(ctx, useradd, append(args, name)...).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("useradd: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return true, nil
}

func deleteDomainUser(ctx context.Context, name string) error {
	if !strings.HasPrefix(name, "fp_") {
		return errors.New("refusing to remove an unmanaged user")
	}
	if _, err := user.Lookup(name); err != nil {
		if _, ok := err.(user.UnknownUserError); ok {
			return nil
		}
		return err
	}
	userdel, err := exec.LookPath("userdel")
	if err != nil {
		return errors.New("userdel is not installed")
	}
	output, err := exec.CommandContext(ctx, userdel, name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("userdel: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if groupdel, err := exec.LookPath("groupdel"); err == nil {
		if output, err := exec.CommandContext(ctx, groupdel, name).CombinedOutput(); err != nil && !strings.Contains(strings.ToLower(string(output)), "does not exist") {
			return fmt.Errorf("groupdel: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func ensureDomainRootOwnership(root, userName string, force bool) error {
	account, err := user.Lookup(userName)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	if !force {
		if info, err := os.Stat(root); err == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) == uid && int(stat.Gid) == gid {
				return nil
			}
		}
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chown(path, uid, gid)
	}); err != nil {
		return err
	}
	return os.Chmod(root, 0o750)
}

func renderDomainPool(input DomainPoolInput, pool DomainPool) string {
	settings := input.Settings
	maxChildren := firstNonEmpty(strings.TrimSpace(settings.FPMMaxChildren), "3")
	idleTimeout := firstNonEmpty(strings.TrimSpace(settings.FPMIdleTimeout), "30s")
	maxRequests := firstNonEmpty(strings.TrimSpace(settings.FPMMaxRequests), "500")
	return fmt.Sprintf(`; Managed by FlowPanel. Manual edits may be overwritten.
[%s]
user = %s
group = %s
listen = %s
listen.owner = %s
listen.group = %s
listen.mode = 0660
pm = ondemand
pm.max_children = %s
pm.process_idle_timeout = %s
pm.max_requests = %s
chdir = %s
catch_workers_output = yes
clear_env = no
security.limit_extensions = .php .phar
php_admin_value[disable_functions] = %s
`, pool.User, pool.User, pool.Group, pool.ListenAddress, pool.User, pool.Group, maxChildren, idleTimeout, maxRequests, input.DocumentRoot, settings.DisableFunctions)
}

func snapshotDomainPoolFile(path string, snapshots map[string]domainPoolFileSnapshot) error {
	if _, exists := snapshots[path]; exists {
		return nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		snapshots[path] = domainPoolFileSnapshot{}
		return nil
	}
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	snapshots[path] = domainPoolFileSnapshot{content: content, mode: info.Mode().Perm(), exists: true}
	return nil
}

func rollbackDomainPoolChanges(
	ctx context.Context,
	snapshots map[string]domainPoolFileSnapshot,
	rootSnapshots map[string]domainRootSnapshot,
	changedVersions map[string]domainPoolRuntimeConfig,
	createdUsers map[string]struct{},
) error {
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	var rollbackErrors []error
	for path, snapshot := range snapshots {
		var err error
		if snapshot.exists {
			err = os.WriteFile(path, snapshot.content, snapshot.mode)
		} else {
			err = os.Remove(path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %q: %w", path, err))
		}
	}
	for version, config := range changedVersions {
		if !config.status.ServiceRunning {
			continue
		}
		if err := runPHPFPMServiceCommand(ctx, config.status.FPMPath, "reload"); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("reload PHP %s: %w", version, err))
		}
	}
	for root, snapshot := range rootSnapshots {
		if err := restoreDomainRoot(root, snapshot); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore ownership for %q: %w", root, err))
		}
	}
	for name := range createdUsers {
		if err := deleteDomainUser(ctx, name); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove PHP user %q: %w", name, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func snapshotDomainRoot(root string, snapshots map[string]domainRootSnapshot) error {
	if _, exists := snapshots[root]; exists {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("unsupported file metadata")
	}
	snapshots[root] = domainRootSnapshot{uid: int(stat.Uid), gid: int(stat.Gid), mode: info.Mode().Perm()}
	return nil
}

func restoreDomainRoot(root string, snapshot domainRootSnapshot) error {
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chown(path, snapshot.uid, snapshot.gid)
	}); err != nil {
		return err
	}
	return os.Chmod(root, snapshot.mode)
}

func writeFileIfChanged(path, content string) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && string(current) == content {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".flowpanel-pool-*")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.WriteString(content); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(temporaryPath, path)
}

func waitForDomainPool(ctx context.Context, address string) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if canDialFastCGI(address) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return fmt.Errorf("PHP-FPM pool did not become ready at %s", address)
}
