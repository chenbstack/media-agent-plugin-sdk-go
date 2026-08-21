package pluginrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

func (c *Client) Manifest() (pluginsdk.Manifest, error) {
	var reply JSONReply
	if err := c.call(context.Background(), "Plugin.Manifest", Empty{}, &reply); err != nil {
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
	if err := c.call(context.Background(), "Plugin.ConfigSchema", Empty{}, &reply); err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	var out pluginsdk.ConfigSchema
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	return out, nil
}

func (c *Client) InstallContext(ctx context.Context, component string) (pluginsdk.InstallResult, error) {
	return c.InstallWithInstanceContext(ctx, component, pluginsdk.Instance{}, nil)
}

func (c *Client) InstallWithInstanceContext(ctx context.Context, component string, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.InstallResult, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return pluginsdk.InstallResult{}, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(ctx, "Plugin.Install", InstallRequest{Component: component, Instance: payload}, &reply); err != nil {
		return pluginsdk.InstallResult{}, err
	}
	var out pluginsdk.InstallResult
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.InstallResult{}, err
	}
	return out, nil
}

func (c *Client) CheckInstallContext(ctx context.Context, component string) (pluginsdk.InstallResult, error) {
	return c.CheckInstallWithInstanceContext(ctx, component, pluginsdk.Instance{}, nil)
}

func (c *Client) CheckInstallWithInstanceContext(ctx context.Context, component string, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.InstallResult, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return pluginsdk.InstallResult{}, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(ctx, "Plugin.CheckInstall", InstallRequest{Component: component, Instance: payload}, &reply); err != nil {
		return pluginsdk.InstallResult{}, err
	}
	var out pluginsdk.InstallResult
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.InstallResult{}, err
	}
	return out, nil
}

func (c *Client) UninstallContext(ctx context.Context, component string) (pluginsdk.UninstallResult, error) {
	return c.UninstallWithInstanceContext(ctx, component, pluginsdk.Instance{}, nil)
}

func (c *Client) UninstallWithInstanceContext(ctx context.Context, component string, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.UninstallResult, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return pluginsdk.UninstallResult{}, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(ctx, "Plugin.Uninstall", InstallRequest{Component: component, Instance: payload}, &reply); err != nil {
		return pluginsdk.UninstallResult{}, err
	}
	var out pluginsdk.UninstallResult
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.UninstallResult{}, err
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

// ValidateConfigWithInstanceContext 带上插件的全局实例做配置校验：跨进程的插件靠它
// 拿到宿主服务（站点插件要读站点规则才知道该站点要哪些认证字段）。
func (c *Client) ValidateConfigWithInstanceContext(ctx context.Context, inst pluginsdk.Instance, config map[string]any) error {
	configJSON, err := encodeConfig(config)
	if err != nil {
		return err
	}
	payload, release, err := c.instancePayload(ctx, inst, nil)
	if err != nil {
		return err
	}
	defer release()
	var reply Empty
	return c.call(ctx, "Plugin.ValidateConfig", ConfigRequest{Instance: payload, ConfigJSON: configJSON}, &reply)
}

// ConfigSchemaForInstanceContext 解析插件在给定配置下的有效 schema。
func (c *Client) ConfigSchemaForInstanceContext(ctx context.Context, inst pluginsdk.Instance, config map[string]any) (pluginsdk.ConfigSchema, error) {
	configJSON, err := encodeConfig(config)
	if err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	payload, release, err := c.instancePayload(ctx, inst, nil)
	if err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(ctx, "Plugin.ConfigSchemaForInstance", ConfigRequest{Instance: payload, ConfigJSON: configJSON}, &reply); err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	var out pluginsdk.ConfigSchema
	if err := decodeJSON(reply.Data, &out); err != nil {
		return pluginsdk.ConfigSchema{}, err
	}
	return out, nil
}

func (c *Client) FieldOptions(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, field string) ([]pluginsdk.Option, error) {
	payload, release, err := c.instancePayload(context.Background(), inst, secrets)
	if err != nil {
		return nil, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(context.Background(), "Plugin.FieldOptions", FieldOptionsRequest{Instance: payload, Field: field}, &reply); err != nil {
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
	payload, release, err := c.instancePayload(ctx, inst, nil)
	if err != nil {
		return pluginsdk.AuthStartResult{}, err
	}
	defer release()
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
	payload, release, err := c.instancePayload(ctx, inst, nil)
	if err != nil {
		return pluginsdk.AuthCheckResult{}, err
	}
	defer release()
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
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return err
	}
	defer release()
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var reply Empty
	return c.call(ctx, "Plugin.HandleEvent", EventRequest{Instance: payload, EventJSON: eventJSON}, &reply)
}

func (c *Client) RunActionContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, actionID string, input map[string]any) (pluginsdk.ActionResult, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return pluginsdk.ActionResult{}, err
	}
	defer release()
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return pluginsdk.ActionResult{}, err
	}
	var reply JSONReply
	if err := c.call(ctx, "Plugin.RunAction", ActionRunRequest{Instance: payload, ActionID: actionID, InputJSON: inputJSON}, &reply); err != nil {
		return pluginsdk.ActionResult{}, err
	}
	var result pluginsdk.ActionResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.ActionResult{}, err
	}
	return result, nil
}

