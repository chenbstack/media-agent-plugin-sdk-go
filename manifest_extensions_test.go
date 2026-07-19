package pluginsdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestFullStackExtensionsRoundTripAndValidate(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
id: family
name: 家庭协作
version: 1.0.0
type: cli
capabilities:
  - api.endpoint
  - ui.module
  - ui.action
  - identity.provider
api:
  service: app
  auth: session
  required_entitlements:
    - collaboration.requests.enabled
ui:
  module: ui/index.js
  routes:
    - id: family.requests
      path: /plugin/family/requests
      export: RequestsPage
      required_entitlements:
        - collaboration.requests.enabled
      menu:
        section: automation
        label: 订阅申请
        icon: rss
        order: 20
  actions:
    - id: family.request
      slot: media.detail.primary-actions
      export: MediaRequestAction
      required_entitlements:
        - collaboration.requests.enabled
      required_permissions: [request.create]
      forbidden_permissions: [system_settings.manage]
identity:
  service: family
  flows:
    - id: local
      type: credentials
      label: 媒体用户
    - id: company
      type: oidc
      label: 公司账号
  required_entitlements:
    - collaboration.users.max
entitlements:
  - collaboration.requests.enabled
  - collaboration.users.max
permissions:
  network: []
  secrets: []
resources:
  memory_limit_mb: 128
`))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if err := (Plugin{Manifest: manifest}).validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if manifest.API == nil || manifest.API.Service != "app" {
		t.Fatalf("api = %#v", manifest.API)
	}
	if manifest.UI == nil || len(manifest.UI.Routes) != 1 || len(manifest.UI.Actions) != 1 || manifest.UI.Routes[0].Menu == nil {
		t.Fatalf("ui = %#v", manifest.UI)
	}
	if manifest.Identity == nil || manifest.Identity.Service != "family" || len(manifest.Identity.Flows) != 2 {
		t.Fatalf("identity = %#v", manifest.Identity)
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for _, field := range []string{`"api"`, `"ui"`, `"identity"`, `"entitlements"`, `"required_entitlements"`} {
		if !strings.Contains(string(data), field) {
			t.Errorf("JSON 缺少 %s: %s", field, data)
		}
	}
}

func TestManifestExtensionsRemainOptionalForLegacyPlugins(t *testing.T) {
	plugin := Plugin{Manifest: Manifest{
		ID: "legacy", Name: "Legacy", Version: "1", Type: "builtin",
		Capabilities: []string{"storage.test"},
	}}
	if err := plugin.validate(); err != nil {
		t.Fatalf("legacy plugin should remain valid: %v", err)
	}

	identityWithoutDetails := Plugin{Manifest: Manifest{
		ID: "identity", Name: "Identity", Version: "1", Type: "builtin",
		Capabilities: []string{"identity.provider"},
	}}
	if err := identityWithoutDetails.validate(); err != nil {
		t.Fatalf("identity capability without optional details should remain valid: %v", err)
	}
}

func TestManifestExtensionValidationRejectsUnsafeOrInconsistentDeclarations(t *testing.T) {
	valid := Manifest{
		ID: "family", Name: "Family", Version: "1", Type: "builtin",
		Capabilities: []string{"api.endpoint", "ui.module", "ui.action"},
		API:          &APIExtension{Service: "app", Auth: "session"},
		UI: &UIExtension{Module: "ui/index.js", Routes: []UIRoute{{
			ID: "family.requests", Path: "/plugin/family/requests", Export: "RequestsPage",
		}}, Actions: []UIAction{{ID: "family.request", Slot: "media.detail.primary-actions", Export: "MediaRequestAction"}}},
	}

	tests := []struct {
		name string
		edit func(*Manifest)
		want string
	}{
		{name: "api missing", edit: func(m *Manifest) { m.API = nil }, want: "必须声明 api"},
		{name: "remote module", edit: func(m *Manifest) { m.UI.Module = "https://evil.test/ui.js" }, want: "相对路径"},
		{name: "script module", edit: func(m *Manifest) { m.UI.Module = "javascript:alert(1)" }, want: "相对路径"},
		{name: "encoded traversal module", edit: func(m *Manifest) { m.UI.Module = "%2e%2e/ui.js" }, want: "相对路径"},
		{name: "traversal module", edit: func(m *Manifest) { m.UI.Module = "../ui.js" }, want: "不能越界"},
		{name: "duplicate route", edit: func(m *Manifest) { m.UI.Routes = append(m.UI.Routes, m.UI.Routes[0]) }, want: "route id 重复"},
		{name: "action without capability", edit: func(m *Manifest) {
			m.Capabilities = []string{"api.endpoint", "ui.module"}
		}, want: "ui.actions"},
		{name: "action duplicates route id", edit: func(m *Manifest) { m.UI.Actions[0].ID = m.UI.Routes[0].ID }, want: "扩展 id 重复"},
		{name: "invalid action slot", edit: func(m *Manifest) { m.UI.Actions[0].Slot = "media/detail" }, want: "slot"},
		{name: "relative route", edit: func(m *Manifest) { m.UI.Routes[0].Path = "plugin/family" }, want: "path"},
		{name: "undeclared entitlement", edit: func(m *Manifest) {
			m.UI.Routes[0].RequiredEntitlements = []string{"collaboration.requests.enabled"}
		}, want: "未在 manifest 声明"},
		{name: "api without capability", edit: func(m *Manifest) {
			m.Capabilities = []string{"ui.module"}
		}, want: "声明 api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := cloneManifest(t, valid)
			tt.edit(&manifest)
			err := (Plugin{Manifest: manifest}).validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func cloneManifest(t *testing.T, manifest Manifest) Manifest {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var out Manifest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
