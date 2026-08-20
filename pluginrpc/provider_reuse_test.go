package pluginrpc

import (
	"context"
	"encoding/json"
	"net"
	"net/rpc"
	"strings"
	"testing"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// countingMetadata 是一个只记账的元数据 Provider：记下自己被造了第几个，以及跨调用留
// 下来的缓存。它不实现任何复用相关的方法——复用是插件用 Plugin.ReuseProviders 声明的，
// Provider 本身什么都不用做。
type countingMetadata struct {
	serial int
	// seen 模拟 TVDB 的 p.episodes 那类进程内缓存：只有 Provider 活过一次调用，它才
	// 有意义。
	seen map[string]int
}

func (p *countingMetadata) Kind() string { return "counting" }

func (p *countingMetadata) Search(context.Context, string, string, int) ([]providers.MetaSearchResult, error) {
	return nil, nil
}

func (p *countingMetadata) Detail(context.Context, string, string) (providers.MetaDetail, error) {
	return providers.MetaDetail{}, nil
}

func (p *countingMetadata) SeasonEpisodes(_ context.Context, providerID string, _ int) ([]providers.MetaEpisode, error) {
	p.seen[providerID]++
	return nil, nil
}

func (p *countingMetadata) FindByExternalID(context.Context, providers.MetaExternalIDs) ([]providers.MetaSearchResult, error) {
	return nil, nil
}

func (p *countingMetadata) TestConnection(context.Context) error { return nil }

func countingServer(reuse bool, built *int) *rpcServer {
	return &rpcServer{plugin: pluginsdk.Plugin{
		Manifest:       pluginsdk.Manifest{ID: "counting"},
		ReuseProviders: reuse,
		NewMetadata: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.MetadataProvider, error) {
			*built++
			return &countingMetadata{serial: *built, seen: map[string]int{}}, nil
		},
	}}
}

func callSeasonEpisodes(t *testing.T, server *rpcServer, payload InstancePayload) {
	t.Helper()
	var reply JSONReply
	if err := server.MetadataSeasonEpisodes(MetadataSeasonEpisodesRequest{
		Instance: payload, ProviderID: "tt42", SeasonNumber: 1,
	}, &reply); err != nil {
		t.Fatal(err)
	}
}

// 走完整的 rpcServer 入口，确认池子确实接在了 Provider 构造点上：连发几次
// SeasonEpisodes 只造一个 Provider，且它的进程内缓存能累计。
func TestMetadataProviderReusedAcrossCalls(t *testing.T) {
	built := 0
	server := countingServer(true, &built)
	payload := InstancePayload{ID: "inst", ConfigHash: "hash", ConfigJSON: []byte(`{}`)}
	for range 3 {
		callSeasonEpisodes(t, server, payload)
	}

	if built != 1 {
		t.Fatalf("三次调用只该造一个 Provider，实际造了 %d 个", built)
	}
	pooled, ok := server.providers.take(providerPoolKey("metadata", payload), time.Now())
	if !ok {
		t.Fatal("Provider 应当留在池子里")
	}
	if seen := pooled.provider.(*countingMetadata).seen["tt42"]; seen != 3 {
		t.Fatalf("进程内缓存应当跨调用累计到 3，实际 %d", seen)
	}
}

// 没声明 ReuseProviders 的插件行为必须一个字不变：每次现造，永不入池。
func TestMetadataProviderRebuiltWithoutDeclaration(t *testing.T) {
	built := 0
	server := countingServer(false, &built)
	payload := InstancePayload{ID: "inst", ConfigHash: "hash", ConfigJSON: []byte(`{}`)}
	for range 3 {
		callSeasonEpisodes(t, server, payload)
	}
	if built != 3 {
		t.Fatalf("未声明复用时应当每次现造，实际造了 %d 个", built)
	}
	if len(server.providers.entries) != 0 {
		t.Fatal("未声明复用的 Provider 不该进池")
	}
}

// 配置改了就必须换 Provider——旧配置（含旧密钥引用）造出来的实例不能漏给新配置。
func TestMetadataProviderRebuiltWhenConfigChanges(t *testing.T) {
	built := 0
	server := countingServer(true, &built)
	configs := map[string][]byte{"before": []byte(`{"a":1}`), "after": []byte(`{"a":2}`)}

	for _, hash := range []string{"before", "before", "after"} {
		callSeasonEpisodes(t, server, InstancePayload{ID: "inst", ConfigHash: hash, ConfigJSON: configs[hash]})
	}
	if built != 2 {
		t.Fatalf("配置变更应当只多造一个 Provider，实际造了 %d 个", built)
	}
}

// 门面是复用的关键：Provider 构造时拿到的那个句柄一直有效，SDK 只换它背后的连接。
// 归还之后句柄立刻失效——插件里跑飞的 goroutine 拿到的是错误，而不是下一次调用的连接。
func TestHostServicesFacadeFollowsCurrentCall(t *testing.T) {
	first := hostServicesConnFor(t, "first")
	second := hostServicesConnFor(t, "second")

	services := &hostServicesClient{}
	whoAmI := func() string {
		var value string
		found, err := services.Get(t.Context(), "who", &value)
		if err != nil {
			return "错误：" + err.Error()
		}
		if !found {
			return ""
		}
		return value
	}

	services.bind(first)
	if got := whoAmI(); got != "first" {
		t.Fatalf("绑到第一条连接时 KV = %q", got)
	}

	services.detach()
	if got := whoAmI(); !strings.Contains(got, errHostServicesDetached.Error()) {
		t.Fatalf("摘掉连接后应当拿到明确的失效错误，实际 %q", got)
	}

	services.bind(second)
	if got := whoAmI(); got != "second" {
		t.Fatalf("换到第二条连接后 KV = %q", got)
	}
}

// hostServicesConnFor 起一条真的 host-services 连接，KV 里只有 who = name。
func hostServicesConnFor(t *testing.T, name string) *rpc.Client {
	t.Helper()
	server := rpc.NewServer()
	target := newHostServicesServer(&hostServicesState{
		ctx: t.Context(), kv: namedKV(name),
		permissions: pluginsdk.Permissions{Data: []string{"storage"}},
	})
	if err := server.RegisterName("Plugin", target); err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	t.Cleanup(func() {
		client.Close()
		serverConn.Close()
	})
	return client
}

// namedKV 是一个只认 "who" 一个键的 KVStore，用来分辨句柄这一刻接在哪条连接上。
type namedKV string

func (kv namedKV) Get(_ context.Context, key string, out any) (bool, error) {
	if key != "who" {
		return false, nil
	}
	encoded, err := json.Marshal(string(kv))
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(encoded, out)
}

func (namedKV) Set(context.Context, string, any, time.Duration) error { return nil }
func (namedKV) Delete(context.Context, string) error                  { return nil }
func (namedKV) DeletePrefix(context.Context, string) error            { return nil }

// 没有 host-services 通道时，SecretResolver 必须是真正的 nil 接口——插件里
// `if secrets == nil` 的守卫全靠这一点，typed-nil 会让守卫静默失效。
func TestInstanceSecretsNilWithoutHostServices(t *testing.T) {
	server := &rpcServer{plugin: pluginsdk.Plugin{Manifest: pluginsdk.Manifest{ID: "probe"}}}
	payload := InstancePayload{ID: "inst", ConfigHash: "hash", ConfigJSON: []byte(`{}`)}

	_, secrets, release, err := server.instance(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if secrets != nil {
		t.Fatalf("无 host-services 通道时 SecretResolver 应当为 nil 接口，实际 %#v", secrets)
	}
}
