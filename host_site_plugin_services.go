package pluginsdk

import (
	"context"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// 本文件是站点插件迁出宿主进程后需要的三项宿主服务。它们原先都是宿主进程内的直接
// 调用：渲染网关是注入的 Go 对象，cloud 凭据是共享的 *http.Client，规则目录是一个
// 全局字符串变量。跨进程之后这些都传不过去，只能成为显式的宿主服务契约。

// PageRenderer 让插件借宿主已启用的浏览器渲染插件取回页面。
//
// 站点插件在账号开启「浏览器仿真」时用它绕过 Cloudflare / DDoS-GUARD 挑战。
// 渲染插件是谁、怎么配置由宿主决定，插件既选不了也读不到它的配置。
//
// 需要 host 权限 "renderer.page"。
type PageRenderer interface {
	// RendererAvailable 报告当前有没有可用的渲染插件。它决定「浏览器仿真」开关
	// 对用户是否可见，所以查询失败按不可用处理，不额外返回错误。
	RendererAvailable(ctx context.Context) bool
	RenderPage(ctx context.Context, req providers.RenderRequest) (providers.RenderResult, error)
}

// CloudCredential 是一次云端调用所需的最小凭据。
//
// Token 是宿主换来的短期访问令牌，插件只能拿它去 BaseURL 发请求。实例长期密钥
// （instance secret）绝不在这里出现——它一旦进了插件进程，闭源插件的信任边界就
// 名存实亡了。
type CloudCredential struct {
	BaseURL    string `json:"base_url"`
	Token      string `json:"token"`
	InstanceID string `json:"instance_id,omitempty"`
	// ExpiresAt 是 RFC3339 过期时刻，可能为空（宿主无法确定时）。插件不应缓存
	// 超过这个时刻的令牌，过期后重新向宿主索取。
	ExpiresAt string `json:"expires_at,omitempty"`
}

// CloudIdentity 让插件以本实例的身份访问云端。
// 需要 host 权限 "cloud.identity"。
type CloudIdentity interface {
	CloudCredential(ctx context.Context) (CloudCredential, error)
}

// SiteRuleFiles 只读访问宿主的站点规则目录（data/site-rules）。
//
// 那是管理员自己往里放 YAML 的用户可见目录，不是插件私有工作区，所以不能用
// Workspace 顶替：换成插件私有目录会改掉一条既有的、用户已经在用的路径。
// 宿主只给读、只给这个目录，插件拿不到路径也写不进去。
//
// 需要 host 权限 "site.rules.read"。
type SiteRuleFiles interface {
	// ListSiteRuleFiles 返回目录下的文件名（不含路径）。目录不存在时返回空列表
	// 而不是错误——没有自有规则是正常状态。
	ListSiteRuleFiles(ctx context.Context) ([]string, error)
	// ReadSiteRuleFile 读取其中一个文件。name 必须是 ListSiteRuleFiles 返回过的
	// 纯文件名，宿主会拒绝任何带路径分隔符或 .. 的名字。
	ReadSiteRuleFile(ctx context.Context, name string) ([]byte, error)
}
