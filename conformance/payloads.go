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
//
// if/then/not/contains/minContains/maxContains left this set in phase 6,
// when the emitter began compiling them. They are now checked against the
// oracle like any other keyword. else stays: it has no corpus occurrence
// and the emitter fails generation rather than guessing at it.
var outOfScopeKeywords = map[string]bool{
	"else":              true,
	"dependentRequired": true,
	"dependentSchemas":  true,
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
	// An empty properties map is not properties. It satisfies the type
	// assertion, which is what turned four types the emitter was breaking
	// into a counted skip line rather than a failure.
	props, _ := node["properties"].(map[string]any)
	if len(props) == 0 {
		return false
	}
	_, unionOK := unionOf(node)
	return unionOK
}

// scanOutOfScope walks a schema and everything it references.
//
// There is deliberately no depth cap. A JSON document is a finite tree, so
// plain recursion terminates on its own; the only unbounded path is a $ref
// cycle, which `seen` already breaks. An earlier cap of 12 silently stopped
// the walk mid-corpus: fulfillment_method reaches total.json's conditionals
// through fulfillment_group and fulfillment_option, and the keyword sat at
// exactly depth 12. When those conditionals moved one level deeper — into
// an allOf residual, where the preprocessor now preserves them — the cap
// hid them, and the harness began exercising a schema whose verdict it
// cannot legitimately predict. A cap that turns a skip into a false pass is
// worse than no cap.
func scanOutOfScope(node any, corpus map[string]map[string]any, rel string, seen map[string]bool, depth int) string {
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
	// An allOf member contributes required properties of its own, and the
	// corpus reaches the builder unmerged: totals.json's items node declares
	// no required properties at all and inherits amount and type entirely
	// through an allOf $ref to total.json. Without folding those in, every
	// instance of such a node is missing a required property, and a mutation
	// built from it is rejected for that rather than for the constraint it
	// set out to break — agreement by accident, which proves nothing.
	//
	// Members contributing no object of their own — the bare if/then pairs
	// total.json carries — fold in as nothing, which is correct: the emitter
	// enforces those conditionals, and building an instance that deliberately
	// trips one is what the mutations are for.
	if members, ok := node["allOf"].([]any); ok {
		for _, m := range members {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			sub, ok := b.instance(mm, rel, depth+1).(map[string]any)
			if !ok {
				continue
			}
			for k, v := range sub {
				if _, have := out[k]; !have {
					out[k] = v
				}
			}
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
//
// The strategy depends on the shape of the root. It used to require an
// object and return nothing otherwise, which silently excluded every
// union-, array- and scalar-rooted schema in the corpus.
func (b *builder) mutations(schema map[string]any, rel string) []payload {
	if members, ok := unionOf(schema); ok {
		props, _ := schema["properties"].(map[string]any)
		if len(props) == 0 {
			return b.unionMutations(schema, rel, members)
		}
	}
	switch typeOf(schema) {
	case "array":
		return b.arrayMutations(schema, rel)
	case "string", "integer", "number", "boolean":
		return b.scalarMutations(schema, rel)
	}
	if base, ok := b.instance(schema, rel, 0).(map[string]any); ok {
		return b.objectMutations(schema, rel, base)
	}
	return nil
}

// scalarMutations exercises a named primitive's constraints. These types
// exist precisely for those constraints — ReverseDomainName is a string
// whose whole purpose is its pattern — so leaving them unexercised left
// the most constraint-dense types in the corpus unchecked.
func (b *builder) scalarMutations(schema map[string]any, rel string) []payload {
	base := b.instance(schema, rel, 0)
	if base == nil {
		return nil
	}
	var out []payload
	add := func(name string, v any) {
		if raw, err := marshalStable(v); err == nil {
			out = append(out, payload{name: name, json: raw})
		}
	}
	add("base", base)

	if _, has := schema["pattern"]; has {
		add("bad-pattern", "!! not a match !!")
	}
	if _, has := schema["enum"]; has {
		add("bad-enum", "__not_in_enum__")
	}
	if min, has := schema["minimum"].(float64); has {
		add("below-minimum", min-1)
	}
	if max, has := schema["maximum"].(float64); has {
		add("above-maximum", max+1)
	}
	if max, has := schema["maxLength"].(float64); has && max < 4096 {
		add("over-maxlength", strings.Repeat("a", int(max)+1))
	}
	if min, has := schema["minLength"].(float64); has && min > 0 {
		add("under-minlength", "")
	}
	// A value of the wrong JSON type is a rejection both sides must reach.
	// An object is wrong for every scalar shape.
	add("wrong-json-type", map[string]any{})
	// null is the wrong JSON type that the decoder alone cannot catch:
	// json.Unmarshal treats it as a no-op for every Go type, so it used to
	// arrive at Validate as an indistinguishable zero value and be accepted.
	// A nil any marshals to the literal null.
	add("null", nil)
	return out
}

// arrayMutations exercises the array's own keywords. contains is the one
// that matters most: totals.json requires exactly one subtotal entry, and
// until this existed that rule was checked only by unit tests — the schema
// produced no payloads at all, so it never reached the oracle.
func (b *builder) arrayMutations(schema map[string]any, rel string) []payload {
	base, ok := b.instance(schema, rel, 0).([]any)
	if !ok {
		return nil
	}
	var out []payload
	add := func(name string, v any) {
		if raw, err := marshalStable(v); err == nil {
			out = append(out, payload{name: name, json: raw})
		}
	}

	match := b.matching(schema, rel)
	if match != nil {
		// An array of exactly one match is the instance the count keywords
		// are supposed to accept. Without it the whole set below is
		// rejections, and a count that rejected everything would pass.
		base = []any{match}
	}
	add("base", base)
	// An empty array carries no items, so nothing but minContains can reject
	// it — which is what makes it a clean reading of that keyword.
	add("empty-array", []any{})
	// null decodes into a slice as a no-op, leaving it nil. Totals then
	// skipped its contains count outright, because that check is guarded on
	// the slice being non-nil — the array root's version of the same hole.
	add("null", nil)

	if match != nil {
		// Zero matches, then one more than maxContains permits. Both are
		// rejections the generated count has to reach the same verdict on.
		add("too-few-matching", []any{})
		max := 1
		if m, ok := schema["maxContains"].(float64); ok {
			max = int(m)
		}
		over := make([]any, 0, max+1)
		for range max + 1 {
			over = append(over, match)
		}
		add("too-many-matching", over)
	}
	return out
}

// matching builds an array element that satisfies both items and contains.
//
// contains is a fragment, not a whole element schema: totals.json's is only
// {"type": "subtotal"}, which says nothing about the amount that items
// requires. An instance of the fragment alone therefore violates items, and
// every payload built from it would be rejected over the missing property
// rather than over the count under test — both sides agreeing for a reason
// that has nothing to do with the keyword. Layering the fragment over a full
// items instance leaves the count as the only thing left to disagree about.
func (b *builder) matching(schema map[string]any, rel string) any {
	contains, ok := schema["contains"].(map[string]any)
	if !ok {
		return nil
	}
	match := b.instance(contains, rel, 0)
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return match
	}
	elem, elemOK := b.instance(items, rel, 0).(map[string]any)
	over, overOK := match.(map[string]any)
	if !elemOK || !overOK {
		return match
	}
	for k, v := range over {
		elem[k] = v
	}
	return elem
}

// unionMutations exercises each alternative in turn. A union's content is
// its alternatives, so the property mutations objectMutations applies have
// nothing to work on.
func (b *builder) unionMutations(schema map[string]any, rel string, members []any) []payload {
	var out []payload
	add := func(name string, v any) {
		if raw, err := marshalStable(v); err == nil {
			out = append(out, payload{name: name, json: raw})
		}
	}
	for i, m := range members {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if v := b.instance(mm, rel, 0); v != nil {
			add(fmt.Sprintf("alternative:%d", i), v)
		}
	}
	// An object satisfying no alternative must be rejected. The key is
	// deliberately one no member declares.
	//
	// What makes this a rejection rather than an accidentally valid instance
	// is that every alternative in this corpus carries at least one required
	// property — directly, as the message_* and fulfillment_destination
	// members do, or through an allOf of a base that requires one, as every
	// ucp.json member does. It is not additionalProperties: false doing the
	// work; retail_location.json sets additionalProperties: true and is still
	// rejected, because it requires id and name. So a member that dropped its
	// required list would turn this payload into a valid instance and the
	// name into a lie. The harness would stay correct either way — it
	// compares verdicts, not names — but the case would quietly stop
	// exercising the rejection path it exists for.
	add("matches-no-alternative", map[string]any{"__no_such_property__": "x"})
	return out
}

// objectMutations is the original strategy, unchanged apart from taking the
// base instance from its caller: mutations now decides the root's shape
// before building anything.
func (b *builder) objectMutations(schema map[string]any, rel string, base map[string]any) []payload {
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
