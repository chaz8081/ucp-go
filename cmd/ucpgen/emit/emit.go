// Package emit renders normalized UCP schemas as Go source.
package emit

import (
	"fmt"
	"go/format"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var initialisms = map[string]string{
	"id": "ID", "url": "URL", "uri": "URI", "api": "API",
	"ap2": "AP2", "ucp": "UCP", "mcp": "MCP", "a2a": "A2A",
	"json": "JSON", "jwk": "JWK", "sku": "SKU", "ip": "IP",
}

// unimplementedAssertionKeywords are JSON Schema assertion/composition
// keywords the emitter recognizes but does not implement yet. Their
// presence — at a schema's top level or on any individual property — must
// fail generation loudly: silently ignoring a known assertion keyword
// would emit a Validate() that is quietly less strict than the schema
// actually requires. additionalProperties is handled separately (see
// checkAssertionKeywords) since only its schema form is unimplemented;
// the boolean form (true/false) is fine to leave unenforced for now.
var unimplementedAssertionKeywords = map[string]bool{
	"enum": true, "const": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true,
	"minLength": true,
	"minItems":  true, "maxItems": true, "uniqueItems": true,
	"minProperties": true, "maxProperties": true,
	"contains": true, "minContains": true, "maxContains": true,
	"anyOf": true, "oneOf": true, "allOf": true, "not": true,
	"if": true, "then": true, "else": true,
	"dependentRequired": true, "dependentSchemas": true,
	"propertyNames": true, "patternProperties": true,
}

// checkAssertionKeywords reports the first (in sorted key order, for
// determinism) unimplemented JSON Schema assertion keyword present in
// node, or "" if none. Keys not in unimplementedAssertionKeywords are left
// alone — including annotation-only keywords (format, examples, default,
// title, description, deprecated, readOnly, writeOnly), the $-metaschema
// keywords ($schema, $id, $defs, $ref), ucp_* extension keys, and the
// keywords this emitter does handle itself (type, properties, required,
// maxLength, pattern, and additionalProperties' boolean form).
//
// format in particular is deliberately not flagged: JSON Schema
// draft-2020-12 treats format as annotation-only by default (assertion
// behavior is opt-in, via a vocabulary the spec doesn't enable here), and
// the project's conformance oracle runs with assertFormat off — so
// silently not enforcing format matches the oracle instead of diverging
// from it.
func checkAssertionKeywords(node map[string]any) string {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if unimplementedAssertionKeywords[k] {
			return k
		}
		if k == "additionalProperties" {
			if _, isBool := node[k].(bool); !isBool {
				return k
			}
		}
	}
	return ""
}

// GoName converts a schema identifier (snake_case, dotted, kebab-case, or
// otherwise punctuated — e.g. "dev.ucp.buyer_ip" or "line-item") to an
// exported Go identifier. It splits on any run of non-alphanumeric
// characters, title-cases each part, and upper-cases well-known
// initialisms (id, url, ip, ...) regardless of the part's original
// casing. If the resulting identifier would start with a digit (which is
// not a legal Go identifier), it is prefixed with "V".
func GoName(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var b strings.Builder
	for _, p := range parts {
		if up, ok := initialisms[strings.ToLower(p)]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + strings.ToLower(p[1:]))
	}
	out := b.String()
	if out != "" && out[0] >= '0' && out[0] <= '9' {
		out = "V" + out
	}
	return out
}

func goType(prop map[string]any) string {
	switch prop["type"] {
	case "string":
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		items, _ := prop["items"].(map[string]any)
		if items == nil {
			return "[]any"
		}
		return "[]" + goType(items)
	default:
		return "any" // objects, refs, unions: extended in later phases
	}
}

