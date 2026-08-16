package emit

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// accessKind describes how a checked value is reached from the Validate
// receiver: directly, through a pointer that may be nil, or as the loop
// variable of a range over a collection.
type accessKind int

const (
	// accessValue is a value that always exists — a required field, or the
	// receiver of a named alias.
	accessValue accessKind = iota
	// accessPointer is an optional field carried as a pointer: the check
	// must be nil-guarded and the value dereferenced.
	accessPointer
	// accessNilable is an optional field whose Go type is already nilable —
	// a slice, map or json.RawMessage, which renderStruct leaves unpointered.
	// It still needs the nil guard: an absent array constrains nothing, so
	// checking minItems against a nil slice would reject what the schema
	// permits.
	accessNilable
	// accessElement is a loop variable, which exists like a value but names
	// a collection member rather than a field.
	accessElement
)

// accessFor reports how a struct field's value is reached, given the Go
// type renderStruct settled on for it. It reads that type rather than
// re-deriving it from the schema: a check that dereferences a non-pointer,
// or fails to dereference a pointer, does not compile.
func accessFor(goType string, required bool) accessKind {
	switch {
	case required:
		// A required field is present by construction — except for the
		// ["x","null"] shape, which goTypeExpr renders as a pointer whether
		// or not the property is required.
		if strings.HasPrefix(goType, "*") {
			return accessPointer
		}
		return accessValue
	case strings.HasPrefix(goType, "*"):
		return accessPointer
	default:
		// An optional field that is not a pointer is one of the nilable
		// types fieldsFor leaves unpointered.
		return accessNilable
	}
}

// resolve returns the guard a check must be prefixed with and the
// expression that reaches the value itself.
func (a accessKind) resolve(expr string) (guard, value string) {
	switch a {
	case accessPointer:
		return expr + " != nil && ", "*" + expr
	case accessNilable:
		return expr + " != nil && ", expr
	}
	return "", expr
}

// constraintSet accumulates what one type's constraints generate: the
// package-level variables (compiled patterns) and the check statements that
// make up its Validate body.
type constraintSet struct {
	vars strings.Builder
	// presence holds the required-property checks, kept apart from checks so
	// they can be emitted first: an absent property is a more fundamental
	// failure than a bad value.
	presence strings.Builder
	checks   strings.Builder
	// nested holds the recursive Validate calls, emitted after this type's
	// own checks so an error names the shallowest thing that is wrong.
	nested strings.Builder
	// loops counts the range loops emitted so far, so nested ones can take
	// distinct variable names.
	loops int
}

// check writes one statement of the form
//
//	if <guard><cond> {
//		return errors.New(<msg>)
//	}
func (c *constraintSet) check(guard, cond, msg string) {
	fmt.Fprintf(&c.checks, "\tif %s%s {\n\t\treturn errors.New(%q)\n\t}\n", guard, cond, msg)
}

// enforcedKeywords records which validation keywords a generated check
// already covers, per schema node.
//
// Nodes are identified by the identity of their backing map rather than by
// path: the emitter reaches the same node through several different naming
// conventions (a bare property name in one place, a prefixed field path in
// another), and a keyword reported as an unchecked coverage gap while a
// check for it exists would make the generated doc comments and
// MANIFEST.json lie. The schema tree is the emitter's input and stays
// reachable for the whole of one file's emission, so a map's address
// identifies it unambiguously for as long as the record is consulted.
type enforcedKeywords map[uintptr]map[string]bool

func nodeID(node map[string]any) uintptr { return reflect.ValueOf(node).Pointer() }

func (k enforcedKeywords) mark(node map[string]any, keywords ...string) {
	id := nodeID(node)
	if k[id] == nil {
		k[id] = map[string]bool{}
	}
	for _, kw := range keywords {
		k[id][kw] = true
	}
}

func (k enforcedKeywords) has(node map[string]any, keyword string) bool {
	return k[nodeID(node)][keyword]
}

