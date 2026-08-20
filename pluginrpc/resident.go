package pluginrpc

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 常驻进程池。
//
// 第三方 cli 插件今天是「每次操作起一个进程」：fork + 握手 + 一次调用 + Kill，实测
// 约 7.0ms，而同一次调用打在常驻进程上只要 130µs 上下——差了近 180 倍。目录扫描这类
// 一次操作要发几十上百个 RPC 的场景，绝大部分时间花在反复起进程上。
//
// 池只管「进程活多久」，不管「谁配得上常驻」：宿主给每个插件都建池，用 IdleTimeout
// 区分——声明了常驻的按 manifest 长期存活，没声明的只留很短一段，够一次操作里的连发
// 复用就行。
//
// 治理刻意做得比官方 Pack 那套轻：池里的进程是**按需拉起**的，死了下一次调用自己会
// 起一个新的，所以不需要重启退避、状态持久化和崩溃回滚。剩下要管的只有三件事：
//
//	活性  交出进程前先看它还在不在，久未使用的先 Ping 一次
//	空闲  没人用且闲置超时的进程要退出，别白占内存
//	内存  超过 manifest 声明的上限就换新进程；正在被调用则等调用结束再换
type ResidentPool struct {
	options ResidentPoolOptions

	mu      sync.Mutex
	entries map[string]*residentEntry
	closed  bool

	stopOnce sync.Once
	stop     chan struct{}
}

type ResidentPoolOptions struct {
	// IdleTimeout 是进程在无人使用后还能活多久。0 用默认值。
	IdleTimeout time.Duration
	// HealthInterval 是「闲置超过这么久，再用之前先 Ping 一次」的阈值。0 用默认值。
	HealthInterval time.Duration
	// HealthTimeout 是那次 Ping 的超时。0 用默认值。
	HealthTimeout time.Duration
	// MemoryLimitBytes 是常驻进程的内存上限，超过就换新进程；0 表示不限。
	MemoryLimitBytes uint64
	// MemoryLimitSamples 是连续超限多少次才动手，用来滤掉瞬时尖峰。0 用默认值。
	MemoryLimitSamples int
	// MemoryBytes 采样进程 RSS，由宿主提供（宿主已有一套采样实现）。nil 表示不做
	// 内存治理。
	MemoryBytes func(pid int) (uint64, error)
}

const (
	defaultResidentIdleTimeout    = 5 * time.Minute
	defaultResidentHealthInterval = 30 * time.Second
	defaultResidentHealthTimeout  = 3 * time.Second
	defaultResidentMemorySamples  = 3
	// residentOperation 是常驻进程在进程观察器里的操作名。进程跨调用存活，记成
	// 起它的那一次操作会让进程列表读起来是错的。
	residentOperation = "plugin.resident"
)

var errResidentPoolClosed = errors.New("插件常驻进程池已关闭")

// NewResidentPool 建一个常驻池。宿主为每个声明了常驻的插件包建一个，包被替换或注销
// 时 Close。
func NewResidentPool(options ResidentPoolOptions) *ResidentPool {
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = defaultResidentIdleTimeout
	}
	if options.HealthInterval <= 0 {
		options.HealthInterval = defaultResidentHealthInterval
	}
	if options.HealthTimeout <= 0 {
		options.HealthTimeout = defaultResidentHealthTimeout
	}
	if options.MemoryLimitSamples <= 0 {
		options.MemoryLimitSamples = defaultResidentMemorySamples
	}
	pool := &ResidentPool{
		options: options,
		entries: map[string]*residentEntry{},
		stop:    make(chan struct{}),
	}
	go pool.sweep()
	return pool
}

// Close 停止巡检并结束池里的进程。还在被调用的进程标记为退休，等最后一次调用结束后
// 自行退出——硬杀会打断正在跑的操作。
func (p *ResidentPool) Close() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() { close(p.stop) })
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.drain()
}

// Restart 清空池里的进程，但池本身还能继续用。插件包被换成新版本时调用：磁盘上的
// 二进制已经变了，手上这些进程跑的还是旧代码。语义与 Close 相同（在用的等用完再退），
// 区别只在于池不关，下一次调用会拉起新二进制的进程。
func (p *ResidentPool) Restart() {
	if p == nil {
		return
	}
	p.drain()
}

// drain 把池里的条目全摘下来退休。没人用的立刻结束，还有人用的等最后一次调用还回来
// 再退出——摘下来之后新的调用会另起进程，所以不必打断正在跑的操作。
func (p *ResidentPool) drain() {
	p.mu.Lock()
	entries := make([]*residentEntry, 0, len(p.entries))
	for key, entry := range p.entries {
		entries = append(entries, entry)
		delete(p.entries, key)
	}
	p.mu.Unlock()
	for _, entry := range entries {
		entry.retire()
	}
}

// acquire 借一个常驻进程。ok 为 false 表示这次调用不该走常驻（没配池、操作不合适、
// 池已关闭），调用方照旧起一次性进程。
func (p *ResidentPool) acquire(ctx context.Context, cfg ClientConfig) (client *Client, release func(), ok bool, err error) {
	if p == nil || !residentEligible(cfg) {
		return nil, nil, false, nil
	}
	entry, err := p.entryFor(cfg)
	if err != nil {
		if errors.Is(err, errResidentPoolClosed) {
			return nil, nil, false, nil
		}
		return nil, nil, true, err
	}
	client, err = entry.lease(ctx, p.options)
	if err != nil {
		return nil, nil, true, err
	}
	return client, entry.release, true, nil
}

