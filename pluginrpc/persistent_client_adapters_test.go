package pluginrpc

import (
	"context"
	"reflect"
	"testing"
	"time"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

func TestPersistentClientAdaptersUseDispensedClient(t *testing.T) {
	cookieSource := &recordingCookieSource{}
	renderer := &recordingRenderer{}
	events := &recordingEventSubscriber{}
	actions := &recordingActionHandler{}
	plugin := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "official", Name: "Official Pack"},
		NewCookieSource: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.CookieSourceProvider, error) {
			return cookieSource, nil
		},
		NewRenderer: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.RendererProvider, error) {
			return renderer, nil
		},
		NewEventSubscriber: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (pluginsdk.EventSubscriber, error) {
			return events, nil
		},
		NewActionHandler: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (pluginsdk.ActionHandler, error) {
			return actions, nil
		},
	}
	client := newProviderTestClient(t, plugin)
	inst := pluginsdk.Instance{ID: "official-global", Config: map[string]any{"mode": "pack"}}

	cookies := client.CookieSource(inst, nil)
	if cookies.Kind() != "official" {
		t.Fatalf("cookie source kind = %q", cookies.Kind())
	}
	if err := cookies.TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := cookies.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cookieSource.tested || snapshot.Source != "cookie-cloud" {
		t.Fatalf("cookie source tested=%v snapshot=%#v", cookieSource.tested, snapshot)
	}

	render := client.Renderer(inst, nil)
	if render.Kind() != "official" {
		t.Fatalf("renderer kind = %q", render.Kind())
	}
	if err := render.TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
	renderRequest := providers.RenderRequest{URL: "https://example.test", WaitUntil: "networkidle"}
	renderResult, err := render.Render(context.Background(), renderRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !renderer.tested || !reflect.DeepEqual(renderer.request, renderRequest) || renderResult.Status != 200 {
		t.Fatalf("renderer tested=%v request=%#v result=%#v", renderer.tested, renderer.request, renderResult)
	}

	event := pluginsdk.EventEnvelope{EventID: "evt-1", Type: "subscription.created", Version: 1}
	if err := client.EventSubscriber(inst, nil).HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events.event, event) {
		t.Fatalf("event = %#v, want %#v", events.event, event)
	}

	input := map[string]any{"request_id": "req-1"}
	actionResult, err := client.ActionHandler(inst, nil).RunAction(context.Background(), "approve", input)
	if err != nil {
		t.Fatal(err)
	}
	if actions.actionID != "approve" || !reflect.DeepEqual(actions.input, input) || actionResult.Message != "approved" {
		t.Fatalf("action id=%q input=%#v result=%#v", actions.actionID, actions.input, actionResult)
	}
}

type recordingCookieSource struct{ tested bool }

func (*recordingCookieSource) Kind() string { return "cookie-cloud" }
func (p *recordingCookieSource) TestConnection(context.Context) error {
	p.tested = true
	return nil
}
func (*recordingCookieSource) Snapshot(context.Context) (providers.CookieSnapshot, error) {
	return providers.CookieSnapshot{Source: "cookie-cloud", FetchedAt: time.Unix(1, 0)}, nil
}

type recordingRenderer struct {
	tested  bool
	request providers.RenderRequest
}

func (*recordingRenderer) Kind() string { return "renderer" }
func (p *recordingRenderer) TestConnection(context.Context) error {
	p.tested = true
	return nil
}
func (p *recordingRenderer) Render(_ context.Context, request providers.RenderRequest) (providers.RenderResult, error) {
	p.request = request
	return providers.RenderResult{HTML: "<main>ok</main>", FinalURL: request.URL, Status: 200}, nil
}

type recordingEventSubscriber struct{ event pluginsdk.EventEnvelope }

func (s *recordingEventSubscriber) HandleEvent(_ context.Context, event pluginsdk.EventEnvelope) error {
	s.event = event
	return nil
}

type recordingActionHandler struct {
	actionID string
	input    map[string]any
}

func (h *recordingActionHandler) RunAction(_ context.Context, actionID string, input map[string]any) (pluginsdk.ActionResult, error) {
	h.actionID = actionID
	h.input = input
	return pluginsdk.ActionResult{Message: "approved", Data: map[string]any{"id": input["request_id"]}}, nil
}
