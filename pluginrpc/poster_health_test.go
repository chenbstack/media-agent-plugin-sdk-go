package pluginrpc

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
	providerfake "github.com/chenbstack/media-agent-plugin-sdk-go/providers/fake"
)

type posterHealthProvider struct {
	*providerfake.MediaServer
	healthCalls               int
	replaceAll, replaceImages bool
}

func (p *posterHealthProvider) MetadataItemsByID(_ context.Context, ids []string, health bool) ([]providers.MetadataItem, error) {
	if len(ids) != 1 || ids[0] != "i1" {
		return nil, errors.New("exact IDs changed")
	}
	status := ""
	if health {
		status = "valid"
	}
	return []providers.MetadataItem{{ExternalID: "i1", HasPoster: true, PosterStatus: status}}, nil
}

func (p *posterHealthProvider) MetadataItemsWithPosterHealth(_ context.Context, library string, start, limit int) ([]providers.MetadataItem, int, error) {
	p.healthCalls++
	if library != "lib1" || start != 2 || limit != 10 {
		return nil, 0, errors.New("query changed")
	}
	return []providers.MetadataItem{{ExternalID: "i1", HasPoster: true, PosterStatus: "broken"}}, 3, nil
}

func (p *posterHealthProvider) RefreshItemMetadataWithImages(_ context.Context, id string, all, images bool) error {
	if id != "i1" {
		return errors.New("id changed")
	}
	p.replaceAll, p.replaceImages = all, images
	return nil
}

func TestPosterHealthProviderRPCRoundTrip(t *testing.T) {
	p := &posterHealthProvider{MediaServer: providerfake.NewMediaServer()}
	c := newProviderTestClient(t, pluginsdk.Plugin{Manifest: pluginsdk.Manifest{ID: "emby"}, NewMediaServer: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.MediaServerProvider, error) {
		return p, nil
	}})
	adapter := c.MediaServer(pluginsdk.Instance{ID: "s1"}, nil)
	for _, health := range []bool{false, true} {
		items, err := adapter.(providers.MediaServerMetadataLookupProvider).MetadataItemsByID(context.Background(), []string{"i1"}, health)
		if err != nil || len(items) != 1 || items[0].ExternalID != "i1" || (items[0].PosterStatus == "valid") != health {
			t.Fatalf("exact lookup=%+v health=%v err=%v", items, health, err)
		}
	}
	items, total, err := adapter.(providers.MediaServerPosterHealthProvider).MetadataItemsWithPosterHealth(context.Background(), "lib1", 2, 10)
	if err != nil || total != 3 || len(items) != 1 || items[0].PosterStatus != "broken" || p.healthCalls != 1 {
		t.Fatalf("items=%+v total=%d calls=%d err=%v", items, total, p.healthCalls, err)
	}
	if err := adapter.(providers.MediaServerImageRefreshProvider).RefreshItemMetadataWithImages(context.Background(), "i1", false, true); err != nil || p.replaceAll || !p.replaceImages {
		t.Fatalf("all=%t images=%t err=%v", p.replaceAll, p.replaceImages, err)
	}
}

func TestPosterHealthRPCRejectsOldProvider(t *testing.T) {
	p := providerfake.NewMediaServer()
	c := newProviderTestClient(t, pluginsdk.Plugin{Manifest: pluginsdk.Manifest{ID: "emby"}, NewMediaServer: func(context.Context, pluginsdk.Instance, pluginsdk.SecretResolver) (providers.MediaServerProvider, error) {
		return p, nil
	}})
	adapter := c.MediaServer(pluginsdk.Instance{ID: "s1"}, nil)
	if _, err := adapter.(providers.MediaServerMetadataLookupProvider).MetadataItemsByID(context.Background(), []string{"i1"}, false); !errors.Is(err, providers.ErrMetadataRefreshUnsupported) {
		t.Fatalf("exact lookup: %v", err)
	}
	if _, _, err := adapter.(providers.MediaServerPosterHealthProvider).MetadataItemsWithPosterHealth(context.Background(), "", 0, 10); !errors.Is(err, providers.ErrMetadataRefreshUnsupported) {
		t.Fatalf("health: %v", err)
	}
	if err := adapter.(providers.MediaServerImageRefreshProvider).RefreshItemMetadataWithImages(context.Background(), "i1", false, true); !errors.Is(err, providers.ErrMetadataRefreshUnsupported) {
		t.Fatalf("image replacement: %v", err)
	}
	if _, _, err := adapter.(providers.MediaServerMetadataProvider).MetadataItems(context.Background(), "", 0, 10); err != nil {
		t.Fatalf("旧能力应继续可用: %v", err)
	}
}

type legacyPosterRPC struct{}

func (legacyPosterRPC) MediaServerMetadataItems(_ MediaServerMetadataItemsRequest, reply *JSONReply) error {
	return setJSONReply(reply, MediaServerMetadataItemsReply{Supported: true}, nil)
}
func (legacyPosterRPC) MediaServerRefreshItemMetadata(_ MediaServerRefreshMetadataRequest, reply *JSONReply) error {
	return setJSONReply(reply, MediaServerRefreshMetadataReply{Supported: true}, nil)
}

func TestPosterHealthRPCRequiresExplicitAcknowledgement(t *testing.T) {
	s := rpc.NewServer()
	if err := s.RegisterName("Plugin", legacyPosterRPC{}); err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	go s.ServeConn(b)
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	c := &Client{client: rpc.NewClient(a), manifest: pluginsdk.Manifest{ID: "emby"}}
	p := c.MediaServer(pluginsdk.Instance{ID: "s1"}, nil)
	if _, err := p.(providers.MediaServerMetadataLookupProvider).MetadataItemsByID(context.Background(), []string{"i1"}, false); !errors.Is(err, providers.ErrMetadataRefreshUnsupported) {
		t.Fatalf("legacy lookup must acknowledge exact filtering: %v", err)
	}
	if _, _, err := p.(providers.MediaServerPosterHealthProvider).MetadataItemsWithPosterHealth(context.Background(), "", 0, 10); !errors.Is(err, providers.ErrMetadataRefreshUnsupported) {
		t.Fatalf("忽略新字段的旧RPC必须返回不支持: %v", err)
	}
	if err := p.(providers.MediaServerImageRefreshProvider).RefreshItemMetadataWithImages(context.Background(), "i1", false, true); !errors.Is(err, providers.ErrMetadataRefreshUnsupported) {
		t.Fatalf("忽略新字段的旧RPC必须返回不支持: %v", err)
	}
}
