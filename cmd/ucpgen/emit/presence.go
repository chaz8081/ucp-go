package emit

import (
	"fmt"
	"strings"
)

// Required-presence tracking.
//
// A required property absent from the JSON is indistinguishable, once
// decoded, from one present with its zero value: both leave a Go string
// empty and a Go pointer nil. Without recording what the decoder actually
// saw, Validate accepts `{}` for a schema that requires an id, which is the
// single largest source of disagreement with a real JSON Schema validator.
//
// The record is a map written during UnmarshalJSON. Its absence is
// meaningful: a value built in Go rather than decoded has no presence
// information at all, and judging it on an empty map would fail every
// hand-constructed request — the SDK's main use for these types.

// requiredNames returns the JSON names of the required properties, sorted,
// which is both the tracking set and the order violations are reported in.
func requiredNames(fields []structField) []string {
	var out []string
	for _, f := range fields {
		if f.required {
			out = append(out, f.jsonName)
		}
	}
	return out // fields arrive in sorted property order
}

// renderPresenceField writes the struct field holding the record. It is
// unexported, which is what keeps it out of the JSON on both sides:
// encoding/json ignores unexported fields entirely, so no tag is needed and
// none is written.
func renderPresenceField(body *strings.Builder) {
	body.WriteString("\n\t// present records which properties the decoder saw, so a required\n")
	body.WriteString("\t// property that was absent can be told from one decoded to its zero\n")
	body.WriteString("\t// value. A nil map means this value was never decoded from JSON.\n")
	body.WriteString("\tpresent map[string]bool\n")
}

// renderPresenceCapture writes the statements that fill the record. It runs
// inside a decoder that has already unmarshaled the payload into `all`.
func renderPresenceCapture(body *strings.Builder, required []string) {
	fmt.Fprintf(body, "\tv.present = make(map[string]bool, %d)\n", len(required))
	fmt.Fprintf(body, "\tfor _, name := range []string{")
	for i, name := range required {
		if i > 0 {
			body.WriteString(", ")
		}
		fmt.Fprintf(body, "%q", name)
	}
	body.WriteString("} {\n\t\tif _, ok := all[name]; ok {\n\t\t\tv.present[name] = true\n\t\t}\n\t}\n")
}

// compilePresenceChecks emits the Validate statements. They are collected
// separately from the value constraints so they can be emitted first: a
// missing property is a more fundamental failure than a bad value, and
// reporting it first makes the error say so.
func compilePresenceChecks(e *fileEmitter, c *constraintSet, required []string) {
	if len(required) == 0 {
		return
	}
	e.usesErrors = true
	c.presence.WriteString("if v.present != nil {\n")
	for _, name := range required {
		fmt.Fprintf(&c.presence, "if !v.present[%q] {\nreturn errors.New(%q)\n}\n",
			name, name+": required property is missing")
	}
	c.presence.WriteString("}\n")
}

// renderPresenceCodec emits UnmarshalJSON for a closed object that needs
// presence tracking. An open object gets its capture folded into the Extra
// codec instead — there must be exactly one UnmarshalJSON per type.
func renderPresenceCodec(body *strings.Builder, typeName string, required []string) {
	alias := typeName + "Alias"
	fmt.Fprintf(body, "\n// UnmarshalJSON decodes the named properties and records which of the\n// required ones were present.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	writeNullGuard(body, typeName, "object")
	fmt.Fprintf(body, "\ttype %s %s\n\tvar named %s\n\tif err := json.Unmarshal(data, &named); err != nil {\n\t\treturn err\n\t}\n\t*v = %s(named)\n\n", alias, typeName, alias, typeName)
	body.WriteString("\tvar all map[string]json.RawMessage\n\tif err := json.Unmarshal(data, &all); err != nil {\n\t\treturn err\n\t}\n")
	renderPresenceCapture(body, required)
	body.WriteString("\treturn nil\n}\n")
}

// renderNullOnlyObjectCodec emits UnmarshalJSON for a closed object that
// needs no presence tracking, purely so a bare null is rejected.
//
// Without it such a type has no decoder at all, and json.Unmarshal treats
// null as a no-op for every Go type — so `null` decoded into the zero value
// and Validate then found nothing wrong, because there is nothing required
// to be missing.
//
// That is the same hole Phase 7 closed for named primitives and slices, and
// the reasoning recorded then had a gap: object roots were exempted on the
// grounds that "the presence codec allocates the record before checking, so
// a decoded null leaves every required property unseen and the check
// rejects it". True, and it silently assumes there IS a required property.
// A schema requiring nothing — common/types/constraint_expression in spec
// 2026-08-25 — got no codec and accepted null.
//
// A rejection that only works as a side effect of another rule is not a
// rejection; it is a coincidence that holds until the corpus changes.
func renderNullOnlyObjectCodec(body *strings.Builder, typeName string) {
	alias := typeName + "Alias"
	fmt.Fprintf(body, "\n// UnmarshalJSON rejects a bare null. encoding/json treats null as a\n// no-op for every Go type, so without this a null document would decode\n// to the zero value and validate as though it were a real object.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	writeNullGuard(body, typeName, "object")
	fmt.Fprintf(body, "\ttype %s %s\n\tvar named %s\n\tif err := json.Unmarshal(data, &named); err != nil {\n\t\treturn err\n\t}\n\t*v = %s(named)\n\treturn nil\n}\n", alias, typeName, alias, typeName)
}
