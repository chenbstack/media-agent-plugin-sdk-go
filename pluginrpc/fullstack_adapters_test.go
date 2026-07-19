package pluginrpc

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

func TestFullStackAdaptersUseDispensedClient(t *testing.T) {
	api := &recordingAPIProvider{}
	identity := &recordingIdentityProvider{}
	plugin := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "family", Name: "Family"},
		NewAPI: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (pluginsdk.APIProvider, error) {
			return api, nil
		},
		NewIdentity: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (pluginsdk.IdentityProvider, error) {
			return identity, nil
		},
	}
	client := newProviderTestClient(t, plugin)
	inst := pluginsdk.Instance{ID: "family-global", Config: map[string]any{"mode": "family"}}

	request := pluginsdk.APIRequest{
		Method: "POST",
		Path:   "/requests",
		Query:  map[string][]string{"state": {"pending", "approved"}},
		Headers: map[string][]string{
			"Accept":       {"application/json"},
			"Content-Type": {"application/json"},
		},
		Body:      []byte(`{"title":"Arrival"}`),
		Principal: &pluginsdk.Principal{ID: "member-1", DisplayName: "Member"},
	}
	response, err := client.API(inst, nil).HandleAPI(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(api.request, request) {
		t.Fatalf("API request = %#v, want %#v", api.request, request)
	}
	if response.Status != 201 || string(response.Body) != `{"id":"request-1"}` || response.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("API response = %#v", response)
	}

	verifyRequest := pluginsdk.IdentityVerifyRequest{Scheme: "password", Identifier: "alice", Credential: "secret"}
	verification, err := client.Identity(inst, nil).VerifyIdentity(context.Background(), verifyRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(identity.request, verifyRequest) {
		t.Fatalf("identity request = %#v, want %#v", identity.request, verifyRequest)
	}
	if !verification.Authenticated || verification.Principal == nil || verification.Principal.ID != "user-alice" {
		t.Fatalf("verification = %#v", verification)
	}
	challengeRequest := pluginsdk.IdentityBeginRequest{FlowID: "oidc", CallbackURL: "https://host.test/callback", State: "state-1"}
	challenge, err := client.Identity(inst, nil).(pluginsdk.IdentityRedirectProvider).BeginIdentity(context.Background(), challengeRequest)
	if err != nil || challenge.RedirectURL != "https://idp.test/authorize?state=state-1" || !reflect.DeepEqual(identity.beginRequest, challengeRequest) {
		t.Fatalf("challenge=%#v request=%#v err=%v", challenge, identity.beginRequest, err)
	}
	completeRequest := pluginsdk.IdentityCompleteRequest{FlowID: "oidc", CallbackURL: challengeRequest.CallbackURL, Parameters: map[string][]string{"code": {"code-1"}}, Data: []byte("verifier")}
	completed, err := client.Identity(inst, nil).(pluginsdk.IdentityRedirectProvider).CompleteIdentity(context.Background(), completeRequest)
	if err != nil || !completed.Authenticated || completed.Principal == nil || completed.Principal.Subject != "oidc-user" || !reflect.DeepEqual(identity.completeRequest, completeRequest) {
		t.Fatalf("completed=%#v request=%#v err=%v", completed, identity.completeRequest, err)
	}
	data, err := json.Marshal(verification)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "session") || strings.Contains(strings.ToLower(string(data)), "token") {
		t.Fatalf("identity verification must not carry session material: %s", data)
	}
}

func TestExternalPluginBuildsFullStackFactoriesFromCapabilities(t *testing.T) {
	plugin := (ExternalPlugin{Manifest: pluginsdk.Manifest{
		ID: "family", Capabilities: []string{pluginsdk.CapabilityAPIEndpoint, pluginsdk.CapabilityIdentityProvider},
	}}).Plugin()
	if plugin.NewAPI == nil || plugin.NewIdentity == nil {
		t.Fatalf("full-stack factories missing: api=%v identity=%v", plugin.NewAPI != nil, plugin.NewIdentity != nil)
	}
	api, err := plugin.NewAPI(context.Background(), pluginsdk.Instance{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := plugin.NewIdentity(context.Background(), pluginsdk.Instance{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := api.(*apiProvider); !ok {
		t.Fatalf("API adapter type = %T", api)
	}
	if _, ok := identity.(*identityProvider); !ok {
		t.Fatalf("identity adapter type = %T", identity)
	}
}

type recordingAPIProvider struct{ request pluginsdk.APIRequest }

func (p *recordingAPIProvider) HandleAPI(_ context.Context, request pluginsdk.APIRequest) (pluginsdk.APIResponse, error) {
	p.request = request
	return pluginsdk.APIResponse{
		Status:  201,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    []byte(`{"id":"request-1"}`),
	}, nil
}

type recordingIdentityProvider struct {
	request         pluginsdk.IdentityVerifyRequest
	beginRequest    pluginsdk.IdentityBeginRequest
	completeRequest pluginsdk.IdentityCompleteRequest
}

func (p *recordingIdentityProvider) BeginIdentity(_ context.Context, request pluginsdk.IdentityBeginRequest) (pluginsdk.IdentityChallenge, error) {
	p.beginRequest = request
	return pluginsdk.IdentityChallenge{RedirectURL: "https://idp.test/authorize?state=" + request.State, Data: []byte("verifier")}, nil
}

func (p *recordingIdentityProvider) CompleteIdentity(_ context.Context, request pluginsdk.IdentityCompleteRequest) (pluginsdk.IdentityVerification, error) {
	p.completeRequest = request
	return pluginsdk.IdentityVerification{Authenticated: true, Principal: &pluginsdk.Principal{ID: "oidc-user", Issuer: "https://idp.test", Subject: "oidc-user"}}, nil
}

func (p *recordingIdentityProvider) VerifyIdentity(_ context.Context, request pluginsdk.IdentityVerifyRequest) (pluginsdk.IdentityVerification, error) {
	p.request = request
	return pluginsdk.IdentityVerification{
		Authenticated: true,
		Principal:     &pluginsdk.Principal{ID: "user-" + request.Identifier, DisplayName: "Alice"},
	}, nil
}
