package systemstatus

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

const cpuSampleWindow = 200 * time.Millisecond

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
	ServerTime        string   `json:"server_time"`
	ServerTimeDisplay string   `json:"server_time_display"`
	Timezone          string   `json:"timezone"`
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
