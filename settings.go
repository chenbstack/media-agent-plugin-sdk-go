package pluginsdk

import (
	"context"
	"encoding/json"
)

// Settings 是宿主注入的全局设置访问器（app_settings），让插件复用系统设置里的
// 基础设施配置（如网络代理），而不必各自重复开配置项。key 用宿主与插件共享的知名常量；
// 放行范围由宿主决定（宿主可只注入允许插件读取的子集）。敏感值（密钥）不经此接口，仍走
// SecretResolver。与 KV/DB/Logger 一样按 Instance 注入，可为 nil（表示宿主未提供设置访问）。
type Settings interface {
	// String 读取字符串设置；ok 为 false 表示不存在或类型不符。
	String(ctx context.Context, key string) (value string, ok bool)
	// Int 读取整数设置。
	Int(ctx context.Context, key string) (value int64, ok bool)
	// Bool 读取布尔设置。
	Bool(ctx context.Context, key string) (value bool, ok bool)
	// JSON 把设置值解码进 out；ok 为 false 表示该设置不存在，err 表示存在但解码失败。
	JSON(ctx context.Context, key string, out any) (ok bool, err error)
	// SetSetting 写入一个合法 JSON 设置值。宿主必须单独校验 settings.write 权限。
	SetSetting(ctx context.Context, input SettingWrite) (HostWriteResult, error)
}

type SettingWrite struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// 宿主与插件共享的知名设置 key（app_settings 表）。集中声明避免各处硬编码字符串漂移。
const (
	// SettingNetworkProxy 是通用网络代理服务器地址（HTTP/HTTPS/SOCKS，如 http://127.0.0.1:7890）。
	SettingNetworkProxy = "network.proxy_server"
	// SettingGitHubProxy 是 GitHub 加速代理前缀（URL 镜像，如 https://gh-proxy.example.com/）。
	SettingGitHubProxy = "network.github_proxy"
)
