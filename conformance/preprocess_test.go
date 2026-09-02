package conformance

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const goldenVersion = "2026-08-25"

// TestPreprocessMatchesGoldens runs the Go preprocessor over the real spec
// schemas and requires byte-identical canonical output against the goldens
// committed under goldens/<version>/, which were produced by the official
// python-sdk preprocessor (see scripts/make-goldens.sh).
//
// Both sides pass through the same canonical encoder, so any difference
// reported here is a difference in schema CONTENT, never in formatting.
func TestPreprocessMatchesGoldens(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	specSchemas := filepath.Join(repoRoot, ".ucp-spec", "source", "schemas")
	if _, err := os.Stat(specSchemas); err != nil {
		t.Skipf("real spec not cloned (%v) — run ./generate.sh %s first", err, goldenVersion)
	}
	goldens := filepath.Join(repoRoot, "goldens", goldenVersion)
	if _, err := os.Stat(goldens); err != nil {
		t.Fatalf("goldens missing: %v", err)
	}

	out := t.TempDir()
	cmd := exec.Command("go", "run", "./cmd/ucpgen", "preprocess",
		"-schemas", specSchemas, "-out-schemas", out)
	cmd.Dir = repoRoot
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("preprocess failed: %v\n%s", err, b)
	}

	var checked, mismatched int
	err = filepath.Walk(goldens, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(goldens, p)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Errorf("missing from Go output: %s", rel)
			mismatched++
			return nil
		}
		checked++
		if !bytes.Equal(want, got) {
			t.Errorf("golden mismatch: %s", rel)
			mismatched++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Reverse direction: Go must not emit files the goldens lack — an extra
	// variant is as much a parity failure as a missing one.
	err = filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(out, p)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(goldens, rel)); err != nil {
			t.Errorf("extra file not in goldens: %s", rel)
			mismatched++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if checked == 0 {
		t.Fatal("no golden files were compared")
	}
	t.Logf("compared %d golden files, %d mismatched", checked, mismatched)
}
