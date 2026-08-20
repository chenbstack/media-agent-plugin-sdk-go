package pluginrpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// capableSite 实现站点的全部可选能力，模拟官方站点插件。
type capableSite struct {
	rpcSiteProvider
	fetchedURL   string
	mediaInfoURL string
	attachURL    string
	subtitleURL  string
	diagnosed    providers.DiagnosticInput
}

func (s *capableSite) FetchTorrent(_ context.Context, url string) ([]byte, error) {
	s.fetchedURL = url
	return []byte("d4:infoe"), nil
}

func (s *capableSite) TorrentMediaInfo(_ context.Context, detailURL string) (providers.TorrentMediaInfo, error) {
	s.mediaInfoURL = detailURL
	return providers.TorrentMediaInfo{Raw: "Video: x264"}, nil
}

func (s *capableSite) SubtitleAttachments(_ context.Context, detailURL string) ([]providers.SubtitleAttachment, error) {
	s.attachURL = detailURL
	return []providers.SubtitleAttachment{{Name: "chs.srt", DownloadURL: "https://site.example/sub/1"}}, nil
}

func (s *capableSite) DownloadSubtitle(_ context.Context, downloadURL string) ([]byte, error) {
	s.subtitleURL = downloadURL
	return []byte("1\n00:00:00,000 --> 00:00:01,000\n"), nil
}

func (s *capableSite) Diagnose(_ context.Context, input providers.DiagnosticInput) providers.DiagnosticReport {
	s.diagnosed = input
	return providers.DiagnosticReport{Status: providers.DiagnosticOK, RuleID: "demo"}
}

func (s *capableSite) SupportsIMDBSearch() bool { return true }

func siteClientFor(t *testing.T, provider providers.SiteProvider) providers.SiteProvider {
	t.Helper()
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "official-site", Name: "Site"},
		NewSite: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.SiteProvider, error) {
			return provider, nil
		},
	})
	return client.Site(pluginsdk.Instance{ID: "instance"}, nil)
}

// 站点插件具备全部可选能力时，跨进程适配器必须如实汇报并把调用透传下去。
// 这是站点插件迁出宿主进程后，种子下载 / MediaInfo 校验 / 字幕 / 诊断能不能继续
// 工作的唯一保证——宿主侧原来的类型断言在 RPC 之后一律失败。
func TestSiteCapabilitiesRoundTrip(t *testing.T) {
	backing := &capableSite{}
	site := siteClientFor(t, backing)

	caps := providers.CapabilitiesOfSite(site)
	want := providers.SiteCapabilities{
		TorrentFetch: true, MediaInfoRead: true, SubtitlesRead: true,
		Diagnose: true, IMDBSearch: true,
	}
	if caps != want {
		t.Fatalf("capabilities = %+v, want %+v", caps, want)
	}

	ctx := context.Background()
	data, err := site.(providers.TorrentFetcher).FetchTorrent(ctx, "https://site.example/download.php?id=1")
	if err != nil || string(data) != "d4:infoe" {
		t.Fatalf("FetchTorrent = %q, %v", data, err)
	}
	if backing.fetchedURL != "https://site.example/download.php?id=1" {
		t.Fatalf("种子地址未透传: %q", backing.fetchedURL)
	}

	info, err := site.(providers.TorrentMediaInfoProvider).TorrentMediaInfo(ctx, "https://site.example/details.php?id=1")
	if err != nil || info.Raw != "Video: x264" {
		t.Fatalf("TorrentMediaInfo = %+v, %v", info, err)
	}

	attachments, err := site.(providers.SiteSubtitleProvider).SubtitleAttachments(ctx, "https://site.example/details.php?id=1")
	if err != nil || len(attachments) != 1 || attachments[0].Name != "chs.srt" {
		t.Fatalf("SubtitleAttachments = %+v, %v", attachments, err)
	}

	subtitle, err := site.(providers.SiteSubtitleProvider).DownloadSubtitle(ctx, "https://site.example/sub/1")
	if err != nil || len(subtitle) == 0 {
		t.Fatalf("DownloadSubtitle = %q, %v", subtitle, err)
	}
	if backing.subtitleURL != "https://site.example/sub/1" {
		t.Fatalf("字幕地址未透传: %q", backing.subtitleURL)
	}

	report := site.(providers.SiteDiagnoser).Diagnose(ctx, providers.DiagnosticInput{Keyword: "沙丘"})
	if report.Status != providers.DiagnosticOK || report.RuleID != "demo" {
		t.Fatalf("Diagnose = %+v", report)
	}
	if backing.diagnosed.Keyword != "沙丘" {
		t.Fatalf("诊断输入未透传: %+v", backing.diagnosed)
	}
}

