package emit

import (
	"strings"
	"testing"
)

// nestedItemsCorpus mirrors shopping/types/totals.json: an array whose
// element schema inherits a sibling file through allOf and adds one
// property of its own. The inherited fields live nowhere else, so a merge
// that stops at the document root and $defs emits the element type with
// only "lines" on it.
func nestedItemsCorpus() map[string]map[string]any {
	return map[string]map[string]any{
		"types/total.json": {
			"title":    "Total",
			"type":     "object",
			"required": []any{"amount", "type"},
			"properties": map[string]any{
				"amount":       map[string]any{"type": "number"},
				"type":         map[string]any{"type": "string"},
				"display_text": map[string]any{"type": "string"},
			},
		},
		"types/totals.json": {
			"title": "Totals",
			"type":  "array",
			"items": map[string]any{
				"type":  "object",
				"allOf": []any{map[string]any{"$ref": "total.json"}},
				"properties": map[string]any{
					"lines": map[string]any{"type": "string"},
				},
			},
		},
	}
}

func TestMergeAllOfResolvesNestedItems(t *testing.T) {
	corpus := nestedItemsCorpus()
	if err := mergeAllOf(corpus["types/totals.json"], "types/totals.json", corpus); err != nil {
		t.Fatalf("mergeAllOf: %v", err)
	}
	items, ok := corpus["types/totals.json"]["items"].(map[string]any)
	if !ok {
		t.Fatalf("items is %T, want an object", corpus["types/totals.json"]["items"])
	}
	if _, unmerged := items["allOf"]; unmerged {
		t.Errorf("items still carries an allOf after merging: %v", items["allOf"])
	}
	props, _ := items["properties"].(map[string]any)
	for _, want := range []string{"amount", "type", "display_text", "lines"} {
		if _, ok := props[want]; !ok {
			t.Errorf("merged items is missing property %q; have %v", want, props)
		}
	}
	req, _ := items["required"].([]any)
	if len(req) != 2 {
		t.Errorf("merged items required = %v, want the inherited amount and type", req)
	}
}

// The user-visible symptom: the emitted element struct is missing every
// inherited field.
func TestEmitNestedItemsInheritsFields(t *testing.T) {
	corpus := nestedItemsCorpus()
	src, err := emitFromCorpus(t, "types/totals.json", corpus)
	if err != nil {
		t.Fatalf("emitFromCorpus: %v", err)
	}
	collapsed := collapse(src)
	for _, want := range []string{
		"Amount float64 `json:\"amount\"`",
		"Type string `json:\"type\"`",
		"DisplayText *string `json:\"display_text,omitzero\"`",
		"Lines *string `json:\"lines,omitzero\"`",
	} {
		if !strings.Contains(collapsed, want) {
			t.Errorf("emitted element struct missing %q\n---\n%s", want, src)
		}
	}
}

// The residual gate is what makes an unresolvable inheritance loud rather
// than a silently narrower type. It has to reach nested nodes too: a
// branch dropped inside items costs exactly as many fields as one dropped
// at the root.
func TestMergeAllOfNestedResidualIsAnError(t *testing.T) {
	corpus := map[string]map[string]any{
		"types/totals.json": {
			"title": "Totals",
			"type":  "array",
			"items": map[string]any{
				"type":  "object",
				"allOf": []any{map[string]any{"$ref": "#/$defs/nonexistent"}},
				"properties": map[string]any{
					"lines": map[string]any{"type": "string"},
				},
			},
		},
	}
	err := mergeAllOf(corpus["types/totals.json"], "types/totals.json", corpus)
	if err == nil {
		t.Fatal("an unresolvable allOf inside items must fail generation, not drop its fields")
	}
	if !strings.Contains(err.Error(), "unresolved") {
		t.Errorf("error = %q, want it to report an unresolved allOf", err)
	}
	if !strings.Contains(err.Error(), "items") {
		t.Errorf("error = %q, want it to name the node the residual is on", err)
	}
}

// A conditional branch is a rule rather than a set of fields, so the
// preprocessor leaves it in the allOf on purpose and the emitter compiles
// it. The gate must not mistake that for dropped inheritance — including
// on a nested node, which is where the corpus actually puts it.
func TestMergeAllOfNestedConditionalResidualIsLegal(t *testing.T) {
	corpus := nestedItemsCorpus()
	items := corpus["types/totals.json"]["items"].(map[string]any)
	items["allOf"] = append(items["allOf"].([]any), map[string]any{
		"if":   map[string]any{"required": []any{"type"}},
		"then": map[string]any{"required": []any{"display_text"}},
	})
	if err := mergeAllOf(corpus["types/totals.json"], "types/totals.json", corpus); err != nil {
		t.Fatalf("a conditional-only residual is legal, got: %v", err)
	}
	residual, ok := items["allOf"].([]any)
	if !ok || len(residual) != 1 {
		t.Fatalf("items allOf = %v, want just the conditional branch", items["allOf"])
	}
}

