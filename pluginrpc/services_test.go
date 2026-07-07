package pluginrpc

import (
	"context"
	"testing"
	"time"

	"media-agent-lab/server/pkg/pluginsdk"
)

func TestHostServicesRequireKVPermission(t *testing.T) {
	server := hostServicesServer{
		ctx: context.Background(),
		kv:  memoryKV{},
	}

	var reply kvGetReply
	if err := server.KVGet(kvGetRequest{Key: "token"}, &reply); err == nil {
		t.Fatal("expected KVGet without data.storage permission to fail")
	}

	server.permissions.Data = []string{"storage"}
	if err := server.KVGet(kvGetRequest{Key: "token"}, &reply); err != nil {
		t.Fatalf("KVGet with data.storage permission: %v", err)
	}
}

func TestHostServicesRequirePrivateDBPermission(t *testing.T) {
	server := hostServicesServer{
		ctx: context.Background(),
		db:  memoryDB{},
	}

	var reply StringReply
	if err := server.DBTableName(dbTableNameRequest{LogicalName: "files"}, &reply); err == nil {
		t.Fatal("expected DBTableName without data.storage permission to fail")
	}

	server.permissions.Data = []string{"storage"}
	if err := server.DBTableName(dbTableNameRequest{LogicalName: "files"}, &reply); err != nil {
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
	if err := server.Reveal(revealRequest{Ref: "ref", Reason: "test"}, &reply); err == nil {
		t.Fatal("expected Reveal without secret permission to fail")
	}

	server.permissions.Secrets = []string{"storage.cookie"}
	if err := server.Reveal(revealRequest{Ref: "ref", Reason: "test"}, &reply); err != nil {
		t.Fatalf("Reveal delegates to injected resolver: %v", err)
	}
	if reply.Value != "secret-value" {
		t.Fatalf("secret = %q", reply.Value)
	}
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
