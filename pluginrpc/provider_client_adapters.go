package pluginrpc

import (
	"context"
	"fmt"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// providerSession hides whether a provider call uses a short-lived standalone
// process or an already-dispensed logical client in a Pack. The provider
// adapters below are deliberately shared by both transports.
type providerSession interface {
	pluginID() string
	withClient(ctx context.Context, operation string, fn func(*Client) error) error
}

type externalProviderSession struct{ external ExternalPlugin }

func (s externalProviderSession) pluginID() string { return s.external.Manifest.ID }
func (s externalProviderSession) withClient(ctx context.Context, operation string, fn func(*Client) error) error {
	return s.external.withClientOperation(ctx, operation, fn)
}

type directProviderSession struct{ client *Client }

func (s directProviderSession) pluginID() string {
	if s.client == nil {
		return ""
	}
	return s.client.manifest.ID
}
func (s directProviderSession) withClient(_ context.Context, _ string, fn func(*Client) error) error {
	if s.client == nil {
		return fmt.Errorf("插件 RPC 客户端为空")
	}
	return fn(s.client)
}

// Downloader creates the same DownloaderProvider adapter used by standalone
// ExternalPlugin. On a Pack client it reuses the already running Pack process.
func (c *Client) Downloader(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.DownloaderProvider {
	return &downloaderProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// MediaServer creates a Pack-safe MediaServerProvider adapter.
func (c *Client) MediaServer(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.MediaServerProvider {
	return &mediaServerProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// Metadata creates a Pack-safe MetadataProvider adapter.
func (c *Client) Metadata(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.MetadataProvider {
	return &metadataProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// Site creates a Pack-safe SiteProvider adapter.
func (c *Client) Site(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.SiteProvider {
	return &siteProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

type downloaderProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.DownloaderProvider = (*downloaderProvider)(nil)

func (p *downloaderProvider) Kind() string { return p.session.pluginID() }

func (p *downloaderProvider) TestConnection(ctx context.Context) error {
	return p.call(ctx, "downloader.test", "Plugin.DownloaderTest", nil)
}

func (p *downloaderProvider) Add(ctx context.Context, request providers.AddTorrentRequest) (providers.TorrentTask, error) {
	var out providers.TorrentTask
	err := p.withPayload(ctx, "downloader.add", func(c *Client, instance InstancePayload) error {
		var reply JSONReply
		if err := c.call(ctx, "Plugin.DownloaderAdd", DownloaderAddRequest{Instance: instance, Request: request}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}

func (p *downloaderProvider) List(ctx context.Context) ([]providers.TorrentTask, error) {
	var out []providers.TorrentTask
	err := p.call(ctx, "downloader.list", "Plugin.DownloaderList", &out)
	return out, err
}

func (p *downloaderProvider) Pause(ctx context.Context, hash string) error {
	return p.hashCall(ctx, "downloader.pause", "Plugin.DownloaderPause", hash)
}

func (p *downloaderProvider) Resume(ctx context.Context, hash string) error {
	return p.hashCall(ctx, "downloader.resume", "Plugin.DownloaderResume", hash)
}

func (p *downloaderProvider) Remove(ctx context.Context, hash string, deleteData bool) error {
	return p.withPayload(ctx, "downloader.remove", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.DownloaderRemove", DownloaderRemoveRequest{Instance: instance, Hash: hash, DeleteData: deleteData}, &reply)
	})
}

func (p *downloaderProvider) Files(ctx context.Context, hash string) ([]providers.TorrentFile, error) {
	var out []providers.TorrentFile
	err := p.hashJSONCall(ctx, "downloader.files", "Plugin.DownloaderFiles", hash, &out)
	return out, err
}

func (p *downloaderProvider) SetFileSelection(ctx context.Context, hash string, files []providers.TorrentFile) error {
	return p.withPayload(ctx, "downloader.set_file_selection", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.DownloaderSetFileSelection", DownloaderFileSelectionRequest{Instance: instance, Hash: hash, Files: files}, &reply)
	})
}

func (p *downloaderProvider) TransferInfo(ctx context.Context) (providers.TransferInfo, error) {
	var out providers.TransferInfo
	err := p.call(ctx, "downloader.transfer_info", "Plugin.DownloaderTransferInfo", &out)
	return out, err
}

func (p *downloaderProvider) call(ctx context.Context, operation, method string, out any) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		if out == nil {
			var reply Empty
			return c.call(ctx, method, instance, &reply)
		}
		var reply JSONReply
		if err := c.call(ctx, method, instance, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, out)
	})
}

func (p *downloaderProvider) hashCall(ctx context.Context, operation, method, hash string) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, method, DownloaderHashRequest{Instance: instance, Hash: hash}, &reply)
	})
}

func (p *downloaderProvider) hashJSONCall(ctx context.Context, operation, method, hash string, out any) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		var reply JSONReply
		if err := c.call(ctx, method, DownloaderHashRequest{Instance: instance, Hash: hash}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, out)
	})
}

