package mihomo

import (
	"testing"

	"sublink/node/protocol"

	"github.com/metacubex/mihomo/adapter"
	"gopkg.in/yaml.v3"
)

const testNodeUniqueKey = "0123456789abcdef0123456789abcdef"

// TestMihomoIgnoresNodeUniqueKeyInURI proves that the extra query parameter
// added to vmess links does not break mihomo's link parser.
func TestMihomoIgnoresNodeUniqueKeyInURI(t *testing.T) {
	link := protocol.EncodeVmessURL(protocol.Vmess{
		Add:  "example.com",
		Port: 443,
		Id:   "11111111-1111-1111-1111-111111111111",
		Ps:   "HK-01",
		Net:  "ws",
	})

	proxy, err := GetMihomoAdapter(protocol.AppendNodeUniqueKey(link, testNodeUniqueKey))
	if err != nil {
		t.Fatalf("mihomo rejected a link with the stable node key: %v", err)
	}
	if proxy == nil {
		t.Fatalf("expected a mihomo proxy adapter")
	}
	if proxy.Name() != "HK-01" {
		t.Fatalf("expected adapter name HK-01, got %q", proxy.Name())
	}
}

// TestMihomoIgnoresNodeUniqueKeyInClashConfig proves that mihomo accepts the
// extra proxy key emitted into Clash/Mihomo proxy entries.
func TestMihomoIgnoresNodeUniqueKeyInClashConfig(t *testing.T) {
	link := protocol.EncodeVmessURL(protocol.Vmess{
		Add:  "example.com",
		Port: 443,
		Id:   "11111111-1111-1111-1111-111111111111",
		Ps:   "HK-01",
		Net:  "ws",
	})

	proxy, err := protocol.LinkToProxy(protocol.Urls{Url: link, NodeUniqueKey: testNodeUniqueKey}, protocol.OutputConfig{})
	if err != nil {
		t.Fatalf("LinkToProxy failed: %v", err)
	}

	encoded, err := yaml.Marshal(proxy)
	if err != nil {
		t.Fatalf("marshal proxy failed: %v", err)
	}
	var proxyMap map[string]any
	if err := yaml.Unmarshal(encoded, &proxyMap); err != nil {
		t.Fatalf("unmarshal proxy map failed: %v", err)
	}
	if proxyMap[protocol.NodeUniqueKeyField] != testNodeUniqueKey {
		t.Fatalf("expected proxy map to carry the stable node key, got %v", proxyMap[protocol.NodeUniqueKeyField])
	}

	parsed, err := adapter.ParseProxy(proxyMap)
	if err != nil {
		t.Fatalf("mihomo rejected a proxy with the stable node key: %v", err)
	}
	if parsed == nil {
		t.Fatalf("expected a mihomo proxy adapter")
	}
	if parsed.Name() != "HK-01" {
		t.Fatalf("expected adapter name HK-01, got %q", parsed.Name())
	}
}
