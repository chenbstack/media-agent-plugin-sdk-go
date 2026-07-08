package pluginrpc

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"testing"
	"time"

	"media-agent-lab/server/pkg/pluginsdk"
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

type blockingRPCServer struct {
	release <-chan struct{}
}

func (s *blockingRPCServer) Wait(_ Empty, _ *Empty) error {
	<-s.release
	return nil
}
