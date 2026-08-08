// Package emit renders normalized UCP schemas as Go source.
package emit

import (
	"fmt"
	"go/format"
	"regexp"
	"sort"
	"strings"
)

var initialisms = map[string]string{
	"id": "ID", "url": "URL", "uri": "URI", "api": "API",
	"ap2": "AP2", "ucp": "UCP", "mcp": "MCP", "a2a": "A2A",
	"json": "JSON", "jwk": "JWK", "sku": "SKU",
}

// GoName converts a snake_case schema name to an exported Go identifier,
// upper-casing well-known initialisms.
func GoName(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if up, ok := initialisms[strings.ToLower(p)]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
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
func EmitFile(pkg, relPath string, schema map[string]any, specRef string) (string, error) {
	title, _ := schema["title"].(string)
	if title == "" {
		return "", fmt.Errorf("%s: schema has no title", relPath)
	}
	typeName := GoName(strings.ReplaceAll(strings.ToLower(title), " ", "_"))

	required := map[string]bool{}
	if reqs, ok := schema["required"].([]any); ok {
		for _, r := range reqs {
			s, ok := r.(string)
			if !ok {
				return "", fmt.Errorf("%s: required entry is not a string", relPath)
			}
			required[s] = true
		}
	}
	props, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names) // determinism

	// body holds everything after the package clause: the struct definition
	// followed by any Validate() machinery. It is built before the header so
	// the import block (built next) can be conditional on what body actually
	// references.
	var body strings.Builder
	if desc, _ := schema["description"].(string); desc != "" {
		fmt.Fprintf(&body, "// %s %s\n", typeName, desc)
	}
	fmt.Fprintf(&body, "type %s struct {\n", typeName)
	for _, name := range names {
		prop, ok := props[name].(map[string]any)
		if !ok {
			return "", fmt.Errorf("%s: property %q is not an object", relPath, name)
		}
		if desc, _ := prop["description"].(string); desc != "" {
			fmt.Fprintf(&body, "\t// %s\n", desc)
		}
		typ, tag := goType(prop), name
		if !required[name] {
			typ, tag = "*"+typ, name+",omitempty"
		}
		fmt.Fprintf(&body, "\t%s %s `json:%q`\n", GoName(name), typ, tag)
	}
	fmt.Fprintf(&body, "}\n")

	var usesFmt, usesRegexp, usesSync, usesUtf8 bool
	var patternVars, checks strings.Builder
	for _, name := range names {
		prop := props[name].(map[string]any)
		fieldName := GoName(name)
		isRequired := required[name]

		if prop["type"] != "string" {
			// TODO(phase2): maxLength/pattern on non-string properties
			// (arrays, objects, etc.) are not yet supported; skip silently.
			continue
		}

		if mlRaw, ok := prop["maxLength"]; ok {
			if ml, ok := mlRaw.(float64); ok {
				usesFmt, usesUtf8 = true, true
				n := int(ml)
				msg := fmt.Sprintf("%s: exceeds maxLength %d", name, n)
				if isRequired {
					fmt.Fprintf(&checks, "\tif utf8.RuneCountInString(v.%s) > %d {\n\t\treturn fmt.Errorf(%q)\n\t}\n", fieldName, n, msg)
				} else {
					fmt.Fprintf(&checks, "\tif v.%s != nil && utf8.RuneCountInString(*v.%s) > %d {\n\t\treturn fmt.Errorf(%q)\n\t}\n", fieldName, fieldName, n, msg)
				}
			}
		}

		if patRaw, ok := prop["pattern"]; ok {
			pat, ok := patRaw.(string)
			if ok {
				// RE2 gate: fail generation loudly rather than emit a
				// MustCompile that would panic at runtime.
				if _, err := regexp.Compile(pat); err != nil {
					return "", fmt.Errorf("%s: pattern %q for %q is not RE2-compatible: %v", relPath, pat, name, err)
				}
				usesFmt, usesRegexp, usesSync = true, true, true
				varName := "pattern" + typeName + fieldName
				fmt.Fprintf(&patternVars, "var %s = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(%q) })\n\n", varName, pat)
				msg := fmt.Sprintf("%s: does not match pattern", name)
				if isRequired {
					fmt.Fprintf(&checks, "\tif !%s().MatchString(v.%s) {\n\t\treturn fmt.Errorf(%q)\n\t}\n", varName, fieldName, msg)
				} else {
					fmt.Fprintf(&checks, "\tif v.%s != nil && !%s().MatchString(*v.%s) {\n\t\treturn fmt.Errorf(%q)\n\t}\n", fieldName, varName, fieldName, msg)
				}
			}
		}
	}

	if checks.Len() > 0 {
		body.WriteString("\n")
		body.WriteString(patternVars.String())
		body.WriteString("// Validate reports the first constraint violation, or nil.\n")
		fmt.Fprintf(&body, "func (v *%s) Validate() error {\n", typeName)
		body.WriteString(checks.String())
		body.WriteString("\treturn nil\n}\n")
	}

	var out strings.Builder
	fmt.Fprintf(&out, "// Code generated by ucpgen. DO NOT EDIT.\n")
	fmt.Fprintf(&out, "// Source: %s (spec %s)\n\n", relPath, specRef)
	fmt.Fprintf(&out, "package %s\n\n", pkg)

	var imports []string
	if usesFmt {
		imports = append(imports, "fmt")
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
		return "", fmt.Errorf("%s: generated source does not parse: %w\n%s", relPath, err, out.String())
	}
	return string(result), nil
}
