package models

import (
	"strings"
	"testing"
)

// Tags are internal metadata: they reach the name a client sees only through
// the $Tags / $TagGroup(group) placeholders of the subscription rename rule.
// Everything else must stay stable when a tag is added, renamed or removed.
func TestNodeNameIgnoresTagsUnlessTheRuleAsks(t *testing.T) {
	rule := "$LinkName - $Index"
	node := Node{Name: "备注", LinkName: "上游节点", NameMode: NodeNameModeLink}

	plain := BuildNodeExportName(node, node.LinkName, rule, "ss", 1, 0)
	if plain != "上游节点 - 1" {
		t.Fatalf("unexpected base name %q", plain)
	}

	for _, tags := range []string{"youtube", "youtube,youtube-ads", ""} {
		node.Tags = tags
		if got := BuildNodeExportName(node, node.LinkName, rule, "ss", 1, 0); got != plain {
			t.Fatalf("tags %q changed the name: %q -> %q", tags, plain, got)
		}
		// An empty rule falls back to the node's own name, which has no tags either.
		if got := BuildNodeExportName(node, node.LinkName, "", "ss", 1, 0); got != "上游节点" {
			t.Fatalf("tags %q changed the fallback name: %q", tags, got)
		}
	}

	node.Tags = "youtube,youtube-ads"
	withTags := BuildNodeExportName(node, node.LinkName, "$LinkName [$Tags]", "ss", 1, 0)
	if withTags != "上游节点 [youtube|youtube-ads]" {
		t.Fatalf("explicit $Tags should render the tags, got %q", withTags)
	}

	node.Tags = "youtube"
	if got := BuildNodeExportName(node, node.LinkName, "$LinkName [$Tags]", "ss", 1, 0); strings.Contains(got, "youtube-ads") {
		t.Fatalf("a removed tag must disappear from $Tags, got %q", got)
	}
}
