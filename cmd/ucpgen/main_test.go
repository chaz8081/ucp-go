package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	raw, err := os.ReadFile(filepath.Join(out, "MANIFEST.json"))
	if err != nil {
		t.Fatalf("manifest file: %v", err)
	}
	var reread Manifest
	if err := json.Unmarshal(raw, &reread); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
}
