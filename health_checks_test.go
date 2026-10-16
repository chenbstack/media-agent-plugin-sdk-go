package pluginsdk

import (
	"strings"
	"testing"
)

func TestManifestHealthChecksValidate(t *testing.T) {
	base := Manifest{
		ID: "metadata-x", Name: "Metadata X", Version: "1.0.0", Type: "builtin",
		Capabilities: []string{"metadata.search"},
	}
	valid := base
	valid.HealthChecks = []HealthCheckDefinition{
		{ID: "api", Label: "接口"},
		{ID: "images", Label: "图片源", Impact: HealthImpactWarning},
	}
	if err := (Plugin{Manifest: valid}).Validate(); err != nil {
		t.Fatalf("合法声明应通过: %v", err)
	}
	cases := map[string]struct {
		checks []HealthCheckDefinition
		want   string
	}{
		"重复 id":     {[]HealthCheckDefinition{{ID: "api", Label: "a"}, {ID: "api", Label: "b"}}, "重复"},
		"非法 id":     {[]HealthCheckDefinition{{ID: "Bad ID", Label: "a"}}, "格式无效"},
		"缺 label":   {[]HealthCheckDefinition{{ID: "api", Label: " "}}, "label"},
		"非法 impact": {[]HealthCheckDefinition{{ID: "api", Label: "a", Impact: "fatal"}}, "impact"},
		"过多": {func() []HealthCheckDefinition {
			var out []HealthCheckDefinition
			for _, id := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9"} {
				out = append(out, HealthCheckDefinition{ID: id, Label: id})
			}
			return out
		}(), "最多"},
	}
	for name, tc := range cases {
		m := base
		m.HealthChecks = tc.checks
		if err := (Plugin{Manifest: m}).Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want 含 %q", name, err, tc.want)
		}
	}
}

func TestManifestHealthChecksParseFromYAML(t *testing.T) {
	m, err := ParseManifest([]byte(`
id: metadata-x
name: Metadata X
version: 1.0.0
type: cli
capabilities: [metadata.search]
health_checks:
  - id: api
    label: 接口
  - id: images
    label: 图片源
    impact: warning
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.HealthChecks) != 2 || m.HealthChecks[1].Impact != HealthImpactWarning {
		t.Fatalf("health_checks 解析结果不对: %+v", m.HealthChecks)
	}
}
