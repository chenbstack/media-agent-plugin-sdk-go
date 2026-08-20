// Package plugins 实现插件内核（docs/plugin-model.md §10-11）：
// Manifest、受限配置 schema、Registry 和配置校验。
// CLI 插件运行时（进程宿主、stdio JSON-RPC）后置，不在本包。
package pluginsdk

import (
	"context"
	"encoding/json"
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

	CapabilityOnboardingConnection = "onboarding.connection"
	CapabilityOnboardingAssessment = "onboarding.assess"
)

type Manifest struct {
	ID                 string                    `yaml:"id" json:"id"`
	Name               string                    `yaml:"name" json:"name"`
	Version            string                    `yaml:"version" json:"version"`
	Description        string                    `yaml:"description" json:"description,omitempty"`
	Category           string                    `yaml:"category,omitempty" json:"category,omitempty"`
	Tags               []string                  `yaml:"tags,omitempty" json:"tags,omitempty"`
	Type               string                    `yaml:"type" json:"type"` // builtin / cli / rule / ui
	Entry              map[string]string         `yaml:"entry,omitempty" json:"entry,omitempty"`
	Protocol           string                    `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Transport          string                    `yaml:"transport,omitempty" json:"transport,omitempty"`
	ServeArgs          []string                  `yaml:"serve_args,omitempty" json:"serve_args,omitempty"`
	StdioArgs          []string                  `yaml:"stdio_args,omitempty" json:"stdio_args,omitempty"`
	Capabilities       []string                  `yaml:"capabilities" json:"capabilities"`
	Subscriptions      []EventSubscription       `yaml:"subscriptions,omitempty" json:"subscriptions,omitempty"`
	API                *APIExtension             `yaml:"api,omitempty" json:"api,omitempty"`
	Agent              *AgentExtension           `yaml:"agent,omitempty" json:"agent,omitempty"`
	UI                 *UIExtension              `yaml:"ui,omitempty" json:"ui,omitempty"`
	Identity           *IdentityExtension        `yaml:"identity,omitempty" json:"identity,omitempty"`
	Onboarding         *OnboardingWorkflow       `yaml:"onboarding,omitempty" json:"onboarding,omitempty"`
	Entitlements       []string                  `yaml:"entitlements,omitempty" json:"entitlements,omitempty"`
	RequiredMembership MembershipLevel           `yaml:"required_membership,omitempty" json:"required_membership,omitempty"`
	Actions            []ActionDefinition        `yaml:"actions,omitempty" json:"actions,omitempty"`
	ScheduledTasks     []ScheduledTaskDefinition `yaml:"scheduled_tasks,omitempty" json:"scheduled_tasks,omitempty"`
	HTTPServices       []HTTPServiceDefinition   `yaml:"http_services,omitempty" json:"http_services,omitempty"`
	Artifacts          []ArtifactDefinition      `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	Permissions        Permissions               `yaml:"permissions" json:"permissions"`
	Resources          Resources                 `yaml:"resources" json:"resources"`
	Install            *InstallInfo              `yaml:"install,omitempty" json:"install,omitempty"`
	ModelBackend       string                    `yaml:"model_backend,omitempty" json:"model_backend,omitempty"`
	ModelUI            *ModelUI                  `yaml:"model_ui,omitempty" json:"model_ui,omitempty"`
}

// AgentExtension declares bounded business tools that the host may expose to
// its Agent runtime. The host still owns authorization, tool registration and
// execution; a declaration never grants access by itself.
type AgentExtension struct {
	Tools  []AgentToolDefinition  `yaml:"tools" json:"tools"`
	Skills []AgentSkillDefinition `yaml:"skills,omitempty" json:"skills,omitempty"`
}

// AgentSkillDefinition declares one reusable workflow for the plugin's Agent
// tools. A plugin may declare multiple skills. Skills are guidance only: the
// host filters them by the referenced tools' current authorization and every
// tool call still passes through the normal permission and confirmation path.
type AgentSkillDefinition struct {
	Name         string   `yaml:"name" json:"name"`
	Description  string   `yaml:"description" json:"description"`
	Instructions string   `yaml:"instructions" json:"instructions"`
	Tools        []string `yaml:"tools" json:"tools"`
}

// AgentToolDefinition maps one model-visible tool to an existing session API
// capability. Permissions and entitlements are deliberately inherited from
// that capability so the Agent path cannot weaken the normal service gateway.
type AgentToolDefinition struct {
	Name         string                 `yaml:"name" json:"name"`
	Description  string                 `yaml:"description" json:"description"`
	Capability   string                 `yaml:"capability" json:"capability"`
	InputSchema  map[string]any         `yaml:"input_schema" json:"input_schema"`
	Risk         string                 `yaml:"risk" json:"risk"`
	Confirmation *AgentToolConfirmation `yaml:"confirmation,omitempty" json:"confirmation,omitempty"`
}

// AgentToolConfirmation describes the host-rendered confirmation card for a
// side-effecting tool. Values are selected from already validated arguments;
// the plugin cannot inject arbitrary HTML or execute UI code here.
type AgentToolConfirmation struct {
	Title        string                       `yaml:"title" json:"title"`
	ConfirmLabel string                       `yaml:"confirm_label" json:"confirm_label"`
	Fields       []AgentToolConfirmationField `yaml:"fields,omitempty" json:"fields,omitempty"`
}

type AgentToolConfirmationField struct {
	Label    string `yaml:"label" json:"label"`
	Argument string `yaml:"argument" json:"argument"`
}

// ModelUI lets a model provider own the user-facing summary of its configured
// models while the host keeps rendering, authorization and operation routing.
// Values reference ModelConfig JSON fields and never execute plugin code.
type ModelUI struct {
	Description string                `yaml:"description" json:"description"`
	Summary     []ModelUISummaryField `yaml:"summary" json:"summary"`
	Download    *ModelUIOperation     `yaml:"download,omitempty" json:"download,omitempty"`
	Uninstall   *ModelUIOperation     `yaml:"uninstall,omitempty" json:"uninstall,omitempty"`
	SpeedTest   *ModelUIOperation     `yaml:"speed_test,omitempty" json:"speed_test,omitempty"`
}

type ModelUISummaryField struct {
	Label       string `yaml:"label" json:"label"`
	Field       string `yaml:"field,omitempty" json:"field,omitempty"`
	Value       string `yaml:"value,omitempty" json:"value,omitempty"`
	Format      string `yaml:"format,omitempty" json:"format,omitempty"` // text / model_file / host
	DetailField string `yaml:"detail_field,omitempty" json:"detail_field,omitempty"`
}

type ModelUIOperation struct {
	Label        string `yaml:"label" json:"label"`
	PendingLabel string `yaml:"pending_label" json:"pending_label"`
}

type MembershipLevel string

const MembershipPro MembershipLevel = "pro"

// ArtifactDefinition declares a user-visible file produced by a plugin from an
// imported media file. The host owns path derivation and persistence; plugins
// only choose the destination storage through one of their config fields and
// provide the content through a restricted host capability.
type ArtifactDefinition struct {
	ID                           string `yaml:"id" json:"id"`
	Kind                         string `yaml:"kind" json:"kind"`
	TargetStorageField           string `yaml:"target_storage_field" json:"target_storage_field"`
	Extension                    string `yaml:"extension,omitempty" json:"extension,omitempty"`
	MediaLibraryVisible          bool   `yaml:"media_library_visible,omitempty" json:"media_library_visible,omitempty"`
	RequiredBeforeLibraryRefresh bool   `yaml:"required_before_library_refresh,omitempty" json:"required_before_library_refresh,omitempty"`
}