// target names one checked value: where it lives, what an error calls it,
// and how a generated variable derived from it is named. Recursion into
// array elements and map keys derives a new target from its parent, which
// is what keeps names unique and messages readable.
type target struct {
	typeName string // enclosing Go type, for generated variable names
	varStem  string // unique-within-type stem for generated variables
	label    string // what an error message calls this value
	expr     string // Go expression reaching it
	access   accessKind
}

// derive returns a target for something reached from this one — an element
// of it, or one of its keys.
func (t target) derive(suffix, varSuffix, expr string, access accessKind) target {
	return target{
		typeName: t.typeName,
		varStem:  t.varStem + varSuffix,
		label:    t.label + " " + suffix,
		expr:     expr,
		access:   access,
	}
}

// compileConstraints emits the checks one schema node implies against the
// Go expression that reaches its value.
//
// typeName names the enclosing Go type and jsonName the JSON property, or
// "" when the node is a named type in its own right rather than a property.
// Together they determine both the error messages a violation reports and
// the names of any generated package-level variables, so the two callers —
// struct fields and scalar aliases — produce distinct, traceable output
// from one implementation.
func compileConstraints(e *fileEmitter, c *constraintSet, typeName, jsonName, expr string, access accessKind, node map[string]any) error {
	return compileConstraintsAs(e, c, typeName, jsonName, expr, access, node, shapeOf(node))
}

// compileConstraintsAs is compileConstraints with the value's shape supplied
// by the caller rather than read off the node. A node that only tightens a
// value typed somewhere else — a conditional's consequent is the one such
// caller — declares no `type` of its own, so reading the shape from it would
// find none and reject every keyword on it as inapplicable.
func compileConstraintsAs(e *fileEmitter, c *constraintSet, typeName, jsonName, expr string, access accessKind, node map[string]any, kind shape) error {
	t := target{typeName: typeName, varStem: typeName, label: typeName, expr: expr, access: access}
	if jsonName != "" {
		t.varStem = typeName + "_" + GoName(jsonName)
		t.label = jsonName
	}
	return compileInto(e, c, t, node, kind)
}

// shape is the family of Go value a schema node describes, which decides
// which constraint keywords can be checked against it.
type shape string

const (
	shapeString  shape = "string"
	shapeInteger shape = "integer"
	shapeNumber  shape = "number"
	shapeBoolean shape = "boolean"
	shapeArray   shape = "array"
	shapeMap     shape = "map"
	shapeStruct  shape = "struct"
	shapeUnknown shape = ""
)

// shapeOf mirrors goTypeExpr's reasoning about what Go value a node
// becomes. It must stay in step with it: a check emitted against the wrong
// shape does not compile, and one skipped because the shape looked unknown
// silently weakens Validate.
func shapeOf(node map[string]any) shape {
	switch t := node["type"].(type) {
	case string:
		switch t {
		case "string", "integer", "number", "boolean", "array":
			return shape(t)
		case "object":
			// An object with named properties is a struct; anything else is a
			// map, whose length and keys are checkable. An empty properties
			// map names none, and calling it a struct here is not merely
			// cosmetic: compileInto drops a struct's own keywords on the
			// floor because renderStruct is supposed to carry them, and an
			// empty properties map is exactly the shape renderStruct no
			// longer sees.
			if props, _ := node["properties"].(map[string]any); len(props) > 0 {
				return shapeStruct
			}
			return shapeMap
		}
		return shapeUnknown
	case nil:
		// JSON Schema implies the type from the keywords present, and
		// goTypeExpr does the same, so const/enum values fix the shape.
		if v, ok := node["const"]; ok {
			return shapeOfValue(v)
		}
		if enum, ok := node["enum"].([]any); ok && len(enum) > 0 {
			return shapeOfValue(enum[0])
		}
		if _, ok := node["properties"]; ok {
			return shapeStruct
		}
		if _, ok := node["additionalProperties"].(map[string]any); ok {
			return shapeMap
		}
		if _, ok := node["items"]; ok {
			return shapeArray
		}
		return shapeUnknown
	default:
		// A multi-type node ("x" or null) has no single Go shape here.
		return shapeUnknown
	}
}

