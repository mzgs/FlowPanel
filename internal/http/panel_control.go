package httpx

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

type panelControlAction string

const (
	restartPanelAction  panelControlAction = "restart-panel"
	restartServerAction panelControlAction = "restart-server"
)

func schedulePanelControlAction(action panelControlAction) error {
	unit := fmt.Sprintf("flowpanel-%s-%d", action, time.Now().UnixNano())
	var command *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		args := []string{"--unit=" + unit, "--collect", "--on-active=1s", "systemctl"}
		if action == restartPanelAction {
			args = append(args, "restart", "flowpanel")
		} else {
			args = append(args, "reboot")
		}
		command = exec.Command("systemd-run", args...)
	case "darwin":
		script := "sleep 1; exec /sbin/shutdown -r now"
		if action == restartPanelAction {
			script = "sleep 1; exec /bin/launchctl kickstart -k system/com.mzgs.flowpanel"
		}
		command = exec.Command("launchctl", "submit", "-l", "com.mzgs.flowpanel."+unit, "--", "/bin/sh", "-c", script)
	default:
		return fmt.Errorf("%s is not supported on %s", action, runtime.GOOS)
	}

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("schedule %s: %w: %s", action, err, output)
	}
	return nil
}
