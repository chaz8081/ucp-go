package conformance

import (
	"encoding/json"
	"testing"

	"github.com/chaz8081/ucp-go/shopping"
	"github.com/chaz8081/ucp-go/shopping/types"
)

// TestModelsRoundTrip guards the defects a build-only check cannot see:
// fields lost to unresolved cross-file inheritance, extension keys dropped
// by an open object, and required properties that decode to their zero
// value indistinguishably from being absent.
func TestModelsRoundTrip(t *testing.T) {
	// Every property checkout.json requires is supplied, and status is a
	// value its enum actually permits.
	var c shopping.Checkout
	in := `{"id":"chk_1","currency":"USD","status":"ready_for_complete","line_items":[],"links":[],"totals":[],"ucp":{"version":"2026-04-08"}}`
	if err := json.Unmarshal([]byte(in), &c); err != nil {
		t.Fatalf("checkout decode: %v", err)
	}
	if c.ID != "chk_1" {
		t.Errorf("checkout id = %q, want chk_1", c.ID)
	}

	// Fields inherited through a cross-file allOf must survive a round trip.
	var d types.ShippingDestination
	if err := json.Unmarshal([]byte(`{"id":"d1","street_address":"1 Main St","postal_code":"12345"}`), &d); err != nil {
		t.Fatalf("shipping_destination decode: %v", err)
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("shipping_destination marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "street_address", "postal_code"} {
		if _, ok := got[k]; !ok {
			t.Errorf("shipping_destination lost inherited field %q: %s", k, out)
		}
	}

	// UCP is extension-first: an open object exists so extensions can
	// contribute keys the base schema never lists.
	var sig types.Signals
	if err := json.Unmarshal([]byte(`{"dev.ucp.buyer_ip":"1.2.3.4","com.example.device_id":"abc"}`), &sig); err != nil {
		t.Fatalf("signals decode: %v", err)
	}
	sigOut, err := json.Marshal(sig)
	if err != nil {
		t.Fatalf("signals marshal: %v", err)
	}
	var sigGot map[string]any
	if err := json.Unmarshal(sigOut, &sigGot); err != nil {
		t.Fatal(err)
	}
	if _, ok := sigGot["com.example.device_id"]; !ok {
		t.Errorf("signals dropped an extension key: %s", sigOut)
	}
}

// TestPrimaryTypesCanValidate is a regression test for a defect that made
// the SDK unusable: `ucp` is required on Cart, Checkout and Order, and its
// metadata union is a synthesized oneOf whose cart, catalog and order
// response members are structurally identical. Enforcing "exactly one"
// against alternatives nothing can tell apart meant those types — the
// protocol's primary responses — could never validate at all.
//
// The emitter now detects an unsatisfiable oneOf and does not enforce
// exclusivity for it, saying so in the generated doc comment. If that
// detection regresses, this fails rather than shipping types no caller can
// use.
func TestPrimaryTypesCanValidate(t *testing.T) {
	var c shopping.Checkout
	in := `{"id":"chk_1","currency":"USD","status":"ready_for_complete","line_items":[],"links":[],"totals":[],"ucp":{"version":"2026-04-08"}}`
	if err := json.Unmarshal([]byte(in), &c); err != nil {
		t.Fatalf("checkout decode: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("a complete checkout must validate, got: %v", err)
	}
}

// TestRequiredPresence pins the rule that keeps the SDK usable for building
// requests while still rejecting incomplete payloads.
func TestRequiredPresence(t *testing.T) {
	// Absent required properties must be rejected. Decoded to their zero
	// values they are indistinguishable from having been present, so this
	// is what the recorded presence exists for.
	var empty shopping.Checkout
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil {
		t.Fatalf("empty checkout decode: %v", err)
	}
	if err := empty.Validate(); err == nil {
		t.Error("Validate accepted {} for a schema with required properties")
	}

	// A value built in Go was never decoded and carries no presence
	// information; judging it on an empty record would fail every request
	// the SDK is used to construct. Value checks still apply, which is why
	// status must be a permitted enum member.
	built := shopping.Checkout{ID: "chk_2", Currency: "USD", Status: "ready_for_complete"}
	if err := built.Validate(); err != nil {
		t.Errorf("Validate rejected a hand-constructed value: %v", err)
	}
	blank := shopping.Checkout{ID: "chk_3"}
	if err := blank.Validate(); err == nil {
		t.Error("Validate accepted a hand-constructed value whose required enum is empty")
	}
}
