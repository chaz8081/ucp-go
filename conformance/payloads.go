package conformance

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// jsonMarshal is the encoder the cases below are built with.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// Payload construction for the differential harness.
//
// The harness asserts that both sides reach the same verdict, not that a
// payload is valid, so these instances do not have to be correct — they
// have to be interesting. The useful shape is a near miss: an instance that
// satisfies everything except the one constraint under test, because that
// is where a missing check on our side shows up as a disagreement rather
// than as both sides rejecting for some unrelated reason.

// payload is one differential case.
type payload struct {
	name string // what is being exercised, for the failure message
	json []byte
}

// outOfScopeKeywords make a schema's verdict legitimately undecidable for
// the generated code: they are documented as unenforced, so the oracle can
// reject where we accept, and a disagreement would be expected rather than
// a defect. Schemas declaring one are skipped, and the skips are counted.
var outOfScopeKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "not": true,
	"contains": true, "minContains": true, "maxContains": true,
	"dependentRequired": true, "dependentSchemas": true,
	"patternProperties": true,
	// format is annotation-only in draft 2020-12 and the oracle runs with
	// assertions off, so it does NOT belong here: both sides ignore it.
}

// usesOutOfScope reports the first out-of-scope keyword reachable from a
// schema, following $refs across the corpus, or "" if none.
func usesOutOfScope(schema map[string]any, corpus map[string]map[string]any, rel string) string {
	if hasUnmodeledUnion(schema) {
		return "union alongside properties"
	}
	return scanOutOfScope(schema, corpus, rel, map[string]bool{}, 0)
}

// hasUnmodeledUnion reports a node that declares alternatives *and* its own
// properties. The emitter renders those as a plain struct of the shared
// base and says so in the generated doc comment: narrowing each alternative
// into its own type is not done, so the choice between them is not
// enforced and the oracle can legitimately reject what we accept.
//
// A union with no properties of its own is a different shape entirely —
// that one becomes a struct of alternatives whose Validate delegates, and
// it is exercised rather than skipped.
func hasUnmodeledUnion(node map[string]any) bool {
	if _, hasProps := node["properties"].(map[string]any); !hasProps {
		return false
	}
	_, unionOK := unionOf(node)
	return unionOK
}

func scanOutOfScope(node any, corpus map[string]map[string]any, rel string, seen map[string]bool, depth int) string {
	if depth > 12 {
		return ""
	}
	switch t := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if outOfScopeKeywords[k] {
				return k
			}
		}
		if ref, ok := t["$ref"].(string); ok {
			target, targetRel, found := resolveRef(corpus, rel, ref)
			if found && !seen[targetRel+"#"+ref] {
				seen[targetRel+"#"+ref] = true
				if kw := scanOutOfScope(target, corpus, targetRel, seen, depth+1); kw != "" {
					return kw
				}
			}
		}
		for _, k := range keys {
			if k == "$ref" {
				continue
			}
			if kw := scanOutOfScope(t[k], corpus, rel, seen, depth+1); kw != "" {
				return kw
			}
		}
	case []any:
		for _, v := range t {
			if kw := scanOutOfScope(v, corpus, rel, seen, depth+1); kw != "" {
				return kw
			}
		}
	}
	return ""
}

// resolveRef follows a $ref to the schema node it names, returning the
// file it now lives in so that relative refs inside it still resolve.
func resolveRef(corpus map[string]map[string]any, rel, ref string) (map[string]any, string, bool) {
	filePart, fragment, _ := strings.Cut(ref, "#")
	targetRel := rel
	var doc map[string]any
	if filePart == "" {
		doc = corpus[rel]
	} else {
		targetRel = path.Join(path.Dir(rel), filePart)
		doc = corpus[targetRel]
	}
	if doc == nil {
		return nil, "", false
	}
	if fragment == "" || fragment == "/" {
		return doc, targetRel, true
	}
	name, ok := strings.CutPrefix(fragment, "/$defs/")
	if !ok || strings.Contains(name, "/") {
		return nil, "", false
	}
	defs, _ := doc["$defs"].(map[string]any)
	sub, ok := defs[name].(map[string]any)
	return sub, targetRel, ok
}

// builder makes instances for one corpus.
type builder struct {
	corpus map[string]map[string]any
}

// instance builds a best-effort valid value for a schema node.
func (b *builder) instance(node map[string]any, rel string, depth int) any {
	if depth > 8 {
		return nil
	}
	if ref, ok := node["$ref"].(string); ok {
		if target, targetRel, found := resolveRef(b.corpus, rel, ref); found {
			return b.instance(target, targetRel, depth+1)
		}
		return nil
	}
	// A fixed value beats anything derived from the type.
	if c, ok := node["const"]; ok {
		return c
	}
	if enum, ok := node["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	if members, ok := unionOf(node); ok {
		// Any member will do: the harness needs a decodable instance, not a
		// canonical one.
		for _, m := range members {
			if mm, ok := m.(map[string]any); ok {
				if v := b.instance(mm, rel, depth+1); v != nil {
					return v
				}
			}
		}
		return nil
	}

	switch typeOf(node) {
	case "string":
		return b.sampleString(node)
	case "integer", "number":
		if min, ok := node["minimum"].(float64); ok {
			return min
		}
		if excl, ok := node["exclusiveMinimum"].(float64); ok {
			return excl + 1
		}
		return float64(1)
	case "boolean":
		return true
	case "array":
		items, _ := node["items"].(map[string]any)
		n := 0
		if min, ok := node["minItems"].(float64); ok {
			n = int(min)
		}
		if n == 0 {
			n = 1 // an empty array exercises nothing inside it
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			if items == nil {
				out = append(out, "x")
				continue
			}
			v := b.instance(items, rel, depth+1)
			if v == nil {
				return []any{}
			}
			out = append(out, v)
		}
		return out
	case "object":
		return b.object(node, rel, depth)
	}
	return map[string]any{}
}

