package emit

import (
	"strings"
	"testing"
)

// TestDocCommentFallback covers the 44 exported types whose schemas carry
// no description. Godoc renders those bare, which reads as an oversight
// rather than as the spec gap it actually is. The fallback says where the
// meaning lives instead of inventing any.
func TestDocCommentFallback(t *testing.T) {
	schema := map[string]any{
		"title": "Line Item", "type": "object",
		"properties": map[string]any{"sku": map[string]any{"type": "string"}},
	}
	src, err := emitOne(t, "shopping/types/line_item.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	want := "// LineItem is generated from shopping/types/line_item.json.\n"
	if !strings.Contains(src, want) {
		t.Errorf("missing %q in:\n%s", want, src)
	}
}

// A schema that does describe itself keeps its own words; the fallback
// must not displace real documentation.
func TestDocCommentPrefersSchemaDescription(t *testing.T) {
	schema := map[string]any{
		"title": "Line Item", "type": "object",
		"description": "One purchasable line.",
		"properties":  map[string]any{"sku": map[string]any{"type": "string"}},
	}
	src, err := emitOne(t, "shopping/types/line_item.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if !strings.Contains(src, "// LineItem One purchasable line.") {
		t.Errorf("schema description was dropped:\n%s", src)
	}
	if strings.Contains(src, "is generated from") {
		t.Errorf("fallback fired despite a real description:\n%s", src)
	}
}

// Every exported type in the real corpus must end up documented — that is
// the whole point, and a single undocumented type would show in godoc.
func TestEveryExportedTypeIsDocumented(t *testing.T) {
	corpus := map[string]map[string]any{
		"a.json": {"title": "Alpha", "type": "object", "properties": map[string]any{}},
		"b.json": {"title": "Beta", "$defs": map[string]any{
			"inner": map[string]any{"type": "string"},
		}},
	}
	for rel := range corpus {
		src, err := emitFromCorpus(t, rel, corpus)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for _, line := range strings.Split(src, "\n") {
			if !strings.HasPrefix(line, "type ") {
				continue
			}
			name := strings.Fields(line)[1]
			if !strings.Contains(src, "// "+name+" ") {
				t.Errorf("%s: type %s has no doc comment:\n%s", rel, name, src)
			}
		}
	}
}
