package emit

import "testing"

func idxFixture(t *testing.T) *TypeIndex {
	t.Helper()
	idx, err := BuildTypeIndex(map[string]map[string]any{
		"ucp.json": {
			"title": "UCP Metadata",
			"$defs": map[string]any{"entity": map[string]any{"type": "object"}},
		},
		"shopping/checkout.json": {
			"title": "Checkout", "type": "object", "properties": map[string]any{},
		},
		"shopping/types/line_item.json": {
			"title": "Line Item", "type": "object", "properties": map[string]any{},
		},
		"capability.json": {
			"title": "Capability",
			"$defs": map[string]any{"base": map[string]any{"type": "object"}},
		},
	}, "github.com/chaz8081/ucp-go")
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestResolveRef(t *testing.T) {
	idx := idxFixture(t)
	cases := []struct{ from, ref, wantName, wantPkg string }{
		{"shopping/checkout.json", "types/line_item.json", "LineItem", "types"},
		{"shopping/checkout.json", "../capability.json#/$defs/base", "CapabilityBase", "ucp"},
		{"ucp.json", "#/$defs/entity", "UCPEntity", "ucp"},
		{"shopping/types/line_item.json", "../checkout.json", "Checkout", "shopping"},
	}
	for _, c := range cases {
		got, err := ResolveRef(idx, c.from, c.ref)
		if err != nil {
			t.Errorf("ResolveRef(%q, %q): %v", c.from, c.ref, err)
			continue
		}
		if got.Name != c.wantName || got.Package != c.wantPkg {
			t.Errorf("ResolveRef(%q, %q) = %s.%s, want %s.%s", c.from, c.ref, got.Package, got.Name, c.wantPkg, c.wantName)
		}
	}
}

func TestResolveRefUnknownTarget(t *testing.T) {
	idx := idxFixture(t)
	if _, err := ResolveRef(idx, "ucp.json", "nope.json"); err == nil {
		t.Error("unknown ref target must error, not silently degrade to any")
	}
	if _, err := ResolveRef(idx, "ucp.json", "#/$defs/missing"); err == nil {
		t.Error("unknown local def must error")
	}
	if _, err := ResolveRef(idx, "ucp.json", "#/$defs/entity/properties/id"); err == nil {
		t.Error("pointer inside a $def has no emitted type and must error")
	}
}

func TestResolveRefEntityInliningFallback(t *testing.T) {
	// Inlining ucp.json#/$defs/entity copies refs the entity wrote relative
	// to ucp.json, so they dangle in the destination document. Corpus-wide
	// this is exactly "#/$defs/version" in three files.
	idx, err := BuildTypeIndex(map[string]map[string]any{
		"ucp.json": {
			"title": "UCP Metadata",
			"$defs": map[string]any{"version": map[string]any{"type": "string"}},
		},
		"capability.json": {
			"title": "Capability",
			"$defs": map[string]any{"base": map[string]any{"type": "object"}},
		},
	}, "m")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRef(idx, "capability.json", "#/$defs/version")
	if err != nil {
		t.Fatalf("dangling local ref should fall back to ucp.json: %v", err)
	}
	if got.Name != "UCPVersion" {
		t.Errorf("resolved to %q, want UCPVersion", got.Name)
	}
	// The fallback must not mask a genuinely unknown name.
	if _, err := ResolveRef(idx, "capability.json", "#/$defs/nonexistent"); err == nil {
		t.Error("a name absent from both documents must still error")
	}
}
