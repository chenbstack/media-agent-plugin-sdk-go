// Package pluginrpc implements the HashiCorp go-plugin transport used by
// third-party Go plugins.
package pluginrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os/exec"
	"time"

	hcplugin "github.com/hashicorp/go-plugin"

	"media-agent-lab/server/pkg/pluginsdk"
	"media-agent-lab/server/pkg/pluginsdk/providers"
)

const PluginName = "media-agent-provider"

var Handshake = hcplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "MEDIA_AGENT_PLUGIN",
	MagicCookieValue: "media-agent-go-plugin-v1",
}

type netRPCPlugin struct {
	impl pluginsdk.Plugin
}

func (p *netRPCPlugin) Server(b *hcplugin.MuxBroker) (interface{}, error) {
	if p.impl.Manifest.ID == "" {
		return nil, fmt.Errorf("插件实现未提供 manifest")
	}
	return &rpcServer{plugin: p.impl, broker: b}, nil
}

func (p *netRPCPlugin) Client(b *hcplugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &Client{client: c, broker: b}, nil
}

func pluginSet(impl pluginsdk.Plugin) hcplugin.PluginSet {
	return hcplugin.PluginSet{PluginName: &netRPCPlugin{impl: impl}}
}

func clientPluginSet() hcplugin.PluginSet {
	return hcplugin.PluginSet{PluginName: &netRPCPlugin{}}
}

// Serve exposes a pluginsdk.Plugin implementation as a HashiCorp go-plugin
// net/rpc plugin. Third-party Go plugins call this from main().
func Serve(impl pluginsdk.Plugin) {
	hcplugin.Serve(&hcplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         pluginSet(impl),
	})
}

type Empty struct{}

type JSONReply struct {
	Data []byte
}

type StringReply struct {
	Value string
}

type Int64Reply struct {
	Value int64
}

type BrokerReply struct {
	ID uint32
}

type InstancePayload struct {
	ID                   string
	Name                 string
	ConfigJSON           []byte
	HostServicesBrokerID uint32
}

type ConfigRequest struct {
	ConfigJSON           []byte
	HostServicesBrokerID uint32
}

type FieldOptionsRequest struct {
	Instance InstancePayload
	Field    string
}

type AuthStartRequest struct {
	Instance InstancePayload
	Flow     string
}

type AuthCheckRequest struct {
	Instance  InstancePayload
	Flow      string
	SessionID string
}

type EventRequest struct {
	Instance  InstancePayload
	EventJSON []byte
}

type StoragePathRequest struct {
	Instance InstancePayload
	Path     string
}

type StorageRenameRequest struct {
	Instance InstancePayload
	OldPath  string
	NewPath  string
}

type StorageUploadRequest struct {
	Instance             InstancePayload
	Path                 string
	UploadSourceBrokerID uint32
}

type StoragePlaybackURLRequest struct {
	Instance InstancePayload
	Input    providers.PlaybackURLInput
}

func encodeJSON(value any) (JSONReply, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return JSONReply{}, err
	}
	return JSONReply{Data: data}, nil
}

func decodeJSON(data []byte, out any) error {
	if len(data) == 0 {
		data = []byte("{}")
	}
	return json.Unmarshal(data, out)
}

func encodeConfig(config map[string]any) ([]byte, error) {
	if config == nil {
		config = map[string]any{}
	}
	return json.Marshal(config)
}

func decodeConfig(data []byte) (map[string]any, error) {
	var config map[string]any
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config == nil {
		config = map[string]any{}
	}
	return config, nil
}

type ClientConfig struct {
	Command           string
	Args              []string
	Dir               string
	Stderr            io.Writer
	Manifest          pluginsdk.Manifest
	Permissions       pluginsdk.Permissions
	ScopeType         string
	ScopeID           string
	Operation         string
	ProcessObserver   ProcessObserver
	PermissionChecker PermissionChecker
	Credentials       *ProcessCredentials
}

type Client struct {
	client            *rpc.Client
	broker            *hcplugin.MuxBroker
	manifest          pluginsdk.Manifest
	permissions       pluginsdk.Permissions
	scopeType         string
	scopeID           string
	permissionChecker PermissionChecker
}

type PermissionChecker interface {
	CheckPluginPermission(ctx context.Context, pluginID, scopeType, scopeID, permission string, manifest pluginsdk.Manifest) error
}

