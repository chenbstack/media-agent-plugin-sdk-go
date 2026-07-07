//go:build !windows

package pluginrpc

import (
	"os/exec"
	"syscall"
)

func applyProcessCredentials(cmd *exec.Cmd, credentials *ProcessCredentials) error {
	if credentials == nil {
		return nil
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{
		Uid:    credentials.UID,
		Gid:    credentials.GID,
		Groups: credentials.AdditionalGroups,
	}
	return nil
}
