package emit

import (
	"strings"
	"testing"
)

// A schema with `properties: {}` has no properties. The empty map still
// satisfies a `.(map[string]any)` type assertion, which sent four request
// variants down the struct path and shipped them as empty structs whose
// Validate accepted any JSON object.
func TestEmptyPropertiesWithUnionRendersAsUnion(t *testing.T) {
	corpus := map[string]map[string]any{
		"types/message_error.json": {
			"title": "Message Error", "type": "object",
			"required":   []any{"code"},
			"properties": map[string]any{"code": map[string]any{"type": "string"}},
		},
		"types/message_info.json": {
			"title": "Message Info", "type": "object",
			"required":   []any{"text"},
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
		},
		"types/message_request.json": {
			"title": "Message Request", "type": "object",
			"properties": map[string]any{},
			"required":   []any{},
			"oneOf": []any{
				map[string]any{"$ref": "message_error.json"},
				map[string]any{"$ref": "message_info.json"},
			},
		},
	}
	src, err := emitFromCorpus(t, "types/message_request.json", corpus)
	if err != nil {
		t.Fatalf("emitFromCorpus: %v", err)
	}
	collapsed := collapse(src)
	for _, want := range []string{"MessageError *MessageError", "MessageInfo *MessageInfo"} {
		if !strings.Contains(collapsed, want) {
			t.Errorf("union member missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(collapsed, "not modeled as distinct types yet") {
		t.Errorf("rendered as a struct with an unmodeled-union note:\n%s", src)
	}
}

// The guard must stay: a union that really does have sibling properties is
// still rendered as the shared base, with the note saying so.
func TestRealPropertiesWithUnionStillRendersAsStruct(t *testing.T) {
	corpus := map[string]map[string]any{
		"types/thing.json": {
			"title": "Thing", "type": "object",
			"required":   []any{"id"},
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"anyOf": []any{
				map[string]any{"properties": map[string]any{"id": map[string]any{"const": "a"}}},
				map[string]any{"properties": map[string]any{"id": map[string]any{"const": "b"}}},
			},
		},
	}
	src, err := emitFromCorpus(t, "types/thing.json", corpus)
	if err != nil {
		t.Fatalf("emitFromCorpus: %v", err)
	}
	if !strings.Contains(collapse(src), "ID string") {
		t.Errorf("expected a struct carrying the shared base:\n%s", src)
	}
}

// The same empty map fools goTypeExpr's object case, which promotes a node
// with properties to a named inline struct. At the file's own type that
// promotion names the type after itself and queues it again, so the name
// grows by one prefix per pass and generation never terminates. An object
// whose properties map is empty names no fields, so it is the map its
// additionalProperties describes.
func TestEmptyPropertiesObjectRendersAsMap(t *testing.T) {
	src, err := emitOne(t, "types/attribution_request.json", map[string]any{
		"title": "Attribution Request", "type": "object",
		"properties":           map[string]any{},
		"required":             []any{},
		"additionalProperties": map[string]any{"type": "string"},
	})
	if err != nil {
		t.Fatalf("emitOne: %v", err)
	}
	if !strings.Contains(src, "type AttributionRequest map[string]string") {
		t.Errorf("want a map alias over the additionalProperties schema:\n%s", src)
	}
}

// shapeOf must stay in step with goTypeExpr, and it reads properties the
// same way. Left as a struct shape, the map keywords are dropped on the
// floor: compileInto returns early for a struct, expecting renderStruct to
// carry them instead — and renderStruct is exactly the path an empty
// properties map no longer takes.
func TestEmptyPropertiesMapKeywordsStillChecked(t *testing.T) {
	src, err := emitOne(t, "types/attribution_request.json", map[string]any{
		"title": "Attribution Request", "type": "object",
		"properties":           map[string]any{},
		"required":             []any{},
		"minProperties":        float64(1),
		"additionalProperties": map[string]any{"type": "string"},
	})
	if err != nil {
		t.Fatalf("emitOne: %v", err)
	}
	if !strings.Contains(collapse(src), "len(*v) < 1") {
		t.Errorf("minProperties was dropped:\n%s", src)
	}
}

// goTypeExpr routes a union with no properties of its own to raw JSON,
// which round-trips losslessly. An empty properties map sent such a node to
// the object case instead, where it becomes an untyped map: the union goes
// unrecorded and the numbers inside come back as float64.
func TestNestedEmptyPropertiesUnionIsRawJSON(t *testing.T) {
	corpus := map[string]map[string]any{
		"types/message_error.json": {
			"title": "Message Error", "type": "object",
			"required":   []any{"code"},
			"properties": map[string]any{"code": map[string]any{"type": "string"}},
		},
		"types/envelope.json": {
			"title": "Envelope", "type": "object",
			"required": []any{"body"},
			"properties": map[string]any{
				"body": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
					"oneOf": []any{
						map[string]any{"$ref": "message_error.json"},
						map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	src, err := emitFromCorpus(t, "types/envelope.json", corpus)
	if err != nil {
		t.Fatalf("emitFromCorpus: %v", err)
	}
	if !strings.Contains(collapse(src), "Body json.RawMessage") {
		t.Errorf("want the union carried as raw JSON:\n%s", src)
	}
}
