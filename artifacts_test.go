package pluginsdk

import (
	"strings"
	"testing"
)

func validArtifactPlugin() Plugin {
	return Plugin{
		Manifest: Manifest{
			ID: "strm", Name: "STRM", Version: "1.0.0", Type: "builtin",
			Capabilities: []string{"event.subscribe"},
			Subscriptions: []EventSubscription{{
				Type: "library.refresh.pending", Version: 1, Phase: "before", Mode: "sync",
			}},
			Artifacts: []ArtifactDefinition{{
				ID: "strm", Kind: "mirror", TargetStorageField: "target_storage_id", Extension: "strm",
				MediaLibraryVisible: true, RequiredBeforeLibraryRefresh: true,
			}},
			Permissions: Permissions{Host: []string{"media.mirror.write"}},
		},
		ConfigSchema: ConfigSchema{Fields: []Field{{
			Name: "target_storage_id", Type: "select", Label: "目标存储",
			UI: &FieldUI{Browse: BrowseStorageInstance},
		}}},
	}
}

func TestManifestAcceptsDeclaredArtifact(t *testing.T) {
	if err := validArtifactPlugin().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestManifestRejectsArtifactWithoutSynchronousRefreshHook(t *testing.T) {
	plugin := validArtifactPlugin()
	plugin.Manifest.Subscriptions[0] = EventSubscription{
		Type: "organize.file.committed", Version: 1, Phase: "after", Mode: "async",
	}
	err := plugin.Validate()
	if err == nil || !strings.Contains(err.Error(), "library.refresh.pending") {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestManifestRejectsArtifactWithoutMirrorPermission(t *testing.T) {
	plugin := validArtifactPlugin()
	plugin.Manifest.Permissions.Host = nil
	err := plugin.Validate()
	if err == nil || !strings.Contains(err.Error(), "media.mirror.write") {
		t.Fatalf("Validate error = %v", err)
	}
}
