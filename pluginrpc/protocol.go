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
	"os"
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

// InstallRequest 指定要安装/检查/卸载的组件；Component 为空串表示默认组件。
type InstallRequest struct {
	Component string
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

type RendererRenderRequest struct {
	Instance InstancePayload
	Request  providers.RenderRequest
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
	// Env 是本次操作追加到子进程环境的额外变量（key=value）；空则仅继承宿主环境。
	Env               []string
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
	// 默认继承宿主进程环境；cfg.Env 为该操作追加的额外变量（如引擎下载代理）。
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
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

// OperationInstall 是自举安装（下载引擎等资源）的操作名。宿主可据此只对安装子进程
// 注入下载代理等环境变量，避免影响渲染等其他操作的子进程。
const OperationInstall = "plugin.install"

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
	// Env 按操作名返回追加到子进程环境的额外变量（key=value）。可为 nil。
	// 宿主用它把全局网络代理等注入到特定操作（如 OperationInstall 的引擎下载）。
	Env func(operation string) []string
}

// envFor 返回某操作要追加的环境变量；Env 未设置时为 nil。
func (e ExternalPlugin) envFor(operation string) []string {
	if e.Env == nil {
		return nil
	}
	return e.Env(operation)
}

const externalPluginAuthTimeout = 45 * time.Second

// externalPluginInstallTimeout 给插件自举安装留足时间：安装可能下载较大的
// 引擎二进制（浏览器仿真插件下载 Lightpanda/Obscura 数十 MB）。
const externalPluginInstallTimeout = 10 * time.Minute

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
	if out.HasCapability("renderer") {
		out.NewRenderer = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.RendererProvider, error) {
			return &rendererProvider{external: e, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasExactCapability("lifecycle.install") {
		// 默认组件（id 为空串）→ out.Install/CheckInstall/Uninstall；manifest 声明的其余
		// 组件 → out.InstallComponents。转发时把组件 id 透传给插件进程，由其按 id 路由。
		def := e.installForwarders("")
		out.Install = def.Install
		out.CheckInstall = def.CheckInstall
		out.Uninstall = def.Uninstall
		for _, comp := range e.Manifest.InstallComponents() {
			if comp.ID == "" {
				continue
			}
			out.InstallComponents = append(out.InstallComponents, e.installForwarders(comp.ID))
		}
	}
	return out
}

// installForwarders 构造某个组件的安装/检查/卸载 RPC 转发闭包，component 透传给插件进程。
func (e ExternalPlugin) installForwarders(component string) pluginsdk.InstallComponent {
	return pluginsdk.InstallComponent{
		ID: component,
		Install: func(ctx context.Context, progress io.Writer) (pluginsdk.InstallResult, error) {
			var result pluginsdk.InstallResult
			callCtx, cancel := contextWithTimeout(ctx, externalPluginInstallTimeout)
			defer cancel()
			// 把 progress 叠加到该次安装进程的 stderr 上：插件进程写 stderr 的进度行经
			// go-plugin SyncStderr 实时流回 progress，宿主据此向前端展示实时进度。
			err := e.withClientOperationStderr(callCtx, "plugin.install", progress, func(c *Client) error {
				got, err := c.InstallContext(callCtx, component)
				if err != nil {
					return err
				}
				result = got
				return nil
			})
			return result, err
		},
		CheckInstall: func(ctx context.Context) (pluginsdk.InstallResult, error) {
			var result pluginsdk.InstallResult
			callCtx, cancel := contextWithTimeout(ctx, externalPluginAuthTimeout)
			defer cancel()
			err := e.withClientOperation(callCtx, "plugin.check_install", func(c *Client) error {
				got, err := c.CheckInstallContext(callCtx, component)
				if err != nil {
					return err
				}
				result = got
				return nil
			})
			return result, err
		},
		Uninstall: func(ctx context.Context, progress io.Writer) (pluginsdk.UninstallResult, error) {
			var result pluginsdk.UninstallResult
			callCtx, cancel := contextWithTimeout(ctx, externalPluginInstallTimeout)
			defer cancel()
			err := e.withClientOperationStderr(callCtx, "plugin.uninstall", progress, func(c *Client) error {
				got, err := c.UninstallContext(callCtx, component)
				if err != nil {
					return err
				}
				result = got
				return nil
			})
			return result, err
		},
	}
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

// withClientOperationStderr 同 withClientOperation，但把 extraStderr 叠加到该次插件
// 进程的 stderr 上（用于实时接收安装进度）。extraStderr 为 nil 时退化为普通调用。
func (e ExternalPlugin) withClientOperationStderr(ctx context.Context, operation string, extraStderr io.Writer, fn func(*Client) error) error {
	stderr := e.Stderr
	if extraStderr != nil {
		if stderr != nil {
			stderr = io.MultiWriter(stderr, extraStderr)
		} else {
			stderr = extraStderr
		}
	}
	return withClient(ctx, ClientConfig{
		Command:           e.Command,
		Args:              e.Args,
		Dir:               e.Dir,
		Stderr:            stderr,
		Manifest:          e.Manifest,
		Permissions:       e.Manifest.Permissions,
		ScopeType:         "plugin",
		ScopeID:           "global",
		Operation:         operation,
		Env:               e.envFor(operation),
		ProcessObserver:   e.ProcessObserver,
		PermissionChecker: e.PermissionChecker,
		Credentials:       e.Credentials,
	}, fn)
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
		Env:               e.envFor(operation),
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
		Env:               e.envFor(operation),
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