func shapeOfValue(v any) shape {
	switch v.(type) {
	case string:
		return shapeString
	case bool:
		return shapeBoolean
	case float64:
		return shapeNumber
	}
	return shapeUnknown
}

// checkableKeywords are the validation keywords compileInto knows how to
// turn into a generated check, grouped by the shape they apply to. A
// keyword present on a node of the wrong shape fails generation rather than
// being skipped: a constraint we cannot express must never be silently
// dropped.
var checkableKeywords = map[shape][]string{
	shapeString:  {"maxLength", "minLength", "pattern", "enum", "const"},
	shapeInteger: {"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "enum", "const"},
	shapeNumber:  {"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "enum", "const"},
	shapeBoolean: {"enum", "const"},
	shapeArray:   {"minItems", "maxItems", "uniqueItems"},
	shapeMap:     {"minProperties", "maxProperties", "propertyNames"},
}

// allCheckable is every keyword any shape can check, used to decide whether
// a node carries constraints at all.
var allCheckable = func() map[string]bool {
	all := map[string]bool{}
	for _, kws := range checkableKeywords {
		for _, kw := range kws {
			all[kw] = true
		}
	}
	return all
}()

// compileInto emits the checks for one node against one target. kind is the
// shape of the value the node constrains, which is normally shapeOf(node)
// but is supplied by the caller when the node's type was fixed elsewhere.
func compileInto(e *fileEmitter, c *constraintSet, t target, node map[string]any, kind shape) error {
	declared := declaredCheckable(node)

	// An array's or map's own keywords may be absent while its elements or
	// keys carry constraints, so those recursions run regardless.
	switch kind {
	case shapeArray:
		if err := compileArray(e, c, t, node); err != nil {
			return err
		}
	case shapeMap:
		if err := compileMap(e, c, t, node); err != nil {
			return err
		}
	}

	if len(declared) == 0 {
		return nil
	}

	// An inline object promoted to its own named type carries its
	// object-level constraints there, via compileObjectSelf, which needs the
	// field list this call does not have. Leaving them here would double
	// them up; the keywords stay reported until that type is rendered.
	if kind == shapeStruct {
		return nil
	}

	applicable := map[string]bool{}
	for _, kw := range checkableKeywords[kind] {
		applicable[kw] = true
	}
	for _, kw := range declared {
		if !applicable[kw] {
			// Keeps the emitter honest: a constraint on a shape whose check we
			// cannot express fails loudly instead of silently emitting an
			// unconstrained field. The node's own type names the shape when it
			// has one; a node whose shape came from the caller has none, so
			// the shape itself is named rather than printing a bare <nil>
			// that tells the reader nothing about the value.
			declaredAs := node["type"]
			if declaredAs == nil {
				declaredAs = string(kind)
			}
			return fmt.Errorf("%s: %s declares %q but has unsupported type %v for it (phase 4)",
				t.typeName, subjectOf(t.label), kw, declaredAs)
		}
	}

	guard, value := t.resolve()

	for _, kw := range declared {
		switch kw {
		case "maxLength", "minLength":
			if err := compileLength(e, c, t, node, kw, guard, value); err != nil {
				return err
			}
		case "pattern":
			if err := compilePattern(e, c, t, node, guard, value); err != nil {
				return err
			}
		case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
			if err := compileBound(e, c, t, node, kw, kind, guard, value); err != nil {
				return err
			}
		case "multipleOf":
			if err := compileMultipleOf(e, c, t, node, guard, value); err != nil {
				return err
			}
		case "enum":
			if err := compileEnum(e, c, t, node, guard, value); err != nil {
				return err
			}
		case "const":
			if err := compileConst(e, c, t, node, guard, value); err != nil {
				return err
			}
		case "minItems", "maxItems", "minProperties", "maxProperties", "uniqueItems", "propertyNames":
			// Handled by compileArray/compileMap above, which need the node
			// rather than the resolved value expression.
			continue
		}
		e.enforced.mark(node, kw)
	}
	return nil
}

// resolve returns the nil guard and value expression for this target.
func (t target) resolve() (guard, value string) { return t.access.resolve(t.expr) }

// declaredCheckable returns, in a deterministic order, the checkable
// keywords a node declares.
func declaredCheckable(node map[string]any) []string {
	var out []string
	for kw := range node {
		if allCheckable[kw] {
			out = append(out, kw)
		}
	}
	sort.Strings(out)
	return out
}

func compileLength(e *fileEmitter, c *constraintSet, t target, node map[string]any, kw, guard, value string) error {
	n, err := integerBound(t, node, kw)
	if err != nil {
		return err
	}
	e.usesErrors, e.usesUtf8 = true, true
	// maxLength counts Unicode code points per JSON Schema, not bytes.
	op, wording := ">", "exceeds maxLength"
	if kw == "minLength" {
		op, wording = "<", "is shorter than minLength"
	}
	c.check(guard, fmt.Sprintf("utf8.RuneCountInString(%s) %s %d", value, op, n),
		fmt.Sprintf("%s: %s %d", t.label, wording, n))
	return nil
}

func compilePattern(e *fileEmitter, c *constraintSet, t target, node map[string]any, guard, value string) error {
	pattern, isString := node["pattern"].(string)
	if !isString {
		return fmt.Errorf("%s: %s pattern is %T, want a string", t.typeName, subjectOf(t.label), node["pattern"])
	}
	name, err := e.patternVar(c, t, pattern)
	if err != nil {
		return err
	}
	// JSON Schema pattern is an unanchored search, so MatchString (not a
	// full-string match) is intentional.
	c.check(guard, fmt.Sprintf("!%s().MatchString(%s)", name, value),
		fmt.Sprintf("%s: does not match pattern", t.label))
	return nil
}

// patternVar declares a package-level compiled pattern and returns its name.
func (e *fileEmitter) patternVar(c *constraintSet, t target, pattern string) (string, error) {
	// RE2 gate: fail generation loudly rather than emit a MustCompile that
	// would panic at runtime.
	if _, err := regexp.Compile(pattern); err != nil {
		return "", fmt.Errorf("%s: pattern %q for %s is not RE2-compatible: %v", t.typeName, pattern, subjectOf(t.label), err)
	}
	e.usesErrors, e.usesRegexp, e.usesSync = true, true, true
	name := "pattern_" + t.varStem
	fmt.Fprintf(&c.vars, "var %s = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(%q) })\n\n", name, pattern)
	return name, nil
}

// boundOps maps each bound keyword to the comparison that VIOLATES it.
var boundOps = map[string]string{
	"minimum": "<", "maximum": ">", "exclusiveMinimum": "<=", "exclusiveMaximum": ">=",
}

var boundWording = map[string]string{
	"minimum": "below minimum", "maximum": "above maximum",
	"exclusiveMinimum": "not above exclusiveMinimum", "exclusiveMaximum": "not below exclusiveMaximum",
}

func compileBound(e *fileEmitter, c *constraintSet, t target, node map[string]any, kw string, kind shape, guard, value string) error {
	raw, isNumber := node[kw].(float64)
	if !isNumber {
		return fmt.Errorf("%s: %s %s is %T, want a number", t.typeName, subjectOf(t.label), kw, node[kw])
	}
	// An integer field is an int64, so the literal must be one too: a
	// fractional bound has no integer form and would not compile.
	literal := formatNumber(raw)
	if kind == shapeInteger {
		if raw != math.Trunc(raw) {
			return fmt.Errorf("%s: %s %s %v is fractional but the field is an integer", t.typeName, subjectOf(t.label), kw, raw)
		}
		literal = fmt.Sprintf("%d", int64(raw))
	}
	e.usesErrors = true
	c.check(guard, fmt.Sprintf("%s %s %s", value, boundOps[kw], literal),
		fmt.Sprintf("%s: %s %s", t.label, boundWording[kw], literal))
	return nil
}

// formatNumber renders a JSON number as a Go literal that keeps its value.
func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return fmt.Sprintf("%d", int64(f))
	}
	return strings.TrimSuffix(fmt.Sprintf("%v", f), "\n")
}

