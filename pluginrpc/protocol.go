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
	"sort"
	"strings"
	"sync"
	"time"

	hcplugin "github.com/hashicorp/go-plugin"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

const PluginName = "media-agent-provider"

// PackPluginName 返回逻辑插件在 HashiCorp PluginSet 中的稳定名称。
// 单插件 Serve 继续使用 PluginName，以保持 provider.v1 完全兼容。
func PackPluginName(pluginID string) string {
	return PluginName + "." + pluginID
}

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

func packPluginSet(impls []pluginsdk.Plugin) (hcplugin.PluginSet, error) {
	if len(impls) == 0 {
		return nil, fmt.Errorf("插件 Pack 至少包含一个逻辑插件")
	}
	set := make(hcplugin.PluginSet, len(impls))
	for _, impl := range impls {
		id := strings.TrimSpace(impl.Manifest.ID)
		if id == "" {
			return nil, fmt.Errorf("插件 Pack 包含空 plugin id")
		}
		if id != impl.Manifest.ID {
			return nil, fmt.Errorf("插件 Pack 的 plugin id 不能包含首尾空白: %q", impl.Manifest.ID)
		}
		name := PackPluginName(id)
		if _, exists := set[name]; exists {
			return nil, fmt.Errorf("插件 Pack 的 plugin id 重复: %s", id)
		}
		set[name] = &netRPCPlugin{impl: impl}
	}
	return set, nil
}

func packClientPluginSet(pluginIDs []string) (hcplugin.PluginSet, error) {
	if len(pluginIDs) == 0 {
		return nil, fmt.Errorf("插件 Pack 至少声明一个逻辑插件")
	}
	set := make(hcplugin.PluginSet, len(pluginIDs))
	for _, rawID := range pluginIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return nil, fmt.Errorf("插件 Pack 包含空 plugin id")
		}
		name := PackPluginName(id)
		if _, exists := set[name]; exists {
			return nil, fmt.Errorf("插件 Pack 的 plugin id 重复: %s", id)
		}
		set[name] = &netRPCPlugin{}
	}
	return set, nil
}

// Serve exposes a pluginsdk.Plugin implementation as a HashiCorp go-plugin
// net/rpc plugin. Third-party Go plugins call this from main().
func Serve(impl pluginsdk.Plugin) {
	hcplugin.Serve(&hcplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         pluginSet(impl),
	})
}

