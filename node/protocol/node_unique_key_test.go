package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"sublink/utils"

	"gopkg.in/yaml.v3"
)

const testNodeUniqueKey = "0123456789abcdef0123456789abcdef"

// legacyNodeUniqueKeyField is the pre-rename key name. It must never be
// emitted anymore: SNKP reads only the new name.
const legacyNodeUniqueKeyField = "sublink_unique_key_node"

func TestAppendNodeUniqueKeyVMessJSON(t *testing.T) {
	link := EncodeVmessURL(Vmess{
		Add:  "example.com",
		Port: 443,
		Id:   "11111111-1111-1111-1111-111111111111",
		Ps:   "HK-01",
		Net:  "ws",
	})

	rendered := AppendNodeUniqueKey(link, testNodeUniqueKey)
	if !strings.HasPrefix(rendered, "vmess://") {
		t.Fatalf("expected vmess link, got %q", rendered)
	}

	payload := utils.Base64Decode(strings.TrimPrefix(rendered, "vmess://"))
	var document map[string]any
	if err := json.Unmarshal([]byte(payload), &document); err != nil {
		t.Fatalf("vmess payload is not valid JSON: %v (%s)", err, payload)
	}
	if document[NodeUniqueKeyField] != testNodeUniqueKey {
		t.Fatalf("expected unique key %q, got %v", testNodeUniqueKey, document[NodeUniqueKeyField])
	}
	if _, exists := document[legacyNodeUniqueKeyField]; exists {
		t.Fatalf("legacy key %q must not be emitted", legacyNodeUniqueKeyField)
	}
	if document["ps"] != "HK-01" {
		t.Fatalf("expected node name to survive, got %v", document["ps"])
	}
	if document["add"] != "example.com" {
		t.Fatalf("expected server to survive, got %v", document["add"])
	}
}

func TestAppendNodeUniqueKeyURIQuery(t *testing.T) {
	link := "vless://11111111-1111-1111-1111-111111111111@example.com:443?type=ws&security=tls&path=%2Fws#HK-01"

	rendered := AppendNodeUniqueKey(link, testNodeUniqueKey)
	if !strings.Contains(rendered, NodeUniqueKeyField+"="+testNodeUniqueKey) {
		t.Fatalf("expected unique key in query, got %q", rendered)
	}
	if strings.Contains(rendered, legacyNodeUniqueKeyField) {
		t.Fatalf("legacy key %q must not be emitted, got %q", legacyNodeUniqueKeyField, rendered)
	}
	if !strings.HasSuffix(rendered, "#HK-01") {
		t.Fatalf("expected fragment to survive, got %q", rendered)
	}

	decoded, _, err := DecodeProtocolObject(rendered)
	if err != nil {
		t.Fatalf("rendered link must stay decodable: %v", err)
	}
	if decoded == nil {
		t.Fatalf("expected decoded protocol object")
	}
	identity, err := ExtractLinkIdentity(rendered)
	if err != nil {
		t.Fatalf("rendered link identity must resolve: %v", err)
	}
	if identity.Name != "HK-01" {
		t.Fatalf("expected name HK-01, got %q", identity.Name)
	}
}

func TestAppendNodeUniqueKeyURIWithoutQuery(t *testing.T) {
	link := "trojan://secret@example.com:443#HK-01"
	rendered := AppendNodeUniqueKey(link, testNodeUniqueKey)
	want := "trojan://secret@example.com:443?" + NodeUniqueKeyField + "=" + testNodeUniqueKey + "#HK-01"
	if rendered != want {
		t.Fatalf("expected %q, got %q", want, rendered)
	}
}

func TestAppendNodeUniqueKeyReplacesExistingValue(t *testing.T) {
	link := "trojan://secret@example.com:443?" + NodeUniqueKeyField + "=old&sni=example.com#HK-01"
	rendered := AppendNodeUniqueKey(link, testNodeUniqueKey)
	if strings.Contains(rendered, "old") {
		t.Fatalf("expected previous key to be replaced, got %q", rendered)
	}
	if strings.Count(rendered, NodeUniqueKeyField+"=") != 1 {
		t.Fatalf("expected exactly one key parameter, got %q", rendered)
	}
	if !strings.Contains(rendered, "sni=example.com") {
		t.Fatalf("expected other parameters to survive, got %q", rendered)
	}
}

func TestAppendNodeUniqueKeyLeavesSSRAndEmptyInputUntouched(t *testing.T) {
	ssr := "ssr://c2VydmVyOjQ0MzpvcmlnaW46YWVzLTI1Ni1jZmI6cGxhaW46Y0dGemMzZHZjbVE9Lz9yZW1hcmtzPTVyMk01TGl2NXBhSA"
	if rendered := AppendNodeUniqueKey(ssr, testNodeUniqueKey); rendered != ssr {
		t.Fatalf("expected ssr link to stay untouched, got %q", rendered)
	}
	if rendered := AppendNodeUniqueKey("", testNodeUniqueKey); rendered != "" {
		t.Fatalf("expected empty link to stay empty, got %q", rendered)
	}
	if rendered := AppendNodeUniqueKey("trojan://secret@example.com:443#HK-01", ""); rendered != "trojan://secret@example.com:443#HK-01" {
		t.Fatalf("expected empty key to be skipped, got %q", rendered)
	}
	raw := "unknown-protocol://example.com/raw#unknown"
	if rendered := AppendNodeUniqueKey(raw, testNodeUniqueKey); rendered != raw {
		t.Fatalf("expected unregistered raw link to stay untouched, got %q", rendered)
	}
}

func TestLinkToProxyCarriesNodeUniqueKey(t *testing.T) {
	link := "vless://11111111-1111-1111-1111-111111111111@example.com:443?type=ws&security=tls#HK-01"

	proxy, err := LinkToProxy(Urls{Url: link, NodeUniqueKey: testNodeUniqueKey}, OutputConfig{})
	if err != nil {
		t.Fatalf("LinkToProxy failed: %v", err)
	}
	if proxy.Sublinkpro_unique_key != testNodeUniqueKey {
		t.Fatalf("expected proxy key %q, got %q", testNodeUniqueKey, proxy.Sublinkpro_unique_key)
	}

	encoded, err := yaml.Marshal(proxy)
	if err != nil {
		t.Fatalf("marshal proxy failed: %v", err)
	}
	if !strings.Contains(string(encoded), NodeUniqueKeyField+": "+testNodeUniqueKey) {
		t.Fatalf("expected key in proxy YAML, got:\n%s", encoded)
	}
	if strings.Contains(string(encoded), legacyNodeUniqueKeyField) {
		t.Fatalf("legacy key %q must not be emitted, got:\n%s", legacyNodeUniqueKeyField, encoded)
	}

	// 稳定键不参与内容哈希，导入导出往返不会改变节点去重结果。
	withoutKey := proxy
	withoutKey.Sublinkpro_unique_key = ""
	if GenerateProxyContentHash(proxy) != GenerateProxyContentHash(withoutKey) {
		t.Fatalf("node unique key must not affect the content hash")
	}
}
