package pluginrpc

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// 配置摘要：同一份配置不重复发。
//
// 每次 RPC 都把实例的整份 Config 重新 json.Marshal 再随包发给插件，而配置在两次调用
// 之间几乎从不变化。实测 8 字段的配置涨到 200 字段（约 14KB）时，单次调用从 47µs 涨到
// 173µs——这 126µs 里绝大部分是在重复搬运同一份字节。
//
// 做法是给 InstancePayload 加一个 ConfigHash：宿主仍照常 marshal 并算摘要，与上次发给
// **这个进程**的比对，一样就把 ConfigJSON 留空只发摘要；插件按 (实例, 摘要) 取出上次收到
// 的字节。两侧缓存都有上限，插件那边未命中时会明确报错，宿主收到后带完整配置重试一次，
// 所以缓存丢失只是白跑一趟，不会让插件拿到错的配置。
//
// 只有握手时声明了 ConfigDigest 的插件才会走这条路（见 Client.features）：老插件收到空的
// ConfigJSON 会当成空配置，那是静默的错误配置，比不优化糟得多。

// maxCachedConfigs 是两侧缓存的实例数上限。一个 Pack 进程可能服务几十个实例，取 512
// 足够宽松；满了之后随机淘汰一条（Go 的 map 遍历顺序本身随机），淘汰掉的下次多发一份
// 完整配置而已。
const maxCachedConfigs = 512

// configHash 是配置字节的摘要。截到 16 字节：碰撞意味着插件会用上一份配置，必须小到
// 可以忽略，而 128 位已经远超这个要求。
func configHash(configJSON []byte) string {
	sum := sha256.Sum256(configJSON)
	return hex.EncodeToString(sum[:16])
}

// hostConfigCache 记住每个实例最近一次发给插件的配置。它属于单个 Client，也就是单个
// 插件进程——进程换了，Client 也换了，缓存自然跟着重建。
type hostConfigCache struct {
	mu      sync.Mutex
	entries map[string]hostConfigEntry
}

type hostConfigEntry struct {
	hash       string
	configJSON []byte
}

// prepare 返回本次该发的 ConfigJSON 与摘要。配置与上次相同时 ConfigJSON 为 nil。
func (c *hostConfigCache) prepare(instanceID string, configJSON []byte) ([]byte, string) {
	hash := configHash(configJSON)
	if instanceID == "" {
		// 没有实例 ID 就没有缓存的键，照常整份发。
		return configJSON, ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[instanceID]; ok {
		if entry.hash == hash {
			return nil, hash
		}
	} else {
		c.evictLocked()
	}
	if c.entries == nil {
		c.entries = make(map[string]hostConfigEntry)
	}
	c.entries[instanceID] = hostConfigEntry{hash: hash, configJSON: configJSON}
	return configJSON, hash
}

// forget 丢掉某实例的记录并交出缓存的配置字节，供插件报未命中后重试用。
func (c *hostConfigCache) forget(instanceID string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[instanceID]
	if !ok {
		return nil
	}
	delete(c.entries, instanceID)
	return entry.configJSON
}

func (c *hostConfigCache) evictLocked() {
	for len(c.entries) >= maxCachedConfigs {
		for key := range c.entries {
			delete(c.entries, key)
			break
		}
	}
}

// pluginConfigCache 是插件侧的另一半：按实例记住上次收到的配置字节。
//
// 存字节而不是解析好的 map，是为了让每次调用仍然拿到一份互不相干的配置——插件若原地
// 改了 Config，共享的 map 会把改动带到下一次调用甚至并发的另一次调用上。省下的传输仍然
// 在，只是不省这一次 Unmarshal。
type pluginConfigCache struct {
	mu      sync.Mutex
	entries map[string]pluginConfigEntry
}

type pluginConfigEntry struct {
	hash       string
	configJSON []byte
}

// resolve 给出本次该用的配置字节。宿主只发了摘要而本地没有对应记录时返回 false。
func (c *pluginConfigCache) resolve(payload InstancePayload) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(payload.ConfigJSON) > 0 || payload.ConfigHash == "" {
		if payload.ID != "" && payload.ConfigHash != "" {
			if _, ok := c.entries[payload.ID]; !ok {
				c.evictLocked()
			}
			if c.entries == nil {
				c.entries = make(map[string]pluginConfigEntry)
			}
			c.entries[payload.ID] = pluginConfigEntry{hash: payload.ConfigHash, configJSON: payload.ConfigJSON}
		}
		return payload.ConfigJSON, true
	}
	entry, ok := c.entries[payload.ID]
	if !ok || entry.hash != payload.ConfigHash {
		return nil, false
	}
	return entry.configJSON, true
}

func (c *pluginConfigCache) evictLocked() {
	for len(c.entries) >= maxCachedConfigs {
		for key := range c.entries {
			delete(c.entries, key)
			break
		}
	}
}
