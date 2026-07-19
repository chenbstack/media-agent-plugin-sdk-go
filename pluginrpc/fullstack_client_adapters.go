package pluginrpc

import (
	"context"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// API returns an api.endpoint adapter bound to this logical Client. A Client
// dispensed by PackClient therefore executes in the existing Pack process;
// ExternalPlugin uses the same adapter through externalProviderSession.
func (c *Client) API(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) pluginsdk.APIProvider {
	return &apiProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

// Identity returns an identity.provider adapter bound to this logical Client.
// It verifies credentials and forwards optional redirect flows; it can never
// mint a host session.
func (c *Client) Identity(inst pluginsdk.Instance, secrets pluginsdk.SecretResolver) pluginsdk.IdentityProvider {
	return &identityProvider{session: directProviderSession{client: c}, inst: inst, secrets: secrets}
}

type apiProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ pluginsdk.APIProvider = (*apiProvider)(nil)

func (p *apiProvider) HandleAPI(ctx context.Context, request pluginsdk.APIRequest) (pluginsdk.APIResponse, error) {
	var response pluginsdk.APIResponse
	err := p.session.withClient(ctx, "api.handle", func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.call(ctx, "Plugin.APIHandle", APIHandleRequest{Instance: instance, Request: request}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &response)
	})
	return response, err
}

type identityProvider struct {
	session providerSession
	inst    pluginsdk.Instance
	secrets pluginsdk.SecretResolver
}

var _ pluginsdk.IdentityProvider = (*identityProvider)(nil)
var _ pluginsdk.IdentityRedirectProvider = (*identityProvider)(nil)

func (p *identityProvider) VerifyIdentity(ctx context.Context, request pluginsdk.IdentityVerifyRequest) (pluginsdk.IdentityVerification, error) {
	var verification pluginsdk.IdentityVerification
	err := p.session.withClient(ctx, "identity.verify", func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.call(ctx, "Plugin.IdentityVerify", IdentityVerifyRequest{Instance: instance, Request: request}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &verification)
	})
	return verification, err
}

func (p *identityProvider) BeginIdentity(ctx context.Context, request pluginsdk.IdentityBeginRequest) (pluginsdk.IdentityChallenge, error) {
	var challenge pluginsdk.IdentityChallenge
	err := p.session.withClient(ctx, "identity.begin", func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.call(ctx, "Plugin.IdentityBegin", IdentityBeginRequest{Instance: instance, Request: request}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &challenge)
	})
	return challenge, err
}

func (p *identityProvider) CompleteIdentity(ctx context.Context, request pluginsdk.IdentityCompleteRequest) (pluginsdk.IdentityVerification, error) {
	var verification pluginsdk.IdentityVerification
	err := p.session.withClient(ctx, "identity.complete", func(c *Client) error {
		instance, err := c.instancePayload(ctx, p.inst, p.secrets)
		if err != nil {
			return err
		}
		var reply JSONReply
		if err := c.call(ctx, "Plugin.IdentityComplete", IdentityCompleteRequest{Instance: instance, Request: request}, &reply); err != nil {
			return err
		}
		return decodeJSON(reply.Data, &verification)
	})
	return verification, err
}
