package pluginrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/rpc"

	hcplugin "github.com/hashicorp/go-plugin"

	"media-agent-lab/server/pkg/pluginsdk"
	"media-agent-lab/server/pkg/pluginsdk/providers"
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
	return provider.TestConnection(context.Background())
}

func (s *rpcServer) StorageInfo(req InstancePayload, reply *JSONReply) error {
	provider, closeFn, err := s.storage(req)
	if err != nil {
		return err
	}
	defer closeFn()
	info, err := provider.Info(context.Background())
	if err != nil {
		return err
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
	return provider.EnsureMounted(context.Background())
}

func (s *rpcServer) StorageUnmount(req InstancePayload, reply *Empty) error {
	provider, closeFn, err := s.storage(req)
	if err != nil {
		return err
	}
	defer closeFn()
	return provider.Unmount(context.Background())
}

func (s *rpcServer) StorageStat(req StoragePathRequest, reply *JSONReply) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	info, err := provider.Stat(context.Background(), req.Path)
	if err != nil {
		return err
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
		return err
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
	return provider.MkdirAll(context.Background(), req.Path)
}

func (s *rpcServer) StorageRemove(req StoragePathRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return provider.Remove(context.Background(), req.Path)
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

func (s *rpcServer) StorageRename(req StorageRenameRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return provider.Rename(context.Background(), req.OldPath, req.NewPath)
}

func (s *rpcServer) StorageLink(req StorageRenameRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return provider.Link(context.Background(), req.OldPath, req.NewPath)
}

func (s *rpcServer) StorageSymlink(req StorageRenameRequest, reply *Empty) error {
	provider, closeFn, err := s.fileStorage(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	return provider.Symlink(context.Background(), req.OldPath, req.NewPath)
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
	return copyProvider.Copy(context.Background(), req.OldPath, req.NewPath)
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
	return uploadProvider.Upload(context.Background(), req.Path, source)
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
		return err
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