func compileMultipleOf(e *fileEmitter, c *constraintSet, t target, node map[string]any, guard, value string) error {
	raw, isNumber := node["multipleOf"].(float64)
	if !isNumber {
		return fmt.Errorf("%s: %s multipleOf is %T, want a number", t.typeName, subjectOf(t.label), node["multipleOf"])
	}
	if raw <= 0 || raw != math.Trunc(raw) {
		// A fractional divisor needs floating-point remainder arithmetic,
		// where "is a multiple of" is not decidable without an epsilon the
		// spec does not define. The keyword stays reported as unenforced
		// rather than checked approximately.
		return fmt.Errorf("%s: %s multipleOf %v is not a positive integer; only integer divisors are checked", t.typeName, subjectOf(t.label), raw)
	}
	e.usesErrors = true
	c.check(guard, fmt.Sprintf("%s%%%d != 0", value, int64(raw)),
		fmt.Sprintf("%s: not a multiple of %d", t.label, int64(raw)))
	return nil
}

func compileEnum(e *fileEmitter, c *constraintSet, t target, node map[string]any, guard, value string) error {
	values, isArray := node["enum"].([]any)
	if !isArray || len(values) == 0 {
		return fmt.Errorf("%s: %s enum is %T, want a non-empty array", t.typeName, subjectOf(t.label), node["enum"])
	}
	// A conjunction of inequalities rather than a switch, so the check
	// composes with the nil guard an optional field needs.
	var conds []string
	for _, v := range values {
		lit, err := goLiteral(t, v)
		if err != nil {
			return err
		}
		conds = append(conds, fmt.Sprintf("%s != %s", value, lit))
	}
	e.usesErrors = true
	c.check(guard, strings.Join(conds, " && "),
		fmt.Sprintf("%s: not one of the permitted values", t.label))
	return nil
}

