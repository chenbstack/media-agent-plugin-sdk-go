package pluginsdk

import (
	"strings"
	"testing"
)

func TestModelUIManifestRoundTripAndValidation(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
id: model
name: Model
version: 0.1.0
type: builtin
capabilities: [model_provider.generate, model_provider.download, model_provider.uninstall, model_provider.speed_test]
model_ui:
  description: 本机模型
  summary:
    - { label: 模型文件, field: model_name, format: model_file }
    - { label: 来源, field: download_url, format: host }
  download: { label: 一键下载, pending_label: 下载中 }
  uninstall: { label: 卸载, pending_label: 卸载中 }
  speed_test: { label: 性能测试, pending_label: 测试中 }
permissions: {}
resources: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ModelUI == nil || len(manifest.ModelUI.Summary) != 2 || manifest.ModelUI.Download.Label != "一键下载" {
		t.Fatalf("model_ui parse mismatch: %+v", manifest.ModelUI)
	}
	if err := (Plugin{Manifest: manifest}).Validate(); err != nil {
		t.Fatalf("valid model_ui rejected: %v", err)
	}
}

func TestModelUIValidationRejectsUnsupportedFieldAndOperation(t *testing.T) {
	base := Manifest{
		ID: "model", Name: "Model", Version: "0.1.0", Type: "builtin",
		Capabilities: []string{"model_provider.generate"},
		ModelUI: &ModelUI{
			Description: "模型",
			Summary:     []ModelUISummaryField{{Label: "Secret", Field: "api_key"}},
		},
	}
	if err := (Plugin{Manifest: base}).Validate(); err == nil || !strings.Contains(err.Error(), "字段声明无效") {
		t.Fatalf("unsafe summary field should fail: %v", err)
	}
	base.ModelUI.Summary = []ModelUISummaryField{{Label: "模型", Field: "model_name"}}
	base.ModelUI.Download = &ModelUIOperation{Label: "下载", PendingLabel: "下载中"}
	if err := (Plugin{Manifest: base}).Validate(); err == nil || !strings.Contains(err.Error(), "model_provider.download") {
		t.Fatalf("operation without capability should fail: %v", err)
	}
}
