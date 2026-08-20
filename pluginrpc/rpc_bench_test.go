package pluginrpc

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// 这组基准量的是宿主与插件之间的**通信成本**，不是插件业务耗时：宿主侧的 KV 实现是
// 空壳，插件侧的 action 除了回调什么都不做。逐层加码，每一层的差值就是那一层的代价：
//
//	A → B   instance payload 的成本
//	B → C   host-services 通道建立与拆除的成本（插件一次回调都没发）
//	C → D   第一次回调（含 gob 在新连接上传类型描述符）
//	D → E   后续每次回调的边际成本
//	F       Config 大小的影响（每次 RPC 都重新 marshal 并整份发送）
//	G → H   每次操作 fork 一个进程的代价，对照常驻
//
// 带 Legacy 后缀的几条把客户端钉在老插件形态上（不复用通道、不用配置摘要），是优化项
// 的对照基线；不带后缀的是今天的实际路径，两者的差就是这项优化的收益。
//
// 改动 RPC 路径后请重跑并对照 PACK_REVIEW.md 附二记录的基线。

// ---- 宿主侧的假实现 ----

type benchKV struct{}

func (benchKV) Get(ctx context.Context, key string, out any) (bool, error)          { return false, nil }
func (benchKV) Set(ctx context.Context, key string, v any, ttl time.Duration) error { return nil }
func (benchKV) Delete(ctx context.Context, key string) error                        { return nil }
func (benchKV) DeletePrefix(ctx context.Context, prefix string) error               { return nil }

type benchChecker struct{}

func (benchChecker) CheckPluginPermission(ctx context.Context, pluginID, scopeType, scopeID, permission string, manifest pluginsdk.Manifest) error {
	return nil
}

func benchManifest() pluginsdk.Manifest {
	return pluginsdk.Manifest{
		ID: "benchplugin", Name: "benchplugin", Version: "0.0.1", Type: "cli",
		Capabilities: []string{"actions"},
		Permissions:  pluginsdk.Permissions{Data: []string{"storage"}},
	}
}

// benchBinary 每个测试二进制只编译一次插件；编译耗时不该计入任何一条基准。
var benchBinary = sync.OnceValues(func() (string, error) {
	source, err := filepath.Abs(filepath.Join("..", "internal", "benchplugin"))
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "pluginrpc-bench")
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, "benchplugin")
	cmd := exec.Command("go", "build", "-o", out, source)
	if combined, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("编译基准插件: %w\n%s", err, combined)
	}
	benchBuildDir = dir
	return out, nil
})

// benchBuildDir 记录 benchBinary 建出的临时目录，TestMain 在退出前删掉它。
var benchBuildDir string

// TestMain 只为清理基准插件的编译产物存在：sync.Once 里拿不到 testing.TB，
// 没有这个钩子每跑一次基准就会在 /tmp 里留下一份二进制。
func TestMain(m *testing.M) {
	code := m.Run()
	if benchBuildDir != "" {
		os.RemoveAll(benchBuildDir)
	}
	os.Exit(code)
}

func benchPluginBinary(tb testing.TB) string {
	tb.Helper()
	path, err := benchBinary()
	if err != nil {
		tb.Fatal(err)
	}
	return path
}

func benchClientConfig(bin string) ClientConfig {
	manifest := benchManifest()
	return ClientConfig{
		Command:           bin,
		Manifest:          manifest,
		Permissions:       manifest.Permissions,
		ScopeType:         "bench",
		ScopeID:           "bench",
		Stderr:            io.Discard,
		PermissionChecker: benchChecker{},
	}
}

// startBenchClient 起一个常驻插件进程，模拟官方 Pack 的形态（进程复用，只量单次 RPC）。
func startBenchClient(tb testing.TB) *Client {
	tb.Helper()
	running, err := startClient(context.Background(), benchClientConfig(benchPluginBinary(tb)))
	if err != nil {
		tb.Fatalf("startClient: %v", err)
	}
	tb.Cleanup(running.Close)
	return running.client
}

// benchInstance 造一个实例。withServices 决定宿主挂不挂 KV，进而决定要不要开
// host-services 通道；configFields 控制 Config 大小。
func benchInstance(withServices bool, configFields int) pluginsdk.Instance {
	config := make(map[string]any, configFields)
	for i := 0; i < configFields; i++ {
		config[fmt.Sprintf("field_%03d", i)] = strings.Repeat("x", 64)
	}
	inst := pluginsdk.Instance{ID: "inst-1", Name: "bench", Config: config}
	if withServices {
		inst.KV = benchKV{}
	}
	return inst
}

// benchRunAction 是 B–F 的公共循环：它们只在 instance 和 action 上有区别。
func benchRunAction(b *testing.B, inst pluginsdk.Instance, actionID string) {
	benchRunActionWith(b, inst, actionID, false)
}

