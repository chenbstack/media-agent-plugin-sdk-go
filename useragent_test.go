package pluginsdk

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// resetAppVersion 让每个用例从「宿主没设过」的状态开始。
func resetAppVersion(t *testing.T) {
	t.Helper()
	appVersion.Store(nil)
	t.Setenv(appVersionEnv, "")
	os.Unsetenv(appVersionEnv)
	t.Cleanup(func() { appVersion.Store(nil) })
}

func TestUserAgentFromAppVersion(t *testing.T) {
	resetAppVersion(t)

	SetAppVersion("v0.31.0")

	if got := AppVersion(); got != "0.31.0" {
		t.Fatalf("AppVersion() = %q，想要 0.31.0", got)
	}
	want := "MediaAgent/0.31.0 (" + runtime.GOOS + "-" + runtime.GOARCH + ")"
	if got := UserAgent(); got != want {
		t.Fatalf("UserAgent() = %q，想要 %q", got, want)
	}
}

func TestAppPlatform(t *testing.T) {
	want := runtime.GOOS + "-" + runtime.GOARCH
	if got := AppPlatform(); got != want {
		t.Fatalf("AppPlatform() = %q，想要 %q", got, want)
	}
}

// 四个部件各自可取，插件才能拼自己那套格式（OpenSubtitles 认的是
// "MediaAgent v0.31.0"，跟标准的 product/version 不是一个形状）。
func TestUserAgentPartsComposeIndependently(t *testing.T) {
	resetAppVersion(t)
	SetAppVersion("v0.31.0")

	if got := AppName + " v" + AppVersion(); got != "MediaAgent v0.31.0" {
		t.Fatalf("自拼 UA = %q，想要 MediaAgent v0.31.0", got)
	}
}

// 宿主把版本号写进自己的环境变量，插件子进程才能继承到同一个值。
func TestSetAppVersionExportsEnvForChildProcesses(t *testing.T) {
	resetAppVersion(t)

	SetAppVersion("v0.31.0")

	if got := os.Getenv(appVersionEnv); got != "0.31.0" {
		t.Fatalf("%s = %q，想要 0.31.0", appVersionEnv, got)
	}
}

// 插件进程从来不调 SetAppVersion，它只有继承来的环境变量。
func TestAppVersionFallsBackToInheritedEnv(t *testing.T) {
	resetAppVersion(t)
	t.Setenv(appVersionEnv, "0.31.0")

	if got := AppVersion(); got != "0.31.0" {
		t.Fatalf("AppVersion() = %q，想要 0.31.0", got)
	}
}

// 两边都没有也必须给出可用的值：空版本号会拼出 "MediaAgent/" 这种畸形 UA。
func TestAppVersionNeverEmpty(t *testing.T) {
	resetAppVersion(t)

	if got := AppVersion(); got != "dev" {
		t.Fatalf("AppVersion() = %q，想要 dev", got)
	}
	if got := UserAgent(); !strings.HasPrefix(got, "MediaAgent/dev (") {
		t.Fatalf("UserAgent() = %q，想要以 MediaAgent/dev ( 开头", got)
	}
}

func TestAppVersionEmptyFallsBackToDev(t *testing.T) {
	resetAppVersion(t)

	SetAppVersion("   ")

	if got := AppVersion(); got != "dev" {
		t.Fatalf("AppVersion() = %q，想要 dev", got)
	}
}

// 开发构建的版本号带 build metadata，原样保留——UA 里能看出是哪个提交很有用。
func TestAppVersionKeepsBuildMetadata(t *testing.T) {
	resetAppVersion(t)

	SetAppVersion("v0.31.0+18.ge55934e.dirty")

	if got := AppVersion(); got != "0.31.0+18.ge55934e.dirty" {
		t.Fatalf("AppVersion() = %q", got)
	}
}

// 插件拿到版本号全靠子进程继承宿主环境这条链路。它断了不会报错，只会静默退回
// dev——所以真的拉个子进程验一遍，而不是只测 Setenv 写没写进去。
func TestAppVersionInheritedByChildProcess(t *testing.T) {
	if os.Getenv("PLUGINSDK_UA_CHILD") == "1" {
		fmt.Print(UserAgent())
		os.Exit(0)
	}

	resetAppVersion(t)
	SetAppVersion("v9.9.9")

	cmd := exec.Command(os.Args[0], "-test.run=TestAppVersionInheritedByChildProcess")
	// 只追加标记位，其余原样继承——pluginrpc 拉起插件进程时就是这么做的。
	cmd.Env = append(os.Environ(), "PLUGINSDK_UA_CHILD=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("子进程执行失败: %v", err)
	}

	want := "MediaAgent/9.9.9 (" + runtime.GOOS + "-" + runtime.GOARCH + ")"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("子进程 UserAgent() = %q，想要 %q", got, want)
	}
}

// 版本号里混进换行会毁掉整个请求头，必须在拼进 UA 之前丢掉。
func TestAppVersionStripsControlCharacters(t *testing.T) {
	resetAppVersion(t)

	SetAppVersion("v0.31.0\r\nX-Injected: 1")

	if got := AppVersion(); got != "0.31.0X-Injected:1" {
		t.Fatalf("AppVersion() = %q，控制字符和空格应被丢掉", got)
	}
}

// 继承来的环境变量同样要过一遍清洗：插件进程信不过自己拿到的环境。
func TestAppVersionSanitizesInheritedEnv(t *testing.T) {
	resetAppVersion(t)
	t.Setenv(appVersionEnv, "v0.31.0\r\nX-Injected: 1")

	if got := AppVersion(); got != "0.31.0X-Injected:1" {
		t.Fatalf("AppVersion() = %q，控制字符和空格应被丢掉", got)
	}
}
