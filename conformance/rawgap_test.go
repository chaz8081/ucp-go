package conformance

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// A field the emitter carries as json.RawMessage is invisible to Validate,
// so the differential harness attributes such disagreements to a named gap
// instead of reporting them as missing checks. That attribution is only
// safe if it is narrow: it must fire when the raw field is the entire
// reason the oracle rejected, and never when anything else is also wrong.
// Otherwise it would quietly absorb exactly the bugs the harness exists to
// find — the same "a skip is not a neutral act" failure that hid the
// unsatisfiable ucp union behind an uncompilable schema.

type gapModel struct {
	Name  string                     `json:"name"`
	UCP   json.RawMessage            `json:"ucp"`
	Extra map[string]json.RawMessage `json:"-"`
}

type noRawModel struct {
	Name string `json:"name"`
}

func gapSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(`{
		"type": "object",
		"required": ["name", "ucp"],
		"properties": {
			"name": {"type": "string", "minLength": 2},
			"ucp":  {"type": "object", "required": ["version"]}
		}
	}`), &doc); err != nil {
		t.Fatal(err)
	}
	c := newCompiler()
	if err := c.AddResource("mem://gap.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("mem://gap.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func instance(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRawFieldExplainsRejection(t *testing.T) {
	schema := gapSchema(t)

	cases := []struct {
		name    string
		model   any
		payload string
		want    string
		wantOK  bool
	}{
		{
			name:    "only the raw field is wrong",
			model:   new(gapModel),
			payload: `{"name":"ok","ucp":{}}`,
			want:    "ucp",
			wantOK:  true,
		},
		{
			name:    "a non-raw field is also wrong",
			model:   new(gapModel),
			payload: `{"name":"x","ucp":{}}`,
			wantOK:  false,
		},
		{
			name:    "only a non-raw field is wrong",
			model:   new(gapModel),
			payload: `{"name":"x","ucp":{"version":"2026-04-08"}}`,
			wantOK:  false,
		},
		{
			name:    "the model carries nothing as raw JSON",
			model:   new(noRawModel),
			payload: `{"name":"ok","ucp":{}}`,
			wantOK:  false,
		},
		{
			name: "a root-level complaint is never attributable",
			// A missing required property is reported at the document root,
			// not inside the property, so it must not be charged to the raw
			// field that happens to be absent.
			model:   new(gapModel),
			payload: `{"name":"ok"}`,
			wantOK:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := instance(t, tc.payload)
			if schema.Validate(inst) == nil {
				t.Fatal("payload must be rejected by the oracle for this test to mean anything")
			}
			got, ok := rawFieldExplainsRejection(schema, tc.model, inst)
			if ok != tc.wantOK {
				t.Fatalf("attributed=%v want %v (field %q)", ok, tc.wantOK, got)
			}
			if ok && got != tc.want {
				t.Errorf("attributed to %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRawJSONPropertiesIgnoresTheOpenObjectCatchAll(t *testing.T) {
	// Extra holds properties the schema never named. The oracle judges those
	// under additionalProperties, which the SDK does enforce, so counting
	// Extra as a raw field would excuse real failures.
	got := rawJSONProperties(new(gapModel))
	if !got["ucp"] {
		t.Error("ucp is json.RawMessage and must be reported")
	}
	if len(got) != 1 {
		t.Errorf("got %v, want only ucp: Extra is tagged \"-\" and is not a named property", got)
	}
}

func TestRawTypeCannotValidate(t *testing.T) {
	// A union whose alternatives share no properties is emitted as a
	// defined type over json.RawMessage — the whole type, not one field.
	// Its Validate has nothing to inspect, so it accepts anything the
	// decoder accepts, which for raw bytes is everything.
	//
	// That is a documented gap (see the README's first "what is and isn't
	// modeled" bullet), not a missing check the harness should report as a
	// disagreement. It is recognised by shape rather than by a list of type
	// names, so it cannot fall out of date as the corpus changes.
	if !isRawCarriedType(new(rawUnionModel)) {
		t.Error("a defined type over json.RawMessage must be recognised")
	}
	if isRawCarriedType(new(gapModel)) {
		t.Error("a struct that merely CONTAINS a raw field is not raw-carried; " +
			"its other fields are still validated and must stay comparable")
	}
	if isRawCarriedType(new(noRawModel)) {
		t.Error("an ordinary struct is not raw-carried")
	}
}

// rawUnionModel mirrors the emitted shape: a defined type over
// json.RawMessage, with a Validate that can only return nil.
type rawUnionModel json.RawMessage

func (v *rawUnionModel) Validate() error { return nil }
