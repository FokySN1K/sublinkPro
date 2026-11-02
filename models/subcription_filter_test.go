package models

import "testing"

func TestApplyFiltersAndRequiresAllGroups(t *testing.T) {
	nodes := filterTestNodes()
	sub := Subcription{
		CountryWhitelist:  "US",
		ProtocolWhitelist: "vmess",
	}

	assertFilteredNodeNames(t, sub.ApplyFilters(nodes), []string{"us-vmess"})
}

func TestApplyFiltersWhitelistValuesAreAlternatives(t *testing.T) {
	nodes := filterTestNodes()
	sub := Subcription{
		CountryWhitelist: "US,JP",
	}

	// Multiple values inside a single whitelist are OR-ed together.
	assertFilteredNodeNames(t, sub.ApplyFilters(nodes), []string{"us-vmess", "us-ss", "jp-vmess"})
}

func TestApplyFiltersBlacklistIsAbsolute(t *testing.T) {
	nodes := filterTestNodes()
	sub := Subcription{
		CountryWhitelist:  "US",
		ProtocolWhitelist: "vmess",
		CountryBlacklist:  "US",
	}

	// The whitelists would keep us-vmess, but a blacklist hit always excludes.
	assertFilteredNodeNames(t, sub.ApplyFilters(nodes), nil)
}

func TestApplyFiltersThresholdGroupMustMatch(t *testing.T) {
	nodes := []Node{
		{Name: "us-slow", LinkCountry: "US", Protocol: "vmess", DelayTime: 500},
		{Name: "us-fast", LinkCountry: "US", Protocol: "vmess", DelayTime: 100},
	}
	sub := Subcription{
		CountryWhitelist: "US",
		DelayTime:        200,
	}

	// The delay group is a regular AND group: a configured threshold is mandatory.
	assertFilteredNodeNames(t, sub.ApplyFilters(nodes), []string{"us-fast"})
}

func filterTestNodes() []Node {
	return []Node{
		{Name: "us-vmess", LinkCountry: "US", Protocol: "vmess", DelayTime: 100},
		{Name: "us-ss", LinkCountry: "US", Protocol: "ss", DelayTime: 100},
		{Name: "jp-vmess", LinkCountry: "JP", Protocol: "vmess", DelayTime: 100},
	}
}

func assertFilteredNodeNames(t *testing.T, got []Node, want []string) {
	t.Helper()

	gotNames := nodeNames(got)
	if len(gotNames) != len(want) {
		t.Fatalf("filtered nodes = %v, want %v", gotNames, want)
	}
	for i := range want {
		if gotNames[i] != want[i] {
			t.Fatalf("filtered nodes = %v, want %v", gotNames, want)
		}
	}
}
