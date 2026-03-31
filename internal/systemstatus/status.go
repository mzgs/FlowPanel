package systemstatus

import (
	"context"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

const cpuSampleWindow = 200 * time.Millisecond

type Status struct {
	Cores            int      `json:"cores"`
	CPUUsagePercent  *float64 `json:"cpu_usage_percent,omitempty"`
	Load1            *float64 `json:"load_1,omitempty"`
	Load5            *float64 `json:"load_5,omitempty"`
	Load15           *float64 `json:"load_15,omitempty"`
	MemoryTotalBytes *uint64  `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes  *uint64  `json:"memory_used_bytes,omitempty"`
}

func Inspect(ctx context.Context) Status {
	status := Status{
		Cores: runtime.NumCPU(),
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

	return status
}

func float64Ptr(value float64) *float64 {
	return &value
}

func uint64Ptr(value uint64) *uint64 {
	return &value
}
