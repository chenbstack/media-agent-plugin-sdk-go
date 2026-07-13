package pluginsdk

import (
	"strings"
	"testing"
)

func TestOnboardingWorkflowParsesAndValidates(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
id: importer
name: Importer
version: 1.0.0
type: builtin
capabilities: [onboarding.connection, action.run, action.status]
onboarding:
  submit_action: sync
  submit_label: 开始同步
  pending_label: 正在同步…
  status_action: status
actions:
  - id: test
    name: 测试
  - id: sync
    name: 同步
  - id: status
    name: 状态
permissions: {}
resources: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Onboarding == nil || manifest.Onboarding.SubmitAction != "sync" ||
		manifest.Onboarding.SubmitLabel != "开始同步" || manifest.Onboarding.PendingLabel != "正在同步…" ||
		manifest.Onboarding.StatusAction != "status" {
		t.Fatalf("onboarding workflow = %+v", manifest.Onboarding)
	}
	if err := (Plugin{Manifest: manifest}).Validate(); err != nil {
		t.Fatalf("valid onboarding workflow rejected: %v", err)
	}
}

func TestOnboardingWorkflowRejectsInvalidActionReferences(t *testing.T) {
	base := Manifest{
		ID: "importer", Name: "Importer", Version: "1.0.0", Type: "builtin",
		Capabilities: []string{CapabilityOnboardingConnection, "action.run", "action.status"},
		Actions: []ActionDefinition{
			{ID: "sync", Name: "同步"},
			{ID: "status", Name: "状态"},
		},
	}
	for _, test := range []struct {
		name     string
		workflow OnboardingWorkflow
		want     string
	}{
		{name: "missing submit", workflow: OnboardingWorkflow{SubmitAction: "missing", SubmitLabel: "开始"}, want: "submit_action"},
		{name: "empty label", workflow: OnboardingWorkflow{SubmitAction: "sync"}, want: "submit_label"},
		{name: "missing status", workflow: OnboardingWorkflow{SubmitAction: "sync", SubmitLabel: "开始", StatusAction: "missing"}, want: "status_action"},
		{name: "same actions", workflow: OnboardingWorkflow{SubmitAction: "sync", SubmitLabel: "开始", StatusAction: "sync"}, want: "不能与 submit_action 相同"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := base
			manifest.Onboarding = &test.workflow
			if err := (Plugin{Manifest: manifest}).Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}
