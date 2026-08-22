package pluginrpc

import (
	"net/rpc"
	"sync"
	"sync/atomic"

	hcplugin "github.com/hashicorp/go-plugin"
)

// host-services 通道池。
//
// 插件回调宿主要走一条单独的 broker 通道。今天每次 RPC 都新建一条：宿主 NextId +
// 起 goroutine AcceptAndServe，插件那边 Dial + rpc.NewClient，调用完再整条拆掉。实测
// 这一建一拆约 43µs，和整个 RPC 本身一样贵；而且新连接上的第一次回调还要重传一遍 gob
// 类型描述符，所以首次回调比后续每次贵将近一倍。
//
// 池把通道留下来跨调用复用。线格式一个字节都没改：payload 里还是那个 broker id，只是
// 这一次的 id 可能是上一次用过的那个。正确性靠**独占**保证——一条通道同一时刻只服务
// 一次调用，所以宿主换上这次调用的 ctx 和服务集合就够了，不需要在请求里带调用编号。
//
// 复用要两边都支持，否则会挂：老插件每次调用都 Dial，而池化的通道已经被
// AcceptAndServe 消费掉了，第二次 Dial 没人 accept；反过来新插件把连接留着不关，老宿主
// 那边的通道早拆了。所以宿主先探一次 Plugin.Features，只对声明了
// PersistentHostServices 的插件用池；插件则只对 payload 里标了 HostServicesPersistent
// 的通道留连接。
type hostChannelPool struct {
	mu     sync.Mutex
	idle   map[string][]*hostChannel
	live   map[string]int
	closed bool
}

type hostChannel struct {
	id     uint32
	server *hostServicesServer
	// accepted 在插件真的连上这条通道之后置位。**没连上过的通道不能进池**：宿主这边
	// 只等 5 秒，超时之后这个 broker id 就是个死号，谁借到谁的调用卡满 5 秒再失败。
	// 这不是假想的边界——插件侧有一批方法在组装实例句柄之前就早退（「插件未实现
	// XXX」那一类），那种调用从头到尾不会 Dial。
	accepted atomic.Bool
	// finished 在这条通道停止服务之后置位：要么没等到连接，要么连接断了。
	finished atomic.Bool
	// retired 保证名额只交还一次——停服的 goroutine 和归还通道的调用方都会触发。
	retired atomic.Bool
}

// maxHostChannelsPerInstance 是一个实例最多留几条通道。通道按实例分桶（不同实例的服务
// 对象不同，混用等于把 A 的回调打到 B 的服务上），桶内条数取决于这个实例的并发调用数。
// 超出上限的调用退回「每次一条、用完就拆」的老路，不排队也不无限增长。
const maxHostChannelsPerInstance = 8

// lease 借一条通道。persistent 为 false 表示这条通道用完就拆（池满，或者调用方压根
// 没启用池），插件那边收到的 payload 也会照实标注。
func (p *hostChannelPool) lease(broker *hcplugin.MuxBroker, instanceID string, state *hostServicesState) (id uint32, persistent bool, release func()) {
	if p == nil {
		return serveHostServices(broker, state), false, func() {}
	}
	p.mu.Lock()
	if p.closed || p.live[instanceID] >= maxHostChannelsPerInstance {
		p.mu.Unlock()
		return serveHostServices(broker, state), false, func() {}
	}
	if channel := p.popIdleLocked(instanceID); channel != nil {
		p.mu.Unlock()
		channel.server.state.Store(state)
		return channel.id, true, func() { p.put(instanceID, channel) }
	}
	p.live[instanceID]++
	p.mu.Unlock()

	channel := &hostChannel{id: broker.NextId(), server: newHostServicesServer(state)}
	go p.serve(broker, instanceID, channel)
	return channel.id, true, func() { p.put(instanceID, channel) }
}

// popIdleLocked 取一条还在服务的空闲通道。已经停服的顺手扔掉，但名额留给它自己的
// retire 去还——两边都减就减重了。
func (p *hostChannelPool) popIdleLocked(instanceID string) *hostChannel {
	entries := p.idle[instanceID]
	defer func() { p.setIdleLocked(instanceID, entries) }()
	for len(entries) > 0 {
		channel := entries[len(entries)-1]
		entries = entries[:len(entries)-1]
		if !channel.finished.Load() {
			return channel
		}
	}
	return nil
}

// serve 守着一条通道，直到它不再可用。
//
// 这里不用 broker.AcceptAndServe：它把「有没有等到连接」这个信息吞了，而池子恰恰要靠
// 它决定这条通道能不能留给下一次调用。注册名 "Plugin" 与 AcceptAndServe 一致，线格式
// 没有任何变化；顺带把它那句无差别打到标准库 log 的 acceptAndServe 报错也收了回来——
// 通道没等到连接是这条路径上的常态，不是故障。
func (p *hostChannelPool) serve(broker *hcplugin.MuxBroker, instanceID string, channel *hostChannel) {
	defer func() {
		channel.finished.Store(true)
		p.retire(instanceID, channel)
	}()
	conn, err := broker.Accept(channel.id)
	if err != nil {
		return
	}
	channel.accepted.Store(true)
	server := rpc.NewServer()
	if err := server.RegisterName("Plugin", channel.server); err != nil {
		_ = conn.Close()
		return
	}
	server.ServeConn(conn)
}

