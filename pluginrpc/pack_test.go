package pluginrpc

import (
	"errors"
	"reflect"
	"testing"

	"github.com/chenbstack/media-agent-plugin-sdk-go"
)

func TestPackPluginSetUsesLogicalPluginIDs(t *testing.T) {
	plugins := []pluginsdk.Plugin{
		{Manifest: pluginsdk.Manifest{ID: "site"}},
		{Manifest: pluginsdk.Manifest{ID: "family"}},
	}
	set, err := packPluginSet(plugins)
	if err != nil {
		t.Fatalf("packPluginSet: %v", err)
	}
	for _, id := range []string{"site", "family"} {
		entry, ok := set[PackPluginName(id)]
		if !ok {
			t.Fatalf("missing plugin set entry for %s", id)
		}
		serverPlugin, ok := entry.(*netRPCPlugin)
		if !ok || serverPlugin.impl.Manifest.ID != id {
			t.Fatalf("entry %s = %#v", id, entry)
		}
	}
	if _, exists := set[PluginName]; exists {
		t.Fatal("pack must not shadow the legacy single-plugin name")
	}
}

func TestPackPluginSetRejectsEmptyAndDuplicateIDs(t *testing.T) {
	if _, err := packPluginSet(nil); err == nil {
		t.Fatal("empty pack should fail")
	}
	if _, err := packPluginSet([]pluginsdk.Plugin{
		{Manifest: pluginsdk.Manifest{ID: "site"}},
		{Manifest: pluginsdk.Manifest{ID: "site"}},
	}); err == nil {
		t.Fatal("duplicate plugin id should fail")
	}
}

func TestProcessStartInfoNormalizesLegacySinglePluginRecords(t *testing.T) {
	legacy := ProcessStartInfo{PluginID: "site"}
	if got := legacy.EffectiveKind(); got != ProcessKindStandalone {
		t.Fatalf("EffectiveKind = %q", got)
	}
	if got := legacy.LogicalPluginIDs(); !reflect.DeepEqual(got, []string{"site"}) {
		t.Fatalf("LogicalPluginIDs = %v", got)
	}

	pack := ProcessStartInfo{Kind: ProcessKindPack, PackID: "official", PluginIDs: []string{"site", "family"}}
	ids := pack.LogicalPluginIDs()
	ids[0] = "mutated"
	if got := pack.LogicalPluginIDs(); !reflect.DeepEqual(got, []string{"site", "family"}) {
		t.Fatalf("pack LogicalPluginIDs = %v", got)
	}
}

func TestPackClientEnumeratesAndDispensesWithoutStartingAnotherProcess(t *testing.T) {
	protocol := &fakeClientProtocol{values: map[string]any{
		PackPluginName("site"):   &Client{},
		PackPluginName("family"): &Client{},
	}}
	pack := &PackClient{
		protocol: protocol,
		ids:      []string{"site", "family"},
		configs: map[string]PackPluginConfig{
			"site":   {Manifest: pluginsdk.Manifest{ID: "site", Name: "Site"}, ScopeType: "plugin", ScopeID: "global"},
			"family": {Manifest: pluginsdk.Manifest{ID: "family", Name: "Family"}, ScopeType: "plugin", ScopeID: "global"},
		},
		clients: map[string]*Client{},
	}

	ids := pack.PluginIDs()
	ids[0] = "mutated"
	if got := pack.PluginIDs(); !reflect.DeepEqual(got, []string{"site", "family"}) {
		t.Fatalf("PluginIDs = %v", got)
	}
	if err := pack.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	site, err := pack.Dispense("site")
	if err != nil {
		t.Fatalf("Dispense(site): %v", err)
	}
	if site.manifest.ID != "site" || site.scopeType != "plugin" {
		t.Fatalf("site binding = %#v", site)
	}
	again, err := pack.Dispense("site")
	if err != nil || again != site {
		t.Fatalf("cached Dispense = %p, %v", again, err)
	}
	if got := protocol.calls; !reflect.DeepEqual(got, []string{PackPluginName("site")}) {
		t.Fatalf("Dispense calls = %v", got)
	}
	if _, err := pack.Dispense("unknown"); err == nil {
		t.Fatal("unknown logical plugin should fail")
	}
}

type fakeClientProtocol struct {
	values map[string]any
	calls  []string
	closed bool
}

func (f *fakeClientProtocol) Dispense(name string) (interface{}, error) {
	f.calls = append(f.calls, name)
	value, ok := f.values[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return value, nil
}

func (f *fakeClientProtocol) Ping() error { return nil }

func (f *fakeClientProtocol) Close() error {
	f.closed = true
	return nil
}

var _ interface {
	Close() error
	Dispense(string) (interface{}, error)
	Ping() error
} = (*fakeClientProtocol)(nil)