// benchRunActionWith 的 legacy 为真时把客户端钉在「老插件」形态上：不用配置摘要，也
// 不复用 host-services 通道。优化项的基线要跟它比。
func benchRunActionWith(b *testing.B, inst pluginsdk.Instance, actionID string, legacy bool) {
	client := startBenchClient(b)
	if legacy {
		benchForceLegacy(client)
	}
	ctx := context.Background()
	// 先跑一次：把进程预热和首次 gob 类型协商挡在计时之外。
	if _, err := client.RunActionContext(ctx, inst, nil, actionID, nil); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.RunActionContext(ctx, inst, nil, actionID, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// benchForceLegacy 假装探测过且对端什么新特性都不支持，省得为了基线再编译一份老 SDK。
func benchForceLegacy(client *Client) {
	client.featuresMu.Lock()
	defer client.featuresMu.Unlock()
	client.featuresProbed, client.features = true, FeaturesReply{}
}

func kvAction(n int) string { return fmt.Sprintf("kv:%d", n) }

// A. 纯传输下限：没有 instance payload，也不开 host-services 通道。
func BenchmarkRPC_A_ManifestOnly(b *testing.B) {
	client := startBenchClient(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Manifest(); err != nil {
			b.Fatal(err)
		}
	}
}

// B. 带 instance payload，但宿主没挂任何服务，因此不开通道。
func BenchmarkRPC_B_ActionNoServices(b *testing.B) {
	benchRunAction(b, benchInstance(false, 8), "noop")
}

// C. 挂上 KV，插件不发起任何回调。C 与 B 的差就是 host-services 通道本身的代价：
// Legacy 那条每次调用建一条拆一条，不带后缀的那条复用池里的通道（hostchannel.go）。
func BenchmarkRPC_C_ActionWithChannelLegacy(b *testing.B) {
	benchRunActionWith(b, benchInstance(true, 8), "noop", true)
}

func BenchmarkRPC_C_ActionWithChannel(b *testing.B) {
	benchRunAction(b, benchInstance(true, 8), "noop")
}

// D/E. 插件回调宿主 N 次，量首次与后续每次的成本。通道复用之后 gob 的类型描述符只在
// 建通道时传一次，所以 D 的首次回调不再比 E 的后续每次贵一倍。
func BenchmarkRPC_D_Callback1Legacy(b *testing.B) {
	benchRunActionWith(b, benchInstance(true, 8), kvAction(1), true)
}

func BenchmarkRPC_D_Callback1(b *testing.B) {
	benchRunAction(b, benchInstance(true, 8), kvAction(1))
}

func BenchmarkRPC_E_Callback10Legacy(b *testing.B) {
	benchRunActionWith(b, benchInstance(true, 8), kvAction(10), true)
}

func BenchmarkRPC_E_Callback10(b *testing.B) {
	benchRunAction(b, benchInstance(true, 8), kvAction(10))
}

// F. Config 大小的影响：整份发时，每次 RPC 都要重新 json.Marshal 并搬运整份 Config。
// 后两条是配置摘要（configdigest.go）生效后的同一场景，差值就是这项优化的收益。
func BenchmarkRPC_F_Config8(b *testing.B) {
	benchRunActionWith(b, benchInstance(false, 8), "noop", true)
}

func BenchmarkRPC_F_Config200(b *testing.B) {
	benchRunActionWith(b, benchInstance(false, 200), "noop", true)
}

func BenchmarkRPC_F_Config8Digest(b *testing.B) {
	benchRunAction(b, benchInstance(false, 8), "noop")
}

func BenchmarkRPC_F_Config200Digest(b *testing.B) {
	benchRunAction(b, benchInstance(false, 200), "noop")
}

// G. 第三方 cli 插件未声明常驻时的形态：每次操作 fork 进程 + 握手 + 一次调用 + Kill。
func BenchmarkRPC_G_SpawnPerOperation(b *testing.B) {
	config := benchClientConfig(benchPluginBinary(b))
	ctx := context.Background()
	inst := benchInstance(true, 8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		running, err := startClient(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := running.client.RunActionContext(ctx, inst, nil, "noop", nil); err != nil {
			b.Fatal(err)
		}
		running.Close()
	}
}

// H. 同一场景下声明了 lifecycle.resident：进程跨操作存活，每次操作只剩一次 RPC 的
// 成本。H 与 G 的差就是「每次操作 fork 一个进程」的代价。
func BenchmarkRPC_H_ResidentPerOperation(b *testing.B) {
	pool := NewResidentPool(ResidentPoolOptions{})
	defer pool.Close()
	external := ExternalPlugin{
		Manifest:          benchManifest(),
		Command:           benchPluginBinary(b),
		Stderr:            io.Discard,
		PermissionChecker: benchChecker{},
		Resident:          pool,
	}
	ctx := context.Background()
	inst := benchInstance(true, 8)
	run := func() {
		err := external.withClientForScopeOperation(ctx, "storage", inst.ID, "plugin.rpc", func(client *Client) error {
			_, err := client.RunActionContext(ctx, inst, nil, "noop", nil)
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	// 先跑一次：进程预热与首次 gob 类型协商不该计进每次操作的成本。
	run()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}
