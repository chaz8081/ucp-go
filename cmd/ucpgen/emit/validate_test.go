package emit

import (
	"strings"
	"testing"
)

func TestEmitValidate(t *testing.T) {
	src, err := EmitFile("shopping", "test/link.json", linkSchema, "release/test@deadbeef")
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	for _, want := range []string{
		"func (v *Link) Validate() error {",
		`utf8.RuneCountInString(v.URL) > 2048`,
		"pattern_Link_Title",
		"sync.OnceValue",
		"errors.New(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("emitted source missing %q\n---\n%s", want, src)
		}
	}
}

func TestEmitValidateRejectsNonRE2(t *testing.T) {
	schema := map[string]any{
		"title": "BadPattern",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "pattern": "(?=lookahead)x"},
		},
	}
	_, err := EmitFile("shopping", "test/badpattern.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for non-RE2 pattern, got nil")
	}
	if !strings.Contains(err.Error(), "RE2") {
		t.Errorf("error = %q, want mention of RE2", err.Error())
	}
}
