package pluginrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/rpc"
	"strings"
	"time"

	"media-agent-lab/server/pkg/pluginsdk"
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
}

type revealRequest struct {
	Ref    string
	Reason string
}

func (s *hostServicesServer) Reveal(req revealRequest, reply *StringReply) error {
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

type kvGetRequest struct {
	Key string
}

type kvGetReply struct {
	Found bool
	Data  []byte
}

func (s *hostServicesServer) KVGet(req kvGetRequest, reply *kvGetReply) error {
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

type kvSetRequest struct {
	Key        string
	Data       []byte
	TTLSeconds int64
}

func (s *hostServicesServer) KVSet(req kvSetRequest, reply *Empty) error {
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

func (s *hostServicesServer) KVDelete(req kvGetRequest, reply *Empty) error {
	if s.kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	return s.kv.Delete(s.ctx, req.Key)
}

func (s *hostServicesServer) KVDeletePrefix(req kvGetRequest, reply *Empty) error {
	if s.kv == nil {
		return fmt.Errorf("宿主未提供 KVStore")
	}
	if err := s.requireDataPermission("storage"); err != nil {
		return err
	}
	return s.kv.DeletePrefix(s.ctx, req.Key)
}

type dbTableNameRequest struct {
	LogicalName string
}

func (s *hostServicesServer) DBTableName(req dbTableNameRequest, reply *StringReply) error {
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

type dbExecRequest struct {
	Statement string
	ArgsJSON  []byte
}

type dbExecReply struct {
	RowsAffected int64
	LastInsertID int64
}

func (s *hostServicesServer) DBExec(req dbExecRequest, reply *dbExecReply) error {
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

type dbQueryReply struct {
	RowsJSON []byte
}

func (s *hostServicesServer) DBQuery(req dbExecRequest, reply *dbQueryReply) error {
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

type logAttr struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type logRequest struct {
	Level   pluginsdk.LogLevel `json:"level"`
	Message string             `json:"message"`
	Attrs   []logAttr          `json:"attrs,omitempty"`
}

func (s *hostServicesServer) Log(req logRequest, reply *Empty) error {
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
	if err := c.client.Call("Plugin.Reveal", revealRequest{Ref: ref, Reason: reason}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}

func (c *hostServicesClient) Get(ctx context.Context, key string, out any) (bool, error) {
	var reply kvGetReply
	if err := c.client.Call("Plugin.KVGet", kvGetRequest{Key: key}, &reply); err != nil {
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
	return c.client.Call("Plugin.KVSet", kvSetRequest{
		Key:        key,
		Data:       data,
		TTLSeconds: int64(ttl / time.Second),
	}, &reply)
}

func (c *hostServicesClient) Delete(ctx context.Context, key string) error {
	var reply Empty
	return c.client.Call("Plugin.KVDelete", kvGetRequest{Key: key}, &reply)
}

func (c *hostServicesClient) DeletePrefix(ctx context.Context, prefix string) error {
	var reply Empty
	return c.client.Call("Plugin.KVDeletePrefix", kvGetRequest{Key: prefix}, &reply)
}

func (c *hostServicesClient) TableName(logicalName string) (string, error) {
	var reply StringReply
	if err := c.client.Call("Plugin.DBTableName", dbTableNameRequest{LogicalName: logicalName}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}

func (c *hostServicesClient) Exec(ctx context.Context, statement string, args ...any) (pluginsdk.DBResult, error) {
	argsJSON, err := encodeDBArgs(args)
	if err != nil {
		return pluginsdk.DBResult{}, err
	}
	var reply dbExecReply
	if err := c.client.Call("Plugin.DBExec", dbExecRequest{Statement: statement, ArgsJSON: argsJSON}, &reply); err != nil {
		return pluginsdk.DBResult{}, err
	}
	return pluginsdk.DBResult{RowsAffected: reply.RowsAffected, LastInsertID: reply.LastInsertID}, nil
}

func (c *hostServicesClient) Query(ctx context.Context, statement string, args ...any) ([]map[string]any, error) {
	argsJSON, err := encodeDBArgs(args)
	if err != nil {
		return nil, err
	}
	var reply dbQueryReply
	if err := c.client.Call("Plugin.DBQuery", dbExecRequest{Statement: statement, ArgsJSON: argsJSON}, &reply); err != nil {
		return nil, err
	}
	return decodeDBRows(reply.RowsJSON)
}

func (c *hostServicesClient) Log(ctx context.Context, level pluginsdk.LogLevel, message string, attrs ...any) {
	var reply Empty
	_ = c.client.Call("Plugin.Log", logRequest{
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

func logAttrs(attrs []any) []logAttr {
	out := make([]logAttr, 0, len(attrs)/2)
	for i := 0; i < len(attrs); i += 2 {
		key := fmt.Sprint(attrs[i])
		if key == "" {
			continue
		}
		var value any = "<missing>"
		if i+1 < len(attrs) {
			value = jsonSafeValue(attrs[i+1])
		}
		out = append(out, logAttr{Key: key, Value: value})
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