// OnboardingWorkflow lets a plugin own the operation performed after the host
// validates and saves its onboarding configuration. The host invokes
// SubmitAction immediately and, when StatusAction is present, polls it and
// renders the standard action-progress payload while the submit action runs.
type OnboardingWorkflow struct {
	SubmitAction string `yaml:"submit_action" json:"submit_action"`
	SubmitLabel  string `yaml:"submit_label" json:"submit_label"`
	PendingLabel string `yaml:"pending_label,omitempty" json:"pending_label,omitempty"`
	StatusAction string `yaml:"status_action,omitempty" json:"status_action,omitempty"`
}

// APIExtension 声明由宿主代理的插件业务 API。Service 会成为
// /api/v1/plugin-services/{plugin_id}/{service}/... 中的 service 段。
type APIExtension struct {
	Service                string      `yaml:"service" json:"service"`
	Auth                   APIAuthMode `yaml:"auth,omitempty" json:"auth,omitempty"`
	RequiredEntitlements   []string    `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
	RequiredPermissions    []string    `yaml:"required_permissions,omitempty" json:"required_permissions,omitempty"`
	RequiredAnyPermissions []string    `yaml:"required_any_permissions,omitempty" json:"required_any_permissions,omitempty"`
	// Capabilities 声明本服务提供的具名业务能力。PluginCallable 默认 false；
	// 只有显式开启的能力才能由其他插件经宿主 broker 调用。
	Capabilities []APIServiceCapability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// APIServiceCapability 声明 api.endpoint 服务的一项具名能力。Method/Path 是
// 宿主内部路由；PluginCallable 控制它是否进入跨插件服务总线。
type APIServiceCapability struct {
	Name                   string   `yaml:"name" json:"name"`
	Method                 string   `yaml:"method" json:"method"`
	Path                   string   `yaml:"path" json:"path"`
	PluginCallable         bool     `yaml:"plugin_callable,omitempty" json:"plugin_callable,omitempty"`
	RequiredPermissions    []string `yaml:"required_permissions,omitempty" json:"required_permissions,omitempty"`
	RequiredAnyPermissions []string `yaml:"required_any_permissions,omitempty" json:"required_any_permissions,omitempty"`
}

// PluginCallableCapabilities 返回本服务显式允许其他插件调用的能力。
func (a APIExtension) PluginCallableCapabilities() []APIServiceCapability {
	out := make([]APIServiceCapability, 0, len(a.Capabilities))
	for _, capability := range a.Capabilities {
		if !capability.PluginCallable {
			continue
		}
		out = append(out, capability)
	}
	return out
}

type APIAuthMode string

const (
	APIAuthSession APIAuthMode = "session"
	APIAuthNone    APIAuthMode = "none"

	CapabilityAPIEndpoint      = "api.endpoint"
	CapabilityUIModule         = "ui.module"
	CapabilityUIAction         = "ui.action"
	CapabilityIdentityProvider = "identity.provider"
)

// UIExtension 声明随已验签制品分发的前端模块及其页面。Module 必须是制品内的
// 相对路径，不能是远程 URL；宿主仍需根据签名和发布策略决定是否允许同源加载。
type UIExtension struct {
	Module   string      `yaml:"module" json:"module"`
	Routes   []UIRoute   `yaml:"routes,omitempty" json:"routes,omitempty"`
	Actions  []UIAction  `yaml:"actions,omitempty" json:"actions,omitempty"`
	Cards    []UICard    `yaml:"cards,omitempty" json:"cards,omitempty"`
	Tabs     []UITab     `yaml:"tabs,omitempty" json:"tabs,omitempty"`
	Settings *UISettings `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// UICard 声明系统总览页的插件卡片。Size 是宿主网格档位（metric/half/full），
// 内部由导出组件完全自定义。刻意没有 order：展示与排序属于用户偏好，由宿主的
// 用户自定义配置决定。权限谓词只做展示过滤，插件 API 仍必须独立鉴权。
//
// Title 非空时卡片进入宿主标题模式：宿主渲染统一排版的标题行，Export 组件只
// 负责卡片体；HeaderExport 可选，声明渲染在标题行右侧的自定义组件（如图例、
// 角标）。Title 为空则保持完全自定义模式，卡片内容（含标题）全部由 Export
// 组件渲染，此时不允许声明 HeaderExport。
//
// PreviewExport 可选，声明卡片在总览自定义面板缩略图里的静态预览组件：
// 用写死的示例数据渲染一张正常尺寸的卡片，由宿主等比缩小展示。预览组件
// 必须纯静态——不得调用宿主 API 或发起任何请求。未声明时宿主用通用骨架示意。
//
// Data 可选，声明卡片数据改由宿主代取（见 UICardData）。不声明则维持原样：
// 卡片组件自己调宿主 API 取数。
type UICard struct {
	ID                   string      `yaml:"id" json:"id"`
	Size                 string      `yaml:"size" json:"size"`
	Export               string      `yaml:"export" json:"export"`
	Title                string      `yaml:"title,omitempty" json:"title,omitempty"`
	HeaderExport         string      `yaml:"header_export,omitempty" json:"header_export,omitempty"`
	PreviewExport        string      `yaml:"preview_export,omitempty" json:"preview_export,omitempty"`
	Data                 *UICardData `yaml:"data,omitempty" json:"data,omitempty"`
	RequiredEntitlements []string    `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
	RequiredPermissions  []string    `yaml:"required_permissions,omitempty" json:"required_permissions,omitempty"`
	// RequiredAnyPermissions 命中其一即可，与 RequiredPermissions（必须全部命中）
	// 是并列条件，两者都声明时要同时满足。卡片背后的接口常常接受几个权限中的任意
	// 一个（「管站点的」或「管系统设置的」都能看站点统计），只有 all 语义时卡片和
	// 接口的门对不齐：填得全了一部分有权限的人看不到卡，填一个又漏掉另一批人。
	RequiredAnyPermissions []string `yaml:"required_any_permissions,omitempty" json:"required_any_permissions,omitempty"`
	ForbiddenPermissions   []string `yaml:"forbidden_permissions,omitempty" json:"forbidden_permissions,omitempty"`
}

// UICardData 把卡片取数的活交给宿主：宿主按 RefreshInterval 周期性调用 Sources
// 里的插件服务端点，把结果随总览事件流一并下发，卡片组件直接读现成数据，自己
// 不再发请求。一屏十来张卡各自轮询会把总览页打成筛子，交给宿主才能合并调度。
//
// 宿主只取真正要显示的卡片：用户在总览里隐藏掉的卡、以及被停用插件的卡，一次
// 都不会取。声明这一段的前提是插件有 api.endpoint capability——数据总得有地方取。
type UICardData struct {
	// RefreshInterval 形如 "30s"、"5m"、"1h"，是这张卡期望的取数间隔。留空由
	// 宿主取默认值。宿主还会按自己的下限收敛，声明得再短也不会真按秒去打插件。
	RefreshInterval string `yaml:"refresh_interval,omitempty" json:"refresh_interval,omitempty"`
	// Sources 至少一路。一张卡往往要拼几个端点才够画，所以这里是列表而不是单值。
	Sources []UICardSource `yaml:"sources" json:"sources"`
}

// UICardSource 是卡片数据的一路来源。Key 是卡片组件读取这一路数据时用的名字，
// 在同一张卡内唯一；Path 是插件自己 api.endpoint 下的绝对路径，可带查询串。
type UICardSource struct {
	Key  string `yaml:"key" json:"key"`
	Path string `yaml:"path" json:"path"`
}

// UICardRefreshIntervalMax 是 refresh_interval 的声明上限。比一天还长的间隔说明
// 这数据压根不该走周期取数，用事件推更合适。
const UICardRefreshIntervalMax = 24 * time.Hour

// UITab 声明插件详情弹窗的自定义 tab：与宿主内置的设置/能力/权限同级，按声明
// 顺序排在内置 tab 之前，第一个自定义 tab 是打开弹窗时的默认 tab。
type UITab struct {
	ID                   string   `yaml:"id" json:"id"`
	Label                string   `yaml:"label" json:"label"`
	Export               string   `yaml:"export" json:"export"`
	RequiredEntitlements []string `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
}

// UISettings 把插件配置弹窗的设置区域替换或扩展为完全自定义的面板。
// Mode 为 replace（默认，整体替换 schema 表单）或 extend（追加在表单之后）。
type UISettings struct {
	Export string `yaml:"export" json:"export"`
	Mode   string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// UIRoute 是插件前端模块导出的一个页面。默认路由应位于
// /plugin/{plugin_id}/ 下；可信插件的顶级别名由宿主额外授权，SDK 不判断发布者信任。
type UIRoute struct {
	ID                   string   `yaml:"id" json:"id"`
	Path                 string   `yaml:"path" json:"path"`
	Export               string   `yaml:"export" json:"export"`
	RequiredEntitlements []string `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
	RequiredPermissions  []string `yaml:"required_permissions,omitempty" json:"required_permissions,omitempty"`
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

// UIAction lets a signed UI module contribute an action to a host-owned slot.
// The host decides which slots exist and owns the resource context passed to
// the exported component. Permission predicates are presentation filters only;
// the plugin API and HostAPI must still authorize every operation.
type UIAction struct {
	ID                   string   `yaml:"id" json:"id"`
	Slot                 string   `yaml:"slot" json:"slot"`
	Export               string   `yaml:"export" json:"export"`
	Order                int      `yaml:"order,omitempty" json:"order,omitempty"`
	RequiredEntitlements []string `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
	RequiredPermissions  []string `yaml:"required_permissions,omitempty" json:"required_permissions,omitempty"`
	ForbiddenPermissions []string `yaml:"forbidden_permissions,omitempty" json:"forbidden_permissions,omitempty"`
}

// IdentityExtension 声明插件身份 Provider 的 RPC service 名称及启用它所需的权益。
// Session 签发、CSRF 和找回入口始终由宿主负责。
type IdentityExtension struct {
	Service              string         `yaml:"service,omitempty" json:"service,omitempty"`
	RequiredEntitlements []string       `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
	Flows                []IdentityFlow `yaml:"flows,omitempty" json:"flows,omitempty"`
}

type IdentityFlowType string

const (
	IdentityFlowCredentials IdentityFlowType = "credentials"
	IdentityFlowOIDC        IdentityFlowType = "oidc"
)

// IdentityFlow describes one host-rendered login option. Credentials keeps
// the v1 VerifyIdentity path available; OIDC uses the optional redirect-flow
// interface below. CAS is intentionally not part of this contract.
type IdentityFlow struct {
	ID    string           `yaml:"id" json:"id"`
	Type  IdentityFlowType `yaml:"type" json:"type"`
	Label string           `yaml:"label" json:"label"`
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
	ID          string            `json:"id"`
	DisplayName string            `json:"display_name,omitempty"`
	Issuer      string            `json:"issuer,omitempty"`
	Subject     string            `json:"subject,omitempty"`
	Email       string            `json:"email,omitempty"`
	AvatarURL   string            `json:"avatar_url,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
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

// IdentityBeginRequest starts a redirect-based authentication transaction.
// CallbackURL is host-owned and State is a random one-time value generated by
// the host. The provider must include State in the external authorization
// request and must not replace CallbackURL with a plugin-controlled target.
type IdentityBeginRequest struct {
	FlowID      string `json:"flow_id"`
	CallbackURL string `json:"callback_url"`
	State       string `json:"state"`
}

// IdentityChallenge contains only navigation data and opaque provider state.
// The host stores Data server-side with strict size/TTL limits; it is never
// trusted as a principal or exposed as a host session token.
type IdentityChallenge struct {
	RedirectURL string `json:"redirect_url"`
	Data        []byte `json:"data,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// IdentityCompleteRequest completes an OIDC-style callback. Parameters are
// callback query values after the host has validated the one-time State.
type IdentityCompleteRequest struct {
	FlowID      string              `json:"flow_id"`
	CallbackURL string              `json:"callback_url"`
	Parameters  map[string][]string `json:"parameters,omitempty"`
	Data        []byte              `json:"data,omitempty"`
}

// IdentityRedirectProvider is an optional v2 extension. Keeping it separate
// preserves source compatibility for v1 credential-only providers.
type IdentityRedirectProvider interface {
	BeginIdentity(ctx context.Context, request IdentityBeginRequest) (IdentityChallenge, error)
	CompleteIdentity(ctx context.Context, request IdentityCompleteRequest) (IdentityVerification, error)
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
	// Priority 决定同步钩子位上的执行顺序，小的先跑；默认 0，同值退回插件 id 字典序。
	//
	// 顺序有时候带语义：字幕钩子上 PT 站附件该先于公开字幕库试一次，站点自带的字幕跟
	// 这个种子严格对得上。默认的 id 字典序纯属巧合（opensubtitles 恰好排在
	// site-subtitles 前面），靠它编排等于没编排。
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`
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
	Entitlements  Entitlements
	SiteAccounts  SiteAccounts
	Subscriptions Subscriptions
	Downloads     Downloads
	Transfers     Transfers
	Rules         Rules
	Connections   Connections
	// ConnectionCredentials reveals a provider-declared secret from an existing
	// connection after a separate high-risk host permission check.
	ConnectionCredentials ConnectionCredentials
	Storages              Storages
	Schedules             Schedules
	// PluginServices 经宿主 broker 调用其他插件的业务 API；只在插件声明了
	// host 权限 "plugin_service.<provider>/<service>" 时由宿主注入。
	PluginServices PluginServices
	// Sidecars 把字幕这类随媒体文件存放的附属文件交给宿主落盘；只在插件声明了
	// host 权限 "media.sidecar.write" 时由宿主注入。
	Sidecars MediaSidecars
	// Mirrors 把 .strm 这类"在另一个存储里按原相对路径生成的替身文件"交给宿主落盘；
	// 只在插件声明了 host 权限 "media.mirror.write" 时由宿主注入。
	Mirrors MediaMirrors
	// Playback 通过宿主已配置的存储 Provider 解析实时播放直链；只在插件声明了
	// host 权限 "media.playback.resolve" 时由宿主注入。
	Playback MediaPlayback
	// Workspace 是宿主分配给本插件的私有工作目录，放解压产物、下载缓存这类只有插件
	// 自己关心的文件；只在插件声明了 host 权限 "workspace.local" 时由宿主注入。
	// 用户的媒体文件不在里面，也不该往里面放——那是 Sidecars / Mirrors 的事。
	Workspace *Workspace
	// Renderer 借宿主已启用的浏览器渲染插件取回页面，供站点插件绕过反爬挑战；
	// 只在插件声明了 host 权限 "renderer.page" 时由宿主注入。
	Renderer PageRenderer
	// Cloud 以本实例身份访问云端，宿主只发短期令牌，实例长期密钥不出宿主进程；
	// 只在插件声明了 host 权限 "cloud.identity" 时由宿主注入。
	Cloud CloudIdentity
	// SiteRules 只读访问宿主的站点规则目录（管理员自有规则）；
	// 只在插件声明了 host 权限 "site.rules.read" 时由宿主注入。
	SiteRules SiteRuleFiles
	Runtime   *runtimesdk.Services
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

	// Schema 声明插件私有库的表结构。宿主每次启动把实际结构对齐到这里的声明，
	// 插件不需要自己建表或写迁移。声明同时是查询编译器的白名单：只有这里出现过的
	// 表和列，插件的查询才引用得到。零值表示插件不使用私有库。
	Schema DBSchema

	// ReuseProviders 声明本插件的 Provider 可以跨调用复用。
	//
	// 默认（false）每次 RPC 都 NewXxx 现造一个，Provider 字段上的任何缓存都只在这一次
	// 调用里有效：TVDB 补全一部 10 季的剧集会把同一份全量集列表分页拉 10 遍，而它一个
	// 月有效的登录 token 同样存在 Provider 上，于是每次调用都先跑一趟 /login。
	//
	// 打开后 SDK 按 (实例, 配置摘要) 池化 Provider。配置一变摘要就变，池子自动换新，
	// 旧配置（含旧密钥引用）造出来的实例不会漏给新配置；租出去的实例同一时刻只服务
	// 一次调用。构造时拿到的 inst 与 secrets 句柄**始终有效**——SDK 会在每次调用前把
	// 它们背后的通道换成本次调用的，插件侧不需要写任何代码配合。
	//
	// 唯一要想清楚的是：Provider 字段上的状态从此跨调用存活。上游数据缓存、登录态、
	// 连接池正是要保住的东西；「这一次调用的参数」之类则不能再往字段上放。插件如果
	// 自己在内部起 goroutine，这些字段仍需自行加锁——这一点与不复用时相同。
	ReuseProviders bool

	// 工厂按能力可选实现；nil 表示插件不提供该类 Provider。
	NewStorage      func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.StorageProvider, error)
	NewDownloader   func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.DownloaderProvider, error)
	NewMediaServer  func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MediaServerProvider, error)
	NewMetadata     func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.MetadataProvider, error)
	NewSite         func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.SiteProvider, error)
	NewCookieSource func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.CookieSourceProvider, error)
	NewModel        func() providers.ModelProvider
	// NewModelWithInstance 是带宿主上下文的模型工厂。模型 Provider 虽然不是用户创建的
	// connection，也可能需要插件私有 Workspace 保存运行器和模型文件；宿主优先调用此
	// 工厂，旧插件仍可只实现 NewModel。
	NewModelWithInstance    func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.ModelProvider, error)
	NewEventSubscriber      func(ctx context.Context, inst Instance, secrets SecretResolver) (EventSubscriber, error)
	NewNotifier             func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.NotifierProvider, error)
	NewSubtitleSource       func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.SubtitleSourceProvider, error)
	NewRenderer             func(ctx context.Context, inst Instance, secrets SecretResolver) (providers.RendererProvider, error)
	NewAPI                  func(ctx context.Context, inst Instance, secrets SecretResolver) (APIProvider, error)
	NewIdentity             func(ctx context.Context, inst Instance, secrets SecretResolver) (IdentityProvider, error)
	NewActionHandler        func(ctx context.Context, inst Instance, secrets SecretResolver) (ActionHandler, error)
	NewScheduledTaskHandler func(ctx context.Context, inst Instance, secrets SecretResolver) (ScheduledTaskHandler, error)
	NewHTTPService          func(ctx context.Context, inst Instance, secrets SecretResolver, name string) (HTTPService, error)
	AssessOnboarding        func(ctx context.Context, inst Instance, secrets SecretResolver) (OnboardingAssessment, error)

	// FieldOptions 为 dynamic_options 的 select 字段提供运行时选项
	// （如从媒体服务器拉取媒体库列表）；nil 表示插件没有动态选项字段。
	FieldOptions func(ctx context.Context, inst Instance, secrets SecretResolver, field string) ([]Option, error)

	// StartAuth / CheckAuth 为插件提供通用交互式认证流程，如扫码登录。
	StartAuth func(ctx context.Context, inst Instance, flow string) (AuthStartResult, error)
	CheckAuth func(ctx context.Context, inst Instance, flow, sessionID string) (AuthCheckResult, error)

	// ValidateConfig 在通用 schema 校验之后运行，供插件按其他字段或外部资源包
	// 做二次校验；例如站点插件按 base_url 匹配资源包后校验认证字段。
	ValidateConfig func(config map[string]any) error

	// SiteSupportForURL 判定一个站点地址是否被支持，以及需要哪些认证字段。
	// 它发生在用户新建站点连接、还没有任何账号凭据的时刻，构造不出 SiteProvider，
	// 所以是插件级查询。nil 表示插件不做站点地址判定。
	//
	// inst 是宿主传入的全局实例：判定要读规则来源，跨进程的插件因此需要一条能拿到
	// 宿主服务（如站点规则目录）的通道，而 Instance 是这条通道唯一的载体。
	SiteSupportForURL func(ctx context.Context, inst Instance, url string) (providers.SiteSupport, error)

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
	// InstallWithInstance 与 Install 语义相同，但能访问宿主按插件权限分配的全局 Instance。
	// 宿主优先调用它；没声明 workspace.local 时 inst.Workspace 为 nil。
	InstallWithInstance func(ctx context.Context, inst Instance, progress io.Writer) (InstallResult, error)

	// CheckInstall 查询插件是否已安装就绪，只读、无副作用、不触发下载。宿主在插件加载时
	// 调用它决定初始安装状态（installed / pending），避免每次启动都执行安装动作。声明了
	// lifecycle.install 的插件应实现它；nil 时宿主退化为把状态标记为 pending。
	CheckInstall             func(ctx context.Context) (InstallResult, error)
	CheckInstallWithInstance func(ctx context.Context, inst Instance) (InstallResult, error)

	// Uninstall 卸载 Install 下载的资源（如引擎二进制），回收磁盘空间。宿主在用户手动
	// 卸载、或停用插件时调用；同 Install 一样只转发、记录状态并展示进度（progress 按行
	// 写入）。必须幂等：无资源可卸时返回 UninstallResult{Removed: false}。nil 表示插件
	// 无可卸载资源。
	//
	// 上面的 Install/CheckInstall/Uninstall 对应"默认组件"（id 为空串）。声明了多个可安装
	// 组件的插件（见 Manifest.Install.Components）把额外组件的钩子放进 InstallComponents，
	// 按 ID 路由；宿主用 InstallHooks(id) 统一取用。
	Uninstall             func(ctx context.Context, progress io.Writer) (UninstallResult, error)
	UninstallWithInstance func(ctx context.Context, inst Instance, progress io.Writer) (UninstallResult, error)

	// InstallComponents 是"非默认组件"的安装钩子集合，按 ID 匹配 Manifest.Install.Components。
	InstallComponents []InstallComponent
}

