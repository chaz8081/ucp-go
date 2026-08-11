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
	// Every property checkout.json requires is supplied, and status is a
	// value its enum actually permits — the payload has to satisfy the
	// generated checks, not merely decode.
	var c shopping.Checkout
	if err := json.Unmarshal([]byte(` + "`" + `{"id":"chk_1","currency":"USD","status":"ready_for_complete","line_items":[],"links":[],"totals":[],"ucp":{"version":"2026-04-08"}}` + "`" + `), &c); err != nil {
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
	// A required property absent from the JSON must be rejected. Decoded to
	// its zero value it is indistinguishable from one that was present, so
	// this is the check that needs the recorded presence to work at all.
	var empty shopping.Checkout
	if err := json.Unmarshal([]byte(` + "`{}`" + `), &empty); err != nil {
		fail("empty checkout decode: %v", err)
	}
	if err := empty.Validate(); err == nil {
		fail("Validate accepted {} for a schema with required properties")
	}

	// The payload that decoded at the top must still validate: presence
	// tracking must not reject what the schema permits.
	if err := c.Validate(); err != nil {
		fail("Validate rejected a complete checkout: %v", err)
	}

	// A value built in Go was never decoded and so carries no presence
	// information. Judging it on an empty record would fail every request
	// the SDK is used to construct, which is most of what it is for — so
	// the presence check is skipped and only the value checks run. Those
	// still apply: a required enum left at its zero value really is
	// invalid, whether or not the value came from JSON.
	built := shopping.Checkout{ID: "chk_2", Currency: "USD", Status: "ready_for_complete"}
	if err := built.Validate(); err != nil {
		fail("Validate rejected a hand-constructed value: %v", err)
	}
	blank := shopping.Checkout{ID: "chk_3"}
	if err := blank.Validate(); err == nil {
		fail("Validate accepted a hand-constructed value whose required enum is empty")
	}

	// An open object must preserve keys the schema never names: UCP is an
	// extension-first protocol and signals.json exists so that multiple
	// extensions can contribute to a shared namespace.
	var sig types.Signals
	ext := ` + "`" + `{"dev.ucp.buyer_ip":"1.2.3.4","com.example.device_id":"abc"}` + "`" + `
	if err := json.Unmarshal([]byte(ext), &sig); err != nil {
		fail("signals decode: %v", err)
	}
	sigOut, err := json.Marshal(sig)
	if err != nil {
		fail("signals marshal: %v", err)
	}
	var sigGot map[string]any
	json.Unmarshal(sigOut, &sigGot)
	if _, ok := sigGot["com.example.device_id"]; !ok {
		fail("signals dropped an extension key: %s", sigOut)
	}

	fmt.Println("round-trip ok")
}
`
