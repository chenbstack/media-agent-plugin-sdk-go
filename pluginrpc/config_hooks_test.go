package pluginrpc

import (
	"context"
	"errors"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

// 站点插件在配置校验时要读站点规则才知道该站点要哪些认证字段，而规则来源挂在实例上。
// 只发配置不发实例的话，跨进程的站点插件只能退回默认的「cookie 必填」，用 api_key 的
// 站点从此保存不了配置——这条锁住实例确实跨过了进程边界。
func TestValidateConfigCarriesInstance(t *testing.T) {
	var gotInstance string
	var gotConfig map[string]any
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "site", Name: "Site"},
		ValidateConfigWithInstance: func(_ context.Context, inst pluginsdk.Instance, config map[string]any) error {
			gotInstance, gotConfig = inst.ID, config
			return nil
		},
	})

	if err := client.ValidateConfigWithInstanceContext(context.Background(),
		pluginsdk.Instance{ID: "global"}, map[string]any{"base_url": "https://demo.example"}); err != nil {
		t.Fatalf("ValidateConfigWithInstanceContext: %v", err)
	}
	if gotInstance != "global" {
		t.Fatalf("实例未透传: %q", gotInstance)
	}
	if gotConfig["base_url"] != "https://demo.example" {
		t.Fatalf("配置未透传: %v", gotConfig)
	}
}

// 校验失败要原样回到宿主：新建连接表单靠这条错误标出该填哪个字段。
func TestValidateConfigWithInstanceError(t *testing.T) {
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "site", Name: "Site"},
		ValidateConfigWithInstance: func(context.Context, pluginsdk.Instance, map[string]any) error {
			return errors.New("api_key: 必填")
		},
	})
	err := client.ValidateConfigWithInstanceContext(context.Background(), pluginsdk.Instance{ID: "global"}, nil)
	if err == nil || err.Error() != "api_key: 必填" {
		t.Fatalf("err = %v", err)
	}
}

// 只实现了老钩子的插件不能因为宿主升级而失效。
func TestValidateConfigFallsBackToLegacyHook(t *testing.T) {
	called := false
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest:       pluginsdk.Manifest{ID: "legacy", Name: "Legacy"},
		ValidateConfig: func(map[string]any) error { called = true; return nil },
	})
	if err := client.ValidateConfigWithInstanceContext(context.Background(), pluginsdk.Instance{ID: "global"}, nil); err != nil {
		t.Fatalf("ValidateConfigWithInstanceContext: %v", err)
	}
	if !called {
		t.Fatal("老钩子未被调用")
	}
}

// 动态 schema 是站点插件认证字段的唯一来源：宿主拿到的字段集合必须来自插件本次解析，
// 而不是打包时的静态声明。
func TestConfigSchemaForInstanceRoundTrip(t *testing.T) {
	var gotInstance string
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest:     pluginsdk.Manifest{ID: "site", Name: "Site"},
		ConfigSchema: pluginsdk.ConfigSchema{Fields: []pluginsdk.Field{{Name: "base_url"}}},
		ConfigSchemaForInstance: func(_ context.Context, inst pluginsdk.Instance, config map[string]any) (pluginsdk.ConfigSchema, error) {
			gotInstance = inst.ID
			return pluginsdk.ConfigSchema{Fields: []pluginsdk.Field{
				{Name: "base_url"},
				{Name: "api_key", Required: true, Secret: true},
			}}, nil
		},
	})

	schema, err := client.ConfigSchemaForInstanceContext(context.Background(),
		pluginsdk.Instance{ID: "global"}, map[string]any{"base_url": "https://demo.example"})
	if err != nil {
		t.Fatalf("ConfigSchemaForInstanceContext: %v", err)
	}
	if gotInstance != "global" {
		t.Fatalf("实例未透传: %q", gotInstance)
	}
	if len(schema.Fields) != 2 || schema.Fields[1].Name != "api_key" || !schema.Fields[1].Secret {
		t.Fatalf("动态字段未透传: %+v", schema.Fields)
	}
}

// 没实现任何动态钩子的插件也要回一份可用的 schema，宿主不必分两种调用方式。
func TestConfigSchemaForInstanceFallsBack(t *testing.T) {
	static := pluginsdk.ConfigSchema{Fields: []pluginsdk.Field{{Name: "token"}}}
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest:     pluginsdk.Manifest{ID: "plain", Name: "Plain"},
		ConfigSchema: static,
	})
	schema, err := client.ConfigSchemaForInstanceContext(context.Background(), pluginsdk.Instance{ID: "global"}, nil)
	if err != nil {
		t.Fatalf("ConfigSchemaForInstanceContext: %v", err)
	}
	if len(schema.Fields) != 1 || schema.Fields[0].Name != "token" {
		t.Fatalf("未退回静态声明: %+v", schema.Fields)
	}
}

// 解析失败必须是错误，不能悄悄变成一份空 schema——那等于把用户填好的认证字段
// 判成「未声明的字段」，整份配置连保存都保存不了，而日志里什么都没有。
func TestConfigSchemaForInstanceError(t *testing.T) {
	client := newProviderTestClient(t, pluginsdk.Plugin{
		Manifest: pluginsdk.Manifest{ID: "site", Name: "Site"},
		ConfigSchemaForInstance: func(context.Context, pluginsdk.Instance, map[string]any) (pluginsdk.ConfigSchema, error) {
			return pluginsdk.ConfigSchema{}, errors.New("规则目录不可读")
		},
	})
	if _, err := client.ConfigSchemaForInstanceContext(context.Background(), pluginsdk.Instance{ID: "global"}, nil); err == nil {
		t.Fatal("want error")
	}
}

// 宿主只对声明了 capability 的插件挂动态 schema 钩子：钩子为 nil 就是「用静态声明」，
// 宿主不必再比对一次 manifest，也不会为 18 个静态插件白发一次 RPC。
func TestExternalPluginDynamicSchemaGatedByCapability(t *testing.T) {
	withCapability := ExternalPlugin{Manifest: pluginsdk.Manifest{
		ID: "site", Capabilities: []string{pluginsdk.CapabilityDynamicConfigSchema},
	}}.Plugin()
	if withCapability.ConfigSchemaForConfig == nil {
		t.Fatal("声明了动态 schema 的插件应挂上钩子")
	}
	without := ExternalPlugin{Manifest: pluginsdk.Manifest{ID: "plain"}}.Plugin()
	if without.ConfigSchemaForConfig != nil {
		t.Fatal("未声明动态 schema 的插件不应挂钩子")
	}
}
