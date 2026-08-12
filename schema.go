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
	Name        string `json:"name"`
	Type        string `json:"type"` // string / password / url / number / boolean / select / multiselect / path
	Label       string `json:"label"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Default     any    `json:"default,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	// Options 是 select / multiselect 的候选项。multiselect 的取值是字符串数组，
	// 归一化后按 Options 的声明顺序排列——存进来的顺序取决于用户点选的先后，
	// 让它决定语言优先级之类的语义，会让同一份配置在两次编辑后行为不同。
	Options     []Option `json:"options,omitempty"`
	AllowCustom bool     `json:"allow_custom,omitempty"`
	Multiline   bool     `json:"multiline,omitempty"`
	// DynamicOptions 表示 select 的选项可由插件实例在运行时补充
	// （宿主经 fields/{field}/options 端点调用 Plugin.FieldOptions）。
	// 此时取值校验放宽为任意非空字符串，前端显示刷新按钮。
	DynamicOptions bool            `json:"dynamic_options,omitempty"`
	ShowWhen       *FieldCondition `json:"show_when,omitempty"`
	// Retired 声明一个已经撤掉的字段：不渲染、不校验取值、也不进归一化后的配置。
	//
	// 撤掉一个配置项时，已装实例的配置里还留着它，而 Validate 遇到 schema 没声明的
	// 键会判「未声明的字段」——用户一打开设置页就是一片红，连保存都保存不了。声明成
	// Retired 就只是让它安静通过，用户下次保存配置时这个键自然消失。
	//
	// 一般配合 Plugin.ConfigSchemaForConfig 用：只在配置里确实还有这个键时才追加，
	// 免得新装的实例也背着一堆历史包袱。
	Retired bool `json:"retired,omitempty"`
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

// BrowseStorageInstance 让 select 字段渲染成"选一个已配置的存储"，取值是存储 id。
//
// 候选项由宿主界面直接取存储列表填充，因此插件既不用实现 FieldOptions，也不用为了
// 填一个下拉框去要 storages.read。取值同理无法在 schema 里穷举，见 optionAllows。
const BrowseStorageInstance = "storage.instance"

// BrowseConnectionMediaServer 让 select 字段引用宿主已有的媒体服务器连接；追加
// ".<kind>"（例如 connection.media_server.emby）可由界面按连接类型过滤。
const BrowseConnectionMediaServer = "connection.media_server"

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

var fieldTypes = map[string]bool{
	"string": true, "password": true, "url": true, "number": true,
	"boolean": true, "select": true, "multiselect": true, "path": true,
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
		if f.Retired {
			// 撤掉的字段不渲染也不校验取值，剩下的规则对它没有意义。
			if f.Required {
				return fmt.Errorf("插件 %s: 字段 %s 不能同时是 retired 和 required", pluginID, f.Name)
			}
			continue
		}
		if (f.Type == "select" || f.Type == "multiselect") && len(f.Options) == 0 && !f.optionsFilledByHost() {
			return fmt.Errorf("插件 %s: %s 字段 %s 必须有 options", pluginID, f.Type, f.Name)
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

// SecretFields 返回所有 secret 字段。撤掉的字段不算——宿主拿这个列表决定插件能
// reveal 哪些密钥引用，已经不读的字段没理由还留着这份权限。
func (s ConfigSchema) SecretFields() []Field {
	var out []Field
	for _, f := range s.Fields {
		if f.Secret && !f.Retired {
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
//   - 必填、类型、select / multiselect 取值校验
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
		if f.Retired {
			// 只是让老配置里的这个键不被判成「未声明的字段」；取值不校验，也不
			// 往 out 里放——用户下次保存配置时它就消失了。
			continue
		}
		value, present := config[f.Name]
		if !present || isBlank(value) {
			if f.Default != nil {
				out[f.Name] = f.defaultValue()
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

// defaultValue 返回填充用的缺省值。
//
// multiselect 的缺省值在 config.schema.json 里是 JSON 数组，解析出来是 []any；
// 用户勾选过的实例存的却是 []string。同一个字段两种形状，插件侧就得两边都认。
// 这里把缺省值也过一遍 check 抹平——其它类型维持原样，缺省值不做校验是既有行为，
// 没必要借这次改动一起翻。
func (f Field) defaultValue() any {
	if f.Type != "multiselect" {
		return f.Default
	}
	normalized, err := f.check(f.Default)
	if err != "" {
		// 缺省值写错是插件自己的问题，但校验期不是报它的地方（这条路径上用户
		// 一个字都没填）。原样返回，让 validate() 那边的 options 检查去暴露。
		return f.Default
	}
	return normalized
}

// isBlank 认「用户什么都没填」，好让 default 填充和 required 校验对所有类型一致。
// multiselect 全不勾时前端交上来的是空数组，那跟空字符串是同一件事——不这么认的话，
// 空数组会绕过 required 直接存下去。
func isBlank(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []string:
		return len(v) == 0
	case []any:
		return len(v) == 0
	}
	return false
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
		if !f.optionAllows(str) {
			return nil, "取值不在选项内"
		}
		return str, ""
	case "multiselect":
		items, ok := stringList(value)
		if !ok {
			return nil, "应为字符串列表"
		}
		seen := map[string]bool{}
		picked := map[string]bool{}
		for _, item := range items {
			if !f.optionAllows(item) {
				return nil, "取值不在选项内"
			}
			seen[item] = true
		}
		// 按 Options 的声明顺序输出，勾选先后不参与决定顺序（见 Field.Options 注释）。
		out := make([]string, 0, len(seen))
		for _, opt := range f.Options {
			if seen[opt.Value] && !picked[opt.Value] {
				picked[opt.Value] = true
				out = append(out, opt.Value)
			}
		}
		// 自定义/动态选项排在声明项之后，它们之间保留输入顺序。
		for _, item := range items {
			if !picked[item] {
				picked[item] = true
				out = append(out, item)
			}
		}
		return out, ""
	}
	return nil, "未知类型"
}

// optionAllows 判断单个取值是否落在候选项里。动态选项、自定义选项，以及由宿主界面
// 填充候选项的 browse 字段都无法在 schema 里穷举，放行非空取值。
func (f Field) optionAllows(value string) bool {
	for _, opt := range f.Options {
		if opt.Value == value {
			return true
		}
	}
	return (f.DynamicOptions || f.AllowCustom || f.optionsFilledByHost()) && value != ""
}

// optionsFilledByHost 表示候选项来自宿主界面而非 schema 声明。
func (f Field) optionsFilledByHost() bool {
	if f.UI == nil {
		return false
	}
	return f.UI.Browse == BrowseStorageInstance ||
		f.UI.Browse == BrowseConnectionMediaServer ||
		strings.HasPrefix(f.UI.Browse, BrowseConnectionMediaServer+".")
}

// stringList 把 multiselect 的取值归一成字符串切片。
//
// []any 是过一趟 JSON 之后的形状；逗号分隔的字符串是这个字段从 string 改成
// multiselect 之前存下来的旧配置——不认它的话，升级后老实例会卡在「应为字符串列表」
// 上，用户得手动重填一遍才能保存。
func stringList(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, str)
		}
		return out, true
	case string:
		var out []string
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out, true
	}
	return nil, false
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
