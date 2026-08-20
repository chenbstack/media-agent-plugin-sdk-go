package pluginrpc

import (
	"context"
	"net"
	"net/rpc"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// configEchoHandler 把配置里的 token 原样回给宿主，好断言插件确实拿到了配置。
type configEchoHandler struct {
	config map[string]any
}

func (h configEchoHandler) RunAction(context.Context, string, map[string]any) (pluginsdk.ActionResult, error) {
	token, _ := h.config["token"].(string)
	return pluginsdk.ActionResult{Message: token}, nil
}

func newConfigDigestPair(t *testing.T) (*Client, *rpcServer) {
	t.Helper()
	impl := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "digest", Name: "Digest"},
		NewActionHandler: func(_ context.Context, inst pluginsdk.Instance, _ pluginsdk.SecretResolver) (pluginsdk.ActionHandler, error) {
			return configEchoHandler{config: inst.Config}, nil
		},
	}
	pluginSide := &rpcServer{plugin: impl}
	server := rpc.NewServer()
	if err := server.RegisterName("Plugin", pluginSide); err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	go server.ServeConn(serverConn)

	client := &Client{client: rpc.NewClient(clientConn), manifest: impl.Manifest}
	t.Cleanup(func() { _ = client.client.Close() })
	return client, pluginSide
}

// 配置摘要的整条闭环：第一次整份发，之后只发摘要，插件那边的缓存丢了宿主自动补发。
func TestClientReusesConfigByDigest(t *testing.T) {
	client, pluginSide := newConfigDigestPair(t)
	ctx := context.Background()
	inst := pluginsdk.Instance{ID: "inst-1", Config: map[string]any{"token": "abc"}}

	run := func() string {
		t.Helper()
		result, err := client.RunActionContext(ctx, inst, nil, "echo", nil)
		if err != nil {
			t.Fatalf("RunAction: %v", err)
		}
		return result.Message
	}

	if got := run(); got != "abc" {
		t.Fatalf("首次调用 = %q", got)
	}
	if !client.featureSet(ctx).ConfigDigest {
		t.Fatal("新版插件应当声明 ConfigDigest")
	}
	if !client.hasCachedConfig("inst-1") {
		t.Fatal("首次调用后宿主应记下这份配置")
	}

	// 第二次配置没变，走的是只发摘要的路径。
	if got := run(); got != "abc" {
		t.Fatalf("第二次调用 = %q", got)
	}

	// 插件淘汰了缓存：宿主必须察觉并带完整配置重试，而不是让插件拿到空配置。
	pluginSide.configs.mu.Lock()
	pluginSide.configs.entries = nil
	pluginSide.configs.mu.Unlock()
	if got := run(); got != "abc" {
		t.Fatalf("插件缓存丢失后 = %q，宿主没有补发完整配置", got)
	}
	if client.hasCachedConfig("inst-1") {
		t.Fatal("补发之后宿主应先丢掉记录，让下一次重新对齐")
	}

	// 配置变了要整份重发，插件不能还用旧的。
	inst.Config = map[string]any{"token": "xyz"}
	if got := run(); got != "xyz" {
		t.Fatalf("改配置后 = %q", got)
	}
}

// 老插件没有 Plugin.Features：宿主必须判定为旧版并整份发配置，否则插件会把空的
// ConfigJSON 当成空配置，静默跑错。
func TestClientTreatsPluginWithoutFeaturesAsLegacy(t *testing.T) {
	server := rpc.NewServer()
	if err := server.RegisterName("Plugin", &immediateRPCServer{}); err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go server.ServeConn(serverConn)

	client := &Client{client: rpc.NewClient(clientConn)}
	defer client.client.Close()

	if client.featureSet(context.Background()).ConfigDigest {
		t.Fatal("老插件不该被判定为支持配置摘要")
	}
	payload, release, err := client.instancePayload(context.Background(), pluginsdk.Instance{ID: "inst-1", Config: map[string]any{"token": "abc"}}, nil)
	if err != nil {
		t.Fatalf("instancePayload: %v", err)
	}
	defer release()
	if len(payload.ConfigJSON) == 0 || payload.ConfigHash != "" {
		t.Fatalf("payload = %+v，老插件必须收到完整配置", payload)
	}
}

// hasCachedConfig 只给测试用：宿主是否记得某个实例的配置。
func (c *Client) hasCachedConfig(instanceID string) bool {
	c.configs.mu.Lock()
	defer c.configs.mu.Unlock()
	_, ok := c.configs.entries[instanceID]
	return ok
}
