package emit

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
)

// Conditional subschema evaluation.
//
// The constraint compiler in constraints.go turns a schema node into a
// check that returns an error. Conditionals need the other thing: a
// schema node compiled into a boolean expression. `if` uses it as a
// guard, `contains` counts elements satisfying it, and `not` negates it.
//
// The supported subschema forms are deliberately narrow — a single
// property tested by const, enum or negated enum, plus presence-only
// `required`. Everything the corpus contains fits; anything else fails
// generation rather than being approximated, because a predicate that is
// subtly wrong mis-scopes the rule it guards instead of reporting a gap.

// predicateKeywords are the keywords predicate understands at the top
// level of a subschema. Anything else means a form we cannot compile.
var predicateKeywords = map[string]bool{
	"properties": true, "required": true,
	// Annotations carry no assertion and are ignored.
	"type": true, "title": true, "description": true,
}

// predicate compiles a subschema into a Go boolean expression over an
// object reached by recv. fields describes that object's struct.
func predicate(e *fileEmitter, typeName, recv string, node map[string]any, fields []structField) (string, error) {
	for k := range node {
		if !predicateKeywords[k] {
			return "", fmt.Errorf("%s: conditional subschema declares %q, which is not supported (phase 6)", typeName, k)
		}
	}

	byJSON := make(map[string]structField, len(fields))
	for _, f := range fields {
		byJSON[f.jsonName] = f
	}

	required := map[string]bool{}
	if raw, ok := node["required"].([]any); ok {
		for _, r := range raw {
			s, isString := r.(string)
			if !isString {
				return "", fmt.Errorf("%s: conditional required entry is not a string", typeName)
			}
			required[s] = true
		}
	}

	props, _ := node["properties"].(map[string]any)
	if len(props) > 1 {
		names := make([]string, 0, len(props))
		for n := range props {
			names = append(names, n)
		}
		sort.Strings(names)
		return "", fmt.Errorf("%s: conditional subschema tests %d properties (%s); only a single-property discriminator is supported (phase 6)",
			typeName, len(props), strings.Join(names, ", "))
	}

	var terms []string

	for name, raw := range props {
		sub, isObj := raw.(map[string]any)
		if !isObj {
			return "", fmt.Errorf("%s: conditional subschema property %q is not an object", typeName, name)
		}
		f, known := byJSON[name]
		if !known {
			return "", fmt.Errorf("%s: conditional tests property %q, which this type does not declare (phase 6)", typeName, name)
		}
		// `properties` constrains a property only when it is there: a
		// subschema with no `required` also matches a value that omits the
		// property entirely. Compiling that to a present-and-matching test
		// would make the guard fire on fewer values than the schema's
		// condition covers, silently under-applying the rule it guards. The
		// property has to be present by construction — pinned by this
		// subschema's own `required` or by the outer schema — for the test
		// to mean what the schema means.
		if !required[name] && !f.required {
			return "", fmt.Errorf("%s: conditional tests property %q without requiring it; a properties test with no required also matches a value where %q is absent, which is not modeled (phase 6)", typeName, name, name)
		}
		term, needsPresence, err := valueTest(typeName, recv, f, sub)
		if err != nil {
			return "", err
		}
		if needsPresence {
			// The zero value satisfies a negated test, so a value the
			// decoder never saw would match. Only a decoded value carries
			// the record that distinguishes them.
			if recv != "v" {
				return "", fmt.Errorf("%s: conditional on %q needs the presence record, which is only reachable on the receiver (phase 6)", typeName, name)
			}
			if !f.required {
				return "", fmt.Errorf("%s: conditional on optional property %q needs a presence record that is not tracked for it (phase 6)", typeName, name)
			}
			terms = append(terms, fmt.Sprintf("%s.present != nil", recv), fmt.Sprintf("%s.present[%q]", recv, name))
		}
		terms = append(terms, term)
		// A positive value test cannot hold unless the property was set, so
		// its `required` is discharged by the test itself.
		delete(required, name)
	}

	names := make([]string, 0, len(required))
	for n := range required {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		f, known := byJSON[name]
		if !known {
			return "", fmt.Errorf("%s: conditional requires property %q, which this type does not declare (phase 6)", typeName, name)
		}
		switch accessFor(f.goType, f.required) {
		case accessPointer, accessNilable:
			// Exact: a decoded absent field is nil, a decoded present one is
			// not, and a hand-built one is nil until the caller sets it.
			terms = append(terms, fmt.Sprintf("%s.%s != nil", recv, f.goName))
		default:
			// A required non-pointer field is present by construction, so
			// the requirement is already discharged by the schema.
			continue
		}
	}

	if len(terms) == 0 {
		return "", fmt.Errorf("%s: conditional subschema asserts nothing (phase 6)", typeName)
	}
	return strings.Join(terms, " && "), nil
}

