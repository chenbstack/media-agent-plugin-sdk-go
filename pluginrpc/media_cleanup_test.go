package pluginrpc

import (
	"context"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

var _ pluginsdk.MediaDelete = (*hostServicesClient)(nil)
var _ pluginsdk.SubscriptionCleanup = (*hostServicesClient)(nil)

type stubMediaDelete struct{}

func (stubMediaDelete) DeleteMediaFiles(_ context.Context, input pluginsdk.MediaDeleteInput) (pluginsdk.MediaDeleteResult, error) {
	return pluginsdk.MediaDeleteResult{Deleted: len(input.FileRefs)}, nil
}

type stubSubscriptionCleanup struct{}

func (stubSubscriptionCleanup) ListSubscriptions(context.Context, pluginsdk.SubscriptionQuery) ([]pluginsdk.SubscriptionInfo, error) {
	return []pluginsdk.SubscriptionInfo{{ID: "sub-1"}}, nil
}

func (stubSubscriptionCleanup) DeleteSubscription(_ context.Context, input pluginsdk.SubscriptionDeleteInput) (pluginsdk.SubscriptionDeleteResult, error) {
	return pluginsdk.SubscriptionDeleteResult{ID: input.ID, Deleted: true}, nil
}

func TestCleanupHostServicesRequireSeparatePermissions(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background(), mediaDelete: stubMediaDelete{}, subscriptionCleanup: stubSubscriptionCleanup{}})
	var reply JSONReply
	del := MediaDeleteRequest{Input: pluginsdk.MediaDeleteInput{FileRefs: []string{"file-1"}}}
	if err := server.DeleteMediaFiles(del, &reply); err == nil {
		t.Fatal("media.library.delete 未声明时不应删除")
	}
	if err := server.ListSubscriptions(SubscriptionListRequest{}, &reply); err == nil {
		t.Fatal("subscriptions.read 未声明时不应列出")
	}
	server.live().permissions.Host = []string{"media.library.delete", "subscriptions.read"}
	if err := server.DeleteMediaFiles(del, &reply); err != nil {
		t.Fatalf("DeleteMediaFiles: %v", err)
	}
	if err := server.ListSubscriptions(SubscriptionListRequest{}, &reply); err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if err := server.DeleteSubscription(SubscriptionDeleteRequest{Input: pluginsdk.SubscriptionDeleteInput{ID: "sub-1"}}, &reply); err == nil {
		t.Fatal("subscriptions.delete 未声明时不应删除订阅")
	}
	server.live().permissions.Host = []string{"subscriptions.delete"}
	withFiles := SubscriptionDeleteRequest{Input: pluginsdk.SubscriptionDeleteInput{ID: "sub-1", DeleteFiles: true}}
	if err := server.DeleteSubscription(withFiles, &reply); err == nil {
		t.Fatal("DeleteFiles 未持有 media.library.delete 时不应删除订阅文件")
	}
	server.live().permissions.Host = []string{"subscriptions.delete", "media.library.delete"}
	if err := server.DeleteSubscription(withFiles, &reply); err != nil {
		t.Fatalf("DeleteSubscription(DeleteFiles): %v", err)
	}
	if err := server.DeleteSubscription(SubscriptionDeleteRequest{Input: pluginsdk.SubscriptionDeleteInput{ID: "sub-1"}}, &reply); err != nil {
		t.Fatalf("DeleteSubscription: %v", err)
	}
}

func TestCleanupCapabilitiesOpenAndAttachHostChannel(t *testing.T) {
	if !needsHostServices(pluginsdk.Instance{MediaDelete: stubMediaDelete{}}, nil) || !needsHostServices(pluginsdk.Instance{SubscriptionCleanup: stubSubscriptionCleanup{}}, nil) {
		t.Fatal("清理能力未触发宿主回调通道")
	}
	inst, err := (&rpcServer{}).assembleInstance(InstancePayload{ID: "cleanup", ConfigJSON: []byte("{}"), HostServicesBrokerID: 1}, &hostServicesClient{})
	if err != nil {
		t.Fatal(err)
	}
	if inst.MediaDelete == nil || inst.SubscriptionCleanup == nil {
		t.Fatalf("assembleInstance 未挂上清理能力: %+v", inst)
	}
}
