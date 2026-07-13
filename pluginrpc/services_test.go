package pluginrpc

import (
	"context"
	"encoding/json"
	"net"
	"net/rpc"
	"testing"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
)

func TestDomainCapabilitiesRoundTripRPC(t *testing.T) {
	resources := memoryResources{}
	server := rpc.NewServer()
	target := &hostServicesServer{
		ctx: context.Background(), connections: resources, storages: resources, schedules: resources, settings: resources,
		permissions: pluginsdk.Permissions{Host: []string{"connections.read", "connections.write", "storages.read", "storages.write", "schedules.read", "schedules.write", "settings.read", "settings.write"}},
	}
	if err := server.RegisterName("Plugin", target); err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go server.ServeConn(serverConn)
	rpcClient := rpc.NewClient(clientConn)
	defer rpcClient.Close()
	client := &hostServicesClient{client: rpcClient}

	connections, err := client.ListConnections(t.Context(), "downloaders")
	if err != nil || len(connections) != 1 || connections[0].ID != "connection-1" {
		t.Fatalf("ListConnections = %+v, %v", connections, err)
	}
	storages, err := client.ListStorages(t.Context())
	if err != nil || len(storages) != 1 || storages[0].ID != "storage-1" {
		t.Fatalf("ListStorages = %+v, %v", storages, err)
	}
	directories, err := client.ListDirectoryMappings(t.Context())
	if err != nil || len(directories) != 1 || directories[0].ID != "directory-1" {
		t.Fatalf("ListDirectoryMappings = %+v, %v", directories, err)
	}
	schedules, err := client.ListSchedules(t.Context())
	if err != nil || len(schedules) != 1 || schedules[0].TaskType != "task-1" {
		t.Fatalf("ListSchedules = %+v, %v", schedules, err)
	}
	if enabled, ok := client.Bool(t.Context(), "enabled"); !ok || !enabled {
		t.Fatalf("Settings.Bool = %v/%v", enabled, ok)
	}
	if _, err := client.SetSetting(t.Context(), pluginsdk.SettingWrite{Key: "enabled", Value: json.RawMessage(`true`)}); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
}

func TestHostServicesRequireKVPermission(t *testing.T) {
	server := hostServicesServer{
		ctx: context.Background(),
		kv:  memoryKV{},
	}

	var reply KVGetReply
	if err := server.KVGet(KVGetRequest{Key: "token"}, &reply); err == nil {
		t.Fatal("expected KVGet without data.storage permission to fail")
	}

	server.permissions.Data = []string{"storage"}
	if err := server.KVGet(KVGetRequest{Key: "token"}, &reply); err != nil {
		t.Fatalf("KVGet with data.storage permission: %v", err)
	}
}

func TestHostServicesRequirePrivateDBPermission(t *testing.T) {
	server := hostServicesServer{
		ctx: context.Background(),
		db:  memoryDB{},
	}

	var reply StringReply
	if err := server.DBTableName(DBTableNameRequest{LogicalName: "files"}, &reply); err == nil {
		t.Fatal("expected DBTableName without data.storage permission to fail")
	}

	server.permissions.Data = []string{"storage"}
	if err := server.DBTableName(DBTableNameRequest{LogicalName: "files"}, &reply); err != nil {
		t.Fatalf("DBTableName with data.storage permission: %v", err)
	}
	if reply.Value != "files" {
		t.Fatalf("table name = %q", reply.Value)
	}
}

func TestHostServicesRequireSecretPermission(t *testing.T) {
	server := hostServicesServer{
		ctx:     context.Background(),
		secrets: staticSecretResolver("secret-value"),
	}

	var reply StringReply
	if err := server.Reveal(RevealRequest{Ref: "ref", Reason: "test"}, &reply); err == nil {
		t.Fatal("expected Reveal without secret permission to fail")
	}

	server.permissions.Secrets = []string{"storage.cookie"}
	if err := server.Reveal(RevealRequest{Ref: "ref", Reason: "test"}, &reply); err != nil {
		t.Fatalf("Reveal delegates to injected resolver: %v", err)
	}
	if reply.Value != "secret-value" {
		t.Fatalf("secret = %q", reply.Value)
	}
}