// valueTest compiles one property's value assertion. needsPresence
// reports that the test is satisfied by the Go zero value and so must be
// gated on the decoder's presence record.
func valueTest(typeName, recv string, f structField, sub map[string]any) (expr string, needsPresence bool, err error) {
	access := accessFor(f.goType, f.required)
	value := recv + "." + f.goName
	prefix := ""
	if access == accessPointer {
		prefix = value + " != nil && "
		value = "*" + value
	}

	if raw, ok := sub["const"]; ok {
		if len(sub) != 1 {
			return "", false, fmt.Errorf("%s: conditional on %q combines const with other keywords (phase 6)", typeName, f.jsonName)
		}
		lit, err := conditionalLiteral(typeName, f, raw)
		if err != nil {
			return "", false, err
		}
		return prefix + value + " == " + lit, false, nil
	}

	if raw, ok := sub["enum"].([]any); ok {
		if len(sub) != 1 {
			return "", false, fmt.Errorf("%s: conditional on %q combines enum with other keywords (phase 6)", typeName, f.jsonName)
		}
		return enumTest(typeName, f, prefix, value, raw, false)
	}

	if raw, ok := sub["not"].(map[string]any); ok {
		if len(sub) != 1 {
			return "", false, fmt.Errorf("%s: conditional on %q combines not with other keywords (phase 6)", typeName, f.jsonName)
		}
		members, ok := raw["enum"].([]any)
		if !ok || len(raw) != 1 {
			return "", false, fmt.Errorf("%s: conditional on %q negates something other than an enum, which is not supported (phase 6)", typeName, f.jsonName)
		}
		expr, _, err := enumTest(typeName, f, prefix, value, members, true)
		// The Go zero value is outside any excluded set, so an unset field
		// satisfies this test and the presence record must gate it.
		return expr, true, err
	}

	kws := make([]string, 0, len(sub))
	for k := range sub {
		kws = append(kws, k)
	}
	sort.Strings(kws)
	return "", false, fmt.Errorf("%s: conditional on %q tests %v, which is not a supported discriminator (phase 6)",
		typeName, f.jsonName, kws)
}

// enumTest builds a disjunction of equality tests, or a conjunction of
// inequalities when negated.
func enumTest(typeName string, f structField, prefix, value string, members []any, negated bool) (string, bool, error) {
	if len(members) == 0 {
		return "", false, fmt.Errorf("%s: conditional on %q has an empty enum (phase 6)", typeName, f.jsonName)
	}
	op, join := " == ", " || "
	if negated {
		op, join = " != ", " && "
	}
	parts := make([]string, 0, len(members))
	for _, m := range members {
		lit, err := conditionalLiteral(typeName, f, m)
		if err != nil {
			return "", false, err
		}
		parts = append(parts, value+op+lit)
	}
	expr := strings.Join(parts, join)
	if len(parts) > 1 && !negated {
		expr = "(" + expr + ")"
	}
	return prefix + expr, negated, nil
}