type ProcessStartInfo struct {
	PluginID   string
	PluginName string
	Operation  string
	ScopeType  string
	ScopeID    string
	PID        int
	Command    string
	StartedAt  time.Time
}

type ProcessObserver interface {
	PluginProcessStarted(info ProcessStartInfo) func()
}

type runningClient struct {
	process *hcplugin.Client
	client  *Client
	done    func()
}

func startClient(ctx context.Context, cfg ClientConfig) (*runningClient, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("插件入口为空")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	}
	if err := applyProcessCredentials(cmd, cfg.Credentials); err != nil {
		return nil, err
	}
	client := hcplugin.NewClient(&hcplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          clientPluginSet(),
		Cmd:              cmd,
		AllowedProtocols: []hcplugin.Protocol{hcplugin.ProtocolNetRPC},
		StartTimeout:     20 * time.Second,
		Stderr:           cfg.Stderr,
		SyncStdout:       io.Discard,
		SyncStderr:       cfg.Stderr,
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}
	raw, err := rpcClient.Dispense(PluginName)
	if err != nil {
		client.Kill()
		return nil, err
	}
	typed, ok := raw.(*Client)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("插件返回了未知客户端类型 %T", raw)
	}
	typed.manifest = cfg.Manifest
	typed.permissions = cfg.Permissions
	typed.scopeType = cfg.ScopeType
	typed.scopeID = cfg.ScopeID
	typed.permissionChecker = cfg.PermissionChecker
	var done func()
	if cfg.ProcessObserver != nil && cmd.Process != nil {
		done = cfg.ProcessObserver.PluginProcessStarted(ProcessStartInfo{
			PluginID:   cfg.Manifest.ID,
			PluginName: cfg.Manifest.Name,
			Operation:  cfg.Operation,
			ScopeType:  cfg.ScopeType,
			ScopeID:    cfg.ScopeID,
			PID:        cmd.Process.Pid,
			Command:    cfg.Command,
			StartedAt:  time.Now(),
		})
	}
	return &runningClient{process: client, client: typed, done: done}, nil
}

func (c *runningClient) Close() {
	if c == nil {
		return
	}
	if c.done != nil {
		c.done()
		c.done = nil
	}
	if c.process != nil {
		c.process.Kill()
	}
}

func withClient(ctx context.Context, cfg ClientConfig, fn func(*Client) error) error {
	running, err := startClient(ctx, cfg)
	if err != nil {
		return err
	}
	defer running.Close()
	return fn(running.client)
}

type ExternalPlugin struct {
	Manifest          pluginsdk.Manifest
	ConfigSchema      pluginsdk.ConfigSchema
	IconSVG           []byte
	Command           string
	Args              []string
	Dir               string
	Stderr            io.Writer
	PermissionChecker PermissionChecker
	ProcessObserver   ProcessObserver
	Credentials       *ProcessCredentials
}

const externalPluginAuthTimeout = 45 * time.Second

func (e ExternalPlugin) Plugin() pluginsdk.Plugin {
	out := pluginsdk.Plugin{
		Manifest:     e.Manifest,
		ConfigSchema: e.ConfigSchema,
		IconSVG:      e.IconSVG,
		ValidateConfig: func(config map[string]any) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return e.withClientOperation(ctx, "plugin.validate_config", func(c *Client) error {
				return c.ValidateConfigContext(ctx, config)
			})
		},
	}
	out.FieldOptions = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, field string) ([]pluginsdk.Option, error) {
		var options []pluginsdk.Option
		err := e.withClientOperation(ctx, "plugin.field_options", func(c *Client) error {
			got, err := c.FieldOptions(inst, secrets, field)
			if err != nil {
				return err
			}
			options = got
			return nil
		})
		return options, err
	}
	out.StartAuth = func(ctx context.Context, inst pluginsdk.Instance, flow string) (pluginsdk.AuthStartResult, error) {
		var result pluginsdk.AuthStartResult
		callCtx, cancel := contextWithTimeout(ctx, externalPluginAuthTimeout)
		defer cancel()
		err := e.withClientOperation(callCtx, "plugin.auth.start", func(c *Client) error {
			got, err := c.StartAuthContext(callCtx, inst, flow)
			if err != nil {
				return err
			}
			result = got
			return nil
		})
		return result, err
	}
	out.CheckAuth = func(ctx context.Context, inst pluginsdk.Instance, flow, sessionID string) (pluginsdk.AuthCheckResult, error) {
		var result pluginsdk.AuthCheckResult
		callCtx, cancel := contextWithTimeout(ctx, externalPluginAuthTimeout)
		defer cancel()
		err := e.withClientOperation(callCtx, "plugin.auth.check", func(c *Client) error {
			got, err := c.CheckAuthContext(callCtx, inst, flow, sessionID)
			if err != nil {
				return err
			}
			result = got
			return nil
		})
		return result, err
	}
	if out.HasCapability("storage") {
		out.NewStorage = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.StorageProvider, error) {
			return &storageProvider{external: e, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasExactCapability("cookie_source.fetch") {
		out.NewCookieSource = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.CookieSourceProvider, error) {
			return &cookieSourceProvider{external: e, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasCapability("event.subscribe") {
		out.NewEventSubscriber = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.EventSubscriber, error) {
			return &eventSubscriber{external: e, inst: inst, secrets: secrets}, nil
		}
	}
	return out
}

func contextWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, timeout)
}

