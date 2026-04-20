package systemstatus

import (
	"context"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

const cpuSampleWindow = 200 * time.Millisecond
const publicIPv4CacheTTL = 10 * time.Minute
const publicIPv4RetryDelay = 1 * time.Minute

var publicIPv4Cache struct {
	mu        sync.Mutex
	value     string
	expiresAt time.Time
}

type Status struct {
	Cores             int      `json:"cores"`
	CPUUsagePercent   *float64 `json:"cpu_usage_percent,omitempty"`
	DiskFreeBytes     *uint64  `json:"disk_free_bytes,omitempty"`
	DiskMountPath     string   `json:"disk_mount_path,omitempty"`
	DiskTotalBytes    *uint64  `json:"disk_total_bytes,omitempty"`
	DiskUsedBytes     *uint64  `json:"disk_used_bytes,omitempty"`
	Hostname          string   `json:"hostname,omitempty"`
	Load1             *float64 `json:"load_1,omitempty"`
	Load5             *float64 `json:"load_5,omitempty"`
	Load15            *float64 `json:"load_15,omitempty"`
	MemoryTotalBytes  *uint64  `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes   *uint64  `json:"memory_used_bytes,omitempty"`
	Platform          string   `json:"platform"`
	PlatformName      string   `json:"platform_name"`
	PlatformVersion   string   `json:"platform_version,omitempty"`
	PublicIPv4        string   `json:"public_ipv4,omitempty"`
	ServerTime        string   `json:"server_time"`
	ServerTimeDisplay string   `json:"server_time_display"`
	Timezone          string   `json:"timezone"`
	UptimeSeconds     uint64   `json:"uptime_seconds,omitempty"`
}

func Inspect(ctx context.Context) Status {
	now := time.Now()
	status := Status{
		Cores:             runtime.NumCPU(),
		Platform:          runtime.GOOS,
		PlatformName:      defaultPlatformName(runtime.GOOS),
		ServerTime:        now.Format(time.RFC3339),
		ServerTimeDisplay: now.Format("Jan 2, 2006 15:04:05"),
		Timezone:          now.Format("MST"),
	}

	if hostname, err := os.Hostname(); err == nil {
		status.Hostname = hostname
	}

	if info, err := host.InfoWithContext(ctx); err == nil {
		if hostname := strings.TrimSpace(info.Hostname); hostname != "" {
			status.Hostname = hostname
		}
		if platform := formatPlatformName(info.Platform); platform != "" {
			status.PlatformName = platform
		}
		if version := strings.TrimSpace(info.PlatformVersion); version != "" {
			status.PlatformVersion = version
		}
		if info.Uptime > 0 {
			status.UptimeSeconds = info.Uptime
		}
	}

	if avg, err := load.AvgWithContext(ctx); err == nil {
		status.Load1 = float64Ptr(avg.Load1)
		status.Load5 = float64Ptr(avg.Load5)
		status.Load15 = float64Ptr(avg.Load15)
	}

	if sample, err := cpu.PercentWithContext(ctx, cpuSampleWindow, false); err == nil && len(sample) > 0 {
		status.CPUUsagePercent = float64Ptr(sample[0])
	}

	if virtualMemory, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		status.MemoryTotalBytes = uint64Ptr(virtualMemory.Total)
		status.MemoryUsedBytes = uint64Ptr(virtualMemory.Used)
	}

	if usage, err := disk.UsageWithContext(ctx, diskUsagePath()); err == nil {
		status.DiskMountPath = usage.Path
		status.DiskTotalBytes = uint64Ptr(usage.Total)
		status.DiskUsedBytes = uint64Ptr(usage.Used)
		status.DiskFreeBytes = uint64Ptr(usage.Free)
	}

	status.PublicIPv4 = cachedPublicIPv4(ctx)

	return status
}

func diskUsagePath() string {
	if runtime.GOOS == "windows" {
		if drive := os.Getenv("SystemDrive"); drive != "" {
			return drive + "\\"
		}

		return "C:\\"
	}

	return "/"
}

func float64Ptr(value float64) *float64 {
	return &value
}

func uint64Ptr(value uint64) *uint64 {
	return &value
}

func cachedPublicIPv4(ctx context.Context) string {
	publicIPv4Cache.mu.Lock()
	defer publicIPv4Cache.mu.Unlock()

	now := time.Now()
	if now.Before(publicIPv4Cache.expiresAt) {
		return publicIPv4Cache.value
	}

	staleValue := publicIPv4Cache.value
	value := fetchPublicIPv4(ctx)
	if value != "" {
		publicIPv4Cache.value = value
		publicIPv4Cache.expiresAt = now.Add(publicIPv4CacheTTL)
		return value
	}

	publicIPv4Cache.expiresAt = now.Add(publicIPv4RetryDelay)
	return staleValue
}

func fetchPublicIPv4(ctx context.Context) string {
	lookupCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	req, err := stdhttp.NewRequestWithContext(lookupCtx, stdhttp.MethodGet, "https://api4.ipify.org", nil)
	if err != nil {
		return ""
	}

	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != stdhttp.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}

	ip := strings.TrimSpace(string(body))
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil || parsedIP.To4() == nil {
		return ""
	}

	return parsedIP.String()
}

func defaultPlatformName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "freebsd":
		return "FreeBSD"
	default:
		return strings.TrimSpace(goos)
	}
}

func formatPlatformName(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "":
		return ""
	case "macos", "mac os", "mac os x", "darwin":
		return "macOS"
	case "ubuntu":
		return "Ubuntu"
	case "debian":
		return "Debian"
	case "centos":
		return "CentOS"
	case "rhel":
		return "RHEL"
	case "rocky":
		return "Rocky Linux"
	case "almalinux":
		return "AlmaLinux"
	case "amzn":
		return "Amazon Linux"
	case "opensuse":
		return "openSUSE"
	default:
		return titleWords(strings.ReplaceAll(value, "-", " "))
	}
}

func titleWords(value string) string {
	parts := strings.Fields(strings.TrimSpace(strings.ToLower(value)))
	for index, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[index] = string(runes)
	}

	return strings.Join(parts, " ")
}