// conditionalLiteral renders a JSON scalar as a Go literal comparable
// against the field's type. A discriminator is always a scalar; anything
// else would need deep equality, which no corpus shape asks for.
func conditionalLiteral(typeName string, f structField, v any) (string, error) {
	base := strings.TrimPrefix(f.goType, "*")
	switch t := v.(type) {
	case string:
		if base != "string" {
			return "", fmt.Errorf("%s: conditional compares %q (Go type %s) against a string (phase 6)", typeName, f.jsonName, f.goType)
		}
		return fmt.Sprintf("%q", t), nil
	case bool:
		if base != "bool" {
			return "", fmt.Errorf("%s: conditional compares %q (Go type %s) against a bool (phase 6)", typeName, f.jsonName, f.goType)
		}
		return fmt.Sprintf("%t", t), nil
	case float64:
		switch base {
		case "int64":
			// An integer field is an int64, so the literal must be one too:
			// a fractional value has no integer form and the comparison
			// would fail to compile rather than fail generation.
			if t != math.Trunc(t) {
				return "", fmt.Errorf("%s: conditional compares %q (Go type %s) against the fractional number %v (phase 6)", typeName, f.jsonName, f.goType, t)
			}
			// The range is stated as a float64 bound, so it has to be a
			// float64-exact one: 2^63-1 is not representable and would round
			// up to 2^63, letting through the one value int64 cannot hold.
			if t < math.MinInt64 || t >= math.MaxInt64+1 {
				return "", fmt.Errorf("%s: conditional compares %q (Go type %s) against %v, which does not fit in an int64 (phase 6)", typeName, f.jsonName, f.goType, t)
			}
			return fmt.Sprintf("%d", int64(t)), nil
		case "float64":
			return formatNumber(t), nil
		}
		return "", fmt.Errorf("%s: conditional compares %q (Go type %s) against a number (phase 6)", typeName, f.jsonName, f.goType)
	}
	return "", fmt.Errorf("%s: conditional compares %q against an unsupported literal %T (phase 6)", typeName, f.jsonName, v)
}

// compileConditional emits the guarded checks for every conditional rule
// this schema carries, both on the node itself and in its allOf residual.
// The preprocessor leaves conditional branches in the allOf deliberately —
// they are rules, not fields, so there is nothing to fold into the parent —
// which means a compiler that reads only the node misses total.json's two
// amount rules entirely. Every branch is compiled, not just the first or
// the last: an upstream implementation that collapsed them kept one rule
// and silently dropped the other, so discounts stopped having to be
// negative.
func compileConditional(e *fileEmitter, c *constraintSet, typeName string, schema map[string]any, fields []structField) error {
	if err := compileOneConditional(e, c, typeName, schema, schema, fields); err != nil {
		return err
	}
	branches, _ := schema["allOf"].([]any)
	for i, b := range branches {
		bm, isObj := b.(map[string]any)
		if !isObj {
			continue
		}
		if err := compileOneConditional(e, c, typeName, schema, bm, fields); err != nil {
			return fmt.Errorf("allOf branch %d: %w", i, err)
		}
	}
	return nil
}

// compileOneConditional emits the guarded checks for a single node
// carrying if/then. A node with `then` but no `if` is meaningless and a
// node with `else` is unsupported; both fail rather than being ignored.
//
// node is the node that declares the conditional, which is the branch
// rather than the parent when the rule came out of an allOf residual. The
// enforced-keyword ledger is keyed on the identity of the map that holds
// the keyword, and unenforcedKeywords scans residual branches by their own
// identity, so marking the parent instead would leave the branch's if/then
// reported as an unfilled coverage gap it no longer is.
//
// owner is the object schema whose properties the rule talks about, which
// for a residual branch is not node: a branch states the rule and the
// parent states the property declarations the rule's consequent tightens.
func compileOneConditional(e *fileEmitter, c *constraintSet, typeName string, owner, node map[string]any, fields []structField) error {
	if _, hasElse := node["else"]; hasElse {
		return fmt.Errorf("%s: schema declares else, which is not supported (phase 6)", typeName)
	}
	ifNode, hasIf := node["if"].(map[string]any)
	thenNode, hasThen := node["then"].(map[string]any)
	if !hasIf && !hasThen {
		return nil
	}
	if !hasIf || !hasThen {
		return fmt.Errorf("%s: schema declares one of if/then without the other (phase 6)", typeName)
	}

	// A request variant is a projection of its response schema: the
	// preprocessor drops every property marked ucp_request:omit and keeps
	// the rules, so total_create_request.json arrives carrying total.json's
	// two amount rules over an empty property set. A rule naming properties
	// this type does not declare has nothing in the Go struct to bind to,
	// and no rewriting would recover it — the fields it talks about are not
	// in this shape. So it is neither approximated nor dropped in silence:
	// no check is emitted, if/then go unmarked, and the doc comment and
	// MANIFEST.json go on reporting them as a coverage gap, which is the
	// same accounting every other unenforceable keyword gets. Every failure
	// that IS about this compiler's reach still fails generation below.
	if !bindsToFields(ifNode, thenNode, fields) {
		return nil
	}

	cond, err := predicate(e, typeName, "v", ifNode, fields)
	if err != nil {
		return err
	}
	body, err := thenChecks(e, c, typeName, owner, thenNode, fields, describe(ifNode))
	if err != nil {
		return err
	}
	// An empty consequent is not a dropped one: a `then` that only repeats
	// what the schema already requires outright is discharged by the
	// unconditional check, so the rule is enforced with nothing to emit.
	if body != "" {
		e.usesErrors = true
		fmt.Fprintf(&c.checks, "if %s {\n%s}\n", cond, body)
	}
	e.enforced.mark(node, "if", "then")
	return nil
}