func (c *Client) RunScheduledTaskContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, request pluginsdk.ScheduledTaskRequest) (pluginsdk.ScheduledTaskResult, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return pluginsdk.ScheduledTaskResult{}, err
	}
	defer release()
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return pluginsdk.ScheduledTaskResult{}, err
	}
	var reply JSONReply
	if err := c.call(ctx, "Plugin.RunScheduledTask", ScheduledTaskRunRequest{Instance: payload, RequestJSON: requestJSON}, &reply); err != nil {
		return pluginsdk.ScheduledTaskResult{}, err
	}
	var result pluginsdk.ScheduledTaskResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.ScheduledTaskResult{}, err
	}
	return result, nil
}

func (c *Client) AssessOnboardingContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (pluginsdk.OnboardingAssessment, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return pluginsdk.OnboardingAssessment{}, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(ctx, "Plugin.AssessOnboarding", payload, &reply); err != nil {
		return pluginsdk.OnboardingAssessment{}, err
	}
	var result pluginsdk.OnboardingAssessment
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.OnboardingAssessment{}, err
	}
	if err := result.Validate(); err != nil {
		return pluginsdk.OnboardingAssessment{}, err
	}
	return result, nil
}

// HTTPServiceStartContext 让插件起一个常驻 HTTP 服务。第二个返回值要留到服务停掉时
// 再调：服务的每个请求都可能回调宿主，host-services 通道得一直活着，不能发完这一次
// RPC 就还回池里。
func (c *Client) HTTPServiceStartContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, name string, options pluginsdk.HTTPServiceOptions) (pluginsdk.HTTPServiceInfo, func(), error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return pluginsdk.HTTPServiceInfo{}, func() {}, err
	}
	var reply JSONReply
	if err := c.call(ctx, "Plugin.HTTPServiceStart", HTTPServiceStartRequest{Instance: payload, Name: name, Options: options}, &reply); err != nil {
		release()
		return pluginsdk.HTTPServiceInfo{}, func() {}, err
	}
	var info pluginsdk.HTTPServiceInfo
	if err := decodeJSON(reply.Data, &info); err != nil {
		release()
		return pluginsdk.HTTPServiceInfo{}, func() {}, err
	}
	return info, release, nil
}

func (c *Client) HTTPServiceStopContext(ctx context.Context, name string) error {
	var reply Empty
	return c.call(ctx, "Plugin.HTTPServiceStop", HTTPServiceStopRequest{Name: name}, &reply)
}

func (c *Client) CookieSourceTestContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) error {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return err
	}
	defer release()
	var reply Empty
	return c.call(ctx, "Plugin.CookieSourceTest", payload, &reply)
}

func (c *Client) CookieSourceSnapshotContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.CookieSnapshot, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return providers.CookieSnapshot{}, err
	}
	defer release()
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

func (c *Client) RendererTestContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) error {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return err
	}
	defer release()
	var reply Empty
	return c.call(ctx, "Plugin.RendererTest", payload, &reply)
}

func (c *Client) RendererRenderContext(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, req providers.RenderRequest) (providers.RenderResult, error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return providers.RenderResult{}, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(ctx, "Plugin.RendererRender", RendererRenderRequest{Instance: payload, Request: req}, &reply); err != nil {
		return providers.RenderResult{}, err
	}
	var out providers.RenderResult
	if err := decodeJSON(reply.Data, &out); err != nil {
		return providers.RenderResult{}, err
	}
	return out, nil
}

