package pluginrpc

import (
	"context"
	"errors"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

type healthMetadataProvider struct{ rpcMetadataProvider }

func (*healthMetadataProvider) HealthChecks(context.Context) ([]providers.HealthCheckResult, error) {
	return []providers.HealthCheckResult{
		{ID: "api", Status: providers.HealthStatusOK, LatencyMS: 12},
		{ID: "images", Status: providers.HealthStatusWarning, Message: "图片源不可达"},
	}, nil
}

func TestHealthChecksRoundTripOverRPC(t *testing.T) {
	plugin := pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "official", Name: "Official"},
		NewMetadata: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.MetadataProvider, error) {
			return &healthMetadataProvider{}, nil
		},
		NewSite: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.SiteProvider, error) {
			return &rpcSiteProvider{}, nil
		},
	}
	client := newProviderTestClient(t, plugin)
	inst := pluginsdk.Instance{ID: "instance", Name: "Instance", Config: map[string]any{"base_url": "https://example.test"}}

	reporter, ok := client.Metadata(inst, nil).(providers.HealthReporter)
	if !ok {
		t.Fatal("元数据客户端适配器应实现 HealthReporter")
	}
	got, err := reporter.HealthChecks(context.Background())
	if err != nil || len(got) != 2 || got[0].LatencyMS != 12 || got[1].Message != "图片源不可达" {
		t.Fatalf("HealthChecks = %+v, %v", got, err)
	}

	siteReporter, ok := client.Site(inst, nil).(providers.HealthReporter)
	if !ok {
		t.Fatal("站点客户端适配器应实现 HealthReporter")
	}
	if _, err := siteReporter.HealthChecks(context.Background()); !errors.Is(err, providers.ErrHealthChecksUnsupported) {
		t.Fatalf("未实现 HealthReporter 的 Provider 应返回哨兵错误，得到 %v", err)
	}
}
