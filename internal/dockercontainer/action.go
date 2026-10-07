package dockercontainer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	gopsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

var portConflictPattern = regexp.MustCompile(`(?:failed to bind host port |listen (tcp|udp) )(\[[^\]]+\]|[^\s:]+):(\d+)(?:/(tcp|udp))?`)

// RunAction reclaims only bindings reported as conflicting by Docker.
func RunAction(ctx context.Context, containerID, action string) error {
	released := make(map[string]bool)
	for {
		_, err := dockerOutput(ctx, action, containerID)
		if err == nil || ctx.Err() != nil || (action != "start" && action != "restart") {
			return err
		}
		message := err.Error()
		binding := portConflictPattern.FindStringSubmatch(message)
		if len(binding) == 0 || released[binding[0]] || !strings.Contains(message, "address already in use") {
			return err
		}
		ip := net.ParseIP(strings.Trim(binding[2], "[]"))
		port, parseErr := strconv.ParseUint(binding[3], 10, 16)
		if ip == nil || parseErr != nil || port == 0 {
			return err
		}
		protocol := binding[4]
		if protocol == "" {
			protocol = binding[1]
		}
		if protocol == "" {
			return err
		}
		if releaseErr := releaseHostPort(ctx, ip, uint32(port), protocol); releaseErr != nil {
			return errors.Join(err, fmt.Errorf("release host port %d/%s: %w", port, protocol, releaseErr))
		}
		released[binding[0]] = true
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
		// A failed restart has already stopped the container.
		action = "start"
	}
}

func releaseHostPort(ctx context.Context, ip net.IP, port uint32, protocol string) error {
	endpoint := os.Getenv("DOCKER_HOST")
	if endpoint == "" || os.Getenv("DOCKER_CONTEXT") != "" {
		var err error
		endpoint, err = dockerOutput(ctx, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
		if err != nil {
			return err
		}
	}
	if !strings.HasPrefix(endpoint, "unix://") {
		return errors.New("automatic port recovery requires a local Docker Unix socket")
	}
	matches := func(host string) bool {
		other := net.ParseIP(host)
		return other != nil && (ip.IsUnspecified() || other.IsUnspecified() || ip.Equal(other))
	}
	// Stop a container through Docker rather than killing its port proxy.
	records, err := Snapshot(ctx)
	if err != nil {
		return err
	}
	hostPort := strconv.FormatUint(uint64(port), 10)
	for _, record := range records {
		if !record.State.Running {
			continue
		}
		conflicts := false
		for key, bindings := range record.Network.Ports {
			if !strings.HasSuffix(key, "/"+protocol) {
				continue
			}
			for _, binding := range bindings {
				if binding.HostPort == hostPort && matches(binding.HostIP) {
					conflicts = true
				}
			}
		}
		if conflicts {
			if _, err := dockerOutput(ctx, "stop", record.ID); err != nil {
				return err
			}
		}
	}
	connections, err := gopsnet.ConnectionsWithContext(ctx, protocol)
	if err != nil {
		return err
	}
	killed := make(map[int32]bool)
	for _, connection := range connections {
		if connection.Laddr.Port != port || !matches(connection.Laddr.IP) || (protocol == "tcp" && connection.Status != "LISTEN") || killed[connection.Pid] {
			continue
		}
		if connection.Pid <= 1 || connection.Pid == int32(os.Getpid()) {
			return fmt.Errorf("cannot kill protected or unidentified listener (PID %d)", connection.Pid)
		}
		proc, err := process.NewProcessWithContext(ctx, connection.Pid)
		if err != nil {
			return err
		}
		name, err := proc.NameWithContext(ctx)
		if err != nil {
			return err
		}
		switch name {
		case "dockerd", "containerd", "com.docker.backend":
			return fmt.Errorf("cannot kill the Docker daemon process %q", name)
		}
		if err := proc.KillWithContext(ctx); err != nil {
			return err
		}
		killed[connection.Pid] = true
	}
	return nil
}
