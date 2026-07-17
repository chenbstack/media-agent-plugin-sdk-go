package pluginrpc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

// storageProviderSession extends the normal provider session with a leased
// client for streaming operations. Standalone plugins keep their short-lived
// process alive until the stream closes, while Pack plugins lease the already
// dispensed client and therefore never start or stop another OS process.
type storageProviderSession interface {
	pluginID() string
	withClientForScope(ctx context.Context, scopeType, scopeID, operation string, fn func(*Client) error) error
	leaseClientForScope(ctx context.Context, scopeType, scopeID, operation string) (*Client, func(), error)
}

type externalStorageProviderSession struct{ external ExternalPlugin }

func (s externalStorageProviderSession) pluginID() string { return s.external.Manifest.ID }
func (s externalStorageProviderSession) withClientForScope(ctx context.Context, scopeType, scopeID, operation string, fn func(*Client) error) error {
	return s.external.withClientForScopeOperation(ctx, scopeType, scopeID, operation, fn)
}
func (s externalStorageProviderSession) leaseClientForScope(ctx context.Context, scopeType, scopeID, operation string) (*Client, func(), error) {
	running, err := s.external.startClientForScopeOperation(ctx, scopeType, scopeID, operation)
	if err != nil {
		return nil, nil, err
	}
	return running.client, running.Close, nil
}

type directStorageProviderSession struct{ client *Client }

func (s directStorageProviderSession) pluginID() string {
	if s.client == nil {
		return ""
	}
	return s.client.manifest.ID
}
func (s directStorageProviderSession) withClientForScope(_ context.Context, _, _, _ string, fn func(*Client) error) error {
	if s.client == nil {
		return fmt.Errorf("插件 RPC 客户端为空")
	}
	return fn(s.client)
}
func (s directStorageProviderSession) leaseClientForScope(_ context.Context, _, _, _ string) (*Client, func(), error) {
	if s.client == nil {
		return nil, nil, fmt.Errorf("插件 RPC 客户端为空")
	}
	return s.client, func() {}, nil
}

