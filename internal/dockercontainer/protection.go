package dockercontainer

import (
	"context"
	"errors"
	"strings"

	"flowpanel/internal/workload"
)

// Verify daemon support before creating or deleting anything. Unsupported
// drivers must not silently ignore the shared workload budget.
func PrepareProtection(ctx context.Context) error {
	if !workload.Enabled() {
		return nil
	}
	if err := workload.Ensure(ctx); err != nil {
		return err
	}
	info, err := dockerOutput(ctx, "info", "--format", "{{.CgroupDriver}} {{.CgroupVersion}}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(info) != "systemd 2" {
		return errors.New("Docker workload protection requires the systemd cgroup driver and cgroup v2")
	}
	return nil
}

func CheckProtection(ctx context.Context, record Record) error {
	if err := PrepareProtection(ctx); err != nil {
		return err
	}
	if workload.Enabled() && record.HostConfig.CgroupParent != workload.AppsSlice {
		return errors.New("recreate this container to apply the PHP-aware workload budget before starting it")
	}
	return nil
}
