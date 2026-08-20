package pluginrpc

import (
	"testing"
	"time"
)

type pooledCounter struct{ constructed int }

func newPool() *providerPool {
	return &providerPool{entries: map[string][]pooledProvider{}}
}

func poolPayload(configHash string) InstancePayload {
	return InstancePayload{ID: "inst", ConfigHash: configHash, HostServicesBrokerID: 7}
}

// lease 是 leaseProvider 里的池子部分，抽出来单独测：取不到就现造，用完还回去。
func lease(t *testing.T, pool *providerPool, key string, now time.Time, construct func() any) (any, func()) {
	t.Helper()
	if entry, ok := pool.take(key, now); ok {
		return entry.provider, func() { pool.put(key, entry, now) }
	}
	entry := pooledProvider{provider: construct(), services: &hostServicesClient{}}
	if key == "" {
		return entry.provider, func() {}
	}
	return entry.provider, func() { pool.put(key, entry, now) }
}

// 连着几次调用只该造一个 Provider——字段上的缓存能跨调用活下来，正是池化的全部目的。
func TestProviderPoolReusesAcrossCalls(t *testing.T) {
	pool := newPool()
	built := 0
	construct := func() any {
		built++
		return &pooledCounter{constructed: built}
	}

	var first any
	for i := range 3 {
		value, release := lease(t, pool, "metadata\x00inst\x00hash", time.Now(), construct)
		if i == 0 {
			first = value
		} else if value != first {
			t.Fatal("后续调用应当拿到同一个 Provider")
		}
		release()
	}
	if built != 1 {
		t.Fatalf("只该造一个 Provider，实际造了 %d 个", built)
	}
}

// 归还之前不能被第二个调用拿到：两个调用共用一个实例，就会共用它那个只指向一次调用的
// 服务门面。
func TestProviderPoolLeasesAreExclusive(t *testing.T) {
	pool := newPool()
	construct := func() any { return &pooledCounter{} }
	key := "metadata\x00inst\x00hash"

	first, releaseFirst := lease(t, pool, key, time.Now(), construct)
	second, releaseSecond := lease(t, pool, key, time.Now(), construct)
	if first == second {
		t.Fatal("未归还的 Provider 不该被第二个调用租走")
	}
	releaseFirst()
	releaseSecond()

	third, release := lease(t, pool, key, time.Now(), construct)
	defer release()
	if third != second {
		t.Fatal("归还后应当复用最近还回来的那个")
	}
}

// 配置摘要是键的一部分，配置一变就换新实例——旧配置造出来的 Provider 绝不会漏给新配置。
func TestProviderPoolKeyedByConfigDigest(t *testing.T) {
	pool := newPool()
	construct := func() any { return &pooledCounter{} }

	before, release := lease(t, pool, providerPoolKey("metadata", poolPayload("aaa")), time.Now(), construct)
	release()
	after, release := lease(t, pool, providerPoolKey("metadata", poolPayload("bbb")), time.Now(), construct)
	defer release()
	if before == after {
		t.Fatal("配置变了应当换一个新 Provider")
	}
}

// 缺摘要（老宿主）时无从判断配置有没有变过，只能退回每次现造。
func TestProviderPoolDisabledWithoutDigest(t *testing.T) {
	cases := map[string]InstancePayload{
		"缺摘要":    {ID: "inst", HostServicesBrokerID: 7},
		"缺实例 ID": {ConfigHash: "aaa", HostServicesBrokerID: 7},
	}
	for name, payload := range cases {
		if key := providerPoolKey("metadata", payload); key != "" {
			t.Fatalf("%s时不该给出池键，实际 %q", name, key)
		}
	}
}

// 有没有 host-services 通道决定了实例的服务字段是不是空的，两种实例不能混用。
func TestProviderPoolKeyedByHostServices(t *testing.T) {
	with := providerPoolKey("metadata", InstancePayload{ID: "inst", ConfigHash: "aaa", HostServicesBrokerID: 7})
	without := providerPoolKey("metadata", InstancePayload{ID: "inst", ConfigHash: "aaa"})
	if with == "" || without == "" || with == without {
		t.Fatalf("两种调用应当落在不同的键上，实际 %q / %q", with, without)
	}
}

// 陈旧的实例要丢掉：secret 的值被改而引用没变时摘要是不动的，TTL 是那种情况下旧密钥的
// 兜底上限。
func TestProviderPoolDropsStaleProviders(t *testing.T) {
	pool := newPool()
	built := 0
	construct := func() any {
		built++
		return &pooledCounter{}
	}
	key := "metadata\x00inst\x00hash"
	now := time.Now()
	_, release := lease(t, pool, key, now, construct)
	release()

	lease(t, pool, key, now.Add(pooledProviderTTL+time.Second), construct)
	if built != 2 {
		t.Fatalf("过期实例应当被丢弃并重造，实际只造了 %d 个", built)
	}
}

// 空闲上限之外的实例现造现丢，池子不该变成并发的瓶颈或内存的黑洞。
func TestProviderPoolCapsIdleInstances(t *testing.T) {
	pool := newPool()
	construct := func() any { return &pooledCounter{} }
	key := "metadata\x00inst\x00hash"

	releases := make([]func(), 0, maxPooledProvidersPerKey+2)
	for range maxPooledProvidersPerKey + 2 {
		_, release := lease(t, pool, key, time.Now(), construct)
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
	if got := len(pool.entries[key]); got != maxPooledProvidersPerKey {
		t.Fatalf("空闲实例数 = %d，应当被压在 %d", got, maxPooledProvidersPerKey)
	}
}
