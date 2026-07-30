package firewall

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"
)

const chainName = "FLOWPANEL-INPUT"

type Config struct {
	AdminAddr       string
	HTTPAddr        string
	HTTPSAddr       string
	FTPEnabled      bool
	FTPPort         int
	FTPPassivePorts string
}

type Port struct {
	Port     int    `json:"port"`
	EndPort  int    `json:"end_port,omitempty"`
	Protocol string `json:"protocol"`
	Source   string `json:"source"`
}

type Status struct {
	Supported   bool   `json:"supported"`
	Enabled     bool   `json:"enabled"`
	Active      bool   `json:"active"`
	Backend     string `json:"backend,omitempty"`
	Allowed     []Port `json:"allowed"`
	DockerPorts []Port `json:"docker_ports"`
	Notice      string `json:"notice,omitempty"`
}

type state struct {
	Enabled     bool   `json:"enabled"`
	CustomPorts []Port `json:"custom_ports,omitempty"`
}

type Service struct {
	logger       *zap.Logger
	statePath    string
	defaultState bool
	mu           sync.Mutex
}

func NewService(logger *zap.Logger, statePath string, enabled bool) *Service {
	return &Service{logger: logger, statePath: statePath, defaultState: enabled}
}

func (s *Service) Status(ctx context.Context, cfg Config) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status(ctx, cfg)
}

func (s *Service) SetEnabled(ctx context.Context, enabled bool, cfg Config) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if runtime.GOOS != "linux" {
		return s.status(ctx, cfg), errors.New("managed firewall is available on Linux hosts only")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		return s.status(ctx, cfg), errors.New("iptables is required for managed firewall")
	}
	if enabled && hostHasPublicIPv6() {
		if _, err := exec.LookPath("ip6tables"); err != nil {
			return s.status(ctx, cfg), errors.New("ip6tables is required because this host has IPv6 connectivity")
		}
	}
	saved := s.load()
	saved.Enabled = enabled
	ports := desiredPorts(ctx, cfg, saved.CustomPorts)
	if enabled {
		if err := s.apply(ctx, "iptables", ports); err != nil {
			return s.status(ctx, cfg), err
		}
		if _, err := exec.LookPath("ip6tables"); err == nil {
			if err := s.apply(ctx, "ip6tables", ports); err != nil {
				return s.status(ctx, cfg), err
			}
		}
	} else {
		if err := s.remove(ctx, "iptables"); err != nil {
			return s.status(ctx, cfg), err
		}
		if err := s.remove(ctx, "ip6tables"); err != nil {
			return s.status(ctx, cfg), err
		}
	}
	if err := s.save(saved); err != nil {
		return s.status(ctx, cfg), err
	}
	return s.status(ctx, cfg), nil
}

func (s *Service) UpdatePort(ctx context.Context, port Port, open bool, cfg Config) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	port.Protocol = strings.ToLower(strings.TrimSpace(port.Protocol))
	port.Source = "Custom"
	if !validPort(port.Port) || (port.EndPort != 0 && (!validPort(port.EndPort) || port.EndPort < port.Port)) {
		return s.status(ctx, cfg), errors.New("port must be between 1 and 65535 and the range end cannot be lower than its start")
	}
	if port.Protocol != "tcp" && port.Protocol != "udp" {
		return s.status(ctx, cfg), errors.New("protocol must be tcp or udp")
	}

	saved := s.load()
	key := portKey(port)
	custom := make([]Port, 0, len(saved.CustomPorts)+1)
	for _, existing := range saved.CustomPorts {
		if portKey(existing) != key {
			custom = append(custom, existing)
		}
	}
	if open {
		custom = append(custom, port)
	}
	saved.CustomPorts = uniquePorts(custom)

	if saved.Enabled {
		if runtime.GOOS != "linux" {
			return s.status(ctx, cfg), errors.New("managed firewall is available on Linux hosts only")
		}
		if _, err := exec.LookPath("iptables"); err != nil {
			return s.status(ctx, cfg), errors.New("iptables is required for managed firewall")
		}
		if hostHasPublicIPv6() {
			if _, err := exec.LookPath("ip6tables"); err != nil {
				return s.status(ctx, cfg), errors.New("ip6tables is required because this host has IPv6 connectivity")
			}
		}
		ports := desiredPorts(ctx, cfg, saved.CustomPorts)
		if err := s.apply(ctx, "iptables", ports); err != nil {
			return s.status(ctx, cfg), err
		}
		if _, err := exec.LookPath("ip6tables"); err == nil {
			if err := s.apply(ctx, "ip6tables", ports); err != nil {
				return s.status(ctx, cfg), err
			}
		}
	}
	if err := s.save(saved); err != nil {
		return s.status(ctx, cfg), err
	}
	return s.status(ctx, cfg), nil
}

