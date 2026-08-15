package emit

import (
	"fmt"
	"math"
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

// compileConditional emits the guarded checks for a node carrying
// if/then. A node with `then` but no `if` is meaningless and a node with
// `else` is unsupported; both fail rather than being ignored.
func compileConditional(e *fileEmitter, c *constraintSet, typeName string, schema map[string]any, fields []structField) error {
	if _, hasElse := schema["else"]; hasElse {
		return fmt.Errorf("%s: schema declares else, which is not supported (phase 6)", typeName)
	}
	ifNode, hasIf := schema["if"].(map[string]any)
	thenNode, hasThen := schema["then"].(map[string]any)
	if !hasIf && !hasThen {
		return nil
	}
	if !hasIf || !hasThen {
		return fmt.Errorf("%s: schema declares one of if/then without the other (phase 6)", typeName)
	}

	cond, err := predicate(e, typeName, "v", ifNode, fields)
	if err != nil {
		return err
	}
	body, err := thenChecks(e, c, typeName, thenNode, fields, describe(ifNode))
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
	e.enforced.mark(schema, "if", "then")
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
func thenChecks(e *fileEmitter, c *constraintSet, typeName string, node map[string]any, fields []structField, when string) (string, error) {
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
			if err := compileConstraints(e, &body, typeName, name, "v."+f.goName,
				accessFor(f.goType, f.required), sub); err != nil {
				return "", err
			}
		}
	}

	c.loops = body.loops
	c.vars.WriteString(body.vars.String())
	return body.checks.String(), nil
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