// InstallComponent 是单个可安装组件的运行时钩子集合，语义同 Plugin.Install/CheckInstall/
// Uninstall，但作用于指定 ID 的组件。Uninstall 为 nil 表示该组件资源不可卸载。
type InstallComponent struct {
	ID                       string
	Install                  func(ctx context.Context, progress io.Writer) (InstallResult, error)
	InstallWithInstance      func(ctx context.Context, inst Instance, progress io.Writer) (InstallResult, error)
	CheckInstall             func(ctx context.Context) (InstallResult, error)
	CheckInstallWithInstance func(ctx context.Context, inst Instance) (InstallResult, error)
	Uninstall                func(ctx context.Context, progress io.Writer) (UninstallResult, error)
	UninstallWithInstance    func(ctx context.Context, inst Instance, progress io.Writer) (UninstallResult, error)
}

// InstallHooks 返回给定组件 ID 的安装钩子；空 ID 命中默认（Install/CheckInstall/Uninstall）。
// ok 为 false 表示该组件不存在或未提供任何钩子。
func (p Plugin) InstallHooks(component string) (InstallComponent, bool) {
	if component == "" {
		if p.Install == nil && p.InstallWithInstance == nil && p.CheckInstall == nil && p.CheckInstallWithInstance == nil && p.Uninstall == nil && p.UninstallWithInstance == nil {
			return InstallComponent{}, false
		}
		return InstallComponent{
			ID: "", Install: p.Install, InstallWithInstance: p.InstallWithInstance,
			CheckInstall: p.CheckInstall, CheckInstallWithInstance: p.CheckInstallWithInstance,
			Uninstall: p.Uninstall, UninstallWithInstance: p.UninstallWithInstance,
		}, true
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
	seenArtifacts := map[string]struct{}{}
	for _, artifact := range m.Artifacts {
		if !manifestIdentifier.MatchString(artifact.ID) {
			return fmt.Errorf("插件 %s: artifact id %q 格式无效", m.ID, artifact.ID)
		}
		if _, exists := seenArtifacts[artifact.ID]; exists {
			return fmt.Errorf("插件 %s: artifact id 重复 %q", m.ID, artifact.ID)
		}
		seenArtifacts[artifact.ID] = struct{}{}
		if artifact.Kind != "mirror" {
			return fmt.Errorf("插件 %s: artifact %s 的 kind %q 不受支持", m.ID, artifact.ID, artifact.Kind)
		}
		if !manifestIdentifier.MatchString(artifact.TargetStorageField) {
			return fmt.Errorf("插件 %s: artifact %s 的 target_storage_field %q 格式无效", m.ID, artifact.ID, artifact.TargetStorageField)
		}
		if artifact.Extension == "" || len(artifact.Extension) > 16 {
			return fmt.Errorf("插件 %s: artifact %s 的 extension 无效", m.ID, artifact.ID)
		}
		for _, r := range artifact.Extension {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return fmt.Errorf("插件 %s: artifact %s 的 extension %q 无效", m.ID, artifact.ID, artifact.Extension)
			}
		}
		if !m.Permissions.HasHost("media.mirror.write") {
			return fmt.Errorf("插件 %s: 声明 mirror artifact 时必须包含 host 权限 media.mirror.write", m.ID)
		}
		if _, ok := p.ConfigSchema.Field(artifact.TargetStorageField); !ok {
			return fmt.Errorf("插件 %s: artifact %s 引用的配置字段 %q 不存在", m.ID, artifact.ID, artifact.TargetStorageField)
		}
		if artifact.RequiredBeforeLibraryRefresh {
			found := false
			for _, sub := range m.Subscriptions {
				if sub.Type == "library.refresh.pending" && sub.Version == 1 && (sub.Phase == "" || sub.Phase == "before") {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("插件 %s: artifact %s 要求刷新前生成，但未订阅 library.refresh.pending v1 before", m.ID, artifact.ID)
			}
		}
	}
	if err := m.validateExtensions(capabilities); err != nil {
		return err
	}
	if err := m.validateModelUI(capabilities); err != nil {
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
	_, hasScheduledTaskCapability := capabilities[CapabilityScheduledTask]
	if hasScheduledTaskCapability && len(m.ScheduledTasks) == 0 {
		return fmt.Errorf("插件 %s: capability %s 必须声明 scheduled_tasks", m.ID, CapabilityScheduledTask)
	}
	if len(m.ScheduledTasks) > 0 && !hasScheduledTaskCapability {
		return fmt.Errorf("插件 %s: 声明 scheduled_tasks 时必须包含 capability %s", m.ID, CapabilityScheduledTask)
	}
	declaredEntitlements, err := validateEntitlements(m.ID, "manifest", m.Entitlements, nil)
	if err != nil {
		return err
	}
	seenScheduledTasks := map[string]bool{}
	for _, task := range m.ScheduledTasks {
		if seenScheduledTasks[task.ID] {
			return fmt.Errorf("插件 %s: scheduled task id 重复 %q", m.ID, task.ID)
		}
		seenScheduledTasks[task.ID] = true
		if err := task.Validate(m.ID, m.Permissions, declaredEntitlements); err != nil {
			return err
		}
	}
	if m.Onboarding != nil {
		if _, ok := capabilities[CapabilityOnboardingConnection]; !ok {
			return fmt.Errorf("插件 %s: 声明 onboarding 时必须包含 capability %s", m.ID, CapabilityOnboardingConnection)
		}
		if _, ok := capabilities["action.run"]; !ok {
			return fmt.Errorf("插件 %s: 声明 onboarding 时必须包含 capability action.run", m.ID)
		}
		if !seenActions[m.Onboarding.SubmitAction] {
			return fmt.Errorf("插件 %s: onboarding.submit_action %q 未声明为 action", m.ID, m.Onboarding.SubmitAction)
		}
		if strings.TrimSpace(m.Onboarding.SubmitLabel) == "" {
			return fmt.Errorf("插件 %s: onboarding.submit_label 不能为空", m.ID)
		}
		if m.Onboarding.StatusAction != "" {
			if !seenActions[m.Onboarding.StatusAction] {
				return fmt.Errorf("插件 %s: onboarding.status_action %q 未声明为 action", m.ID, m.Onboarding.StatusAction)
			}
			if m.Onboarding.StatusAction == m.Onboarding.SubmitAction {
				return fmt.Errorf("插件 %s: onboarding.status_action 不能与 submit_action 相同", m.ID)
			}
			if _, ok := capabilities["action.status"]; !ok {
				return fmt.Errorf("插件 %s: 声明 onboarding.status_action 时必须包含 capability action.status", m.ID)
			}
		}
	}
	_, hasHTTPService := capabilities[CapabilityHTTPService]
	if hasHTTPService && len(m.HTTPServices) == 0 {
		return fmt.Errorf("插件 %s: capability %s 必须声明 http_services", m.ID, CapabilityHTTPService)
	}
	if len(m.HTTPServices) > 0 && !hasHTTPService {
		return fmt.Errorf("插件 %s: 声明 http_services 时必须包含 capability %s", m.ID, CapabilityHTTPService)
	}
	seenHTTPServices := map[string]struct{}{}
	for _, service := range m.HTTPServices {
		if !manifestIdentifier.MatchString(service.Name) {
			return fmt.Errorf("插件 %s: http service name %q 格式无效", m.ID, service.Name)
		}
		if _, exists := seenHTTPServices[service.Name]; exists {
			return fmt.Errorf("插件 %s: http service name 重复 %q", m.ID, service.Name)
		}
		seenHTTPServices[service.Name] = struct{}{}
		if service.PublicHostConfigField != "" && !manifestIdentifier.MatchString(service.PublicHostConfigField) {
			return fmt.Errorf("插件 %s: http service %s 的 public_host_config_field %q 格式无效", m.ID, service.Name, service.PublicHostConfigField)
		}
		if service.PathPrefix != "" && (service.PathPrefix == "/" || !strings.HasPrefix(service.PathPrefix, "/") || path.Clean(service.PathPrefix) != service.PathPrefix) {
			return fmt.Errorf("插件 %s: http service %s 的 path_prefix %q 格式无效", m.ID, service.Name, service.PathPrefix)
		}
		if service.AuthMode != HTTPServiceAuthSession && service.AuthMode != HTTPServiceAuthToken && service.AuthMode != HTTPServiceAuthPublic {
			return fmt.Errorf("插件 %s: http service %s 的 auth_mode 必须显式声明 session、token 或 public", m.ID, service.Name)
		}
		if err := validateIdentityKeys(m.ID, "http service "+service.Name+" required_permissions", service.RequiredPermissions); err != nil {
			return err
		}
		if service.AuthMode != HTTPServiceAuthSession && len(service.RequiredPermissions) > 0 {
			return fmt.Errorf("插件 %s: 只有 session HTTP service 可以声明 required_permissions", m.ID)
		}
		seenMethods := map[string]struct{}{}
		for _, method := range service.Methods {
			switch method {
			case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
			default:
				return fmt.Errorf("插件 %s: http service %s 的 method %q 无效", m.ID, service.Name, method)
			}
			if _, exists := seenMethods[method]; exists {
				return fmt.Errorf("插件 %s: http service %s 的 method 重复 %q", m.ID, service.Name, method)
			}
			seenMethods[method] = struct{}{}
		}
	}
	if err := p.Schema.Validate(); err != nil {
		return fmt.Errorf("插件 %s 的私有表声明无效: %w", m.ID, err)
	}
	return p.ConfigSchema.validate(m.ID)
}

func (p Plugin) validate() error { return p.Validate() }

func (m Manifest) validateModelUI(capabilities map[string]struct{}) error {
	if m.ModelUI == nil {
		return nil
	}
	if _, ok := capabilities["model_provider.generate"]; !ok {
		return fmt.Errorf("插件 %s: model_ui 只能由模型提供方声明", m.ID)
	}
	ui := m.ModelUI
	if strings.TrimSpace(ui.Description) == "" {
		return fmt.Errorf("插件 %s: model_ui.description 不能为空", m.ID)
	}
	if len(ui.Summary) == 0 || len(ui.Summary) > 4 {
		return fmt.Errorf("插件 %s: model_ui.summary 必须包含 1 至 4 个字段", m.ID)
	}
	allowedFields := map[string]bool{
		"id": true, "name": true, "provider": true, "runtime": true, "backend": true,
		"command": true, "model_path": true, "model_name": true, "base_url": true,
		"api_key_env": true, "download_site": true, "download_url": true, "sha256": true,
		"threads": true, "context_tokens": true, "default_max_tokens": true,
	}
	seenLabels := map[string]bool{}
	for _, field := range ui.Summary {
		label := strings.TrimSpace(field.Label)
		name := strings.TrimSpace(field.Field)
		value := strings.TrimSpace(field.Value)
		if label == "" || (name == "") == (value == "") || (name != "" && !allowedFields[name]) {
			return fmt.Errorf("插件 %s: model_ui.summary 字段声明无效", m.ID)
		}
		if seenLabels[label] {
			return fmt.Errorf("插件 %s: model_ui.summary 标签重复 %q", m.ID, label)
		}
		seenLabels[label] = true
		switch field.Format {
		case "", "text", "model_file", "host":
		default:
			return fmt.Errorf("插件 %s: model_ui.summary format %q 不受支持", m.ID, field.Format)
		}
		if field.DetailField != "" && (name == "" || !allowedFields[field.DetailField]) {
			return fmt.Errorf("插件 %s: model_ui.summary detail_field %q 不受支持", m.ID, field.DetailField)
		}
	}
	operations := []struct {
		name       string
		capability string
		definition *ModelUIOperation
	}{
		{name: "download", capability: "model_provider.download", definition: ui.Download},
		{name: "uninstall", capability: "model_provider.uninstall", definition: ui.Uninstall},
		{name: "speed_test", capability: "model_provider.speed_test", definition: ui.SpeedTest},
	}
	for _, operation := range operations {
		if operation.definition == nil {
			continue
		}
		if _, ok := capabilities[operation.capability]; !ok {
			return fmt.Errorf("插件 %s: model_ui.%s 需要 capability %s", m.ID, operation.name, operation.capability)
		}
		if strings.TrimSpace(operation.definition.Label) == "" || strings.TrimSpace(operation.definition.PendingLabel) == "" {
			return fmt.Errorf("插件 %s: model_ui.%s 必须声明 label 和 pending_label", m.ID, operation.name)
		}
	}
	return nil
}

var manifestIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validateCardData 校验卡片的宿主代取声明。宿主拿着这段声明去打插件自己的
// api.endpoint，所以路径必须是能直接拼进去的绝对路径——留给宿主去猜就晚了。
func validateCardData(pluginID string, card UICard, hasAPI bool) error {
	if card.Data == nil {
		return nil
	}
	label := "ui card " + card.ID
	if !hasAPI {
		return fmt.Errorf("插件 %s: %s 声明了 data 但没有 capability api.endpoint（宿主无处取数）", pluginID, label)
	}
	if len(card.Data.Sources) == 0 {
		return fmt.Errorf("插件 %s: %s 的 data 必须声明至少一路 sources", pluginID, label)
	}
	if raw := strings.TrimSpace(card.Data.RefreshInterval); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("插件 %s: %s 的 refresh_interval %q 格式无效", pluginID, label, card.Data.RefreshInterval)
		}
		if interval <= 0 {
			return fmt.Errorf("插件 %s: %s 的 refresh_interval 必须为正数", pluginID, label)
		}
		if interval > UICardRefreshIntervalMax {
			return fmt.Errorf("插件 %s: %s 的 refresh_interval 不能超过 %s", pluginID, label, UICardRefreshIntervalMax)
		}
	}
	keys := make(map[string]struct{}, len(card.Data.Sources))
	for _, source := range card.Data.Sources {
		if !manifestIdentifier.MatchString(source.Key) {
			return fmt.Errorf("插件 %s: %s 的 data source key %q 格式无效", pluginID, label, source.Key)
		}
		if _, exists := keys[source.Key]; exists {
			return fmt.Errorf("插件 %s: %s 的 data source key 重复 %q", pluginID, label, source.Key)
		}
		keys[source.Key] = struct{}{}
		if err := validateCardSourcePath(pluginID, label, source); err != nil {
			return err
		}
	}
	return nil
}

