// Package emit renders normalized UCP schemas as Go source.
package emit

import (
	"fmt"
	"go/format"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/chaz8081/ucp-go/cmd/ucpgen/preprocess"
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
// typeAffectingKeywords change the SHAPE a schema describes, so the
// emitter cannot produce a correct Go type without implementing them.
// Their presence fails generation.
var typeAffectingKeywords = map[string]bool{
	// patternProperties gives an object per-key-pattern value schemas, which
	// a single map[string]T cannot express.
	"patternProperties": true,
}

// validationOnlyKeywords constrain values without changing their Go type.
// Phase 3 emits types; enforcing these is phase 4. They are neither
// implemented nor silently dropped: every occurrence is recorded in a doc
// comment on the generated field or type, and in MANIFEST.json, so the
// coverage gap is visible in the output rather than only in a plan.
var validationOnlyKeywords = map[string]bool{
	"enum": true, "const": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true,
	"minLength": true,
	"minItems":  true, "maxItems": true, "uniqueItems": true,
	"minProperties": true, "maxProperties": true,
	"contains": true, "minContains": true, "maxContains": true,
	"not": true, "if": true, "then": true, "else": true,
	"dependentRequired": true, "dependentSchemas": true,
	"propertyNames": true,
}

// Keywords deliberately in neither set, and why:
//
//   - oneOf / anyOf: modeled by renderNamedType as an interface when every
//     member is a $ref, as a struct when the node also carries its own
//     properties, and as json.RawMessage otherwise.
//   - allOf: merged locally by EmitFile via preprocess.MergeAllOf. ucp.json
//     and its two request variants arrive unmerged because the preprocessing
//     pipeline skips them, mirroring python.
//   - additionalProperties: the schema form becomes a map value type; the
//     boolean form constrains nothing we model.

// checkTypeAffectingKeywords reports the first (in sorted key order, for
// determinism) keyword present in node that the emitter cannot model as a
// Go type, or "" if none.
func checkTypeAffectingKeywords(node map[string]any) string {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if typeAffectingKeywords[k] {
			return k
		}
	}
	return ""
}

