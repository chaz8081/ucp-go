package conformance

import (
	"encoding/json"
	"testing"

	"github.com/chaz8081/ucp-go/shopping"
	"github.com/chaz8081/ucp-go/shopping/types"
)

// validCheckoutJSON is a checkout that satisfies checkout.json in full.
//
// totals is deliberately not `[]`: the schema requires exactly one
// subtotal entry, so an empty list is invalid. Both tests below carried
// `"totals":[]` and called themselves complete until phase 6 began
// enforcing contains — neither fixture had ever been schema-valid, the
// same way phase 4's round-trip fixture never was. Keeping one copy means
// the claim "this is a valid checkout" is only made in one place.
const validCheckoutJSON = `{"id":"chk_1","currency":"USD","status":"ready_for_complete",` +
	`"line_items":[],"links":[],` +
	`"totals":[{"type":"subtotal","amount":1000,"display_text":"Subtotal"}],` +
	`"ucp":{"version":"2026-04-08"}}`

// TestModelsRoundTrip guards the defects a build-only check cannot see:
// fields lost to unresolved cross-file inheritance, extension keys dropped
// by an open object, and required properties that decode to their zero
// value indistinguishably from being absent.
func TestModelsRoundTrip(t *testing.T) {
	// Every property checkout.json requires is supplied, and status is a
	// value its enum actually permits.
	var c shopping.Checkout
	in := validCheckoutJSON
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
	in := validCheckoutJSON
	if err := json.Unmarshal([]byte(in), &c); err != nil {
		t.Fatalf("checkout decode: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("a complete checkout must validate, got: %v", err)
	}
}

// TestBareNullRejected pins the fix for a defect the differential harness
// surfaced: json.Unmarshal treats null as a no-op for every Go type, and the
// decoder is this SDK's JSON type check, so a null document decoded to the
// zero value and then validated as though it were real. Amount accepted it
// outright; Totals accepted it and additionally skipped its contains count,
// which is guarded on the slice being non-nil. ReverseDomainName rejected it
// only by luck, because the empty string fails its pattern — which is why
// the rule cannot live in Validate.
func TestBareNullRejected(t *testing.T) {
	var a types.Amount
	if err := json.Unmarshal([]byte(`null`), &a); err == nil {
		t.Error("Amount decoded a bare null; the schema is type: integer")
	}
	var tot types.Totals
	if err := json.Unmarshal([]byte(`null`), &tot); err == nil {
		t.Error("Totals decoded a bare null; the schema is type: array")
	}
	var code types.ErrorCode
	if err := json.Unmarshal([]byte(`null`), &code); err == nil {
		t.Error("ErrorCode decoded a bare null; the schema is type: string")
	}

	// A required property of one of these types is a value, not a pointer,
	// so its decoder runs and the null is rejected there rather than
	// reaching Validate. total.json requires amount.
	var tl types.Total
	if err := json.Unmarshal([]byte(`{"amount":null,"type":"subtotal"}`), &tl); err == nil {
		t.Error("Total accepted null for its required amount")
	}

	// Real values must still decode, or the codec has broken the type.
	if err := json.Unmarshal([]byte(`1000`), &a); err != nil {
		t.Errorf("Amount rejected a real value: %v", err)
	}
	if a != 1000 {
		t.Errorf("Amount = %d, want 1000", a)
	}
	if err := json.Unmarshal([]byte(validTotalsJSON), &tot); err != nil {
		t.Errorf("Totals rejected a real value: %v", err)
	}
	if err := tot.Validate(); err != nil {
		t.Errorf("a valid Totals must validate: %v", err)
	}
}

const validTotalsJSON = `[{"type":"subtotal","amount":1000,"display_text":"Subtotal"}]`

// TestOptionalNullStillAccepted is the regression guard on the fix above.
// An explicit null for an OPTIONAL property means absent, and the schema
// permits it. It keeps working because an optional non-nilable property is
// rendered as a pointer and encoding/json stores nil for a null pointer
// field without ever consulting the pointed-to type's Unmarshaler — so the
// null-rejecting decoder is never reached. If that stops holding, the fix
// has started rejecting payloads the spec allows.
func TestOptionalNullStillAccepted(t *testing.T) {
	// price_filter.json makes both of its Amount properties optional.
	var pf types.PriceFilter
	if err := json.Unmarshal([]byte(`{"max":null,"min":null}`), &pf); err != nil {
		t.Fatalf("PriceFilter rejected an explicit null for its optional Amounts: %v", err)
	}
	if pf.Max != nil || pf.Min != nil {
		t.Errorf("an explicit null must mean absent, got max=%v min=%v", pf.Max, pf.Min)
	}
	if err := pf.Validate(); err != nil {
		t.Errorf("a PriceFilter with no bounds must validate: %v", err)
	}

	// Absence must behave identically to an explicit null.
	var absent types.PriceFilter
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatalf("PriceFilter rejected an absent optional: %v", err)
	}
	if absent.Max != nil {
		t.Error("an absent optional must decode to nil")
	}

	// A real value still reaches the decoder and is kept.
	var set types.PriceFilter
	if err := json.Unmarshal([]byte(`{"max":5000}`), &set); err != nil {
		t.Fatalf("PriceFilter rejected a real optional value: %v", err)
	}
	if set.Max == nil || *set.Max != 5000 {
		t.Errorf("max = %v, want 5000", set.Max)
	}

	// The corpus has no optional property of an ARRAY alias today — every
	// totals field is required — so the same rule is pinned on the shape the
	// emitter would produce for one, which is what a spec release adding
	// such a property would ship.
	var opt struct {
		Totals *types.Totals `json:"totals,omitzero"`
	}
	if err := json.Unmarshal([]byte(`{"totals":null}`), &opt); err != nil {
		t.Fatalf("an optional Totals rejected an explicit null: %v", err)
	}
	if opt.Totals != nil {
		t.Error("an explicit null for an optional Totals must mean absent")
	}
	if err := json.Unmarshal([]byte(`{"totals":`+validTotalsJSON+`}`), &opt); err != nil {
		t.Fatalf("an optional Totals rejected a real value: %v", err)
	}
	if opt.Totals == nil || len(*opt.Totals) != 1 {
		t.Errorf("optional Totals = %v, want one entry", opt.Totals)
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