// EmitFile renders one schema file as one Go source file and returns
// gofmt-formatted source. pkg is the target package name; relPath is the
// schema's path relative to the schemas root; specRef stamps provenance.
//
// Only top-level object schemas are supported in this phase: a schema
// whose top-level "type" is anything other than "object" (or which omits
// both "type" and "properties" entirely) fails generation loudly rather
// than silently emitting an empty, unconstrained struct.
func EmitFile(pkg, relPath string, schema map[string]any, specRef string) (string, error) {
	title, _ := schema["title"].(string)
	if title == "" {
		return "", fmt.Errorf("schema has no title")
	}
	typeName := GoName(title)

	rawType, hasType := schema["type"]
	_, hasPropsKey := schema["properties"]
	if hasType {
		if ts, ok := rawType.(string); !ok || ts != "object" {
			return "", fmt.Errorf("top-level type %q not supported yet (phase 2)", fmt.Sprintf("%v", rawType))
		}
	} else if !hasPropsKey {
		return "", fmt.Errorf("top-level type %q not supported yet (phase 2)", "<missing>")
	}

	required := map[string]bool{}
	if reqRaw, hasReq := schema["required"]; hasReq {
		reqs, isArray := reqRaw.([]any)
		if !isArray {
			if _, isBool := reqRaw.(bool); isBool {
				return "", fmt.Errorf("top-level required is a boolean (OpenRPC parameter semantics); object schemas need an array of property-name strings")
			}
			return "", fmt.Errorf("top-level required is %T, want an array of property-name strings", reqRaw)
		}
		for _, r := range reqs {
			s, ok := r.(string)
			if !ok {
				return "", fmt.Errorf("required entry is not a string")
			}
			required[s] = true
		}
	}
	props, propsOK := schema["properties"].(map[string]any)
	if hasPropsKey && !propsOK {
		// Fail loud: a "properties" key that isn't a JSON object (e.g. a
		// number or array) would otherwise silently type-assert to nil and
		// emit an empty, unconstrained struct.
		return "", fmt.Errorf("properties is %T, want an object of property definitions", schema["properties"])
	}
	if kw := checkAssertionKeywords(schema); kw != "" {
		return "", fmt.Errorf("unsupported constraint keyword %q (phase 2)", kw)
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names) // determinism

	// body holds everything after the package clause: the struct definition
	// followed by the Validate() machinery. It is built before the header so
	// the import block (built next) can be conditional on what body actually
	// references.
	var body strings.Builder
	if desc, _ := schema["description"].(string); desc != "" {
		fmt.Fprintf(&body, "// %s %s\n", typeName, strings.ReplaceAll(desc, "\n", "\n// "))
	}
	fmt.Fprintf(&body, "type %s struct {\n", typeName)

	seenFields := map[string]string{} // Go field name -> originating property name
	fieldNames := make(map[string]string, len(names))
	for _, name := range names {
		prop, ok := props[name].(map[string]any)
		if !ok {
			return "", fmt.Errorf("property %q is not an object", name)
		}
		if kw := checkAssertionKeywords(prop); kw != "" {
			return "", fmt.Errorf("unsupported constraint keyword %q (phase 2)", kw)
		}
		fieldName := GoName(name)
		if fieldName == "Validate" {
			return "", fmt.Errorf("property %q sanitizes to Go field name %q, which collides with the generated Validate() method", name, fieldName)
		}
		if orig, dup := seenFields[fieldName]; dup {
			return "", fmt.Errorf("properties %q and %q both sanitize to Go field name %q", orig, name, fieldName)
		}
		seenFields[fieldName] = name
		fieldNames[name] = fieldName

		if desc, _ := prop["description"].(string); desc != "" {
			fmt.Fprintf(&body, "\t// %s\n", strings.ReplaceAll(desc, "\n", "\n\t// "))
		}
		typ, tag := goType(prop), name
		if !required[name] {
			// Pointer + omitempty approximates JSON Schema optionality.
			// Go 1.24's `omitzero` is closer to the design's intended
			// pointer-optionality semantics; omitempty is equivalent for
			// pointers here. Revisit in Phase 2.
			typ, tag = "*"+typ, name+",omitempty"
		}
		fmt.Fprintf(&body, "\t%s %s `json:%q`\n", fieldName, typ, tag)
	}
	fmt.Fprintf(&body, "}\n")

	var usesErrors, usesRegexp, usesSync, usesUtf8 bool
	var patternVars, checks strings.Builder
	for _, name := range names {
		prop := props[name].(map[string]any)
		fieldName := fieldNames[name]
		isRequired := required[name]
		isString := prop["type"] == "string"

		_, hasML := prop["maxLength"]
		_, hasPattern := prop["pattern"]

		if !isString {
			if hasML || hasPattern {
				// Keeps the MVP honest: unsupported constraint targets
				// fail loudly instead of silently emitting an
				// unconstrained field. Arrays/objects/unions deferred to
				// phase 2.
				return "", fmt.Errorf("property %q has string constraints but unsupported type %v (phase 2)", name, prop["type"])
			}
			continue
		}

		if hasML {
			mlRaw := prop["maxLength"]
			ml, ok := mlRaw.(float64)
			if !ok {
				return "", fmt.Errorf("property %q maxLength is %T, want a number", name, mlRaw)
			}
			if ml < 0 || ml != math.Trunc(ml) {
				// JSON Schema's maxLength is a non-negative integer.
				// Silently truncating a negative or fractional value (via
				// int(ml)) would emit a Validate() that enforces a
				// different bound than the schema actually specifies.
				return "", fmt.Errorf("property %q maxLength %v is not a non-negative integer", name, ml)
			}
			n := int(ml)
			msg := fmt.Sprintf("%s: exceeds maxLength %d", name, n)
			usesErrors, usesUtf8 = true, true
			// maxLength counts Unicode code points per JSON Schema, not
			// bytes, hence RuneCountInString rather than len().
			if isRequired {
				fmt.Fprintf(&checks, "\tif utf8.RuneCountInString(v.%s) > %d {\n\t\treturn errors.New(%q)\n\t}\n", fieldName, n, msg)
			} else {
				fmt.Fprintf(&checks, "\tif v.%s != nil && utf8.RuneCountInString(*v.%s) > %d {\n\t\treturn errors.New(%q)\n\t}\n", fieldName, fieldName, n, msg)
			}
		}

		if hasPattern {
			patRaw := prop["pattern"]
			pat, ok := patRaw.(string)
			if !ok {
				return "", fmt.Errorf("property %q pattern is %T, want a string", name, patRaw)
			}
			// RE2 gate: fail generation loudly rather than emit a
			// MustCompile that would panic at runtime.
			if _, err := regexp.Compile(pat); err != nil {
				return "", fmt.Errorf("pattern %q for %q is not RE2-compatible: %v", pat, name, err)
			}
			usesErrors, usesRegexp, usesSync = true, true, true
			varName := fmt.Sprintf("pattern_%s_%s", typeName, fieldName)
			fmt.Fprintf(&patternVars, "var %s = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(%q) })\n\n", varName, pat)
			msg := fmt.Sprintf("%s: does not match pattern", name)
			// JSON Schema pattern is an unanchored search, so MatchString
			// (not a full-string match) is intentional here.
			if isRequired {
				fmt.Fprintf(&checks, "\tif !%s().MatchString(v.%s) {\n\t\treturn errors.New(%q)\n\t}\n", varName, fieldName, msg)
			} else {
				fmt.Fprintf(&checks, "\tif v.%s != nil && !%s().MatchString(*v.%s) {\n\t\treturn errors.New(%q)\n\t}\n", fieldName, varName, fieldName, msg)
			}
		}
	}

	// Validate() is always emitted, even with zero checks, so downstream
	// code (Task 9's conformance oracle, generated SDK callers) can rely on
	// a uniform interface{ Validate() error } across every generated type.
	body.WriteString("\n")
	if patternVars.Len() > 0 {
		body.WriteString(patternVars.String())
	}
	body.WriteString("// Validate reports the first constraint violation, or nil.\n")
	fmt.Fprintf(&body, "func (v *%s) Validate() error {\n", typeName)
	body.WriteString(checks.String())
	body.WriteString("\treturn nil\n}\n")

	var out strings.Builder
	fmt.Fprintf(&out, "// Code generated by ucpgen. DO NOT EDIT.\n")
	fmt.Fprintf(&out, "// Source: %s (spec %s)\n\n", relPath, specRef)
	fmt.Fprintf(&out, "package %s\n\n", pkg)

	var imports []string
	if usesErrors {
		imports = append(imports, "errors")
	}
	if usesRegexp {
		imports = append(imports, "regexp")
	}
	if usesSync {
		imports = append(imports, "sync")
	}
	if usesUtf8 {
		imports = append(imports, "unicode/utf8")
	}
	sort.Strings(imports)
	if len(imports) == 1 {
		fmt.Fprintf(&out, "import %q\n\n", imports[0])
	} else if len(imports) > 1 {
		out.WriteString("import (\n")
		for _, imp := range imports {
			fmt.Fprintf(&out, "\t%q\n", imp)
		}
		out.WriteString(")\n\n")
	}

	out.WriteString(body.String())

	result, err := format.Source([]byte(out.String()))
	if err != nil {
		return "", fmt.Errorf("generated source does not parse: %w\n%s", err, out.String())
	}
	return string(result), nil
}
