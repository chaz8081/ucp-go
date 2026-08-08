package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPipeline(t *testing.T) {
	out := t.TempDir()
	manifest, err := run("preprocess/testdata/schemas", out, "release/test@deadbeef")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "test", "link.go")); err != nil {
		t.Fatalf("expected generated file: %v", err)
	}
	entry, ok := manifest.Schemas["test/link.json"]
	if !ok || entry.Type != "Link" {
		t.Fatalf("manifest missing test/link.json -> Link; got %+v", manifest.Schemas)
	}
	// File must be relative to outDir (portable across regeneration into a
	// scratch dir and diffing against a committed manifest), not an
	// absolute path baked with outDir's location.
	if entry.File != "test/link.go" {
		t.Fatalf("manifest File = %q, want %q (relative to outDir)", entry.File, "test/link.go")
	}
	raw, err := os.ReadFile(filepath.Join(out, "MANIFEST.json"))
	if err != nil {
		t.Fatalf("manifest file: %v", err)
	}
	var reread Manifest
	if err := json.Unmarshal(raw, &reread); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
}

// TestRunErrorNotDoublePrefixed guards against EmitFile prefixing its own
// errors with relPath and then run() wrapping the result with the same
// relPath again (e.g. "bad.json: bad.json: ..."). run() is the single
// prefixer; EmitFile's errors must be prefix-free.
func TestRunErrorNotDoublePrefixed(t *testing.T) {
	schemaDir := t.TempDir()
	// A schema with a title but a non-object top-level type triggers one
	// of EmitFile's own errors.
	bad := `{"title": "Bad", "type": "string", "pattern": "^[a-z]+$"}`
	if err := os.WriteFile(filepath.Join(schemaDir, "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := run(schemaDir, t.TempDir(), "release/test@deadbeef")
	if err == nil {
		t.Fatalf("run: expected error for non-object top-level schema, got nil")
	}
	if got := strings.Count(err.Error(), "bad.json"); got != 1 {
		t.Fatalf("run error mentions %q %d times, want exactly 1 (no doubled prefix): %q", "bad.json", got, err.Error())
	}
}
