// Package plugins 实现插件内核（docs/plugin-model.md §10-11）：
// Manifest、受限配置 schema、Registry 和配置校验。
// CLI 插件运行时（进程宿主、stdio JSON-RPC）后置，不在本包。
package pluginsdk

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	"media-agent-lab/server/pkg/pluginsdk/providers"
)

type Manifest struct {
	ID           string      `yaml:"id" json:"id"`
	Name         string      `yaml:"name" json:"name"`
	Version      string      `yaml:"version" json:"version"`
	Description  string      `yaml:"description" json:"description,omitempty"`
	Type         string      `yaml:"type" json:"type"` // builtin / cli / rule / ui
	Capabilities []string    `yaml:"capabilities" json:"capabilities"`
	Permissions  Permissions `yaml:"permissions" json:"permissions"`
	Resources    Resources   `yaml:"resources" json:"resources"`
}

type Permissions struct {
	Network []string `yaml:"network" json:"network"`
	Secrets []string `yaml:"secrets" json:"secrets"`
}

type Resources struct {
	MemoryLimitMB      int `yaml:"memory_limit_mb" json:"memory_limit_mb"`
	IdleTimeoutSeconds int `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
}

// SecretResolver 由宿主注入，插件按引用解密密钥；每次读取都会写审计。
type SecretResolver interface {
	Reveal(ctx context.Context, ref, reason string) (string, error)
}

// Instance 是一个已校验的连接实例配置（downloaders/media_servers 表中的一行）。
// Config 中 secret 字段的值是 secrets 表引用，需通过 SecretResolver 解密。
type Instance struct {
	ID     string
	Name   string
	Config map[string]any
}

// Plugin 是注册到内核的插件描述。官方插件在编译期构造；
// 将来第三方 CLI 插件由宿主解析 `plugin manifest` / `plugin config-schema` 输出后构造。
type Plugin struct {
	Manifest     Manifest
	ConfigSchema ConfigSchema
	// IconSVG 是插件图标（SVG 内容），可为空；由宿主经 /plugins/{id}/icon 提供给前端。
	IconSVG []byte

	// 工厂按能力可选实现；nil 表示插件不提供该类 Provider。
	NewDownloader  func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.DownloaderProvider, error)
	NewMediaServer func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MediaServerProvider, error)
	NewMetadata    func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MetadataProvider, error)
	NewSite        func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.SiteProvider, error)
	NewModel       func() providers.ModelProvider

	// FieldOptions 为 dynamic_options 的 select 字段提供运行时选项
	// （如从媒体服务器拉取媒体库列表）；nil 表示插件没有动态选项字段。
	FieldOptions func(ctx context.Context, inst Instance, secrets SecretResolver, field string) ([]Option, error)
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
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		panic("解析插件 manifest: " + err.Error())
	}
	return m
}
