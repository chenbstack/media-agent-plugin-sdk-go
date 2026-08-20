package pluginrpc

import (
	"context"
	"io"
	"sync"
	"syscall"
	"testing"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// startRecorder 记下每次真正 fork 出来的插件进程，用来断言「第二次调用没再起进程」。
type startRecorder struct {
	mu   sync.Mutex
	pids []int
}

func (r *startRecorder) PluginProcessStarted(info ProcessStartInfo) func() {
	r.mu.Lock()
	r.pids = append(r.pids, info.PID)
	r.mu.Unlock()
	return func() {}
}

func (r *startRecorder) snapshot() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.pids...)
}

func newResidentExternal(t *testing.T, options ResidentPoolOptions) (ExternalPlugin, *startRecorder, *ResidentPool) {
	t.Helper()
	recorder := &startRecorder{}
	pool := NewResidentPool(options)
	t.Cleanup(pool.Close)
	external := ExternalPlugin{
		Manifest:          benchManifest(),
		Command:           benchPluginBinary(t),
		Stderr:            io.Discard,
		PermissionChecker: benchChecker{},
		ProcessObserver:   recorder,
		Resident:          pool,
	}
	return external, recorder, pool
}

func runResidentAction(t *testing.T, external ExternalPlugin, scopeID, operation string) {
	t.Helper()
	inst := pluginsdk.Instance{ID: scopeID, Name: "bench"}
	err := external.withClientForScopeOperation(context.Background(), "storage", scopeID, operation, func(client *Client) error {
		_, err := client.RunActionContext(context.Background(), inst, nil, "noop", nil)
		return err
	})
	if err != nil {
		t.Fatalf("调用插件: %v", err)
	}
}

// 常驻的本分：同一个 scope 连着调用只起一个进程；不同 scope 各起各的。
func TestResidentPoolReusesProcessPerScope(t *testing.T) {
	external, recorder, _ := newResidentExternal(t, ResidentPoolOptions{})

	runResidentAction(t, external, "inst-1", "plugin.rpc")
	runResidentAction(t, external, "inst-1", "plugin.rpc")
	if pids := recorder.snapshot(); len(pids) != 1 {
		t.Fatalf("同一 scope 的两次调用起了 %d 个进程", len(pids))
	}

	runResidentAction(t, external, "inst-2", "plugin.rpc")
	if pids := recorder.snapshot(); len(pids) != 2 {
		t.Fatalf("换 scope 后进程数 = %d，应各自独立", len(pids))
	}
}

// 安装类操作不进池：它们的环境变量和 stderr 因操作而异，复用进程会串味。
func TestResidentPoolSkipsInstallOperations(t *testing.T) {
	external, recorder, _ := newResidentExternal(t, ResidentPoolOptions{})

	runResidentAction(t, external, "inst-1", OperationInstall)
	runResidentAction(t, external, "inst-1", OperationInstall)
	if pids := recorder.snapshot(); len(pids) != 2 {
		t.Fatalf("安装操作起了 %d 个进程，应当每次一个", len(pids))
	}
}

// 进程被外部杀掉后，下一次调用要自己拉起一个新的，而不是把错误抛给用户。
func TestResidentPoolRestartsDeadProcess(t *testing.T) {
	// HealthInterval 设为 0 会取默认值，这里给一个极小值，让第二次租用先探活。
	external, recorder, _ := newResidentExternal(t, ResidentPoolOptions{HealthInterval: time.Nanosecond})

	runResidentAction(t, external, "inst-1", "plugin.rpc")
	pids := recorder.snapshot()
	if len(pids) != 1 || pids[0] <= 0 {
		t.Fatalf("首次调用的进程记录 = %v", pids)
	}
	if err := syscall.Kill(pids[0], syscall.SIGKILL); err != nil {
		t.Fatalf("杀死插件进程: %v", err)
	}
	// 等 go-plugin 的看门狗把退出状态记上，否则探活会撞在半关闭的连接上。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pids[0], 0); err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	runResidentAction(t, external, "inst-1", "plugin.rpc")
	after := recorder.snapshot()
	if len(after) != 2 || after[1] == after[0] {
		t.Fatalf("进程记录 = %v，应当换了一个新进程", after)
	}
}

// 池关掉就要收干净：插件包被替换或注销时不能留下孤儿进程。
func TestResidentPoolCloseStopsProcesses(t *testing.T) {
	external, recorder, pool := newResidentExternal(t, ResidentPoolOptions{})

	runResidentAction(t, external, "inst-1", "plugin.rpc")
	pids := recorder.snapshot()
	if len(pids) != 1 {
		t.Fatalf("进程记录 = %v", pids)
	}
	pool.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pids[0], 0); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("池关闭后插件进程仍在运行")
}