// object builds an instance carrying every required property, which is what
// makes the required-presence mutations below meaningful.
func (b *builder) object(node map[string]any, rel string, depth int) map[string]any {
	out := map[string]any{}
	props, _ := node["properties"].(map[string]any)
	for _, name := range requiredOf(node) {
		prop, ok := props[name].(map[string]any)
		if !ok {
			out[name] = "x"
			continue
		}
		if v := b.instance(prop, rel, depth+1); v != nil {
			out[name] = v
		}
	}
	// A map-shaped object needs at least one entry to satisfy minProperties
	// and to give propertyNames something to check.
	if len(props) == 0 {
		if ap, ok := node["additionalProperties"].(map[string]any); ok {
			key := b.sampleKey(node)
			if v := b.instance(ap, rel, depth+1); v != nil {
				out[key] = v
			}
		}
	}
	if min, ok := node["minProperties"].(float64); ok && float64(len(out)) < min {
		for name := range props {
			if _, have := out[name]; have {
				continue
			}
			if prop, ok := props[name].(map[string]any); ok {
				if v := b.instance(prop, rel, depth+1); v != nil {
					out[name] = v
				}
			}
			if float64(len(out)) >= min {
				break
			}
		}
	}
	return out
}

// sampleString returns a string the schema is likely to accept. A pattern
// cannot be inverted in general, so a small set of shapes covering the
// corpus is tried and the first match wins; when none does, the plain
// sample is returned and the case simply becomes a mutation rather than a
// valid seed — both sides still have to agree on it.
func (b *builder) sampleString(node map[string]any) string {
	candidates := []string{"x", "dev.ucp.sample", "sample", "2026-04-08T00:00:00Z", "https://example.test/x", "USD", "0"}
	pattern, hasPattern := node["pattern"].(string)
	if !hasPattern {
		if max, ok := node["maxLength"].(float64); ok && max < 1 {
			return ""
		}
		return "x"
	}
	re, err := ecmaRegexp(pattern)
	if err != nil {
		return "x"
	}
	for _, c := range candidates {
		if re.MatchString(c) {
			return c
		}
	}
	return "x"
}

// sampleKey returns a map key the schema's propertyNames is likely to
// accept, by the same reasoning as sampleString.
func (b *builder) sampleKey(node map[string]any) string {
	if names, ok := node["propertyNames"].(map[string]any); ok {
		return b.sampleString(names)
	}
	return "k"
}

func typeOf(node map[string]any) string {
	switch t := node["type"].(type) {
	case string:
		return t
	case []any:
		for _, v := range t {
			if s, _ := v.(string); s != "" && s != "null" {
				return s
			}
		}
	case nil:
		if _, ok := node["properties"]; ok {
			return "object"
		}
		if _, ok := node["additionalProperties"].(map[string]any); ok {
			return "object"
		}
		if _, ok := node["items"]; ok {
			return "array"
		}
	}
	return ""
}

func unionOf(node map[string]any) ([]any, bool) {
	for _, k := range []string{"oneOf", "anyOf"} {
		if m, ok := node[k].([]any); ok && len(m) > 0 {
			return m, true
		}
	}
	return nil, false
}

func requiredOf(node map[string]any) []string {
	raw, _ := node["required"].([]any)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// mutations returns the differential cases for one schema: a best-effort
// valid instance, then one variant per constraint that instance satisfies.
// Each variant breaks exactly one thing, so a disagreement names the check
// that is missing or wrong.
func (b *builder) mutations(schema map[string]any, rel string) []payload {
	base, ok := b.instance(schema, rel, 0).(map[string]any)
	if !ok {
		return nil
	}
	var out []payload
	add := func(name string, v map[string]any) {
		if raw, err := marshalStable(v); err == nil {
			out = append(out, payload{name: name, json: raw})
		}
	}
	add("base", base)
	add("empty-object", map[string]any{})

	props, _ := schema["properties"].(map[string]any)
	for _, name := range requiredOf(schema) {
		add("missing-required:"+name, without(base, name))
	}

	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prop, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		node := prop
		if ref, isRef := prop["$ref"].(string); isRef {
			if target, targetRel, found := resolveRef(b.corpus, rel, ref); found {
				node = target
				_ = targetRel
			}
		}
		if _, has := node["pattern"]; has {
			add("bad-pattern:"+name, with(base, name, "!! not a match !!"))
		}
		if _, has := node["enum"]; has {
			add("bad-enum:"+name, with(base, name, "__not_in_enum__"))
		}
		if min, has := node["minimum"].(float64); has {
			add("below-minimum:"+name, with(base, name, min-1))
		}
		if max, has := node["maxLength"].(float64); has && max < 4096 {
			add("over-maxlength:"+name, with(base, name, strings.Repeat("a", int(max)+1)))
		}
		if min, has := node["minItems"].(float64); has && min > 0 {
			add("too-few-items:"+name, with(base, name, []any{}))
		}
		if unique, has := node["uniqueItems"].(bool); has && unique {
			if arr, ok := base[name].([]any); ok && len(arr) > 0 {
				add("duplicate-items:"+name, with(base, name, []any{arr[0], arr[0]}))
			}
		}
	}
	return out
}

func without(m map[string]any, key string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

func with(m map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out[key] = value
	return out
}

// marshalStable encodes with sorted keys, which encoding/json already does
// for maps, and rejects anything unencodable rather than emitting it.
func marshalStable(v any) ([]byte, error) {
	raw, err := jsonMarshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return raw, nil
}
