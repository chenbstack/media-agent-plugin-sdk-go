package pluginrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"media-agent-lab/server/pkg/pluginsdk"
	"media-agent-lab/server/pkg/pluginsdk/providers"
)

func (c *Client) Manifest() (pluginsdk.Manifest, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.Manifest", Empty{}, &reply); err != nil {
		return pluginsdk.Manifest{}, err
	}
	var out pluginsdk.Manifest
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.Manifest{}, err
	}
	return out, nil
}

func (c *Client) ConfigSchema() (pluginsdk.ConfigSchema, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.ConfigSchema", Empty{}, &reply); err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	var out pluginsdk.ConfigSchema
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	return out, nil
}

func (c *Client) ValidateConfig(config map[string]any) error {
	return c.ValidateConfigContext(context.Background(), config)
}

func (c *Client) ValidateConfigContext(ctx context.Context, config map[string]any) error {
	configJSON, err := encodeConfig(config)
	if err != nil {
		return err
	}
	var reply Empty
	return c.call(ctx, "Plugin.ValidateConfig", ConfigRequest{ConfigJSON: configJSON}, &reply)
}

func (c *Client) FieldOptions(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, field string) ([]pluginsdk.Option, error) {
	payload, err := c.instancePayload(context.Background(), inst, secrets)
	if err != nil {
		return nil, err
	}
	var reply JSONReply
	if err := c.client.Call("Plugin.FieldOptions", FieldOptionsRequest{Instance: payload, Field: field}, &reply); err != nil {
		return nil, err
	}
	var out []pluginsdk.Option
	if err := decodeJSON(reply.Data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) StartAuth(inst pluginsdk.Instance, flow string) (pluginsdk.AuthStartResult, error) {
	return c.StartAuthContext(context.Background(), inst, flow)
}

func (c *Client) StartAuthContext(ctx context.Context, inst pluginsdk.Instance, flow string) (pluginsdk.AuthStartResult, error) {
	payload, err := c.instancePayload(ctx, inst, nil)
	if err != nil {
		return pluginsdk.AuthStartResult{}, err
	}
	var reply JSONReply
	if err := c.call(ctx, "Plugin.StartAuth", AuthStartRequest{Instance: payload, Flow: flow}, &reply); err != nil {
		return pluginsdk.AuthStartResult{}, err
	}
	var out pluginsdk.AuthStartResult
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.AuthStartResult{}, err
	}
	return out, nil
}

func (c *Client) CheckAuth(inst pluginsdk.Instance, flow, sessionID string) (pluginsdk.AuthCheckResult, error) {
	return c.CheckAuthContext(context.Background(), inst, flow, sessionID)
}

func (c *Client) CheckAuthContext(ctx context.Context, inst pluginsdk.Instance, flow, sessionID string) (pluginsdk.AuthCheckResult, error) {
	payload, err := c.instancePayload(ctx, inst, nil)
	if err != nil {
		return pluginsdk.AuthCheckResult{}, err
	}
	var reply JSONReply
	if err := c.call(ctx, "Plugin.CheckAuth", AuthCheckRequest{Instance: payload, Flow: flow, SessionID: sessionID}, &reply); err != nil {
		return pluginsdk.AuthCheckResult{}, err
	}
	var out pluginsdk.AuthCheckResult
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.AuthCheckResult{}, err
	}
	return out, nil
}

func (c *Client) HandleEventContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, event pluginsdk.EventEnvelope) error {
	payload, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return err
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var reply Empty
	return c.call(ctx, "Plugin.HandleEvent", EventRequest{Instance: payload, EventJSON: eventJSON}, &reply)
}

func (c *Client) CookieSourceTestContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) error {
	payload, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return err
	}
	var reply Empty
	return c.call(ctx, "Plugin.CookieSourceTest", payload, &reply)
}

func (c *Client) CookieSourceSnapshotContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.CookieSnapshot, error) {
	payload, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return providers.CookieSnapshot{}, err
	}
	var reply JSONReply
	if err := c.call(ctx, "Plugin.CookieSourceSnapshot", payload, &reply); err != nil {
		return providers.CookieSnapshot{}, err
	}
	var out providers.CookieSnapshot
	if err := decodeJSON(reply.Data, &out); err != nil {
		return providers.CookieSnapshot{}, err
	}
	return out, nil
}

func (c *Client) call(ctx context.Context, serviceMethod string, args any, reply any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	call := c.client.Go(serviceMethod, args, reply, nil)
	select {
	case done := <-call.Done:
		return done.Error
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", serviceMethod, ctx.Err())
	}
}

