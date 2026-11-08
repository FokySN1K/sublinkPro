package protocol

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// realityGuardVLESSLink 是只有 short-id、缺少 public-key 的 VLESS 链接。
const realityGuardVLESSLink = "vless://b831381d-6324-4d53-ad4f-8cda48b30811@203.0.113.10:443" +
	"?encryption=none&security=reality&type=tcp&sni=example.com&sid=9ecdbcce4f70a126#sid-only"

// realityGuardTrojanLink 是只有 short-id、缺少 public-key 的 Trojan 链接。
const realityGuardTrojanLink = "trojan://secret@203.0.113.10:443?security=reality&sni=example.com&sid=9ecdbcce4f70a126#sid-only"

// mihomo 会把缺少 public-key 的 reality-opts 视为非法并拒绝整个配置，
// 因此 public-key 缺失时必须整组省略。
func TestVLESSShortIDWithoutPublicKeySkipsRealityOpts(t *testing.T) {
	proxy, err := buildVLESSProxy(Urls{Url: realityGuardVLESSLink}, OutputConfig{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if proxy.Reality_opts != nil {
		t.Fatalf("reality-opts without public-key: %v", proxy.Reality_opts)
	}
	encoded, err := yaml.Marshal(proxy)
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if strings.Contains(string(encoded), "reality-opts") {
		t.Fatalf("reality-opts leaked into:\n%s", encoded)
	}
}

func TestTrojanShortIDWithoutPublicKeySkipsRealityOpts(t *testing.T) {
	proxy, err := buildTrojanProxy(Urls{Url: realityGuardTrojanLink}, OutputConfig{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if proxy.Reality_opts != nil {
		t.Fatalf("reality-opts without public-key: %v", proxy.Reality_opts)
	}
	encoded, err := yaml.Marshal(proxy)
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if strings.Contains(string(encoded), "reality-opts") {
		t.Fatalf("reality-opts leaked into:\n%s", encoded)
	}
}

// 回写链接时同样要求 public-key，否则会生成 security=reality 但没有密钥的坏链接。
func TestVLESSProxyWithoutPublicKeyFallsBackToTLS(t *testing.T) {
	proxy := Proxy{
		Name:         "guard",
		Type:         "vless",
		Server:       "203.0.113.10",
		Port:         443,
		Uuid:         "b831381d-6324-4d53-ad4f-8cda48b30811",
		Tls:          true,
		Reality_opts: map[string]any{"short-id": "9ecdbcce4f70a126"},
	}
	vless := ConvertProxyToVless(proxy)
	if vless.Query.Security != "tls" {
		t.Fatalf("security = %q", vless.Query.Security)
	}
	if vless.Query.Pbk != "" || vless.Query.Sid != "" {
		t.Fatalf("reality material kept: pbk=%q sid=%q", vless.Query.Pbk, vless.Query.Sid)
	}
}

func TestTrojanProxyWithoutPublicKeyDropsReality(t *testing.T) {
	proxy := Proxy{
		Name:         "guard",
		Type:         "trojan",
		Server:       "203.0.113.10",
		Port:         443,
		Password:     "secret",
		Reality_opts: map[string]any{"short-id": "9ecdbcce4f70a126"},
	}
	trojan := ConvertProxyToTrojan(proxy)
	if trojan.Query.Security != "" {
		t.Fatalf("security = %q", trojan.Query.Security)
	}
	if trojan.Query.Pbk != "" || trojan.Query.Sid != "" {
		t.Fatalf("reality material kept: pbk=%q sid=%q", trojan.Query.Pbk, trojan.Query.Sid)
	}
}
