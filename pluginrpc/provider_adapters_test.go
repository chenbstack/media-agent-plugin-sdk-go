package pluginrpc

import (
	"context"
	"net"
	"net/rpc"
	"reflect"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
	providerfake "github.com/chenbstack/media-agent-plugin-sdk-go/providers/fake"
)

func TestProviderAdaptersUseDispensedClientForAllCoreProviders(t *testing.T) {
	downloader := providerfake.NewDownloader()
	downloader.SetTransferInfo(providers.TransferInfo{DownloadSpeed: 12, UploadSpeed: 3})
	media := providerfake.NewMediaServer()
	media.AddLibrary(providers.Library{ExternalID: "lib", Name: "Movies"})
	media.AddItem(providers.LibraryItem{ExternalID: "movie", LibraryID: "lib", Title: "Arrival", TMDBID: 329865})
	metadata := &rpcMetadataProvider{}
	site := &rpcSiteProvider{}

	plugin := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "official", Name: "Official"},
		NewDownloader: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.DownloaderProvider, error) {
			return downloader, nil
		},
		NewMediaServer: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.MediaServerProvider, error) {
			return media, nil
		},
		NewMetadata: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.MetadataProvider, error) {
			return metadata, nil
		},
		NewSite: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.SiteProvider, error) {
			return site, nil
		},
	}
	client := newProviderTestClient(t, plugin)
	inst := pluginsdk.Instance{ID: "instance", Name: "Instance", Config: map[string]any{"base_url": "https://example.test"}}

	d := client.Downloader(inst, nil)
	if d.Kind() != "official" {
		t.Fatalf("downloader kind = %q", d.Kind())
	}
	if err := d.TestConnection(context.Background()); err != nil {
		t.Fatalf("downloader TestConnection: %v", err)
	}
	task, err := d.Add(context.Background(), providers.AddTorrentRequest{Magnet: "magnet:?xt=urn:btih:test"})
	if err != nil {
		t.Fatalf("downloader Add: %v", err)
	}
	if got, err := d.List(context.Background()); err != nil || len(got) != 1 {
		t.Fatalf("downloader List = %#v, %v", got, err)
	}
	if err := d.Pause(context.Background(), task.Hash); err != nil {
		t.Fatalf("downloader Pause: %v", err)
	}
	if err := d.Resume(context.Background(), task.Hash); err != nil {
		t.Fatalf("downloader Resume: %v", err)
	}
	selection := []providers.TorrentFile{{Index: 0, Path: "movie.mkv", Selected: true}}
	if err := d.SetFileSelection(context.Background(), task.Hash, selection); err != nil {
		t.Fatalf("downloader SetFileSelection: %v", err)
	}
	if got, err := d.Files(context.Background(), task.Hash); err != nil || len(got) != 1 || got[0].Priority != 1 {
		t.Fatalf("downloader Files = %#v, %v", got, err)
	}
	if got, err := d.TransferInfo(context.Background()); err != nil || got.DownloadSpeed != 12 {
		t.Fatalf("downloader TransferInfo = %#v, %v", got, err)
	}
	if err := d.Remove(context.Background(), task.Hash, true); err != nil {
		t.Fatalf("downloader Remove: %v", err)
	}

	m := client.MediaServer(inst, nil)
	if err := m.TestConnection(context.Background()); err != nil {
		t.Fatalf("media TestConnection: %v", err)
	}
	if got, err := m.Libraries(context.Background()); err != nil || len(got) != 1 || got[0].ExternalID != "lib" {
		t.Fatalf("media Libraries = %#v, %v", got, err)
	}
	if got, total, err := m.Items(context.Background(), "lib", 0, 10); err != nil || total != 1 || len(got) != 1 {
		t.Fatalf("media Items = %#v, %d, %v", got, total, err)
	}
	if got, err := m.Search(context.Background(), "Arrival"); err != nil || len(got) != 1 {
		t.Fatalf("media Search = %#v, %v", got, err)
	}
	if got, err := m.Exists(context.Background(), providers.MediaRef{TMDBID: 329865}); err != nil || !got.Exists {
		t.Fatalf("media Exists = %#v, %v", got, err)
	}
	if err := m.RefreshItem(context.Background(), "movie"); err != nil {
		t.Fatalf("media RefreshItem: %v", err)
	}
	if err := m.RefreshLibrary(context.Background(), "lib"); err != nil {
		t.Fatalf("media RefreshLibrary: %v", err)
	}
	if got, err := m.Latest(context.Background(), 1); err != nil || len(got) != 1 {
		t.Fatalf("media Latest = %#v, %v", got, err)
	}

	meta := client.Metadata(inst, nil)
	if err := meta.TestConnection(context.Background()); err != nil {
		t.Fatalf("metadata TestConnection: %v", err)
	}
	if got, err := meta.Search(context.Background(), "Arrival", "movie", 2016); err != nil || got[0].ProviderID != "search" {
		t.Fatalf("metadata Search = %#v, %v", got, err)
	}
	if got, err := meta.Detail(context.Background(), "movie", "42"); err != nil || got.ProviderID != "42" {
		t.Fatalf("metadata Detail = %#v, %v", got, err)
	}
	if got, err := meta.SeasonEpisodes(context.Background(), "series", 2); err != nil || got[0].SeasonNumber != 2 {
		t.Fatalf("metadata SeasonEpisodes = %#v, %v", got, err)
	}
	if got, err := meta.FindByExternalID(context.Background(), providers.MetaExternalIDs{IMDBID: "tt2543164"}); err != nil || got[0].ProviderID != "tt2543164" {
		t.Fatalf("metadata FindByExternalID = %#v, %v", got, err)
	}

	s := client.Site(inst, nil)
	if err := s.TestConnection(context.Background()); err != nil {
		t.Fatalf("site TestConnection: %v", err)
	}
	if got, err := s.Profile(context.Background()); err != nil || got.Username != "tester" {
		t.Fatalf("site Profile = %#v, %v", got, err)
	}
	request := providers.TorrentSearchRequest{Keyword: "Arrival", Page: 2}
	if got, err := s.Search(context.Background(), request); err != nil || got[0].Title != "Arrival" || !reflect.DeepEqual(site.lastSearch, request) {
		t.Fatalf("site Search = %#v, last=%#v, %v", got, site.lastSearch, err)
	}
}

