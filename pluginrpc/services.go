package pluginrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/rpc"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
	runtimesdk "github.com/chenbstack/media-agent-plugin-sdk-go/runtime"
)

// hostServicesState 是一次调用期间宿主这一侧的全部上下文：这次调用的 ctx，加上这次
// 调用允许插件回调的服务集合。
//
// 它从 hostServicesServer 里分出来，是为了让一条 host-services 通道能跨调用复用：建
// 通道（NextId + AcceptAndServe + 插件侧 Dial + rpc.NewClient + 首次 gob 类型协商）实测
// 和整个 RPC 一样贵，约 43µs，而这些东西每次调用都重做了一遍。通道留着不动、只换这份
// 状态，线格式一个字节都不用改。
type hostServicesState struct {
	ctx                   context.Context
	pluginID              string
	scopeType             string
	scopeID               string
	manifest              pluginsdk.Manifest
	permissions           pluginsdk.Permissions
	permissionChecker     PermissionChecker
	secrets               pluginsdk.SecretResolver
	kv                    pluginsdk.KVStore
	db                    pluginsdk.PluginDB
	logger                pluginsdk.Logger
	siteAccounts          pluginsdk.SiteAccounts
	subscriptions         pluginsdk.Subscriptions
	downloads             pluginsdk.Downloads
	transfers             pluginsdk.Transfers
	rules                 pluginsdk.Rules
	connections           pluginsdk.Connections
	connectionCredentials pluginsdk.ConnectionCredentials
	storages              pluginsdk.Storages
	schedules             pluginsdk.Schedules
	settings              pluginsdk.Settings
	entitlements          pluginsdk.Entitlements
	pluginServices        pluginsdk.PluginServices
	sidecars              pluginsdk.MediaSidecars
	mirrors               pluginsdk.MediaMirrors
	playback              pluginsdk.MediaPlayback
	renderer              pluginsdk.PageRenderer
	cloud                 pluginsdk.CloudIdentity
	siteRules             pluginsdk.SiteRuleFiles
}

// hostServicesServer 是插件回调宿主的那一端。通道池会在每次租用前换掉 state，所以
// 服务器本身除了这个指针不持有任何东西。
type hostServicesServer struct {
	state atomic.Pointer[hostServicesState]
}

func newHostServicesServer(state *hostServicesState) *hostServicesServer {
	server := &hostServicesServer{}
	server.state.Store(state)
	return server
}

// live 取当前这次调用的状态。通道还回池里之后取到的是 releasedHostServices：ctx 已
// 取消、服务全为空，迟到的回调因此拿到明确的失败，而不是打在上一次调用的服务上。
//
// 一个处理器里可能读好几次（先看 ctx 再看服务），中间要是正好被还回池里，就会读到
// 半新半旧的组合。这与今天「调用结束通道被拆掉、迟到的回调撞上断开的连接」是同一类
// 竞态，结果都是那次回调失败。
func (s *hostServicesServer) live() *hostServicesState {
	if state := s.state.Load(); state != nil {
		return state
	}
	return releasedHostServices
}

// releasedHostServices 是「这条通道现在没人租」的状态。ctx 建好就取消：插件里跑飞的
// goroutine 迟到的回调会立刻拿到 context canceled，而不是悄悄用上别人的服务。
var releasedHostServices = func() *hostServicesState {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return &hostServicesState{ctx: ctx}
}()

type EntitlementCheckRequest struct{ Feature string }
type EntitlementCheckReply struct{ Granted bool }

func (s *hostServicesServer) HasEntitlement(req EntitlementCheckRequest, reply *EntitlementCheckReply) error {
	if s.live().entitlements == nil {
		reply.Granted = false
		return nil
	}
	reply.Granted = s.live().entitlements.HasEntitlement(s.live().ctx, strings.TrimSpace(req.Feature))
	return nil
}

type RevealRequest struct {
	Ref    string
	Reason string
}

func (s *hostServicesServer) Reveal(req RevealRequest, reply *StringReply) error {
	if s.live().secrets == nil {
		return fmt.Errorf("宿主未提供 SecretResolver")
	}
	if err := s.requireSecretPermission(); err != nil {
		return err
	}
	value, err := s.live().secrets.Reveal(s.live().ctx, req.Ref, req.Reason)
	if err != nil {
		return err
	}
	reply.Value = value
	return nil
}

type KVGetRequest struct {
	Key string
}

type KVGetReply struct {
	Found bool
	Data  []byte
}

func (s *hostServicesServer) KVGet(req KVGetRequest, reply *KVGetReply) error {
	if s.live().kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	var raw json.RawMessage
	found, err := s.live().kv.Get(s.live().ctx, req.Key, &raw)
	if err != nil {
		return err
	}
	reply.Found = found
	reply.Data = raw
	return nil
}

type KVSetRequest struct {
	Key        string
	Data       []byte
	TTLSeconds int64
}

func (s *hostServicesServer) KVSet(req KVSetRequest, reply *Empty) error {
	if s.live().kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	var value any
	if len(req.Data) > 0 {
		if err := json.Unmarshal(req.Data, &value); err != nil {
			return err
		}
	}
	return s.live().kv.Set(s.live().ctx, req.Key, value, time.Duration(req.TTLSeconds)*time.Second)
}

func (s *hostServicesServer) KVDelete(req KVGetRequest, reply *Empty) error {
	if s.live().kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	return s.live().kv.Delete(s.live().ctx, req.Key)
}

func (s *hostServicesServer) KVDeletePrefix(req KVGetRequest, reply *Empty) error {
	if s.live().kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	return s.live().kv.DeletePrefix(s.live().ctx, req.Key)
}