func (s *Service) Reconcile(ctx context.Context, cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	saved := s.load()
	if !saved.Enabled || runtime.GOOS != "linux" {
		return nil
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		return errors.New("iptables is required for managed firewall")
	}
	if hostHasPublicIPv6() {
		if _, err := exec.LookPath("ip6tables"); err != nil {
			return errors.New("ip6tables is required because this host has IPv6 connectivity")
		}
	}
	ports := desiredPorts(ctx, cfg, saved.CustomPorts)
	if err := s.apply(ctx, "iptables", ports); err != nil {
		return err
	}
	if _, err := exec.LookPath("ip6tables"); err == nil {
		return s.apply(ctx, "ip6tables", ports)
	}
	return nil
}

func (s *Service) status(ctx context.Context, cfg Config) Status {
	supported := runtime.GOOS == "linux"
	backend := ""
	notice := ""
	if supported {
		if _, err := exec.LookPath("iptables"); err == nil {
			backend = "iptables"
		} else {
			supported = false
			notice = "Install iptables to use the managed firewall."
		}
	} else {
		notice = "Managed firewall is available on Linux hosts only."
	}

	ipv6Required := supported && hostHasPublicIPv6()
	ipv6Available := false
	if supported {
		_, err := exec.LookPath("ip6tables")
		ipv6Available = err == nil
		if ipv6Required && !ipv6Available {
			notice = "Install ip6tables to protect this host's IPv6 traffic."
		}
	}
	saved := s.load()
	enabled := saved.Enabled
	active := false
	if supported {
		active = commandOK(ctx, "iptables", "-C", "INPUT", "-j", chainName)
		if ipv6Required {
			active = active && ipv6Available && commandOK(ctx, "ip6tables", "-C", "INPUT", "-j", chainName)
		}
	}
	if enabled && supported && !active && notice == "" {
		notice = "Managed firewall is enabled but its rules are not active. Reconcile the rules."
	}

	return Status{
		Supported:   supported,
		Enabled:     enabled,
		Active:      active,
		Backend:     backend,
		Allowed:     desiredPorts(ctx, cfg, saved.CustomPorts),
		DockerPorts: dockerPublicPorts(ctx),
		Notice:      notice,
	}
}

func hostHasPublicIPv6() bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() == nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return true
			}
		}
	}
	return false
}

func (s *Service) load() state {
	raw, err := os.ReadFile(s.statePath)
	if err != nil {
		return state{Enabled: s.defaultState}
	}
	var saved state
	if json.Unmarshal(raw, &saved) != nil {
		return state{Enabled: s.defaultState}
	}
	saved.CustomPorts = uniquePorts(saved.CustomPorts)
	for index := range saved.CustomPorts {
		saved.CustomPorts[index].Source = "Custom"
	}
	return saved
}

func (s *Service) save(saved state) error {
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return fmt.Errorf("create firewall state directory: %w", err)
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.statePath), ".firewall-*.json")
	if err != nil {
		return fmt.Errorf("create firewall state: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.statePath); err != nil {
		return fmt.Errorf("save firewall state: %w", err)
	}
	return nil
}

