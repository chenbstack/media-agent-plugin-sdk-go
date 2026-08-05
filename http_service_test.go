package pluginsdk

import "testing"

func TestHTTPServiceManifestValidation(t *testing.T) {
	valid := Plugin{Manifest: Manifest{
		ID: "proxy", Name: "Proxy", Version: "0.1.0", Type: "cli",
		Capabilities: []string{CapabilityHTTPService},
		HTTPServices: []HTTPServiceDefinition{{Name: "emby", PublicHostConfigField: "public_host", PathPrefix: "/proxy", Methods: []string{"GET", "HEAD"}, Streaming: true}},
		Resources:    Resources{MemoryLimitMB: 64},
	}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	missing := valid
	missing.Manifest.HTTPServices = nil
	if err := missing.Validate(); err == nil {
		t.Fatal("service.http without http_services must fail")
	}
	badHostField := valid
	badHostField.Manifest.HTTPServices = []HTTPServiceDefinition{{Name: "emby", PublicHostConfigField: "bad field"}}
	if err := badHostField.Validate(); err == nil {
		t.Fatal("invalid public host field must fail")
	}
	badPath := valid
	badPath.Manifest.HTTPServices = []HTTPServiceDefinition{{Name: "emby", PublicHostConfigField: "public_host", PathPrefix: "proxy"}}
	if err := badPath.Validate(); err == nil {
		t.Fatal("relative path prefix must fail")
	}
}