func TestHostServicesRequireTypedDomainPermissions(t *testing.T) {
	resources := memoryResources{}
	server := hostServicesServer{
		ctx:           context.Background(),
		subscriptions: memorySubscriptions{},
		downloads:     memoryDownloads{},
		transfers:     memoryTransfers{},
		rules:         memoryRules{},
		connections:   resources,
		storages:      resources,
		schedules:     resources,
		settings:      resources,
	}
	var writeReply JSONReply
	if err := server.UpsertSubscription(SubscriptionUpsertRequest{}, &writeReply); err == nil {
		t.Fatal("expected subscription write without host permission to fail")
	}
	if err := server.SetSetting(SettingSetRequest{}, &writeReply); err == nil {
		t.Fatal("expected settings write without host permission to fail")
	}
	server.permissions.Host = []string{"subscriptions.write", "downloads.read", "downloads.write", "transfers.write", "rules.read", "rules.write", "connections.read", "connections.write", "storages.read", "storages.write", "schedules.read", "schedules.write", "settings.read", "settings.write"}
	if err := server.UpsertSubscription(SubscriptionUpsertRequest{}, &writeReply); err != nil {
		t.Fatalf("UpsertSubscription with permission: %v", err)
	}
	if err := server.UpsertDownload(DownloadUpsertRequest{}, &writeReply); err != nil {
		t.Fatalf("UpsertDownload with permission: %v", err)
	}
	var findReply DownloadFindReply
	if err := server.FindDownloadByHash(DownloadFindRequest{Hash: "abc"}, &findReply); err != nil {
		t.Fatalf("FindDownloadByHash with permission: %v", err)
	}
	if !findReply.Found || findReply.Result.TargetID != "download-1" {
		t.Fatalf("FindDownloadByHash reply = %+v", findReply)
	}
	if err := server.UpsertTransfer(TransferUpsertRequest{}, &writeReply); err != nil {
		t.Fatalf("UpsertTransfer with permission: %v", err)
	}
	if err := server.GetRuleCatalog(Empty{}, &writeReply); err != nil {
		t.Fatalf("GetRuleCatalog with permission: %v", err)
	}
	if err := server.UpsertRuleProfile(RuleProfileUpsertRequest{}, &writeReply); err != nil {
		t.Fatalf("UpsertRuleProfile with permission: %v", err)
	}
	if err := server.SetRuleSort(RuleSortSetRequest{}, &writeReply); err != nil {
		t.Fatalf("SetRuleSort with permission: %v", err)
	}
	if err := server.SetRuleDefault(RuleDefaultSetRequest{}, &writeReply); err != nil {
		t.Fatalf("SetRuleDefault with permission: %v", err)
	}
	if err := server.SetSetting(SettingSetRequest{}, &writeReply); err != nil {
		t.Fatalf("SetSetting with permission: %v", err)
	}
	if err := server.ListConnections(ConnectionListRequest{}, &writeReply); err != nil {
		t.Fatalf("ListConnections with permission: %v", err)
	}
	if err := server.ListStorages(Empty{}, &writeReply); err != nil {
		t.Fatalf("ListStorages with permission: %v", err)
	}
	if err := server.ListSchedules(Empty{}, &writeReply); err != nil {
		t.Fatalf("ListSchedules with permission: %v", err)
	}
}

func TestHostServicesRPCAcceptsMatchingLegacyStructShape(t *testing.T) {
	server := rpc.NewServer()
	target := &hostServicesServer{
		ctx:         context.Background(),
		kv:          memoryKV{},
		permissions: pluginsdk.Permissions{Data: []string{"storage"}},
	}
	if err := server.RegisterName("Plugin", target); err != nil {
		t.Fatalf("RegisterName returned error: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go server.ServeConn(serverConn)

	client := rpc.NewClient(clientConn)
	defer client.Close()

	var reply Empty
	err := client.Call("Plugin.KVSet", legacyKVSetRequest{
		Key:        "token",
		Data:       []byte(`{"ok":true}`),
		TTLSeconds: 60,
	}, &reply)
	if err != nil {
		t.Fatalf("KVSet with legacy request shape: %v", err)
	}
}

type legacyKVSetRequest struct {
	Key        string
	Data       []byte
	TTLSeconds int64
}

type memoryKV struct{}

func (memoryKV) Get(ctx context.Context, key string, out any) (bool, error) {
	return false, nil
}

func (memoryKV) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	return nil
}

func (memoryKV) Delete(ctx context.Context, key string) error {
	return nil
}

func (memoryKV) DeletePrefix(ctx context.Context, prefix string) error {
	return nil
}

type memoryDB struct{}

func (memoryDB) TableName(logicalName string) (string, error) {
	return logicalName, nil
}

func (memoryDB) Exec(ctx context.Context, statement string, args ...any) (pluginsdk.DBResult, error) {
	return pluginsdk.DBResult{}, nil
}

func (memoryDB) Query(ctx context.Context, statement string, args ...any) ([]map[string]any, error) {
	return nil, nil
}

type staticSecretResolver string

func (s staticSecretResolver) Reveal(ctx context.Context, ref, reason string) (string, error) {
	return string(s), nil
}

type memorySubscriptions struct{}

func (memorySubscriptions) UpsertSubscription(context.Context, pluginsdk.SubscriptionWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "subscription-1", Change: "created"}, nil
}

