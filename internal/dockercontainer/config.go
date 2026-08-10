package dockercontainer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	imagePullIdleTimeout    = 5 * time.Minute
	restoreProgressInterval = 5 * time.Second
)

type Record struct {
	ID         string      `json:"Id"`
	Name       string      `json:"Name"`
	Config     Config      `json:"Config"`
	HostConfig HostConfig  `json:"HostConfig"`
	Mounts     []Mount     `json:"Mounts"`
	Network    Network     `json:"NetworkSettings"`
	State      StateRecord `json:"State"`
}

type Config struct {
	Image        string            `json:"Image"`
	Env          []string          `json:"Env"`
	Entrypoint   []string          `json:"Entrypoint"`
	Cmd          []string          `json:"Cmd"`
	WorkingDir   string            `json:"WorkingDir"`
	User         string            `json:"User"`
	Labels       map[string]string `json:"Labels"`
	Hostname     string            `json:"Hostname"`
	Domainname   string            `json:"Domainname"`
	StopSignal   string            `json:"StopSignal"`
	Tty          bool              `json:"Tty"`
	OpenStdin    bool              `json:"OpenStdin"`
	Volumes      map[string]any    `json:"Volumes"`
	ExposedPorts map[string]any    `json:"ExposedPorts"`
}

type HostConfig struct {
	Binds           []string                 `json:"Binds"`
	PortBindings    map[string][]PortBinding `json:"PortBindings"`
	RestartPolicy   RestartPolicy            `json:"RestartPolicy"`
	NetworkMode     string                   `json:"NetworkMode"`
	ExtraHosts      []string                 `json:"ExtraHosts"`
	CapAdd          []string                 `json:"CapAdd"`
	CapDrop         []string                 `json:"CapDrop"`
	DNS             []string                 `json:"Dns"`
	DNSSearch       []string                 `json:"DnsSearch"`
	Tmpfs           map[string]string        `json:"Tmpfs"`
	ShmSize         int64                    `json:"ShmSize"`
	AutoRemove      bool                     `json:"AutoRemove"`
	PublishAllPorts bool                     `json:"PublishAllPorts"`
	ReadonlyRootfs  bool                     `json:"ReadonlyRootfs"`
	Privileged      bool                     `json:"Privileged"`
	Init            *bool                    `json:"Init"`
}

type Mount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

type StateRecord struct {
	Status    string `json:"Status"`
	Running   bool   `json:"Running"`
	OOMKilled bool   `json:"OOMKilled"`
	Dead      bool   `json:"Dead"`
	Error     string `json:"Error"`
	ExitCode  int    `json:"ExitCode"`
}

type Network struct {
	Ports map[string][]PortBinding `json:"Ports"`
}

type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type RestartPolicy struct {
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount"`
}

type RestoreProgress struct {
	Container string
	Image     string
	Current   int
	Total     int
	Pulling   bool
}

func Snapshot(ctx context.Context) ([]Record, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, nil
	}
	ids, err := dockerOutput(ctx, "ps", "-aq")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(ids)
	if len(fields) == 0 {
		return []Record{}, nil
	}
	payload, err := dockerOutput(ctx, append([]string{"inspect"}, fields...)...)
	if err != nil {
		return nil, err
	}
	var records []Record
	if err := json.Unmarshal([]byte(payload), &records); err != nil {
		return nil, fmt.Errorf("decode Docker container definitions: %w", err)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return records, nil
}

