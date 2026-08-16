package emit

import (
	"fmt"
	"strings"
)

// Rejecting a bare null.
//
// json.Unmarshal([]byte("null"), &v) is a documented no-op for every Go
// type: it succeeds and leaves the value at its zero value. That matters
// here because the decoder is this SDK's JSON type check — a string payload
// fails to decode into an int64, and that decode failure IS the rejection.
// null is the one input the decoder lets through for everything, so every
// scalar- and array-rooted named type accepted a null document and then
// validated the zero value as though it were the real one. ReverseDomainName
// rejected it only by luck, because the empty string happens to fail its
// pattern; Totals was worse still, since its contains count is guarded on
// the slice being non-nil and a nil slice skipped the check entirely.
//
// Object roots need none of this: they already carry an UnmarshalJSON for
// presence tracking, and after decoding null their present map stays nil,
// so the required-property check fires.
//
// The codec is emitted only from renderNamedType's alias path, which is the
// one branch that writes no decoder of its own. There must be exactly one
// UnmarshalJSON per type or the generated package will not compile.
//
// An OPTIONAL property of one of these types is unaffected, and that is the
// whole reason this can live in the decoder. An optional non-nilable
// property is rendered as a pointer, and encoding/json handles a null for a
// pointer field itself: it stores nil without ever consulting the pointed-to
// type's Unmarshaler. Verified against go1.26 — `{"max": null}` for a
// `*Amount` field leaves Max nil and returns no error, while the same null
// for a required, non-pointer Amount field reaches Amount.UnmarshalJSON and
// is rejected. Absence behaves identically to an explicit null, as it must.

// needsNullCodec reports whether a named type's underlying Go type is one
// whose schema cannot admit null.
//
// The four primitives and any slice qualify. Deliberately excluded:
//
//   - A pointer underlying (*string) is exactly how goTypeExpr renders
//     type: ["string","null"], i.e. a schema that DOES permit null. Such a
//     type would keep accepting it. None exists in this corpus today —
//     Go forbids a method on a defined pointer type, so the generated
//     Validate would not compile — but the rule has to state the exemption
//     rather than rely on that accident.
//   - A named type (types.Fulfillment) or json.RawMessage, where the alias
//     is standing in for a $ref or an untyped union and the shape the null
//     would have to be judged against lives elsewhere.
//
// A map underlying — what a property-less object root becomes — qualifies
// for the same reason a slice does. A struct root does not need it: its
// presence codec allocates the record before checking, so a decoded null
// leaves every required property unseen and the check rejects it.
func needsNullCodec(underlying string) bool {
	switch underlying {
	case "string", "int64", "float64", "bool":
		return true
	}
	return strings.HasPrefix(underlying, "[]") || strings.HasPrefix(underlying, "map[")
}

// jsonTypeName renders the schema type a null was rejected in favour of, for
// the error message. The schema's own word is preferred over the Go type
// because that is the vocabulary the caller's payload was written in:
// "null is not a valid integer" points at the schema, "not a valid int64"
// points at our translation of it.
func jsonTypeName(schema map[string]any, underlying string) string {
	if t, ok := schema["type"].(string); ok && t != "" {
		return t
	}
	if strings.HasPrefix(underlying, "[]") {
		return "array"
	}
	if strings.HasPrefix(underlying, "map[") {
		return "object"
	}
	return underlying
}

// renderNullCodec emits UnmarshalJSON for a scalar or array alias. The alias
// indirection is the same one renderPresenceCodec uses: the local type drops
// the method set, so the nested Unmarshal decodes normally instead of
// recursing into the method being defined.
// writeNullGuard emits the bare-null rejection that opens every generated
// decoder.
//
// Every named type needs it, not only the aliases. A struct whose schema
// requires nothing has no check a zero value fails, so a decoded null
// passed straight through — Checkout happened to reject it only because it
// has required properties, which made the hole look narrower than it was.
// Putting the guard in each decoder rather than in Validate is what keeps
// a hand-built zero value legal while a null document is not.
func writeNullGuard(body *strings.Builder, typeName, jsonType string) {
	fmt.Fprintf(body, "\tif string(data) == \"null\" {\n\t\treturn errors.New(%q)\n\t}\n",
		fmt.Sprintf("%s: null is not a valid %s", typeName, jsonType))
}

func renderNullCodec(body *strings.Builder, typeName, underlying string, schema map[string]any) {
	alias := typeName + "Alias"
	fmt.Fprintf(body, "// UnmarshalJSON rejects a bare null. encoding/json treats null as a\n// no-op for every Go type, so without this the zero value would pass\n// every check and a null document would validate as though it were a\n// real value.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	writeNullGuard(body, typeName, jsonTypeName(schema, underlying))
	fmt.Fprintf(body, "\ttype %s %s\n\tvar alias %s\n\tif err := json.Unmarshal(data, &alias); err != nil {\n\t\treturn err\n\t}\n\t*v = %s(alias)\n\treturn nil\n}\n\n",
		alias, typeName, alias, typeName)
}