func (c *Client) instancePayload(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (InstancePayload, error) {
	configJSON, err := encodeConfig(inst.Config)
	if err != nil {
		return InstancePayload{}, err
	}
	payload := InstancePayload{
		ID:         inst.ID,
		Name:       inst.Name,
		ConfigJSON: configJSON,
	}
	if secrets != nil || inst.KV != nil || inst.DB != nil || inst.Logger != nil {
		id := c.broker.NextId()
		payload.HostServicesBrokerID = id
		go c.broker.AcceptAndServe(id, &hostServicesServer{
			ctx:               ctx,
			pluginID:          c.manifest.ID,
			scopeType:         c.scopeType,
			scopeID:           c.scopeID,
			manifest:          c.manifest,
			permissions:       c.permissions,
			permissionChecker: c.permissionChecker,
			secrets:           secrets,
			kv:                inst.KV,
			db:                inst.DB,
			logger:            inst.Logger,
		})
	}
	return payload, nil
}

type storageProvider struct {
	external ExternalPlugin
	inst     pluginsdk.Instance
	secrets  pluginsdk.SecretResolver
}

type cookieSourceProvider struct {
	external ExternalPlugin
	inst     pluginsdk.Instance
	secrets  pluginsdk.SecretResolver
}

type eventSubscriber struct {
	external ExternalPlugin
	inst     pluginsdk.Instance
	secrets  pluginsdk.SecretResolver
}

func (s *eventSubscriber) HandleEvent(ctx context.Context, event pluginsdk.EventEnvelope) error {
	return s.external.withClientOperation(ctx, "plugin.event.handle", func(c *Client) error {
		return c.HandleEventContext(ctx, s.inst, s.secrets, event)
	})
}

func (p *cookieSourceProvider) Kind() string {
	return p.external.Manifest.ID
}

func (p *cookieSourceProvider) TestConnection(ctx context.Context) error {
	return p.external.withClientOperation(ctx, "cookie_source.test", func(c *Client) error {
		return c.CookieSourceTestContext(ctx, p.inst, p.secrets)
	})
}

func (p *cookieSourceProvider) Snapshot(ctx context.Context) (providers.CookieSnapshot, error) {
	var out providers.CookieSnapshot
	err := p.external.withClientOperation(ctx, "cookie_source.fetch", func(c *Client) error {
		got, err := c.CookieSourceSnapshotContext(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	return out, err
}

func (p *storageProvider) Kind() string {
	return p.external.Manifest.ID
}

func (p *storageProvider) TestConnection(ctx context.Context) error {
	return p.withClientOperation(ctx, "storage.test", func(c *Client) error {
		payload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply Empty
		return c.client.Call("Plugin.StorageTest", payload, &reply)
	})
}

func (p *storageProvider) Info(ctx context.Context) (providers.StorageInfo, error) {
	var out providers.StorageInfo
	err := p.withClientOperation(ctx, "storage.info", func(c *Client) error {
		payload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.client.Call("Plugin.StorageInfo", payload, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}

func (p *storageProvider) EnsureMounted(ctx context.Context) error {
	return p.withClientOperation(ctx, "storage.ensure_mounted", func(c *Client) error {
		payload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply Empty
		return c.client.Call("Plugin.StorageEnsureMounted", payload, &reply)
	})
}

func (p *storageProvider) Unmount(ctx context.Context) error {
	return p.withClientOperation(ctx, "storage.unmount", func(c *Client) error {
		payload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply Empty
		return c.client.Call("Plugin.StorageUnmount", payload, &reply)
	})
}

func (p *storageProvider) Stat(ctx context.Context, name string) (providers.StorageFileInfo, error) {
	var out providers.StorageFileInfo
	err := p.withClientOperation(ctx, "storage.stat", func(c *Client) error {
		req, err := c.pathRequest(ctx, p.inst, p.secrets, name)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.client.Call("Plugin.StorageStat", req, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}

func (p *storageProvider) ListDir(ctx context.Context, path string) ([]providers.StorageFileInfo, error) {
	var out []providers.StorageFileInfo
	err := p.withClientOperation(ctx, "storage.list_dir", func(c *Client) error {
		req, err := c.pathRequest(ctx, p.inst, p.secrets, path)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.client.Call("Plugin.StorageListDir", req, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}

func (p *storageProvider) MkdirAll(ctx context.Context, path string) error {
	return p.callPath(ctx, "storage.mkdir_all", "Plugin.StorageMkdirAll", path)
}

func (p *storageProvider) Remove(ctx context.Context, name string) error {
	return p.callPath(ctx, "storage.remove", "Plugin.StorageRemove", name)
}

func (p *storageProvider) OpenReader(ctx context.Context, name string) (io.ReadCloser, error) {
	running, err := p.startClientOperation(ctx, "storage.open_reader")
	if err != nil {
		return nil, err
	}
	req, err := running.client.pathRequest(ctx, p.inst, p.secrets, name)
	if err != nil {
		running.Close()
		return nil, err
	}
	var reply BrokerReply
	if err := running.client.client.Call("Plugin.StorageOpenReader", req, &reply); err != nil {
		running.Close()
		return nil, err
	}
	conn, err := running.client.broker.Dial(reply.ID)
	if err != nil {
		running.Close()
		return nil, err
	}
	return pluginClientReadCloser{ReadCloser: closeReadConn{Conn: conn}, closeClient: running.Close}, nil
}

func (p *storageProvider) OpenWriter(ctx context.Context, name string) (io.WriteCloser, error) {
	running, err := p.startClientOperation(ctx, "storage.open_writer")
	if err != nil {
		return nil, err
	}
	req, err := running.client.pathRequest(ctx, p.inst, p.secrets, name)
	if err != nil {
		running.Close()
		return nil, err
	}
	var reply BrokerReply
	if err := running.client.client.Call("Plugin.StorageOpenWriter", req, &reply); err != nil {
		running.Close()
		return nil, err
	}
	conn, err := running.client.broker.Dial(reply.ID)
	if err != nil {
		running.Close()
		return nil, err
	}
	return pluginClientWriteCloser{WriteCloser: conn, closeClient: running.Close}, nil
}

func (p *storageProvider) Rename(ctx context.Context, oldpath, newpath string) error {
	return p.callRename(ctx, "storage.rename", "Plugin.StorageRename", oldpath, newpath)
}

func (p *storageProvider) Link(ctx context.Context, oldname, newname string) error {
	return p.callRename(ctx, "storage.link", "Plugin.StorageLink", oldname, newname)
}

func (p *storageProvider) Symlink(ctx context.Context, oldname, newname string) error {
	return p.callRename(ctx, "storage.symlink", "Plugin.StorageSymlink", oldname, newname)
}

func (p *storageProvider) Copy(ctx context.Context, oldname, newname string) error {
	return p.callRename(ctx, "storage.copy", "Plugin.StorageCopy", oldname, newname)
}

func (p *storageProvider) Upload(ctx context.Context, name string, source providers.UploadSource) error {
	return p.withClientOperation(ctx, "storage.upload", func(c *Client) error {
		payload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		sourceID := c.broker.NextId()
		go c.broker.AcceptAndServe(sourceID, &uploadSourceServer{ctx: ctx, source: source, broker: c.broker})
		var reply Empty
		return c.client.Call("Plugin.StorageUpload", StorageUploadRequest{
			Instance:             payload,
			Path:                 name,
			UploadSourceBrokerID: sourceID,
		}, &reply)
	})
}

func (p *storageProvider) ResolvePlaybackURL(ctx context.Context, input providers.PlaybackURLInput) (providers.PlaybackURLResult, error) {
	var out providers.PlaybackURLResult
	err := p.withClientOperation(ctx, "storage.playback_url", func(c *Client) error {
		payload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.client.Call("Plugin.StorageResolvePlaybackURL", StoragePlaybackURLRequest{
			Instance: payload,
			Input:    input,
		}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}

func (p *storageProvider) callPath(ctx context.Context, operation, method, path string) error {
	return p.withClientOperation(ctx, operation, func(c *Client) error {
		req, err := c.pathRequest(ctx, p.inst, p.secrets, path)
		if err != nil {
			return err
		}
		var reply Empty
		return c.client.Call(method, req, &reply)
	})
}

func (p *storageProvider) callRename(ctx context.Context, operation, method, oldpath, newpath string) error {
	return p.withClientOperation(ctx, operation, func(c *Client) error {
		payload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply Empty
		return c.client.Call(method, StorageRenameRequest{Instance: payload, OldPath: oldpath, NewPath: newpath}, &reply)
	})
}

func (p *storageProvider) withClient(ctx context.Context, fn func(*Client) error) error {
	return p.withClientOperation(ctx, "storage.rpc", fn)
}

func (p *storageProvider) withClientOperation(ctx context.Context, operation string, fn func(*Client) error) error {
	scopeType, scopeID := p.scope()
	return p.external.withClientForScopeOperation(ctx, scopeType, scopeID, operation, fn)
}

func (p *storageProvider) startClient(ctx context.Context) (*runningClient, error) {
	return p.startClientOperation(ctx, "storage.rpc")
}

func (p *storageProvider) startClientOperation(ctx context.Context, operation string) (*runningClient, error) {
	scopeType, scopeID := p.scope()
	return p.external.startClientForScopeOperation(ctx, scopeType, scopeID, operation)
}

func (p *storageProvider) scope() (string, string) {
	if p.inst.ID == "" {
		return "plugin", "global"
	}
	return "storage", p.inst.ID
}

func (c *Client) pathRequest(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, path string) (StoragePathRequest, error) {
	payload, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return StoragePathRequest{}, err
	}
	return StoragePathRequest{Instance: payload, Path: path}, nil
}

type pluginClientReadCloser struct {
	io.ReadCloser
	closeClient func()
}

func (c pluginClientReadCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.closeClient != nil {
		c.closeClient()
	}
	return err
}

type pluginClientWriteCloser struct {
	io.WriteCloser
	closeClient func()
}

func (c pluginClientWriteCloser) Close() error {
	err := c.WriteCloser.Close()
	if c.closeClient != nil {
		c.closeClient()
	}
	return err
}
