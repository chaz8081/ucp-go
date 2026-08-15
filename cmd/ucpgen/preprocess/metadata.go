package preprocess

import (
	"sort"
	"strings"
)

// metadataUnionMembers returns the $defs names forming the UcpMetadata root
// union: discovery profiles plus every response_*_schema
// (preprocess_schemas.py:535-551). Sorted for deterministic output; Python
// preserves dict insertion order here, so the resulting oneOf ordering can
// differ — CanonicalJSON (Task 9) sorts oneOf-of-single-ref arrays on both
// sides of the golden comparison, making the orderings equivalent.
func metadataUnionMembers(ucpSchema map[string]any) []string {
	defs, _ := ucpSchema["$defs"].(map[string]any)
	var out []string
	for name := range defs {
		if name == "platform_schema" || name == "business_schema" ||
			(strings.HasPrefix(name, "response_") && strings.HasSuffix(name, "_schema")) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// NormalizeMetadata gives ucp.json a root anyOf union over its metadata
// members and truncates every other file's `ucp` property $ref to the file
// part only, so all models share one generic metadata type
// (preprocess_schemas.py:554-578). Files whose path contains "ucp.json" or
// "_request.json" are skipped in the truncation pass, matching python's
// substring checks.
//
// The union is anyOf, not oneOf. It exists to give code generation one
// metadata type, not to assert that the members are mutually exclusive —
// and they are not: response_cart_schema, response_catalog_schema and
// response_order_schema are identical apart from title and description, so
// an oneOf could never be satisfied while `ucp` is required on Cart,
// Checkout and Order (python-sdk 35af25c, reported as python-sdk#73).
func NormalizeMetadata(set *SchemaSet) {
	if ucp, ok := set.Files["ucp.json"]; ok {
		members := metadataUnionMembers(ucp)
		anyOf := make([]any, len(members))
		for i, name := range members {
			anyOf[i] = map[string]any{"$ref": "#/$defs/" + name}
		}
		ucp["anyOf"] = anyOf
	}
	for _, rel := range set.Paths() {
		if strings.Contains(rel, "ucp.json") || strings.Contains(rel, "_request.json") {
			continue
		}
		props, _ := set.Files[rel]["properties"].(map[string]any)
		ucpProp, _ := props["ucp"].(map[string]any)
		if ucpProp == nil {
			continue
		}
		if ref, ok := ucpProp["$ref"].(string); ok && strings.Contains(ref, "ucp.json") {
			filePart, _, _ := strings.Cut(ref, "#")
			ucpProp["$ref"] = filePart
		}
	}
}