func (c *Client) call(ctx context.Context, serviceMethod string, args any, reply any) error {
	var activityDone func()
	if c.activityObserver != nil {
		activityDone = c.activityObserver.PluginActivityStarted(PluginActivityStartInfo{
			PluginID:   c.manifest.ID,
			PluginName: c.manifest.Name,
			PackID:     c.packID,
			Operation:  serviceMethod,
			ScopeType:  c.scopeType,
			ScopeID:    c.scopeID,
			StartedAt:  time.Now(),
		})
	}
	if activityDone != nil {
		defer activityDone()
	}
	err := c.invoke(ctx, serviceMethod, args, reply)
	if err != nil && errors.Is(err, ErrConfigNotCached) {
		// 插件那边把这份配置淘汰了（或换了进程）。带完整配置原样重试一次。
		if full, ok := c.withFullConfig(args); ok {
			err = c.invoke(ctx, serviceMethod, full, reply)
		}
	}
	return err
}

func (c *Client) invoke(ctx context.Context, serviceMethod string, args any, reply any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	call := c.client.Go(serviceMethod, args, reply, nil)
	select {
	case done := <-call.Done:
		return decodeRPCError(done.Error)
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", serviceMethod, ctx.Err())
	}
}

// withFullConfig 把完整配置塞回请求，供配置摘要未命中后重试。
//
// 请求结构体有几十个，但携带实例的字段一律叫 Instance；按名字找一次，比给每个结构体
// 补一遍接口方法便宜得多，而且这段只在未命中这条罕见路径上跑。
func (c *Client) withFullConfig(args any) (any, bool) {
	value := reflect.ValueOf(args)
	if value.Kind() != reflect.Struct {
		return nil, false
	}
	copied := reflect.New(value.Type()).Elem()
	copied.Set(value)
	field := copied.FieldByName("Instance")
	if !field.IsValid() || field.Type() != reflect.TypeOf(InstancePayload{}) || !field.CanSet() {
		return nil, false
	}
	payload, ok := field.Interface().(InstancePayload)
	if !ok {
		return nil, false
	}
	// forget 顺带丢掉本地记录：下一次调用会重新整份发一遍，两边由此重新对齐。
	configJSON := c.configs.forget(payload.ID)
	if configJSON == nil {
		return nil, false
	}
	payload.ConfigJSON = configJSON
	field.Set(reflect.ValueOf(payload))
	return copied.Interface(), true
}

// featureSet 问一次插件支持哪些协议优化，结果记在 Client 上。
//
// 探测失败分两种：插件没有这个方法是终局答案（老 SDK 编译的插件），记下来不再问；
// 连接或上下文出问题则不下结论，留给下次调用重探。
func (c *Client) featureSet(ctx context.Context) FeaturesReply {
	c.featuresMu.Lock()
	defer c.featuresMu.Unlock()
	if c.featuresProbed {
		return c.features
	}
	var reply FeaturesReply
	switch err := c.invoke(ctx, "Plugin.Features", Empty{}, &reply); {
	case err == nil:
		c.features, c.featuresProbed = reply, true
	case isUnknownRPCMethod(err):
		c.featuresProbed = true
	}
	return c.features
}

// isUnknownRPCMethod 认出 net/rpc 对未注册方法的回复。老插件的 SDK 里没有新方法，
// 这是判定它是老版本的唯一信号。
func isUnknownRPCMethod(err error) bool {
	message := err.Error()
	return strings.Contains(message, "can't find method") || strings.Contains(message, "can't find service")
}

