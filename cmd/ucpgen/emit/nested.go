package emit

import (
	"fmt"
	"strings"
)

// Recursive validation.
//
// JSON Schema applies a subschema wherever it is referenced, so a document
// is valid only if every value inside it is. A Validate that checked only
// its own fields would call a Checkout fine while one of its line items
// broke a constraint — and since every generated type has a Validate, the
// check that closes that gap is just a call.

// scalarGoTypes are the Go types the emitter produces that are not
// generated named types, and so have no Validate method to call.
var scalarGoTypes = map[string]bool{
	"string": true, "int64": true, "float64": true, "bool": true,
	"any": true, "json.RawMessage": true,
}

// elementType strips the pointer, slice and map wrappers off a Go type
// expression to reach the type a Validate would be called on.
func elementType(goType string) string {
	t := goType
	for {
		switch {
		case strings.HasPrefix(t, "*"):
			t = t[1:]
		case strings.HasPrefix(t, "[]"):
			t = t[2:]
		case strings.HasPrefix(t, "map[string]"):
			t = t[len("map[string]"):]
		default:
			return t
		}
	}
}

// hasValidateMethod reports whether a field's type is a generated one, and
// so carries a Validate. Every named type the emitter produces has one —
// including aliases over scalars, whose Validate is often the only place
// their pattern is enforced.
func hasValidateMethod(goType string) bool {
	t := elementType(goType)
	return t != "" && !scalarGoTypes[t]
}

// compileNested emits the recursive Validate calls for one field.
func compileNested(c *constraintSet, goName, goType string) {
	if !hasValidateMethod(goType) {
		return
	}
	emitValidateCall(&c.nested, "v."+goName, goType, 0)
}

// compileNestedAlias emits the recursion for a named slice or map type,
// whose elements are reached through the receiver rather than a field.
//
// Only structs used to recurse, so an alias over a slice of objects
// emitted a Validate that ignored every element. The corpus's only such
// types are the totals family, whose element carries the money rules —
// and the differential harness skipped those schemas for their contains
// keyword, so nothing was watching.
//
// A scalar alias needs nothing. A named alias over another named struct
// is deliberately left alone: the conversion it would take to reach the
// value is not addressable, so a pointer-receiver Validate cannot be
// called on it.
func compileNestedAlias(c *constraintSet, underlying string) {
	if !strings.HasPrefix(underlying, "[]") && !strings.HasPrefix(underlying, "map[") {
		return
	}
	if !hasValidateMethod(underlying) {
		return
	}
	// Parenthesized: *v[i] would parse as *(v[i]).
	emitValidateCall(&c.nested, "(*v)", underlying, 0)
}

// emitValidateCall peels one layer off a Go type expression and recurses,
// so a type built from several wrappers — the corpus has
// map[string][]CapabilityBase — reaches the value that actually has the
// method. Peeling only one layer would emit a call on a slice.
//
// Ranging over a nil slice or map does nothing, so only a pointer needs a
// guard; an absent optional collection is skipped for free.
func emitValidateCall(b *strings.Builder, expr, goType string, depth int) {
	switch {
	case strings.HasPrefix(goType, "[]"):
		// Indexed rather than ranged by value, so the element stays
		// addressable for its pointer-receiver Validate.
		i := loopName("i", depth)
		fmt.Fprintf(b, "for %s := range %s {\n", i, expr)
		emitValidateCall(b, fmt.Sprintf("%s[%s]", expr, i), goType[2:], depth+1)
		b.WriteString("}\n")
	case strings.HasPrefix(goType, "map[string]"):
		// A map value is not addressable, so it is copied into a variable
		// first.
		m := loopName("m", depth)
		fmt.Fprintf(b, "for _, %s := range %s {\n", m, expr)
		emitValidateCall(b, m, goType[len("map[string]"):], depth+1)
		b.WriteString("}\n")
	case strings.HasPrefix(goType, "*"):
		fmt.Fprintf(b, "if %s != nil {\n", expr)
		emitValidateCall(b, expr, goType[1:], depth+1)
		b.WriteString("}\n")
	default:
		fmt.Fprintf(b, "if err := %s.Validate(); err != nil {\nreturn err\n}\n", expr)
	}
}

// loopName numbers loop variables past the first so a nested loop never
// shadows the one outside it.
func loopName(stem string, depth int) string {
	if depth == 0 {
		return stem
	}
	return fmt.Sprintf("%s%d", stem, depth)
}
