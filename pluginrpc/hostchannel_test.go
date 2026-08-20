package pluginrpc

import (
	"context"
	"sync"
	"testing"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// countingKV 记下插件回调了几次，用来确认复用通道之后回调照样打得到宿主。
type countingKV struct {
	mu   sync.Mutex
	gets int
}

func (k *countingKV) Get(ctx context.Context, key string, out any) (bool, error) {
	k.mu.Lock()
	k.gets++
	k.mu.Unlock()
	return false, nil
}

func (k *countingKV) Set(context.Context, string, any, time.Duration) error { return nil }
func (k *countingKV) Delete(context.Context, string) error                  { return nil }
func (k *countingKV) DeletePrefix(context.Context, string) error            { return nil }

func runCallbackAction(t *testing.T, client *Client, inst pluginsdk.Instance) {
	t.Helper()
	if _, err := client.RunActionContext(context.Background(), inst, nil, kvAction(1), nil); err != nil {
		t.Fatalf("调用插件: %v", err)
	}
}

// 复用的本分：连着三次带回调的调用只建一条通道，回调一次不少。
func TestHostChannelReusedAcrossCalls(t *testing.T) {
	client := startBenchClient(t)
	kv := &countingKV{}
	inst := pluginsdk.Instance{ID: "inst-1", Name: "bench", KV: kv}

	for i := 0; i < 3; i++ {
		runCallbackAction(t, client, inst)
	}

	pool := client.hostChannels
	pool.mu.Lock()
	live, idle := pool.live["inst-1"], len(pool.idle["inst-1"])
	pool.mu.Unlock()
	if live != 1 || idle != 1 {
		t.Fatalf("通道数 live=%d idle=%d，三次调用应当共用一条并已归还", live, idle)
	}
	if kv.gets != 3 {
		t.Fatalf("宿主收到 %d 次回调，应当是 3 次", kv.gets)
	}
}

// 老插件每次调用都会重新 Dial，池化的通道它拿不到第二次——所以宿主探测到对端不支持
// 复用时必须退回「每次一条」的老路，而不是把调用挂死在无人 accept 的通道上。
func TestHostChannelSkippedForLegacyPlugin(t *testing.T) {
	client := startBenchClient(t)
	benchForceLegacy(client)
	kv := &countingKV{}
	inst := pluginsdk.Instance{ID: "inst-1", Name: "bench", KV: kv}

	for i := 0; i < 3; i++ {
		runCallbackAction(t, client, inst)
	}

	pool := client.hostChannels
	pool.mu.Lock()
	live := len(pool.live)
	pool.mu.Unlock()
	if live != 0 {
		t.Fatalf("老插件不该用到通道池，live=%d", live)
	}
	if kv.gets != 3 {
		t.Fatalf("宿主收到 %d 次回调，应当是 3 次", kv.gets)
	}
}

// 通道还回池里之后，插件里跑飞的 goroutine 迟到的回调必须失败，不能打在下一次调用的
// 服务上。这与今天「调用结束通道被拆掉」的边界等价。
func TestHostChannelReleaseRejectsLateCallbacks(t *testing.T) {
	pool := newHostChannelPool()
	kv := &countingKV{}
	channel := &hostChannel{server: newHostServicesServer(&hostServicesState{
		ctx:         context.Background(),
		permissions: pluginsdk.Permissions{Data: []string{"storage"}},
		kv:          kv,
	})}

	var reply KVGetReply
	if err := channel.server.KVGet(KVGetRequest{Key: "k"}, &reply); err != nil {
		t.Fatalf("租用期间的回调应当放行: %v", err)
	}
	pool.put("inst-1", channel)
	if err := channel.server.KVGet(KVGetRequest{Key: "k"}, &reply); err == nil {
		t.Fatal("归还之后的回调应当失败")
	}
	if kv.gets != 1 {
		t.Fatalf("宿主被回调 %d 次，归还之后不该再有", kv.gets)
	}
}
