package emit

import (
	"fmt"
	"sort"
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

// dependentRequiredRules returns the schema's dependentRequired entries in a
// deterministic order, as (trigger, required) pairs.
//
// JSON Schema: if the trigger property is PRESENT, every name in its list
// must be present too. Absence of the trigger asserts nothing — which is
// why this is a presence rule and not a required-property rule, and why it
// can only be checked on a value that was decoded.
func dependentRequiredRules(schema map[string]any) [][2]any {
	raw, ok := schema["dependentRequired"].(map[string]any)
	if !ok {
		return nil
	}
	triggers := make([]string, 0, len(raw))
	for name := range raw {
		triggers = append(triggers, name)
	}
	sort.Strings(triggers)

	var out [][2]any
	for _, trigger := range triggers {
		list, isList := raw[trigger].([]any)
		if !isList {
			continue
		}
		names := make([]string, 0, len(list))
		for _, n := range list {
			if s, isStr := n.(string); isStr {
				names = append(names, s)
			}
		}
		sort.Strings(names)
		if len(names) > 0 {
			out = append(out, [2]any{trigger, names})
		}
	}
	return out
}

// presenceNames returns every property whose presence the decoder must
// record: the unconditionally required ones, plus every property named by a
// dependentRequired rule.
//
// The two sets are tracked together but checked apart. A schema can carry a
// dependentRequired rule while requiring nothing outright — common/types/
// time_interval makes `opens` and `closes` require each other and neither
// on its own — and such a type used to get no presence record at all,
// leaving the rule with nothing to read.
func presenceNames(schema map[string]any, fields []structField) []string {
	seen := map[string]bool{}
	for _, name := range requiredNames(fields) {
		seen[name] = true
	}
	declared := map[string]bool{}
	for _, f := range fields {
		declared[f.jsonName] = true
	}
	for _, rule := range dependentRequiredRules(schema) {
		trigger := rule[0].(string)
		if !declared[trigger] {
			continue
		}
		for _, name := range rule[1].([]string) {
			if declared[name] {
				seen[name] = true
			}
		}
		seen[trigger] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// compileDependentRequired emits the conditional presence checks.
//
// Guarded on `v.present != nil` like every other presence check: a value
// built in Go rather than decoded has no record, and reporting a dependency
// unmet on one would fail every hand-constructed request. A decoded value
// always has the record, so the rule holds exactly where it can be judged.
func compileDependentRequired(e *fileEmitter, c *constraintSet, schema map[string]any, fields []structField) error {
	rules := dependentRequiredRules(schema)
	if len(rules) == 0 {
		return nil
	}
	declared := map[string]bool{}
	for _, f := range fields {
		declared[f.jsonName] = true
	}
	var body strings.Builder
	for _, rule := range rules {
		trigger := rule[0].(string)
		if !declared[trigger] {
			// A request variant drops properties while keeping the rules, so
			// a rule can name a property this type no longer has. Nothing to
			// bind it to; leaving the keyword unmarked reports it.
			return nil
		}
		for _, name := range rule[1].([]string) {
			if !declared[name] {
				return nil
			}
			fmt.Fprintf(&body, "if v.present[%q] && !v.present[%q] {\nreturn errors.New(%q)\n}\n",
				trigger, name, fmt.Sprintf("%s: required when %s is present", name, trigger))
		}
	}
	if body.Len() == 0 {
		return nil
	}
	e.usesErrors = true
	c.presence.WriteString("if v.present != nil {\n")
	c.presence.WriteString(body.String())
	c.presence.WriteString("}\n")
	e.enforced.mark(schema, "dependentRequired")
	return nil
}
