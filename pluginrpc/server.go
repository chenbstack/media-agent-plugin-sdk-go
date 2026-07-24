package pluginrpc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/rpc"
	"os"

	hcplugin "github.com/hashicorp/go-plugin"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
	runtimesdk "github.com/chenbstack/media-agent-plugin-sdk-go/runtime"
)

type rpcServer struct {
	plugin pluginsdk.Plugin
	broker *hcplugin.MuxBroker
}

func (s *rpcServer) Manifest(args Empty, reply *JSONReply) error {
	out, err := encodeJSON(s.plugin.Manifest)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) ConfigSchema(args Empty, reply *JSONReply) error {
	out, err := encodeJSON(s.plugin.ConfigSchema)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) Install(req InstallRequest, reply *JSONReply) error {
	hooks, ok := s.plugin.InstallHooks(req.Component)
	if !ok || hooks.Install == nil {
		return fmt.Errorf("插件未实现组件 %q 的安装步骤", req.Component)
	}
	// 进度写到插件进程自身的 stderr；go-plugin 经 SyncStderr 实时转发给宿主，
	// 宿主再喂给前端展示。这样单次阻塞 RPC 也能呈现实时进度。
	result, err := hooks.Install(context.Background(), os.Stderr)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) CheckInstall(req InstallRequest, reply *JSONReply) error {
	hooks, ok := s.plugin.InstallHooks(req.Component)
	if !ok || hooks.CheckInstall == nil {
		return fmt.Errorf("插件未实现组件 %q 的安装检查", req.Component)
	}
	result, err := hooks.CheckInstall(context.Background())
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) Uninstall(req InstallRequest, reply *JSONReply) error {
	hooks, ok := s.plugin.InstallHooks(req.Component)
	if !ok || hooks.Uninstall == nil {
		return fmt.Errorf("插件未实现组件 %q 的资源卸载", req.Component)
	}
	// 卸载进度同安装：写插件进程 stderr，go-plugin 实时转发给宿主。
	result, err := hooks.Uninstall(context.Background(), os.Stderr)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) ValidateConfig(req ConfigRequest, reply *Empty) error {
	if s.plugin.ValidateConfig == nil {
		return nil
	}
	config, err := decodeConfig(req.ConfigJSON)
	if err != nil {
		return err
	}
	return s.plugin.ValidateConfig(config)
}

