package pluginsdk

import (
	"os"
	"runtime"
	"strings"
	"sync/atomic"
)

// AppName 是宿主和插件对外统一的应用名。OpenSubtitles 这类服务按「一个应用一个
// 标识」做调用统计，改掉它等于换了个应用，别为了好看随手动。
//
// 是常量不是函数：插件常拿它拼编译期常量，函数化会平白逼出一堆 var。
const AppName = "MediaAgent"

// appVersionEnv 是宿主把版本号交给插件进程的通道。插件跑在 go-plugin 拉起的子
// 进程里，默认继承宿主环境（见 pluginrpc 启动子进程时的 os.Environ()），所以
// 宿主 SetAppVersion 一次，插件侧就拿到同一个版本号：不用多一轮 RPC，也不用等
// Instance 注入——插件建 http.Client 时往往还没有实例。
//
// 只传版本号，不传拼好的 UA：平台是编译期就定死的，插件自己算即可（跟宿主同一
// 个平台）；而有些服务要的根本不是标准 product/version 形状，插件得自己拼。
const appVersionEnv = "MEDIA_AGENT_APP_VERSION"

// devVersion 是取不到版本号时的兜底。不留空：空版本号会拼出 "MediaAgent/" 这种
// 畸形 UA，有些服务干脆当成没有 UA 拒掉，而那种失败在日志里没有任何线索。
const devVersion = "dev"

var appVersion atomic.Pointer[string]

// SetAppVersion 由宿主在启动时调用一次，把编译期版本号定为全局应用版本。
// 它同时写进本进程环境变量，好让之后拉起的插件子进程继承。插件不需要调它。
func SetAppVersion(version string) {
	v := normalizeVersion(version)
	appVersion.Store(&v)
	// 忽略错误：Setenv 只在 key 非法时失败，这里的 key 是常量。
	_ = os.Setenv(appVersionEnv, v)
}

// AppVersion 返回归一化后的应用版本号（去掉前导 v），形如 "0.31.0"。宿主进程取
// SetAppVersion 定下的值，插件进程取继承来的环境变量，两边都没有时返回 "dev"。
func AppVersion() string {
	if v := appVersion.Load(); v != nil && *v != "" {
		return *v
	}
	return normalizeVersion(os.Getenv(appVersionEnv))
}

// AppPlatform 返回运行平台，形如 "darwin-arm64"。跟插件包制品名和插件商店的
// platform 字段是同一种写法，排查时日志能直接对上。
//
// 不走环境变量：插件进程跟宿主是同一个平台，各自按编译期常量算就行。
func AppPlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// UserAgent 返回完整 User-Agent，形如 "MediaAgent/0.31.0 (darwin-arm64)"。
//
// 要自定义格式的插件——比如 OpenSubtitles 认的是 "MediaAgent v0.31.0"，跟标准的
// product/version 不是一个形状——用 AppName / AppVersion() / AppPlatform() 自己拼。
func UserAgent() string {
	return AppName + "/" + AppVersion() + " (" + AppPlatform() + ")"
}

func normalizeVersion(version string) string {
	v := strings.TrimPrefix(stripUnsafe(version), "v")
	if v == "" {
		return devVersion
	}
	return v
}

// stripUnsafe 丢掉版本号里的空白和控制字符。版本号来自编译期 ldflags，正常不会
// 有，但真混进空格会被对端当成两个 product token，混进 \r\n 则整个请求头作废。
func stripUnsafe(version string) string {
	return strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f {
			return -1
		}
		return r
	}, version)
}