func compileConst(e *fileEmitter, c *constraintSet, t target, node map[string]any, guard, value string) error {
	lit, err := goLiteral(t, node["const"])
	if err != nil {
		return err
	}
	e.usesErrors = true
	c.check(guard, fmt.Sprintf("%s != %s", value, lit),
		fmt.Sprintf("%s: must be %s", t.label, lit))
	return nil
}

// goLiteral renders a JSON scalar as the Go literal it compares against.
// Objects and arrays are not modeled: comparing them needs deep equality,
// and no schema in the corpus uses one.
func goLiteral(t target, v any) (string, error) {
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("%q", x), nil
	case bool:
		return fmt.Sprintf("%t", x), nil
	case float64:
		return formatNumber(x), nil
	}
	return "", fmt.Errorf("%s: %s has a %T enum/const value, which is not a scalar (phase 5)", t.typeName, subjectOf(t.label), v)
}

// compileArray emits an array's own checks and recurses into its elements.
func compileArray(e *fileEmitter, c *constraintSet, t target, node map[string]any) error {
	guard, value := t.resolve()

	for _, kw := range []string{"minItems", "maxItems"} {
		if _, ok := node[kw]; !ok {
			continue
		}
		n, err := integerBound(t, node, kw)
		if err != nil {
			return err
		}
		e.usesErrors = true
		op, wording := "<", "has fewer than minItems"
		if kw == "maxItems" {
			op, wording = ">", "has more than maxItems"
		}
		c.check(guard, fmt.Sprintf("len(%s) %s %d", value, op, n),
			fmt.Sprintf("%s: %s %d", t.label, wording, n))
		e.enforced.mark(node, kw)
	}

	items, _ := node["items"].(map[string]any)

	if unique, ok := node["uniqueItems"].(bool); ok {
		if unique {
			e.usesErrors = true
			c.uniqueCheck(e, t, guard, value, scalarGoType(items))
		}
		e.enforced.mark(node, "uniqueItems")
	}

	if err := compileContains(e, c, t, node); err != nil {
		return err
	}

	// An element carrying its own constraints is only reachable through a
	// loop: a scalar element produces no named Go type to hang a Validate
	// on. Object elements are promoted to named types by goTypeExpr and
	// validate themselves.
	if items != nil && scalarGoType(items) != "" && len(declaredCheckable(items)) > 0 {
		elem := c.loopVar()
		sub := t.derive("item", "_Item", elem, accessElement)
		var body constraintSet
		body.loops = c.loops
		if err := compileInto(e, &body, sub, items, shapeOf(items)); err != nil {
			return err
		}
		c.loops = body.loops
		c.vars.WriteString(body.vars.String())
		open, closed := guardBlock(guard)
		c.checks.WriteString(open)
		fmt.Fprintf(&c.checks, "for _, %s := range %s {\n", elem, value)
		c.checks.WriteString(body.checks.String())
		c.checks.WriteString("}\n")
		c.checks.WriteString(closed)
	}
	return nil
}

