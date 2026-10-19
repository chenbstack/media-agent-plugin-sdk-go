package pluginrpc

import (
	"context"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

var _ pluginsdk.MetadataSources = (*hostServicesClient)(nil)

type stubMetadataSources struct{}

func (stubMetadataSources) ListMetadataSources(context.Context) ([]pluginsdk.MetadataSourceInfo, error) {
	return []pluginsdk.MetadataSourceInfo{{Kind: "tmdb", Name: "TMDB"}}, nil
}

func TestMetadataSourcesRequirePermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), metadataSources: stubMetadataSources{}})
	var reply JSONReply
	if err := server.ListMetadataSources(Empty{}, &reply); err == nil {
		t.Fatal("metadata.sources.read 未声明时不应读取")
	}
	server.live().permissions.Host = []string{"metadata.sources.read"}
	if err := server.ListMetadataSources(Empty{}, &reply); err != nil {
		t.Fatalf("ListMetadataSources: %v", err)
	}
	var items []pluginsdk.MetadataSourceInfo
	if err := decodeJSON(reply.Data, &items); err != nil || len(items) != 1 || items[0].Kind != "tmdb" {
		t.Fatalf("返回内容不对: %+v %v", items, err)
	}
}

// 漏掉任何一项，插件侧字段就是 nil：功能静默消失。
func TestMetadataSourcesOpenAndAttachHostChannel(t *testing.T) {
	if !needsHostServices(pluginsdk.Instance{MetadataSources: stubMetadataSources{}}, nil) {
		t.Fatal("元数据源能力未触发宿主回调通道")
	}
	inst, err := (&rpcServer{}).assembleInstance(InstancePayload{ID: "x", ConfigJSON: []byte("{}"), HostServicesBrokerID: 1}, &hostServicesClient{})
	if err != nil || inst.MetadataSources == nil {
		t.Fatalf("assembleInstance 未挂上元数据源能力: %+v %v", inst, err)
	}
}