type memoryDownloads struct{}

func (memoryDownloads) UpsertDownload(context.Context, pluginsdk.DownloadWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "download-1", Change: "created"}, nil
}

func (memoryDownloads) FindDownloadByHash(context.Context, string) (pluginsdk.HostWriteResult, bool, error) {
	return pluginsdk.HostWriteResult{TargetID: "download-1"}, true, nil
}

type memoryTransfers struct{}

func (memoryTransfers) UpsertTransfer(context.Context, pluginsdk.TransferWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "transfer-1", Change: "created"}, nil
}

type memoryRules struct{}

func (memoryRules) GetRuleCatalog(context.Context) (pluginsdk.RuleCatalog, error) {
	return pluginsdk.RuleCatalog{}, nil
}

func (memoryRules) UpsertRuleProfile(context.Context, pluginsdk.RuleProfileWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "rule-1", Change: "created"}, nil
}

func (memoryRules) SetRuleSort(context.Context, pluginsdk.RuleSortWrite) (pluginsdk.RuleSortResult, error) {
	return pluginsdk.RuleSortResult{}, nil
}

func (memoryRules) SetRuleDefault(context.Context, pluginsdk.RuleDefaultWrite) (pluginsdk.RuleDefaultResult, error) {
	return pluginsdk.RuleDefaultResult{}, nil
}

type memoryResources struct{}

func (memoryResources) ListConnections(context.Context, string) ([]pluginsdk.Connection, error) {
	return []pluginsdk.Connection{{ID: "connection-1"}}, nil
}
func (memoryResources) GetConnection(context.Context, string, string) (pluginsdk.Connection, error) {
	return pluginsdk.Connection{}, nil
}
func (memoryResources) UpsertConnection(context.Context, pluginsdk.ConnectionWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "connection-1", Change: "created"}, nil
}
func (memoryResources) ListStorages(context.Context) ([]pluginsdk.Storage, error) {
	return []pluginsdk.Storage{{ID: "storage-1"}}, nil
}
func (memoryResources) GetStorage(context.Context, string) (pluginsdk.Storage, error) {
	return pluginsdk.Storage{}, nil
}
func (memoryResources) UpsertStorage(context.Context, pluginsdk.StorageWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "storage-1", Change: "created"}, nil
}
func (memoryResources) ListDirectoryMappings(context.Context) ([]pluginsdk.DirectoryMapping, error) {
	return []pluginsdk.DirectoryMapping{{ID: "directory-1"}}, nil
}
func (memoryResources) GetDirectoryMapping(context.Context, string) (pluginsdk.DirectoryMapping, error) {
	return pluginsdk.DirectoryMapping{}, nil
}
func (memoryResources) UpsertDirectoryMapping(context.Context, pluginsdk.DirectoryMappingWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "directory-1", Change: "created"}, nil
}
func (memoryResources) String(context.Context, string) (string, bool) { return "", false }
func (memoryResources) Int(context.Context, string) (int64, bool)     { return 0, false }
func (memoryResources) Bool(context.Context, string) (bool, bool)     { return false, false }
func (memoryResources) JSON(_ context.Context, _ string, out any) (bool, error) {
	return true, json.Unmarshal([]byte(`true`), out)
}
func (memoryResources) SetSetting(context.Context, pluginsdk.SettingWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "setting-1", Change: "updated"}, nil
}
func (memoryResources) ListSchedules(context.Context) ([]pluginsdk.Schedule, error) {
	return []pluginsdk.Schedule{{TaskType: "task-1"}}, nil
}
func (memoryResources) GetSchedule(context.Context, string) (pluginsdk.Schedule, error) {
	return pluginsdk.Schedule{}, nil
}
func (memoryResources) SetSchedule(context.Context, pluginsdk.ScheduleWrite) (pluginsdk.HostWriteResult, error) {
	return pluginsdk.HostWriteResult{TargetID: "schedule-1", Change: "updated"}, nil
}
