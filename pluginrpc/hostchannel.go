package pluginrpc

import (
	"net/rpc"
	"sync"

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
	if entries := p.idle[instanceID]; len(entries) > 0 {
		channel := entries[len(entries)-1]
		p.idle[instanceID] = entries[:len(entries)-1]
		p.mu.Unlock()
		channel.server.state.Store(state)
		return channel.id, true, func() { p.put(instanceID, channel) }
	}
	p.live[instanceID]++
	p.mu.Unlock()

	server := newHostServicesServer(state)
	channel := &hostChannel{id: broker.NextId(), server: server}
	go broker.AcceptAndServe(channel.id, server)
	return channel.id, true, func() { p.put(instanceID, channel) }
}

// put 把通道还回池里。先摘掉这次调用的状态：还回去之后插件里跑飞的 goroutine 再回调，
// 拿到的是已取消的 ctx 和空服务，而不是下一次调用的东西。
func (p *hostChannelPool) put(instanceID string, channel *hostChannel) {
	channel.server.state.Store(releasedHostServices)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		p.live[instanceID]--
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
