package pluginrpc

import (
	"context"
	"fmt"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

func (s *rpcServer) DownloaderTest(req InstancePayload, _ *Empty) error {
	p, closeFn, err := s.downloader(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.TestConnection(context.Background())
}

func (s *rpcServer) DownloaderAdd(req DownloaderAddRequest, reply *JSONReply) error {
	p, closeFn, err := s.downloader(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Add(context.Background(), req.Request)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) DownloaderList(req InstancePayload, reply *JSONReply) error {
	p, closeFn, err := s.downloader(req)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.List(context.Background())
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) DownloaderPause(req DownloaderHashRequest, _ *Empty) error {
	p, closeFn, err := s.downloader(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.Pause(context.Background(), req.Hash)
}

func (s *rpcServer) DownloaderResume(req DownloaderHashRequest, _ *Empty) error {
	p, closeFn, err := s.downloader(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.Resume(context.Background(), req.Hash)
}

func (s *rpcServer) DownloaderRemove(req DownloaderRemoveRequest, _ *Empty) error {
	p, closeFn, err := s.downloader(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.Remove(context.Background(), req.Hash, req.DeleteData)
}

func (s *rpcServer) DownloaderFiles(req DownloaderHashRequest, reply *JSONReply) error {
	p, closeFn, err := s.downloader(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Files(context.Background(), req.Hash)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) DownloaderSetFileSelection(req DownloaderFileSelectionRequest, _ *Empty) error {
	p, closeFn, err := s.downloader(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.SetFileSelection(context.Background(), req.Hash, req.Files)
}

func (s *rpcServer) DownloaderTransferInfo(req InstancePayload, reply *JSONReply) error {
	p, closeFn, err := s.downloader(req)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.TransferInfo(context.Background())
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MediaServerTest(req InstancePayload, _ *Empty) error {
	p, closeFn, err := s.mediaServer(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.TestConnection(context.Background())
}

func (s *rpcServer) MediaServerLibraries(req InstancePayload, reply *JSONReply) error {
	p, closeFn, err := s.mediaServer(req)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Libraries(context.Background())
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MediaServerItems(req MediaServerItemsRequest, reply *JSONReply) error {
	p, closeFn, err := s.mediaServer(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	items, total, err := p.Items(context.Background(), req.LibraryID, req.StartIndex, req.Limit)
	if err != nil {
		return err
	}
	out, err := encodeJSON(MediaServerItemsReply{Items: items, Total: total})
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) MediaServerSearch(req MediaServerSearchRequest, reply *JSONReply) error {
	p, closeFn, err := s.mediaServer(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Search(context.Background(), req.Query)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MediaServerExists(req MediaServerExistsRequest, reply *JSONReply) error {
	p, closeFn, err := s.mediaServer(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Exists(context.Background(), req.Ref)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MediaServerRefreshItem(req MediaServerIDRequest, _ *Empty) error {
	p, closeFn, err := s.mediaServer(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.RefreshItem(context.Background(), req.ExternalID)
}

func (s *rpcServer) MediaServerRefreshLibrary(req MediaServerIDRequest, _ *Empty) error {
	p, closeFn, err := s.mediaServer(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.RefreshLibrary(context.Background(), req.ExternalID)
}

func (s *rpcServer) MediaServerLatest(req MediaServerLatestRequest, reply *JSONReply) error {
	p, closeFn, err := s.mediaServer(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Latest(context.Background(), req.Limit)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MetadataTest(req InstancePayload, _ *Empty) error {
	p, closeFn, err := s.metadata(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.TestConnection(context.Background())
}

func (s *rpcServer) MetadataSearch(req MetadataSearchRequest, reply *JSONReply) error {
	p, closeFn, err := s.metadata(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Search(context.Background(), req.Query, req.MediaType, req.Year)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MetadataDetail(req MetadataDetailRequest, reply *JSONReply) error {
	p, closeFn, err := s.metadata(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Detail(context.Background(), req.MediaType, req.ProviderID)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MetadataSeasonEpisodes(req MetadataSeasonEpisodesRequest, reply *JSONReply) error {
	p, closeFn, err := s.metadata(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.SeasonEpisodes(context.Background(), req.ProviderID, req.SeasonNumber)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) MetadataFindByExternalID(req MetadataExternalIDRequest, reply *JSONReply) error {
	p, closeFn, err := s.metadata(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.FindByExternalID(context.Background(), req.IDs)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) SiteTest(req InstancePayload, _ *Empty) error {
	p, closeFn, err := s.site(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.TestConnection(context.Background())
}

func (s *rpcServer) SiteProfile(req InstancePayload, reply *JSONReply) error {
	p, closeFn, err := s.site(req)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Profile(context.Background())
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) SiteSearch(req SiteSearchRequest, reply *JSONReply) error {
	p, closeFn, err := s.site(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Search(context.Background(), req.Request)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) NotifierTest(req InstancePayload, _ *Empty) error {
	p, closeFn, err := s.notifier(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.TestConnection(context.Background())
}

func (s *rpcServer) NotifierSend(req NotifierSendRequest, _ *Empty) error {
	p, closeFn, err := s.notifier(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.Send(context.Background(), req.Message)
}

func (s *rpcServer) SubtitleSourceTest(req InstancePayload, _ *Empty) error {
	p, closeFn, err := s.subtitleSource(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return p.TestConnection(context.Background())
}

func (s *rpcServer) SubtitleSourceSearch(req SubtitleSearchRequest, reply *JSONReply) error {
	p, closeFn, err := s.subtitleSource(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Search(context.Background(), req.Request)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) SubtitleSourceDownload(req SubtitleDownloadRequest, reply *JSONReply) error {
	p, closeFn, err := s.subtitleSource(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	value, callErr := p.Download(context.Background(), req.Result)
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) ModelKind(_ Empty, reply *StringReply) error {
	p, err := s.model()
	if err != nil {
		return err
	}
	reply.Value = p.Kind()
	return nil
}

func (s *rpcServer) ModelValidate(req ModelConfigRequest, _ *Empty) error {
	p, err := s.model()
	if err != nil {
		return err
	}
	return p.ValidateModel(req.Model)
}

func (s *rpcServer) ModelGenerate(req ModelGenerateRequest, reply *JSONReply) error {
	p, err := s.model()
	if err != nil {
		return err
	}
	value, callErr := p.Generate(context.Background(), providers.ModelGenerateRequest{
		Model: req.Model, Prompt: req.Prompt, MaxTokens: req.MaxTokens, Now: restoreClock(req.Now, req.HasNow),
	})
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) ModelDownload(req ModelDownloadRequest, reply *JSONReply) error {
	p, err := s.model()
	if err != nil {
		return err
	}
	value, callErr := p.Download(context.Background(), providers.ModelDownloadRequest{
		Model: req.Model, TimeoutSeconds: req.TimeoutSeconds, Now: restoreClock(req.Now, req.HasNow),
	})
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) ModelUninstall(req ModelUninstallRequest, reply *JSONReply) error {
	p, err := s.model()
	if err != nil {
		return err
	}
	value, callErr := p.Uninstall(context.Background(), providers.ModelUninstallRequest{
		Model: req.Model, TimeoutSeconds: req.TimeoutSeconds, Now: restoreClock(req.Now, req.HasNow),
	})
	return setJSONReply(reply, value, callErr)
}

func (s *rpcServer) ModelCommandDisplay(req ModelConfigRequest, reply *StringReply) error {
	p, err := s.model()
	if err != nil {
		return err
	}
	reply.Value = p.CommandDisplay(req.Model)
	return nil
}

func setJSONReply[T any](reply *JSONReply, value T, callErr error) error {
	if callErr != nil {
		return callErr
	}
	out, err := encodeJSON(value)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) downloader(payload InstancePayload) (providers.DownloaderProvider, func(), error) {
	if s.plugin.NewDownloader == nil {
		return nil, nil, fmt.Errorf("插件未实现 DownloaderProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewDownloader(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) mediaServer(payload InstancePayload) (providers.MediaServerProvider, func(), error) {
	if s.plugin.NewMediaServer == nil {
		return nil, nil, fmt.Errorf("插件未实现 MediaServerProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewMediaServer(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) metadata(payload InstancePayload) (providers.MetadataProvider, func(), error) {
	if s.plugin.NewMetadata == nil {
		return nil, nil, fmt.Errorf("插件未实现 MetadataProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewMetadata(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) site(payload InstancePayload) (providers.SiteProvider, func(), error) {
	if s.plugin.NewSite == nil {
		return nil, nil, fmt.Errorf("插件未实现 SiteProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewSite(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) notifier(payload InstancePayload) (providers.NotifierProvider, func(), error) {
	if s.plugin.NewNotifier == nil {
		return nil, nil, fmt.Errorf("插件未实现 NotifierProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewNotifier(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) subtitleSource(payload InstancePayload) (providers.SubtitleSourceProvider, func(), error) {
	if s.plugin.NewSubtitleSource == nil {
		return nil, nil, fmt.Errorf("插件未实现 SubtitleSourceProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewSubtitleSource(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) model() (providers.ModelProvider, error) {
	if s.plugin.NewModel == nil {
		return nil, fmt.Errorf("插件未实现 ModelProvider")
	}
	provider := s.plugin.NewModel()
	if provider == nil {
		return nil, fmt.Errorf("插件返回了空 ModelProvider")
	}
	return provider, nil
}

func restoreClock(now time.Time, ok bool) func() time.Time {
	if !ok {
		return nil
	}
	return func() time.Time { return now }
}
