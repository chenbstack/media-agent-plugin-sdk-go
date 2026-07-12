// Package plugins 实现插件内核（docs/plugin-model.md §10-11）：
// Manifest、受限配置 schema、Registry 和配置校验。
// CLI 插件运行时（进程宿主、stdio JSON-RPC）后置，不在本包。
package pluginsdk

import (
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
	runtimesdk "github.com/chenbstack/media-agent-plugin-sdk-go/runtime"
)

const (
	CategorySiteMetadata = "site-metadata"
	CategoryDownloader   = "downloader"
	CategoryMediaServer  = "media-server"
	CategoryStorage      = "storage"
	CategorySubtitle     = "subtitle"
	CategoryNotification = "notification"
	CategoryAIModel      = "ai-model"
	CategoryAutomation   = "automation"
	CategoryOther        = "other"

	CapabilityOnboardingAssessment = "onboarding.assess"
)

type Manifest struct {
	ID            string              `yaml:"id" json:"id"`
	Name          string              `yaml:"name" json:"name"`
	Version       string              `yaml:"version" json:"version"`
	Description   string              `yaml:"description" json:"description,omitempty"`
	Category      string              `yaml:"category,omitempty" json:"category,omitempty"`
	Tags          []string            `yaml:"tags,omitempty" json:"tags,omitempty"`
	Type          string              `yaml:"type" json:"type"` // builtin / cli / rule / ui
	Entry         map[string]string   `yaml:"entry,omitempty" json:"entry,omitempty"`
	Protocol      string              `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Transport     string              `yaml:"transport,omitempty" json:"transport,omitempty"`
	ServeArgs     []string            `yaml:"serve_args,omitempty" json:"serve_args,omitempty"`
	StdioArgs     []string            `yaml:"stdio_args,omitempty" json:"stdio_args,omitempty"`
	Capabilities  []string            `yaml:"capabilities" json:"capabilities"`
	Subscriptions []EventSubscription `yaml:"subscriptions,omitempty" json:"subscriptions,omitempty"`
	API           *APIExtension       `yaml:"api,omitempty" json:"api,omitempty"`
	UI            *UIExtension        `yaml:"ui,omitempty" json:"ui,omitempty"`
	Identity      *IdentityExtension  `yaml:"identity,omitempty" json:"identity,omitempty"`
	Entitlements  []string            `yaml:"entitlements,omitempty" json:"entitlements,omitempty"`
	Actions       []ActionDefinition  `yaml:"actions,omitempty" json:"actions,omitempty"`
	Permissions   Permissions         `yaml:"permissions" json:"permissions"`
	Resources     Resources           `yaml:"resources" json:"resources"`
	Install       *InstallInfo        `yaml:"install,omitempty" json:"install,omitempty"`
}

// APIExtension 声明由宿主代理的插件业务 API。Service 会成为
// /api/v1/plugin-services/{plugin_id}/{service}/... 中的 service 段。
type APIExtension struct {
	Service              string      `yaml:"service" json:"service"`
	Auth                 APIAuthMode `yaml:"auth,omitempty" json:"auth,omitempty"`
	RequiredEntitlements []string    `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
}

type APIAuthMode string

const (
	APIAuthSession APIAuthMode = "session"
	APIAuthNone    APIAuthMode = "none"

	CapabilityAPIEndpoint      = "api.endpoint"
	CapabilityUIModule         = "ui.module"
	CapabilityIdentityProvider = "identity.provider"
)

// UIExtension 声明随已验签制品分发的前端模块及其页面。Module 必须是制品内的
// 相对路径，不能是远程 URL；宿主仍需根据签名和发布策略决定是否允许同源加载。
type UIExtension struct {
	Module string    `yaml:"module" json:"module"`
	Routes []UIRoute `yaml:"routes" json:"routes"`
}