// thenKeywords are the keywords a consequent may carry. The list is the
// assertions this compiler can place under a guard plus the annotations
// that assert nothing; anything else fails rather than being ignored,
// because a dropped consequent leaves the rule looking enforced.
var thenKeywords = map[string]bool{
	"required": true, "properties": true,
	"type": true, "title": true, "description": true,
}

// thenChecks compiles the consequent. Its constraints are ordinary
// constraints — only the guard around them is new — so property value
// rules go through the existing compiler unchanged.
//
// The statements come back as a string for the caller to wrap in the
// guard, but everything else the constraint compiler produces belongs to
// the enclosing type and is merged into c: a compiled pattern is declared
// at package level, and loop variables are numbered across one Validate
// body so a nested loop never shadows its parent. Dropping either would
// emit code referring to a variable that was never declared. The set's
// other two builders need no merging, because compileConstraints never
// writes to them — recursive Validate calls come from compileNested and
// required-property checks from compilePresence, both driven by
// renderStruct.
func thenChecks(e *fileEmitter, c *constraintSet, typeName string, owner, node map[string]any, fields []structField, when string) (string, error) {
	for k := range node {
		if !thenKeywords[k] {
			return "", fmt.Errorf("%s: conditional then declares %q, which is not supported (phase 6)", typeName, k)
		}
	}

	byJSON := make(map[string]structField, len(fields))
	for _, f := range fields {
		byJSON[f.jsonName] = f
	}

	var body constraintSet
	body.loops = c.loops

	if raw, ok := node["required"].([]any); ok {
		names := make([]string, 0, len(raw))
		for _, r := range raw {
			s, isString := r.(string)
			if !isString {
				return "", fmt.Errorf("%s: conditional then required entry is not a string", typeName)
			}
			names = append(names, s)
		}
		sort.Strings(names)
		for _, name := range names {
			f, known := byJSON[name]
			if !known {
				return "", fmt.Errorf("%s: conditional then requires property %q, which this type does not declare (phase 6)", typeName, name)
			}
			switch accessFor(f.goType, f.required) {
			case accessPointer, accessNilable:
				e.usesErrors = true
				fmt.Fprintf(&body.checks, "if v.%s == nil {\nreturn errors.New(%q)\n}\n",
					f.goName, fmt.Sprintf("%s: required property is missing when %s", name, when))
			default:
				// Required by the schema outright: the unconditional
				// presence check already covers it.
			}
		}
	}

	if props, ok := node["properties"].(map[string]any); ok {
		names := make([]string, 0, len(props))
		for n := range props {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			sub, isObj := props[name].(map[string]any)
			if !isObj {
				return "", fmt.Errorf("%s: conditional then property %q is not an object", typeName, name)
			}
			f, known := byJSON[name]
			if !known {
				return "", fmt.Errorf("%s: conditional then constrains property %q, which this type does not declare (phase 6)", typeName, name)
			}
			// A consequent restates only the assertion it tightens —
			// total.json's is a bare {"exclusiveMaximum": 0} — because the
			// property's type was already fixed where the property was
			// declared. Reading the shape off the consequent alone finds
			// none and rejects every keyword on it as inapplicable, so the
			// shape comes from the declaration when the consequent is silent.
			kind := shapeOf(sub)
			if kind == shapeUnknown {
				kind = declaredShape(e, owner, name)
			}
			if err := compileConstraintsAs(e, &body, typeName, name, "v."+f.goName,
				accessFor(f.goType, f.required), sub, kind); err != nil {
				return "", err
			}
		}
	}

	c.loops = body.loops
	c.vars.WriteString(body.vars.String())
	return body.checks.String(), nil
}