// A $ref of "#" names the document itself. preprocess.MergeAllOf follows
// only "#/…" pointers, so it arrives here unresolved; leaving it in the
// residual would drop the whole borrowed schema.
func TestMergeAllOfResolvesWholeDocumentSelfRef(t *testing.T) {
	corpus := map[string]map[string]any{
		"types/payment_instrument.json": {
			"title":    "Payment Instrument",
			"type":     "object",
			"required": []any{"id"},
			"properties": map[string]any{
				"id":   map[string]any{"type": "string"},
				"type": map[string]any{"type": "string"},
			},
			"$defs": map[string]any{
				"selected_payment_instrument": map[string]any{
					"title": "Selected Payment Instrument",
					"type":  "object",
					"allOf": []any{map[string]any{"$ref": "#"}},
					"properties": map[string]any{
						"selected": map[string]any{"type": "boolean"},
					},
				},
			},
		},
	}
	doc := corpus["types/payment_instrument.json"]
	if err := mergeAllOf(doc, "types/payment_instrument.json", corpus); err != nil {
		t.Fatalf("mergeAllOf: %v", err)
	}
	def := doc["$defs"].(map[string]any)["selected_payment_instrument"].(map[string]any)
	props, _ := def["properties"].(map[string]any)
	for _, want := range []string{"id", "type", "selected"} {
		if _, ok := props[want]; !ok {
			t.Errorf("self-ref merge is missing property %q; have %v", want, props)
		}
	}
	// The borrowed document contributes fields, not identity.
	if title, _ := def["title"].(string); title != "Selected Payment Instrument" {
		t.Errorf("title = %q, want the borrowing schema's own", title)
	}
	if _, leaked := def["$defs"]; leaked {
		t.Error("the borrowed document's $defs must not be inlined into the borrower")
	}
}

// crossFileConditionalCorpus mirrors the totals.json / total.json pair:
// the element schema of an array borrows a sibling file through allOf, and
// that sibling declares its own conditional rule. The rule lives only in
// the borrowed file, so the element type can only get it by inlining.
func crossFileConditionalCorpus() map[string]map[string]any {
	return map[string]map[string]any{
		"types/total.json": {
			"title":    "Total",
			"type":     "object",
			"required": []any{"amount", "type"},
			"properties": map[string]any{
				"amount": map[string]any{"type": "number"},
				"type":   map[string]any{"type": "string"},
			},
			"allOf": []any{
				map[string]any{
					"if": map[string]any{
						"properties": map[string]any{
							"type": map[string]any{"enum": []any{"discount"}},
						},
						"required": []any{"type"},
					},
					"then": map[string]any{
						"properties": map[string]any{
							"amount": map[string]any{"exclusiveMaximum": float64(0)},
						},
					},
				},
			},
		},
		"types/totals.json": {
			"title": "Totals",
			"type":  "array",
			"items": map[string]any{
				"type":  "object",
				"allOf": []any{map[string]any{"$ref": "total.json"}},
				"properties": map[string]any{
					"lines": map[string]any{"type": "string"},
				},
			},
		},
	}
}

// The rule has to reach the borrowing node's own allOf, because that is
// the only residual the conditional compiler reads.
func TestResolveCrossFileAllOfCarriesConditionals(t *testing.T) {
	corpus := crossFileConditionalCorpus()
	items := corpus["types/totals.json"]["items"].(map[string]any)
	if err := resolveCrossFileAllOf(items, "types/totals.json", corpus, map[string]bool{}); err != nil {
		t.Fatalf("resolveCrossFileAllOf: %v", err)
	}
	branches, _ := items["allOf"].([]any)
	conditionals := 0
	for _, b := range branches {
		bm, isObj := b.(map[string]any)
		if !isObj {
			continue
		}
		if _, hasIf := bm["if"]; hasIf {
			conditionals++
		}
		if leftover, still := bm["allOf"]; still {
			t.Errorf("the inlined copy still holds a conditional allOf: %v", leftover)
		}
	}
	if conditionals != 1 {
		t.Errorf("borrowing node carries %d conditional branches, want 1: %v", conditionals, branches)
	}
}

// The user-visible symptom: the element type accepts a positive discount
// while the type generated straight from total.json rejects it.
func TestEmitNestedItemsInheritsConditional(t *testing.T) {
	corpus := crossFileConditionalCorpus()
	src, err := emitFromCorpus(t, "types/totals.json", corpus)
	if err != nil {
		t.Fatalf("emitFromCorpus: %v", err)
	}
	collapsed := collapse(src)
	for _, want := range []string{
		`if v.Type == "discount" {`,
		"if v.Amount >= 0 {",
	} {
		if n := strings.Count(collapsed, want); n != 1 {
			t.Errorf("emitted element type has %d occurrences of %q, want exactly 1\n---\n%s", n, want, src)
		}
	}
}

// Only rules carry down. A branch left in the target's allOf for any other
// reason is scoped to that target, and lifting it would apply a constraint
// the borrowing node never inherited.
func TestResolveCrossFileAllOfLeavesNonConditionalResidual(t *testing.T) {
	corpus := crossFileConditionalCorpus()
	total := corpus["types/total.json"]
	total["allOf"] = append(total["allOf"].([]any), map[string]any{"$ref": "#/$defs/absent"})
	items := corpus["types/totals.json"]["items"].(map[string]any)
	if err := resolveCrossFileAllOf(items, "types/totals.json", corpus, map[string]bool{}); err != nil {
		t.Fatalf("resolveCrossFileAllOf: %v", err)
	}
	for _, b := range items["allOf"].([]any) {
		bm, isObj := b.(map[string]any)
		if !isObj {
			continue
		}
		if ref, isRef := bm["$ref"].(string); isRef && ref == "#/$defs/absent" {
			t.Error("a non-conditional leftover branch was lifted onto the borrowing node")
		}
	}
}
