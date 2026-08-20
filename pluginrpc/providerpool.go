package pluginrpc

import (
	"context"
	"sync"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// Provider 池：让 Provider 字段上的缓存跨得过单次调用。
//
// 每次 RPC 都 NewXxx 现造一个 Provider，所以挂在它字段上的任何东西都只在这一次调用里
// 有效。TVDB 的 p.episodes 注释写着「留给 SeasonEpisodes 复用」，实际每季都重新分页拉
// 一遍全量集列表；它的登录 token（有效期一个月）同样缓存在 Provider 上，于是每次调用
// 都得先跑一趟 /login。Bangumi 的关联边缓存是 buildChain 的局部变量，每次 Detail 都从
// 零重建前传-续集链。这些浪费的都不是微秒，是整趟上游请求。
//
// 池化的难点不在缓存本身，而在 Provider 手里攥着的宿主句柄（Logger / KV / DB /
// secrets ……）——它们背后是**那一次调用**的 host-services 通道，而那条通道用完就还给
// 通道池、转手服务下一次调用了。这里的解法是让句柄本身不动，由 SDK 换掉它背后的连接
// （见 hostServicesClient 的门面）：Provider 构造时拿到的 inst 与 secrets 终生有效，
// 插件那边一行代码都不用写。
//
// 池子只保证租出去的实例同一时刻服务一个调用——与 host-services 通道池同构，正确性
// 同样来自独占而不是加锁。只有声明了 Plugin.ReuseProviders 的插件才进池。
const (
	// maxPooledProvidersPerKey 是同一 (种类, 实例, 配置摘要) 下的空闲上限。并发调用
	// 多于这个数时，多出来的现造现丢——池子是用来省重复工作的，不是用来堵并发的。
	maxPooledProvidersPerKey = 4
	// pooledProviderTTL 限制 Provider 能有多陈旧。配置变了摘要会变，池子自然换新；
	// 但 secret 的**值**被改而引用没变时摘要是不动的，这段超时就是那种情况下旧密钥
	// 的最长存活时间。
	pooledProviderTTL = 5 * time.Minute
	// maxPooledProviderKeys 兜住键的无界增长（实例多、配置反复改）。满了整体清空，
	// 代价只是退回没有池子的样子。
	maxPooledProviderKeys = 256
)

type providerPool struct {
	mu      sync.Mutex
	entries map[string][]pooledProvider
}

// pooledProvider 把 Provider 和它构造时拿到的那个门面绑在一起：门面终生归这个
// Provider，每次租出去只是把本次调用的连接绑上，还回来时摘掉。
type pooledProvider struct {
	provider any
	services *hostServicesClient
	pooledAt time.Time
}

func (p *providerPool) take(key string, now time.Time) (pooledProvider, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idle := p.entries[key]
	for len(idle) > 0 {
		last := idle[len(idle)-1]
		idle = idle[:len(idle)-1]
		if now.Sub(last.pooledAt) < pooledProviderTTL {
			p.setIdle(key, idle)
			return last, true
		}
		// 过期的直接丢，继续往下找——栈顶是最近归还的，它过期了下面的只会更旧。
	}
	p.setIdle(key, idle)
	return pooledProvider{}, false
}

func (p *providerPool) put(key string, entry pooledProvider, now time.Time) {
	entry.pooledAt = now
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = map[string][]pooledProvider{}
	}
	if len(p.entries) >= maxPooledProviderKeys {
		if _, known := p.entries[key]; !known {
			p.entries = map[string][]pooledProvider{}
		}
	}
	idle := p.entries[key]
	if len(idle) >= maxPooledProvidersPerKey {
		return
	}
	p.entries[key] = append(idle, entry)
}

// setIdle 在持锁状态下写回空闲列表，顺手把空键摘掉。
func (p *providerPool) setIdle(key string, idle []pooledProvider) {
	if len(idle) == 0 {
		delete(p.entries, key)
		return
	}
	p.entries[key] = idle
}

// providerPoolKey 是池子的键，空串表示这次调用不该进池。
//
// 配置摘要必须在——它是「配置变了就换 Provider」的唯一保证，老宿主不发摘要时无从判断
// 配置有没有变过。「这次调用有没有 host-services」也进键：没有通道时组装出来的实例
// 服务字段是空的（插件按 nil 判断有没有这项能力），这样的实例不能拿去服务有通道的调用。
func providerPoolKey(kind string, payload InstancePayload) string {
	if payload.ID == "" || payload.ConfigHash == "" {
		return ""
	}
	services := "0"
	if payload.HostServicesBrokerID != 0 {
		services = "1"
	}
	return kind + "\x00" + payload.ID + "\x00" + payload.ConfigHash + "\x00" + services
}

// leaseProvider 是全部 Provider 构造点的共同入口：能复用就从池子里借一个，把本次调用的
// 连接绑到它自己的门面上；否则现造。construct 的签名正是 Plugin 上那些 NewXxx 字段的
// 形状，所以调用方直接把字段传进来即可。
//
// 返回的 close 先释放通道，再把 Provider 还回池子。
func leaseProvider[T any](s *rpcServer, kind string, payload InstancePayload,
	construct func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (T, error),
) (T, func(), error) {
	var zero T
	key := ""
	if s.plugin.ReuseProviders {
		key = providerPoolKey(kind, payload)
	}
	if key != "" {
		if entry, ok := s.providers.take(key, time.Now()); ok {
			// 键里已经含种类，类型对不上只可能是插件的 NewXxx 返回了预期外的类型；
			// 那就丢掉这个实例，照常现造。
			if provider, typed := entry.provider.(T); typed {
				release, err := s.bindHostServices(payload, entry.services)
				if err != nil {
					return zero, nil, err
				}
				return provider, func() { release(); s.providers.put(key, entry, time.Now()) }, nil
			}
		}
	}

	services := &hostServicesClient{}
	inst, err := s.assembleInstance(payload, services)
	if err != nil {
		return zero, nil, err
	}
	release, err := s.bindHostServices(payload, services)
	if err != nil {
		return zero, nil, err
	}
	provider, err := construct(context.Background(), inst, services)
	if err != nil {
		release()
		return zero, nil, err
	}
	if key == "" {
		return provider, release, nil
	}
	entry := pooledProvider{provider: provider, services: services}
	return provider, func() { release(); s.providers.put(key, entry, time.Now()) }, nil
}
