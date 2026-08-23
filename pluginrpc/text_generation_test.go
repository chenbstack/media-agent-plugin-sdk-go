package pluginrpc

import (
	"context"
	"strings"
	"testing"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
)

type stubTextGeneration struct{}

func (stubTextGeneration) ListTextModels(context.Context) ([]pluginsdk.TextModel, error) {
	return []pluginsdk.TextModel{
		{ID: "local-qwen", Name: "Qwen 本机", DataEgress: "local", Default: true},
		{ID: "remote-gpt", Name: "GPT 远程", DataEgress: "remote"},
	}, nil
}

func (stubTextGeneration) GenerateText(_ context.Context, req pluginsdk.TextGenerationRequest) (pluginsdk.TextGenerationResult, error) {
	modelID := req.ModelID
	if modelID == "" {
		modelID = "local-qwen"
	}
	return pluginsdk.TextGenerationResult{
		Output:  "译文：" + req.Prompt,
		ModelID: modelID,
	}, nil
}

func TestTextGenerationServiceRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), textGeneration: stubTextGeneration{}})
	var reply JSONReply
	if err := server.ListTextModels(Empty{}, &reply); err == nil {
		t.Fatal("未声明 model.generate 的插件不应列出模型")
	}
	if err := server.GenerateText(TextGenerationRequest{Prompt: "hi"}, &reply); err == nil {
		t.Fatal("未声明 model.generate 的插件不应调用模型")
	}

	server.live().permissions.Host = []string{"model.generate"}
	if err := server.ListTextModels(Empty{}, &reply); err != nil {
		t.Fatalf("ListTextModels: %v", err)
	}
	var models []pluginsdk.TextModel
	if err := decodeJSON(reply.Data, &models); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(models) != 2 || !models[0].Default {
		t.Fatalf("models = %+v", models)
	}
	// 数据去向必须过得来：插件送进模型的是用户的媒体内容，本机还是远程得能讲清楚。
	if models[0].DataEgress != "local" || models[1].DataEgress != "remote" {
		t.Fatalf("data egress 丢失: %+v", models)
	}
}

// 模型 id 是用户在插件设置里选的，必须原样传到宿主；被适配器吞掉的表现是用户
// 选了哪个模型都跑默认那个，界面上完全看不出来。
func TestGenerateTextPassesModelAndPromptThrough(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{
		ctx:            context.Background(),
		textGeneration: stubTextGeneration{},
		permissions:    pluginsdk.Permissions{Host: []string{"model.generate"}},
	})
	var reply JSONReply
	if err := server.GenerateText(TextGenerationRequest{ModelID: "remote-gpt", Prompt: "你好"}, &reply); err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	var result pluginsdk.TextGenerationResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.ModelID != "remote-gpt" {
		t.Fatalf("模型 id 没传下去: %+v", result)
	}
	if !strings.Contains(result.Output, "你好") {
		t.Fatalf("prompt 没传下去: %+v", result)
	}
}

func TestTextGenerationRejectsMissingHostService(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{
		ctx:         context.Background(),
		permissions: pluginsdk.Permissions{Host: []string{"model.generate"}},
	})
	var reply JSONReply
	if err := server.ListTextModels(Empty{}, &reply); err == nil {
		t.Fatal("宿主未提供 TextGeneration 时应报错")
	}
	if err := server.GenerateText(TextGenerationRequest{Prompt: "hi"}, &reply); err == nil {
		t.Fatal("宿主未提供 TextGeneration 时应报错")
	}
}

// needsHostServices 漏掉一项的表现是插件侧字段为 nil——功能没了，日志里什么都没有。
func TestNeedsHostServicesCoversTextGeneration(t *testing.T) {
	if !needsHostServices(pluginsdk.Instance{TextGeneration: stubTextGeneration{}}, nil) {
		t.Fatal("只提供 TextGeneration 时未开回调通道")
	}
}

// assembleInstance 是对称的陷阱：宿主开了通道，插件进程里字段却是 nil。
func TestAssembleInstanceAttachesTextGeneration(t *testing.T) {
	server := &rpcServer{}
	inst, err := server.assembleInstance(InstancePayload{
		ID:                   "translator",
		ConfigJSON:           []byte("{}"),
		HostServicesBrokerID: 1,
	}, &hostServicesClient{})
	if err != nil {
		t.Fatalf("assembleInstance: %v", err)
	}
	if inst.TextGeneration == nil {
		t.Fatal("assembleInstance 没挂上 TextGeneration")
	}
}
