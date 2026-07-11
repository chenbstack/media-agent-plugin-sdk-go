package pluginrpc

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

func TestClientStorageAdapterUsesDispensedClient(t *testing.T) {
	implementation := &storageTestProvider{root: t.TempDir()}
	plugin := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "storage", Name: "Storage"},
		NewStorage: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.StorageProvider, error) {
			return implementation, nil
		},
	}
	client := newProviderTestClient(t, plugin)
	provider := client.Storage(pluginsdk.Instance{ID: "disk-1"}, nil)

	if provider.Kind() != "storage" {
		t.Fatalf("Kind = %q", provider.Kind())
	}
	if err := provider.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	info, err := provider.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.RootPath != implementation.root {
		t.Fatalf("Info.RootPath = %q", info.RootPath)
	}
	if err := provider.EnsureMounted(context.Background()); err != nil {
		t.Fatalf("EnsureMounted: %v", err)
	}
	if err := provider.Unmount(context.Background()); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	if got := implementation.calls(); strings.Join(got, ",") != "test,info,mount,unmount" {
		t.Fatalf("server calls = %v", got)
	}
}

func TestRPCServerStorageRejectsUnsupportedOptionalCapability(t *testing.T) {
	server := &rpcServer{plugin: pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "storage"},
		NewStorage: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.StorageProvider, error) {
			return storageBaseTestProvider{}, nil
		},
	}}
	request := StoragePathRequest{Instance: InstancePayload{ID: "disk-1"}, Path: "movie.mkv"}
	if err := server.StorageStat(request, &JSONReply{}); err == nil || !strings.Contains(err.Error(), "FileStorageProvider") {
		t.Fatalf("StorageStat error = %v", err)
	}
	if err := server.StorageCopy(StorageRenameRequest{Instance: request.Instance}, &Empty{}); err == nil || !strings.Contains(err.Error(), "服务端复制") {
		t.Fatalf("StorageCopy error = %v", err)
	}
	if err := server.StorageResolvePlaybackURL(StoragePlaybackURLRequest{Instance: request.Instance}, &JSONReply{}); err == nil || !strings.Contains(err.Error(), "播放 URL") {
		t.Fatalf("StorageResolvePlaybackURL error = %v", err)
	}
}

type storageBaseTestProvider struct{}

func (storageBaseTestProvider) Kind() string                         { return "storage" }
func (storageBaseTestProvider) TestConnection(context.Context) error { return nil }
func (storageBaseTestProvider) Info(context.Context) (providers.StorageInfo, error) {
	return providers.StorageInfo{Kind: "storage"}, nil
}
func (storageBaseTestProvider) EnsureMounted(context.Context) error { return nil }
func (storageBaseTestProvider) Unmount(context.Context) error       { return nil }

func TestPackStorageAdapterReusesPersistentProcessAndBrokers(t *testing.T) {
	root := t.TempDir()
	observer := &packStorageProcessObserver{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pack, err := StartPackClient(ctx, PackClientConfig{
		Command:  os.Args[0],
		Args:     []string{"-test.run=^TestPackStorageHelperProcess$"},
		Env:      []string{"MEDIA_AGENT_PACK_STORAGE_HELPER=1", "MEDIA_AGENT_PACK_STORAGE_ROOT=" + root},
		Stderr:   io.Discard,
		PackID:   "official",
		PackName: "Official",
		Plugins: []PackPluginConfig{{
			Manifest: pluginsdk.Manifest{
				ID:           "storage",
				Name:         "Storage",
				Capabilities: []string{"storage", "storage.browse"},
				Permissions: pluginsdk.Permissions{
					Data:    []string{"storage"},
					Secrets: []string{"storage.token"},
				},
			},
			ScopeType: "storage",
			ScopeID:   "disk-1",
		}},
		ProcessObserver: observer,
	})
	if err != nil {
		t.Fatalf("StartPackClient: %v", err)
	}
	defer pack.Close()

	client, err := pack.Dispense("storage")
	if err != nil {
		t.Fatalf("Dispense: %v", err)
	}
	kv := &packStorageKV{values: map[string]any{"label": "pack"}}
	inst := pluginsdk.Instance{
		ID:     "disk-1",
		Config: map[string]any{"secret_ref": "secret://storage-token"},
		KV:     kv,
	}
	provider := client.Storage(inst, packStorageSecrets{})
	info, err := provider.Info(context.Background())
	if err != nil {
		t.Fatalf("Info through HostServices: %v", err)
	}
	if info.RootPath != "pack:secret-value" {
		t.Fatalf("Info.RootPath = %q", info.RootPath)
	}

	files, ok := provider.(providers.FileStorageProvider)
	if !ok {
		t.Fatalf("storage adapter does not implement FileStorageProvider: %T", provider)
	}
	writer, err := files.OpenWriter(context.Background(), "stream.txt")
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := writer.Write([]byte("persistent pack stream")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer Close: %v", err)
	}
	reader, err := files.OpenReader(context.Background(), "stream.txt")
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader Close: %v", err)
	}
	if string(data) != "persistent pack stream" {
		t.Fatalf("stream contents = %q", data)
	}

	uploader, ok := provider.(providers.UploadProvider)
	if !ok {
		t.Fatalf("storage adapter does not implement UploadProvider: %T", provider)
	}
	if err := uploader.Upload(context.Background(), "uploaded.txt", newPackUploadSource("upload.txt", []byte("reverse broker upload"))); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	uploaded, err := os.ReadFile(filepath.Join(root, "uploaded.txt"))
	if err != nil || string(uploaded) != "reverse broker upload" {
		t.Fatalf("uploaded file = %q, %v", uploaded, err)
	}

	if err := pack.Ping(); err != nil {
		t.Fatalf("Pack Ping after storage streams: %v", err)
	}
	if got := observer.starts.Load(); got != 1 {
		t.Fatalf("physical process starts = %d, want 1", got)
	}
	if got := observer.finishes.Load(); got != 0 {
		t.Fatalf("physical process ended during logical storage calls: %d", got)
	}
}