// validateCardSourcePath 卡住的是宿主拼接时会出事的形状：非绝对路径、协议相对
// 的 "//host"、以及能翻出插件命名空间的 ".." 段。查询串允许，锚点没有意义。
func validateCardSourcePath(pluginID, label string, source UICardSource) error {
	path := source.Path
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "#") || strings.Contains(path, "\\") {
		return fmt.Errorf("插件 %s: %s 的 data source %s 路径 %q 必须是插件命名空间内的绝对路径", pluginID, label, source.Key, path)
	}
	for _, segment := range strings.Split(strings.SplitN(path, "?", 2)[0], "/") {
		if segment == ".." {
			return fmt.Errorf("插件 %s: %s 的 data source %s 路径 %q 不能包含 .. 段", pluginID, label, source.Key, path)
		}
	}
	return nil
}

func (m Manifest) validateExtensions(capabilities map[string]struct{}) error {
	if m.RequiredMembership != "" && m.RequiredMembership != MembershipPro {
		return fmt.Errorf("插件 %s: required_membership 只支持 pro", m.ID)
	}
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
		if m.API.Auth == "" {
			return fmt.Errorf("插件 %s: api.auth 必须显式声明 session 或 none", m.ID)
		}
		if err := validateIdentityKeys(m.ID, "api.required_permissions", m.API.RequiredPermissions); err != nil {
			return err
		}
		if err := validateIdentityKeys(m.ID, "api.required_any_permissions", m.API.RequiredAnyPermissions); err != nil {
			return err
		}
		if m.API.Auth != APIAuthSession && (len(m.API.RequiredPermissions) > 0 || len(m.API.RequiredAnyPermissions) > 0) {
			return fmt.Errorf("插件 %s: 只有 session API 可以声明 required_permissions", m.ID)
		}
		if _, err := validateEntitlements(m.ID, "api", m.API.RequiredEntitlements, declaredEntitlements); err != nil {
			return err
		}
		if len(m.API.Capabilities) == 0 {
			return fmt.Errorf("插件 %s: api.capabilities 必须逐项声明允许访问的 method 与 path", m.ID)
		}
		seenExports := map[string]struct{}{}
		for _, capability := range m.API.Capabilities {
			if err := validateAPIServiceCapability(m.ID, "api.capabilities", capability.Name, capability.Method, capability.Path, seenExports); err != nil {
				return err
			}
			if err := validateIdentityKeys(m.ID, "api capability "+capability.Name+" required_permissions", capability.RequiredPermissions); err != nil {
				return err
			}
			if err := validateIdentityKeys(m.ID, "api capability "+capability.Name+" required_any_permissions", capability.RequiredAnyPermissions); err != nil {
				return err
			}
			if m.API.Auth != APIAuthSession && (len(capability.RequiredPermissions) > 0 || len(capability.RequiredAnyPermissions) > 0) {
				return fmt.Errorf("插件 %s: 只有 session API capability 可以声明 required_permissions", m.ID)
			}
			if capability.PluginCallable && strings.Contains(capability.Path, "{") {
				return fmt.Errorf("插件 %s: 可供插件调用的 API capability %q 不支持路径参数", m.ID, capability.Name)
			}
		}
	}
	if m.Agent != nil {
		if m.API == nil || m.API.Auth != APIAuthSession {
			return fmt.Errorf("插件 %s: agent.tools 只能引用 session API capability", m.ID)
		}
		if len(m.Agent.Tools) == 0 {
			return fmt.Errorf("插件 %s: agent.tools 不能为空", m.ID)
		}
		apiCapabilities := make(map[string]APIServiceCapability, len(m.API.Capabilities))
		for _, capability := range m.API.Capabilities {
			apiCapabilities[capability.Name] = capability
		}
		seenTools := make(map[string]struct{}, len(m.Agent.Tools))
		for _, tool := range m.Agent.Tools {
			if !manifestIdentifier.MatchString(tool.Name) || !strings.HasPrefix(tool.Name, m.ID+".") {
				return fmt.Errorf("插件 %s: agent tool name %q 必须使用插件 id 作为命名空间", m.ID, tool.Name)
			}
			if _, exists := seenTools[tool.Name]; exists {
				return fmt.Errorf("插件 %s: agent tool name 重复 %q", m.ID, tool.Name)
			}
			seenTools[tool.Name] = struct{}{}
			if strings.TrimSpace(tool.Description) == "" {
				return fmt.Errorf("插件 %s: agent tool %q 必须声明 description", m.ID, tool.Name)
			}
			capability, exists := apiCapabilities[tool.Capability]
			if !exists {
				return fmt.Errorf("插件 %s: agent tool %q 引用了不存在的 API capability %q", m.ID, tool.Name, tool.Capability)
			}
			if strings.Contains(capability.Path, "{") {
				return fmt.Errorf("插件 %s: agent tool %q 暂不支持带路径参数的 API capability", m.ID, tool.Name)
			}
			readOnly := capability.Method == "GET" || capability.Method == "HEAD"
			if !readOnly && tool.Confirmation == nil {
				return fmt.Errorf("插件 %s: 写入型 agent tool %q 必须声明 confirmation", m.ID, tool.Name)
			}
			if tool.Confirmation != nil {
				if strings.TrimSpace(tool.Confirmation.Title) == "" || strings.TrimSpace(tool.Confirmation.ConfirmLabel) == "" {
					return fmt.Errorf("插件 %s: agent tool %q 的 confirmation 必须声明 title 与 confirm_label", m.ID, tool.Name)
				}
				for _, field := range tool.Confirmation.Fields {
					if strings.TrimSpace(field.Label) == "" || !manifestIdentifier.MatchString(field.Argument) {
						return fmt.Errorf("插件 %s: agent tool %q 的 confirmation field 无效", m.ID, tool.Name)
					}
				}
			}
			switch tool.Risk {
			case "none", "low", "medium", "high", "critical":
			default:
				return fmt.Errorf("插件 %s: agent tool %q 的 risk %q 无效", m.ID, tool.Name, tool.Risk)
			}
			if tool.InputSchema == nil || tool.InputSchema["type"] != "object" {
				return fmt.Errorf("插件 %s: agent tool %q 的 input_schema 必须是 JSON object schema", m.ID, tool.Name)
			}
			if _, err := json.Marshal(tool.InputSchema); err != nil {
				return fmt.Errorf("插件 %s: agent tool %q 的 input_schema 无法编码: %w", m.ID, tool.Name, err)
			}
		}
		seenSkills := make(map[string]struct{}, len(m.Agent.Skills))
		if len(m.Agent.Skills) > 50 {
			return fmt.Errorf("插件 %s: agent.skills 不能超过 50 项", m.ID)
		}
		for _, skill := range m.Agent.Skills {
			if !manifestIdentifier.MatchString(skill.Name) || !strings.HasPrefix(skill.Name, m.ID+".") {
				return fmt.Errorf("插件 %s: agent skill name %q 必须使用插件 id 作为命名空间", m.ID, skill.Name)
			}
			if _, exists := seenSkills[skill.Name]; exists {
				return fmt.Errorf("插件 %s: agent skill name 重复 %q", m.ID, skill.Name)
			}
			seenSkills[skill.Name] = struct{}{}
			if strings.TrimSpace(skill.Description) == "" || strings.TrimSpace(skill.Instructions) == "" {
				return fmt.Errorf("插件 %s: agent skill %q 必须声明 description 与 instructions", m.ID, skill.Name)
			}
			if len(skill.Description) > 500 || len(skill.Instructions) > 8000 {
				return fmt.Errorf("插件 %s: agent skill %q 内容过长", m.ID, skill.Name)
			}
			if len(skill.Tools) == 0 || len(skill.Tools) > 20 {
				return fmt.Errorf("插件 %s: agent skill %q 必须引用 1 到 20 个 agent tools", m.ID, skill.Name)
			}
			seenSkillTools := map[string]struct{}{}
			for _, toolName := range skill.Tools {
				if _, exists := seenTools[toolName]; !exists {
					return fmt.Errorf("插件 %s: agent skill %q 引用了未声明的 agent tool %q", m.ID, skill.Name, toolName)
				}
				if _, duplicate := seenSkillTools[toolName]; duplicate {
					return fmt.Errorf("插件 %s: agent skill %q 重复引用 agent tool %q", m.ID, skill.Name, toolName)
				}
				seenSkillTools[toolName] = struct{}{}
			}
		}
	}

	_, hasUI := capabilities[CapabilityUIModule]
	_, hasUIActions := capabilities[CapabilityUIAction]
	if hasUI && m.UI == nil {
		return fmt.Errorf("插件 %s: capability ui.module 必须声明 ui", m.ID)
	}
	if hasUIActions && m.UI == nil {
		return fmt.Errorf("插件 %s: capability ui.action 必须声明 ui", m.ID)
	}
	if m.UI != nil {
		if !hasUI {
			return fmt.Errorf("插件 %s: 声明 ui 时必须包含 capability ui.module", m.ID)
		}
		if err := validateAssetPath(m.UI.Module); err != nil {
			return fmt.Errorf("插件 %s: ui.module %w", m.ID, err)
		}
		if len(m.UI.Routes) == 0 && len(m.UI.Actions) == 0 && len(m.UI.Cards) == 0 && len(m.UI.Tabs) == 0 && m.UI.Settings == nil {
			return fmt.Errorf("插件 %s: ui 至少需要声明 routes、actions、cards、tabs 或 settings 之一", m.ID)
		}
		if len(m.UI.Actions) > 0 && !hasUIActions {
			return fmt.Errorf("插件 %s: 声明 ui.actions 时必须包含 capability ui.action", m.ID)
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
			if err := validateIdentityKeys(m.ID, "ui route "+route.ID+" required_permissions", route.RequiredPermissions); err != nil {
				return err
			}
			if route.Menu != nil {
				if !manifestIdentifier.MatchString(route.Menu.Section) || !manifestIdentifier.MatchString(route.Menu.Icon) || strings.TrimSpace(route.Menu.Label) == "" {
					return fmt.Errorf("插件 %s: ui route %s 的 menu 必须包含合法 section、label、icon", m.ID, route.ID)
				}
			}
		}
		actionIDs := make(map[string]struct{}, len(routeIDs)+len(m.UI.Actions))
		for id := range routeIDs {
			actionIDs[id] = struct{}{}
		}
		for _, action := range m.UI.Actions {
			if !manifestIdentifier.MatchString(action.ID) {
				return fmt.Errorf("插件 %s: ui action id %q 格式无效", m.ID, action.ID)
			}
			if _, exists := actionIDs[action.ID]; exists {
				return fmt.Errorf("插件 %s: ui 扩展 id 重复 %q", m.ID, action.ID)
			}
			actionIDs[action.ID] = struct{}{}
			if !manifestIdentifier.MatchString(action.Slot) {
				return fmt.Errorf("插件 %s: ui action %s 的 slot %q 格式无效", m.ID, action.ID, action.Slot)
			}
			if !manifestIdentifier.MatchString(action.Export) {
				return fmt.Errorf("插件 %s: ui action %s 的 export %q 格式无效", m.ID, action.ID, action.Export)
			}
			if _, err := validateEntitlements(m.ID, "ui action "+action.ID, action.RequiredEntitlements, declaredEntitlements); err != nil {
				return err
			}
			if err := validateIdentityKeys(m.ID, "ui action "+action.ID+" required_permissions", action.RequiredPermissions); err != nil {
				return err
			}
			if err := validateIdentityKeys(m.ID, "ui action "+action.ID+" forbidden_permissions", action.ForbiddenPermissions); err != nil {
				return err
			}
		}
		for _, card := range m.UI.Cards {
			if !manifestIdentifier.MatchString(card.ID) {
				return fmt.Errorf("插件 %s: ui card id %q 格式无效", m.ID, card.ID)
			}
			if _, exists := actionIDs[card.ID]; exists {
				return fmt.Errorf("插件 %s: ui 扩展 id 重复 %q", m.ID, card.ID)
			}
			actionIDs[card.ID] = struct{}{}
			switch card.Size {
			case "metric", "half", "full":
			default:
				return fmt.Errorf("插件 %s: ui card %s 的 size %q 只支持 metric、half 或 full", m.ID, card.ID, card.Size)
			}
			if !manifestIdentifier.MatchString(card.Export) {
				return fmt.Errorf("插件 %s: ui card %s 的 export %q 格式无效", m.ID, card.ID, card.Export)
			}
			if card.HeaderExport != "" {
				if strings.TrimSpace(card.Title) == "" {
					return fmt.Errorf("插件 %s: ui card %s 声明了 header_export 但缺少 title（header_export 只在宿主标题模式下生效）", m.ID, card.ID)
				}
				if !manifestIdentifier.MatchString(card.HeaderExport) {
					return fmt.Errorf("插件 %s: ui card %s 的 header_export %q 格式无效", m.ID, card.ID, card.HeaderExport)
				}
			}
			if card.PreviewExport != "" && !manifestIdentifier.MatchString(card.PreviewExport) {
				return fmt.Errorf("插件 %s: ui card %s 的 preview_export %q 格式无效", m.ID, card.ID, card.PreviewExport)
			}
			if err := validateCardData(m.ID, card, hasAPI); err != nil {
				return err
			}
			if _, err := validateEntitlements(m.ID, "ui card "+card.ID, card.RequiredEntitlements, declaredEntitlements); err != nil {
				return err
			}
			if err := validateIdentityKeys(m.ID, "ui card "+card.ID+" required_permissions", card.RequiredPermissions); err != nil {
				return err
			}
			if err := validateIdentityKeys(m.ID, "ui card "+card.ID+" required_any_permissions", card.RequiredAnyPermissions); err != nil {
				return err
			}
			if err := validateIdentityKeys(m.ID, "ui card "+card.ID+" forbidden_permissions", card.ForbiddenPermissions); err != nil {
				return err
			}
		}
		for _, tab := range m.UI.Tabs {
			if !manifestIdentifier.MatchString(tab.ID) {
				return fmt.Errorf("插件 %s: ui tab id %q 格式无效", m.ID, tab.ID)
			}
			if _, exists := actionIDs[tab.ID]; exists {
				return fmt.Errorf("插件 %s: ui 扩展 id 重复 %q", m.ID, tab.ID)
			}
			actionIDs[tab.ID] = struct{}{}
			if strings.TrimSpace(tab.Label) == "" {
				return fmt.Errorf("插件 %s: ui tab %s 缺少 label", m.ID, tab.ID)
			}
			if !manifestIdentifier.MatchString(tab.Export) {
				return fmt.Errorf("插件 %s: ui tab %s 的 export %q 格式无效", m.ID, tab.ID, tab.Export)
			}
			if _, err := validateEntitlements(m.ID, "ui tab "+tab.ID, tab.RequiredEntitlements, declaredEntitlements); err != nil {
				return err
			}
		}
		if m.UI.Settings != nil {
			if !manifestIdentifier.MatchString(m.UI.Settings.Export) {
				return fmt.Errorf("插件 %s: ui settings 的 export %q 格式无效", m.ID, m.UI.Settings.Export)
			}
			switch m.UI.Settings.Mode {
			case "", "replace", "extend":
			default:
				return fmt.Errorf("插件 %s: ui settings 的 mode %q 只支持 replace 或 extend", m.ID, m.UI.Settings.Mode)
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
		flowIDs := make(map[string]struct{}, len(m.Identity.Flows))
		for _, flow := range m.Identity.Flows {
			if !manifestIdentifier.MatchString(flow.ID) {
				return fmt.Errorf("插件 %s: identity flow id %q 格式无效", m.ID, flow.ID)
			}
			if _, exists := flowIDs[flow.ID]; exists {
				return fmt.Errorf("插件 %s: identity flow id 重复 %q", m.ID, flow.ID)
			}
			flowIDs[flow.ID] = struct{}{}
			if flow.Type != IdentityFlowCredentials && flow.Type != IdentityFlowOIDC {
				return fmt.Errorf("插件 %s: identity flow %s 类型 %q 不受支持", m.ID, flow.ID, flow.Type)
			}
			if strings.TrimSpace(flow.Label) == "" {
				return fmt.Errorf("插件 %s: identity flow %s 缺少 label", m.ID, flow.ID)
			}
		}
	}
	return nil
}

func validateAPIServiceCapability(pluginID, owner, name, method, path string, seen map[string]struct{}) error {
	if !manifestIdentifier.MatchString(name) {
		return fmt.Errorf("插件 %s: %s 能力名 %q 格式无效", pluginID, owner, name)
	}
	if _, dup := seen[name]; dup {
		return fmt.Errorf("插件 %s: api 能力名重复 %q", pluginID, name)
	}
	seen[name] = struct{}{}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return fmt.Errorf("插件 %s: %s 能力 %q 的 method %q 无效", pluginID, owner, name, method)
	}
	if !validAPICapabilityPath(path) {
		return fmt.Errorf("插件 %s: %s 能力 %q 的 path 必须以 / 开头", pluginID, owner, name)
	}
	return nil
}

func validAPICapabilityPath(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, `\\%?#`) || strings.TrimSpace(value) != value {
		return false
	}
	for _, segment := range strings.Split(strings.Trim(value, "/"), "/") {
		if segment == "." || segment == ".." {
			return false
		}
		if strings.ContainsAny(segment, "{}") {
			if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' || !manifestIdentifier.MatchString(segment[1:len(segment)-1]) {
				return false
			}
		}
	}
	return true
}

func validateIdentityKeys(pluginID, owner string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !manifestIdentifier.MatchString(value) {
			return fmt.Errorf("插件 %s: %s %q 格式无效", pluginID, owner, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("插件 %s: %s 重复 %q", pluginID, owner, value)
		}
		seen[value] = struct{}{}
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
	return p.Manifest.HasExactCapability(capability)
}

// HasExactCapability 判断 manifest 是否声明了某个完整 capability。宿主在只拿到
// manifest（还没构造 Plugin）的地方用它，比如加载插件包时决定要不要给它建常驻池。
func (m Manifest) HasExactCapability(capability string) bool {
	for _, c := range m.Capabilities {
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