// 站点插件不具备可选能力时，能力集必须如实为空，且调用返回显式的
// ErrCapabilityUnsupported——不能是空结果加 nil error，那正是迁移要消灭的静默降级。
func TestSiteWithoutCapabilitiesReportsAndFailsExplicitly(t *testing.T) {
	site := siteClientFor(t, &rpcSiteProvider{})

	if caps := providers.CapabilitiesOfSite(site); caps != (providers.SiteCapabilities{}) {
		t.Fatalf("capabilities = %+v, want 全空", caps)
	}

	ctx := context.Background()
	if _, err := site.(providers.TorrentFetcher).FetchTorrent(ctx, "https://site.example/d"); !isUnsupported(err) {
		t.Fatalf("FetchTorrent err = %v, want ErrCapabilityUnsupported", err)
	}
	if _, err := site.(providers.TorrentMediaInfoProvider).TorrentMediaInfo(ctx, "https://site.example/x"); !isUnsupported(err) {
		t.Fatalf("TorrentMediaInfo err = %v, want ErrCapabilityUnsupported", err)
	}
	if _, err := site.(providers.SiteSubtitleProvider).SubtitleAttachments(ctx, "https://site.example/x"); !isUnsupported(err) {
		t.Fatalf("SubtitleAttachments err = %v, want ErrCapabilityUnsupported", err)
	}
	if _, err := site.(providers.SiteSubtitleProvider).DownloadSubtitle(ctx, "https://site.example/x"); !isUnsupported(err) {
		t.Fatalf("DownloadSubtitle err = %v, want ErrCapabilityUnsupported", err)
	}
	report := site.(providers.SiteDiagnoser).Diagnose(ctx, providers.DiagnosticInput{})
	if report.Status != providers.DiagnosticError {
		t.Fatalf("Diagnose 不支持时应返回 error 报告，实得 %+v", report)
	}
}

// RPC 把错误压成字符串，errors.Is 跨不过进程边界，宿主只能按错误文本识别。
// 这条约束本身也要锁住：哨兵错误的文本一旦改动，宿主的判定会静默失效。
func isUnsupported(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, providers.ErrCapabilityUnsupported) ||
		strings.Contains(err.Error(), providers.ErrCapabilityUnsupported.Error())
}

// 站点地址判定发生在用户还没建站点账号的时刻，所以它是插件级 RPC 而不是
// Provider 方法。宿主新建连接表单要靠它决定「这个站点支持吗、要填哪些字段」。
func TestSiteSupportForURLRoundTrip(t *testing.T) {
	var gotURL string
	var gotInstance string
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "official-site", Name: "Site"},
		SiteSupportForURL: func(_ context.Context, inst pluginsdk.Instance, url string) (providers.SiteSupport, error) {
			gotURL = url
			gotInstance = inst.ID
			return providers.SiteSupport{
				Supported: true, ID: "demo", Name: "Demo",
				AuthFields: []providers.AuthField{{Name: "cookie", Required: true, Secret: true}},
			}, nil
		},
	})

	support, err := client.SiteSupportForURL(context.Background(), pluginsdk.Instance{ID: "global"}, "https://demo.example/")
	if err != nil {
		t.Fatalf("SiteSupportForURL: %v", err)
	}
	if gotURL != "https://demo.example/" {
		t.Fatalf("地址未透传: %q", gotURL)
	}
	if !support.Supported || support.ID != "demo" || len(support.AuthFields) != 1 {
		t.Fatalf("support = %+v", support)
	}
	if field := support.AuthFields[0]; field.Name != "cookie" || !field.Required || !field.Secret {
		t.Fatalf("认证字段声明未透传: %+v", field)
	}
	// 判定要读规则来源，所以这次调用必须带着实例过去（宿主服务挂在实例上）；
	// 只传 URL 的话，跨进程的插件读不到规则目录，只能对所有地址回答「不支持」。
	if gotInstance != "global" {
		t.Fatalf("实例未透传: %q", gotInstance)
	}
}

// 插件不做站点地址判定时必须是显式的不支持错误，不能是一个「什么都不支持」的空结果
// ——后者会让宿主把所有站点地址都当成不受支持，且看不出是插件没实现。
func TestSiteSupportForURLUnsupported(t *testing.T) {
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "other", Name: "Other"},
	})
	if _, err := client.SiteSupportForURL(context.Background(), pluginsdk.Instance{ID: "global"}, "https://demo.example/"); !isUnsupported(err) {
		t.Fatalf("err = %v, want ErrCapabilityUnsupported", err)
	}
}
