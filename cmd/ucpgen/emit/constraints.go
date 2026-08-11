package emit

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// accessKind describes how a checked value is reached from the Validate
// receiver: directly, through a pointer that may be nil, or as the loop
// variable of a range over a collection.
type accessKind int

const (
	// accessValue is a value that always exists — a required field, or the
	// receiver of a named alias.
	accessValue accessKind = iota
	// accessPointer is an optional field: the check must be nil-guarded and
	// the value dereferenced.
	accessPointer
	// accessElement is a loop variable, which exists like a value but names
	// a collection member rather than a field.
	accessElement
)

// resolve returns the guard a check must be prefixed with and the
// expression that reaches the value itself.
func (a accessKind) resolve(expr string) (guard, value string) {
	if a == accessPointer {
		return expr + " != nil && ", "*" + expr
	}
	return "", expr
}

// constraintSet accumulates what one type's constraints generate: the
// package-level variables (compiled patterns) and the check statements that
// make up its Validate body.
type constraintSet struct {
	vars   strings.Builder
	checks strings.Builder
}

// check writes one statement of the form
//
//	if <guard><cond> {
//		return errors.New(<msg>)
//	}
func (c *constraintSet) check(guard, cond, msg string) {
	fmt.Fprintf(&c.checks, "\tif %s%s {\n\t\treturn errors.New(%q)\n\t}\n", guard, cond, msg)
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
	_, hasMaxLength := node["maxLength"]
	_, hasPattern := node["pattern"]
	if !hasMaxLength && !hasPattern {
		return nil
	}

	// Keeps the emitter honest: a string constraint on a shape whose check
	// we cannot express fails generation loudly instead of silently emitting
	// an unconstrained field.
	if node["type"] != "string" {
		return fmt.Errorf("%s: %s has string constraints but unsupported type %v (phase 4)",
			typeName, subjectOf(jsonName), node["type"])
	}

	guard, value := access.resolve(expr)
	label := jsonName
	if label == "" {
		label = typeName
	}

	if hasMaxLength {
		raw := node["maxLength"]
		max, isNumber := raw.(float64)
		if !isNumber {
			return fmt.Errorf("%s: %s maxLength is %T, want a number", typeName, subjectOf(jsonName), raw)
		}
		if max < 0 || max != math.Trunc(max) {
			return fmt.Errorf("%s: %s maxLength %v is not a non-negative integer", typeName, subjectOf(jsonName), max)
		}
		n := int(max)
		e.usesErrors, e.usesUtf8 = true, true
		// maxLength counts Unicode code points per JSON Schema, not bytes.
		c.check(guard, fmt.Sprintf("utf8.RuneCountInString(%s) > %d", value, n),
			fmt.Sprintf("%s: exceeds maxLength %d", label, n))
	}

	if hasPattern {
		raw := node["pattern"]
		pattern, isString := raw.(string)
		if !isString {
			return fmt.Errorf("%s: %s pattern is %T, want a string", typeName, subjectOf(jsonName), raw)
		}
		// RE2 gate: fail generation loudly rather than emit a MustCompile
		// that would panic at runtime.
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("%s: pattern %q for %s is not RE2-compatible: %v", typeName, pattern, subjectOf(jsonName), err)
		}
		e.usesErrors, e.usesRegexp, e.usesSync = true, true, true
		name := patternVarName(typeName, jsonName)
		fmt.Fprintf(&c.vars, "var %s = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(%q) })\n\n", name, pattern)
		// JSON Schema pattern is an unanchored search, so MatchString (not a
		// full-string match) is intentional.
		c.check(guard, fmt.Sprintf("!%s().MatchString(%s)", name, value),
			fmt.Sprintf("%s: does not match pattern", label))
	}

	return nil
}

// subjectOf names what an error is about: a property, or the type itself.
func subjectOf(jsonName string) string {
	if jsonName == "" {
		return "the schema"
	}
	return fmt.Sprintf("property %q", jsonName)
}

// patternVarName names the package-level variable holding a compiled
// pattern. Property patterns are qualified by field so two properties of
// one type cannot collide; a named type's own pattern needs no suffix.
func patternVarName(typeName, jsonName string) string {
	if jsonName == "" {
		return "pattern_" + typeName
	}
	return "pattern_" + typeName + "_" + GoName(jsonName)
}
