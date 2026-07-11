package pluginsdk

import "testing"

func TestActionPermissionsMustBeDeclaredByPlugin(t *testing.T) {
	base := Manifest{
		ID: "action-permissions", Name: "Action Permissions", Version: "1.0.0", Type: "builtin",
		Capabilities: []string{"action.run"},
		Permissions:  Permissions{Secrets: []string{"plugin.example.password"}, Data: []string{"storage"}},
	}
	valid := base
	valid.Actions = []ActionDefinition{{ID: "test", Name: "测试", Permissions: &Permissions{Secrets: []string{"plugin.example.password"}}}}
	if err := NewRegistry().Register(Plugin{Manifest: valid}); err != nil {
		t.Fatalf("valid action permissions rejected: %v", err)
	}
	invalid := base
	invalid.Actions = []ActionDefinition{{ID: "test", Name: "测试", Permissions: &Permissions{Host: []string{"rules.write"}}}}
	if err := NewRegistry().Register(Plugin{Manifest: invalid}); err == nil {
		t.Fatal("undeclared action permission was accepted")
	}
}
