package pluginrpc

import (
	"context"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

var _ pluginsdk.MediaLibrary = (*hostServicesClient)(nil)
var _ pluginsdk.MediaArchive = (*hostServicesClient)(nil)

type stubMediaLibrary struct{}

func (stubMediaLibrary) List(_ context.Context, query pluginsdk.MediaLibraryQuery) ([]pluginsdk.MediaLibraryItem, error) {
	return []pluginsdk.MediaLibraryItem{{FileRef: "file-1", Title: query.Query, StorageID: "hot"}}, nil
}

type stubMediaArchive struct{}

func (stubMediaArchive) Archive(_ context.Context, input pluginsdk.MediaArchiveInput) (pluginsdk.MediaArchiveResult, error) {
	return pluginsdk.MediaArchiveResult{Completed: len(input.FileRefs)}, nil
}

func TestMediaArchiveHostServicesRequireSeparatePermissions(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), mediaLibrary: stubMediaLibrary{}, mediaArchive: stubMediaArchive{}})
	var reply JSONReply
	if err := server.ListMediaLibrary(MediaLibraryListRequest{}, &reply); err == nil {
		t.Fatal("media.library.read 未声明时不应读取")
	}
	server.live().permissions.Host = []string{"media.library.read"}
	if err := server.ListMediaLibrary(MediaLibraryListRequest{Query: pluginsdk.MediaLibraryQuery{Query: "旧电影"}}, &reply); err != nil {
		t.Fatalf("ListMediaLibrary: %v", err)
	}
	if err := server.ArchiveMedia(MediaArchiveRequest{Input: pluginsdk.MediaArchiveInput{FileRefs: []string{"file-1"}}}, &reply); err == nil {
		t.Fatal("media.archive.write 未声明时不应执行")
	}
	server.live().permissions.Host = append(server.live().permissions.Host, "media.archive.write")
	if err := server.ArchiveMedia(MediaArchiveRequest{Input: pluginsdk.MediaArchiveInput{FileRefs: []string{"file-1"}}}, &reply); err != nil {
		t.Fatalf("ArchiveMedia: %v", err)
	}
}

func TestArchiveCapabilitiesOpenAndAttachHostChannel(t *testing.T) {
	if !needsHostServices(pluginsdk.Instance{MediaLibrary: stubMediaLibrary{}}, nil) || !needsHostServices(pluginsdk.Instance{MediaArchive: stubMediaArchive{}}, nil) {
		t.Fatal("归档能力未触发宿主回调通道")
	}
	inst, err := (&rpcServer{}).assembleInstance(InstancePayload{ID: "archive", ConfigJSON: []byte("{}"), HostServicesBrokerID: 1}, &hostServicesClient{})
	if err != nil {
		t.Fatal(err)
	}
	if inst.MediaLibrary == nil || inst.MediaArchive == nil {
		t.Fatalf("assembleInstance 未挂上归档能力: %+v", inst)
	}
}
