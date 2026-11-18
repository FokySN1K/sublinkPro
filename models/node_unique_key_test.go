package models

import (
	"strings"
	"testing"
)

func TestBuildNodeUniqueKeyPrefersContentHash(t *testing.T) {
	contentHash := strings.Repeat("ab", 32)
	linkHash := strings.Repeat("cd", 32)

	key := BuildNodeUniqueKey(contentHash, linkHash, "vless://example")
	if key != contentHash[:32] {
		t.Fatalf("expected the content hash prefix, got %q", key)
	}
}

func TestBuildNodeUniqueKeyFallsBackToLinkHash(t *testing.T) {
	linkHash := strings.Repeat("CD", 32)

	key := BuildNodeUniqueKey("", linkHash, "vless://example")
	if key != strings.ToLower(linkHash)[:32] {
		t.Fatalf("expected the link hash prefix, got %q", key)
	}
}

func TestBuildNodeUniqueKeyHashesLinkWhenHashesMissing(t *testing.T) {
	link := "vless://11111111-1111-1111-1111-111111111111@example.com:443#HK-01"

	first := BuildNodeUniqueKey("", "", link)
	second := BuildNodeUniqueKey("", "", link)
	if first == "" || len(first) != nodeUniqueKeyLength {
		t.Fatalf("expected a %d character key, got %q", nodeUniqueKeyLength, first)
	}
	if first != second {
		t.Fatalf("expected a stable key, got %q and %q", first, second)
	}
	if strings.Contains(first, link) {
		t.Fatalf("expected a hash rather than the raw link")
	}
	if key := BuildNodeUniqueKey("", "", ""); key != "" {
		t.Fatalf("expected an empty key for an empty link, got %q", key)
	}
}

func TestBuildNodeUniqueKeyRejectsNonHexHashes(t *testing.T) {
	key := BuildNodeUniqueKey(strings.Repeat("zz", 32), "", "vless://example")
	if key == strings.Repeat("zz", 16) {
		t.Fatalf("expected a non-hex hash to be rejected")
	}
	if len(key) != nodeUniqueKeyLength {
		t.Fatalf("expected the link hash fallback, got %q", key)
	}
}

// TestSublinkNodeUniqueKeySurvivesRenaming is the regression guard for name
// processing that renames every node in a pool to the same value.
func TestSublinkNodeUniqueKeySurvivesRenaming(t *testing.T) {
	contentHash := strings.Repeat("ef", 32)
	original := Node{Link: "vless://a", Name: "HK-01", LinkName: "HK-01", ContentHash: contentHash}
	renamed := Node{Link: "vless://a", Name: "1", LinkName: "1", ContentHash: contentHash}

	if original.SublinkNodeUniqueKey() != renamed.SublinkNodeUniqueKey() {
		t.Fatalf("stable node key must not depend on the node name")
	}
	if renamed.SublinkNodeUniqueKey() != contentHash[:32] {
		t.Fatalf("unexpected stable node key %q", renamed.SublinkNodeUniqueKey())
	}
}