func TestExternalPluginBuildsCoreProviderFactoriesFromCapabilities(t *testing.T) {
	plugin := (ExternalPlugin{Manifest: pluginsdk.Manifest{
		ID: "official", Capabilities: []string{"downloader.add", "media_server.search", "metadata.search", "site.search"},
	}}).Plugin()
	if plugin.NewDownloader == nil || plugin.NewMediaServer == nil || plugin.NewMetadata == nil || plugin.NewSite == nil {
		t.Fatalf("provider factories missing: downloader=%v media=%v metadata=%v site=%v",
			plugin.NewDownloader != nil, plugin.NewMediaServer != nil, plugin.NewMetadata != nil, plugin.NewSite != nil)
	}
}

func newProviderTestClient(t *testing.T, plugin pluginsdk.Plugin) *Client {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Plugin", &rpcServer{plugin: plugin}); err != nil {
		t.Fatalf("RegisterName: %v", err)
	}
	clientConn, serverConn := net.Pipe()
	go server.ServeConn(serverConn)
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	return &Client{client: rpc.NewClient(clientConn), manifest: plugin.Manifest}
}

type rpcMetadataProvider struct{}

func (*rpcMetadataProvider) Kind() string                         { return "metadata" }
func (*rpcMetadataProvider) TestConnection(context.Context) error { return nil }
func (*rpcMetadataProvider) Search(context.Context, string, string, int) ([]providers.MetaSearchResult, error) {
	return []providers.MetaSearchResult{{ProviderID: "search"}}, nil
}
func (*rpcMetadataProvider) Detail(_ context.Context, mediaType, providerID string) (providers.MetaDetail, error) {
	return providers.MetaDetail{MediaType: mediaType, ProviderID: providerID}, nil
}
func (*rpcMetadataProvider) SeasonEpisodes(_ context.Context, _ string, seasonNumber int) ([]providers.MetaEpisode, error) {
	return []providers.MetaEpisode{{SeasonNumber: seasonNumber, EpisodeNumber: 1}}, nil
}
func (*rpcMetadataProvider) FindByExternalID(_ context.Context, ids providers.MetaExternalIDs) ([]providers.MetaSearchResult, error) {
	return []providers.MetaSearchResult{{ProviderID: ids.IMDBID}}, nil
}

type rpcSiteProvider struct {
	lastSearch providers.TorrentSearchRequest
}

func (*rpcSiteProvider) Kind() string                         { return "site" }
func (*rpcSiteProvider) TestConnection(context.Context) error { return nil }
func (*rpcSiteProvider) Profile(context.Context) (providers.SiteProfile, error) {
	return providers.SiteProfile{Username: "tester"}, nil
}
func (p *rpcSiteProvider) Search(_ context.Context, request providers.TorrentSearchRequest) ([]providers.TorrentResult, error) {
	p.lastSearch = request
	return []providers.TorrentResult{{Title: request.Keyword}}, nil
}
