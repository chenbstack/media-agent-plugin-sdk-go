package pluginrpc

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// 站点插件迁出宿主进程后要靠这五项宿主服务活下去：渲染兜底、云端身份、管理员自有
// 规则目录，以及加密规则包的密文读取与实例绑定密钥。每一项都锁两件事——权限缺失时
// 必须失败（而不是静默返回空值），以及数据能原样跨过 RPC。前者尤其重要：这里三项
// 碰凭据或密钥，两项能读磁盘。

type stubPageRenderer struct{ available bool }

func (s stubPageRenderer) RendererAvailable(context.Context) bool { return s.available }

func (stubPageRenderer) RenderPage(_ context.Context, req providers.RenderRequest) (providers.RenderResult, error) {
	return providers.RenderResult{HTML: "<html>" + req.URL + "</html>", Status: 200}, nil
}

type stubCloudIdentity struct{}

func (stubCloudIdentity) CloudCredential(context.Context) (pluginsdk.CloudCredential, error) {
	return pluginsdk.CloudCredential{BaseURL: "https://cloud.test", Token: "short-lived", InstanceID: "inst-1"}, nil
}

type stubSiteRuleFiles struct{}

func (stubSiteRuleFiles) ListSiteRuleFiles(context.Context) ([]string, error) {
	return []string{"example.yaml"}, nil
}

func (stubSiteRuleFiles) ReadSiteRuleFile(_ context.Context, name string) ([]byte, error) {
	if name != "example.yaml" {
		return nil, errors.New("no such rule file")
	}
	return []byte("id: example\n"), nil
}

type stubSiteRulePackFiles struct{}

func (stubSiteRulePackFiles) ListSiteRulePackVersions(context.Context) ([]int64, error) {
	return []int64{41, 42}, nil
}

func (stubSiteRulePackFiles) ReadSiteRulePackFile(_ context.Context, version int64, name string) ([]byte, error) {
	if version != 42 || name != "rules.bin" {
		return nil, errors.New("no such pack entry")
	}
	return []byte{0x00, 0xff, 0x10}, nil
}

type stubSiteRulePackKeys struct{}

func (stubSiteRulePackKeys) InstanceKey(_ context.Context, packVersion int64) ([]byte, error) {
	if packVersion != 42 {
		return nil, errors.New("no such pack version")
	}
	return bytes.Repeat([]byte{0x7f}, 32), nil
}

func TestRendererServiceRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), renderer: stubPageRenderer{available: true}})
	var reply JSONReply
	if err := server.RendererAvailable(Empty{}, &reply); err == nil {
		t.Fatal("未声明 renderer.page 的插件不应查得到渲染可用性")
	}
	if err := server.RenderPage(RenderPageRequest{Input: providers.RenderRequest{URL: "https://site.test"}}, &reply); err == nil {
		t.Fatal("未声明 renderer.page 的插件不应能渲染页面")
	}

	server.live().permissions.Host = []string{"renderer.page"}
	if err := server.RendererAvailable(Empty{}, &reply); err != nil {
		t.Fatalf("RendererAvailable: %v", err)
	}
	var available bool
	if err := decodeJSON(reply.Data, &available); err != nil || !available {
		t.Fatalf("available = %v, err = %v", available, err)
	}
	if err := server.RenderPage(RenderPageRequest{Input: providers.RenderRequest{URL: "https://site.test"}}, &reply); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	var result providers.RenderResult
	if err := decodeJSON(reply.Data, &result); err != nil || !strings.Contains(result.HTML, "https://site.test") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestCloudIdentityServiceRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), cloud: stubCloudIdentity{}})
	var reply JSONReply
	if err := server.CloudCredential(Empty{}, &reply); err == nil {
		t.Fatal("未声明 cloud.identity 的插件不应拿到云端令牌")
	}
	server.live().permissions.Host = []string{"cloud.identity"}
	if err := server.CloudCredential(Empty{}, &reply); err != nil {
		t.Fatalf("CloudCredential: %v", err)
	}
	var cred pluginsdk.CloudCredential
	if err := decodeJSON(reply.Data, &cred); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cred.Token != "short-lived" || cred.BaseURL != "https://cloud.test" {
		t.Fatalf("credential = %+v", cred)
	}
}

func TestSiteRuleFilesServiceRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), siteRules: stubSiteRuleFiles{}})
	var listReply JSONReply
	if err := server.ListSiteRuleFiles(Empty{}, &listReply); err == nil {
		t.Fatal("未声明 site.rules.read 的插件不应列出规则目录")
	}
	var bytesReply BytesReply
	if err := server.ReadSiteRuleFile(SiteRuleFileRequest{Name: "example.yaml"}, &bytesReply); err == nil {
		t.Fatal("未声明 site.rules.read 的插件不应读到规则文件")
	}

	server.live().permissions.Host = []string{"site.rules.read"}
	if err := server.ListSiteRuleFiles(Empty{}, &listReply); err != nil {
		t.Fatalf("ListSiteRuleFiles: %v", err)
	}
	var names []string
	if err := decodeJSON(listReply.Data, &names); err != nil || len(names) != 1 || names[0] != "example.yaml" {
		t.Fatalf("names = %v, err = %v", names, err)
	}
	if err := server.ReadSiteRuleFile(SiteRuleFileRequest{Name: "example.yaml"}, &bytesReply); err != nil {
		t.Fatalf("ReadSiteRuleFile: %v", err)
	}
	if string(bytesReply.Data) != "id: example\n" {
		t.Fatalf("data = %q", bytesReply.Data)
	}
}

// 宿主没提供某项服务时，插件拿到的必须是明确错误。空实现加静默成功会让站点插件
// 以为「渲染不可用」「云端没有规则」，用户侧只看到搜不到结果。
func TestSitePluginServicesFailWhenHostDoesNotProvide(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{
		ctx: context.Background(),
		permissions: pluginsdk.Permissions{Host: []string{
			"renderer.page", "cloud.identity", "site.rules.read",
			"site.rules.pack.read", "site.rules.pack.keys",
		}},
	})
	var reply JSONReply
	if err := server.RenderPage(RenderPageRequest{}, &reply); err == nil {
		t.Fatal("宿主未提供 PageRenderer 时应报错")
	}
	if err := server.CloudCredential(Empty{}, &reply); err == nil {
		t.Fatal("宿主未提供 CloudIdentity 时应报错")
	}
	if err := server.ListSiteRuleFiles(Empty{}, &reply); err == nil {
		t.Fatal("宿主未提供 SiteRuleFiles 时应报错")
	}
	if err := server.ListSiteRulePackVersions(Empty{}, &reply); err == nil {
		t.Fatal("宿主未提供 SiteRulePackFiles 时应报错")
	}
	var bytesReply BytesReply
	if err := server.ReadSiteRulePackFile(SiteRulePackFileRequest{Version: 42, Name: "rules.bin"}, &bytesReply); err == nil {
		t.Fatal("宿主未提供 SiteRulePackFiles 时应报错")
	}
	if err := server.InstanceKey(SiteRulePackKeyRequest{Version: 42}, &bytesReply); err == nil {
		t.Fatal("宿主未提供 SiteRulePackKeys 时应报错")
	}
}

// 五项站点服务任意一项单独出现时，都必须让宿主为这次调用开出回调通道。漏掉一项的
// 表现是插件侧字段为 nil——功能没了，日志里却什么都没有。
func TestNeedsHostServicesCoversSitePluginServices(t *testing.T) {
	if needsHostServices(pluginsdk.Instance{}, nil) {
		t.Fatal("空实例不该开回调通道")
	}
	cases := map[string]pluginsdk.Instance{
		"renderer":            {Renderer: stubPageRenderer{}},
		"cloud":               {Cloud: stubCloudIdentity{}},
		"site_rules":          {SiteRules: stubSiteRuleFiles{}},
		"site_rule_packs":     {SiteRulePacks: stubSiteRulePackFiles{}},
		"site_rule_pack_keys": {SiteRulePackKeys: stubSiteRulePackKeys{}},
	}
	for name, inst := range cases {
		if !needsHostServices(inst, nil) {
			t.Fatalf("只提供 %s 时未开回调通道", name)
		}
	}
}

func TestSiteRulePackFilesServiceRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), siteRulePacks: stubSiteRulePackFiles{}})
	var listReply JSONReply
	if err := server.ListSiteRulePackVersions(Empty{}, &listReply); err == nil {
		t.Fatal("未声明 site.rules.pack.read 的插件不应列出规则包版本")
	}
	var bytesReply BytesReply
	if err := server.ReadSiteRulePackFile(SiteRulePackFileRequest{Version: 42, Name: "rules.bin"}, &bytesReply); err == nil {
		t.Fatal("未声明 site.rules.pack.read 的插件不应读到规则包条目")
	}

	server.live().permissions.Host = []string{"site.rules.pack.read"}
	if err := server.ListSiteRulePackVersions(Empty{}, &listReply); err != nil {
		t.Fatalf("ListSiteRulePackVersions: %v", err)
	}
	var versions []int64
	if err := decodeJSON(listReply.Data, &versions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(versions) != 2 || versions[1] != 42 {
		t.Fatalf("versions = %v", versions)
	}
	if err := server.ReadSiteRulePackFile(SiteRulePackFileRequest{Version: 42, Name: "rules.bin"}, &bytesReply); err != nil {
		t.Fatalf("ReadSiteRulePackFile: %v", err)
	}
	// 密文按原始字节过 RPC：base64 化的 rules.bin 白白膨胀三分之一。
	if !bytes.Equal(bytesReply.Data, []byte{0x00, 0xff, 0x10}) {
		t.Fatalf("data = %v", bytesReply.Data)
	}
}

func TestSiteRulePackKeysServiceRequiresPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), siteRulePackKeys: stubSiteRulePackKeys{}})
	var reply BytesReply
	if err := server.InstanceKey(SiteRulePackKeyRequest{Version: 42}, &reply); err == nil {
		t.Fatal("未声明 site.rules.pack.keys 的插件不应拿到实例绑定密钥")
	}

	server.live().permissions.Host = []string{"site.rules.pack.keys"}
	if err := server.InstanceKey(SiteRulePackKeyRequest{Version: 42}, &reply); err != nil {
		t.Fatalf("InstanceKey: %v", err)
	}
	if !bytes.Equal(reply.Data, bytes.Repeat([]byte{0x7f}, 32)) {
		t.Fatalf("key = %x", reply.Data)
	}
	// 版本参与派生，换包就换密钥——版本必须原样传到宿主实现，不能被适配器吞掉。
	if err := server.InstanceKey(SiteRulePackKeyRequest{Version: 41}, &reply); err == nil {
		t.Fatal("桩实现只认版本 42，换个版本仍然成功说明版本没被当作入参传下去")
	}
}

// assembleInstance 是插件那一侧的对称陷阱：宿主明明开了通道，插件进程里字段却是
// nil，功能同样静默消失。这里逐项断言五项站点服务都挂上了句柄。
func TestAssembleInstanceCoversSitePluginServices(t *testing.T) {
	server := &rpcServer{}
	inst, err := server.assembleInstance(InstancePayload{
		ID:                   "site",
		ConfigJSON:           []byte("{}"),
		HostServicesBrokerID: 1,
	}, &hostServicesClient{})
	if err != nil {
		t.Fatalf("assembleInstance: %v", err)
	}
	missing := map[string]bool{
		"Renderer":         inst.Renderer == nil,
		"Cloud":            inst.Cloud == nil,
		"SiteRules":        inst.SiteRules == nil,
		"SiteRulePacks":    inst.SiteRulePacks == nil,
		"SiteRulePackKeys": inst.SiteRulePackKeys == nil,
	}
	for name, absent := range missing {
		if absent {
			t.Fatalf("assembleInstance 没挂上 %s", name)
		}
	}
}