// instancePayload 把实例打包成一次 RPC 的载荷。第二个返回值必须在这次调用结束后调用：
// 它把 host-services 通道还回池里（没走池时是空操作）。开流、起常驻服务这类调用的通道
// 要活到会话结束，release 就挂在会话的 Close 上，而不是发完 RPC 就还。
// needsHostServices 判断这次调用要不要为插件开一条回调宿主的通道。
//
// 漏掉任何一项的后果都是同一种：插件那边字段为 nil，功能静默消失，宿主日志里什么
// 都没有。所以判定集中在这里，并由测试逐项守着。
func needsHostServices(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) bool {
	return secrets != nil || inst.KV != nil || inst.DB != nil || inst.Logger != nil || inst.Runtime != nil ||
		inst.SiteAccounts != nil || inst.Subscriptions != nil || inst.Downloads != nil || inst.Transfers != nil ||
		inst.Rules != nil || inst.Connections != nil || inst.ConnectionCredentials != nil || inst.Storages != nil ||
		inst.Schedules != nil || inst.Settings != nil || inst.Entitlements != nil || inst.PluginServices != nil ||
		inst.Sidecars != nil || inst.Mirrors != nil || inst.Playback != nil || inst.Renderer != nil ||
		inst.Cloud != nil || inst.SiteRules != nil
}