// DBStatementRequest 承载一条结构化语句。语句里含 any 类型的绑定值，gob 编不了，
// 所以按 JSON 传——与 KV / 宿主写入通道的做法一致。
type DBStatementRequest struct {
	QueryJSON []byte
}

// DBWriteReply 是 Insert / Update / Delete 的结果。
type DBWriteReply struct {
	RowsAffected int64
	LastInsertID int64
}

func (s *hostServicesServer) requireDB() error {
	if s.live().db == nil {
		return fmt.Errorf("宿主未提供 PluginDB")
	}
	return s.requireDataPermission("storage")
}

func (s *hostServicesServer) DBInsert(req DBStatementRequest, reply *DBWriteReply) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	var query pluginsdk.Insert
	if err := decodeDBStatement(req.QueryJSON, &query); err != nil {
		return err
	}
	result, err := s.live().db.Insert(s.live().ctx, query)
	if err != nil {
		return err
	}
	reply.RowsAffected, reply.LastInsertID = result.RowsAffected, result.LastInsertID
	return nil
}

func (s *hostServicesServer) DBUpdate(req DBStatementRequest, reply *DBWriteReply) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	var query pluginsdk.Update
	if err := decodeDBStatement(req.QueryJSON, &query); err != nil {
		return err
	}
	result, err := s.live().db.Update(s.live().ctx, query)
	if err != nil {
		return err
	}
	reply.RowsAffected, reply.LastInsertID = result.RowsAffected, result.LastInsertID
	return nil
}

func (s *hostServicesServer) DBDelete(req DBStatementRequest, reply *DBWriteReply) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	var query pluginsdk.Delete
	if err := decodeDBStatement(req.QueryJSON, &query); err != nil {
		return err
	}
	result, err := s.live().db.Delete(s.live().ctx, query)
	if err != nil {
		return err
	}
	reply.RowsAffected, reply.LastInsertID = result.RowsAffected, result.LastInsertID
	return nil
}

// DBBatchReply 是一批写语句的结果，顺序与请求里的语句一一对应。
type DBBatchReply struct {
	Results []DBWriteReply
}

func (s *hostServicesServer) DBBatch(req DBStatementRequest, reply *DBBatchReply) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	var statements []pluginsdk.Statement
	if err := decodeDBStatement(req.QueryJSON, &statements); err != nil {
		return err
	}
	results, err := s.live().db.Batch(s.live().ctx, statements)
	if err != nil {
		return err
	}
	reply.Results = make([]DBWriteReply, len(results))
	for i, result := range results {
		reply.Results[i] = DBWriteReply{RowsAffected: result.RowsAffected, LastInsertID: result.LastInsertID}
	}
	return nil
}

type DBQueryReply struct {
	RowsJSON []byte
}

type SiteAccountUpsertRequest struct {
	Input pluginsdk.SiteAccountWrite
}

type SubscriptionUpsertRequest struct {
	Input pluginsdk.SubscriptionWrite
}

type DownloadUpsertRequest struct {
	Input pluginsdk.DownloadWrite
}

type DownloadFindRequest struct {
	Hash string
}

type DownloadFindReply struct {
	Found  bool
	Result pluginsdk.HostWriteResult
}

type TransferUpsertRequest struct {
	Input pluginsdk.TransferWrite
}

type RuleProfileUpsertRequest struct {
	Input pluginsdk.RuleProfileWrite
}

type RuleSortSetRequest struct {
	Input pluginsdk.RuleSortWrite
}

type RuleDefaultSetRequest struct {
	Input pluginsdk.RuleDefaultWrite
}

type ConnectionListRequest struct{ Section string }
type ConnectionGetRequest struct{ Section, ID string }
type ConnectionCredentialRequest struct{ Section, ID, Field, Reason string }
type ConnectionUpsertRequest struct{ Input pluginsdk.ConnectionWrite }
type StorageGetRequest struct{ ID string }
type StorageUpsertRequest struct{ Input pluginsdk.StorageWrite }
type DirectoryMappingGetRequest struct{ ID string }
type DirectoryMappingUpsertRequest struct {
	Input pluginsdk.DirectoryMappingWrite
}
type SettingGetRequest struct{ Key string }
type SettingGetReply struct {
	Found bool
	Value []byte
}
type SettingSetRequest struct{ Input pluginsdk.SettingWrite }
type ScheduleGetRequest struct{ TaskType string }
type ScheduleSetRequest struct{ Input pluginsdk.ScheduleWrite }

