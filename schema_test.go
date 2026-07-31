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

var multiSchema = ConfigSchema{Fields: []Field{
	{Name: "languages", Type: "multiselect", Label: "语言", Default: []any{"zh-cn", "en"}, Options: []Option{
		{Value: "zh-cn", Label: "简体中文"},
		{Value: "zh-tw", Label: "繁体中文"},
		{Value: "en", Label: "英语"},
	}},
	{Name: "tags", Type: "multiselect", Label: "标签", Required: true, Options: []Option{
		{Value: "a", Label: "A"}, {Value: "b", Label: "B"},
	}},
}}

func TestValidateMultiselect(t *testing.T) {
	out, err := multiSchema.Validate(map[string]any{
		// []any 是过一趟 JSON 之后的形状。
		"languages": []any{"en", "zh-cn"},
		"tags":      []string{"b", "a"},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// 勾选先后不该决定顺序，否则同一份配置在两次编辑后行为不同。
	if got, ok := out["languages"].([]string); !ok || len(got) != 2 || got[0] != "zh-cn" || got[1] != "en" {
		t.Errorf("应按 options 声明顺序归一化: %#v", out["languages"])
	}
	if got, ok := out["tags"].([]string); !ok || got[0] != "a" || got[1] != "b" {
		t.Errorf("[]string 输入也应归一化: %#v", out["tags"])
	}
}

// 缺省值在 schema.json 里是 JSON 数组、用户勾选过的是 []string；两种形状都漏给插件
// 的话，插件侧就得两边都认。
func TestValidateMultiselectDefaultIsNormalized(t *testing.T) {
	out, err := multiSchema.Validate(map[string]any{"tags": []any{"a"}})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got, ok := out["languages"].([]string)
	if !ok || len(got) != 2 || got[0] != "zh-cn" || got[1] != "en" {
		t.Fatalf("缺省值应归一成 []string: %#v", out["languages"])
	}
}

// 这个字段从 string 改成 multiselect 之前存下来的配置是逗号分隔的字符串。不认它的话，
// 升级后老实例会卡在「应为字符串列表」上，用户得手动重填一遍才能保存。
func TestValidateMultiselectAcceptsLegacyCommaString(t *testing.T) {
	out, err := multiSchema.Validate(map[string]any{
		"languages": "en, zh-tw",
		"tags":      "a",
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got, _ := out["languages"].([]string)
	if len(got) != 2 || got[0] != "zh-tw" || got[1] != "en" {
		t.Fatalf("逗号字符串应认: %#v", out["languages"])
	}
}

// 全不勾时前端交上来的是空数组，跟一个字都没填是同一件事：required 要拦住它，
// 有 default 的要退回 default。
func TestValidateMultiselectTreatsEmptyAsBlank(t *testing.T) {
	out, err := multiSchema.Validate(map[string]any{"languages": []any{}, "tags": []string{"a"}})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got, _ := out["languages"].([]string); len(got) != 2 {
		t.Errorf("空数组应退回 default: %#v", out["languages"])
	}

	_, err = multiSchema.Validate(map[string]any{"tags": []any{}})
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Fields["tags"] != "必填" {
		t.Fatalf("空数组不该绕过 required: %v", err)
	}
}

func TestValidateMultiselectRejectsUnknownOption(t *testing.T) {
	_, err := multiSchema.Validate(map[string]any{"languages": []any{"zh-cn", "kl"}, "tags": []any{"a"}})
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Fields["languages"] == "" {
		t.Fatalf("选项外的取值应报错: %v", err)
	}

	_, err = multiSchema.Validate(map[string]any{"languages": []any{1, 2}, "tags": []any{"a"}})
	if !errors.As(err, &verr) || verr.Fields["languages"] != "应为字符串列表" {
		t.Fatalf("非字符串元素应报错: %v", err)
	}
}

// 存储 id 是用户装出来的，schema 写不出候选项。不放行的话，用户在界面上选中一个
// 存储、保存时会被判「取值不在选项内」——这个字段永远存不进去。
func TestValidateStorageInstanceFieldAcceptsAnyStorageID(t *testing.T) {
	schema := ConfigSchema{Fields: []Field{{
		Name:    "target_storage_id",
		Type:    "select",
		Label:   "目标存储",
		Options: []Option{{Value: "", Label: "请选择存储"}},
		UI:      &FieldUI{Browse: BrowseStorageInstance},
	}}}

	out, err := schema.Validate(map[string]any{"target_storage_id": "st_7f3a"})
	if err != nil {
		t.Fatalf("存储 id 应当被接受: %v", err)
	}
	if out["target_storage_id"] != "st_7f3a" {
		t.Fatalf("归一化结果 = %+v", out)
	}

	// 没有这个 browse 标记的 select 仍然只认声明过的选项。
	plain := ConfigSchema{Fields: []Field{{
		Name: "mode", Type: "select", Label: "模式", Options: []Option{{Value: "a", Label: "A"}},
	}}}
	if _, err := plain.Validate(map[string]any{"mode": "st_7f3a"}); err == nil {
		t.Error("普通 select 不该放行选项外的取值")
	}
}

// 撤掉一个配置项时，已装实例的配置里还留着它。不认的话 Validate 判「未声明的字段」，
// 用户一打开设置页就是一片红，连保存都保存不了。
func TestValidateRetiredField(t *testing.T) {
	schema := ConfigSchema{Fields: []Field{
		{Name: "token", Type: "password", Label: "令牌", Secret: true},
		{Name: "api_key", Type: "password", Label: "旧 Key", Secret: true, Retired: true},
	}}

	out, err := schema.Validate(map[string]any{"token": "ref-1", "api_key": "ref-0"})
	if err != nil {
		t.Fatalf("撤掉的字段不该让老配置校验失败: %v", err)
	}
	// 不进归一化结果，用户下次保存配置时这个键就消失了。
	if _, ok := out["api_key"]; ok {
		t.Errorf("撤掉的字段不该出现在归一化后的配置里: %+v", out)
	}
	if out["token"] != "ref-1" {
		t.Errorf("在用的字段应照常输出: %+v", out)
	}
	// 已经不读的字段没理由还带着 reveal 权限。
	for _, field := range schema.SecretFields() {
		if field.Name == "api_key" {
			t.Error("撤掉的 secret 字段不该出现在 SecretFields 里")
		}
	}

	badRetired := ConfigSchema{Fields: []Field{
		{Name: "a", Type: "string", Label: "A", Required: true, Retired: true},
	}}
	if err := badRetired.validate("test"); err == nil {
		t.Error("retired + required 应报错")
	}
}

func TestSchemaSelfValidation(t *testing.T) {
	bad := ConfigSchema{Fields: []Field{{Name: "a", Type: "select", Label: "A"}}}
	if err := bad.validate("test"); err == nil {
		t.Error("select 无 options 应报错")
	}
	badMulti := ConfigSchema{Fields: []Field{{Name: "a", Type: "multiselect", Label: "A"}}}
	if err := badMulti.validate("test"); err == nil {
		t.Error("multiselect 无 options 应报错")
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

func TestSchemaGroupValidation(t *testing.T) {
	ok := ConfigSchema{
		Groups: []FieldGroup{{ID: "conn", Label: "连接"}, {ID: "advanced", Label: "高级", Collapsed: true}},
		Fields: []Field{
			{Name: "base_url", Type: "url", Label: "地址", Group: "conn", UI: &FieldUI{Width: "half"}},
			{Name: "timeout", Type: "number", Label: "超时", Group: "advanced"},
			{Name: "note", Type: "string", Label: "备注"},
		},
	}
	if err := ok.validate("test"); err != nil {
		t.Errorf("合法分组 schema 不应报错: %v", err)
	}
	parsed, err := ParseConfigSchema([]byte(`{
		"groups": [{"id": "conn", "label": "连接", "collapsed": true}],
		"fields": [{"name": "a", "type": "string", "label": "A", "group": "conn", "ui": {"width": "half"}}]
	}`))
	if err != nil || len(parsed.Groups) != 1 || !parsed.Groups[0].Collapsed ||
		parsed.Fields[0].Group != "conn" || parsed.Fields[0].UI.Width != "half" {
		t.Errorf("分组与布局字段应完整解析: %+v, %v", parsed, err)
	}

	unknownGroup := ConfigSchema{Fields: []Field{{Name: "a", Type: "string", Label: "A", Group: "missing"}}}
	if err := unknownGroup.validate("test"); err == nil {
		t.Error("引用未声明分组应报错")
	}
	dupGroup := ConfigSchema{
		Groups: []FieldGroup{{ID: "g", Label: "G"}, {ID: "g", Label: "G2"}},
	}
	if err := dupGroup.validate("test"); err == nil {
		t.Error("分组 id 重复应报错")
	}
	unnamedGroup := ConfigSchema{Groups: []FieldGroup{{ID: "g"}}}
	if err := unnamedGroup.validate("test"); err == nil {
		t.Error("分组缺 label 应报错")
	}
	badWidth := ConfigSchema{Fields: []Field{{Name: "a", Type: "string", Label: "A", UI: &FieldUI{Width: "third"}}}}
	if err := badWidth.validate("test"); err == nil {
		t.Error("非法 ui.width 应报错")
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