// UIRoute 是插件前端模块导出的一个页面。默认路由应位于
// /plugin/{plugin_id}/ 下；可信插件的顶级别名由宿主额外授权，SDK 不判断发布者信任。
type UIRoute struct {
	ID                   string   `yaml:"id" json:"id"`
	Path                 string   `yaml:"path" json:"path"`
	Export               string   `yaml:"export" json:"export"`
	RequiredEntitlements []string `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
	Menu                 *UIMenu  `yaml:"menu,omitempty" json:"menu,omitempty"`
}

// UIMenu 声明页面在宿主导航中的位置。Icon 是宿主提供的稳定图标 ID，不能是源码
// 或任意资源 URL。
type UIMenu struct {
	Section string `yaml:"section" json:"section"`
	Label   string `yaml:"label" json:"label"`
	Icon    string `yaml:"icon" json:"icon"`
	Order   int    `yaml:"order,omitempty" json:"order,omitempty"`
}

// IdentityExtension 声明插件身份 Provider 的 RPC service 名称及启用它所需的权益。
// Session 签发、CSRF 和找回入口始终由宿主负责。
type IdentityExtension struct {
	Service              string   `yaml:"service,omitempty" json:"service,omitempty"`
	RequiredEntitlements []string `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
}

// APIRequest is the bounded HTTP-like request delivered to an api.endpoint
// plugin. The host owns routing, authentication, entitlement checks and input
// limits: Path must already be canonical and relative to the declared service,
// Headers must contain only the host allowlist, and Body must already satisfy
// the host's size limit. Raw http.Request, cookies and the host Authorization
// header are intentionally never part of this contract.
type APIRequest struct {
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     map[string][]string `json:"query,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      []byte              `json:"body,omitempty"`
	Principal *Principal          `json:"principal,omitempty"`
}

// APIResponse is a non-streaming plugin response. The host must validate the
// status, cap Body and filter response headers before writing it to the client;
// hop-by-hop headers, Set-Cookie and authentication headers are never trusted.
type APIResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// APIProvider handles short, structured api.endpoint calls. Long-running,
// streaming and WebSocket services require a separate sidecar contract.
type APIProvider interface {
	HandleAPI(ctx context.Context, request APIRequest) (APIResponse, error)
}

// IdentityVerifyRequest contains only a credential scheme and its minimum
// fields. Credential is plaintext for the duration of this RPC and must never
// be persisted or logged. Examples of Scheme are password, bearer and code.
type IdentityVerifyRequest struct {
	Scheme     string `json:"scheme"`
	Identifier string `json:"identifier,omitempty"`
	Credential string `json:"credential"`
}

// Principal is the minimum stable identity returned by an IdentityProvider.
// Authorization roles, arbitrary claims and host session data are deliberately
// excluded: the host maps this identity to its own authorization model.
type Principal struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

// IdentityVerification reports credential verification only. A successful
// result does not contain or authorize a session token; the host validates the
// principal and remains the sole session/CSRF signer.
type IdentityVerification struct {
	Authenticated bool       `json:"authenticated"`
	Principal     *Principal `json:"principal,omitempty"`
}

type IdentityProvider interface {
	VerifyIdentity(ctx context.Context, request IdentityVerifyRequest) (IdentityVerification, error)
}

// InstallInfo 是插件对其自举安装步骤（capability lifecycle.install）的自我描述。
// 宿主不理解安装内容，安装区块的标题与说明都取自这里，而非宿主硬编码文案。
//
// 一个插件可声明多个可安装组件（Components，如浏览器仿真插件的"轻量引擎"与"隐身
// Chromium"），各自独立安装/检查/卸载。为向后兼容，只有单个安装目标的插件可直接用
// 顶层 Title/Description，宿主归一化为一个 id 为空串的默认组件（见 Manifest.InstallComponents）。
type InstallInfo struct {
	// Title 是（单组件插件的）安装区块标题（如"浏览器引擎"），由插件按其安装内容命名。
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Description 向用户说明这一步会做什么、为何需要；可含手动触发/重试的提示。
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Components 声明多个可独立安装的组件；非空时忽略上面的单数 Title/Description。
	Components []ComponentInfo `yaml:"components,omitempty" json:"components,omitempty"`
}

// ComponentInfo 是插件对某个可安装组件（资源）的自我描述。宿主原样展示其标题/说明，
// 并按 ID 路由安装、检查、卸载；一个插件的多个组件互不影响。
type ComponentInfo struct {
	// ID 是组件稳定标识（安装/卸载/日志路由用）；默认组件用空串。
	ID string `yaml:"id" json:"id"`
	// Title 是该组件的安装区块标题（如"隐身 Chromium"）。
	Title string `yaml:"title" json:"title"`
	// Description 说明该组件会安装什么、为何需要。
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Uninstallable 为 true 表示该组件的资源可被卸载（前端显示"卸载"按钮）。
	Uninstallable bool `yaml:"uninstallable,omitempty" json:"uninstallable,omitempty"`
	// AutoInstall 为 true 表示该组件在插件启用后由宿主自动预装（适合体积小、默认必需的资源，
	// 如默认引擎）；为 false 则仅用户在详情页手动安装（适合体积大、按需启用的资源）。
	AutoInstall bool `yaml:"auto_install,omitempty" json:"auto_install,omitempty"`
}

// InstallComponents 返回插件声明的可安装组件列表，已归一化：优先 install.components；
// 否则若用了单数 install.title/description，归一为一个 id 为空串的默认组件；都没有则返回 nil。
func (m Manifest) InstallComponents() []ComponentInfo {
	if m.Install == nil {
		return nil
	}
	if len(m.Install.Components) > 0 {
		return m.Install.Components
	}
	if m.Install.Title != "" || m.Install.Description != "" {
		return []ComponentInfo{{ID: "", Title: m.Install.Title, Description: m.Install.Description}}
	}
	return nil
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

type EventSubscription struct {
	Type    string `yaml:"type" json:"type"`
	Version int    `yaml:"version" json:"version"`
	Phase   string `yaml:"phase,omitempty" json:"phase,omitempty"`
	Mode    string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type EventResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type EventActor struct {
	Type   string `json:"type"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
}

