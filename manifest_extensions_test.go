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
  capabilities:
    - name: requests.preview
      method: GET
      path: /requests/preview
    - name: requests.create
      method: POST
      path: /requests
      plugin_callable: true
agent:
  tools:
    - name: family.requests_list
      description: 查询当前用户可见的媒体申请
      capability: requests.preview
      risk: none
      input_schema:
        type: object
        additionalProperties: false
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
      required_permissions: [request.create]
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
  cards:
    - id: family.overview
      size: half
      export: OverviewCard
      title: 家庭总览
      header_export: OverviewCardHeader
      preview_export: OverviewCardPreview
      data:
        refresh_interval: 5m
        sources:
          - key: summary
            path: /summary
          - key: trend
            path: "/trend?days=30"
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
	if manifest.Agent == nil || len(manifest.Agent.Tools) != 1 || manifest.Agent.Tools[0].Capability != "requests.preview" {
		t.Fatalf("agent tools = %#v", manifest.Agent)
	}
	if len(manifest.API.Capabilities) != 2 || manifest.API.Capabilities[0].PluginCallable || !manifest.API.Capabilities[1].PluginCallable {
		t.Fatalf("api capabilities = %#v", manifest.API.Capabilities)
	}
	callable := manifest.API.PluginCallableCapabilities()
	if len(callable) != 1 || callable[0].Name != "requests.create" {
		t.Fatalf("callable plugin services = %#v", callable)
	}
	if manifest.UI == nil || len(manifest.UI.Routes) != 1 || len(manifest.UI.Actions) != 1 || manifest.UI.Routes[0].Menu == nil {
		t.Fatalf("ui = %#v", manifest.UI)
	}
	if got := manifest.UI.Routes[0].RequiredPermissions; len(got) != 1 || got[0] != "request.create" {
		t.Fatalf("route required_permissions = %v", got)
	}
	if len(manifest.UI.Cards) != 1 || manifest.UI.Cards[0].Title != "家庭总览" || manifest.UI.Cards[0].HeaderExport != "OverviewCardHeader" {
		t.Fatalf("cards = %#v", manifest.UI.Cards)
	}
	if manifest.UI.Cards[0].PreviewExport != "OverviewCardPreview" {
		t.Fatalf("cards preview_export = %#v", manifest.UI.Cards)
	}
	// 宿主代取声明：多路 sources 必须按声明顺序原样保留，带查询串的路径不能被截断。
	cardData := manifest.UI.Cards[0].Data
	if cardData == nil || cardData.RefreshInterval != "5m" || len(cardData.Sources) != 2 {
		t.Fatalf("cards data = %#v", cardData)
	}
	if cardData.Sources[0] != (UICardSource{Key: "summary", Path: "/summary"}) ||
		cardData.Sources[1] != (UICardSource{Key: "trend", Path: "/trend?days=30"}) {
		t.Fatalf("cards data sources = %#v", cardData.Sources)
	}
	if manifest.Identity == nil || manifest.Identity.Service != "family" || len(manifest.Identity.Flows) != 2 {
		t.Fatalf("identity = %#v", manifest.Identity)
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for _, field := range []string{`"api"`, `"agent"`, `"ui"`, `"identity"`, `"entitlements"`, `"required_entitlements"`} {
		if !strings.Contains(string(data), field) {
			t.Errorf("JSON 缺少 %s: %s", field, data)
		}
	}
}