// Storage creates a Pack-safe StorageProvider adapter. File streams and
// HostServices broker calls share this Client's persistent Pack connection.
func (c *Client) Storage(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.StorageProvider {
	return &storageProvider{session: directStorageProviderSession{client: c}, inst: inst, secrets: secrets}
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

// Notifier creates a Pack-safe NotifierProvider adapter.
func (c *Client) Notifier(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.NotifierProvider {
	return &notifierProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// SubtitleSource creates a Pack-safe SubtitleSourceProvider adapter.
func (c *Client) SubtitleSource(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.SubtitleSourceProvider {
	return &subtitleSourceProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// Model creates a Pack-safe ModelProvider adapter. Model providers are not
// instance-scoped, matching Plugin.NewModel's factory contract.
func (c *Client) Model() providers.ModelProvider {
	return &modelProvider{session: directProviderSession{client: c}}
}

type downloaderProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.DownloaderProvider = (*downloaderProvider)(nil)
var _ providers.DownloaderTagProvider = (*downloaderProvider)(nil)

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

func (p *downloaderProvider) AddTags(ctx context.Context, hash string, tags []string) error {
	err := p.withPayload(ctx, "downloader.add_tags", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.DownloaderAddTags", DownloaderTagsRequest{Instance: instance, Hash: hash, Tags: tags}, &reply)
	})
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "can't find method") || strings.Contains(message, "method not found") ||
			strings.Contains(message, providers.ErrDownloaderTagsUnsupported.Error()) {
			return providers.ErrDownloaderTagsUnsupported
		}
	}
	return err
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
var _ providers.MediaServerIncrementalProvider = (*mediaServerProvider)(nil)

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
func (p *mediaServerProvider) ItemsChangedSince(ctx context.Context, libraryID, since string, startIndex, limit int) ([]providers.LibraryItem, int, error) {
	var out MediaServerChangedItemsReply
	err := p.json(ctx, "media_server.items_changed_since", "Plugin.MediaServerItemsChangedSince", func(instance InstancePayload) any {
		return MediaServerChangedItemsRequest{
			Instance: instance, LibraryID: libraryID, Since: since, StartIndex: startIndex, Limit: limit,
		}
	}, &out)
	if err != nil {
		// 升级后的宿主可能暂时连接仍运行旧 SDK 的 Pack。旧 RPC 服务没有该
		// 方法时按“不支持增量”降级，避免小时任务把兼容性差异当作同步故障。
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "can't find method") || strings.Contains(message, "method not found") {
			return nil, 0, providers.ErrIncrementalSyncUnsupported
		}
		return nil, 0, err
	}
	if !out.Supported {
		return nil, 0, providers.ErrIncrementalSyncUnsupported
	}
	return out.Items, out.Total, nil
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

type notifierProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.NotifierProvider = (*notifierProvider)(nil)

func (p *notifierProvider) Kind() string { return p.session.pluginID() }
func (p *notifierProvider) TestConnection(ctx context.Context) error {
	return p.withPayload(ctx, "notifier.test", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.NotifierTest", instance, &reply)
	})
}
func (p *notifierProvider) Send(ctx context.Context, message providers.NotificationMessage) error {
	return p.withPayload(ctx, "notifier.send", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.NotifierSend", NotifierSendRequest{Instance: instance, Message: message}, &reply)
	})
}
func (p *notifierProvider) withPayload(ctx context.Context, operation string, fn func(*Client, InstancePayload) error) error {
	return p.session.withClient(ctx, operation, func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		return fn(c, instance)
	})
}

type subtitleSourceProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.SubtitleSourceProvider = (*subtitleSourceProvider)(nil)

func (p *subtitleSourceProvider) Kind() string { return p.session.pluginID() }
func (p *subtitleSourceProvider) TestConnection(ctx context.Context) error {
	return p.withPayload(ctx, "subtitle_source.test", func(c *Client, instance InstancePayload) error {
		var reply Empty
		return c.call(ctx, "Plugin.SubtitleSourceTest", instance, &reply)
	})
}
func (p *subtitleSourceProvider) Search(ctx context.Context, request providers.SubtitleSearchRequest) ([]providers.SubtitleResult, error) {
	var out []providers.SubtitleResult
	err := p.withPayload(ctx, "subtitle_source.search", func(c *Client, instance InstancePayload) error {
		var reply JSONReply
		if err := c.call(ctx, "Plugin.SubtitleSourceSearch", SubtitleSearchRequest{Instance: instance, Request: request}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}
func (p *subtitleSourceProvider) Download(ctx context.Context, result providers.SubtitleResult) ([]byte, error) {
	var out []byte
	err := p.withPayload(ctx, "subtitle_source.download", func(c *Client, instance InstancePayload) error {
		var reply JSONReply
		if err := c.call(ctx, "Plugin.SubtitleSourceDownload", SubtitleDownloadRequest{Instance: instance, Result: result}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}
func (p *subtitleSourceProvider) withPayload(ctx context.Context, operation string, fn func(*Client, InstancePayload) error) error {
	return p.session.withClient(ctx, operation, func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		return fn(c, instance)
	})
}

type modelProvider struct {
	session  providerSession
	kindOnce sync.Once
	kind     string
}

var _ providers.ModelProvider = (*modelProvider)(nil)

func (p *modelProvider) Kind() string {
	p.kindOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = p.session.withClient(ctx, "model_provider.kind", func(c *Client) error {
			var reply StringReply
			if err := c.call(ctx, "Plugin.ModelKind", Empty{}, &reply); err != nil {
				return err
			}
			p.kind = reply.Value
			return nil
		})
		if p.kind == "" {
			p.kind = p.session.pluginID()
		}
	})
	return p.kind
}

func (p *modelProvider) ValidateModel(model providers.ModelConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := p.session.withClient(ctx, "model_provider.validate", func(c *Client) error {
		var reply Empty
		return c.call(ctx, "Plugin.ModelValidate", ModelConfigRequest{Model: model}, &reply)
	})
	return normalizeModelError(err)
}

func (p *modelProvider) Generate(ctx context.Context, request providers.ModelGenerateRequest) (providers.ModelGenerateResult, error) {
	var out providers.ModelGenerateResult
	err := p.session.withClient(ctx, "model_provider.generate", func(c *Client) error {
		wire := ModelGenerateRequest{Model: request.Model, Prompt: request.Prompt, MaxTokens: request.MaxTokens}
		wire.Now, wire.HasNow = snapshotClock(request.Now)
		var reply JSONReply
		if err := c.call(ctx, "Plugin.ModelGenerate", wire, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, normalizeModelError(err)
}

func (p *modelProvider) Download(ctx context.Context, request providers.ModelDownloadRequest) (providers.ModelDownloadResult, error) {
	var out providers.ModelDownloadResult
	err := p.session.withClient(ctx, "model_provider.download", func(c *Client) error {
		wire := ModelDownloadRequest{Model: request.Model, TimeoutSeconds: request.TimeoutSeconds}
		wire.Now, wire.HasNow = snapshotClock(request.Now)
		var reply JSONReply
		if err := c.call(ctx, "Plugin.ModelDownload", wire, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, normalizeModelError(err)
}

func (p *modelProvider) Uninstall(ctx context.Context, request providers.ModelUninstallRequest) (providers.ModelUninstallResult, error) {
	var out providers.ModelUninstallResult
	err := p.session.withClient(ctx, "model_provider.uninstall", func(c *Client) error {
		wire := ModelUninstallRequest{Model: request.Model, TimeoutSeconds: request.TimeoutSeconds}
		wire.Now, wire.HasNow = snapshotClock(request.Now)
		var reply JSONReply
		if err := c.call(ctx, "Plugin.ModelUninstall", wire, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, normalizeModelError(err)
}

func (p *modelProvider) CommandDisplay(model providers.ModelConfig) string {
	var out string
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = p.session.withClient(ctx, "model_provider.command_display", func(c *Client) error {
		var reply StringReply
		if err := c.call(ctx, "Plugin.ModelCommandDisplay", ModelConfigRequest{Model: model}, &reply); err != nil {
			return err
		}
		out = reply.Value
		return nil
	})
	return out
}

func snapshotClock(now func() time.Time) (time.Time, bool) {
	if now == nil {
		return time.Time{}, false
	}
	return now(), true
}

// net/rpc serializes server errors as text. Reattach the SDK's stable model
// sentinels so hosts can keep using errors.Is for HTTP/status classification.
func normalizeModelError(err error) error {
	if err == nil {
		return nil
	}
	for _, sentinel := range []error{
		providers.ErrModelProviderInvalidInput,
		providers.ErrModelProviderNotConfigured,
		providers.ErrModelProviderRuntimeMissing,
		providers.ErrModelProviderDownloadFailed,
		providers.ErrModelProviderUninstallFailed,
		providers.ErrModelProviderGenerationFailed,
	} {
		if strings.Contains(err.Error(), sentinel.Error()) {
			return fmt.Errorf("%w: %v", sentinel, err)
		}
	}
	return err
}
