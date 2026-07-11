package pluginrpc

import (
	"context"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// CookieSource returns a CookieSourceProvider bound to this logical Client.
// Clients dispensed from a Pack therefore reuse the already-running Pack
// process instead of starting a short-lived standalone plugin process.
func (c *Client) CookieSource(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.CookieSourceProvider {
	return &persistentCookieSourceProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// Renderer returns a RendererProvider bound to this logical Client.
func (c *Client) Renderer(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) providers.RendererProvider {
	return &persistentRendererProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// EventSubscriber returns an EventSubscriber bound to this logical Client.
func (c *Client) EventSubscriber(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) pluginsdk.EventSubscriber {
	return &persistentEventSubscriber{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// ActionHandler returns an ActionHandler bound to this logical Client.
func (c *Client) ActionHandler(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) pluginsdk.ActionHandler {
	return &persistentActionHandler{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

type persistentCookieSourceProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.CookieSourceProvider = (*persistentCookieSourceProvider)(nil)

func (p *persistentCookieSourceProvider) Kind() string { return p.session.pluginID() }

func (p *persistentCookieSourceProvider) TestConnection(ctx context.Context) error {
	return p.session.withClient(ctx, "cookie_source.test", func(c *Client) error {
		return c.CookieSourceTestContext(ctx, p.inst, p.secrets)
	})
}

func (p *persistentCookieSourceProvider) Snapshot(ctx context.Context) (providers.CookieSnapshot, error) {
	var snapshot providers.CookieSnapshot
	err := p.session.withClient(ctx, "cookie_source.fetch", func(c *Client) error {
		var err error
		snapshot, err = c.CookieSourceSnapshotContext(ctx, p.inst, p.secrets)
		return err
	})
	return snapshot, err
}

type persistentRendererProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ providers.RendererProvider = (*persistentRendererProvider)(nil)

func (p *persistentRendererProvider) Kind() string { return p.session.pluginID() }

func (p *persistentRendererProvider) TestConnection(ctx context.Context) error {
	return p.session.withClient(ctx, "renderer.test", func(c *Client) error {
		return c.RendererTestContext(ctx, p.inst, p.secrets)
	})
}

func (p *persistentRendererProvider) Render(ctx context.Context, request providers.RenderRequest) (providers.RenderResult, error) {
	var result providers.RenderResult
	err := p.session.withClient(ctx, "renderer.render", func(c *Client) error {
		var err error
		result, err = c.RendererRenderContext(ctx, p.inst, p.secrets, request)
		return err
	})
	return result, err
}

type persistentEventSubscriber struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ pluginsdk.EventSubscriber = (*persistentEventSubscriber)(nil)

func (s *persistentEventSubscriber) HandleEvent(ctx context.Context, event pluginsdk.EventEnvelope) error {
	return s.session.withClient(ctx, "plugin.event.handle", func(c *Client) error {
		return c.HandleEventContext(ctx, s.inst, s.secrets, event)
	})
}

type persistentActionHandler struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ pluginsdk.ActionHandler = (*persistentActionHandler)(nil)

func (h *persistentActionHandler) RunAction(ctx context.Context, actionID string, input map[string]any) (pluginsdk.ActionResult, error) {
	var result pluginsdk.ActionResult
	err := h.session.withClient(ctx, "plugin.action."+actionID, func(c *Client) error {
		var err error
		result, err = c.RunActionContext(ctx, h.inst, h.secrets, actionID, input)
		return err
	})
	return result, err
}
