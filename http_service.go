package pluginsdk

import (
	"context"
	"strings"
)

const CapabilityHTTPService = "service.http"

// DefaultHTTPServicePathPrefix is the namespace used for plugin-owned HTTP
// services that do not declare an explicit path_prefix.
const DefaultHTTPServicePathPrefix = "/api/v1/plugins"

// DefaultHTTPServicePath returns the Host-managed route for a plugin service.
func DefaultHTTPServicePath(pluginID, serviceName string) string {
	return strings.TrimRight(DefaultHTTPServicePathPrefix, "/") + "/" + strings.Trim(pluginID, "/") + "/" + strings.Trim(serviceName, "/")
}

// HTTPServiceDefinition declares a long-lived HTTP data plane owned by a
// plugin. A non-empty PublicHostConfigField routes on that configured Host;
// when it is empty, the service is available on the Host's current entrypoint.
// An empty PathPrefix receives a stable Host-managed plugin/service prefix.
type HTTPServiceDefinition struct {
	Name                  string   `yaml:"name" json:"name"`
	PublicHostConfigField string   `yaml:"public_host_config_field" json:"public_host_config_field"`
	PathPrefix            string   `yaml:"path_prefix,omitempty" json:"path_prefix,omitempty"`
	Methods               []string `yaml:"methods,omitempty" json:"methods,omitempty"`
	Streaming             bool     `yaml:"streaming,omitempty" json:"streaming,omitempty"`
	WebSocket             bool     `yaml:"websocket,omitempty" json:"websocket,omitempty"`
}

type HTTPServiceOptions struct {
	ListenHost string `json:"listen_host"`
	ListenPort int    `json:"listen_port"`
	BasePath   string `json:"base_path,omitempty"`
}

type HTTPServiceInfo struct {
	BaseURL    string `json:"base_url"`
	HealthPath string `json:"health_path,omitempty"`
}

// HTTPService is a plugin-managed, long-lived loopback HTTP service. Start
// returns after the listener is ready; Stop must be idempotent.
type HTTPService interface {
	Start(context.Context, HTTPServiceOptions) (HTTPServiceInfo, error)
	Stop(context.Context) error
}
