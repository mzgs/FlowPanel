//go:build !linux

package workload

import "os/exec"

func scopeIdentity(cmd *exec.Cmd) ([]string, error) { return nil, nil }