// ServePack 在一个 HashiCorp go-plugin 进程中暴露多个逻辑 Plugin。每个逻辑插件
// 仍拥有独立 manifest、权限和 RPC service；调用方应从 main() 使用本函数，且不应
// 再调用 Serve。是否允许某发布者分发 Pack 是 Cloud/宿主策略，不由 SDK 硬编码。
func ServePack(impls []pluginsdk.Plugin) {
	set, err := packPluginSet(impls)
	if err != nil {
		panic(err)
	}
	hcplugin.Serve(&hcplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         set,
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

type ActionRunRequest struct {
	Instance  InstancePayload
	ActionID  string
	InputJSON []byte
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

type DownloaderAddRequest struct {
	Instance InstancePayload
	Request  providers.AddTorrentRequest
}

type DownloaderHashRequest struct {
	Instance InstancePayload
	Hash     string
}

type DownloaderRemoveRequest struct {
	Instance   InstancePayload
	Hash       string
	DeleteData bool
}

type DownloaderFileSelectionRequest struct {
	Instance InstancePayload
	Hash     string
	Files    []providers.TorrentFile
}

type MediaServerItemsRequest struct {
	Instance   InstancePayload
	LibraryID  string
	StartIndex int
	Limit      int
}

type MediaServerItemsReply struct {
	Items []providers.LibraryItem
	Total int
}

type MediaServerSearchRequest struct {
	Instance InstancePayload
	Query    string
}

type MediaServerExistsRequest struct {
	Instance InstancePayload
	Ref      providers.MediaRef
}

type MediaServerIDRequest struct {
	Instance   InstancePayload
	ExternalID string
}

type MediaServerLatestRequest struct {
	Instance InstancePayload
	Limit    int
}

type MetadataSearchRequest struct {
	Instance  InstancePayload
	Query     string
	MediaType string
	Year      int
}

type MetadataDetailRequest struct {
	Instance   InstancePayload
	MediaType  string
	ProviderID string
}

type MetadataSeasonEpisodesRequest struct {
	Instance     InstancePayload
	ProviderID   string
	SeasonNumber int
}

type MetadataExternalIDRequest struct {
	Instance InstancePayload
	IDs      providers.MetaExternalIDs
}

type SiteSearchRequest struct {
	Instance InstancePayload
	Request  providers.TorrentSearchRequest
}

// APIHandleRequest wraps the host-filtered api.endpoint DTO with the plugin
// instance payload used by every other instance-scoped RPC.
type APIHandleRequest struct {
	Instance InstancePayload
	Request  pluginsdk.APIRequest
}

type IdentityVerifyRequest struct {
	Instance InstancePayload
	Request  pluginsdk.IdentityVerifyRequest
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
	Command     string
	Args        []string
	Dir         string
	Stderr      io.Writer
	Manifest    pluginsdk.Manifest
	Permissions pluginsdk.Permissions
	ScopeType   string
	ScopeID     string
	Operation   string
	// Env 是本次操作追加到子进程环境的额外变量（key=value）；空则仅继承宿主环境。
	Env               []string
	ProcessObserver   ProcessObserver
	ActivityObserver  PluginActivityObserver
	PermissionChecker PermissionChecker
	Credentials       *ProcessCredentials
}

// PackPluginConfig 是宿主从已校验 pack.yaml 和逻辑 plugin manifest 组装出的
// 客户端绑定信息。Manifest.ID 是 Dispense 和权限隔离的逻辑 plugin id；权限只读取
// Manifest.Permissions，运行时授权由 PermissionChecker 继续收窄。
type PackPluginConfig struct {
	Manifest  pluginsdk.Manifest
	ScopeType string
	ScopeID   string
}

// PackClientConfig 启动一个常驻 Plugin Pack 进程。Plugins 是宿主允许加载的逻辑
// 插件清单，同时也是可枚举/Dispense 的信任边界；不能以进程自行声称的列表替代。
type PackClientConfig struct {
	Command           string
	Args              []string
	Dir               string
	Stderr            io.Writer
	PackID            string
	PackName          string
	Plugins           []PackPluginConfig
	Operation         string
	Env               []string
	ProcessObserver   ProcessObserver
	ActivityObserver  PluginActivityObserver
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
	activityObserver  PluginActivityObserver
	packID            string
}

type PermissionChecker interface {
	CheckPluginPermission(ctx context.Context, pluginID, scopeType, scopeID, permission string, manifest pluginsdk.Manifest) error
}

type ProcessStartInfo struct {
	Kind       ProcessKind
	PackID     string
	PluginIDs  []string
	PluginID   string
	PluginName string
	Operation  string
	ScopeType  string
	ScopeID    string
	PID        int
	Command    string
	StartedAt  time.Time
}

// ProcessKind 区分普通单插件进程和承载多个逻辑插件的 Pack 物理进程。零值表示
// 旧调用方尚未声明类型，消费者应按 standalone 兼容处理。
type ProcessKind string

const (
	ProcessKindStandalone ProcessKind = "standalone"
	ProcessKindPack       ProcessKind = "plugin_pack"
)

// EffectiveKind 把旧版 observer 产生的零值 Kind 归一化为 standalone。
func (i ProcessStartInfo) EffectiveKind() ProcessKind {
	if i.Kind == "" {
		return ProcessKindStandalone
	}
	return i.Kind
}

// LogicalPluginIDs 返回该物理进程承载的逻辑插件。旧版单插件 observer 只有
// PluginID 时会自动归一化为单元素列表。
func (i ProcessStartInfo) LogicalPluginIDs() []string {
	if len(i.PluginIDs) > 0 {
		return append([]string(nil), i.PluginIDs...)
	}
	if i.PluginID != "" {
		return []string{i.PluginID}
	}
	return nil
}

type ProcessObserver interface {
	PluginProcessStarted(info ProcessStartInfo) func()
}

// PluginActivityStartInfo 描述共享 Pack 内一次逻辑插件 RPC 活动。它与物理进程
// ProcessStartInfo 分离，避免把同一 PID 的资源重复计入每个插件。
type PluginActivityStartInfo struct {
	PluginID   string
	PluginName string
	PackID     string
	Operation  string
	ScopeType  string
	ScopeID    string
	StartedAt  time.Time
}

type PluginActivityObserver interface {
	PluginActivityStarted(info PluginActivityStartInfo) func()
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
	typed.activityObserver = cfg.ActivityObserver
	var done func()
	if cfg.ProcessObserver != nil && cmd.Process != nil {
		done = cfg.ProcessObserver.PluginProcessStarted(ProcessStartInfo{
			Kind:       ProcessKindStandalone,
			PluginIDs:  []string{cfg.Manifest.ID},
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

// PackClient 持有一个 Pack 物理进程和按逻辑 plugin id 分发的 RPC 客户端。
// Close 只执行一次并终止整个 Pack；停用单个逻辑插件不应调用 Close。
type PackClient struct {
	process  *hcplugin.Client
	protocol hcplugin.ClientProtocol
	configs  map[string]PackPluginConfig
	ids      []string
	packID   string
	done     func()
	checker  PermissionChecker
	activity PluginActivityObserver

	mu      sync.Mutex
	clients map[string]*Client
	closed  bool
}

// StartPackClient 启动一个 Pack 进程但不主动初始化各逻辑插件。宿主可先调用
// PluginIDs 枚举，再按需 Dispense；进程观察器只收到一条 Pack 级启动记录。
func StartPackClient(ctx context.Context, cfg PackClientConfig) (*PackClient, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("插件 Pack 入口为空")
	}
	packID := strings.TrimSpace(cfg.PackID)
	if packID == "" {
		return nil, fmt.Errorf("插件 Pack id 为空")
	}
	configs := make(map[string]PackPluginConfig, len(cfg.Plugins))
	ids := make([]string, 0, len(cfg.Plugins))
	for _, pluginCfg := range cfg.Plugins {
		id := strings.TrimSpace(pluginCfg.Manifest.ID)
		if id == "" {
			return nil, fmt.Errorf("插件 Pack 包含空 plugin id")
		}
		if id != pluginCfg.Manifest.ID {
			return nil, fmt.Errorf("插件 Pack 的 plugin id 不能包含首尾空白: %q", pluginCfg.Manifest.ID)
		}
		if _, exists := configs[id]; exists {
			return nil, fmt.Errorf("插件 Pack 的 plugin id 重复: %s", id)
		}
		configs[id] = pluginCfg
		ids = append(ids, id)
	}
	set, err := packClientPluginSet(ids)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	}
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	if err := applyProcessCredentials(cmd, cfg.Credentials); err != nil {
		return nil, err
	}
	process := hcplugin.NewClient(&hcplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          set,
		Cmd:              cmd,
		AllowedProtocols: []hcplugin.Protocol{hcplugin.ProtocolNetRPC},
		StartTimeout:     20 * time.Second,
		Stderr:           cfg.Stderr,
		SyncStdout:       io.Discard,
		SyncStderr:       cfg.Stderr,
	})
	protocol, err := process.Client()
	if err != nil {
		process.Kill()
		return nil, err
	}

	var done func()
	if cfg.ProcessObserver != nil && cmd.Process != nil {
		observedIDs := append([]string(nil), ids...)
		sort.Strings(observedIDs)
		done = cfg.ProcessObserver.PluginProcessStarted(ProcessStartInfo{
			Kind:       ProcessKindPack,
			PackID:     packID,
			PluginIDs:  observedIDs,
			PluginName: cfg.PackName,
			Operation:  cfg.Operation,
			ScopeType:  "pack",
			ScopeID:    packID,
			PID:        cmd.Process.Pid,
			Command:    cfg.Command,
			StartedAt:  time.Now(),
		})
	}
	return &PackClient{
		process: process, protocol: protocol, configs: configs,
		ids: append([]string(nil), ids...), packID: packID, done: done, checker: cfg.PermissionChecker,
		activity: cfg.ActivityObserver,
		clients:  make(map[string]*Client),
	}, nil
}

// PluginIDs 返回 Pack 中可被宿主 Dispense 的逻辑插件 ID，顺序与 PackClientConfig 一致。
func (c *PackClient) PluginIDs() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.ids...)
}

// Ping 检查 Pack RPC 连接是否仍然健康，供宿主健康检查和崩溃回滚使用。
func (c *PackClient) Ping() error {
	if c == nil {
		return fmt.Errorf("插件 Pack 客户端为空")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("插件 Pack 客户端已关闭")
	}
	return c.protocol.Ping()
}

// Dispense 返回绑定到指定逻辑插件 manifest 和权限上下文的客户端。同一 ID 重复调用
// 返回缓存客户端，不会启动新 OS 进程。
func (c *PackClient) Dispense(pluginID string) (*Client, error) {
	if c == nil {
		return nil, fmt.Errorf("插件 Pack 客户端为空")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("插件 Pack 客户端已关闭")
	}
	if client := c.clients[pluginID]; client != nil {
		return client, nil
	}
	pluginCfg, ok := c.configs[pluginID]
	if !ok {
		return nil, fmt.Errorf("插件 Pack 未声明 plugin id %q", pluginID)
	}
	raw, err := c.protocol.Dispense(PackPluginName(pluginID))
	if err != nil {
		return nil, err
	}
	typed, ok := raw.(*Client)
	if !ok {
		return nil, fmt.Errorf("插件 Pack %s 返回了未知客户端类型 %T", pluginID, raw)
	}
	typed.manifest = pluginCfg.Manifest
	// 权限只能来自该逻辑插件已校验的 manifest；动态授权由 PermissionChecker
	// 再收窄，避免 Pack 配置通过第二份权限清单扩大能力。
	typed.permissions = pluginCfg.Manifest.Permissions
	typed.scopeType = pluginCfg.ScopeType
	typed.scopeID = pluginCfg.ScopeID
	typed.permissionChecker = c.checker
	typed.activityObserver = c.activity
	// PackID 只用于逻辑活动归属，不参与 RPC 寻址或权限判断。
	typed.packID = c.packID
	c.clients[pluginID] = typed
	return typed, nil
}

// Close 终止整个 Pack 进程并结束 Pack 级资源观察。
func (c *PackClient) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	done := c.done
	c.done = nil
	process := c.process
	c.mu.Unlock()
	if done != nil {
		done()
	}
	if process != nil {
		process.Kill()
	}
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
const externalPluginActionTimeout = 30 * time.Minute

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
	providerSession := externalProviderSession{external: e}
	if out.HasCapability("downloader") {
		out.NewDownloader = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.DownloaderProvider, error) {
			return &downloaderProvider{session: providerSession, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasCapability("media_server") {
		out.NewMediaServer = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.MediaServerProvider, error) {
			return &mediaServerProvider{session: providerSession, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasCapability("metadata") {
		out.NewMetadata = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.MetadataProvider, error) {
			return &metadataProvider{session: providerSession, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasCapability("site") {
		out.NewSite = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.SiteProvider, error) {
			return &siteProvider{session: providerSession, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasExactCapability(pluginsdk.CapabilityAPIEndpoint) {
		out.NewAPI = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.APIProvider, error) {
			return &apiProvider{session: providerSession, inst: inst, secrets: secrets}, nil
		}
	}
	if out.HasExactCapability(pluginsdk.CapabilityIdentityProvider) {
		out.NewIdentity = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.IdentityProvider, error) {
			return &identityProvider{session: providerSession, inst: inst, secrets: secrets}, nil
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
	if out.HasExactCapability("action.run") && len(out.Manifest.Actions) > 0 {
		out.NewActionHandler = func(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.ActionHandler, error) {
			return &actionHandler{external: e, inst: inst, secrets: secrets}, nil
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