// retire 把通道从池子里彻底摘掉并交还它占的名额。谁先到谁做一次。
func (p *hostChannelPool) retire(instanceID string, channel *hostChannel) {
	if channel.retired.Swap(true) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.removeIdleLocked(instanceID, channel)
	p.live[instanceID]--
	if p.live[instanceID] <= 0 {
		delete(p.live, instanceID)
	}
}

func (p *hostChannelPool) removeIdleLocked(instanceID string, channel *hostChannel) {
	entries := p.idle[instanceID]
	for i, entry := range entries {
		if entry == channel {
			p.setIdleLocked(instanceID, append(entries[:i], entries[i+1:]...))
			return
		}
	}
}

func (p *hostChannelPool) setIdleLocked(instanceID string, entries []*hostChannel) {
	if len(entries) == 0 {
		delete(p.idle, instanceID)
		return
	}
	p.idle[instanceID] = entries
}

// put 把通道还回池里。先摘掉这次调用的状态：还回去之后插件里跑飞的 goroutine 再回调，
// 拿到的是已取消的 ctx 和空服务，而不是下一次调用的东西。
//
// 插件这一次没连上来（调用在组装实例之前就返回了），或者连接已经断了——这两种通道不
// 进池：那个 broker id 之后不会再有人 accept，留在池里就是给下一次调用埋雷，而且没人
// 会把它清出去，这一路的调用会一直失败到进程重启。
func (p *hostChannelPool) put(instanceID string, channel *hostChannel) {
	channel.server.state.Store(releasedHostServices)
	if !channel.accepted.Load() || channel.finished.Load() {
		p.retire(instanceID, channel)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || channel.retired.Load() {
		return
	}
	p.idle[instanceID] = append(p.idle[instanceID], channel)
}

// close 之后 lease 一律退回不复用的老路。通道本身随插件进程结束一起消失，这里不单独
// 拆——拆的动作要么打断正在跑的回调，要么就得再做一套引用计数。
func (p *hostChannelPool) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.idle = nil
	p.live = nil
}

func newHostChannelPool() *hostChannelPool {
	return &hostChannelPool{idle: map[string][]*hostChannel{}, live: map[string]int{}}
}

// serveHostServices 是不复用的老路：开一条通道，插件用完关掉连接，AcceptAndServe 的
// goroutine 随之结束。
func serveHostServices(broker *hcplugin.MuxBroker, state *hostServicesState) uint32 {
	id := broker.NextId()
	go broker.AcceptAndServe(id, newHostServicesServer(state))
	return id
}

// pluginHostChannels 是插件这一侧的 host-services 连接缓存。
//
// 宿主复用通道时，payload 里的 broker id 会是上一次用过的那个，而那条连接插件早就
// 建好了：连接留着不关，就省掉每次调用的 Dial + rpc.NewClient，以及新连接上第一次
// 回调要重传的那份 gob 类型描述符。
//
// 只对 HostServicesPersistent 为真的 payload 这么做。老宿主不设这个字段，它那边的
// 通道每次调用都会拆掉，插件留着连接只会攒下一堆死连接。
type pluginHostChannels struct {
	mu    sync.Mutex
	conns map[uint32]*rpc.Client
}

// dial 取这次调用要用的 host-services 连接。返回的 release 对池化连接是空操作——
// 连接归缓存管，不归这一次调用管。
func (c *pluginHostChannels) dial(broker *hcplugin.MuxBroker, payload InstancePayload) (*rpc.Client, func(), error) {
	if !payload.HostServicesPersistent {
		conn, err := dialHostServices(broker, payload.HostServicesBrokerID)
		if err != nil {
			return nil, nil, err
		}
		return conn, func() { _ = conn.Close() }, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if conn, ok := c.conns[payload.HostServicesBrokerID]; ok {
		return conn, func() {}, nil
	}
	conn, err := dialHostServices(broker, payload.HostServicesBrokerID)
	if err != nil {
		return nil, nil, err
	}
	if c.conns == nil {
		c.conns = map[uint32]*rpc.Client{}
	}
	c.conns[payload.HostServicesBrokerID] = conn
	return conn, func() {}, nil
}

func dialHostServices(broker *hcplugin.MuxBroker, id uint32) (*rpc.Client, error) {
	conn, err := broker.Dial(id)
	if err != nil {
		return nil, err
	}
	return rpc.NewClient(conn), nil
}
