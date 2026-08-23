package pluginrpc

import (
	"context"
	"errors"
	"testing"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
)

type stubSidecarReader struct {
	lastRef  string
	lastName string
}

func (s *stubSidecarReader) ListSubtitles(_ context.Context, fileRef string) ([]pluginsdk.SubtitleSidecar, error) {
	s.lastRef = fileRef
	return []pluginsdk.SubtitleSidecar{
		{Name: "S01E01.en.srt", Language: "en", Ext: "srt", SizeBytes: 42},
		{Name: "S01E01.en.forced.srt", Language: "en", Ext: "srt", Forced: true},
	}, nil
}

func (s *stubSidecarReader) ReadSubtitle(_ context.Context, fileRef, name string) ([]byte, error) {
	s.lastRef, s.lastName = fileRef, name
	if name != "S01E01.en.srt" {
		return nil, errors.New("不在列出的字幕里")
	}
	return []byte("1\n00:00:01,000 --> 00:00:02,000\nhello\n"), nil
}

func TestSidecarReaderRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), sidecarReader: &stubSidecarReader{}})
	var listReply JSONReply
	if err := server.ListSubtitles(SubtitleSidecarListRequest{FileRef: "f1"}, &listReply); err == nil {
		t.Fatal("未声明 media.sidecar.read 的插件不应列出字幕")
	}
	var readReply BytesReply
	if err := server.ReadSubtitle(SubtitleSidecarReadRequest{FileRef: "f1", Name: "S01E01.en.srt"}, &readReply); err == nil {
		t.Fatal("未声明 media.sidecar.read 的插件不应读取字幕")
	}

	// 写权限不能顺带把读放进来：写是往用户目录里放东西，读是把用户的内容取出来。
	server.live().permissions.Host = []string{"media.sidecar.write"}
	if err := server.ListSubtitles(SubtitleSidecarListRequest{FileRef: "f1"}, &listReply); err == nil {
		t.Fatal("media.sidecar.write 不应带来读取字幕的能力")
	}

	server.live().permissions.Host = []string{"media.sidecar.read"}
	if err := server.ListSubtitles(SubtitleSidecarListRequest{FileRef: "f1"}, &listReply); err != nil {
		t.Fatalf("ListSubtitles: %v", err)
	}
	var listed []pluginsdk.SubtitleSidecar
	if err := decodeJSON(listReply.Data, &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed) != 2 || listed[0].Language != "en" || listed[0].SizeBytes != 42 {
		t.Fatalf("listed = %+v", listed)
	}
	// forced 段必须过得来：拿只翻译外语对白的那份当翻译源，成品只剩零星几句。
	if listed[0].Forced || !listed[1].Forced {
		t.Fatalf("forced 标记丢失: %+v", listed)
	}
}

func TestReadSubtitlePassesRefAndNameThrough(t *testing.T) {
	reader := &stubSidecarReader{}
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), sidecarReader: reader})
	server.live().permissions.Host = []string{"media.sidecar.read"}

	var reply BytesReply
	if err := server.ReadSubtitle(SubtitleSidecarReadRequest{FileRef: "f9", Name: "S01E01.en.srt"}, &reply); err != nil {
		t.Fatalf("ReadSubtitle: %v", err)
	}
	if reader.lastRef != "f9" || reader.lastName != "S01E01.en.srt" {
		t.Fatalf("ref/name 未透传: %q %q", reader.lastRef, reader.lastName)
	}
	if len(reply.Data) == 0 {
		t.Fatal("字幕内容为空")
	}
}

func TestSidecarReaderRejectsMissingHostService(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background()})
	server.live().permissions.Host = []string{"media.sidecar.read"}
	var reply JSONReply
	if err := server.ListSubtitles(SubtitleSidecarListRequest{FileRef: "f1"}, &reply); err == nil {
		t.Fatal("宿主没提供读取能力时应报错")
	}
}

// 漏了这两处不报错，服务只是静默不存在——插件调用时拿到的是「宿主未提供」。
func TestNeedsHostServicesCoversSidecarReader(t *testing.T) {
	if !needsHostServices(pluginsdk.Instance{SidecarReader: &stubSidecarReader{}}, nil) {
		t.Fatal("needsHostServices 漏了 SidecarReader")
	}
	server := &rpcServer{}
	inst, err := server.assembleInstance(InstancePayload{
		ID:                   "translator",
		ConfigJSON:           []byte("{}"),
		HostServicesBrokerID: 1,
	}, &hostServicesClient{})
	if err != nil {
		t.Fatalf("assembleInstance: %v", err)
	}
	if inst.SidecarReader == nil {
		t.Fatal("assembleInstance 没接上 SidecarReader")
	}
}
