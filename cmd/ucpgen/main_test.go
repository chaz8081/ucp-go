package main

import (
	"bytes"
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
	// patternProperties changes the shape of the object it appears on, so
	// the emitter refuses it — one of EmitFile's own errors.
	bad := `{"title": "Bad", "type": "object", "properties": {"cfg": {"type": "object", "patternProperties": {"^x": {"type": "string"}}}}}`
	if err := os.WriteFile(filepath.Join(schemaDir, "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := run(schemaDir, t.TempDir(), "release/test@deadbeef")
	if err == nil {
		t.Fatalf("run: expected error for patternProperties, got nil")
	}
	if got := strings.Count(err.Error(), "bad.json"); got != 1 {
		t.Fatalf("run error mentions %q %d times, want exactly 1 (no doubled prefix): %q", "bad.json", got, err.Error())
	}
}

// TestRunPreprocessWritesDeterministicSet covers the first of the two
// stages generate.sh runs: normalize a raw schema set and write it as
// canonical JSON, byte-identically across runs — the property the goldens
// comparison rests on.
//
// The two stages are exercised on different fixtures rather than chained,
// because the emitter cannot yet consume a fully normalized set: real
// (and synthetic) ucp.json documents have no top-level "type", which the
// emitter deliberately rejects as an unsupported shape. Emitting the whole
// normalized spec is Phase 3 work; the emit stage is covered separately by
// TestRunPipeline on a flat fixture.
func TestRunPreprocessWritesDeterministicSet(t *testing.T) {
	normalized := t.TempDir()
	if err := runPreprocess("preprocess/testdata/synth", normalized); err != nil {
		t.Fatalf("runPreprocess: %v", err)
	}
	if _, err := os.Stat(filepath.Join(normalized, "a.json")); err != nil {
		t.Fatalf("normalized schema not written: %v", err)
	}

	// Byte-identical on a second run into a fresh directory.
	again := t.TempDir()
	if err := runPreprocess("preprocess/testdata/synth", again); err != nil {
		t.Fatalf("runPreprocess (second): %v", err)
	}
	for _, name := range []string{"a.json", "ucp.json", filepath.Join("sub", "b.json")} {
		first, err := os.ReadFile(filepath.Join(normalized, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		second, err := os.ReadFile(filepath.Join(again, name))
		if err != nil {
			t.Fatalf("read %s (second): %v", name, err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("%s differs between runs:\n%s\n---\n%s", name, first, second)
		}
	}

	// Normalization actually ran: the dotted $defs key was renamed and the
	// request variant was generated into the written set.
	raw, err := os.ReadFile(filepath.Join(normalized, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("dev.ucp.ext.thing")) {
		t.Errorf("dotted def not renamed in written output:\n%s", raw)
	}
}

func TestRunCanonicalizeIsTransformFree(t *testing.T) {
	// canonicalize must reformat without normalizing: the synth fixture's
	// dotted $defs keys survive untouched.
	out := t.TempDir()
	if err := runCanonicalize("preprocess/testdata/synth", out); err != nil {
		t.Fatalf("runCanonicalize: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("dev.ucp.ext.thing")) {
		t.Errorf("canonicalize must not rename dotted defs:\n%s", raw)
	}
}
