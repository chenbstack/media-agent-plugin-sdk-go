package pluginsdk

import "context"

const CapabilityHTTPService = "service.http"

// HTTPServiceDefinition declares a long-lived HTTP data plane owned by a
// plugin. The host matches PublicHostConfigField against the request Host and,
// when PathPrefix is set, only proxies requests under that path to the plugin.
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
