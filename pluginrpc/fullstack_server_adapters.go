package pluginrpc

import (
	"context"
	"fmt"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

func (s *rpcServer) APIHandle(req APIHandleRequest, reply *JSONReply) error {
	provider, closeFn, err := s.api(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	response, err := provider.HandleAPI(context.Background(), req.Request)
	if err != nil {
		return err
	}
	out, err := encodeJSON(response)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) IdentityVerify(req IdentityVerifyRequest, reply *JSONReply) error {
	provider, closeFn, err := s.identity(req.Instance)
	if err != nil {
		return err
	}
	defer closeFn()
	verification, err := provider.VerifyIdentity(context.Background(), req.Request)
	if err != nil {
		return err
	}
	out, err := encodeJSON(verification)
	if err != nil {
		return err
	}
	*reply = out
	return nil
}

func (s *rpcServer) api(payload InstancePayload) (pluginsdk.APIProvider, func(), error) {
	if s.plugin.NewAPI == nil {
		return nil, nil, fmt.Errorf("插件未实现 APIProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewAPI(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}

func (s *rpcServer) identity(payload InstancePayload) (pluginsdk.IdentityProvider, func(), error) {
	if s.plugin.NewIdentity == nil {
		return nil, nil, fmt.Errorf("插件未实现 IdentityProvider")
	}
	inst, secrets, closeFn, err := s.instance(payload)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.plugin.NewIdentity(context.Background(), inst, secrets)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return provider, closeFn, nil
}
