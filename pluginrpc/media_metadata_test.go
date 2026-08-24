package pluginrpc

import (
	"context"
	"strings"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// hostServicesClient 靠鸭子类型满足所有 host service 接口，没有编译期断言时，
// 方法名撞车（Detail 这种通名尤其容易）只会在 assembleInstance 赋值那一行报错，
// 错误信息还指不到真正的原因。这一行把它钉死在本文件里。
var _ pluginsdk.MediaMetadata = (*hostServicesClient)(nil)

type stubMediaMetadata struct{ languages []string }

func (s *stubMediaMetadata) Detail(_ context.Context, mediaID, language string) (pluginsdk.MediaMetadataDetail, error) {
	s.languages = append(s.languages, language)
	if language == "en-US" {
		return pluginsdk.MediaMetadataDetail{
			MediaType: "movie", Title: "Sintel", Language: "en-US",
			Cast: []pluginsdk.MediaMetadataCastMember{{Name: "Thom Hoffman", Character: "Scales"}},
		}, nil
	}
	return pluginsdk.MediaMetadataDetail{
		MediaType: "movie", Title: "辛特尔", Language: "zh-CN",
		Cast: []pluginsdk.MediaMetadataCastMember{{Name: "Thom Hoffman", Character: "鳞鳞"}},
	}, nil
}

func TestMediaMetadataRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), mediaMetadata: &stubMediaMetadata{}})
	var reply JSONReply
	if err := server.MediaMetadataDetail(MediaMetadataDetailRequest{MediaID: "m1"}, &reply); err == nil {
		t.Fatal("未声明 media.metadata.read 的插件不应读到元数据")
	}

	server.live().permissions.Host = []string{"media.metadata.read"}
	if err := server.MediaMetadataDetail(MediaMetadataDetailRequest{MediaID: "m1", Language: "en-US"}, &reply); err != nil {
		t.Fatalf("MediaMetadataDetail: %v", err)
	}
	var detail pluginsdk.MediaMetadataDetail
	if err := decodeJSON(reply.Data, &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 语言要原样传到宿主，而不是在中途被丢掉——丢了插件拿到的是库里那一份，
	// 和它已有的那侧重复，对照表凑不出来却又不报错。
	if detail.Title != "Sintel" || detail.Language != "en-US" {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.Cast) != 1 || detail.Cast[0].Character != "Scales" {
		t.Fatalf("cast = %+v", detail.Cast)
	}
}

func TestMediaMetadataRejectsMissingHostService(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background()})
	server.live().permissions.Host = []string{"media.metadata.read"}
	var reply JSONReply
	err := server.MediaMetadataDetail(MediaMetadataDetailRequest{MediaID: "m1"}, &reply)
	if err == nil || !strings.Contains(err.Error(), "MediaMetadata") {
		t.Fatalf("err = %v，宿主没提供实现时应当说清是哪个服务", err)
	}
}

// needsHostServices 漏掉一项的表现是插件侧字段为 nil——功能没了，日志里什么都没有。
func TestNeedsHostServicesCoversMediaMetadata(t *testing.T) {
	if !needsHostServices(pluginsdk.Instance{MediaMetadata: &stubMediaMetadata{}}, nil) {
		t.Fatal("只提供 MediaMetadata 时未开回调通道")
	}
}

// assembleInstance 是对称的陷阱：宿主开了通道，插件进程里字段却是 nil。
func TestAssembleInstanceAttachesMediaMetadata(t *testing.T) {
	server := &rpcServer{}
	inst, err := server.assembleInstance(InstancePayload{
		ID:                   "translator",
		ConfigJSON:           []byte("{}"),
		HostServicesBrokerID: 1,
	}, &hostServicesClient{})
	if err != nil {
		t.Fatalf("assembleInstance: %v", err)
	}
	if inst.MediaMetadata == nil {
		t.Fatal("assembleInstance 没挂上 MediaMetadata")
	}
}