func Restore(ctx context.Context, records []Record, managedDataRoot string, report func(RestoreProgress)) ([]string, error) {
	if len(records) == 0 {
		return []string{}, nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, errors.New("Docker is not installed on this server")
	}

	type restoreRecord struct {
		record Record
		name   string
		image  string
	}
	prepared := make([]restoreRecord, 0, len(records))
	var restoreErrors []error
	for _, record := range records {
		item := restoreRecord{
			record: record,
			name:   strings.TrimPrefix(strings.TrimSpace(record.Name), "/"),
			image:  strings.TrimSpace(record.Config.Image),
		}
		if item.name == "" || item.image == "" {
			restoreErrors = append(restoreErrors, errors.New("Docker backup contains an invalid container definition"))
			continue
		}
		prepared = append(prepared, item)
	}

	progressFor := func(index int, item restoreRecord, pulling bool) RestoreProgress {
		return RestoreProgress{Container: item.name, Image: item.image, Current: index + 1, Total: len(prepared), Pulling: pulling}
	}
	available := make([]restoreRecord, 0, len(prepared))
	for index, item := range prepared {
		if _, err := dockerOutput(ctx, "image", "inspect", item.image); err == nil {
			available = append(available, item)
			continue
		}
		progress := progressFor(index, item, true)
		if report != nil {
			report(progress)
		}
		_, pullErr := dockerOutputWithHeartbeat(ctx, imagePullIdleTimeout, func() {
			if report != nil {
				report(progress)
			}
		}, "pull", item.image)
		if pullErr != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore Docker image %q: %w", item.image, pullErr))
			continue
		}
		available = append(available, item)
	}

	restored := make([]string, 0, len(available))
	for index, item := range available {
		progress := progressFor(index, item, false)
		if report != nil {
			report(progress)
		}
		if network := strings.TrimSpace(item.record.HostConfig.NetworkMode); isCustomNetwork(network) {
			if _, err := dockerOutput(ctx, "network", "inspect", network); err != nil {
				if _, createErr := dockerOutput(ctx, "network", "create", network); createErr != nil {
					restoreErrors = append(restoreErrors, fmt.Errorf("restore Docker network %q: %w", network, createErr))
					continue
				}
			}
		}
		_, _ = dockerOutput(ctx, "rm", "-f", item.name)
		containerID, err := dockerOutput(ctx, CreateArgs(item.record)...)
		if err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore Docker container %q: %w", item.name, err))
			continue
		}
		if err := PrepareManagedVolumePermissions(ctx, item.record, managedDataRoot); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("prepare restored Docker container %q data: %w", item.name, err))
			continue
		}
		if item.record.State.Running {
			if _, err := dockerOutput(ctx, "start", strings.TrimSpace(containerID)); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("start restored Docker container %q: %w", item.name, err))
				continue
			}
		}
		restored = append(restored, item.name)
	}
	return restored, errors.Join(restoreErrors...)
}

func PrepareManagedVolumePermissions(ctx context.Context, record Record, root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return nil
	}

	sources := make([]string, 0, len(record.HostConfig.Binds)+len(record.Mounts))
	for _, bind := range record.HostConfig.Binds {
		if source := strings.TrimSpace(strings.SplitN(bind, ":", 2)[0]); source != "" {
			sources = append(sources, source)
		}
	}
	for _, mount := range record.Mounts {
		if mount.Type == "bind" && strings.TrimSpace(mount.Source) != "" {
			sources = append(sources, mount.Source)
		}
	}

	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		source = filepath.Clean(source)
		relative, err := filepath.Rel(root, source)
		if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		if err := PrepareVolumePermissions(ctx, record, source); err != nil {
			return err
		}
	}
	return nil
}

func PrepareVolumePermissions(ctx context.Context, record Record, source string) error {
	user := strings.TrimSpace(record.Config.User)
	if user == "" || user == "0" || user == "0:0" || user == "root" || user == "root:root" {
		return nil
	}

	parts := strings.SplitN(user, ":", 2)
	uid, uidErr := strconv.Atoi(parts[0])
	gid, gidErr := -1, error(nil)
	if len(parts) == 2 {
		gid, gidErr = strconv.Atoi(parts[1])
	}
	if uidErr == nil && gidErr == nil {
		if err := filepath.Walk(source, func(path string, _ os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			return os.Lchown(path, uid, gid)
		}); err != nil {
			return fmt.Errorf("set Docker volume ownership on %q to %q: %w", source, user, err)
		}
		return nil
	}

	if strings.HasPrefix(user, "-") || strings.ContainsAny(user, "\x00\r\n") {
		return fmt.Errorf("Docker image uses an unsupported container user %q", user)
	}
	if _, err := dockerOutput(ctx, "run", "--rm", "--user", "0:0", "--entrypoint", "chown", "--volume", source+":/flowpanel-volume", record.Config.Image, "-R", user, "/flowpanel-volume"); err != nil {
		return fmt.Errorf("prepare Docker volume %q for container user %q: %w", source, user, err)
	}
	return nil
}

