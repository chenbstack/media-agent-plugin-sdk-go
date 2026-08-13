package pluginrpc

import (
	"context"
	"io"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

func TestLifecycleInstallReceivesWorkspace(t *testing.T) {
	want := t.TempDir()
	var got string
	plugin := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "workspace-install", Name: "Workspace install"},
		InstallWithInstance: func(_ context.Context, inst pluginsdk.Instance, _ io.Writer) (pluginsdk.InstallResult, error) {
			got = inst.Workspace.Root()
			return pluginsdk.InstallResult{Installed: true}, nil
		},
	}
	client := newProviderTestClient(t, plugin)
	if _, err := client.InstallWithInstanceContext(context.Background(), "", pluginsdk.Instance{ID: "global", Workspace: pluginsdk.NewWorkspace(want)}, nil); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspace = %q, want %q", got, want)
	}
}
