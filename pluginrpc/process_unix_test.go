//go:build !windows

package pluginrpc

import (
	"os/exec"
	"testing"
)

func TestApplyProcessCredentials(t *testing.T) {
	cmd := exec.Command("true")
	err := applyProcessCredentials(cmd, &ProcessCredentials{UID: 10002, GID: 10002, AdditionalGroups: []uint32{10003}})
	if err != nil {
		t.Fatalf("applyProcessCredentials: %v", err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil {
		t.Fatal("expected SysProcAttr.Credential to be set")
	}
	if cmd.SysProcAttr.Credential.Uid != 10002 || cmd.SysProcAttr.Credential.Gid != 10002 {
		t.Fatalf("credential = %#v", cmd.SysProcAttr.Credential)
	}
}