func (p *downloaderProvider) withPayload(ctx context.Context, operation string, fn func(*Client, InstancePayload) error) error {
	return p.session.withClient(ctx, operation, func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		return fn(c, instance)
	})
}

type mediaServerProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.MediaServerProvider = (*mediaServerProvider)(nil)

func (p *mediaServerProvider) Kind() string { return p.session.pluginID() }
func (p *mediaServerProvider) TestConnection(ctx context.Context) error {
	return p.empty(ctx, "media_server.test", "Plugin.MediaServerTest")
}
func (p *mediaServerProvider) Libraries(ctx context.Context) ([]providers.Library, error) {
	var out []providers.Library
	err := p.json(ctx, "media_server.libraries", "Plugin.MediaServerLibraries", nil, &out)
	return out, err
}
func (p *mediaServerProvider) Items(ctx context.Context, libraryID string, startIndex, limit int) ([]providers.LibraryItem, int, error) {
	var out MediaServerItemsReply
	err := p.json(ctx, "media_server.items", "Plugin.MediaServerItems", func(instance InstancePayload) any {
		return MediaServerItemsRequest{Instance: instance, LibraryID: libraryID, StartIndex: startIndex, Limit: limit}
	}, &out)
	return out.Items, out.Total, err
}
func (p *mediaServerProvider) Search(ctx context.Context, query string) ([]providers.LibraryItem, error) {
	var out []providers.LibraryItem
	err := p.json(ctx, "media_server.search", "Plugin.MediaServerSearch", func(instance InstancePayload) any {
		return MediaServerSearchRequest{Instance: instance, Query: query}
	}, &out)
	return out, err
}
func (p *mediaServerProvider) Exists(ctx context.Context, ref providers.MediaRef) (providers.Existence, error) {
	var out providers.Existence
	err := p.json(ctx, "media_server.exists", "Plugin.MediaServerExists", func(instance InstancePayload) any {
		return MediaServerExistsRequest{Instance: instance, Ref: ref}
	}, &out)
	return out, err
}
func (p *mediaServerProvider) RefreshItem(ctx context.Context, externalID string) error {
	return p.idCall(ctx, "media_server.refresh_item", "Plugin.MediaServerRefreshItem", externalID)
}
func (p *mediaServerProvider) RefreshLibrary(ctx context.Context, externalLibraryID string) error {
	return p.idCall(ctx, "media_server.refresh_library", "Plugin.MediaServerRefreshLibrary", externalLibraryID)
}
func (p *mediaServerProvider) Latest(ctx context.Context, limit int) ([]providers.LibraryItem, error) {
	var out []providers.LibraryItem
	err := p.json(ctx, "media_server.latest", "Plugin.MediaServerLatest", func(instance InstancePayload) any {
		return MediaServerLatestRequest{Instance: instance, Limit: limit}
	}, &out)
	return out, err
}
func (p *mediaServerProvider) empty(ctx context.Context, operation, method string) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, method, instance, &reply)
	})
}
func (p *mediaServerProvider) idCall(ctx context.Context, operation, method, externalID string) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, method, MediaServerIDRequest{Instance: instance, ExternalID: externalID}, &reply)
	})
}
func (p *mediaServerProvider) json(ctx context.Context, operation, method string, request func(InstancePayload) any, out any) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		args := any(instance)
		if request != nil {
			args = request(instance)
		}
		var reply JSONReply
		if err := c.call(ctx, method, args, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, out)
	})
}
func (p *mediaServerProvider) withPayload(ctx context.Context, operation string, fn func(*Client, InstancePayload) error) error {
	return p.session.withClient(ctx, operation, func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		return fn(c, instance)
	})
}

type metadataProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.MetadataProvider = (*metadataProvider)(nil)

func (p *metadataProvider) Kind() string { return p.session.pluginID() }
func (p *metadataProvider) TestConnection(ctx context.Context) error {
	return p.withPayload(ctx, "metadata.test", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.MetadataTest", instance, &reply)
	})
}
func (p *metadataProvider) Search(ctx context.Context, query, mediaType string, year int) ([]providers.MetaSearchResult, error) {
	var out []providers.MetaSearchResult
	err := p.json(ctx, "metadata.search", "Plugin.MetadataSearch", func(instance InstancePayload) any {
		return MetadataSearchRequest{Instance: instance, Query: query, MediaType: mediaType, Year: year}
	}, &out)
	return out, err
}
func (p *metadataProvider) Detail(ctx context.Context, mediaType, providerID string) (providers.MetaDetail, error) {
	var out providers.MetaDetail
	err := p.json(ctx, "metadata.detail", "Plugin.MetadataDetail", func(instance InstancePayload) any {
		return MetadataDetailRequest{Instance: instance, MediaType: mediaType, ProviderID: providerID}
	}, &out)
	return out, err
}
func (p *metadataProvider) SeasonEpisodes(ctx context.Context, providerID string, seasonNumber int) ([]providers.MetaEpisode, error) {
	var out []providers.MetaEpisode
	err := p.json(ctx, "metadata.season_episodes", "Plugin.MetadataSeasonEpisodes", func(instance InstancePayload) any {
		return MetadataSeasonEpisodesRequest{Instance: instance, ProviderID: providerID, SeasonNumber: seasonNumber}
	}, &out)
	return out, err
}
func (p *metadataProvider) FindByExternalID(ctx context.Context, ids providers.MetaExternalIDs) ([]providers.MetaSearchResult, error) {
	var out []providers.MetaSearchResult
	err := p.json(ctx, "metadata.find_by_external_id", "Plugin.MetadataFindByExternalID", func(instance InstancePayload) any {
		return MetadataExternalIDRequest{Instance: instance, IDs: ids}
	}, &out)
	return out, err
}
func (p *metadataProvider) json(ctx context.Context, operation, method string, request func(InstancePayload) any, out any) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		var reply JSONReply
		if err := c.call(ctx, method, request(instance), &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, out)
	})
}
func (p *metadataProvider) withPayload(ctx context.Context, operation string, fn func(*Client, InstancePayload) error) error {
	return p.session.withClient(ctx, operation, func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		return fn(c, instance)
	})
}

type siteProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.SiteProvider = (*siteProvider)(nil)

func (p *siteProvider) Kind() string { return p.session.pluginID() }
func (p *siteProvider) TestConnection(ctx context.Context) error {
	return p.withPayload(ctx, "site.test", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.SiteTest", instance, &reply)
	})
}
func (p *siteProvider) Profile(ctx context.Context) (providers.SiteProfile, error) {
	var out providers.SiteProfile
	err := p.json(ctx, "site.profile", "Plugin.SiteProfile", nil, &out)
	return out, err
}
func (p *siteProvider) Search(ctx context.Context, request providers.TorrentSearchRequest) ([]providers.TorrentResult, error) {
	var out []providers.TorrentResult
	err := p.json(ctx, "site.search", "Plugin.SiteSearch", func(instance InstancePayload) any {
		return SiteSearchRequest{Instance: instance, Request: request}
	}, &out)
	return out, err
}
func (p *siteProvider) json(ctx context.Context, operation, method string, request func(InstancePayload) any, out any) error {
	return p.withPayload(ctx, operation, func(c *Client, instance InstancePayload) error {
		args := any(instance)
		if request != nil {
			args = request(instance)
		}
		var reply JSONReply
		if err := c.call(ctx, method, args, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, out)
	})
}
func (p *siteProvider) withPayload(ctx context.Context, operation string, fn func(*Client, InstancePayload) error) error {
	return p.session.withClient(ctx, operation, func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		return fn(c, instance)
	})
}