// bindsToFields reports whether every property the rule names — tested by
// the condition or constrained by the consequent — exists as a field on the
// Go struct. It answers a different question from the errors below: not
// "can this compiler express the rule" but "is the rule even about this
// type", which is what a ucp_request projection breaks.
func bindsToFields(ifNode, thenNode map[string]any, fields []structField) bool {
	declared := make(map[string]bool, len(fields))
	for _, f := range fields {
		declared[f.jsonName] = true
	}
	for _, node := range []map[string]any{ifNode, thenNode} {
		props, _ := node["properties"].(map[string]any)
		for name := range props {
			if !declared[name] {
				return false
			}
		}
		required, _ := node["required"].([]any)
		for _, r := range required {
			if name, isString := r.(string); isString && !declared[name] {
				return false
			}
		}
	}
	return true
}

// refChainLimit bounds the $ref walk below. A cycle among $refs would
// otherwise spin; the emitter has its own cycle handling for the type
// graph, and this lookup is not the place to duplicate it.
const refChainLimit = 8

// declaredShape reports the shape of the value owner's property `name`
// holds, following $refs until a node states a type. It returns
// shapeUnknown when nothing does, which leaves compileInto to reject the
// consequent's keywords by name rather than guessing at a shape.
func declaredShape(e *fileEmitter, owner map[string]any, name string) shape {
	props, _ := owner["properties"].(map[string]any)
	node, isObj := props[name].(map[string]any)
	from := e.rel
	for hops := 0; isObj && hops < refChainLimit; hops++ {
		if kind := shapeOf(node); kind != shapeUnknown {
			return kind
		}
		ref, isRef := node["$ref"].(string)
		if !isRef {
			return shapeUnknown
		}
		node, from, isObj = e.refTarget(from, ref)
	}
	return shapeUnknown
}

// refTarget resolves one $ref, written relative to the schema at `from`, to
// the node it names and the schema that node lives in. Unlike ResolveRef,
// which answers which Go type a reference becomes, this answers what the
// referenced schema SAYS — the two are different questions, and only the
// second can tell a consequent that the value it constrains is an integer.
func (e *fileEmitter) refTarget(from, ref string) (node map[string]any, at string, ok bool) {
	filePart, fragment, _ := strings.Cut(ref, "#")
	doc := e.doc
	at = from
	if filePart != "" {
		at = path.Join(path.Dir(from), filePart)
		doc, ok = e.corpus[at]
		if !ok {
			return nil, "", false
		}
	}
	if fragment == "" {
		return doc, at, doc != nil
	}
	defName, hasPrefix := strings.CutPrefix(fragment, defsFragmentPrefix)
	if !hasPrefix || strings.Contains(defName, "/") {
		return nil, "", false
	}
	defs, _ := doc["$defs"].(map[string]any)
	node, ok = defs[defName].(map[string]any)
	return node, at, ok
}

// describe renders an if-condition in prose for an error message, so a
// violation says which rule fired rather than only which property is bad.
func describe(ifNode map[string]any) string {
	props, _ := ifNode["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		sub, _ := props[name].(map[string]any)
		if raw, ok := sub["const"]; ok {
			return fmt.Sprintf("%s is %s", name, prose([]any{raw}))
		}
		if members, ok := sub["enum"].([]any); ok {
			return fmt.Sprintf("%s is one of %s", name, prose(members))
		}
		if not, ok := sub["not"].(map[string]any); ok {
			if members, ok := not["enum"].([]any); ok {
				return fmt.Sprintf("%s is not one of %s", name, prose(members))
			}
		}
	}
	return "the schema's condition holds"
}

// prose renders enum members as a comma-separated list for an error
// message. Go's own %v on a []any would print `[discount items_discount]`,
// which reads as Go syntax rather than as the values a caller sees in
// their JSON.
func prose(members []any) string {
	parts := make([]string, 0, len(members))
	for _, m := range members {
		if s, isString := m.(string); isString {
			parts = append(parts, fmt.Sprintf("%q", s))
			continue
		}
		parts = append(parts, fmt.Sprintf("%v", m))
	}
	return strings.Join(parts, ", ")
}