func (s *Service) apply(ctx context.Context, binary string, ports []Port) error {
	if _, err := exec.LookPath(binary); err != nil {
		return nil
	}
	_ = run(ctx, binary, "-N", chainName)
	if err := run(ctx, binary, "-F", chainName); err != nil {
		return err
	}

	rules := [][]string{
		{"-A", chainName, "-i", "lo", "-j", "ACCEPT"},
		{"-A", chainName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
	}
	if binary == "ip6tables" {
		rules = append(rules, []string{"-A", chainName, "-p", "ipv6-icmp", "-j", "ACCEPT"})
	} else {
		rules = append(rules, []string{"-A", chainName, "-p", "icmp", "-j", "ACCEPT"})
	}
	for _, port := range ports {
		destination := strconv.Itoa(port.Port)
		if port.EndPort > port.Port {
			destination += ":" + strconv.Itoa(port.EndPort)
		}
		rules = append(rules, []string{"-A", chainName, "-p", port.Protocol, "--dport", destination, "-j", "ACCEPT"})
	}
	rules = append(rules, []string{"-A", chainName, "-j", "DROP"})
	for _, args := range rules {
		if err := run(ctx, binary, args...); err != nil {
			return err
		}
	}

	for commandOK(ctx, binary, "-C", "INPUT", "-j", chainName) {
		if err := run(ctx, binary, "-D", "INPUT", "-j", chainName); err != nil {
			break
		}
	}
	if err := run(ctx, binary, "-I", "INPUT", "1", "-j", chainName); err != nil {
		return err
	}
	s.logger.Info("reconciled managed firewall", zap.String("backend", binary), zap.Int("allowed_ports", len(ports)))
	return nil
}

func (s *Service) remove(ctx context.Context, binary string) error {
	if _, err := exec.LookPath(binary); err != nil {
		return nil
	}
	for commandOK(ctx, binary, "-C", "INPUT", "-j", chainName) {
		if run(ctx, binary, "-D", "INPUT", "-j", chainName) != nil {
			break
		}
	}
	if commandOK(ctx, binary, "-C", "INPUT", "-j", chainName) {
		return fmt.Errorf("could not detach the managed firewall from %s", binary)
	}
	_ = run(ctx, binary, "-F", chainName)
	_ = run(ctx, binary, "-X", chainName)
	return nil
}

func desiredPorts(ctx context.Context, cfg Config, custom []Port) []Port {
	ports := []Port{
		{Port: addressPort(cfg.AdminAddr, 8443), Protocol: "tcp", Source: "FlowPanel"},
		{Port: addressPort(cfg.HTTPAddr, 80), Protocol: "tcp", Source: "HTTP"},
		{Port: addressPort(cfg.HTTPSAddr, 443), Protocol: "tcp", Source: "HTTPS"},
		{Port: sshPort(ctx), Protocol: "tcp", Source: "SSH"},
	}
	if cfg.FTPEnabled {
		ports = append(ports, Port{Port: cfg.FTPPort, Protocol: "tcp", Source: "FTP"})
		if start, end, ok := parsePortRange(cfg.FTPPassivePorts); ok {
			ports = append(ports, Port{Port: start, EndPort: end, Protocol: "tcp", Source: "FTP passive"})
		}
	}
	ports = append(ports, custom...)
	ports = append(ports, dockerPublicPorts(ctx)...)
	return uniquePorts(ports)
}

func addressPort(address string, fallback int) int {
	address = strings.TrimSpace(address)
	if _, port, err := net.SplitHostPort(address); err == nil {
		if value, err := strconv.Atoi(port); err == nil {
			return value
		}
	}
	if strings.HasPrefix(address, ":") {
		if value, err := strconv.Atoi(strings.TrimPrefix(address, ":")); err == nil {
			return value
		}
	}
	return fallback
}

func sshPort(ctx context.Context) int {
	if fields := strings.Fields(os.Getenv("SSH_CONNECTION")); len(fields) == 4 {
		if port, err := strconv.Atoi(fields[3]); err == nil && validPort(port) {
			return port
		}
	}
	output, err := exec.CommandContext(ctx, "sshd", "-T").Output()
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(output)))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) == 2 && fields[0] == "port" {
				if port, err := strconv.Atoi(fields[1]); err == nil && validPort(port) {
					return port
				}
			}
		}
	}
	if output, err := exec.CommandContext(ctx, "ss", "-H", "-ltnp").Output(); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(output)))
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, "sshd") {
				continue
			}
			for _, field := range strings.Fields(line) {
				if index := strings.LastIndex(field, ":"); index >= 0 {
					if port, err := strconv.Atoi(strings.Trim(field[index+1:], "[]")); err == nil && validPort(port) {
						return port
					}
				}
			}
		}
	}
	return 22
}

func parsePortRange(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) == 1 {
		port, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		if validPort(port) {
			return port, port, true
		}
		return 0, 0, false
	}
	start, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	if !validPort(start) || !validPort(end) || end < start {
		return 0, 0, false
	}
	return start, end, true
}

func dockerPublicPorts(ctx context.Context) []Port {
	ports := make([]Port, 0)
	if _, err := exec.LookPath("docker"); err != nil {
		return ports
	}
	output, err := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Ports}}").Output()
	if err != nil {
		return ports
	}
	for _, line := range strings.Split(string(output), "\n") {
		for _, mapping := range strings.Split(line, ",") {
			host, container, published := strings.Cut(strings.TrimSpace(mapping), "->")
			if !published || strings.Contains(host, "127.0.0.1:") || strings.Contains(host, "[::1]:") {
				continue
			}
			protocol := "tcp"
			if strings.HasSuffix(container, "/udp") {
				protocol = "udp"
			}
			host = strings.TrimSpace(host)
			portText := host
			if index := strings.LastIndex(host, ":"); index >= 0 {
				portText = host[index+1:]
			}
			if port, err := strconv.Atoi(portText); err == nil && validPort(port) {
				ports = append(ports, Port{Port: port, Protocol: protocol, Source: "Docker"})
			}
		}
	}
	return uniquePorts(ports)
}

func uniquePorts(ports []Port) []Port {
	seen := make(map[string]struct{})
	result := make([]Port, 0, len(ports))
	for _, port := range ports {
		if !validPort(port.Port) || (port.Protocol != "tcp" && port.Protocol != "udp") {
			continue
		}
		if port.EndPort != 0 && (!validPort(port.EndPort) || port.EndPort < port.Port) {
			continue
		}
		key := portKey(port)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, port)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port == result[j].Port {
			return result[i].Protocol < result[j].Protocol
		}
		return result[i].Port < result[j].Port
	})
	return result
}

func portKey(port Port) string {
	return fmt.Sprintf("%s:%d:%d", port.Protocol, port.Port, port.EndPort)
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}

func commandOK(ctx context.Context, binary string, args ...string) bool {
	return exec.CommandContext(ctx, binary, append([]string{"-w"}, args...)...).Run() == nil
}

func run(ctx context.Context, binary string, args ...string) error {
	output, err := exec.CommandContext(ctx, binary, append([]string{"-w"}, args...)...).CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s %s: %w", binary, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s: %s", binary, strings.Join(args, " "), message)
}
