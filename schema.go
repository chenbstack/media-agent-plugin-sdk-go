package pluginsdk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ConfigSchema 是受限的配置字段声明（docs/plugin-model.md §11）。
// 不用完整 JSON Schema：字段类型有限、前端一定能渲染、校验规则宿主完全可控。
type ConfigSchema struct {
	Fields []Field `json:"fields"`
	// Groups 声明字段分组：字段用 Field.Group 引用组 ID，前端按声明顺序
	// 渲染带标题的区块；未引用组的字段渲染在所有分组之前。分组只影响呈现，
	// 不改变字段校验和存储。
	Groups []FieldGroup `json:"groups,omitempty"`
}

// FieldGroup 是配置表单的一个呈现分组。
type FieldGroup struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Collapsed 为 true 时该组默认折叠（适合高级/低频配置）。
	Collapsed bool `json:"collapsed,omitempty"`
}

type Field struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // string / password / url / number / boolean / select / path
	Label       string   `json:"label"`
	Required    bool     `json:"required,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Default     any      `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Options     []Option `json:"options,omitempty"`
	AllowCustom bool     `json:"allow_custom,omitempty"`
	Multiline   bool     `json:"multiline,omitempty"`
	// DynamicOptions 表示 select 的选项可由插件实例在运行时补充
	// （宿主经 fields/{field}/options 端点调用 Plugin.FieldOptions）。
	// 此时取值校验放宽为任意非空字符串，前端显示刷新按钮。
	DynamicOptions bool            `json:"dynamic_options,omitempty"`
	ShowWhen       *FieldCondition `json:"show_when,omitempty"`
	// Group 引用 ConfigSchema.Groups 里某个组的 ID；空串表示不分组。
	Group string   `json:"group,omitempty"`
	UI    *FieldUI `json:"ui,omitempty"`
}

type FieldCondition struct {
	Field  string `json:"field"`
	Equals any    `json:"equals"`
}

type FieldUI struct {
	Placement         string `json:"placement,omitempty"`
	Browse            string `json:"browse,omitempty"`
	Gate              string `json:"gate,omitempty"`
	HideAccessPreview bool   `json:"hide_access_preview,omitempty"`
	// Width 控制字段在两列网格中的宽度："half"（半宽）或 "full"（整行，默认）。
	Width string `json:"width,omitempty"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

var fieldTypes = map[string]bool{
	"string": true, "password": true, "url": true, "number": true,
	"boolean": true, "select": true, "path": true,
}

func (s ConfigSchema) validate(pluginID string) error {
	groups := map[string]bool{}
	for _, g := range s.Groups {
		if g.ID == "" || g.Label == "" {
			return fmt.Errorf("插件 %s: 字段分组必须有 id 和 label", pluginID)
		}
		if groups[g.ID] {
			return fmt.Errorf("插件 %s: 字段分组 id 重复 %q", pluginID, g.ID)
		}
		groups[g.ID] = true
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		if f.Name == "" || f.Label == "" {
			return fmt.Errorf("插件 %s: 字段必须有 name 和 label", pluginID)
		}
		if seen[f.Name] {
			return fmt.Errorf("插件 %s: 字段名重复 %q", pluginID, f.Name)
		}
		seen[f.Name] = true
		if !fieldTypes[f.Type] {
			return fmt.Errorf("插件 %s: 字段 %s 类型未知 %q", pluginID, f.Name, f.Type)
		}
		if f.Type == "select" && len(f.Options) == 0 {
			return fmt.Errorf("插件 %s: select 字段 %s 必须有 options", pluginID, f.Name)
		}
		if f.Secret && f.Type != "password" && f.Type != "string" {
			return fmt.Errorf("插件 %s: secret 字段 %s 只能是 password 或 string 类型", pluginID, f.Name)
		}
		if f.Group != "" && !groups[f.Group] {
			return fmt.Errorf("插件 %s: 字段 %s 引用了未声明的分组 %q", pluginID, f.Name, f.Group)
		}
		if f.UI != nil && f.UI.Width != "" && f.UI.Width != "half" && f.UI.Width != "full" {
			return fmt.Errorf("插件 %s: 字段 %s 的 ui.width 只能是 half 或 full", pluginID, f.Name)
		}
	}
	return nil
}

// Field 按名查找。
func (s ConfigSchema) Field(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// SecretFields 返回所有 secret 字段。
func (s ConfigSchema) SecretFields() []Field {
	var out []Field
	for _, f := range s.Fields {
		if f.Secret {
			out = append(out, f)
		}
	}
	return out
}

// ValidationError 携带按字段的错误信息，API 层可直接放进 error.details。
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	var parts []string
	for name, msg := range e.Fields {
		parts = append(parts, name+": "+msg)
	}
	return "配置校验失败: " + strings.Join(parts, "; ")
}

// Validate 校验并归一化实例配置：
//   - 未声明的字段拒绝
//   - 必填、类型、select 取值校验
//   - 缺省字段填充 default
//
// secret 字段的值在此处是字符串（保存前为明文，保存后为 secret 引用），只做非空校验。
func (s ConfigSchema) Validate(config map[string]any) (map[string]any, error) {
	errs := map[string]string{}
	out := map[string]any{}

	for key := range config {
		if _, ok := s.Field(key); !ok {
			errs[key] = "未声明的字段"
		}
	}

	for _, f := range s.Fields {
		value, present := config[f.Name]
		if !present || value == nil || value == "" {
			if f.Default != nil {
				out[f.Name] = f.Default
				continue
			}
			if f.Required {
				errs[f.Name] = "必填"
			}
			continue
		}
		normalized, err := f.check(value)
		if err != "" {
			errs[f.Name] = err
			continue
		}
		out[f.Name] = normalized
	}

	if len(errs) > 0 {
		return nil, &ValidationError{Fields: errs}
	}
	return out, nil
}

func (f Field) check(value any) (any, string) {
	switch f.Type {
	case "string", "password", "path":
		str, ok := value.(string)
		if !ok {
			return nil, "应为字符串"
		}
		return str, ""
	case "url":
		str, ok := value.(string)
		if !ok {
			return nil, "应为字符串"
		}
		u, err := url.Parse(str)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, "应为 http(s) 地址"
		}
		return strings.TrimRight(str, "/"), ""
	case "number":
		switch n := value.(type) {
		case float64:
			return n, ""
		case int:
			return float64(n), ""
		case json.Number:
			v, err := n.Float64()
			if err != nil {
				return nil, "应为数字"
			}
			return v, ""
		default:
			return nil, "应为数字"
		}
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return nil, "应为布尔值"
		}
		return b, ""
	case "select":
		str, ok := value.(string)
		if !ok {
			return nil, "应为字符串"
		}
		for _, opt := range f.Options {
			if opt.Value == str {
				return str, ""
			}
		}
		// 动态选项或自定义选项无法在 schema 里穷举，放行非空取值。
		if (f.DynamicOptions || f.AllowCustom) && str != "" {
			return str, ""
		}
		return nil, "取值不在选项内"
	}
	return nil, "未知类型"
}

// MustParseConfigSchema 解析 go:embed 的 config.schema.json。
func MustParseConfigSchema(data []byte) ConfigSchema {
	s, err := ParseConfigSchema(data)
	if err != nil {
		panic("解析 config.schema.json: " + err.Error())
	}
	return s
}

// ParseConfigSchema 解析配置 schema。
func ParseConfigSchema(data []byte) (ConfigSchema, error) {
	var s ConfigSchema
	if err := json.Unmarshal(data, &s); err != nil {
		return ConfigSchema{}, err
	}
	return s, nil
}
