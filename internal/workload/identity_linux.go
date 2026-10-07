package workload

import (
	"os/exec"
	"strconv"
	"strings"
)

// Connect to systemd as root, then restore the worker credentials inside the scope.
func scopeIdentity(cmd *exec.Cmd) ([]string, error) {
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil {
		return nil, nil
	}
	path, err := exec.LookPath("setpriv")
	if err != nil {
		return nil, err
	}
	credential := cmd.SysProcAttr.Credential
	args := []string{path, "--reuid=" + strconv.FormatUint(uint64(credential.Uid), 10), "--regid=" + strconv.FormatUint(uint64(credential.Gid), 10)}
	groupFlag := "--clear-groups"
	if credential.NoSetGroups {
		groupFlag = "--keep-groups"
	} else if len(credential.Groups) > 0 {
		groups := make([]string, len(credential.Groups))
		for i, group := range credential.Groups {
			groups[i] = strconv.FormatUint(uint64(group), 10)
		}
		groupFlag = "--groups=" + strings.Join(groups, ",")
	}
	attributes := *cmd.SysProcAttr
	attributes.Credential = nil
	cmd.SysProcAttr = &attributes
	return append(args, groupFlag, "--"), nil
}