func TestManifestAgentToolValidation(t *testing.T) {
	base := Manifest{
		ID: "family", Name: "Family", Version: "1", Type: "builtin",
		Capabilities: []string{"api.endpoint"},
		API: &APIExtension{Service: "app", Auth: APIAuthSession, Capabilities: []APIServiceCapability{{
			Name: "requests.list", Method: "GET", Path: "/requests", RequiredPermissions: []string{"request.create"},
		}}},
		Agent: &AgentExtension{Tools: []AgentToolDefinition{{
			Name: "family.requests_list", Description: "查询媒体申请", Capability: "requests.list",
			InputSchema: map[string]any{"type": "object"}, Risk: "none",
		}}},
	}
	if err := (Plugin{Manifest: base}).Validate(); err != nil {
		t.Fatalf("valid agent declaration: %v", err)
	}
	tests := []struct {
		name string
		edit func(*Manifest)
		want string
	}{
		{"namespace", func(m *Manifest) { m.Agent.Tools[0].Name = "other.create" }, "命名空间"},
		{"capability", func(m *Manifest) { m.Agent.Tools[0].Capability = "missing" }, "不存在"},
		{"schema", func(m *Manifest) { m.Agent.Tools[0].InputSchema = map[string]any{"type": "string"} }, "JSON object schema"},
		{"write capability", func(m *Manifest) {
			m.API.Capabilities[0].Method = "POST"
		}, "必须声明 confirmation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := base
			copy.API = &APIExtension{Service: base.API.Service, Auth: base.API.Auth, Capabilities: append([]APIServiceCapability(nil), base.API.Capabilities...)}
			copy.Agent = &AgentExtension{Tools: append([]AgentToolDefinition(nil), base.Agent.Tools...)}
			tt.edit(&copy)
			if err := (Plugin{Manifest: copy}).Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
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

func TestManifestRequiredMembership(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
id: pro-only
name: Pro Only
version: 1.0.0
type: cli
required_membership: pro
capabilities: [test.capability]
permissions: {network: [], secrets: []}
resources: {memory_limit_mb: 32, idle_timeout_seconds: 60}
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RequiredMembership != MembershipPro {
		t.Fatalf("required_membership = %q", manifest.RequiredMembership)
	}
	manifest.RequiredMembership = "enterprise"
	if err := (Plugin{Manifest: manifest}).Validate(); err == nil || !strings.Contains(err.Error(), "只支持 pro") {
		t.Fatalf("invalid required_membership error = %v", err)
	}
}

func TestManifestExtensionValidationRejectsUnsafeOrInconsistentDeclarations(t *testing.T) {
	valid := Manifest{
		ID: "family", Name: "Family", Version: "1", Type: "builtin",
		Capabilities: []string{"api.endpoint", "ui.module", "ui.action"},
		API: &APIExtension{
			Service: "app",
			Auth:    "session",
			Capabilities: []APIServiceCapability{{
				Name: "requests.list", Method: "GET", Path: "/requests",
			}},
		},
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
		{name: "invalid route permission", edit: func(m *Manifest) {
			m.UI.Routes[0].RequiredPermissions = []string{"users/manage"}
		}, want: "required_permissions"},
		{name: "api without capability", edit: func(m *Manifest) {
			m.Capabilities = []string{"ui.module"}
		}, want: "声明 api"},
		{name: "api without auth", edit: func(m *Manifest) { m.API.Auth = "" }, want: "必须显式声明"},
		{name: "invalid api permission", edit: func(m *Manifest) {
			m.API.RequiredPermissions = []string{"users/manage"}
		}, want: "required_permissions"},
		{name: "card header export without title", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", HeaderExport: "CardHeaderExtra"}}
		}, want: "缺少 title"},
		{name: "card header export invalid", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Title: "家庭卡片", HeaderExport: "bad name"}}
		}, want: "header_export"},
		{name: "card preview export invalid", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", PreviewExport: "bad name"}}
		}, want: "preview_export"},
		{name: "card data without api capability", edit: func(m *Manifest) {
			m.Capabilities = []string{"ui.module"}
			m.API = nil
			m.UI.Actions = nil
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{Sources: []UICardSource{{Key: "summary", Path: "/summary"}}}}}
		}, want: "没有 capability api.endpoint"},
		{name: "card data without sources", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{RefreshInterval: "5m"}}}
		}, want: "至少一路 sources"},
		{name: "card data bad interval", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{RefreshInterval: "5 minutes", Sources: []UICardSource{{Key: "a", Path: "/a"}}}}}
		}, want: "refresh_interval"},
		{name: "card data negative interval", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{RefreshInterval: "-1m", Sources: []UICardSource{{Key: "a", Path: "/a"}}}}}
		}, want: "必须为正数"},
		{name: "card data interval too long", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{RefreshInterval: "48h", Sources: []UICardSource{{Key: "a", Path: "/a"}}}}}
		}, want: "不能超过"},
		{name: "card data duplicate source key", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{Sources: []UICardSource{{Key: "a", Path: "/a"}, {Key: "a", Path: "/b"}}}}}
		}, want: "source key 重复"},
		{name: "card data bad source key", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{Sources: []UICardSource{{Key: "bad key", Path: "/a"}}}}}
		}, want: "source key"},
		{name: "card data relative source path", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{Sources: []UICardSource{{Key: "a", Path: "summary"}}}}}
		}, want: "绝对路径"},
		{name: "card data protocol relative source path", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{Sources: []UICardSource{{Key: "a", Path: "//evil.test/x"}}}}}
		}, want: "绝对路径"},
		{name: "card data traversal source path", edit: func(m *Manifest) {
			m.UI.Cards = []UICard{{ID: "family.card", Size: "half", Export: "CardBody", Data: &UICardData{Sources: []UICardSource{{Key: "a", Path: "/../other/x"}}}}}
		}, want: ".. 段"},
		{name: "api capabilities bad name", edit: func(m *Manifest) {
			m.API.Capabilities = []APIServiceCapability{{Name: "bad name", Method: "GET", Path: "/x"}}
		}, want: "能力名"},
		{name: "api capabilities bad path", edit: func(m *Manifest) {
			m.API.Capabilities = []APIServiceCapability{{Name: "op", Method: "GET", Path: "x"}}
		}, want: "path"},
		{name: "api capabilities bad method", edit: func(m *Manifest) {
			m.API.Capabilities = []APIServiceCapability{{Name: "op", Method: "TRACE", Path: "/x"}}
		}, want: "method"},
		{name: "api capabilities duplicate", edit: func(m *Manifest) {
			m.API.Capabilities = []APIServiceCapability{{Name: "op", Method: "GET", Path: "/x"}, {Name: "op", Method: "POST", Path: "/y"}}
		}, want: "重复"},
		{name: "api capabilities malformed parameter", edit: func(m *Manifest) {
			m.API.Capabilities = []APIServiceCapability{{Name: "op", Method: "GET", Path: "/users/prefix{id}"}}
		}, want: "path"},
		{name: "api callable capability with parameter", edit: func(m *Manifest) {
			m.API.Capabilities = []APIServiceCapability{{Name: "op", Method: "GET", Path: "/users/{id}", PluginCallable: true}}
		}, want: "不支持路径参数"},
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