func (s *hostServicesServer) hostWriteResult(permission string, run func() (pluginsdk.HostWriteResult, error), reply *JSONReply) error {
	if err := s.requireHostPermission(permission); err != nil {
		return err
	}
	result, err := run()
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

func (s *hostServicesServer) ListConnections(req ConnectionListRequest, reply *JSONReply) error {
	if s.live().connections == nil {
		return fmt.Errorf("宿主未提供 Connections")
	}
	if err := s.requireHostPermission("connections.read"); err != nil {
		return err
	}
	result, err := s.live().connections.ListConnections(s.live().ctx, req.Section)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) GetConnection(req ConnectionGetRequest, reply *JSONReply) error {
	if s.live().connections == nil {
		return fmt.Errorf("宿主未提供 Connections")
	}
	if err := s.requireHostPermission("connections.read"); err != nil {
		return err
	}
	result, err := s.live().connections.GetConnection(s.live().ctx, req.Section, req.ID)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) RevealConnectionCredential(req ConnectionCredentialRequest, reply *StringReply) error {
	if s.live().connectionCredentials == nil {
		return fmt.Errorf("宿主未提供连接凭据读取能力")
	}
	if err := s.requireHostPermission("connections.credentials.read"); err != nil {
		return err
	}
	value, err := s.live().connectionCredentials.RevealConnectionCredential(s.live().ctx, req.Section, req.ID, req.Field, req.Reason)
	if err != nil {
		return err
	}
	reply.Value = value
	return nil
}

func (s *hostServicesServer) UpsertConnection(req ConnectionUpsertRequest, reply *JSONReply) error {
	if s.live().connections == nil {
		return fmt.Errorf("宿主未提供 Connections")
	}
	return s.hostWriteResult("connections.write", func() (pluginsdk.HostWriteResult, error) {
		return s.live().connections.UpsertConnection(s.live().ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListStorages(_ Empty, reply *JSONReply) error {
	if s.live().storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.live().storages.ListStorages(s.live().ctx)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) GetStorage(req StorageGetRequest, reply *JSONReply) error {
	if s.live().storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.live().storages.GetStorage(s.live().ctx, req.ID)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) UpsertStorage(req StorageUpsertRequest, reply *JSONReply) error {
	if s.live().storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	return s.hostWriteResult("storages.write", func() (pluginsdk.HostWriteResult, error) {
		return s.live().storages.UpsertStorage(s.live().ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListDirectoryMappings(_ Empty, reply *JSONReply) error {
	if s.live().storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.live().storages.ListDirectoryMappings(s.live().ctx)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) GetDirectoryMapping(req DirectoryMappingGetRequest, reply *JSONReply) error {
	if s.live().storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.live().storages.GetDirectoryMapping(s.live().ctx, req.ID)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) UpsertDirectoryMapping(req DirectoryMappingUpsertRequest, reply *JSONReply) error {
	if s.live().storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	return s.hostWriteResult("storages.write", func() (pluginsdk.HostWriteResult, error) {
		return s.live().storages.UpsertDirectoryMapping(s.live().ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) GetSetting(req SettingGetRequest, reply *SettingGetReply) error {
	if s.live().settings == nil {
		return fmt.Errorf("宿主未提供 Settings")
	}
	if err := s.requireHostPermission("settings.read"); err != nil {
		return err
	}
	var raw json.RawMessage
	found, err := s.live().settings.JSON(s.live().ctx, req.Key, &raw)
	if err != nil {
		return err
	}
	reply.Found, reply.Value = found, append([]byte(nil), raw...)
	return nil
}

func (s *hostServicesServer) SetSetting(req SettingSetRequest, reply *JSONReply) error {
	if s.live().settings == nil {
		return fmt.Errorf("宿主未提供 Settings")
	}
	return s.hostWriteResult("settings.write", func() (pluginsdk.HostWriteResult, error) {
		return s.live().settings.SetSetting(s.live().ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListSchedules(_ Empty, reply *JSONReply) error {
	if s.live().schedules == nil {
		return fmt.Errorf("宿主未提供 Schedules")
	}
	if err := s.requireHostPermission("schedules.read"); err != nil {
		return err
	}
	result, err := s.live().schedules.ListSchedules(s.live().ctx)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) GetSchedule(req ScheduleGetRequest, reply *JSONReply) error {
	if s.live().schedules == nil {
		return fmt.Errorf("宿主未提供 Schedules")
	}
	if err := s.requireHostPermission("schedules.read"); err != nil {
		return err
	}
	result, err := s.live().schedules.GetSchedule(s.live().ctx, req.TaskType)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) SetSchedule(req ScheduleSetRequest, reply *JSONReply) error {
	if s.live().schedules == nil {
		return fmt.Errorf("宿主未提供 Schedules")
	}
	return s.hostWriteResult("schedules.write", func() (pluginsdk.HostWriteResult, error) {
		return s.live().schedules.SetSchedule(s.live().ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListSiteAccounts(_ Empty, reply *JSONReply) error {
	if s.live().siteAccounts == nil {
		return fmt.Errorf("宿主未提供 SiteAccounts")
	}
	if err := s.requireHostPermission("site.accounts.read"); err != nil {
		return err
	}
	result, err := s.live().siteAccounts.ListSiteAccounts(s.live().ctx)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

type PluginServiceCallRequest struct {
	Call pluginsdk.PluginServiceCall
}

func (s *hostServicesServer) CallPluginService(req PluginServiceCallRequest, reply *JSONReply) error {
	if s.live().pluginServices == nil {
		return fmt.Errorf("宿主未提供 PluginServices")
	}
	provider := strings.TrimSpace(req.Call.Provider)
	capability := strings.TrimSpace(req.Call.Capability)
	if provider == "" || capability == "" {
		return fmt.Errorf("plugin_service 调用缺少 provider/capability")
	}
	// 调用方需按能力粒度声明并被授予 host 权限
	// plugin_service.<provider>/<capability>；提供方是否开放该能力与实际路由由
	// 宿主实现在 CallPluginService 内部完成。
	if err := s.requireHostPermission("plugin_service." + provider + "/" + capability); err != nil {
		return err
	}
	result, err := s.live().pluginServices.CallPluginService(s.live().ctx, req.Call)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

type SubtitleWriteRequest struct {
	Input pluginsdk.SubtitleWrite
}

// WriteSubtitle 是插件唯一能落文件的口子。插件给的是 FileRef 而不是路径，
// 由宿主自己解析成存储和目标文件名——插件既指不了目录，也没法用 ../ 走出去。
func (s *hostServicesServer) WriteSubtitle(req SubtitleWriteRequest, reply *JSONReply) error {
	if s.live().sidecars == nil {
		return fmt.Errorf("宿主未提供 MediaSidecars")
	}
	if err := s.requireHostPermission("media.sidecar.write"); err != nil {
		return err
	}
	result, err := s.live().sidecars.WriteSubtitle(s.live().ctx, req.Input)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

type MirrorWriteRequest struct {
	Input pluginsdk.MirrorWrite
}

type PlaybackResolveRequest struct {
	Input pluginsdk.PlaybackResolveInput
}

func (s *hostServicesServer) ResolvePlaybackURL(req PlaybackResolveRequest, reply *JSONReply) error {
	if s.live().playback == nil {
		return fmt.Errorf("宿主未提供 MediaPlayback")
	}
	if err := s.requireHostPermission("media.playback.resolve"); err != nil {
		return err
	}
	result, err := s.live().playback.ResolvePlaybackURL(s.live().ctx, req.Input)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

type RenderPageRequest struct {
	Input providers.RenderRequest
}

type SiteRuleFileRequest struct {
	Name string
}

// RendererAvailable 只报可用性，插件据此决定是否展示「浏览器仿真」开关。
func (s *hostServicesServer) RendererAvailable(_ Empty, reply *JSONReply) error {
	if s.live().renderer == nil {
		return fmt.Errorf("宿主未提供 PageRenderer")
	}
	if err := s.requireHostPermission("renderer.page"); err != nil {
		return err
	}
	out, err := encodeJSON(s.live().renderer.RendererAvailable(s.live().ctx))
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) RenderPage(req RenderPageRequest, reply *JSONReply) error {
	if s.live().renderer == nil {
		return fmt.Errorf("宿主未提供 PageRenderer")
	}
	if err := s.requireHostPermission("renderer.page"); err != nil {
		return err
	}
	result, err := s.live().renderer.RenderPage(s.live().ctx, req.Input)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

// CloudCredential 只发短期令牌。实例长期密钥不经过这条通道，插件也无从索取。
func (s *hostServicesServer) CloudCredential(_ Empty, reply *JSONReply) error {
	if s.live().cloud == nil {
		return fmt.Errorf("宿主未提供 CloudIdentity")
	}
	if err := s.requireHostPermission("cloud.identity"); err != nil {
		return err
	}
	result, err := s.live().cloud.CloudCredential(s.live().ctx)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) ListSiteRuleFiles(_ Empty, reply *JSONReply) error {
	if s.live().siteRules == nil {
		return fmt.Errorf("宿主未提供 SiteRuleFiles")
	}
	if err := s.requireHostPermission("site.rules.read"); err != nil {
		return err
	}
	result, err := s.live().siteRules.ListSiteRuleFiles(s.live().ctx)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

// ReadSiteRuleFile 返回原始字节而不是 JSON：规则文件是 YAML，base64 化没有意义，
// 而宿主对文件名的越界校验在实现侧做，不依赖插件自觉。
func (s *hostServicesServer) ReadSiteRuleFile(req SiteRuleFileRequest, reply *BytesReply) error {
	if s.live().siteRules == nil {
		return fmt.Errorf("宿主未提供 SiteRuleFiles")
	}
	if err := s.requireHostPermission("site.rules.read"); err != nil {
		return err
	}
	data, err := s.live().siteRules.ReadSiteRuleFile(s.live().ctx, req.Name)
	if err != nil {
		return err
	}
	reply.Data = data
	return nil
}

// WriteMirror 和 WriteSubtitle 一样只收 FileRef 不收路径：目标存储由用户的插件配置
// 决定，存储内的相对路径由宿主从 FileRef 推导，插件指不了目录也走不出去。
func (s *hostServicesServer) WriteMirror(req MirrorWriteRequest, reply *JSONReply) error {
	if s.live().mirrors == nil {
		return fmt.Errorf("宿主未提供 MediaMirrors")
	}
	if err := s.requireHostPermission("media.mirror.write"); err != nil {
		return err
	}
	result, err := s.live().mirrors.WriteMirror(s.live().ctx, req.Input)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) UpsertSiteAccount(req SiteAccountUpsertRequest, reply *JSONReply) error {
	if s.live().siteAccounts == nil {
		return fmt.Errorf("宿主未提供 SiteAccounts")
	}
	if err := s.requireHostPermission("site.accounts.write"); err != nil {
		return err
	}
	result, err := s.live().siteAccounts.UpsertSiteAccount(s.live().ctx, req.Input)
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

func (s *hostServicesServer) UpsertSubscription(req SubscriptionUpsertRequest, reply *JSONReply) error {
	if s.live().subscriptions == nil {
		return fmt.Errorf("宿主未提供 Subscriptions")
	}
	if err := s.requireSubscriptionPermission(); err != nil {
		return err
	}
	result, err := s.live().subscriptions.UpsertSubscription(s.live().ctx, req.Input)
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

// requireSubscriptionPermission keeps the RPC transport compatible with both
// the formal subscription workflow and legacy import plugins. New manifests
// declare subscriptions.create; only plugins that still declare the removed
// subscriptions.write permission use the legacy path, which the host can
// reject with its explicit deprecation error.
func (s *hostServicesServer) requireSubscriptionPermission() error {
	if s.live().permissions.HasHost("subscriptions.create") {
		return s.requireHostPermission("subscriptions.create")
	}
	return s.requireHostPermission("subscriptions.write")
}

func (s *hostServicesServer) UpsertDownload(req DownloadUpsertRequest, reply *JSONReply) error {
	if s.live().downloads == nil {
		return fmt.Errorf("宿主未提供 Downloads")
	}
	if err := s.requireHostPermission("downloads.write"); err != nil {
		return err
	}
	result, err := s.live().downloads.UpsertDownload(s.live().ctx, req.Input)
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

func (s *hostServicesServer) FindDownloadByHash(req DownloadFindRequest, reply *DownloadFindReply) error {
	if s.live().downloads == nil {
		return fmt.Errorf("宿主未提供 Downloads")
	}
	if err := s.requireHostPermission("downloads.read"); err != nil {
		return err
	}
	result, found, err := s.live().downloads.FindDownloadByHash(s.live().ctx, req.Hash)
	if err != nil {
		return err
	}
	reply.Found = found
	reply.Result = result
	return nil
}

func (s *hostServicesServer) UpsertTransfer(req TransferUpsertRequest, reply *JSONReply) error {
	if s.live().transfers == nil {
		return fmt.Errorf("宿主未提供 Transfers")
	}
	if err := s.requireHostPermission("transfers.write"); err != nil {
		return err
	}
	result, err := s.live().transfers.UpsertTransfer(s.live().ctx, req.Input)
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

func (s *hostServicesServer) GetRuleCatalog(_ Empty, reply *JSONReply) error {
	if s.live().rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.read"); err != nil {
		return err
	}
	result, err := s.live().rules.GetRuleCatalog(s.live().ctx)
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

func (s *hostServicesServer) UpsertRuleProfile(req RuleProfileUpsertRequest, reply *JSONReply) error {
	if s.live().rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.write"); err != nil {
		return err
	}
	result, err := s.live().rules.UpsertRuleProfile(s.live().ctx, req.Input)
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

func (s *hostServicesServer) SetRuleSort(req RuleSortSetRequest, reply *JSONReply) error {
	if s.live().rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.write"); err != nil {
		return err
	}
	result, err := s.live().rules.SetRuleSort(s.live().ctx, req.Input)
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

func (s *hostServicesServer) SetRuleDefault(req RuleDefaultSetRequest, reply *JSONReply) error {
	if s.live().rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.write"); err != nil {
		return err
	}
	result, err := s.live().rules.SetRuleDefault(s.live().ctx, req.Input)
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

func (s *hostServicesServer) DBSelect(req DBStatementRequest, reply *DBQueryReply) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	var query pluginsdk.Select
	if err := decodeDBStatement(req.QueryJSON, &query); err != nil {
		return err
	}
	rows, err := s.live().db.Select(s.live().ctx, query)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	reply.RowsJSON = data
	return nil
}

type LogAttr struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type LogRequest struct {
	Level   pluginsdk.LogLevel `json:"level"`
	Message string             `json:"message"`
	Attrs   []LogAttr          `json:"attrs,omitempty"`
}

func (s *hostServicesServer) Log(req LogRequest, reply *Empty) error {
	if s.live().logger == nil {
		return nil
	}
	attrs := make([]any, 0, len(req.Attrs)*2)
	for _, attr := range req.Attrs {
		if attr.Key == "" {
			continue
		}
		attrs = append(attrs, attr.Key, attr.Value)
	}
	s.live().logger.Log(s.live().ctx, req.Level, req.Message, attrs...)
	return nil
}

func (s *hostServicesServer) requireDataPermission(permission string) error {
	if !s.live().permissions.HasData(permission) {
		return fmt.Errorf("插件未声明权限: data.%s", permission)
	}
	return s.requireUserGrant("data." + permission)
}

func (s *hostServicesServer) requireSecretPermission() error {
	declared := false
	for _, permission := range s.live().permissions.Secrets {
		permission = strings.TrimSpace(strings.TrimPrefix(permission, "secret:"))
		if permission == "" {
			continue
		}
		declared = true
		if err := s.requireUserGrant("secret." + permission); err != nil {
			return err
		}
	}
	if declared {
		return nil
	}
	return fmt.Errorf("插件未声明权限: secret")
}

func (s *hostServicesServer) requireHostPermission(permission string) error {
	if !s.live().permissions.HasHost(permission) {
		return fmt.Errorf("插件未声明权限: host.%s", permission)
	}
	return s.requireUserGrant("host." + permission)
}

func (s *hostServicesServer) requireUserGrant(permission string) error {
	if s.live().permissionChecker == nil {
		return nil
	}
	pluginID := s.live().pluginID
	if pluginID == "" {
		pluginID = s.live().manifest.ID
	}
	return s.live().permissionChecker.CheckPluginPermission(s.live().ctx, pluginID, s.live().scopeType, s.live().scopeID, permission, s.live().manifest)
}

// hostServicesClient 是插件侧全部宿主服务句柄（KV / DB / Logger / Secrets ……）的唯一
// 实现，同时是它们的门面：每个方法都从 target 取**这一次调用**的连接。
//
// 门面这一层是 Provider 池化的关键。Provider 一旦跨调用复用，它构造时存下的那些句柄就
// 会指向一条早已还给通道池、转手服务别的调用的连接。与其要求插件每次调用重新去拿一遍
// 句柄（那要插件实现一个方法，还要它记得一个字段都不能漏），不如让句柄本身保持不变、
// 由 SDK 换掉它背后的连接——插件那边于是什么都不用做。
//
// 没绑连接时（Provider 闲置在池子里、这次调用已经结束）一律返回
// errHostServicesDetached：插件里跑飞的 goroutine 会拿到一个明确的错误，而不是打到下
// 一次调用的连接上。
type hostServicesClient struct {
	target atomic.Pointer[rpc.Client]
}

var errHostServicesDetached = errors.New("宿主服务句柄已失效：这次调用已经结束")

func (c *hostServicesClient) bind(conn *rpc.Client) { c.target.Store(conn) }

func (c *hostServicesClient) detach() { c.target.Store(nil) }

func (c *hostServicesClient) call(method string, args, reply any) error {
	conn := c.target.Load()
	if conn == nil {
		return errHostServicesDetached
	}
	return conn.Call(method, args, reply)
}

func (c *hostServicesClient) HasEntitlement(_ context.Context, feature string) bool {
	var reply EntitlementCheckReply
	return c.call("Plugin.HasEntitlement", EntitlementCheckRequest{Feature: feature}, &reply) == nil && reply.Granted
}

func (c *hostServicesClient) Reveal(ctx context.Context, ref, reason string) (string, error) {
	var reply StringReply
	if err := c.call("Plugin.Reveal", RevealRequest{Ref: ref, Reason: reason}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}

func (c *hostServicesClient) Get(ctx context.Context, key string, out any) (bool, error) {
	var reply KVGetReply
	if err := c.call("Plugin.KVGet", KVGetRequest{Key: key}, &reply); err != nil {
		return false, err
	}
	if !reply.Found {
		return false, nil
	}
	if len(reply.Data) == 0 {
		return true, nil
	}
	if err := json.Unmarshal(reply.Data, out); err != nil {
		return false, err
	}
	return true, nil
}

func (c *hostServicesClient) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var reply Empty
	return c.call("Plugin.KVSet", KVSetRequest{
		Key:        key,
		Data:       data,
		TTLSeconds: int64(ttl / time.Second),
	}, &reply)
}

func (c *hostServicesClient) Delete(ctx context.Context, key string) error {
	var reply Empty
	return c.call("Plugin.KVDelete", KVGetRequest{Key: key}, &reply)
}

func (c *hostServicesClient) DeletePrefix(ctx context.Context, prefix string) error {
	var reply Empty
	return c.call("Plugin.KVDeletePrefix", KVGetRequest{Key: prefix}, &reply)
}

// dbServicesClient 单独承载 PluginDB。它和 hostServicesClient 共用同一条 RPC 连接，
// 分成两个类型只因为方法名会撞：KVStore 和 PluginDB 都有 Delete。
type dbServicesClient struct {
	host *hostServicesClient
}

func (c *dbServicesClient) Select(ctx context.Context, query pluginsdk.Select) ([]map[string]any, error) {
	payload, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	var reply DBQueryReply
	if err := c.host.call("Plugin.DBSelect", DBStatementRequest{QueryJSON: payload}, &reply); err != nil {
		return nil, err
	}
	return decodeDBRows(reply.RowsJSON)
}

func (c *dbServicesClient) Insert(ctx context.Context, query pluginsdk.Insert) (pluginsdk.DBResult, error) {
	return c.dbWrite("Plugin.DBInsert", query)
}

func (c *dbServicesClient) Update(ctx context.Context, query pluginsdk.Update) (pluginsdk.DBResult, error) {
	return c.dbWrite("Plugin.DBUpdate", query)
}

func (c *dbServicesClient) Delete(ctx context.Context, query pluginsdk.Delete) (pluginsdk.DBResult, error) {
	return c.dbWrite("Plugin.DBDelete", query)
}

func (c *dbServicesClient) Batch(ctx context.Context, statements []pluginsdk.Statement) ([]pluginsdk.DBResult, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(statements)
	if err != nil {
		return nil, err
	}
	var reply DBBatchReply
	if err := c.host.call("Plugin.DBBatch", DBStatementRequest{QueryJSON: payload}, &reply); err != nil {
		return nil, err
	}
	results := make([]pluginsdk.DBResult, len(reply.Results))
	for i, item := range reply.Results {
		results[i] = pluginsdk.DBResult{RowsAffected: item.RowsAffected, LastInsertID: item.LastInsertID}
	}
	return results, nil
}

func (c *dbServicesClient) dbWrite(method string, query any) (pluginsdk.DBResult, error) {
	payload, err := json.Marshal(query)
	if err != nil {
		return pluginsdk.DBResult{}, err
	}
	var reply DBWriteReply
	if err := c.host.call(method, DBStatementRequest{QueryJSON: payload}, &reply); err != nil {
		return pluginsdk.DBResult{}, err
	}
	return pluginsdk.DBResult{RowsAffected: reply.RowsAffected, LastInsertID: reply.LastInsertID}, nil
}

func (c *hostServicesClient) CallPluginService(_ context.Context, call pluginsdk.PluginServiceCall) (pluginsdk.PluginServiceResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.CallPluginService", PluginServiceCallRequest{Call: call}, &reply); err != nil {
		return pluginsdk.PluginServiceResult{}, err
	}
	var result pluginsdk.PluginServiceResult
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) WriteSubtitle(_ context.Context, input pluginsdk.SubtitleWrite) (pluginsdk.SubtitleWriteResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.WriteSubtitle", SubtitleWriteRequest{Input: input}, &reply); err != nil {
		return pluginsdk.SubtitleWriteResult{}, err
	}
	var result pluginsdk.SubtitleWriteResult
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) WriteMirror(_ context.Context, input pluginsdk.MirrorWrite) (pluginsdk.MirrorWriteResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.WriteMirror", MirrorWriteRequest{Input: input}, &reply); err != nil {
		return pluginsdk.MirrorWriteResult{}, err
	}
	var result pluginsdk.MirrorWriteResult
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) ResolvePlaybackURL(_ context.Context, input pluginsdk.PlaybackResolveInput) (pluginsdk.PlaybackResolveResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.ResolvePlaybackURL", PlaybackResolveRequest{Input: input}, &reply); err != nil {
		return pluginsdk.PlaybackResolveResult{}, err
	}
	var result pluginsdk.PlaybackResolveResult
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) RendererAvailable(_ context.Context) bool {
	var reply JSONReply
	if err := c.call("Plugin.RendererAvailable", Empty{}, &reply); err != nil {
		return false
	}
	var available bool
	if err := decodeJSON(reply.Data, &available); err != nil {
		return false
	}
	return available
}

func (c *hostServicesClient) RenderPage(_ context.Context, req providers.RenderRequest) (providers.RenderResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.RenderPage", RenderPageRequest{Input: req}, &reply); err != nil {
		return providers.RenderResult{}, err
	}
	var result providers.RenderResult
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) CloudCredential(_ context.Context) (pluginsdk.CloudCredential, error) {
	var reply JSONReply
	if err := c.call("Plugin.CloudCredential", Empty{}, &reply); err != nil {
		return pluginsdk.CloudCredential{}, err
	}
	var result pluginsdk.CloudCredential
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) ListSiteRuleFiles(_ context.Context) ([]string, error) {
	var reply JSONReply
	if err := c.call("Plugin.ListSiteRuleFiles", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []string
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) ReadSiteRuleFile(_ context.Context, name string) ([]byte, error) {
	var reply BytesReply
	if err := c.call("Plugin.ReadSiteRuleFile", SiteRuleFileRequest{Name: name}, &reply); err != nil {
		return nil, err
	}
	return reply.Data, nil
}

func (c *hostServicesClient) ListSiteAccounts(_ context.Context) ([]pluginsdk.SiteAccountInfo, error) {
	var reply JSONReply
	if err := c.call("Plugin.ListSiteAccounts", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.SiteAccountInfo
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) UpsertSiteAccount(ctx context.Context, input pluginsdk.SiteAccountWrite) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.UpsertSiteAccount", SiteAccountUpsertRequest{Input: input}, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	var result pluginsdk.HostWriteResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) UpsertSubscription(ctx context.Context, input pluginsdk.SubscriptionWrite) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.UpsertSubscription", SubscriptionUpsertRequest{Input: input}, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	var result pluginsdk.HostWriteResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) UpsertDownload(ctx context.Context, input pluginsdk.DownloadWrite) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.UpsertDownload", DownloadUpsertRequest{Input: input}, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	var result pluginsdk.HostWriteResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) FindDownloadByHash(ctx context.Context, hash string) (pluginsdk.HostWriteResult, bool, error) {
	var reply DownloadFindReply
	if err := c.call("Plugin.FindDownloadByHash", DownloadFindRequest{Hash: hash}, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, false, err
	}
	return reply.Result, reply.Found, nil
}

func (c *hostServicesClient) UpsertTransfer(ctx context.Context, input pluginsdk.TransferWrite) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.UpsertTransfer", TransferUpsertRequest{Input: input}, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	var result pluginsdk.HostWriteResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) GetRuleCatalog(ctx context.Context) (pluginsdk.RuleCatalog, error) {
	var reply JSONReply
	if err := c.call("Plugin.GetRuleCatalog", Empty{}, &reply); err != nil {
		return pluginsdk.RuleCatalog{}, err
	}
	var result pluginsdk.RuleCatalog
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.RuleCatalog{}, err
	}
	return result, nil
}

func (c *hostServicesClient) UpsertRuleProfile(ctx context.Context, input pluginsdk.RuleProfileWrite) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.UpsertRuleProfile", RuleProfileUpsertRequest{Input: input}, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	var result pluginsdk.HostWriteResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) SetRuleSort(ctx context.Context, input pluginsdk.RuleSortWrite) (pluginsdk.RuleSortResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.SetRuleSort", RuleSortSetRequest{Input: input}, &reply); err != nil {
		return pluginsdk.RuleSortResult{}, err
	}
	var result pluginsdk.RuleSortResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.RuleSortResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) SetRuleDefault(ctx context.Context, input pluginsdk.RuleDefaultWrite) (pluginsdk.RuleDefaultResult, error) {
	var reply JSONReply
	if err := c.call("Plugin.SetRuleDefault", RuleDefaultSetRequest{Input: input}, &reply); err != nil {
		return pluginsdk.RuleDefaultResult{}, err
	}
	var result pluginsdk.RuleDefaultResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.RuleDefaultResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) hostWriteCall(method string, input any) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.call(method, input, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	var result pluginsdk.HostWriteResult
	if err := decodeJSON(reply.Data, &result); err != nil {
		return pluginsdk.HostWriteResult{}, err
	}
	return result, nil
}

func (c *hostServicesClient) ListConnections(_ context.Context, section string) ([]pluginsdk.Connection, error) {
	var reply JSONReply
	if err := c.call("Plugin.ListConnections", ConnectionListRequest{Section: section}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.Connection
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetConnection(_ context.Context, section, id string) (pluginsdk.Connection, error) {
	var reply JSONReply
	if err := c.call("Plugin.GetConnection", ConnectionGetRequest{Section: section, ID: id}, &reply); err != nil {
		return pluginsdk.Connection{}, err
	}
	var result pluginsdk.Connection
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) RevealConnectionCredential(_ context.Context, section, id, field, reason string) (string, error) {
	var reply StringReply
	if err := c.call("Plugin.RevealConnectionCredential", ConnectionCredentialRequest{Section: section, ID: id, Field: field, Reason: reason}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}
func (c *hostServicesClient) UpsertConnection(_ context.Context, input pluginsdk.ConnectionWrite) (pluginsdk.HostWriteResult, error) {
	return c.hostWriteCall("Plugin.UpsertConnection", ConnectionUpsertRequest{Input: input})
}
func (c *hostServicesClient) ListStorages(_ context.Context) ([]pluginsdk.Storage, error) {
	var reply JSONReply
	if err := c.call("Plugin.ListStorages", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.Storage
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetStorage(_ context.Context, id string) (pluginsdk.Storage, error) {
	var reply JSONReply
	if err := c.call("Plugin.GetStorage", StorageGetRequest{ID: id}, &reply); err != nil {
		return pluginsdk.Storage{}, err
	}
	var result pluginsdk.Storage
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) UpsertStorage(_ context.Context, input pluginsdk.StorageWrite) (pluginsdk.HostWriteResult, error) {
	return c.hostWriteCall("Plugin.UpsertStorage", StorageUpsertRequest{Input: input})
}
func (c *hostServicesClient) ListDirectoryMappings(_ context.Context) ([]pluginsdk.DirectoryMapping, error) {
	var reply JSONReply
	if err := c.call("Plugin.ListDirectoryMappings", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.DirectoryMapping
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetDirectoryMapping(_ context.Context, id string) (pluginsdk.DirectoryMapping, error) {
	var reply JSONReply
	if err := c.call("Plugin.GetDirectoryMapping", DirectoryMappingGetRequest{ID: id}, &reply); err != nil {
		return pluginsdk.DirectoryMapping{}, err
	}
	var result pluginsdk.DirectoryMapping
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) UpsertDirectoryMapping(_ context.Context, input pluginsdk.DirectoryMappingWrite) (pluginsdk.HostWriteResult, error) {
	return c.hostWriteCall("Plugin.UpsertDirectoryMapping", DirectoryMappingUpsertRequest{Input: input})
}
func (c *hostServicesClient) setting(key string) ([]byte, bool, error) {
	var reply SettingGetReply
	if err := c.call("Plugin.GetSetting", SettingGetRequest{Key: key}, &reply); err != nil {
		return nil, false, err
	}
	return reply.Value, reply.Found, nil
}
func (c *hostServicesClient) String(_ context.Context, key string) (string, bool) {
	raw, found, err := c.setting(key)
	if err != nil || !found {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}
func (c *hostServicesClient) Int(_ context.Context, key string) (int64, bool) {
	raw, found, err := c.setting(key)
	if err != nil || !found {
		return 0, false
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	return value, true
}
func (c *hostServicesClient) Bool(_ context.Context, key string) (bool, bool) {
	raw, found, err := c.setting(key)
	if err != nil || !found {
		return false, false
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return value, true
}
func (c *hostServicesClient) JSON(_ context.Context, key string, out any) (bool, error) {
	raw, found, err := c.setting(key)
	if err != nil || !found {
		return false, err
	}
	return true, json.Unmarshal(raw, out)
}
func (c *hostServicesClient) SetSetting(_ context.Context, input pluginsdk.SettingWrite) (pluginsdk.HostWriteResult, error) {
	return c.hostWriteCall("Plugin.SetSetting", SettingSetRequest{Input: input})
}
func (c *hostServicesClient) ListSchedules(_ context.Context) ([]pluginsdk.Schedule, error) {
	var reply JSONReply
	if err := c.call("Plugin.ListSchedules", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.Schedule
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetSchedule(_ context.Context, taskType string) (pluginsdk.Schedule, error) {
	var reply JSONReply
	if err := c.call("Plugin.GetSchedule", ScheduleGetRequest{TaskType: taskType}, &reply); err != nil {
		return pluginsdk.Schedule{}, err
	}
	var result pluginsdk.Schedule
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) SetSchedule(_ context.Context, input pluginsdk.ScheduleWrite) (pluginsdk.HostWriteResult, error) {
	return c.hostWriteCall("Plugin.SetSchedule", ScheduleSetRequest{Input: input})
}

func (c *hostServicesClient) Log(ctx context.Context, level pluginsdk.LogLevel, message string, attrs ...any) {
	var reply Empty
	_ = c.call("Plugin.Log", LogRequest{
		Level:   level,
		Message: message,
		Attrs:   logAttrs(attrs),
	}, &reply)
}

func (c *hostServicesClient) Debug(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, pluginsdk.LogLevelDebug, message, attrs...)
}

func (c *hostServicesClient) Info(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, pluginsdk.LogLevelInfo, message, attrs...)
}

func (c *hostServicesClient) Warn(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, pluginsdk.LogLevelWarn, message, attrs...)
}

func (c *hostServicesClient) Error(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, pluginsdk.LogLevelError, message, attrs...)
}

// runtimeFeedbackClient adapts the Runtime SDK feedback levels to the legacy
// logger RPC while keeping Toast and Notify on the same host-services channel.
type runtimeFeedbackClient struct{ host *hostServicesClient }

func (c *runtimeFeedbackClient) Log(ctx context.Context, level runtimesdk.LogLevel, message string, attrs ...any) {
	c.host.Log(ctx, pluginsdk.LogLevel(level), message, attrs...)
}
func (c *runtimeFeedbackClient) Debug(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, runtimesdk.LogDebug, message, attrs...)
}
func (c *runtimeFeedbackClient) Info(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, runtimesdk.LogInfo, message, attrs...)
}
func (c *runtimeFeedbackClient) Warn(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, runtimesdk.LogWarn, message, attrs...)
}
func (c *runtimeFeedbackClient) Error(ctx context.Context, message string, attrs ...any) {
	c.Log(ctx, runtimesdk.LogError, message, attrs...)
}
func (c *runtimeFeedbackClient) Toast(context.Context, runtimesdk.ToastInput) error {
	return fmt.Errorf("宿主尚未提供 Toast 能力")
}
func (c *runtimeFeedbackClient) Notify(context.Context, runtimesdk.NotificationInput) error {
	return fmt.Errorf("宿主尚未提供通知能力")
}

func logAttrs(attrs []any) []LogAttr {
	out := make([]LogAttr, 0, len(attrs)/2)
	for i := 0; i < len(attrs); i += 2 {
		key := fmt.Sprint(attrs[i])
		if key == "" {
			continue
		}
		var value any = "<missing>"
		if i+1 < len(attrs) {
			value = jsonSafeValue(attrs[i+1])
		}
		out = append(out, LogAttr{Key: key, Value: value})
	}
	return out
}

// decodeDBStatement 还原一条结构化语句。UseNumber 保住整数精度：绑定值经
// JSON 往返后如果退化成 float64，写进 INTEGER 列的大整数会丢低位。
func decodeDBStatement(data []byte, out any) error {
	if len(data) == 0 {
		return fmt.Errorf("插件数据库语句为空")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(out)
}

func decodeDBRows(data []byte) ([]map[string]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var out []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func jsonSafeValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return fmt.Sprint(value)
	}
	return out
}
