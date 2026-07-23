package pluginrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/rpc"
	"strings"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
	runtimesdk "github.com/chenbstack/media-agent-plugin-sdk-go/runtime"
)

type hostServicesServer struct {
	ctx               context.Context
	pluginID          string
	scopeType         string
	scopeID           string
	manifest          pluginsdk.Manifest
	permissions       pluginsdk.Permissions
	permissionChecker PermissionChecker
	secrets           pluginsdk.SecretResolver
	kv                pluginsdk.KVStore
	db                pluginsdk.PluginDB
	logger            pluginsdk.Logger
	siteAccounts      pluginsdk.SiteAccounts
	subscriptions     pluginsdk.Subscriptions
	downloads         pluginsdk.Downloads
	transfers         pluginsdk.Transfers
	rules             pluginsdk.Rules
	connections       pluginsdk.Connections
	storages          pluginsdk.Storages
	schedules         pluginsdk.Schedules
	settings          pluginsdk.Settings
}

type RevealRequest struct {
	Ref    string
	Reason string
}

func (s *hostServicesServer) Reveal(req RevealRequest, reply *StringReply) error {
	if s.secrets == nil {
		return fmt.Errorf("宿主未提供 SecretResolver")
	}
	if err := s.requireSecretPermission(); err != nil {
		return err
	}
	value, err := s.secrets.Reveal(s.ctx, req.Ref, req.Reason)
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
	if s.kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	var raw json.RawMessage
	found, err := s.kv.Get(s.ctx, req.Key, &raw)
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
	if s.kv == nil {
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
	return s.kv.Set(s.ctx, req.Key, value, time.Duration(req.TTLSeconds)*time.Second)
}

func (s *hostServicesServer) KVDelete(req KVGetRequest, reply *Empty) error {
	if s.kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	return s.kv.Delete(s.ctx, req.Key)
}

func (s *hostServicesServer) KVDeletePrefix(req KVGetRequest, reply *Empty) error {
	if s.kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	return s.kv.DeletePrefix(s.ctx, req.Key)
}

type DBTableNameRequest struct {
	LogicalName string
}

func (s *hostServicesServer) DBTableName(req DBTableNameRequest, reply *StringReply) error {
	if s.db == nil {
		return fmt.Errorf("宿主未提供 PluginDB")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	name, err := s.db.TableName(req.LogicalName)
	if err != nil {
		return err
	}
	reply.Value = name
	return nil
}

type DBExecRequest struct {
	Statement string
	ArgsJSON  []byte
}

type DBExecReply struct {
	RowsAffected int64
	LastInsertID int64
}

func (s *hostServicesServer) DBExec(req DBExecRequest, reply *DBExecReply) error {
	if s.db == nil {
		return fmt.Errorf("宿主未提供 PluginDB")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	args, err := decodeDBArgs(req.ArgsJSON)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(s.ctx, req.Statement, args...)
	if err != nil {
		return err
	}
	reply.RowsAffected = result.RowsAffected
	reply.LastInsertID = result.LastInsertID
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
	if s.connections == nil {
		return fmt.Errorf("宿主未提供 Connections")
	}
	if err := s.requireHostPermission("connections.read"); err != nil {
		return err
	}
	result, err := s.connections.ListConnections(s.ctx, req.Section)
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
	if s.connections == nil {
		return fmt.Errorf("宿主未提供 Connections")
	}
	if err := s.requireHostPermission("connections.read"); err != nil {
		return err
	}
	result, err := s.connections.GetConnection(s.ctx, req.Section, req.ID)
	if err != nil {
		return err
	}
	out, err := encodeJSON(result)
	if err == nil {
		*reply = out
	}
	return err
}

func (s *hostServicesServer) UpsertConnection(req ConnectionUpsertRequest, reply *JSONReply) error {
	if s.connections == nil {
		return fmt.Errorf("宿主未提供 Connections")
	}
	return s.hostWriteResult("connections.write", func() (pluginsdk.HostWriteResult, error) {
		return s.connections.UpsertConnection(s.ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListStorages(_ Empty, reply *JSONReply) error {
	if s.storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.storages.ListStorages(s.ctx)
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
	if s.storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.storages.GetStorage(s.ctx, req.ID)
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
	if s.storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	return s.hostWriteResult("storages.write", func() (pluginsdk.HostWriteResult, error) {
		return s.storages.UpsertStorage(s.ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListDirectoryMappings(_ Empty, reply *JSONReply) error {
	if s.storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.storages.ListDirectoryMappings(s.ctx)
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
	if s.storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	if err := s.requireHostPermission("storages.read"); err != nil {
		return err
	}
	result, err := s.storages.GetDirectoryMapping(s.ctx, req.ID)
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
	if s.storages == nil {
		return fmt.Errorf("宿主未提供 Storages")
	}
	return s.hostWriteResult("storages.write", func() (pluginsdk.HostWriteResult, error) {
		return s.storages.UpsertDirectoryMapping(s.ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) GetSetting(req SettingGetRequest, reply *SettingGetReply) error {
	if s.settings == nil {
		return fmt.Errorf("宿主未提供 Settings")
	}
	if err := s.requireHostPermission("settings.read"); err != nil {
		return err
	}
	var raw json.RawMessage
	found, err := s.settings.JSON(s.ctx, req.Key, &raw)
	if err != nil {
		return err
	}
	reply.Found, reply.Value = found, append([]byte(nil), raw...)
	return nil
}

func (s *hostServicesServer) SetSetting(req SettingSetRequest, reply *JSONReply) error {
	if s.settings == nil {
		return fmt.Errorf("宿主未提供 Settings")
	}
	return s.hostWriteResult("settings.write", func() (pluginsdk.HostWriteResult, error) {
		return s.settings.SetSetting(s.ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListSchedules(_ Empty, reply *JSONReply) error {
	if s.schedules == nil {
		return fmt.Errorf("宿主未提供 Schedules")
	}
	if err := s.requireHostPermission("schedules.read"); err != nil {
		return err
	}
	result, err := s.schedules.ListSchedules(s.ctx)
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
	if s.schedules == nil {
		return fmt.Errorf("宿主未提供 Schedules")
	}
	if err := s.requireHostPermission("schedules.read"); err != nil {
		return err
	}
	result, err := s.schedules.GetSchedule(s.ctx, req.TaskType)
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
	if s.schedules == nil {
		return fmt.Errorf("宿主未提供 Schedules")
	}
	return s.hostWriteResult("schedules.write", func() (pluginsdk.HostWriteResult, error) {
		return s.schedules.SetSchedule(s.ctx, req.Input)
	}, reply)
}

func (s *hostServicesServer) ListSiteAccounts(_ Empty, reply *JSONReply) error {
	if s.siteAccounts == nil {
		return fmt.Errorf("宿主未提供 SiteAccounts")
	}
	if err := s.requireHostPermission("site.accounts.read"); err != nil {
		return err
	}
	result, err := s.siteAccounts.ListSiteAccounts(s.ctx)
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
	if s.siteAccounts == nil {
		return fmt.Errorf("宿主未提供 SiteAccounts")
	}
	if err := s.requireHostPermission("site.accounts.write"); err != nil {
		return err
	}
	result, err := s.siteAccounts.UpsertSiteAccount(s.ctx, req.Input)
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
	if s.subscriptions == nil {
		return fmt.Errorf("宿主未提供 Subscriptions")
	}
	if err := s.requireHostPermission("subscriptions.write"); err != nil {
		return err
	}
	result, err := s.subscriptions.UpsertSubscription(s.ctx, req.Input)
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

func (s *hostServicesServer) UpsertDownload(req DownloadUpsertRequest, reply *JSONReply) error {
	if s.downloads == nil {
		return fmt.Errorf("宿主未提供 Downloads")
	}
	if err := s.requireHostPermission("downloads.write"); err != nil {
		return err
	}
	result, err := s.downloads.UpsertDownload(s.ctx, req.Input)
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
	if s.downloads == nil {
		return fmt.Errorf("宿主未提供 Downloads")
	}
	if err := s.requireHostPermission("downloads.read"); err != nil {
		return err
	}
	result, found, err := s.downloads.FindDownloadByHash(s.ctx, req.Hash)
	if err != nil {
		return err
	}
	reply.Found = found
	reply.Result = result
	return nil
}

func (s *hostServicesServer) UpsertTransfer(req TransferUpsertRequest, reply *JSONReply) error {
	if s.transfers == nil {
		return fmt.Errorf("宿主未提供 Transfers")
	}
	if err := s.requireHostPermission("transfers.write"); err != nil {
		return err
	}
	result, err := s.transfers.UpsertTransfer(s.ctx, req.Input)
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
	if s.rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.read"); err != nil {
		return err
	}
	result, err := s.rules.GetRuleCatalog(s.ctx)
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
	if s.rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.write"); err != nil {
		return err
	}
	result, err := s.rules.UpsertRuleProfile(s.ctx, req.Input)
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
	if s.rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.write"); err != nil {
		return err
	}
	result, err := s.rules.SetRuleSort(s.ctx, req.Input)
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
	if s.rules == nil {
		return fmt.Errorf("宿主未提供 Rules")
	}
	if err := s.requireHostPermission("rules.write"); err != nil {
		return err
	}
	result, err := s.rules.SetRuleDefault(s.ctx, req.Input)
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

func (s *hostServicesServer) DBQuery(req DBExecRequest, reply *DBQueryReply) error {
	if s.db == nil {
		return fmt.Errorf("宿主未提供 PluginDB")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	args, err := decodeDBArgs(req.ArgsJSON)
	if err != nil {
		return err
	}
	rows, err := s.db.Query(s.ctx, req.Statement, args...)
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
	if s.logger == nil {
		return nil
	}
	attrs := make([]any, 0, len(req.Attrs)*2)
	for _, attr := range req.Attrs {
		if attr.Key == "" {
			continue
		}
		attrs = append(attrs, attr.Key, attr.Value)
	}
	s.logger.Log(s.ctx, req.Level, req.Message, attrs...)
	return nil
}

func (s *hostServicesServer) requireDataPermission(permission string) error {
	if !s.permissions.HasData(permission) {
		return fmt.Errorf("插件未声明权限: data.%s", permission)
	}
	return s.requireUserGrant("data." + permission)
}

func (s *hostServicesServer) requireSecretPermission() error {
	declared := false
	for _, permission := range s.permissions.Secrets {
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
	if !s.permissions.HasHost(permission) {
		return fmt.Errorf("插件未声明权限: host.%s", permission)
	}
	return s.requireUserGrant("host." + permission)
}

func (s *hostServicesServer) requireUserGrant(permission string) error {
	if s.permissionChecker == nil {
		return nil
	}
	pluginID := s.pluginID
	if pluginID == "" {
		pluginID = s.manifest.ID
	}
	return s.permissionChecker.CheckPluginPermission(s.ctx, pluginID, s.scopeType, s.scopeID, permission, s.manifest)
}

type hostServicesClient struct {
	client *rpc.Client
}

func (c *hostServicesClient) Close() error {
	return c.client.Close()
}

func (c *hostServicesClient) Reveal(ctx context.Context, ref, reason string) (string, error) {
	var reply StringReply
	if err := c.client.Call("Plugin.Reveal", RevealRequest{Ref: ref, Reason: reason}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}

func (c *hostServicesClient) Get(ctx context.Context, key string, out any) (bool, error) {
	var reply KVGetReply
	if err := c.client.Call("Plugin.KVGet", KVGetRequest{Key: key}, &reply); err != nil {
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
	return c.client.Call("Plugin.KVSet", KVSetRequest{
		Key:        key,
		Data:       data,
		TTLSeconds: int64(ttl / time.Second),
	}, &reply)
}

func (c *hostServicesClient) Delete(ctx context.Context, key string) error {
	var reply Empty
	return c.client.Call("Plugin.KVDelete", KVGetRequest{Key: key}, &reply)
}

func (c *hostServicesClient) DeletePrefix(ctx context.Context, prefix string) error {
	var reply Empty
	return c.client.Call("Plugin.KVDeletePrefix", KVGetRequest{Key: prefix}, &reply)
}

func (c *hostServicesClient) TableName(logicalName string) (string, error) {
	var reply StringReply
	if err := c.client.Call("Plugin.DBTableName", DBTableNameRequest{LogicalName: logicalName}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}

func (c *hostServicesClient) Exec(ctx context.Context, statement string, args ...any) (pluginsdk.DBResult, error) {
	argsJSON, err := encodeDBArgs(args)
	if err != nil {
		return pluginsdk.DBResult{}, err
	}
	var reply DBExecReply
	if err := c.client.Call("Plugin.DBExec", DBExecRequest{Statement: statement, ArgsJSON: argsJSON}, &reply); err != nil {
		return pluginsdk.DBResult{}, err
	}
	return pluginsdk.DBResult{RowsAffected: reply.RowsAffected, LastInsertID: reply.LastInsertID}, nil
}

func (c *hostServicesClient) Query(ctx context.Context, statement string, args ...any) ([]map[string]any, error) {
	argsJSON, err := encodeDBArgs(args)
	if err != nil {
		return nil, err
	}
	var reply DBQueryReply
	if err := c.client.Call("Plugin.DBQuery", DBExecRequest{Statement: statement, ArgsJSON: argsJSON}, &reply); err != nil {
		return nil, err
	}
	return decodeDBRows(reply.RowsJSON)
}

func (c *hostServicesClient) ListSiteAccounts(_ context.Context) ([]pluginsdk.SiteAccountInfo, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.ListSiteAccounts", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.SiteAccountInfo
	return result, decodeJSON(reply.Data, &result)
}

func (c *hostServicesClient) UpsertSiteAccount(ctx context.Context, input pluginsdk.SiteAccountWrite) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.UpsertSiteAccount", SiteAccountUpsertRequest{Input: input}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.UpsertSubscription", SubscriptionUpsertRequest{Input: input}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.UpsertDownload", DownloadUpsertRequest{Input: input}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.FindDownloadByHash", DownloadFindRequest{Hash: hash}, &reply); err != nil {
		return pluginsdk.HostWriteResult{}, false, err
	}
	return reply.Result, reply.Found, nil
}

func (c *hostServicesClient) UpsertTransfer(ctx context.Context, input pluginsdk.TransferWrite) (pluginsdk.HostWriteResult, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.UpsertTransfer", TransferUpsertRequest{Input: input}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.GetRuleCatalog", Empty{}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.UpsertRuleProfile", RuleProfileUpsertRequest{Input: input}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.SetRuleSort", RuleSortSetRequest{Input: input}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.SetRuleDefault", RuleDefaultSetRequest{Input: input}, &reply); err != nil {
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
	if err := c.client.Call(method, input, &reply); err != nil {
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
	if err := c.client.Call("Plugin.ListConnections", ConnectionListRequest{Section: section}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.Connection
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetConnection(_ context.Context, section, id string) (pluginsdk.Connection, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.GetConnection", ConnectionGetRequest{Section: section, ID: id}, &reply); err != nil {
		return pluginsdk.Connection{}, err
	}
	var result pluginsdk.Connection
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) UpsertConnection(_ context.Context, input pluginsdk.ConnectionWrite) (pluginsdk.HostWriteResult, error) {
	return c.hostWriteCall("Plugin.UpsertConnection", ConnectionUpsertRequest{Input: input})
}
func (c *hostServicesClient) ListStorages(_ context.Context) ([]pluginsdk.Storage, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.ListStorages", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.Storage
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetStorage(_ context.Context, id string) (pluginsdk.Storage, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.GetStorage", StorageGetRequest{ID: id}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.ListDirectoryMappings", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.DirectoryMapping
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetDirectoryMapping(_ context.Context, id string) (pluginsdk.DirectoryMapping, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.GetDirectoryMapping", DirectoryMappingGetRequest{ID: id}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.GetSetting", SettingGetRequest{Key: key}, &reply); err != nil {
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
	if err := c.client.Call("Plugin.ListSchedules", Empty{}, &reply); err != nil {
		return nil, err
	}
	var result []pluginsdk.Schedule
	return result, decodeJSON(reply.Data, &result)
}
func (c *hostServicesClient) GetSchedule(_ context.Context, taskType string) (pluginsdk.Schedule, error) {
	var reply JSONReply
	if err := c.client.Call("Plugin.GetSchedule", ScheduleGetRequest{TaskType: taskType}, &reply); err != nil {
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
	_ = c.client.Call("Plugin.Log", LogRequest{
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

func encodeDBArgs(args []any) ([]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	return json.Marshal(args)
}

func decodeDBArgs(data []byte) ([]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var out []any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
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
