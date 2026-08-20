package pluginrpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
)

type recordingSidecars struct {
	got pluginsdk.SubtitleWrite
}

func (r *recordingSidecars) WriteSubtitle(_ context.Context, input pluginsdk.SubtitleWrite) (pluginsdk.SubtitleWriteResult, error) {
	r.got = input
	return pluginsdk.SubtitleWriteResult{Path: "/media/The Bear/S01E01.zh.srt", Change: "created"}, nil
}

func TestWriteSubtitleRequiresHostPermission(t *testing.T) {
	sidecars := &recordingSidecars{}
	server := newHostServicesServer(&hostServicesState{
		ctx:      context.Background(),
		sidecars: sidecars,
	})
	input := pluginsdk.SubtitleWrite{FileRef: "ref-1", Content: []byte("1\n"), Language: "zh", Ext: "srt"}

	var reply JSONReply
	if err := server.WriteSubtitle(SubtitleWriteRequest{Input: input}, &reply); err == nil {
		t.Fatal("没有 media.sidecar.write 权限时不应放行落盘")
	}

	server.live().permissions.Host = []string{"media.sidecar.write"}
	if err := server.WriteSubtitle(SubtitleWriteRequest{Input: input}, &reply); err != nil {
		t.Fatalf("声明权限后应放行: %v", err)
	}
	if sidecars.got.FileRef != "ref-1" || string(sidecars.got.Content) != "1\n" {
		t.Fatalf("输入未原样转交宿主: %+v", sidecars.got)
	}
	var result pluginsdk.SubtitleWriteResult
	if err := json.Unmarshal(reply.Data, &result); err != nil {
		t.Fatalf("解析回包: %v", err)
	}
	if result.Change != "created" || result.Path == "" {
		t.Fatalf("宿主返回的落盘结果未透传: %+v", result)
	}
}

func TestWriteSubtitleFailsWithoutHostCapability(t *testing.T) {
	// 宿主没注入这个能力时要明确报错，而不是静默当成写成功了——
	// 插件据此决定要不要换个来源再试。
	server := newHostServicesServer(&hostServicesState{ctx: context.Background()})
	server.live().permissions.Host = []string{"media.sidecar.write"}

	var reply JSONReply
	if err := server.WriteSubtitle(SubtitleWriteRequest{Input: pluginsdk.SubtitleWrite{FileRef: "ref-1"}}, &reply); err == nil {
		t.Fatal("宿主未提供 MediaSidecars 时应报错")
	}
}
