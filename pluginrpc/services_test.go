package pluginrpc

import (
	"context"
	"net"
	"net/rpc"
	"testing"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
)

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
	server := hostServicesServer{
		ctx:           context.Background(),
		subscriptions: memorySubscriptions{},
		downloads:     memoryDownloads{},
		transfers:     memoryTransfers{},
	}
	var writeReply JSONReply
	if err := server.UpsertSubscription(SubscriptionUpsertRequest{}, &writeReply); err == nil {
		t.Fatal("expected subscription write without host permission to fail")
	}
	server.permissions.Host = []string{"subscriptions.write", "downloads.read", "downloads.write", "transfers.write"}
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
