package systemstatus

import (
	"context"
	"runtime"
	"testing"
)

func TestInspectReturnsHostMetrics(t *testing.T) {
	status := Inspect(context.Background())

	if status.Cores <= 0 {
		t.Fatalf("cores = %d, want positive value", status.Cores)
	}

	if status.MemoryTotalBytes == nil || *status.MemoryTotalBytes == 0 {
		t.Fatal("memory total is empty")
	}

	switch runtime.GOOS {
	case "darwin", "linux", "freebsd":
		if status.Load1 == nil || status.Load5 == nil || status.Load15 == nil {
			t.Fatal("load averages are empty")
		}
		if status.CPUUsagePercent == nil {
			t.Fatal("cpu usage is empty")
		}
	}
}
