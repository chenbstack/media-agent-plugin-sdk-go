package pluginrpc

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

func TestExtendedProviderAdaptersUseDispensedClient(t *testing.T) {
	notifier := &recordingNotifier{}
	subtitle := &recordingSubtitleSource{}
	model := &recordingModelProvider{}
	plugin := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "official-tools", Name: "Official tools"},
		NewNotifier: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.NotifierProvider, error) {
			return notifier, nil
		},
		NewSubtitleSource: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.SubtitleSourceProvider, error) {
			return subtitle, nil
		},
		NewModel: func() providers.ModelProvider { return model },
	}
	client := newProviderTestClient(t, plugin)
	inst := pluginsdk.Instance{ID: "instance", Config: map[string]any{"endpoint": "https://example.test"}}

	n := client.Notifier(inst, nil)
	if n.Kind() != "official-tools" {
		t.Fatalf("notifier kind = %q", n.Kind())
	}
	if err := n.TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
	message := providers.NotificationMessage{Title: "Ready", Body: "Pack started", Metadata: map[string]string{"pack": "official"}}
	if err := n.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(notifier.message, message) {
		t.Fatalf("notifier message = %#v", notifier.message)
	}

	s := client.SubtitleSource(inst, nil)
	if s.Kind() != "official-tools" {
		t.Fatalf("subtitle kind = %q", s.Kind())
	}
	if err := s.TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
	search := providers.SubtitleSearchRequest{Title: "Arrival", Languages: []string{"zh", "en"}, Context: map[string]string{"site": "demo"}}
	results, err := s.Search(context.Background(), search)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(subtitle.search, search) || len(results) != 1 || results[0].Ref["id"] != "subtitle-1" {
		t.Fatalf("subtitle search = %#v, recorded=%#v", results, subtitle.search)
	}
	content, err := s.Download(context.Background(), results[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, []byte{0, 1, 2, 0xff}) || !reflect.DeepEqual(subtitle.download, results[0]) {
		t.Fatalf("subtitle download = %v, recorded=%#v", content, subtitle.download)
	}

	m := client.Model()
	if m.Kind() != "ollama" {
		t.Fatalf("model kind = %q", m.Kind())
	}
	modelConfig := providers.ModelConfig{ID: "model-1", Backend: "ollama", ModelName: "qwen3"}
	if err := m.ValidateModel(modelConfig); err != nil {
		t.Fatal(err)
	}
	model.validateErr = providers.ErrModelProviderNotConfigured
	if err := m.ValidateModel(modelConfig); !errors.Is(err, providers.ErrModelProviderNotConfigured) {
		t.Fatalf("model sentinel error = %v", err)
	}
	model.validateErr = nil
	fixed := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	generated, err := m.Generate(context.Background(), providers.ModelGenerateRequest{
		Model: modelConfig, Prompt: "hello", MaxTokens: 32, Now: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatal(err)
	}
	if generated.Output != "generated" || !generated.Started.Equal(fixed) || model.generate.Prompt != "hello" || model.generate.Now == nil || !model.generate.Now().Equal(fixed) {
		t.Fatalf("model generate = %#v, recorded=%#v", generated, model.generate)
	}
	downloaded, err := m.Download(context.Background(), providers.ModelDownloadRequest{
		Model: modelConfig, TimeoutSeconds: 42, Now: func() time.Time { return fixed },
	})
	if err != nil || downloaded.ModelID != "model-1" || model.download.TimeoutSeconds != 42 || model.download.Now == nil || !model.download.Now().Equal(fixed) {
		t.Fatalf("model download = %#v, recorded=%#v, err=%v", downloaded, model.download, err)
	}
	uninstalled, err := m.Uninstall(context.Background(), providers.ModelUninstallRequest{
		Model: modelConfig, TimeoutSeconds: 9, Now: func() time.Time { return fixed },
	})
	if err != nil || uninstalled.ModelID != "model-1" || model.uninstall.TimeoutSeconds != 9 || model.uninstall.Now == nil || !model.uninstall.Now().Equal(fixed) {
		t.Fatalf("model uninstall = %#v, recorded=%#v, err=%v", uninstalled, model.uninstall, err)
	}
	if got := m.CommandDisplay(modelConfig); got != "ollama run qwen3" {
		t.Fatalf("command display = %q", got)
	}
}

func TestExternalPluginBuildsExtendedProviderFactoriesFromCapabilities(t *testing.T) {
	plugin := (ExternalPlugin{Manifest: pluginsdk.Manifest{
		ID: "official-tools",
		Capabilities: []string{
			"notifier.send",
			"subtitle_source.search",
			"model_provider.generate",
		},
	}}).Plugin()
	if plugin.NewNotifier == nil || plugin.NewSubtitleSource == nil || plugin.NewModel == nil {
		t.Fatalf("extended factories missing: notifier=%v subtitle=%v model=%v",
			plugin.NewNotifier != nil, plugin.NewSubtitleSource != nil, plugin.NewModel != nil)
	}
	notifier, err := plugin.NewNotifier(context.Background(), pluginsdk.Instance{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	subtitle, err := plugin.NewSubtitleSource(context.Background(), pluginsdk.Instance{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := notifier.(*notifierProvider); !ok {
		t.Fatalf("notifier adapter type = %T", notifier)
	}
	if _, ok := subtitle.(*subtitleSourceProvider); !ok {
		t.Fatalf("subtitle adapter type = %T", subtitle)
	}
	if _, ok := plugin.NewModel().(*modelProvider); !ok {
		t.Fatalf("model adapter type = %T", plugin.NewModel())
	}
}

type recordingNotifier struct {
	message providers.NotificationMessage
}

func (*recordingNotifier) Kind() string                         { return "dingtalk" }
func (*recordingNotifier) TestConnection(context.Context) error { return nil }
func (p *recordingNotifier) Send(_ context.Context, message providers.NotificationMessage) error {
	p.message = message
	return nil
}

type recordingSubtitleSource struct {
	search   providers.SubtitleSearchRequest
	download providers.SubtitleResult
}

func (*recordingSubtitleSource) Kind() string                         { return "opensubtitles" }
func (*recordingSubtitleSource) TestConnection(context.Context) error { return nil }
func (p *recordingSubtitleSource) Search(_ context.Context, request providers.SubtitleSearchRequest) ([]providers.SubtitleResult, error) {
	p.search = request
	return []providers.SubtitleResult{{Provider: "opensubtitles", Name: "Arrival.zh.srt", Ref: map[string]string{"id": "subtitle-1"}}}, nil
}
func (p *recordingSubtitleSource) Download(_ context.Context, result providers.SubtitleResult) ([]byte, error) {
	p.download = result
	return []byte{0, 1, 2, 0xff}, nil
}

type recordingModelProvider struct {
	validated   providers.ModelConfig
	validateErr error
	generate    providers.ModelGenerateRequest
	download    providers.ModelDownloadRequest
	uninstall   providers.ModelUninstallRequest
}

func (*recordingModelProvider) Kind() string { return "ollama" }
func (p *recordingModelProvider) ValidateModel(model providers.ModelConfig) error {
	p.validated = model
	return p.validateErr
}
func (p *recordingModelProvider) Generate(_ context.Context, request providers.ModelGenerateRequest) (providers.ModelGenerateResult, error) {
	p.generate = request
	now := time.Now()
	if request.Now != nil {
		now = request.Now()
	}
	return providers.ModelGenerateResult{Output: "generated", Started: now, Finished: now}, nil
}
func (p *recordingModelProvider) Download(_ context.Context, request providers.ModelDownloadRequest) (providers.ModelDownloadResult, error) {
	p.download = request
	return providers.ModelDownloadResult{ModelID: request.Model.ID}, nil
}
func (p *recordingModelProvider) Uninstall(_ context.Context, request providers.ModelUninstallRequest) (providers.ModelUninstallResult, error) {
	p.uninstall = request
	return providers.ModelUninstallResult{ModelID: request.Model.ID}, nil
}
func (*recordingModelProvider) CommandDisplay(model providers.ModelConfig) string {
	return "ollama run " + model.ModelName
}
