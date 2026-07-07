//go:build windows

package pluginrpc

import (
	"fmt"
	"os/exec"
)

func applyProcessCredentials(cmd *exec.Cmd, credentials *ProcessCredentials) error {
	if credentials == nil {
		return nil
	}
	return fmt.Errorf("当前平台不支持插件进程 UID/GID 降权")
}
