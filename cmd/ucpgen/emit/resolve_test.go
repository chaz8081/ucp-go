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

func TestResolveRefDoesNotRescueDanglingLocalRefs(t *testing.T) {
	// python-sdk#72: inlining ucp.json#/$defs/entity used to copy refs the
	// entity had written relative to ucp.json, leaving 24 of them dangling
	// in capability.json, payment_handler.json and service.json. We carried
	// a narrow fallback that resolved those against ucp.json.
	//
	// python-sdk d650f0b (PR #79) resolves the entity's own local refs
	// before inlining it, so the corpus no longer contains a single
	// unresolvable local ref and the fallback is gone. This pins that: a
	// local ref must resolve in its OWN document or fail. Silently reaching
	// into ucp.json would resolve a name the document never declared.
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
	if _, err := ResolveRef(idx, "capability.json", "#/$defs/version"); err == nil {
		t.Error("a local ref absent from its own document must error, not resolve against ucp.json")
	}
	// The same name still resolves in the document that actually declares it.
	got, err := ResolveRef(idx, "ucp.json", "#/$defs/version")
	if err != nil {
		t.Fatalf("ucp.json declares version: %v", err)
	}
	if got.Name != "UCPVersion" {
		t.Errorf("resolved to %q, want UCPVersion", got.Name)
	}
}