// residentEligible 判断一次调用能不能落到常驻进程上。
//
// 安装与卸载排除在外：宿主会按操作给它们不同的环境变量和 stderr（安装进度要实时回显），
// 而这两样在 exec 时就定死了，复用进程会把别的操作的环境带进来。它们本身低频且耗时以
// 秒计，省这几毫秒没有意义。带了额外环境变量的操作同理。
func residentEligible(cfg ClientConfig) bool {
	switch cfg.Operation {
	case OperationInstall, OperationUninstall:
		return false
	}
	return len(cfg.Env) == 0
}

func (p *ResidentPool) entryFor(cfg ClientConfig) (*residentEntry, error) {
	key := cfg.ScopeType + "\x00" + cfg.ScopeID
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errResidentPoolClosed
	}
	if entry, ok := p.entries[key]; ok {
		return entry, nil
	}
	entry := &residentEntry{config: cfg}
	entry.config.Operation = residentOperation
	p.entries[key] = entry
	return entry, nil
}

// sweep 定期回收闲置进程并执行内存上限。频率取空闲超时的一半，够及时也不至于空转。
func (p *ResidentPool) sweep() {
	interval := p.options.IdleTimeout / 2
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.sweepOnce()
		}
	}
}

func (p *ResidentPool) sweepOnce() {
	p.mu.Lock()
	entries := make(map[string]*residentEntry, len(p.entries))
	for key, entry := range p.entries {
		entries[key] = entry
	}
	p.mu.Unlock()

	idle := make([]string, 0, len(entries))
	for key, entry := range entries {
		if entry.sweep(p.options) {
			idle = append(idle, key)
		}
	}
	if len(idle) == 0 {
		return
	}
	p.mu.Lock()
	for _, key := range idle {
		// 巡检和 acquire 之间池里可能已经换了别的条目，只摘同一个。
		if entry, ok := p.entries[key]; ok && entry == entries[key] {
			delete(p.entries, key)
		}
	}
	p.mu.Unlock()
}

// residentEntry 是某个 scope 上的常驻进程。mu 同时串行化这个 scope 的进程启动，
// 所以池的锁不会被一次 20 秒的握手堵住。
type residentEntry struct {
	config ClientConfig

	mu         sync.Mutex
	running    *runningClient
	leases     int
	lastUsed   time.Time
	overMemory int
	// retired 表示这个进程已经被判死刑（池关闭、内存超限），但还有调用在用它。
	// 最后一个租约还回来时它就退出。条目本身此时已从池里摘掉，新的调用会另起一个。
	retired bool
}

func (e *residentEntry) lease(ctx context.Context, options ResidentPoolOptions) (*Client, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running != nil && !e.healthyLocked(options) {
		e.stopLocked()
	}
	if e.running == nil {
		// 进程的生死由池管，不能被某一次调用的 ctx 带走；ctx 只用来限制这次握手。
		config := e.config
		config.Detached = true
		running, err := startClient(ctx, config)
		if err != nil {
			return nil, err
		}
		e.running = running
		e.overMemory = 0
	}
	e.leases++
	e.lastUsed = time.Now()
	return e.running.client, nil
}

func (e *residentEntry) release() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.leases > 0 {
		e.leases--
	}
	e.lastUsed = time.Now()
	if e.leases == 0 && e.retired {
		e.stopLocked()
	}
}

// healthyLocked 判断手上这个进程还能不能用。闲置久了先 Ping 一次：进程活着但 RPC 卡死
// 时，只看 Exited 是看不出来的。
func (e *residentEntry) healthyLocked(options ResidentPoolOptions) bool {
	if e.running == nil || e.retired || e.running.exited() {
		return false
	}
	if e.leases > 0 || time.Since(e.lastUsed) < options.HealthInterval {
		return true
	}
	return e.running.ping(options.HealthTimeout) == nil
}

// sweep 是巡检的一次动作：回收闲置进程、执行内存上限。返回 true 表示这个条目该从池里
// 摘掉——要么已经空了，要么进程正在退休（还在跑的调用用完自会收尾）。
func (e *residentEntry) sweep(options ResidentPoolOptions) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running == nil {
		return e.leases == 0
	}
	if e.running.exited() {
		e.stopLocked()
		return e.leases == 0
	}
	if e.leases == 0 && time.Since(e.lastUsed) >= options.IdleTimeout {
		e.stopLocked()
		return true
	}
	if e.overMemoryLocked(options) {
		e.retireLocked()
		return true
	}
	return false
}

// overMemoryLocked 连续多次采到超限才算数，避免一次尖峰就换进程。
func (e *residentEntry) overMemoryLocked(options ResidentPoolOptions) bool {
	if options.MemoryBytes == nil || options.MemoryLimitBytes == 0 || e.running == nil {
		return false
	}
	pid := e.running.pid()
	if pid <= 0 {
		return false
	}
	used, err := options.MemoryBytes(pid)
	if err != nil {
		return false
	}
	if used <= options.MemoryLimitBytes {
		e.overMemory = 0
		return false
	}
	e.overMemory++
	return e.overMemory >= options.MemoryLimitSamples
}

// retire 用于池关闭：没人用就立刻结束，还有人用就等他们用完——Close 不该打断正在跑
// 的操作。
func (e *residentEntry) retire() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retireLocked()
}

func (e *residentEntry) retireLocked() {
	if e.leases == 0 {
		e.stopLocked()
		return
	}
	e.retired = true
}

func (e *residentEntry) stopLocked() {
	if e.running == nil {
		e.retired = false
		return
	}
	e.running.Close()
	e.running = nil
	e.overMemory = 0
	e.retired = false
}
