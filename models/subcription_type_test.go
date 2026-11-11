package models

import "testing"

func TestNormalizeSubcriptionType(t *testing.T) {
	cases := map[string]string{
		"":               SubcriptionTypeCollection,
		"collection":     SubcriptionTypeCollection,
		"  COLLECTION  ": SubcriptionTypeCollection,
		"proxy":          SubcriptionTypeProxy,
		" PROXY ":        SubcriptionTypeProxy,
		"unknown":        SubcriptionTypeCollection,
	}

	for input, want := range cases {
		if got := NormalizeSubcriptionType(input); got != want {
			t.Fatalf("NormalizeSubcriptionType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSubcriptionIsProxy(t *testing.T) {
	if (&Subcription{Type: SubcriptionTypeProxy}).IsProxy() != true {
		t.Fatal("proxy subscription should be detected")
	}
	if (&Subcription{}).IsProxy() != false {
		t.Fatal("legacy subscription without type should not be proxy")
	}
	if (&Subcription{Type: SubcriptionTypeCollection}).IsProxy() != false {
		t.Fatal("collection subscription should not be proxy")
	}
}

func TestFilterSubcriptionsByType(t *testing.T) {
	subs := []Subcription{
		{ID: 1, Name: "legacy"},
		{ID: 2, Name: "collection", Type: SubcriptionTypeCollection},
		{ID: 3, Name: "proxy", Type: SubcriptionTypeProxy},
	}

	if got := filterSubcriptionsByType(subs, ""); len(got) != 3 {
		t.Fatalf("empty filter returned %d subscriptions, want 3", len(got))
	}
	if got := filterSubcriptionsByType(subs, SubcriptionTypeCollection); len(got) != 2 {
		t.Fatalf("collection filter returned %d subscriptions, want 2", len(got))
	}
	if got := filterSubcriptionsByType(subs, SubcriptionTypeProxy); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("proxy filter returned unexpected result: %+v", got)
	}
}
