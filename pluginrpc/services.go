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
