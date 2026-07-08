package pluginrpc

import (
	"context"
	"net"
	"net/rpc"
	"testing"
	"time"

	"media-agent-lab/server/pkg/pluginsdk"
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