func (c *Client) instancePayload(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (InstancePayload, func(), error) {
	configJSON, err := encodeConfig(inst.Config)
	if err != nil {
		return InstancePayload{}, func() {}, err
	}
	payload := InstancePayload{
		ID:           inst.ID,
		Name:         inst.Name,
		ConfigJSON:   configJSON,
		WorkspaceDir: inst.Workspace.Root(),
	}
	features := c.featureSet(ctx)
	// 老插件收到空的 ConfigJSON 会当成空配置，所以只对声明了 ConfigDigest 的插件省这份传输。
	if features.ConfigDigest {
		payload.ConfigJSON, payload.ConfigHash = c.configs.prepare(inst.ID, configJSON)
	}
	if needsHostServices(inst, secrets) {
		state := &hostServicesState{
			ctx:                   ctx,
			pluginID:              c.manifest.ID,
			scopeType:             c.scopeType,
			scopeID:               c.scopeID,
			manifest:              c.manifest,
			permissions:           c.permissions,
			permissionChecker:     c.permissionChecker,
			secrets:               secrets,
			kv:                    inst.KV,
			db:                    inst.DB,
			logger:                inst.Logger,
			siteAccounts:          inst.SiteAccounts,
			subscriptions:         inst.Subscriptions,
			downloads:             inst.Downloads,
			transfers:             inst.Transfers,
			rules:                 inst.Rules,
			connections:           inst.Connections,
			connectionCredentials: inst.ConnectionCredentials,
			storages:              inst.Storages,
			schedules:             inst.Schedules,
			settings:              inst.Settings,
			entitlements:          inst.Entitlements,
			pluginServices:        inst.PluginServices,
			sidecars:              inst.Sidecars,
			mirrors:               inst.Mirrors,
			playback:              inst.Playback,
			renderer:              inst.Renderer,
			cloud:                 inst.Cloud,
			siteRules:             inst.SiteRules,
		}
		// 只对声明了复用的插件走池：老插件每次调用都会 Dial，而池化的通道已经被
		// AcceptAndServe 消费掉了，它的第二次 Dial 会一直等不到人 accept。
		pool := c.hostChannels
		if !features.PersistentHostServices {
			pool = nil
		}
		id, persistent, release := pool.lease(c.broker, inst.ID, state)
		payload.HostServicesBrokerID = id
		payload.HostServicesPersistent = persistent
		return payload, release, nil
	}
	return payload, func() {}, nil
}

type storageProvider struct {
	session storageProviderSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
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

type actionHandler struct {
	external ExternalPlugin
	inst     pluginsdk.Instance
	secrets  pluginsdk.SecretResolver
}

type scheduledTaskHandler struct {
	external ExternalPlugin
	inst     pluginsdk.Instance
	secrets  pluginsdk.SecretResolver
}

func (h *actionHandler) RunAction(ctx context.Context, actionID string, input map[string]any) (pluginsdk.ActionResult, error) {
	var result pluginsdk.ActionResult
	callCtx, cancel := contextWithTimeout(ctx, externalPluginActionTimeout)
	defer cancel()
	err := h.external.withClientOperation(callCtx, "plugin.action."+actionID, func(c *Client) error {
		got, err := c.RunActionContext(callCtx, h.inst, h.secrets, actionID, input)
		if err != nil {
			return err
		}
		result = got
		return nil
	})
	return result, err
}

func (h *scheduledTaskHandler) RunScheduledTask(ctx context.Context, request pluginsdk.ScheduledTaskRequest) (pluginsdk.ScheduledTaskResult, error) {
	var result pluginsdk.ScheduledTaskResult
	callCtx, cancel := contextWithTimeout(ctx, externalPluginActionTimeout)
	defer cancel()
	err := h.external.withClientOperation(callCtx, "plugin.scheduled_task."+request.TaskID, func(c *Client) error {
		got, err := c.RunScheduledTaskContext(callCtx, h.inst, h.secrets, request)
		if err != nil {
			return err
		}
		result = got
		return nil
	})
	return result, err
}

type rendererProvider struct {
	external ExternalPlugin
	inst     pluginsdk.Instance
	secrets  pluginsdk.SecretResolver
}

func (p *rendererProvider) Kind() string {
	return p.external.Manifest.ID
}

func (p *rendererProvider) TestConnection(ctx context.Context) error {
	return p.external.withClientOperation(ctx, "renderer.test", func(c *Client) error {
		return c.RendererTestContext(ctx, p.inst, p.secrets)
	})
}

func (p *rendererProvider) Render(ctx context.Context, req providers.RenderRequest) (providers.RenderResult, error) {
	var out providers.RenderResult
	err := p.external.withClientOperation(ctx, "renderer.render", func(c *Client) error {
		got, err := c.RendererRenderContext(ctx, p.inst, p.secrets, req)
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	return out, err
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
	return p.session.pluginID()
}

func (p *storageProvider) TestConnection(ctx context.Context) error {
	return p.withClientOperation(ctx, "storage.test", func(c *Client) error {
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		var reply Empty
		return c.call(ctx, "Plugin.StorageTest", payload, &reply)
	})
}

func (p *storageProvider) Info(ctx context.Context) (providers.StorageInfo, error) {
	var out providers.StorageInfo
	err := p.withClientOperation(ctx, "storage.info", func(c *Client) error {
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		var reply JSONReply
		if err := c.call(ctx, "Plugin.StorageInfo", payload, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}

func (p *storageProvider) EnsureMounted(ctx context.Context) error {
	return p.withClientOperation(ctx, "storage.ensure_mounted", func(c *Client) error {
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		var reply Empty
		return c.call(ctx, "Plugin.StorageEnsureMounted", payload, &reply)
	})
}

func (p *storageProvider) Unmount(ctx context.Context) error {
	return p.withClientOperation(ctx, "storage.unmount", func(c *Client) error {
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		var reply Empty
		return c.call(ctx, "Plugin.StorageUnmount", payload, &reply)
	})
}

func (p *storageProvider) Stat(ctx context.Context, name string) (providers.StorageFileInfo, error) {
	var out providers.StorageFileInfo
	err := p.withClientOperation(ctx, "storage.stat", func(c *Client) error {
		req, release, err := c.pathRequest(ctx, p.inst, p.secrets, name)
		if err != nil {
			return err
		}
		defer release()
		var reply JSONReply
		if err := c.call(ctx, "Plugin.StorageStat", req, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &out)
	})
	return out, err
}

func (p *storageProvider) ListDir(ctx context.Context, path string) ([]providers.StorageFileInfo, error) {
	var out []providers.StorageFileInfo
	err := p.withClientOperation(ctx, "storage.list_dir", func(c *Client) error {
		req, release, err := c.pathRequest(ctx, p.inst, p.secrets, path)
		if err != nil {
			return err
		}
		defer release()
		var reply JSONReply
		if err := c.call(ctx, "Plugin.StorageListDir", req, &reply); err != nil {
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
	req, release, err := running.client.pathRequest(ctx, p.inst, p.secrets, name)
	if err != nil {
		running.Close()
		return nil, err
	}
	// 流会活到调用方 Close 为止，插件也会在这期间回调宿主，所以 host-services 通道
	// 不能发完这一次 RPC 就还回池里。
	closeSession := func() { release(); running.Close() }
	var reply BrokerReply
	if err := running.client.call(ctx, "Plugin.StorageOpenReader", req, &reply); err != nil {
		closeSession()
		return nil, err
	}
	conn, err := running.client.broker.Dial(reply.ID)
	if err != nil {
		closeSession()
		return nil, err
	}
	return pluginClientReadCloser{ReadCloser: closeReadConn{Conn: conn}, closeClient: closeSession}, nil
}

func (p *storageProvider) OpenWriter(ctx context.Context, name string) (io.WriteCloser, error) {
	running, err := p.startClientOperation(ctx, "storage.open_writer")
	if err != nil {
		return nil, err
	}
	req, release, err := running.client.pathRequest(ctx, p.inst, p.secrets, name)
	if err != nil {
		running.Close()
		return nil, err
	}
	// 流会活到调用方 Close 为止，插件也会在这期间回调宿主，所以 host-services 通道
	// 不能发完这一次 RPC 就还回池里。
	closeSession := func() { release(); running.Close() }
	var reply BrokerReply
	if err := running.client.call(ctx, "Plugin.StorageOpenWriter", req, &reply); err != nil {
		closeSession()
		return nil, err
	}
	conn, err := running.client.broker.Dial(reply.ID)
	if err != nil {
		closeSession()
		return nil, err
	}
	return pluginClientWriteCloser{WriteCloser: conn, closeClient: closeSession}, nil
}

func (p *storageProvider) OpenRangeReader(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	running, err := p.startClientOperation(ctx, "storage.open_range_reader")
	if err != nil {
		return nil, err
	}
	payload, release, err := running.client.instancePayload(ctx, p.inst, p.secrets)
	if err != nil {
		running.Close()
		return nil, err
	}
	// 同 OpenReader：通道要活到流关掉。
	closeSession := func() { release(); running.Close() }
	var reply BrokerReply
	req := StorageRangeRequest{Instance: payload, Path: name, Offset: offset, Length: length}
	if err := running.client.client.Call("Plugin.StorageOpenRangeReader", req, &reply); err != nil {
		closeSession()
		return nil, decodeRPCError(err)
	}
	conn, err := running.client.broker.Dial(reply.ID)
	if err != nil {
		closeSession()
		return nil, err
	}
	return pluginClientReadCloser{ReadCloser: closeReadConn{Conn: conn}, closeClient: closeSession}, nil
}

func (p *storageProvider) OpenRangeWriter(ctx context.Context, name string, offset int64) (io.WriteCloser, error) {
	running, err := p.startClientOperation(ctx, "storage.open_range_writer")
	if err != nil {
		return nil, err
	}
	payload, release, err := running.client.instancePayload(ctx, p.inst, p.secrets)
	if err != nil {
		running.Close()
		return nil, err
	}
	// 同 OpenReader：通道要活到流关掉。
	closeSession := func() { release(); running.Close() }
	var reply BrokerReply
	req := StorageRangeRequest{Instance: payload, Path: name, Offset: offset}
	if err := running.client.client.Call("Plugin.StorageOpenRangeWriter", req, &reply); err != nil {
		closeSession()
		return nil, decodeRPCError(err)
	}
	conn, err := running.client.broker.Dial(reply.ID)
	if err != nil {
		closeSession()
		return nil, err
	}
	return pluginClientWriteCloser{WriteCloser: conn, closeClient: closeSession}, nil
}

func (p *storageProvider) Truncate(ctx context.Context, name string, size int64) error {
	return p.withClientOperation(ctx, "storage.truncate", func(c *Client) error {
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		var reply Empty
		return c.call(ctx, "Plugin.StorageTruncate", StorageTruncateRequest{Instance: payload, Path: name, Size: size}, &reply)
	})
}

// CopyBetweenInstances 请求插件在进程内把同插件另一实例 source 的 sourcePath
// 复制到本实例 targetPath；source 不是同一插件的实例时返回
// providers.ErrCrossInstanceCopyUnsupported，调用方应回退到其他复制方式。
func (p *storageProvider) CopyBetweenInstances(ctx context.Context, source providers.StorageProvider, sourcePath, targetPath string, progress providers.ProgressFunc) error {
	src, ok := source.(*storageProvider)
	if !ok || src.session.pluginID() == "" || src.session.pluginID() != p.session.pluginID() {
		return providers.ErrCrossInstanceCopyUnsupported
	}
	return p.withClientOperation(ctx, "storage.copy_between_instances", func(c *Client) error {
		sourcePayload, releaseSourcePayload, err := c.instancePayload(ctx, src.inst, src.secrets)
		if err != nil {
			return err
		}
		defer releaseSourcePayload()
		targetPayload, releaseTargetPayload, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer releaseTargetPayload()
		req := StorageCopyBetweenRequest{
			Source:     sourcePayload,
			Target:     targetPayload,
			SourcePath: sourcePath,
			TargetPath: targetPath,
		}
		if progress != nil {
			req.ProgressBrokerID = serveProgressSink(c.broker, progress)
		}
		var reply Empty
		return c.call(ctx, "Plugin.StorageCopyBetween", req, &reply)
	})
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
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		sourceID := c.broker.NextId()
		go c.broker.AcceptAndServe(sourceID, &uploadSourceServer{ctx: ctx, source: source, broker: c.broker})
		var reply Empty
		return c.call(ctx, "Plugin.StorageUpload", StorageUploadRequest{
			Instance:             payload,
			Path:                 name,
			UploadSourceBrokerID: sourceID,
		}, &reply)
	})
}

func (p *storageProvider) ResolvePlaybackURL(ctx context.Context, input providers.PlaybackURLInput) (providers.PlaybackURLResult, error) {
	var out providers.PlaybackURLResult
	err := p.withClientOperation(ctx, "storage.playback_url", func(c *Client) error {
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		var reply JSONReply
		if err := c.call(ctx, "Plugin.StorageResolvePlaybackURL", StoragePlaybackURLRequest{
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
		req, release, err := c.pathRequest(ctx, p.inst, p.secrets, path)
		if err != nil {
			return err
		}
		defer release()
		var reply Empty
		return c.call(ctx, method, req, &reply)
	})
}

func (p *storageProvider) callRename(ctx context.Context, operation, method, oldpath, newpath string) error {
	return p.withClientOperation(ctx, operation, func(c *Client) error {
		payload, release, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		defer release()
		var reply Empty
		return c.call(ctx, method, StorageRenameRequest{Instance: payload, OldPath: oldpath, NewPath: newpath}, &reply)
	})
}

func (p *storageProvider) withClient(ctx context.Context, fn func(*Client) error) error {
	return p.withClientOperation(ctx, "storage.rpc", fn)
}

func (p *storageProvider) withClientOperation(ctx context.Context, operation string, fn func(*Client) error) error {
	scopeType, scopeID := p.scope()
	return p.session.withClientForScope(ctx, scopeType, scopeID, operation, fn)
}

func (p *storageProvider) startClient(ctx context.Context) (*runningClient, error) {
	return p.startClientOperation(ctx, "storage.rpc")
}

func (p *storageProvider) startClientOperation(ctx context.Context, operation string) (*runningClient, error) {
	scopeType, scopeID := p.scope()
	client, closeFn, err := p.session.leaseClientForScope(ctx, scopeType, scopeID, operation)
	if err != nil {
		return nil, err
	}
	return &runningClient{client: client, done: closeFn}, nil
}

func (p *storageProvider) scope() (string, string) {
	if p.inst.ID == "" {
		return "plugin", "global"
	}
	return "storage", p.inst.ID
}

// pathRequest 组一个「实例 + 路径」的请求。release 与 instancePayload 同义：调用方
// 发完这一次 RPC 就该调，开流的调用则要等流关掉再调。
func (c *Client) pathRequest(ctx context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver, path string) (StoragePathRequest, func(), error) {
	payload, release, err := c.instancePayload(ctx, inst, secrets)
	if err != nil {
		return StoragePathRequest{}, func() {}, err
	}
	return StoragePathRequest{Instance: payload, Path: path}, release, nil
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

// SiteSupportForURL 查询插件对某个站点地址的支持情况。
func (c *Client) SiteSupportForURL(ctx context.Context, inst pluginsdk.Instance, url string) (providers.SiteSupport, error) {
	payload, release, err := c.instancePayload(ctx, inst, nil)
	if err != nil {
		return providers.SiteSupport{}, err
	}
	defer release()
	var reply JSONReply
	if err := c.call(ctx, "Plugin.SiteSupportForURL", SiteSupportRequest{Instance: payload, URL: url}, &reply); err != nil {
		return providers.SiteSupport{}, err
	}
	var out providers.SiteSupport
	if err := decodeJSON(reply.Data, &out); err != nil {
		return providers.SiteSupport{}, err
	}
	return out, nil
}
