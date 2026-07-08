package pluginrpc

import (
	"context"
	"io"
	"net/rpc"

	hcplugin "github.com/hashicorp/go-plugin"

	"media-agent-lab/server/pkg/pluginsdk/providers"
)

type uploadSourceServer struct {
	ctx    context.Context
	source providers.UploadSource
	broker *hcplugin.MuxBroker
}

type OpenRangeRequest struct {
	Offset int64
	Length int64
}

func (s *uploadSourceServer) Name(args Empty, reply *StringReply) error {
	reply.Value = s.source.Name()
	return nil
}

func (s *uploadSourceServer) Size(args Empty, reply *Int64Reply) error {
	reply.Value = s.source.Size()
	return nil
}

func (s *uploadSourceServer) SHA1(args Empty, reply *StringReply) error {
	value, err := s.source.SHA1(s.ctx)
	if err != nil {
		return err
	}
	reply.Value = value
	return nil
}

func (s *uploadSourceServer) Open(args Empty, reply *BrokerReply) error {
	reply.ID = serveReader(s.broker, func() (io.ReadCloser, error) {
		return s.source.Open(s.ctx)
	})
	return nil
}

func (s *uploadSourceServer) OpenRange(req OpenRangeRequest, reply *BrokerReply) error {
	reply.ID = serveReader(s.broker, func() (io.ReadCloser, error) {
		return s.source.OpenRange(s.ctx, req.Offset, req.Length)
	})
	return nil
}

type remoteUploadSource struct {
	client *rpc.Client
	broker *hcplugin.MuxBroker
}

func (s *remoteUploadSource) Name() string {
	var reply StringReply
	if err := s.client.Call("Plugin.Name", Empty{}, &reply); err != nil {
		return ""
	}
	return reply.Value
}

func (s *remoteUploadSource) Size() int64 {
	var reply Int64Reply
	if err := s.client.Call("Plugin.Size", Empty{}, &reply); err != nil {
		return 0
	}
	return reply.Value
}

func (s *remoteUploadSource) SHA1(ctx context.Context) (string, error) {
	var reply StringReply
	if err := s.client.Call("Plugin.SHA1", Empty{}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}

func (s *remoteUploadSource) Open(ctx context.Context) (io.ReadCloser, error) {
	var reply BrokerReply
	if err := s.client.Call("Plugin.Open", Empty{}, &reply); err != nil {
		return nil, err
	}
	conn, err := s.broker.Dial(reply.ID)
	if err != nil {
		return nil, err
	}
	return closeReadConn{Conn: conn}, nil
}

func (s *remoteUploadSource) OpenRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	var reply BrokerReply
	if err := s.client.Call("Plugin.OpenRange", OpenRangeRequest{Offset: offset, Length: length}, &reply); err != nil {
		return nil, err
	}
	conn, err := s.broker.Dial(reply.ID)
	if err != nil {
		return nil, err
	}
	return closeReadConn{Conn: conn}, nil
}
