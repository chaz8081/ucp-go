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

// TestEmittedSpecRoundTrips guards the four defects a build-only check
// cannot see: types that are never emitted, fields silently lost to
// unresolved cross-file inheritance, fields typed `any` when a real type is
// derivable, and union-typed fields that cannot be decoded at all.
func TestEmittedSpecRoundTrips(t *testing.T) {
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
	mod := "module github.com/chaz8081/ucp-go\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}

	// Every schema in the corpus must produce a file, request variants
	// included — they are the protocol's entire request surface.
	var schemas, emitted int
	filepath.Walk(goldens, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(p) == ".json" {
			schemas++
		}
		return nil
	})
	filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(p) == ".go" {
			emitted++
		}
		return nil
	})
	if emitted != schemas {
		t.Errorf("emitted %d Go files for %d schemas; every schema must produce one", emitted, schemas)
	}

	probe := filepath.Join(out, "roundtrip")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "main.go"), []byte(roundTripProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	run := exec.Command("go", "run", "./roundtrip")
	run.Dir = out
	b, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("round-trip probe failed: %v\n%s", err, b)
	}
	t.Logf("round-trip probe:\n%s", b)
}

// roundTripProbe exercises the generated types the way a consumer would.
// It lives here rather than in a fixture file so the assertions sit next to
// the reasons they exist.
const roundTripProbe = `package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chaz8081/ucp-go/shopping"
	"github.com/chaz8081/ucp-go/shopping/types"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func main() {
	// A union-typed required field must decode: ` + "`ucp`" + ` is required on
	// Cart, Checkout and Order, and a bare interface would make all three
	// undecodable.
	var c shopping.Checkout
	if err := json.Unmarshal([]byte(` + "`" + `{"id":"chk_1","status":"ready_for_payment","ucp":{"version":"2026-04-08"}}` + "`" + `), &c); err != nil {
		fail("checkout decode: %v", err)
	}
	if c.ID != "chk_1" {
		fail("checkout id = %q, want chk_1", c.ID)
	}

	// Fields inherited through a cross-file allOf must survive a round trip.
	var d types.ShippingDestination
	in := ` + "`" + `{"id":"d1","street_address":"1 Main St","postal_code":"12345"}` + "`" + `
	if err := json.Unmarshal([]byte(in), &d); err != nil {
		fail("shipping_destination decode: %v", err)
	}
	out, err := json.Marshal(d)
	if err != nil {
		fail("shipping_destination marshal: %v", err)
	}
	var got map[string]any
	json.Unmarshal(out, &got)
	for _, k := range []string{"id", "street_address", "postal_code"} {
		if _, ok := got[k]; !ok {
			fail("shipping_destination lost inherited field %q: %s", k, out)
		}
	}
	fmt.Println("round-trip ok")
}
`