// TestPackStorageHelperProcess is launched by HashiCorp go-plugin. It is a real
// Pack process so the integration test covers MuxBroker streams in both
// directions as well as HostServices on the same persistent Client.
func TestPackStorageHelperProcess(t *testing.T) {
	if os.Getenv("MEDIA_AGENT_PACK_STORAGE_HELPER") != "1" {
		return
	}
	root := os.Getenv("MEDIA_AGENT_PACK_STORAGE_ROOT")
	ServePack([]pluginsdk.Plugin{{
		Manifest: pluginsdk.Manifest{ID: "storage", Name: "Storage"},
		NewStorage: func(_ context.Context, inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) (providers.StorageProvider, error) {
			return &storageTestProvider{root: root, inst: inst, secrets: secrets}, nil
		},
	}})
}

type storageTestProvider struct {
	root    string
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
	mu      sync.Mutex
	called  []string
}

func (p *storageTestProvider) record(call string) {
	p.mu.Lock()
	p.called = append(p.called, call)
	p.mu.Unlock()
}
func (p *storageTestProvider) calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.called...)
}
func (*storageTestProvider) Kind() string { return "storage-implementation" }
func (p *storageTestProvider) TestConnection(context.Context) error {
	p.record("test")
	return nil
}
func (p *storageTestProvider) Info(ctx context.Context) (providers.StorageInfo, error) {
	p.record("info")
	if p.inst.KV == nil || p.secrets == nil {
		return providers.StorageInfo{Kind: p.Kind(), RootPath: p.root}, nil
	}
	var label string
	found, err := p.inst.KV.Get(ctx, "label", &label)
	if err != nil || !found {
		return providers.StorageInfo{}, fmt.Errorf("read host KV: found=%v: %w", found, err)
	}
	ref, _ := p.inst.Config["secret_ref"].(string)
	secret, err := p.secrets.Reveal(ctx, ref, "storage pack integration test")
	if err != nil {
		return providers.StorageInfo{}, err
	}
	return providers.StorageInfo{Kind: p.Kind(), RootPath: label + ":" + secret}, nil
}
func (p *storageTestProvider) EnsureMounted(context.Context) error { p.record("mount"); return nil }
func (p *storageTestProvider) Unmount(context.Context) error       { p.record("unmount"); return nil }
func (p *storageTestProvider) path(name string) string             { return filepath.Join(p.root, name) }
func (p *storageTestProvider) Stat(_ context.Context, name string) (providers.StorageFileInfo, error) {
	info, err := os.Stat(p.path(name))
	if err != nil {
		return providers.StorageFileInfo{}, err
	}
	return providers.StorageFileInfo{Name: info.Name(), Size: info.Size(), IsDir: info.IsDir(), ModTime: info.ModTime()}, nil
}
func (p *storageTestProvider) ListDir(_ context.Context, name string) ([]providers.StorageFileInfo, error) {
	entries, err := os.ReadDir(p.path(name))
	if err != nil {
		return nil, err
	}
	out := make([]providers.StorageFileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, providers.StorageFileInfo{Name: entry.Name(), Size: info.Size(), IsDir: entry.IsDir(), ModTime: info.ModTime()})
	}
	return out, nil
}
func (p *storageTestProvider) MkdirAll(_ context.Context, name string) error {
	return os.MkdirAll(p.path(name), 0o755)
}
func (p *storageTestProvider) Remove(_ context.Context, name string) error {
	return os.RemoveAll(p.path(name))
}
func (p *storageTestProvider) OpenReader(_ context.Context, name string) (io.ReadCloser, error) {
	return os.Open(p.path(name))
}
func (p *storageTestProvider) OpenWriter(_ context.Context, name string) (io.WriteCloser, error) {
	return os.Create(p.path(name))
}
func (p *storageTestProvider) Rename(_ context.Context, oldpath, newpath string) error {
	return os.Rename(p.path(oldpath), p.path(newpath))
}
func (p *storageTestProvider) Link(_ context.Context, oldname, newname string) error {
	return os.Link(p.path(oldname), p.path(newname))
}
func (p *storageTestProvider) Symlink(_ context.Context, oldname, newname string) error {
	return os.Symlink(p.path(oldname), p.path(newname))
}
func (p *storageTestProvider) Copy(_ context.Context, oldname, newname string) error {
	data, err := os.ReadFile(p.path(oldname))
	if err != nil {
		return err
	}
	return os.WriteFile(p.path(newname), data, 0o644)
}
func (p *storageTestProvider) Upload(ctx context.Context, name string, source providers.UploadSource) error {
	reader, err := source.Open(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := os.Create(p.path(name))
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, reader)
	closeErr := writer.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func (p *storageTestProvider) ResolvePlaybackURL(_ context.Context, input providers.PlaybackURLInput) (providers.PlaybackURLResult, error) {
	return providers.PlaybackURLResult{URL: "file://" + p.path(input.Path)}, nil
}

var _ providers.FileStorageProvider = (*storageTestProvider)(nil)
var _ providers.StorageDirectoryLister = (*storageTestProvider)(nil)
var _ providers.ServerSideCopyProvider = (*storageTestProvider)(nil)
var _ providers.UploadProvider = (*storageTestProvider)(nil)
var _ providers.PlaybackURLProvider = (*storageTestProvider)(nil)

type packStorageProcessObserver struct {
	starts   atomic.Int32
	finishes atomic.Int32
}

func (o *packStorageProcessObserver) PluginProcessStarted(ProcessStartInfo) func() {
	o.starts.Add(1)
	return func() { o.finishes.Add(1) }
}

type packStorageSecrets struct{}

func (packStorageSecrets) Reveal(_ context.Context, ref, _ string) (string, error) {
	if ref != "secret://storage-token" {
		return "", fmt.Errorf("unknown secret ref %q", ref)
	}
	return "secret-value", nil
}

type packStorageKV struct {
	mu     sync.Mutex
	values map[string]any
}

func (s *packStorageKV) Get(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	value, ok := s.values[key]
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(data, out)
}
func (s *packStorageKV) Set(_ context.Context, key string, value any, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}
func (s *packStorageKV) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}
func (s *packStorageKV) DeletePrefix(_ context.Context, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.values {
		if strings.HasPrefix(key, prefix) {
			delete(s.values, key)
		}
	}
	return nil
}

type packUploadSource struct {
	name string
	data []byte
}

func newPackUploadSource(name string, data []byte) packUploadSource {
	return packUploadSource{name: name, data: append([]byte(nil), data...)}
}
func (s packUploadSource) Name() string { return s.name }
func (s packUploadSource) Size() int64  { return int64(len(s.data)) }
func (s packUploadSource) SHA1(context.Context) (string, error) {
	sum := sha1.Sum(s.data)
	return hex.EncodeToString(sum[:]), nil
}
func (s packUploadSource) Open(context.Context) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}
func (s packUploadSource) OpenRange(_ context.Context, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length < 0 || offset > int64(len(s.data)) {
		return nil, fmt.Errorf("invalid range")
	}
	end := offset + length
	if end > int64(len(s.data)) {
		end = int64(len(s.data))
	}
	return io.NopCloser(bytes.NewReader(s.data[offset:end])), nil
}