// unenforcedKeywords returns, sorted, the validation-only keywords present
// in node. They are reported in the generated output rather than enforced.
func unenforcedKeywords(node map[string]any) []string {
	var out []string
	for k := range node {
		if validationOnlyKeywords[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
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

// EmitFile renders every Go type a schema file produces and returns
// gofmt-formatted source.
//
// A file yields: its file-level type (when the document root carries one),
// one type per $def, and one type per inline object promoted out of a
// property. idx must already contain every type in the corpus so that
// cross-file references resolve.
func EmitFile(idx *TypeIndex, modulePath, relPath string, schema map[string]any, specRef string) (string, error) {
	return EmitFileWithBreaks(idx, modulePath, relPath, schema, specRef, nil, nil)
}

// EmitFileWithBreaks is EmitFile with the cycle-breaking edge set for the
// package this schema belongs to (see CycleBreaks).
func EmitFileWithBreaks(idx *TypeIndex, modulePath, relPath string, schema map[string]any, specRef string, breaks map[string]bool, corpus map[string]map[string]any) (string, error) {
	pkg, _ := PackageForSchema(relPath, modulePath)
	e := newFileEmitterWithBreaks(idx, relPath, pkg, breaks)

	// ucp.json and its two request variants are skipped by the preprocessing
	// pipeline (mirroring python), so they arrive with allOf still unmerged
	// inside their $defs. Merge locally so every renderer below sees a flat
	// node.
	if err := mergeAllOf(schema, relPath, corpus); err != nil {
		return "", fmt.Errorf("%w", err)
	}

	var body strings.Builder

	if hasFileLevelType(schema) {
		ref, ok := idx.Lookup(relPath, "")
		if !ok {
			return "", fmt.Errorf("%s: file-level type is not in the index", relPath)
		}
		if err := renderNamedType(e, &body, ref.Name, schema); err != nil {
			return "", err
		}
	}

	defs, _ := schema["$defs"].(map[string]any)
	defNames := make([]string, 0, len(defs))
	for name := range defs {
		defNames = append(defNames, name)
	}
	sort.Strings(defNames)
	for _, name := range defNames {
		def, ok := defs[name].(map[string]any)
		if !ok {
			return "", fmt.Errorf("$defs/%s is %T, want an object", name, defs[name])
		}
		if isNamespaceDef(def) {
			// A grouping object, not a schema — see isNamespaceDef. Recorded
			// in the output so the omission is visible rather than silent.
			fmt.Fprintf(&body, "// $defs/%s is an extension mount point (a namespace of schemas,\n// not a schema); no type is emitted for it.\n\n", name)
			continue
		}
		ref, ok := idx.Lookup(relPath, name)
		if !ok {
			return "", fmt.Errorf("$defs/%s is not in the index", name)
		}
		if err := renderNamedType(e, &body, ref.Name, def); err != nil {
			return "", err
		}
	}

	// Inline object types are discovered while rendering, and rendering one
	// can discover more, so drain until the queue is empty.
	for i := 0; i < len(e.nested); i++ {
		n := e.nested[i]
		if err := renderNamedType(e, &body, n.name, n.schema); err != nil {
			return "", err
		}
	}

	return assembleFile(e, relPath, pkg, specRef, body.String())
}

// mergeAllOf flattens allOf at the document root and inside each $def.
//
// preprocess.MergeAllOf resolves only same-document refs and re-emits
// cross-file branches as a residual allOf. Those branches carry real
// inherited fields — shipping_destination.json is allOf[postal_address.json]
// plus one own property, so ignoring the residual would silently drop nine
// address fields — so they are resolved here against the rest of the
// corpus, which the emitter (unlike the preprocessor) has in hand.
func mergeAllOf(schema map[string]any, relPath string, corpus map[string]map[string]any) error {
	if err := resolveCrossFileAllOf(schema, relPath, corpus, map[string]bool{}); err != nil {
		return err
	}
	if err := preprocess.MergeAllOf(schema, schema); err != nil {
		return err
	}
	defs, _ := schema["$defs"].(map[string]any)
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		def, ok := defs[name].(map[string]any)
		if !ok {
			continue
		}
		if err := resolveCrossFileAllOf(def, relPath, corpus, map[string]bool{}); err != nil {
			return fmt.Errorf("$defs/%s: %w", name, err)
		}
		if err := preprocess.MergeAllOf(def, schema); err != nil {
			return fmt.Errorf("$defs/%s: %w", name, err)
		}
	}
	if residual, ok := schema["allOf"].([]any); ok && len(residual) > 0 {
		return fmt.Errorf("allOf branches remain unresolved after merging: %v", residual)
	}
	return nil
}

// resolveCrossFileAllOf replaces each cross-file $ref branch of a node's
// allOf with a deep copy of the referenced schema, so the subsequent local
// merge folds its fields in. seen guards against a ref cycle between files.
func resolveCrossFileAllOf(node map[string]any, relPath string, corpus map[string]map[string]any, seen map[string]bool) error {
	branches, ok := node["allOf"].([]any)
	if !ok || corpus == nil {
		return nil
	}
	for i, raw := range branches {
		branch, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ref, ok := branch["$ref"].(string)
		if !ok {
			continue
		}
		filePart, fragment, _ := strings.Cut(ref, "#")
		if filePart == "" {
			continue // local ref: preprocess.MergeAllOf handles it
		}
		target := path.Join(path.Dir(relPath), filePart)
		targetSchema, ok := corpus[target]
		if !ok {
			return fmt.Errorf("allOf references %q, which is not in the corpus", ref)
		}
		resolved := targetSchema
		if fragment != "" {
			defName, hasPrefix := strings.CutPrefix(fragment, defsFragmentPrefix)
			if !hasPrefix || strings.Contains(defName, "/") {
				return fmt.Errorf("allOf ref %q has an unsupported fragment", ref)
			}
			defs, _ := targetSchema["$defs"].(map[string]any)
			resolved, ok = defs[defName].(map[string]any)
			if !ok {
				return fmt.Errorf("allOf ref %q resolves to no schema", ref)
			}
		}
		if seen[target+"#"+fragment] {
			return fmt.Errorf("allOf ref cycle through %q", ref)
		}
		seen[target+"#"+fragment] = true

		inlined := preprocess.CopyTree(resolved).(map[string]any)
		// Relative refs inside the borrowed schema were written against its
		// own directory; once inlined they are read against the borrowing
		// file's. Without rebasing, product.json's "category.json" becomes
		// shopping/category.json instead of shopping/types/category.json.
		rebaseRefs(inlined, path.Dir(target), path.Dir(relPath))
		// The branch contributes fields, not identity: a borrowed title or
		// description would otherwise override the borrowing type's own.
		delete(inlined, "title")
		delete(inlined, "description")
		delete(inlined, "$id")
		delete(inlined, "$schema")
		delete(inlined, "$defs")
		// The inlined schema may itself inherit across files.
		if err := resolveCrossFileAllOf(inlined, target, corpus, seen); err != nil {
			return err
		}
		branches[i] = inlined
	}
	return nil
}

// rebaseRefs rewrites every relative cross-file $ref in a subtree that was
// moved from fromDir to toDir, so it still names the same schema.
func rebaseRefs(node any, fromDir, toDir string) {
	if fromDir == toDir {
		return
	}
	switch t := node.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok {
			if filePart, fragment, hasFrag := strings.Cut(ref, "#"); filePart != "" {
				rebased := relPathBetween(toDir, path.Join(fromDir, filePart))
				if hasFrag {
					rebased += "#" + fragment
				}
				t["$ref"] = rebased
			}
		}
		for _, v := range t {
			rebaseRefs(v, fromDir, toDir)
		}
	case []any:
		for _, v := range t {
			rebaseRefs(v, fromDir, toDir)
		}
	}
}

// relPathBetween returns the slash-relative path from dir to target.
func relPathBetween(dir, target string) string {
	if dir == "." || dir == "" {
		return target
	}
	dirParts := strings.Split(dir, "/")
	targetParts := strings.Split(target, "/")
	common := 0
	for common < len(dirParts) && common < len(targetParts) && dirParts[common] == targetParts[common] {
		common++
	}
	var out []string
	for i := common; i < len(dirParts); i++ {
		out = append(out, "..")
	}
	out = append(out, targetParts[common:]...)
	return strings.Join(out, "/")
}

// renderNamedType writes one named Go type — struct, union interface, or
// scalar alias — plus its Validate method.
func renderNamedType(e *fileEmitter, body *strings.Builder, typeName string, schema map[string]any) error {
	if kw := checkTypeAffectingKeywords(schema); kw != "" {
		return fmt.Errorf("%s: keyword %q changes the schema's shape and is not modeled yet (phase 4)", typeName, kw)
	}

	if raw, hasKey := schema["properties"]; hasKey {
		if _, ok := raw.(map[string]any); !ok {
			// A "properties" key that is not a JSON object would otherwise
			// type-assert to nil and quietly emit an empty, unconstrained
			// struct.
			return fmt.Errorf("%s: properties is %T, want an object of property definitions", typeName, raw)
		}
	}
	_, hasProps := schema["properties"].(map[string]any)

	// A union whose members are all $refs becomes a Go interface, with each
	// member type given a marker method. Members must be in this package:
	// Go cannot add a method to a type declared elsewhere.
	if hasUnion(schema) && !hasProps {
		keyword, members := unionMembers(schema)
		if allRefMembers(members) {
			return renderUnion(e, body, typeName, keyword, members, schema)
		}
		// Mixed or inline members: modeled as raw JSON by goTypeExpr.
	}

	if hasProps {
		return renderStruct(e, body, typeName, schema)
	}

	// A schema that declares no type, no properties, no $defs, no $ref and
	// no union constrains nothing at all. Emitting `type X any` for it would
	// accept any JSON whatsoever, so treat it as a defect in the input.
	if _, hasType := schema["type"]; !hasType {
		if _, hasRef := schema["$ref"]; !hasRef {
			if _, hasDefs := schema["$defs"]; !hasDefs && !hasUnion(schema) {
				return fmt.Errorf("%s: schema declares neither type, properties, $defs, $ref nor a union; nothing to emit", typeName)
			}
		}
	}

	// Everything else is an alias over whatever goTypeExpr yields: scalar
	// types, arrays, maps, and inline-member unions.
	underlying, err := e.goTypeExpr(schema, typeName)
	if err != nil {
		return err
	}
	writeDoc(body, typeName, schema)
	if keyword, members := unionMembers(schema); len(members) > 0 {
		fmt.Fprintf(body, "//\n// This schema is a %s of %d alternatives with no shared\n// properties, so it is carried as raw JSON. Typed alternatives are\n// phase 4.\n", keyword, len(members))
	}
	fmt.Fprintf(body, "type %s %s\n\n", typeName, underlying)
	fmt.Fprintf(body, "// Validate reports the first constraint violation, or nil.\nfunc (v *%s) Validate() error {\n\treturn nil\n}\n\n", typeName)
	return nil
}

func allRefMembers(members []any) bool {
	for _, m := range members {
		mm, ok := m.(map[string]any)
		if !ok {
			return false
		}
		if _, ok := mm["$ref"].(string); !ok {
			return false
		}
	}
	return len(members) > 0
}

// renderUnion emits a closed union as a struct with one optional field per
// member, plus UnmarshalJSON/MarshalJSON.
//
// A marker interface would be the tidier Go shape, but encoding/json cannot
// unmarshal into an interface: `ucp` is a required union-typed field on
// Cart, Checkout and Order, so a bare interface makes the protocol's
// primary response types undecodable. A struct of pointers decodes, keeps
// every member statically typed, and round-trips.
//
// Members are tried in declaration order and the first that unmarshals
// without error wins. The spec declares no discriminator at the union
// level, so this is the available strategy; a discriminated decode is
// phase 4.
func renderUnion(e *fileEmitter, body *strings.Builder, typeName, keyword string, members []any, schema map[string]any) error {
	type unionMember struct{ field, typ string }
	var fields []unionMember
	for _, m := range members {
		ref := m.(map[string]any)["$ref"].(string)
		target, err := ResolveRef(e.idx, e.rel, ref)
		if err != nil {
			return err
		}
		fields = append(fields, unionMember{field: target.Name, typ: e.qualify(target)})
	}

	e.imports["encoding/json"] = "json"
	e.usesErrors = true

	writeDoc(body, typeName, schema)
	fmt.Fprintf(body, "//\n// %s is a closed %s union: exactly one field is set.\n", typeName, keyword)
	fmt.Fprintf(body, "type %s struct {\n", typeName)
	for _, f := range fields {
		fmt.Fprintf(body, "\t%s *%s `json:\"-\"`\n", f.field, f.typ)
	}
	body.WriteString("}\n\n")

	// UnmarshalJSON: first member that decodes cleanly wins.
	fmt.Fprintf(body, "// UnmarshalJSON decodes the first union member that accepts the input.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	for _, f := range fields {
		fmt.Fprintf(body, "\tvar as%s %s\n\tif err := json.Unmarshal(data, &as%s); err == nil {\n\t\tv.%s = &as%s\n\t\treturn nil\n\t}\n", f.field, f.typ, f.field, f.field, f.field)
	}
	fmt.Fprintf(body, "\treturn errors.New(%q)\n}\n\n", typeName+": no union member accepted the input")

	// MarshalJSON: emit whichever member is set.
	fmt.Fprintf(body, "// MarshalJSON encodes whichever union member is set.\nfunc (v %s) MarshalJSON() ([]byte, error) {\n", typeName)
	for _, f := range fields {
		fmt.Fprintf(body, "\tif v.%s != nil {\n\t\treturn json.Marshal(v.%s)\n\t}\n", f.field, f.field)
	}
	fmt.Fprintf(body, "\treturn nil, errors.New(%q)\n}\n\n", typeName+": no union member is set")

	fmt.Fprintf(body, "// Validate reports the first constraint violation, or nil.\nfunc (v *%s) Validate() error {\n\treturn nil\n}\n\n", typeName)
	return nil
}

func writeDoc(body *strings.Builder, typeName string, schema map[string]any) {
	if desc, _ := schema["description"].(string); desc != "" {
		fmt.Fprintf(body, "// %s %s\n", typeName, strings.ReplaceAll(desc, "\n", "\n// "))
	}
}

// renderStruct emits an object schema as a Go struct plus its Validate.
func renderStruct(e *fileEmitter, body *strings.Builder, typeName string, schema map[string]any) error {
	required := map[string]bool{}
	if reqRaw, hasReq := schema["required"]; hasReq {
		reqs, isArray := reqRaw.([]any)
		if !isArray {
			if _, isBool := reqRaw.(bool); isBool {
				return fmt.Errorf("%s: required is a boolean (OpenRPC parameter semantics); object schemas need an array of property-name strings", typeName)
			}
			return fmt.Errorf("%s: required is %T, want an array of property-name strings", typeName, reqRaw)
		}
		for _, r := range reqs {
			s, ok := r.(string)
			if !ok {
				return fmt.Errorf("%s: required entry is not a string", typeName)
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

	// Nested types discovered under this struct are namespaced by it.
	savedPrefix := e.prefix
	e.prefix = typeName
	defer func() { e.prefix = savedPrefix }()

	writeDoc(body, typeName, schema)
	if keyword, members := unionMembers(schema); len(members) > 0 {
		fmt.Fprintf(body, "//\n// The schema also declares %d %s narrowings over these same\n// fields; they are not modeled as distinct types yet (phase 4).\n", len(members), keyword)
	}
	fmt.Fprintf(body, "type %s struct {\n", typeName)

	seenFields := map[string]string{}
	fieldNames := make(map[string]string, len(names))
	for _, name := range names {
		prop, ok := props[name].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: property %q is not an object", typeName, name)
		}
		if kw := checkTypeAffectingKeywords(prop); kw != "" {
			return fmt.Errorf("%s: property %q has keyword %q, which changes its shape and is not modeled yet (phase 4)", typeName, name, kw)
		}
		fieldName := GoName(name)
		if fieldName == "Validate" {
			return fmt.Errorf("%s: property %q sanitizes to Go field name %q, which collides with the generated Validate() method", typeName, name, fieldName)
		}
		if orig, dup := seenFields[fieldName]; dup {
			return fmt.Errorf("%s: properties %q and %q both sanitize to Go field name %q", typeName, orig, name, fieldName)
		}
		seenFields[fieldName] = name
		fieldNames[name] = fieldName

		if desc, _ := prop["description"].(string); desc != "" {
			fmt.Fprintf(body, "\t// %s\n", strings.ReplaceAll(desc, "\n", "\n\t// "))
		}
		if kws := unenforcedKeywords(prop); len(kws) > 0 {
			e.unenforced[name] = kws
			fmt.Fprintf(body, "\t//\n\t// Not enforced yet (phase 4): %s.\n", strings.Join(kws, ", "))
		}
		typ, err := e.goTypeExpr(prop, name)
		if err != nil {
			return err
		}
		if ref, hasRef := prop["$ref"].(string); hasRef {
			if was, degraded := e.degradedRefs[ref]; degraded {
				fmt.Fprintf(body, "\t//\n\t// Carried as raw JSON rather than %s: typing it would make\n\t// this package import the root package, which already imports\n\t// this one (Go forbids import cycles).\n", was)
			}
		}
		tag := name
		if !required[name] {
			typ, tag = "*"+typ, name+",omitempty"
		}
		fmt.Fprintf(body, "\t%s %s `json:%q`\n", fieldName, typ, tag)
	}
	fmt.Fprintf(body, "}\n")

	return renderValidate(e, body, typeName, schema, props, names, fieldNames, required)
}

// renderValidate emits the pattern vars and Validate method for a struct.
// Validate is always emitted, even with zero checks, so callers can rely on
// a uniform interface{ Validate() error } across every generated type.
func renderValidate(e *fileEmitter, body *strings.Builder, typeName string, schema map[string]any, props map[string]any, names []string, fieldNames map[string]string, required map[string]bool) error {
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
				// Keeps the emitter honest: a constraint on a shape whose
				// check we cannot express fails loudly instead of silently
				// emitting an unconstrained field.
				return fmt.Errorf("%s: property %q has string constraints but unsupported type %v (phase 4)", typeName, name, prop["type"])
			}
			continue
		}

		if hasML {
			mlRaw := prop["maxLength"]
			ml, ok := mlRaw.(float64)
			if !ok {
				return fmt.Errorf("%s: property %q maxLength is %T, want a number", typeName, name, mlRaw)
			}
			if ml < 0 || ml != math.Trunc(ml) {
				return fmt.Errorf("%s: property %q maxLength %v is not a non-negative integer", typeName, name, ml)
			}
			n := int(ml)
			msg := fmt.Sprintf("%s: exceeds maxLength %d", name, n)
			e.usesErrors, e.usesUtf8 = true, true
			// maxLength counts Unicode code points per JSON Schema, not bytes.
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
				return fmt.Errorf("%s: property %q pattern is %T, want a string", typeName, name, patRaw)
			}
			// RE2 gate: fail generation loudly rather than emit a MustCompile
			// that would panic at runtime.
			if _, err := regexp.Compile(pat); err != nil {
				return fmt.Errorf("%s: pattern %q for %q is not RE2-compatible: %v", typeName, pat, name, err)
			}
			e.usesErrors, e.usesRegexp, e.usesSync = true, true, true
			varName := fmt.Sprintf("pattern_%s_%s", typeName, fieldName)
			fmt.Fprintf(&patternVars, "var %s = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(%q) })\n\n", varName, pat)
			msg := fmt.Sprintf("%s: does not match pattern", name)
			// JSON Schema pattern is an unanchored search, so MatchString
			// (not a full-string match) is intentional.
			if isRequired {
				fmt.Fprintf(&checks, "\tif !%s().MatchString(v.%s) {\n\t\treturn errors.New(%q)\n\t}\n", varName, fieldName, msg)
			} else {
				fmt.Fprintf(&checks, "\tif v.%s != nil && !%s().MatchString(*v.%s) {\n\t\treturn errors.New(%q)\n\t}\n", fieldName, varName, fieldName, msg)
			}
		}
	}

	body.WriteString("\n")
	if patternVars.Len() > 0 {
		body.WriteString(patternVars.String())
	}
	body.WriteString("// Validate reports the first constraint violation, or nil.\n")
	fmt.Fprintf(body, "func (v *%s) Validate() error {\n", typeName)
	body.WriteString(checks.String())
	body.WriteString("\treturn nil\n}\n\n")
	return nil
}

// assembleFile prepends the generated header, package clause, and the
// import block the body actually needs, then gofmts the result.
func assembleFile(e *fileEmitter, relPath, pkg, specRef, body string) (string, error) {
	var out strings.Builder
	fmt.Fprintf(&out, "// Code generated by ucpgen. DO NOT EDIT.\n")
	fmt.Fprintf(&out, "// Source: %s (spec %s)\n\n", relPath, specRef)
	fmt.Fprintf(&out, "package %s\n\n", pkg)

	var imports []string
	if e.usesErrors {
		imports = append(imports, "errors")
	}
	if e.usesRegexp {
		imports = append(imports, "regexp")
	}
	if e.usesSync {
		imports = append(imports, "sync")
	}
	if e.usesUtf8 {
		imports = append(imports, "unicode/utf8")
	}
	imports = append(imports, e.sortedImports()...)
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

	out.WriteString(body)

	result, err := format.Source([]byte(out.String()))
	if err != nil {
		return "", fmt.Errorf("generated source does not parse: %w\n%s", err, out.String())
	}
	return string(result), nil
}