func Stop(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker is not installed on this server")
	}
	var stopErrors []error
	for _, record := range records {
		name := strings.TrimPrefix(strings.TrimSpace(record.Name), "/")
		if name == "" {
			stopErrors = append(stopErrors, errors.New("Docker backup contains an invalid container definition"))
			continue
		}
		if _, err := dockerOutput(ctx, "container", "inspect", name); err != nil {
			continue
		}
		if _, err := dockerOutput(ctx, "stop", name); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("stop Docker container %q: %w", name, err))
		}
	}
	return errors.Join(stopErrors...)
}

func Start(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker is not installed on this server")
	}
	var startErrors []error
	for _, record := range records {
		if !record.State.Running {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(record.Name), "/")
		if name == "" {
			startErrors = append(startErrors, errors.New("Docker backup contains an invalid container definition"))
			continue
		}
		if _, err := dockerOutput(ctx, "start", name); err != nil {
			startErrors = append(startErrors, fmt.Errorf("restart Docker container %q after backup: %w", name, err))
		}
	}
	return errors.Join(startErrors...)
}

func CreateArgs(record Record) []string {
	args := []string{"create", "-q"}
	add := func(flag, value string) {
		if value = strings.TrimSpace(value); value != "" {
			args = append(args, flag, value)
		}
	}
	add("--name", strings.TrimPrefix(record.Name, "/"))
	add("--hostname", record.Config.Hostname)
	add("--domainname", record.Config.Domainname)
	add("--workdir", record.Config.WorkingDir)
	add("--user", record.Config.User)
	add("--stop-signal", record.Config.StopSignal)
	if record.Config.Tty {
		args = append(args, "--tty")
	}
	if record.Config.OpenStdin {
		args = append(args, "--interactive")
	}
	if record.HostConfig.AutoRemove {
		args = append(args, "--rm")
	}
	if record.HostConfig.PublishAllPorts {
		args = append(args, "--publish-all")
	}
	if record.HostConfig.ReadonlyRootfs {
		args = append(args, "--read-only")
	}
	if record.HostConfig.Privileged {
		args = append(args, "--privileged")
	}
	if record.HostConfig.Init != nil && *record.HostConfig.Init {
		args = append(args, "--init")
	}
	if record.HostConfig.ShmSize > 0 {
		add("--shm-size", strconv.FormatInt(record.HostConfig.ShmSize, 10))
	}
	entrypoint, entrypointArgs := commandParts(record.Config.Entrypoint)
	add("--entrypoint", entrypoint)
	add("--restart", restartPolicyValue(record.HostConfig.RestartPolicy))
	if network := strings.TrimSpace(record.HostConfig.NetworkMode); network != "" && network != "default" {
		add("--network", network)
	}

	keys := sortedKeys(record.Config.Labels)
	for _, key := range keys {
		add("--label", key+"="+record.Config.Labels[key])
	}
	for _, value := range record.Config.Env {
		add("--env", value)
	}
	for _, value := range record.HostConfig.ExtraHosts {
		add("--add-host", value)
	}
	for _, value := range record.HostConfig.DNS {
		add("--dns", value)
	}
	for _, value := range record.HostConfig.DNSSearch {
		add("--dns-search", value)
	}
	for _, value := range record.HostConfig.CapAdd {
		add("--cap-add", value)
	}
	for _, value := range record.HostConfig.CapDrop {
		add("--cap-drop", value)
	}

	portKeys := make([]string, 0, len(record.HostConfig.PortBindings))
	for key := range record.HostConfig.PortBindings {
		portKeys = append(portKeys, key)
	}
	sort.Strings(portKeys)
	for _, key := range portKeys {
		bindings := record.HostConfig.PortBindings[key]
		if len(bindings) == 0 {
			add("--expose", key)
			continue
		}
		for _, binding := range bindings {
			add("--publish", publishValue(key, binding))
		}
	}
	for _, bind := range record.HostConfig.Binds {
		add("--volume", bind)
	}

	bindDestinations := make(map[string]struct{}, len(record.HostConfig.Binds))
	for _, bind := range record.HostConfig.Binds {
		if destination := bindDestination(bind); destination != "" {
			bindDestinations[destination] = struct{}{}
		}
	}
	for _, mount := range record.Mounts {
		if mount.Destination == "" {
			continue
		}
		if _, exists := bindDestinations[mount.Destination]; exists {
			continue
		}
		switch mount.Type {
		case "bind":
			spec := strings.TrimSpace(mount.Source) + ":" + strings.TrimSpace(mount.Destination)
			if !mount.RW {
				spec += ":ro"
			}
			add("--volume", spec)
		case "volume":
			spec := "type=volume"
			if mount.Name != "" {
				spec += ",src=" + mount.Name
			}
			spec += ",dst=" + mount.Destination
			if !mount.RW {
				spec += ",readonly"
			}
			add("--mount", spec)
		}
	}
	tmpfsKeys := sortedKeys(record.HostConfig.Tmpfs)
	for _, key := range tmpfsKeys {
		spec := key
		if value := strings.TrimSpace(record.HostConfig.Tmpfs[key]); value != "" {
			spec += ":" + value
		}
		add("--tmpfs", spec)
	}
	exposedKeys := make([]string, 0, len(record.Config.ExposedPorts))
	for key := range record.Config.ExposedPorts {
		exposedKeys = append(exposedKeys, key)
	}
	sort.Strings(exposedKeys)
	for _, key := range exposedKeys {
		if _, published := record.HostConfig.PortBindings[key]; !published {
			add("--expose", key)
		}
	}
	args = append(args, strings.TrimSpace(record.Config.Image))
	args = append(args, entrypointArgs...)
	return append(args, record.Config.Cmd...)
}

func dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", dockerCommandError(stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

type activityBuffer struct {
	bytes.Buffer
	activity chan<- struct{}
}

func (b *activityBuffer) Write(data []byte) (int, error) {
	select {
	case b.activity <- struct{}{}:
	default:
	}
	return b.Buffer.Write(data)
}

func dockerOutputWithHeartbeat(ctx context.Context, idleTimeout time.Duration, heartbeat func(), args ...string) (string, error) {
	type result struct {
		err error
	}
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	activity := make(chan struct{}, 1)
	stdout := activityBuffer{activity: activity}
	stderr := activityBuffer{activity: activity}
	cmd := exec.CommandContext(commandCtx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	done := make(chan result, 1)
	go func() {
		done <- result{err: cmd.Run()}
	}()

	ticker := time.NewTicker(restoreProgressInterval)
	defer ticker.Stop()
	idleTimer := time.NewTimer(idleTimeout)
	defer idleTimer.Stop()
	for {
		select {
		case result := <-done:
			if result.err != nil {
				return "", dockerCommandError(stderr.String(), result.err)
			}
			return strings.TrimSpace(stdout.String()), nil
		case <-activity:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)
		case <-idleTimer.C:
			cancel()
			<-done
			return "", fmt.Errorf("Docker command produced no output for %s", idleTimeout)
		case <-ticker.C:
			heartbeat()
		}
	}
}

func dockerCommandError(stderr string, err error) error {
	if message := strings.TrimSpace(stderr); message != "" {
		return errors.New(message)
	}
	return err
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func commandParts(command []string) (string, []string) {
	values := make([]string, 0, len(command))
	for _, value := range command {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], values[1:]
}

func restartPolicyValue(policy RestartPolicy) string {
	name := strings.TrimSpace(policy.Name)
	if name == "" || name == "no" {
		return ""
	}
	if name == "on-failure" && policy.MaximumRetryCount > 0 {
		return fmt.Sprintf("%s:%d", name, policy.MaximumRetryCount)
	}
	return name
}

func publishValue(containerPort string, binding PortBinding) string {
	hostPort, hostIP := strings.TrimSpace(binding.HostPort), strings.TrimSpace(binding.HostIP)
	if hostIP != "" && hostPort != "" {
		return hostIP + ":" + hostPort + ":" + containerPort
	}
	if hostPort != "" {
		return hostPort + ":" + containerPort
	}
	if hostIP != "" {
		return hostIP + "::" + containerPort
	}
	return containerPort
}

func bindDestination(bind string) string {
	parts := strings.Split(strings.TrimSpace(bind), ":")
	if len(parts) < 2 {
		return ""
	}
	if len(parts) > 2 {
		return strings.TrimSpace(parts[len(parts)-2])
	}
	return strings.TrimSpace(parts[1])
}

func isCustomNetwork(name string) bool {
	return name != "" && name != "default" && name != "bridge" && name != "host" && name != "none" && !strings.HasPrefix(name, "container:")
}