// uniqueCheck emits a duplicate scan over a slice. A comparable element
// type is used as the map key directly; anything else is compared by its
// JSON encoding, which is what the spec means by equal — at the cost of one
// allocation per element.
func (c *constraintSet) uniqueCheck(e *fileEmitter, t target, guard, value, scalar string) {
	elem, seen := c.loopVar(), c.loopVar()
	msg := t.label + ": contains duplicate items"
	open, closed := guardBlock(guard)
	c.checks.WriteString(open)
	c.checks.WriteString("{\n")
	if scalar != "" {
		fmt.Fprintf(&c.checks, "%s := make(map[%s]bool, len(%s))\n", seen, scalar, value)
		fmt.Fprintf(&c.checks, "for _, %s := range %s {\n", elem, value)
		fmt.Fprintf(&c.checks, "if %s[%s] {\nreturn errors.New(%q)\n}\n", seen, elem, msg)
		fmt.Fprintf(&c.checks, "%s[%s] = true\n}\n", seen, elem)
	} else {
		e.imports["encoding/json"] = "json"
		key := c.loopVar()
		fmt.Fprintf(&c.checks, "%s := make(map[string]bool, len(%s))\n", seen, value)
		fmt.Fprintf(&c.checks, "for _, %s := range %s {\n", elem, value)
		fmt.Fprintf(&c.checks, "%s, err := json.Marshal(%s)\nif err != nil {\nreturn err\n}\n", key, elem)
		fmt.Fprintf(&c.checks, "if %s[string(%s)] {\nreturn errors.New(%q)\n}\n", seen, key, msg)
		fmt.Fprintf(&c.checks, "%s[string(%s)] = true\n}\n", seen, key)
	}
	c.checks.WriteString("}\n")
	c.checks.WriteString(closed)
}

// structField is what an object-level check needs to know about one
// property: what it is called on each side, whether it is always there,
// and the Go type fieldsFor settled on. The type is what tells a
// conditional predicate whether to dereference: `required` is only a
// proxy for it, and a false one for optional slices and maps, which
// fieldsFor leaves unpointered.
type structField struct {
	jsonName string
	goName   string
	goType   string
	required bool
}