type EventEnvelope struct {
	EventID    string         `json:"event_id"`
	Type       string         `json:"type"`
	Version    int            `json:"version"`
	Phase      string         `json:"phase,omitempty"`
	OccurredAt string         `json:"occurred_at"`
	Actor      EventActor     `json:"actor,omitempty"`
	Resource   EventResource  `json:"resource,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type EventSubscriber interface {
	HandleEvent(ctx context.Context, event EventEnvelope) error
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
	ID            string
	Name          string
	Config        map[string]any
	KV            KVStore
	DB            PluginDB
	Logger        Logger
	Settings      Settings
	SiteAccounts  SiteAccounts
	Subscriptions Subscriptions
	Downloads     Downloads
	Transfers     Transfers
	Rules         Rules
	Configuration Configuration
	Runtime       *runtimesdk.Services
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

type OnboardingAssessmentStatus string

const (
	OnboardingNeedsSetup OnboardingAssessmentStatus = "needs_setup"
	OnboardingSatisfied  OnboardingAssessmentStatus = "satisfied"
)

// OnboardingAssessment is a plugin-owned, read-only decision about whether one
// persisted instance already satisfies the plugin's first-run setup. The host
// owns visibility and navigation; plugins only report semantic readiness.
type OnboardingAssessment struct {
	Status OnboardingAssessmentStatus `json:"status"`
	Reason string                     `json:"reason,omitempty"`
}

func (a OnboardingAssessment) Validate() error {
	switch a.Status {
	case OnboardingNeedsSetup, OnboardingSatisfied:
		return nil
	default:
		return fmt.Errorf("invalid onboarding assessment status %q", a.Status)
	}
}

// InstallResult 是插件自举安装（Install）或安装检查（CheckInstall）的结果。
// 宿主只负责触发和记录，不理解安装内容。
type InstallResult struct {
	// 对 Install：Installed 为 true 表示本次真正执行了安装（如下载了引擎二进制），
	// 为 false 表示调用前已就绪、本次未执行安装动作。
	// 对 CheckInstall：Installed 为 true 表示插件已安装就绪，false 表示尚未安装。
	Installed bool `json:"installed"`
	// Message 是可读的安装结果，宿主写入安装状态并展示给用户。
	Message string `json:"message,omitempty"`
}

// Plugin 是注册到内核的插件描述。官方插件在编译期构造；
// 将来第三方 CLI 插件由宿主解析 `plugin manifest` / `plugin config-schema` 输出后构造。
type Plugin struct {
	Manifest     Manifest
	ConfigSchema ConfigSchema
	// IconSVG 是插件图标（SVG 内容），可为空；由宿主经 /plugins/{id}/icon 提供给前端。
	IconSVG []byte

	// 工厂按能力可选实现；nil 表示插件不提供该类 Provider。
	NewStorage         func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.StorageProvider, error)
	NewDownloader      func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.DownloaderProvider, error)
	NewMediaServer     func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MediaServerProvider, error)
	NewMetadata        func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MetadataProvider, error)
	NewSite            func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.SiteProvider, error)
	NewCookieSource    func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.CookieSourceProvider, error)
	NewModel           func() providers.ModelProvider
	NewEventSubscriber func(ctx context.Context, inst Instance, secrets SecretResolver) (EventSubscriber, error)
	NewNotifier        func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.NotifierProvider, error)
	NewSubtitleSource  func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.SubtitleSourceProvider, error)
	NewRenderer        func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.RendererProvider, error)
	NewAPI             func(ctx context.Context, inst Instance, secrets SecretResolver) (APIProvider, error)
	NewIdentity        func(ctx context.Context, inst Instance, secrets SecretResolver) (IdentityProvider, error)
	NewActionHandler   func(ctx context.Context, inst Instance, secrets SecretResolver) (ActionHandler, error)
	AssessOnboarding   func(ctx context.Context, inst Instance, secrets SecretResolver) (OnboardingAssessment, error)

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

	// Install 是插件自举安装钩子（capability lifecycle.install）。用于插件下载运行所需
	// 的外部资源（如浏览器引擎二进制）。宿主只负责触发、记录状态并向用户展示进度，不
	// 理解安装内容；安装逻辑在插件进程内运行，使用插件进程自身的网络与文件权限。
	//
	// progress 是进度接收器：插件应把可读的进度按行写入（每行一条），宿主实时转发给
	// 前端展示。对外部插件，插件进程写到自己的 stderr 即等价于写入 progress。
	//
	// 插件必须幂等：已就绪时快速返回 InstallResult{Installed: false} 且不产生副作用；
	// 安装失败后可被反复调用，插件需自行清理半成品（如临时下载文件）。nil 表示插件无
	// 需安装步骤。
	Install func(ctx context.Context, progress io.Writer) (InstallResult, error)

	// CheckInstall 查询插件是否已安装就绪，只读、无副作用、不触发下载。宿主在插件加载时
	// 调用它决定初始安装状态（installed / pending），避免每次启动都执行安装动作。声明了
	// lifecycle.install 的插件应实现它；nil 时宿主退化为把状态标记为 pending。
	CheckInstall func(ctx context.Context) (InstallResult, error)

	// Uninstall 卸载 Install 下载的资源（如引擎二进制），回收磁盘空间。宿主在用户手动
	// 卸载、或停用插件时调用；同 Install 一样只转发、记录状态并展示进度（progress 按行
	// 写入）。必须幂等：无资源可卸时返回 UninstallResult{Removed: false}。nil 表示插件
	// 无可卸载资源。
	//
	// 上面的 Install/CheckInstall/Uninstall 对应"默认组件"（id 为空串）。声明了多个可安装
	// 组件的插件（见 Manifest.Install.Components）把额外组件的钩子放进 InstallComponents，
	// 按 ID 路由；宿主用 InstallHooks(id) 统一取用。
	Uninstall func(ctx context.Context, progress io.Writer) (UninstallResult, error)

	// InstallComponents 是"非默认组件"的安装钩子集合，按 ID 匹配 Manifest.Install.Components。
	InstallComponents []InstallComponent
}

// InstallComponent 是单个可安装组件的运行时钩子集合，语义同 Plugin.Install/CheckInstall/
// Uninstall，但作用于指定 ID 的组件。Uninstall 为 nil 表示该组件资源不可卸载。
type InstallComponent struct {
	ID           string
	Install      func(ctx context.Context, progress io.Writer) (InstallResult, error)
	CheckInstall func(ctx context.Context) (InstallResult, error)
	Uninstall    func(ctx context.Context, progress io.Writer) (UninstallResult, error)
}

// InstallHooks 返回给定组件 ID 的安装钩子；空 ID 命中默认（Install/CheckInstall/Uninstall）。
// ok 为 false 表示该组件不存在或未提供任何钩子。
func (p Plugin) InstallHooks(component string) (InstallComponent, bool) {
	if component == "" {
		if p.Install == nil && p.CheckInstall == nil && p.Uninstall == nil {
			return InstallComponent{}, false
		}
		return InstallComponent{ID: "", Install: p.Install, CheckInstall: p.CheckInstall, Uninstall: p.Uninstall}, true
	}
	for _, c := range p.InstallComponents {
		if c.ID == component {
			return c, true
		}
	}
	return InstallComponent{}, false
}

// UninstallResult 是插件卸载下载资源（Uninstall）的结果。
type UninstallResult struct {
	// Removed 为 true 表示本次真正删除了资源；false 表示调用前已无资源可卸。
	Removed bool `json:"removed"`
	// Message 是可读的卸载结果，宿主写入状态并展示给用户。
	Message string `json:"message,omitempty"`
}

// Validate 校验插件 manifest、全栈扩展声明和配置 schema。宿主应在信任或加载
// 插件资源前调用；Registry.Register/Upsert 也会自动执行相同校验。
func (p Plugin) Validate() error {
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
	capabilities := make(map[string]struct{}, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if !manifestIdentifier.MatchString(capability) {
			return fmt.Errorf("插件 %s: capability %q 格式无效", m.ID, capability)
		}
		if _, exists := capabilities[capability]; exists {
			return fmt.Errorf("插件 %s: capability 重复 %q", m.ID, capability)
		}
		capabilities[capability] = struct{}{}
	}
	for _, sub := range m.Subscriptions {
		if sub.Type == "" {
			return fmt.Errorf("插件 %s: event subscription 必须包含 type", m.ID)
		}
		if sub.Version <= 0 {
			return fmt.Errorf("插件 %s: event subscription %s 必须包含正数 version", m.ID, sub.Type)
		}
	}
	if err := m.validateExtensions(capabilities); err != nil {
		return err
	}
	seenActions := map[string]bool{}
	for _, action := range m.Actions {
		if action.ID == "" || action.Name == "" {
			return fmt.Errorf("插件 %s: action 必须包含 id 和 name", m.ID)
		}
		if seenActions[action.ID] {
			return fmt.Errorf("插件 %s: action id 重复 %q", m.ID, action.ID)
		}
		seenActions[action.ID] = true
		if action.Permissions != nil {
			if err := validatePermissionSubset(m.Permissions, *action.Permissions); err != nil {
				return fmt.Errorf("插件 %s action %s 权限声明无效: %w", m.ID, action.ID, err)
			}
		}
	}
	return p.ConfigSchema.validate(m.ID)
}

func (p Plugin) validate() error { return p.Validate() }

var manifestIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (m Manifest) validateExtensions(capabilities map[string]struct{}) error {
	declaredEntitlements, err := validateEntitlements(m.ID, "manifest", m.Entitlements, nil)
	if err != nil {
		return err
	}

	_, hasAPI := capabilities[CapabilityAPIEndpoint]
	if hasAPI && m.API == nil {
		return fmt.Errorf("插件 %s: capability api.endpoint 必须声明 api", m.ID)
	}
	if m.API != nil {
		if !hasAPI {
			return fmt.Errorf("插件 %s: 声明 api 时必须包含 capability api.endpoint", m.ID)
		}
		if !manifestIdentifier.MatchString(m.API.Service) {
			return fmt.Errorf("插件 %s: api.service %q 格式无效", m.ID, m.API.Service)
		}
		if m.API.Auth != "" && m.API.Auth != APIAuthSession && m.API.Auth != APIAuthNone {
			return fmt.Errorf("插件 %s: api.auth 只支持 session 或 none", m.ID)
		}
		if _, err := validateEntitlements(m.ID, "api", m.API.RequiredEntitlements, declaredEntitlements); err != nil {
			return err
		}
	}

	_, hasUI := capabilities[CapabilityUIModule]
	if hasUI && m.UI == nil {
		return fmt.Errorf("插件 %s: capability ui.module 必须声明 ui", m.ID)
	}
	if m.UI != nil {
		if !hasUI {
			return fmt.Errorf("插件 %s: 声明 ui 时必须包含 capability ui.module", m.ID)
		}
		if err := validateAssetPath(m.UI.Module); err != nil {
			return fmt.Errorf("插件 %s: ui.module %w", m.ID, err)
		}
		if len(m.UI.Routes) == 0 {
			return fmt.Errorf("插件 %s: ui.routes 不能为空", m.ID)
		}
		routeIDs := make(map[string]struct{}, len(m.UI.Routes))
		routePaths := make(map[string]struct{}, len(m.UI.Routes))
		for _, route := range m.UI.Routes {
			if !manifestIdentifier.MatchString(route.ID) {
				return fmt.Errorf("插件 %s: ui route id %q 格式无效", m.ID, route.ID)
			}
			if _, exists := routeIDs[route.ID]; exists {
				return fmt.Errorf("插件 %s: ui route id 重复 %q", m.ID, route.ID)
			}
			routeIDs[route.ID] = struct{}{}
			if !validRoutePath(route.Path) {
				return fmt.Errorf("插件 %s: ui route %s 的 path %q 格式无效", m.ID, route.ID, route.Path)
			}
			if _, exists := routePaths[route.Path]; exists {
				return fmt.Errorf("插件 %s: ui route path 重复 %q", m.ID, route.Path)
			}
			routePaths[route.Path] = struct{}{}
			if !manifestIdentifier.MatchString(route.Export) {
				return fmt.Errorf("插件 %s: ui route %s 的 export %q 格式无效", m.ID, route.ID, route.Export)
			}
			if _, err := validateEntitlements(m.ID, "ui route "+route.ID, route.RequiredEntitlements, declaredEntitlements); err != nil {
				return err
			}
			if route.Menu != nil {
				if !manifestIdentifier.MatchString(route.Menu.Section) || !manifestIdentifier.MatchString(route.Menu.Icon) || strings.TrimSpace(route.Menu.Label) == "" {
					return fmt.Errorf("插件 %s: ui route %s 的 menu 必须包含合法 section、label、icon", m.ID, route.ID)
				}
			}
		}
	}

	_, hasIdentity := capabilities[CapabilityIdentityProvider]
	if m.Identity != nil {
		if !hasIdentity {
			return fmt.Errorf("插件 %s: 声明 identity 时必须包含 capability identity.provider", m.ID)
		}
		if m.Identity.Service != "" && !manifestIdentifier.MatchString(m.Identity.Service) {
			return fmt.Errorf("插件 %s: identity.service %q 格式无效", m.ID, m.Identity.Service)
		}
		if _, err := validateEntitlements(m.ID, "identity", m.Identity.RequiredEntitlements, declaredEntitlements); err != nil {
			return err
		}
	}
	return nil
}

func validateAssetPath(value string) error {
	if value == "" || strings.ContainsAny(value, "\\:%?#") || strings.HasPrefix(value, "/") || strings.TrimSpace(value) != value {
		return fmt.Errorf("必须是制品内相对路径")
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return fmt.Errorf("必须是规范化且不能越界的相对路径")
	}
	return nil
}

func validRoutePath(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.ContainsAny(value, "\\:%?#") && strings.TrimSpace(value) == value && path.Clean(value) == value
}

func validateEntitlements(pluginID, owner string, values []string, declared map[string]struct{}) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !manifestIdentifier.MatchString(value) {
			return nil, fmt.Errorf("插件 %s: %s entitlement %q 格式无效", pluginID, owner, value)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("插件 %s: %s entitlement 重复 %q", pluginID, owner, value)
		}
		if declared != nil {
			if _, exists := declared[value]; !exists {
				return nil, fmt.Errorf("插件 %s: %s 使用了未在 manifest 声明的 entitlement %q", pluginID, owner, value)
			}
		}
		seen[value] = struct{}{}
	}
	return seen, nil
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

// HasExactCapability 判断插件是否声明了某个完整 capability。
func (p Plugin) HasExactCapability(capability string) bool {
	for _, c := range p.Manifest.Capabilities {
		if c == capability {
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
