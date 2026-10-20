package pluginrpc

import (
	"context"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

var _ pluginsdk.MediaServerMetadata = (*hostServicesClient)(nil)

type stubMediaServerMetadata struct{}

func (stubMediaServerMetadata) ListServers(context.Context) ([]pluginsdk.MetadataServer, error) {
	return []pluginsdk.MetadataServer{{ID: "s1", Name: "Emby"}}, nil
}

func (stubMediaServerMetadata) ListItems(context.Context, pluginsdk.MetadataItemQuery) (pluginsdk.MetadataItemPage, error) {
	return pluginsdk.MetadataItemPage{Total: 1, Items: []pluginsdk.MetadataItem{{ServerID: "s1", ItemID: "i1"}}}, nil
}

func (stubMediaServerMetadata) RefreshItems(_ context.Context, input pluginsdk.MetadataRefreshInput) (pluginsdk.MetadataRefreshResult, error) {
	return pluginsdk.MetadataRefreshResult{Refreshed: len(input.ItemIDs)}, nil
}

func TestMediaServerMetadataNeedsPermission(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), mediaServerMetadata: stubMediaServerMetadata{}})
	var reply JSONReply
	if err := server.ListMetadataServers(Empty{}, &reply); err == nil {
		t.Fatal("media.server.metadata 未声明时不应列出服务器")
	}
	if err := server.RefreshMetadataItems(MediaServerMetadataRefreshInput{}, &reply); err == nil {
		t.Fatal("media.server.metadata 未声明时不应刷新")
	}
	server.live().permissions.Host = []string{"media.server.metadata"}
	if err := server.ListMetadataServers(Empty{}, &reply); err != nil {
		t.Fatalf("ListMetadataServers: %v", err)
	}
	if err := server.ListMetadataItems(MediaServerMetadataItemsQuery{}, &reply); err != nil {
		t.Fatalf("ListMetadataItems: %v", err)
	}
	if err := server.RefreshMetadataItems(MediaServerMetadataRefreshInput{Input: pluginsdk.MetadataRefreshInput{ItemIDs: []string{"i1"}}}, &reply); err != nil {
		t.Fatalf("RefreshMetadataItems: %v", err)
	}

	missing := *newHostServicesServer(&hostServicesState{ctx: context.Background()})
	missing.live().permissions.Host = []string{"media.server.metadata"}
	if err := missing.ListMetadataServers(Empty{}, &reply); err == nil {
		t.Fatal("宿主未提供能力时应报错")
	}
}

func TestMediaServerMetadataOpensHostChannel(t *testing.T) {
	if !needsHostServices(pluginsdk.Instance{MediaServerMetadata: stubMediaServerMetadata{}}, nil) {
		t.Fatal("元数据刷新能力未触发宿主回调通道")
	}
}