func (e ExternalPlugin) withClient(ctx context.Context, fn func(*Client) error) error {
	return e.withClientOperation(ctx, "plugin.rpc", fn)
}

func (e ExternalPlugin) withClientOperation(ctx context.Context, operation string, fn func(*Client) error) error {
	return e.withClientForScopeOperation(ctx, "plugin", "global", operation, fn)
}

func (e ExternalPlugin) withClientForScope(ctx context.Context, scopeType, scopeID string, fn func(*Client) error) error {
	return e.withClientForScopeOperation(ctx, scopeType, scopeID, "plugin.rpc", fn)
}

func (e ExternalPlugin) withClientForScopeOperation(ctx context.Context, scopeType, scopeID, operation string, fn func(*Client) error) error {
	return withClient(ctx, ClientConfig{
		Command:           e.Command,
		Args:              e.Args,
		Dir:               e.Dir,
		Stderr:            e.Stderr,
		Manifest:          e.Manifest,
		Permissions:       e.Manifest.Permissions,
		ScopeType:         scopeType,
		ScopeID:           scopeID,
		Operation:         operation,
		ProcessObserver:   e.ProcessObserver,
		PermissionChecker: e.PermissionChecker,
		Credentials:       e.Credentials,
	}, fn)
}

func (e ExternalPlugin) startClient(ctx context.Context) (*runningClient, error) {
	return e.startClientForScopeOperation(ctx, "plugin", "global", "plugin.rpc")
}

func (e ExternalPlugin) startClientForScope(ctx context.Context, scopeType, scopeID string) (*runningClient, error) {
	return e.startClientForScopeOperation(ctx, scopeType, scopeID, "plugin.rpc")
}

func (e ExternalPlugin) startClientForScopeOperation(ctx context.Context, scopeType, scopeID, operation string) (*runningClient, error) {
	return startClient(ctx, ClientConfig{
		Command:           e.Command,
		Args:              e.Args,
		Dir:               e.Dir,
		Stderr:            e.Stderr,
		Manifest:          e.Manifest,
		Permissions:       e.Manifest.Permissions,
		ScopeType:         scopeType,
		ScopeID:           scopeID,
		Operation:         operation,
		ProcessObserver:   e.ProcessObserver,
		PermissionChecker: e.PermissionChecker,
		Credentials:       e.Credentials,
	})
}

func serveReader(broker *hcplugin.MuxBroker, open func() (io.ReadCloser, error)) uint32 {
	id := broker.NextId()
	go func() {
		conn, err := broker.Accept(id)
		if err != nil {
			return
		}
		defer conn.Close()
		reader, err := open()
		if err != nil {
			return
		}
		defer reader.Close()
		_, _ = io.Copy(conn, reader)
	}()
	return id
}

func serveWriter(broker *hcplugin.MuxBroker, open func() (io.WriteCloser, error)) uint32 {
	id := broker.NextId()
	go func() {
		conn, err := broker.Accept(id)
		if err != nil {
			return
		}
		defer conn.Close()
		writer, err := open()
		if err != nil {
			return
		}
		_, copyErr := io.Copy(writer, conn)
		closeErr := writer.Close()
		_ = copyErr
		_ = closeErr
	}()
	return id
}

type closeReadConn struct {
	net.Conn
}

func (c closeReadConn) Close() error {
	return c.Conn.Close()
}
