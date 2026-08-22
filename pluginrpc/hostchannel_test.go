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

// 通道没等到插件连上来就不能进池：宿主的 accept 只等 5 秒，之后那个 broker id 就是
// 死号，谁借到谁的调用先卡满 5 秒再失败。
func TestHostChannelNotPooledWhenNeverAccepted(t *testing.T) {
	pool := newHostChannelPool()
	pool.live["inst-1"] = 1
	channel := &hostChannel{id: 7, server: newHostServicesServer(&hostServicesState{ctx: context.Background()})}

	pool.put("inst-1", channel)

	pool.mu.Lock()
	idle, live := len(pool.idle["inst-1"]), pool.live["inst-1"]
	pool.mu.Unlock()
	if idle != 0 {
		t.Fatalf("没连上来的通道进了池，idle=%d", idle)
	}
	if live != 0 {
		t.Fatalf("名额没交还，live=%d", live)
	}
}

// 停止服务的通道要从池子里摘掉，名额一并交还：连接断了之后再借出去，那次调用必然失败。
func TestHostChannelRetiredWhenServingEnds(t *testing.T) {
	pool := newHostChannelPool()
	pool.live["inst-1"] = 1
	channel := &hostChannel{id: 9, server: newHostServicesServer(&hostServicesState{ctx: context.Background()})}
	channel.accepted.Store(true)

	pool.put("inst-1", channel)
	pool.mu.Lock()
	pooled := len(pool.idle["inst-1"])
	pool.mu.Unlock()
	if pooled != 1 {
		t.Fatalf("连上过又正常归还的通道应当留在池里，idle=%d", pooled)
	}

	channel.finished.Store(true)
	pool.retire("inst-1", channel)

	pool.mu.Lock()
	idle, live := len(pool.idle["inst-1"]), pool.live["inst-1"]
	pool.mu.Unlock()
	if idle != 0 || live != 0 {
		t.Fatalf("停服的通道没摘干净，idle=%d live=%d", idle, live)
	}
}

// 生产上真实发生过的那条路径：插件没实现这个方法，调用在组装实例句柄之前就返回了，
// 那条已经开好的通道从头到尾没人 Dial。它要是留在池里，同一个实例之后的每一次调用都会
// 借到这个死号，卡满 5 秒再失败，一直到插件进程重启为止。
func TestHostChannelNotReusedAfterPluginNeverConnected(t *testing.T) {
	if testing.Short() {
		t.Skip("要等满 broker accept 的 5 秒超时")
	}
	client := startBenchClient(t)
	kv := &countingKV{}
	inst := pluginsdk.Instance{ID: "inst-1", Name: "bench", KV: kv}

	// 基准插件只实现了 ActionHandler，事件订阅这条会早退。
	if err := client.HandleEventContext(context.Background(), inst, nil, pluginsdk.EventEnvelope{Type: "noop"}); err == nil {
		t.Fatal("基准插件没实现事件订阅，这次调用应当失败")
	}
	// 等宿主那条通道的 accept 超时，死号才真的死掉。
	time.Sleep(6 * time.Second)

	runCallbackAction(t, client, inst)
	if kv.gets != 1 {
		t.Fatalf("宿主收到 %d 次回调，应当是 1 次", kv.gets)
	}
}
