// Package plugins 实现插件内核（docs/plugin-model.md §10-11）：
// Manifest、受限配置 schema、Registry 和配置校验。
// CLI 插件运行时（进程宿主、stdio JSON-RPC）后置，不在本包。
package pluginsdk

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"media-agent-lab/server/pkg/pluginsdk/providers"
)

type Manifest struct {
	ID           string            `yaml:"id" json:"id"`
	Name         string            `yaml:"name" json:"name"`
	Version      string            `yaml:"version" json:"version"`
	Description  string            `yaml:"description" json:"description,omitempty"`
	Type         string            `yaml:"type" json:"type"` // builtin / cli / rule / ui
	Entry        map[string]string `yaml:"entry,omitempty" json:"entry,omitempty"`
	Protocol     string            `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Transport    string            `yaml:"transport,omitempty" json:"transport,omitempty"`
	ServeArgs    []string          `yaml:"serve_args,omitempty" json:"serve_args,omitempty"`
	StdioArgs    []string          `yaml:"stdio_args,omitempty" json:"stdio_args,omitempty"`
	Capabilities []string          `yaml:"capabilities" json:"capabilities"`
	Permissions  Permissions       `yaml:"permissions" json:"permissions"`
	Resources    Resources         `yaml:"resources" json:"resources"`
}

type Permissions struct {
	Network    []string               `yaml:"network" json:"network"`
	Secrets    []string               `yaml:"secrets" json:"secrets"`
	Data       []string               `yaml:"data,omitempty" json:"data,omitempty"`
	Host       []string               `yaml:"host,omitempty" json:"host,omitempty"`
	Filesystem []FilesystemPermission `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`
}

type FilesystemPermission struct {
	Path   string `yaml:"path" json:"path"`
	Access string `yaml:"access" json:"access"` // read / read_write
}

func (p Permissions) HasHost(permission string) bool {
	for _, value := range p.Host {
		if value == permission || value == "host:"+permission {
			return true
		}
	}
	return false
}

func (p Permissions) HasData(permission string) bool {
	for _, value := range p.Data {
		if value == permission || value == "data:"+permission {
			return true
		}
	}
	return false
}

type Resources struct {
	MemoryLimitMB      int `yaml:"memory_limit_mb" json:"memory_limit_mb"`
	IdleTimeoutSeconds int `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
}

// SecretResolver 由宿主注入，插件按引用解密密钥；每次读取都会写审计。
type SecretResolver interface {
	Reveal(ctx context.Context, ref, reason string) (string, error)
}

// KVStore 是宿主为单个插件实例注入的轻量 JSON KV 存储。
// key 在该插件实例内唯一；ttl <= 0 表示不过期。
type KVStore interface {
	Get(ctx context.Context, key string, out any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

// Instance 是一个已校验的连接实例配置（downloaders/media_servers 表中的一行）。
// Config 中 secret 字段的值是 secrets 表引用，需通过 SecretResolver 解密。
type Instance struct {
	ID     string
	Name   string
	Config map[string]any
	KV     KVStore
	DB     PluginDB
	Logger Logger
}

// AuthStartResult 是插件交互式认证流程的启动结果。
type AuthStartResult struct {
	Flow        string `json:"flow"`
	SessionID   string `json:"session_id"`
	CodeContent string `json:"code_content,omitempty"`
	CodeURL     string `json:"code_url,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Message     string `json:"message,omitempty"`
}

// AuthCheckResult 是插件交互式认证流程的轮询结果。
// Config 中返回的明文字段由前端合并到当前配置表单，随后走原有保存流程入库和加密。
type AuthCheckResult struct {
	Status  string         `json:"status"`
	Message string         `json:"message,omitempty"`
	Config  map[string]any `json:"config,omitempty"`
}

// Plugin 是注册到内核的插件描述。官方插件在编译期构造；
// 将来第三方 CLI 插件由宿主解析 `plugin manifest` / `plugin config-schema` 输出后构造。
type Plugin struct {
	Manifest     Manifest
	ConfigSchema ConfigSchema
	// IconSVG 是插件图标（SVG 内容），可为空；由宿主经 /plugins/{id}/icon 提供给前端。
	IconSVG []byte

	// 工厂按能力可选实现；nil 表示插件不提供该类 Provider。
	NewStorage     func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.StorageProvider, error)
	NewDownloader  func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.DownloaderProvider, error)
	NewMediaServer func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MediaServerProvider, error)
	NewMetadata    func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MetadataProvider, error)
	NewSite        func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.SiteProvider, error)
	NewModel       func() providers.ModelProvider

	// FieldOptions 为 dynamic_options 的 select 字段提供运行时选项
	// （如从媒体服务器拉取媒体库列表）；nil 表示插件没有动态选项字段。
	FieldOptions func(ctx context.Context, inst Instance, secrets SecretResolver, field string) ([]Option, error)

	// StartAuth / CheckAuth 为插件提供通用交互式认证流程，如扫码登录。
	StartAuth func(ctx context.Context, inst Instance, flow string) (AuthStartResult, error)
	CheckAuth func(ctx context.Context, inst Instance, flow, sessionID string) (AuthCheckResult, error)

	// ValidateConfig 在通用 schema 校验之后运行，供插件按其他字段或外部资源包
	// 做二次校验；例如站点插件按 base_url 匹配资源包后校验认证字段。
	ValidateConfig func(config map[string]any) error

	// ConfigSchemaForConfig 根据当前配置返回有效 schema。用于字段集合需要依赖
	// 其他字段或资源包的插件；nil 表示始终使用 ConfigSchema。
	ConfigSchemaForConfig func(config map[string]any) ConfigSchema
}

func (p Plugin) validate() error {
	m := p.Manifest
	if m.ID == "" || m.Name == "" || m.Version == "" {
		return fmt.Errorf("manifest 必须包含 id、name、version")
	}
	switch m.Type {
	case "builtin", "cli", "rule", "ui":
	default:
		return fmt.Errorf("插件 %s: 未知 type %q", m.ID, m.Type)
	}
	if m.Type == "cli" && m.Resources.MemoryLimitMB <= 0 {
		return fmt.Errorf("插件 %s: CLI 插件必须声明正数 memory_limit_mb", m.ID)
	}
	if len(m.Capabilities) == 0 {
		return fmt.Errorf("插件 %s: 必须声明至少一个 capability", m.ID)
	}
	return p.ConfigSchema.validate(m.ID)
}

// HasCapability 判断插件是否声明了某能力域（如 "downloader" 匹配 "downloader.add"）。
func (p Plugin) HasCapability(domain string) bool {
	for _, c := range p.Manifest.Capabilities {
		if c == domain || len(c) > len(domain) && c[:len(domain)] == domain && c[len(domain)] == '.' {
			return true
		}
	}
	return false
}

// MustParseManifest 解析 go:embed 的 manifest.yaml，用于官方插件编译期声明。
func MustParseManifest(data []byte) Manifest {
	m, err := ParseManifest(data)
	if err != nil {
		panic("解析插件 manifest: " + err.Error())
	}
	return m
}

// ParseManifest 解析插件 manifest.yaml / plugin.yaml。
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
