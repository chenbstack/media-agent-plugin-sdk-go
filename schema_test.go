package pluginsdk

import (
	"errors"
	"testing"

	"gopkg.in/yaml.v3"
)

var testSchema = ConfigSchema{Fields: []Field{
	{Name: "base_url", Type: "url", Label: "地址", Required: true},
	{Name: "username", Type: "string", Label: "用户名", Required: true},
	{Name: "password", Type: "password", Label: "密码", Required: true, Secret: true},
	{Name: "category", Type: "string", Label: "分类", Default: "media-agent"},
	{Name: "verify_tls", Type: "boolean", Label: "校验证书", Default: true},
	{Name: "mode", Type: "select", Label: "模式", Options: []Option{{Value: "fast", Label: "快"}, {Value: "slow", Label: "慢"}}},
	{Name: "timeout", Type: "number", Label: "超时"},
}}

func TestValidateOK(t *testing.T) {
	out, err := testSchema.Validate(map[string]any{
		"base_url": "http://192.168.1.10:8080/",
		"username": "admin",
		"password": "secret-value",
		"mode":     "fast",
		"timeout":  float64(30),
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if out["base_url"] != "http://192.168.1.10:8080" {
		t.Errorf("url 应去掉尾部斜杠: %v", out["base_url"])
	}
	if out["category"] != "media-agent" {
		t.Errorf("缺省字段应填 default: %v", out["category"])
	}
	if out["verify_tls"] != true {
		t.Errorf("boolean default 未填充: %v", out["verify_tls"])
	}
}

func TestValidateErrors(t *testing.T) {
	_, err := testSchema.Validate(map[string]any{
		"base_url": "not-a-url",
		"password": "x",
		"mode":     "unknown",
		"extra":    "nope",
	})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("应返回 ValidationError，得到 %v", err)
	}
	for _, field := range []string{"base_url", "username", "mode", "extra"} {
		if _, ok := verr.Fields[field]; !ok {
			t.Errorf("缺少字段错误: %s（全部: %v）", field, verr.Fields)
		}
	}
}

func TestSchemaSelfValidation(t *testing.T) {
	bad := ConfigSchema{Fields: []Field{{Name: "a", Type: "select", Label: "A"}}}
	if err := bad.validate("test"); err == nil {
		t.Error("select 无 options 应报错")
	}
	dup := ConfigSchema{Fields: []Field{
		{Name: "a", Type: "string", Label: "A"},
		{Name: "a", Type: "string", Label: "A2"},
	}}
	if err := dup.validate("test"); err == nil {
		t.Error("重复字段名应报错")
	}
	badSecret := ConfigSchema{Fields: []Field{{Name: "a", Type: "boolean", Label: "A", Secret: true}}}
	if err := badSecret.validate("test"); err == nil {
		t.Error("boolean secret 应报错")
	}
}

func TestParseManifestClassification(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
id: drive115
name: 115
version: 1.0.0
category: storage
tags: [115, cloud-drive]
type: cli
capabilities: [storage.path]
`))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if manifest.Category != CategoryStorage || len(manifest.Tags) != 2 || manifest.Tags[1] != "cloud-drive" {
		t.Fatalf("classification = %q %+v", manifest.Category, manifest.Tags)
	}
}

func TestManifestRejectsDuplicateActions(t *testing.T) {
	p := Plugin{Manifest: Manifest{
		ID: "automation", Name: "Automation", Version: "1", Type: "cli",
		Capabilities: []string{"action.run"}, Resources: Resources{MemoryLimitMB: 16},
		Actions: []ActionDefinition{{ID: "sync", Name: "同步"}, {ID: "sync", Name: "再次同步"}},
	}}
	if err := p.validate(); err == nil {
		t.Fatal("expected duplicate action validation error")
	}
}

func TestManifestScheduledTasks(t *testing.T) {
	permissions := Permissions{Network: []string{"configured:server_url"}, Host: []string{"site.credentials.apply"}}
	p := Plugin{Manifest: Manifest{
		ID: "automation", Name: "Automation", Version: "1", Type: "builtin",
		Capabilities: []string{CapabilityScheduledTask},
		Entitlements: []string{"automation.enabled"},
		Permissions:  permissions,
		ScheduledTasks: []ScheduledTaskDefinition{{
			ID: "sync", Name: "同步", DefaultEnabled: false,
			DefaultIntervalSeconds: 3600, MinIntervalSeconds: 60, MaxIntervalSeconds: 86400,
			TimeoutSeconds: 300, MaxAttempts: 3, OverlapPolicy: ScheduledTaskOverlapSkip,
			Executor:             ScheduledTaskExecutor{Kind: ScheduledTaskExecutorPluginHandler},
			Permissions:          &Permissions{Network: []string{"configured:server_url"}},
			RequiredEntitlements: []string{"automation.enabled"},
		}},
	}}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	data, err := yaml.Marshal(p.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.ScheduledTasks) != 1 || parsed.ScheduledTasks[0].Executor.Kind != ScheduledTaskExecutorPluginHandler {
		t.Fatalf("scheduled tasks = %#v", parsed.ScheduledTasks)
	}
}

func TestManifestRejectsInvalidScheduledTasks(t *testing.T) {
	tests := []struct {
		name     string
		manifest Manifest
	}{
		{
			name: "missing capability",
			manifest: Manifest{ID: "x", Name: "X", Version: "1", Type: "builtin", Capabilities: []string{"action.run"},
				ScheduledTasks: []ScheduledTaskDefinition{{ID: "sync", Name: "Sync", DefaultIntervalSeconds: 60, Executor: ScheduledTaskExecutor{Kind: ScheduledTaskExecutorPluginHandler}}}},
		},
		{
			name: "invalid interval",
			manifest: Manifest{ID: "x", Name: "X", Version: "1", Type: "builtin", Capabilities: []string{CapabilityScheduledTask},
				ScheduledTasks: []ScheduledTaskDefinition{{ID: "sync", Name: "Sync", DefaultIntervalSeconds: 30, MinIntervalSeconds: 60, Executor: ScheduledTaskExecutor{Kind: ScheduledTaskExecutorPluginHandler}}}},
		},
		{
			name: "host workflow missing id",
			manifest: Manifest{ID: "x", Name: "X", Version: "1", Type: "builtin", Capabilities: []string{CapabilityScheduledTask},
				ScheduledTasks: []ScheduledTaskDefinition{{ID: "sync", Name: "Sync", DefaultIntervalSeconds: 60, Executor: ScheduledTaskExecutor{Kind: ScheduledTaskExecutorHostWorkflow}}}},
		},
		{
			name: "permission escapes parent",
			manifest: Manifest{ID: "x", Name: "X", Version: "1", Type: "builtin", Capabilities: []string{CapabilityScheduledTask},
				ScheduledTasks: []ScheduledTaskDefinition{{ID: "sync", Name: "Sync", DefaultIntervalSeconds: 60, Executor: ScheduledTaskExecutor{Kind: ScheduledTaskExecutorPluginHandler}, Permissions: &Permissions{Host: []string{"site.credentials.apply"}}}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := (Plugin{Manifest: test.manifest}).Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	p := Plugin{Manifest: Manifest{
		ID: "demo", Name: "Demo", Version: "0.1.0", Type: "builtin",
		Capabilities: []string{"downloader.add", "downloader.list"},
	}}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(p); err == nil {
		t.Error("重复注册应报错")
	}
	if got := r.List("downloader"); len(got) != 1 {
		t.Errorf("按能力域过滤失败: %d", len(got))
	}
	if got := r.List("media_server"); len(got) != 0 {
		t.Errorf("不匹配能力域应为空: %d", len(got))
	}

	cli := Plugin{Manifest: Manifest{
		ID: "x", Name: "X", Version: "1", Type: "cli",
		Capabilities: []string{"downloader.add"},
	}}
	if err := r.Register(cli); err == nil {
		t.Error("CLI 插件缺 memory_limit_mb 应拒绝注册")
	}
}
