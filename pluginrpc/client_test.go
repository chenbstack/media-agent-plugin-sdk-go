package pluginrpc

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"testing"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
)

func TestRPCServerRegistersPluginMethods(t *testing.T) {
	cases := map[string]any{
		"rpcServer": &rpcServer{
			plugin: pluginsdk.Plugin{Manifest: pluginsdk.Manifest{ID: "test", Name: "Test"}},
		},
		"hostServicesServer": &hostServicesServer{},
		"uploadSourceServer": &uploadSourceServer{},
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			server := rpc.NewServer()
			if err := server.RegisterName("Plugin", target); err != nil {
				t.Fatalf("RegisterName returned error: %v", err)
			}
		})
	}
}

func TestClientCallReturnsWhenContextExpires(t *testing.T) {
	release := make(chan struct{})
	server := rpc.NewServer()
	if err := server.RegisterName("Plugin", &blockingRPCServer{release: release}); err != nil {
		t.Fatalf("RegisterName returned error: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go server.ServeConn(serverConn)

	client := &Client{client: rpc.NewClient(clientConn)}
	defer client.client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	var reply Empty
	err := client.call(ctx, "Plugin.Wait", Empty{}, &reply)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("call error = %v, want context deadline exceeded", err)
	}
	close(release)
}

func TestClientCallReportsLogicalPackActivity(t *testing.T) {
	server := rpc.NewServer()
	if err := server.RegisterName("Plugin", &immediateRPCServer{}); err != nil {
		t.Fatalf("RegisterName: %v", err)
	}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go server.ServeConn(serverConn)

	observer := &recordingActivityObserver{}
	client := &Client{
		client: rpc.NewClient(clientConn),
		manifest: pluginsdk.Manifest{
			ID: "family", Name: "Family",
		},
		packID: "official", scopeType: "plugin", scopeID: "global",
		activityObserver: observer,
	}
	defer client.client.Close()
	var reply Empty
	if err := client.call(context.Background(), "Plugin.Ping", Empty{}, &reply); err != nil {
		t.Fatalf("call: %v", err)
	}
	if observer.started.PluginID != "family" || observer.started.PackID != "official" || observer.started.Operation != "Plugin.Ping" {
		t.Fatalf("activity = %#v", observer.started)
	}
	if observer.completed != 1 {
		t.Fatalf("completed = %d", observer.completed)
	}
}

func TestAssessOnboardingRoundTrip(t *testing.T) {
	server := rpc.NewServer()
	impl := pluginsdk.Plugin{AssessOnboarding: func(_ context.Context, inst pluginsdk.Instance, _ pluginsdk.SecretResolver) (pluginsdk.OnboardingAssessment, error) {
		if inst.ID != "media-1" || inst.Config["base_url"] != "http://emby.local" {
			t.Fatalf("instance = %#v", inst)
		}
		return pluginsdk.OnboardingAssessment{Status: pluginsdk.OnboardingSatisfied, Reason: "已迁移"}, nil
	}}
	if err := server.RegisterName("Plugin", &rpcServer{plugin: impl}); err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go server.ServeConn(serverConn)
	client := &Client{client: rpc.NewClient(clientConn)}
	defer client.client.Close()
	result, err := client.AssessOnboardingContext(t.Context(), pluginsdk.Instance{
		ID: "media-1", Config: map[string]any{"base_url": "http://emby.local"},
	}, nil)
	if err != nil || result.Status != pluginsdk.OnboardingSatisfied || result.Reason != "已迁移" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type blockingRPCServer struct {
	release <-chan struct{}
}

type immediateRPCServer struct{}

func (*immediateRPCServer) Ping(_ Empty, _ *Empty) error { return nil }

type recordingActivityObserver struct {
	started   PluginActivityStartInfo
	completed int
}

func (o *recordingActivityObserver) PluginActivityStarted(info PluginActivityStartInfo) func() {
	o.started = info
	return func() { o.completed++ }
}

func (s *blockingRPCServer) Wait(_ Empty, _ *Empty) error {
	<-s.release
	return nil
}