// compileObjectSelf emits the checks an object schema places on itself
// rather than on any one property.
//
// A struct is not a map, so these cannot reuse compileMap: the property
// count is the number of fields actually set plus whatever extension keys
// landed in Extra, and the only names checkable at runtime are Extra's —
// the named ones are fixed by the schema and so are verified here, while
// the code is being generated.
func compileObjectSelf(e *fileEmitter, c *constraintSet, typeName string, schema map[string]any, fields []structField, open bool) error {
	// First, because this function returns early when the schema declares no
	// propertyNames. A conditional compiled at the end would run for only
	// some objects, and the ones it skipped would report the rule as
	// enforced while emitting nothing.
	if err := compileConditional(e, c, typeName, schema, fields); err != nil {
		return err
	}

	t := target{typeName: typeName, varStem: typeName, label: typeName, expr: "v", access: accessValue}

	for _, kw := range []string{"minProperties", "maxProperties"} {
		if _, ok := schema[kw]; !ok {
			continue
		}
		n, err := integerBound(t, schema, kw)
		if err != nil {
			return err
		}
		e.usesErrors = true
		op, wording := "<", "has fewer than minProperties"
		if kw == "maxProperties" {
			op, wording = ">", "has more than maxProperties"
		}
		// Required properties are present by construction, so they seed the
		// count without a runtime test.
		base := 0
		for _, f := range fields {
			if f.required {
				base++
			}
		}
		c.checks.WriteString("{\n")
		fmt.Fprintf(&c.checks, "n := %d\n", base)
		for _, f := range fields {
			if f.required {
				continue
			}
			// Every optional field is nilable: renderStruct gives a pointer to
			// anything that is not already a slice, map or json.RawMessage.
			fmt.Fprintf(&c.checks, "if v.%s != nil {\nn++\n}\n", f.goName)
		}
		if open {
			c.checks.WriteString("n += len(v.Extra)\n")
		}
		fmt.Fprintf(&c.checks, "if n %s %d {\nreturn errors.New(%q)\n}\n}\n",
			op, n, fmt.Sprintf("%s: %s %d", typeName, wording, n))
		e.enforced.mark(schema, kw)
	}

	names, ok := schema["propertyNames"].(map[string]any)
	if !ok {
		return nil
	}
	// The named properties are literals the schema author wrote, so they are
	// checkable now. Letting one through that violates the constraint would
	// leave the generated code quietly weaker than the schema, since the
	// runtime loop below only ever sees the keys the schema did not name.
	for _, f := range fields {
		if err := checkLiteralAgainst(names, f.jsonName); err != nil {
			return fmt.Errorf("%s: property %q violates the schema's own propertyNames: %w", typeName, f.jsonName, err)
		}
	}
	if !open {
		// A closed object has no other keys, so the check is fully discharged
		// by the verification above.
		e.enforced.mark(schema, "propertyNames")
		return nil
	}

	key := c.loopVar()
	sub := t.derive("property name", "_Key", key, accessElement)
	var body constraintSet
	body.loops = c.loops
	if err := compileStringChecks(e, &body, sub, names); err != nil {
		return err
	}
	c.loops = body.loops
	c.vars.WriteString(body.vars.String())
	if body.checks.Len() > 0 {
		fmt.Fprintf(&c.checks, "for %s := range v.Extra {\n", key)
		c.checks.WriteString(body.checks.String())
		c.checks.WriteString("}\n")
	}
	e.enforced.mark(schema, "propertyNames")
	return nil
}

// checkLiteralAgainst evaluates a string subschema against a known value,
// at generation time.
func checkLiteralAgainst(node map[string]any, value string) error {
	if pattern, ok := node["pattern"].(string); ok {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("pattern %q is not RE2-compatible: %v", pattern, err)
		}
		if !re.MatchString(value) {
			return fmt.Errorf("does not match pattern %q", pattern)
		}
	}
	if max, ok := node["maxLength"].(float64); ok && float64(utf8.RuneCountInString(value)) > max {
		return fmt.Errorf("exceeds maxLength %v", max)
	}
	if min, ok := node["minLength"].(float64); ok && float64(utf8.RuneCountInString(value)) < min {
		return fmt.Errorf("is shorter than minLength %v", min)
	}
	if values, ok := node["enum"].([]any); ok {
		for _, v := range values {
			if s, _ := v.(string); s == value {
				return nil
			}
		}
		return fmt.Errorf("is not one of the permitted values")
	}
	return nil
}