// compileContains emits the counting loop for contains/minContains/
// maxContains: how many elements satisfy the subschema, and whether that
// count falls in the permitted range.
//
// This is the other consumer of predicate. `contains` is a rule about the
// array rather than about any one element, so it cannot ride on the
// element type's own Validate — an element that fails the contains
// subschema is not invalid, it simply does not count.
func compileContains(e *fileEmitter, c *constraintSet, t target, node map[string]any) error {
	contains, hasContains := node["contains"].(map[string]any)
	_, hasMin := node["minContains"]
	_, hasMax := node["maxContains"]
	if !hasContains {
		if hasMin || hasMax {
			return fmt.Errorf("%s: %s declares minContains/maxContains with no contains, which asserts nothing (phase 6)",
				t.typeName, subjectOf(t.label))
		}
		return nil
	}

	// JSON Schema defaults minContains to 1: contains on its own means the
	// array must hold at least one match.
	min, max := 1, -1
	if hasMin {
		n, err := integerBound(t, node, "minContains")
		if err != nil {
			return err
		}
		min = n
	}
	if hasMax {
		n, err := integerBound(t, node, "maxContains")
		if err != nil {
			return err
		}
		max = n
	}
	if max >= 0 && max < min {
		return fmt.Errorf("%s: %s has maxContains %d below minContains %d, which no array can satisfy",
			t.typeName, subjectOf(t.label), max, min)
	}

	items, _ := node["items"].(map[string]any)
	if items == nil {
		return fmt.Errorf("%s: %s declares contains on an array with no items schema (phase 6)",
			t.typeName, subjectOf(t.label))
	}
	elemType, err := elementTypeName(e, t, node)
	if err != nil {
		return err
	}
	fields, err := fieldsFor(e, elemType, items)
	if err != nil {
		return err
	}

	elem, count := c.loopVar(), c.loopVar()
	// The element is a loop variable, not the receiver, so a predicate
	// needing the decoder's presence record cannot be built here — predicate
	// rejects that rather than silently testing a zero value.
	cond, err := predicate(e, elemType, elem, contains, fields)
	if err != nil {
		return err
	}

	guard, value := t.resolve()
	open, closed := guardBlock(guard)
	e.usesErrors = true
	c.checks.WriteString(open)
	// A nil array was never set. For a value built in Go rather than
	// decoded that is unknowable rather than empty, so the same rule that
	// skips presence checks applies — otherwise every hand-built Checkout
	// fails on a totals list the caller has not filled in yet. A decoded
	// [] is non-nil, so it is still counted and still rejected.
	fmt.Fprintf(&c.checks, "if %s != nil {\n", value)
	fmt.Fprintf(&c.checks, "%s := 0\n", count)
	fmt.Fprintf(&c.checks, "for _, %s := range %s {\n", elem, value)
	fmt.Fprintf(&c.checks, "if %s {\n%s++\n}\n}\n", cond, count)
	fmt.Fprintf(&c.checks, "if %s < %d {\nreturn errors.New(%q)\n}\n",
		count, min, fmt.Sprintf("%s: has fewer than minContains %d matching items", t.label, min))
	if max >= 0 {
		fmt.Fprintf(&c.checks, "if %s > %d {\nreturn errors.New(%q)\n}\n",
			count, max, fmt.Sprintf("%s: has more than maxContains %d matching items", t.label, max))
	}
	c.checks.WriteString("}\n")
	c.checks.WriteString(closed)

	e.enforced.mark(node, "contains")
	if hasMin {
		e.enforced.mark(node, "minContains")
	}
	if hasMax {
		e.enforced.mark(node, "maxContains")
	}
	return nil
}

// elementTypeName reports the Go type goTypeExpr gives an array's element,
// stripped of its slice and pointer decoration. fieldsFor needs it both to
// name the type in an error and to namespace anything nested under it, and
// reading it back from goTypeExpr is what keeps that name in step with the
// type actually emitted.
func elementTypeName(e *fileEmitter, t target, node map[string]any) (string, error) {
	typ, err := e.goTypeExpr(node, t.label)
	if err != nil {
		return "", err
	}
	base := strings.TrimPrefix(strings.TrimPrefix(typ, "[]"), "*")
	if base == "" || strings.HasPrefix(base, "[]") || strings.Contains(base, "map[") {
		return "", fmt.Errorf("%s: %s has contains over element type %q, which is not an object (phase 6)",
			t.typeName, subjectOf(t.label), typ)
	}
	return base, nil
}
