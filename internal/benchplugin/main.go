// Command benchplugin 是 pluginrpc 基准测试用的最小插件二进制。
//
// 它只实现一个 ActionHandler，按 action id 决定回调宿主多少次，从而把一次 RPC 的
// 成本拆成「传输 / host-services 通道建立 / 每次回调」三块。业务逻辑压到零，测出来
// 的就是通信本身。
package main

import (
	"context"
	"strconv"
	"strings"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/pluginrpc"
)

// ActionNoop 不回调宿主；ActionKVPrefix+N 回调宿主 KVGet N 次。
const (
	ActionNoop     = "noop"
	ActionKVPrefix = "kv:"
)

type handler struct {
	inst pluginsdk.Instance
}

func (h *handler) RunAction(ctx context.Context, actionID string, input map[string]any) (pluginsdk.ActionResult, error) {
	if n, ok := strings.CutPrefix(actionID, ActionKVPrefix); ok {
		count, err := strconv.Atoi(n)
		if err != nil {
			return pluginsdk.ActionResult{}, err
		}
		for i := 0; i < count; i++ {
			var out any
			if _, err := h.inst.KV.Get(ctx, "bench-key", &out); err != nil {
				return pluginsdk.ActionResult{}, err
			}
		}
	}
	return pluginsdk.ActionResult{Message: "ok"}, nil
}

func main() {
	pluginrpc.Serve(pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{
			ID: "benchplugin", Name: "benchplugin", Version: "0.0.1", Type: "cli",
			Capabilities: []string{"actions"},
			Permissions:  pluginsdk.Permissions{Data: []string{"storage"}},
		},
		NewActionHandler: func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.ActionHandler, error) {
			return &handler{inst: inst}, nil
		},
	})
}