// compileMap emits a map's own checks and recurses into its keys.
func compileMap(e *fileEmitter, c *constraintSet, t target, node map[string]any) error {
	guard, value := t.resolve()

	for _, kw := range []string{"minProperties", "maxProperties"} {
		if _, ok := node[kw]; !ok {
			continue
		}
		n, err := integerBound(t, node, kw)
		if err != nil {
			return err
		}
		e.usesErrors = true
		op, wording := "<", "has fewer than minProperties"
		if kw == "maxProperties" {
			op, wording = ">", "has more than maxProperties"
		}
		c.check(guard, fmt.Sprintf("len(%s) %s %d", value, op, n),
			fmt.Sprintf("%s: %s %d", t.label, wording, n))
		e.enforced.mark(node, kw)
	}

	names, ok := node["propertyNames"].(map[string]any)
	if !ok {
		return nil
	}
	// Map keys are always Go strings, so a propertyNames subschema reduces
	// to the string checks applied to the loop variable.
	if kind := shapeOf(names); kind != shapeString && kind != shapeUnknown {
		return fmt.Errorf("%s: %s propertyNames is not a string schema", t.typeName, subjectOf(t.label))
	}
	key := c.loopVar()
	sub := t.derive("property name", "_Key", key, accessElement)
	var body constraintSet
	body.loops = c.loops
	if err := compileStringChecks(e, &body, sub, names); err != nil {
		return err
	}
	c.loops = body.loops
	c.vars.WriteString(body.vars.String())
	if body.checks.Len() > 0 {
		open, closed := guardBlock(guard)
		c.checks.WriteString(open)
		fmt.Fprintf(&c.checks, "for %s := range %s {\n", key, value)
		c.checks.WriteString(body.checks.String())
		c.checks.WriteString("}\n")
		c.checks.WriteString(closed)
	}
	e.enforced.mark(node, "propertyNames")
	return nil
}

// compileStringChecks runs only the keywords that apply to a string, which
// is what a propertyNames subschema constrains regardless of whether it
// bothers to say "type": "string".
func compileStringChecks(e *fileEmitter, c *constraintSet, t target, node map[string]any) error {
	guard, value := t.resolve()
	for _, kw := range declaredCheckable(node) {
		switch kw {
		case "maxLength", "minLength":
			if err := compileLength(e, c, t, node, kw, guard, value); err != nil {
				return err
			}
		case "pattern":
			if err := compilePattern(e, c, t, node, guard, value); err != nil {
				return err
			}
		case "enum":
			if err := compileEnum(e, c, t, node, guard, value); err != nil {
				return err
			}
		case "const":
			if err := compileConst(e, c, t, node, guard, value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: %s constrains property names with %q, which does not apply to a string", t.typeName, subjectOf(t.label), kw)
		}
		e.enforced.mark(node, kw)
	}
	return nil
}

// integerBound reads a keyword whose value must be a non-negative whole
// number — a count of runes, items or properties.
func integerBound(t target, node map[string]any, kw string) (int, error) {
	raw, isNumber := node[kw].(float64)
	if !isNumber {
		return 0, fmt.Errorf("%s: %s %s is %T, want a number", t.typeName, subjectOf(t.label), kw, node[kw])
	}
	if raw < 0 || raw != math.Trunc(raw) {
		return 0, fmt.Errorf("%s: %s %s %v is not a non-negative integer", t.typeName, subjectOf(t.label), kw, raw)
	}
	return int(raw), nil
}

// scalarGoType returns the Go type of a schema that describes a comparable
// scalar, or "" for anything else. It reads the schema rather than calling
// goTypeExpr, which has side effects (registering nested types and imports)
// that must not run twice for one node.
func scalarGoType(node map[string]any) string {
	if node == nil {
		return ""
	}
	if _, isRef := node["$ref"]; isRef {
		return ""
	}
	switch node["type"] {
	case "string":
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	}
	return ""
}

// loopVar returns a fresh loop-variable name. Names are numbered rather
// than reused so a nested loop never shadows its parent.
func (c *constraintSet) loopVar() string {
	c.loops++
	if c.loops == 1 {
		return "k"
	}
	return fmt.Sprintf("k%d", c.loops)
}

// guardBlock turns a nil guard into the statements that open and close a
// compound construct — a loop or a scoped block — since those cannot take
// the guard as a leading conjunct the way a bare `if` can. An absent
// optional value constrains nothing, so its loop must not run at all.
//
// The generated text is not indented: assembleFile runs the whole file
// through gofmt, which lays out nesting far more reliably than tracking a
// depth counter through recursive emission would.
func guardBlock(guard string) (open, closed string) {
	if guard == "" {
		return "", ""
	}
	return "if " + strings.TrimSuffix(guard, " && ") + " {\n", "}\n"
}

// subjectOf names what an error is about.
func subjectOf(label string) string { return fmt.Sprintf("%q", label) }
