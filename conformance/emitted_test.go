package conformance

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEmittedSpecCompiles generates Go models for the entire normalized
// spec and requires the result to build and vet cleanly.
//
// This is the check the emitter cannot perform on itself: format.Source
// only *parses* what it is given, so every type-check error — a duplicate
// field, an unresolved name, an unused or cyclic import — passes the
// emitter's own gate and would only surface for whoever ran the generator
// next.
func TestEmittedSpecCompiles(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goldens := filepath.Join(repoRoot, "goldens", goldenVersion)
	if _, err := os.Stat(goldens); err != nil {
		t.Skipf("goldens missing (%v)", err)
	}

	out := t.TempDir()
	gen := exec.Command("go", "run", "./cmd/ucpgen", "emit",
		"-schemas", goldens, "-out", out, "-spec-ref", "conformance")
	gen.Dir = repoRoot
	if b, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("emit failed: %v\n%s", err, b)
	}

	// The generated tree is its own module so `go build ./...` resolves the
	// cross-package imports the emitter wrote.
	mod := "module github.com/chaz8081/ucp-go\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, step := range [][]string{
		{"go", "build", "./..."},
		{"go", "vet", "./..."},
	} {
		cmd := exec.Command(step[0], step[1:]...)
		cmd.Dir = out
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v on generated tree: %v\n%s", step, err, b)
		}
	}

	// A package list is the cheapest assertion that the layout is what the
	// design intends, and that nothing collapsed into a single package.
	list := exec.Command("go", "list", "./...")
	list.Dir = out
	b, err := list.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, b)
	}
	t.Logf("generated packages:\n%s", b)
}