func (s *rpcServer) FieldOptions(req FieldOptionsRequest, reply *JSONReply) error {
	if s.plugin.FieldOptions == nil {
		return fmt.Errorf("插件未实现动态选项")
	}
	inst, secrets, closeFn, err := s.instance(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	options, err := s.plugin.FieldOptions(context.Background(), inst, secrets, req.Field)
	if err != nil {
		return err
	}
	out, err := encodeJSON(options)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) StartAuth(req AuthStartRequest, reply *JSONReply) error {
	if s.plugin.StartAuth == nil {
		return fmt.Errorf("插件未实现认证流程")
	}
	inst, _, closeFn, err := s.instance(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	result, err := s.plugin.StartAuth(context.Background(), inst, req.Flow)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) CheckAuth(req AuthCheckRequest, reply *JSONReply) error {
	if s.plugin.CheckAuth == nil {
		return fmt.Errorf("插件未实现认证流程")
	}
	inst, _, closeFn, err := s.instance(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	result, err := s.plugin.CheckAuth(context.Background(), inst, req.Flow, req.SessionID)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) HandleEvent(req EventRequest, reply *Empty) error {
	if s.plugin.NewEventSubscriber == nil {
		return fmt.Errorf("插件未实现事件订阅")
	}
	inst, secrets, closeFn, err := s.instance(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	var event pluginsdk.EventEnvelope
	if err := json.Unmarshal(req.EventJSON, &event); err != nil {
		return err
	}
	subscriber, err := s.plugin.NewEventSubscriber(context.Background(), inst, secrets)
	if err != nil {
		return err
	}
	return subscriber.HandleEvent(context.Background(), event)
}

func (s *rpcServer) RunAction(req ActionRunRequest, reply *JSONReply) error {
	if s.plugin.NewActionHandler == nil {
		return fmt.Errorf("插件未实现 ActionHandler")
	}
	inst, secrets, closeFn, err := s.instance(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	input := map[string]any{}
	if len(req.InputJSON) > 0 {
		if err := json.Unmarshal(req.InputJSON, &input); err != nil {
			return err
		}
	}
	handler, err := s.plugin.NewActionHandler(context.Background(), inst, secrets)
	if err != nil {
		return err
	}
	result, err := handler.RunAction(context.Background(), req.ActionID, input)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) RunScheduledTask(req ScheduledTaskRunRequest, reply *JSONReply) error {
	if s.plugin.NewScheduledTaskHandler == nil {
		return fmt.Errorf("插件未实现 ScheduledTaskHandler")
	}
	inst, secrets, closeFn, err := s.instance(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	var request pluginsdk.ScheduledTaskRequest
	if err := json.Unmarshal(req.RequestJSON, &request); err != nil {
		return err
	}
	handler, err := s.plugin.NewScheduledTaskHandler(context.Background(), inst, secrets)
	if err != nil {
		return err
	}
	result, err := handler.RunScheduledTask(context.Background(), request)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) AssessOnboarding(req InstancePayload, reply *JSONReply) error {
	if s.plugin.AssessOnboarding == nil {
		return fmt.Errorf("插件未实现引导状态评估")
	}
	inst, secrets, closeFn, err := s.instance(req)
	if err != nil {
		return err
	}
	defer closeFn()
	result, err := s.plugin.AssessOnboarding(context.Background(), inst, secrets)
	if err != nil {
		return err
	}
	if err := result.Validate(); err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) RendererTest(req InstancePayload, reply *Empty) error {
	provider, closeFn, err := s.renderer(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return provider.TestConnection(context.Background())
}

func (s *rpcServer) RendererRender(req RendererRenderRequest, reply *JSONReply) error {
	provider, closeFn, err := s.renderer(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	result, err := provider.Render(context.Background(), req.Request)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) CookieSourceTest(req InstancePayload, reply *Empty) error {
	provider, closeFn, err := s.cookieSource(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return provider.TestConnection(context.Background())
}

func (s *rpcServer) CookieSourceSnapshot(req InstancePayload, reply *JSONReply) error {
	provider, closeFn, err := s.cookieSource(req)
	if err != nil {
		return err
	}
	defer closeFn()
	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		return err
	}
	out, err := encodeJSON(snapshot)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) StorageKind(req InstancePayload, reply *StringReply) error {
	provider, closeFn, err := s.storage(req)
	if err != nil {
		return err
	}
	defer closeFn()
	reply.Value = provider.Kind()
	return nil
}

func (s *rpcServer) StorageTest(req InstancePayload, reply *Empty) error {
	provider, closeFn, err := s.storage(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.TestConnection(context.Background()))
}

func (s *rpcServer) StorageInfo(req InstancePayload, reply *JSONReply) error {
	provider, closeFn, err := s.storage(req)
	if err != nil {
		return err
	}
	defer closeFn()
	info, err := provider.Info(context.Background())
	if err != nil {
		return encodeRPCError(err)
	}
	out, err := encodeJSON(info)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) StorageEnsureMounted(req InstancePayload, reply *Empty) error {
	provider, closeFn, err := s.storage(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.EnsureMounted(context.Background()))
}

func (s *rpcServer) StorageUnmount(req InstancePayload, reply *Empty) error {
	provider, closeFn, err := s.storage(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.Unmount(context.Background()))
}

func (s *rpcServer) StorageStat(req StoragePathRequest, reply *JSONReply) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	info, err := provider.Stat(context.Background(), req.Path)
	if err != nil {
		return encodeRPCError(err)
	}
	out, err := encodeJSON(info)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) StorageListDir(req StoragePathRequest, reply *JSONReply) error {
	provider, closeFn, err := s.storage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	lister, ok := provider.(providers.StorageDirectoryLister)
	if !ok {
		return fmt.Errorf("插件未实现目录浏览")
	}
	entries, err := lister.ListDir(context.Background(), req.Path)
	if err != nil {
		return encodeRPCError(err)
	}
	out, err := encodeJSON(entries)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) StorageMkdirAll(req StoragePathRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.MkdirAll(context.Background(), req.Path))
}

func (s *rpcServer) StorageRemove(req StoragePathRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.Remove(context.Background(), req.Path))
}

func (s *rpcServer) StorageOpenReader(req StoragePathRequest, reply *BrokerReply) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	id := serveReader(s.broker, func() (io.ReadCloser, error) {
		reader, err := provider.OpenReader(context.Background(), req.Path)
		closeFn()
		return reader, err
	})
	reply.ID = id
	return nil
}

func (s *rpcServer) StorageOpenWriter(req StoragePathRequest, reply *BrokerReply) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	id := serveWriter(s.broker, func() (io.WriteCloser, error) {
		writer, err := provider.OpenWriter(context.Background(), req.Path)
		closeFn()
		return writer, err
	})
	reply.ID = id
	return nil
}

func (s *rpcServer) StorageOpenRangeReader(req StorageRangeRequest, reply *BrokerReply) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	ranged, ok := provider.(providers.RangeReadProvider)
	if !ok {
		closeFn()
		return fmt.Errorf("插件未实现分段读取")
	}
	id := serveReader(s.broker, func() (io.ReadCloser, error) {
		reader, err := ranged.OpenRangeReader(context.Background(), req.Path, req.Offset, req.Length)
		closeFn()
		return reader, err
	})
	reply.ID = id
	return nil
}

func (s *rpcServer) StorageOpenRangeWriter(req StorageRangeRequest, reply *BrokerReply) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	ranged, ok := provider.(providers.RangeWriteProvider)
	if !ok {
		closeFn()
		return fmt.Errorf("插件未实现分段写入")
	}
	id := serveWriter(s.broker, func() (io.WriteCloser, error) {
		writer, err := ranged.OpenRangeWriter(context.Background(), req.Path, req.Offset)
		closeFn()
		return writer, err
	})
	reply.ID = id
	return nil
}

func (s *rpcServer) StorageTruncate(req StorageTruncateRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	ranged, ok := provider.(providers.RangeWriteProvider)
	if !ok {
		return fmt.Errorf("插件未实现分段写入")
	}
	return encodeRPCError(ranged.Truncate(context.Background(), req.Path, req.Size))
}

// StorageCopyBetween 在同一插件进程内的两个存储实例之间复制文件，数据不回宿主。
// 两侧都支持分段读写且文件够大时自动分段并行，否则退化为进程内流式复制。
func (s *rpcServer) StorageCopyBetween(req StorageCopyBetweenRequest, reply *Empty) error {
	source, sourceClose, err := s.fileStorage(req.Source)
	if err != nil {
		return err
	}
	defer sourceClose()
	target, targetClose, err := s.fileStorage(req.Target)
	if err != nil {
		return err
	}
	defer targetClose()

	var progress providers.ProgressFunc
	if req.ProgressBrokerID != 0 {
		conn, dialErr := s.broker.Dial(req.ProgressBrokerID)
		if dialErr == nil {
			defer conn.Close()
			progress = func(copied int64) {
				var buf [8]byte
				binary.BigEndian.PutUint64(buf[:], uint64(copied))
				_, _ = conn.Write(buf[:])
			}
		}
	}
	return encodeRPCError(copyBetweenProviders(context.Background(), source, req.SourcePath, target, req.TargetPath, progress))
}

func copyBetweenProviders(ctx context.Context, source providers.FileStorageProvider, sourcePath string, target providers.FileStorageProvider, targetPath string, progress providers.ProgressFunc) error {
	info, err := source.Stat(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("读取源文件信息: %w", err)
	}
	rangeSource, canRangeRead := source.(providers.RangeReadProvider)
	rangeTarget, canRangeWrite := target.(providers.RangeWriteProvider)
	if canRangeRead && canRangeWrite {
		return providers.RangeCopy(ctx, rangeSource, sourcePath, info.Size, rangeTarget, targetPath, providers.RangeCopyOptions{Progress: progress})
	}
	reader, err := source.OpenReader(ctx, sourcePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := target.OpenWriter(ctx, targetPath)
	if err != nil {
		return err
	}
	if _, err := providers.StreamCopy(ctx, writer, reader, providers.RangeCopyOptions{Progress: progress}); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func (s *rpcServer) StorageRename(req StorageRenameRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.Rename(context.Background(), req.OldPath, req.NewPath))
}

func (s *rpcServer) StorageLink(req StorageRenameRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.Link(context.Background(), req.OldPath, req.NewPath))
}

func (s *rpcServer) StorageSymlink(req StorageRenameRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return encodeRPCError(provider.Symlink(context.Background(), req.OldPath, req.NewPath))
}

func (s *rpcServer) StorageCopy(req StorageRenameRequest, reply *Empty) error {
	provider, closeFn, err := s.storage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	copyProvider, ok := provider.(providers.ServerSideCopyProvider)
	if !ok {
		return fmt.Errorf("插件未实现服务端复制")
	}
	return encodeRPCError(copyProvider.Copy(context.Background(), req.OldPath, req.NewPath))
}

func (s *rpcServer) StorageUpload(req StorageUploadRequest, reply *Empty) error {
	provider, closeFn, err := s.storage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	uploadProvider, ok := provider.(providers.UploadProvider)
	if !ok {
		return fmt.Errorf("插件未实现 UploadProvider")
	}
	if req.UploadSourceBrokerID == 0 {
		return fmt.Errorf("缺少上传源 broker")
	}
	conn, err := s.broker.Dial(req.UploadSourceBrokerID)
	if err != nil {
		return err
	}
	defer conn.Close()
	source := &remoteUploadSource{client: rpc.NewClient(conn), broker: s.broker}
	defer source.client.Close()
	return encodeRPCError(uploadProvider.Upload(context.Background(), req.Path, source))
}

func (s *rpcServer) StorageResolvePlaybackURL(req StoragePlaybackURLRequest, reply *JSONReply) error {
	provider, closeFn, err := s.storage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	playbackProvider, ok := provider.(providers.PlaybackURLProvider)
	if !ok {
		return fmt.Errorf("插件未实现播放 URL")
	}
	result, err := playbackProvider.ResolvePlaybackURL(context.Background(), req.Input)
	if err != nil {
		return encodeRPCError(err)
	}
	out, err := encodeJSON(result)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) instance(payload InstancePayload) (pluginsdk.Instance, pluginsdk.SecretResolver, func(), error) {
	config, err := decodeConfig(payload.ConfigJSON)
	if err != nil {
		return pluginsdk.Instance{}, nil, nil, err
	}
	inst := pluginsdk.Instance{ID: payload.ID, Name: payload.Name, Config: config, Logger: pluginsdk.NoopLogger()}
	var services *hostServicesClient
	if payload.HostServicesBrokerID != 0 {
		conn, err := s.broker.Dial(payload.HostServicesBrokerID)
		if err != nil {
			return pluginsdk.Instance{}, nil, nil, err
		}
		services = &hostServicesClient{client: rpc.NewClient(conn)}
		inst.KV = services
		inst.DB = services
		inst.Logger = services
		inst.Runtime = &runtimesdk.Services{Feedback: &runtimeFeedbackClient{host: services}}
		inst.SiteAccounts = services
		inst.Subscriptions = services
		inst.Downloads = services
		inst.Transfers = services
		inst.Rules = services
		inst.Connections = services
		inst.Storages = services
		inst.Schedules = services
		inst.Settings = services
		inst.PluginServices = services
	}
	closeFn := func() {}
	if services != nil {
		closeFn = func() { _ = services.Close() }
	}
	return inst, services, closeFn, nil
}

func (s *rpcServer) storage(payload InstancePayload) (providers.StorageProvider, func(), error) {
	if s.plugin.NewStorage == nil {
		return nil, nil, fmt.Errorf("插件未实现 StorageProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewStorage(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) renderer(payload InstancePayload) (providers.RendererProvider, func(), error) {
	if s.plugin.NewRenderer == nil {
		return nil, nil, fmt.Errorf("插件未实现 RendererProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewRenderer(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) cookieSource(payload InstancePayload) (providers.CookieSourceProvider, func(), error) {
	if s.plugin.NewCookieSource == nil {
		return nil, nil, fmt.Errorf("插件未实现 CookieSourceProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewCookieSource(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) fileStorage(payload InstancePayload) (providers.FileStorageProvider, func(), error) {
	provider, closeFn, err := s.storage(payload)
	if err != nil {
		return nil, nil, err
	}
	fileProvider, ok := provider.(providers.FileStorageProvider)
	if !ok {
		closeFn()
		return nil, nil, fmt.Errorf("插件未实现 FileStorageProvider")
	}
	return fileProvider, closeFn, nil
}
