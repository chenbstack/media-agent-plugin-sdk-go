package pluginsdk

import (
	"context"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// 本文件是站点插件迁出宿主进程后需要的宿主服务。前三项原先都是宿主进程内的直接
// 调用：渲染网关是注入的 Go 对象，cloud 凭据是共享的 *http.Client，规则目录是一个
// 全局字符串变量。跨进程之后这些都传不过去，只能成为显式的宿主服务契约。
//
// 后两项（SiteRulePackFiles / SiteRulePackKeys）服务于加密站点规则包：宿主负责取
// 回并校验密文、派生实例绑定密钥，解密始终闭合在插件进程内。

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

// SiteRulePackFiles 只读访问宿主缓存的加密站点规则包（data/site-rules-cache）。
//
// 包里是密文。宿主下载、校验制品摘要与 Ed25519 签名、按版本落盘，但**不解密**：
// 主程序是开源的，把解密放进来等于连密钥带规则一起公开，攻击者改一行重新编译即可
// dump 全部规则。解密与解析必须闭合在插件进程内。
//
// 需要 host 权限 "site.rules.pack.read"。
type SiteRulePackFiles interface {
	// ListSiteRulePackVersions 返回本地可用的包版本，升序。宿主会滤掉校验不过和
	// 已过期的版本，所以空列表是正常状态——还没下到包，或本地的包都过期了。
	ListSiteRulePackVersions(ctx context.Context) ([]int64, error)
	// ReadSiteRulePackFile 读取某个版本里的一个条目（manifest.json / manifest.sig
	// / rules.bin）。name 必须是纯文件名，宿主会拒绝任何带路径分隔符或 .. 的名字，
	// 并在每次读取时重新校验该版本是否仍然有效，插件绕不过这道门。
	ReadSiteRulePackFile(ctx context.Context, version int64, name string) ([]byte, error)
}

// SiteRulePackKeys 派生与本实例绑定的密钥，供插件把规则包密钥封存到本地。
//
// 返回的是 HKDF 派生结果，不是实例长期密钥本身——与 CloudCredential 只发短期令牌
// 是同一条边界。插件必须再叠一层自己的内嵌常量才得到最终的封存密钥，两半各挡一种
// 攻击：宿主开源，改一行 dump 出来的只有派生结果，缺插件内嵌常量解不开缓存；缓存
// 文件拷到另一台机器，那边的实例密钥不同，派生结果对不上。缺任何一层都不成立。
//
// 需要 host 权限 "site.rules.pack.keys"。
type SiteRulePackKeys interface {
	// InstanceKey 返回绑定到本实例与该包版本的 32 字节密钥。版本参与派生，换包
	// 就换密钥，旧版本的缓存自然失效，不需要额外的清理逻辑。
	InstanceKey(ctx context.Context, packVersion int64) ([]byte, error)
}
