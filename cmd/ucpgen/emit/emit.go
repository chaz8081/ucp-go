// Package emit renders normalized UCP schemas as Go source.
package emit

import (
	"encoding/json"
	"fmt"
	"go/format"
	"path"
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
	// format is annotation-only in draft 2020-12 and the conformance oracle
	// runs with assertFormat off, so not enforcing it is correct — but 104
	// occurrences (94 uri, 10 date-time) disappearing without a trace is
	// not. Reported like any other unenforced keyword.
	"format": true,
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
// in node that no generated check covers. They are reported in the
// generated output rather than enforced.
//
// A keyword stays in validationOnlyKeywords even once the compiler can
// check it, because whether it IS checked depends on where it appears: an
// enum on a property becomes a real check, while the same enum inside an
// `if` branch or an unmodeled anyOf member does not. Subtracting per node,
// rather than removing the keyword from the set outright, is what keeps
// those positions visible instead of silently unenforced.
func (e *fileEmitter) unenforcedKeywords(node map[string]any) []string {
	seen := map[string]bool{}
	for k := range node {
		if validationOnlyKeywords[k] && !e.enforced.has(node, k) {
			seen[k] = true
		}
	}
	// Conditional branches survive the merge as an allOf residual, because
	// they are rules rather than fields. Their keywords are part of this
	// node's coverage gap and nothing else would report them — total.json's
	// two amount rules would otherwise vanish from both the doc comment and
	// the manifest, which is precisely what this accounting exists to stop.
	if branches, ok := node["allOf"].([]any); ok {
		for _, b := range branches {
			bm, isObj := b.(map[string]any)
			if !isObj {
				continue
			}
			for k := range bm {
				if validationOnlyKeywords[k] {
					seen[k] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
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
	// Kept so a union can compare its members' bodies, which means resolving
	// local $refs against the document rather than against the node.
	e.doc = schema

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

	src, err := assembleFile(e, relPath, pkg, specRef, body.String())
	if err != nil {
		return "", err
	}
	lastUnenforced = e.Unenforced()
	return src, nil
}

// lastUnenforced carries the unenforced-keyword map from the most recent
// EmitFileWithBreaks call, so the caller can record it in the manifest
// without threading a second return value through every call site.
var lastUnenforced map[string][]string

// LastUnenforced returns the validation-only keywords the most recently
// emitted file declares but does not check, keyed by "Type" or
// "Type.field-path".
func LastUnenforced() map[string][]string { return lastUnenforced }

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
		// A conditional branch is a rule, not a set of fields, so the
		// preprocessor deliberately leaves it in the allOf rather than
		// folding it in. That residual is expected and carries no fields to
		// lose; conditional evaluation is unimplemented, and the keywords
		// are reported as such. Anything else remaining here is unresolved
		// inheritance, which silently drops real fields — see the comment
		// above — and must still fail.
		for _, b := range residual {
			bm, isObj := b.(map[string]any)
			if !isObj || !preprocess.HasConditional(bm) {
				return fmt.Errorf("allOf branches remain unresolved after merging: %v", residual)
			}
		}
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
	// Constraints are compiled before anything is written, so the doc
	// comment below reports only the keywords no check ended up covering.
	var c constraintSet
	if err := compileConstraints(e, &c, typeName, "", aliasExpr(underlying), accessValue, schema); err != nil {
		return err
	}

	writeDoc(e, body, typeName, schema)
	if keyword, members := unionMembers(schema); len(members) > 0 {
		fmt.Fprintf(body, "//\n// This schema is a %s of %d alternatives with no shared\n// properties, so it is carried as raw JSON. Typed alternatives are\n// phase 4.\n", keyword, len(members))
	}
	// A named primitive often exists precisely for its constraint —
	// ReverseDomainName is a string whose whole purpose is its pattern — so
	// an unenforced constraint here is the most misleading place to omit
	// the note.
	if kws := e.unenforcedKeywords(schema); len(kws) > 0 {
		e.unenforced[typeName] = schema
		fmt.Fprintf(body, "//\n// Not enforced yet (phase 4): %s.\n", strings.Join(kws, ", "))
	}
	fmt.Fprintf(body, "type %s %s\n\n", typeName, underlying)

	// A named primitive usually exists for its constraint, so emitting an
	// empty Validate here would leave it unenforced precisely where it
	// carries the most meaning.
	if c.vars.Len() > 0 {
		body.WriteString(c.vars.String())
	}
	fmt.Fprintf(body, "// Validate reports the first constraint violation, or nil.\nfunc (v *%s) Validate() error {\n%s\treturn nil\n}\n\n", typeName, c.checks.String())
	return nil
}

// aliasExpr reaches a named type's own value from its Validate receiver.
// The receiver is a distinct type from the one it is defined over, so a
// stdlib check has to see it converted back.
func aliasExpr(underlying string) string {
	if strings.HasPrefix(underlying, "[]") || strings.HasPrefix(underlying, "map[") {
		// A conversion is unnecessary for a composite: len and range work on
		// the named type directly, and `[]T(*v)` would just add noise.
		return "*v"
	}
	return underlying + "(*v)"
}

// indistinguishableMembers returns the names of union members whose
// schemas are structurally identical, sorted, or nil when every member can
// be told from every other.
//
// Only local `#/$defs/...` members are compared, which is where the corpus
// puts them; a cross-file member is left alone rather than guessed at, so
// the check can miss a duplicate but never invent one. Titles and
// descriptions are ignored: they are what make these three members look
// different while validating identically.
func indistinguishableMembers(e *fileEmitter, members []any) []string {
	defs, _ := e.doc["$defs"].(map[string]any)
	if defs == nil {
		return nil
	}
	byShape := map[string][]string{}
	for _, m := range members {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		ref, _ := mm["$ref"].(string)
		name, ok := strings.CutPrefix(ref, "#"+defsFragmentPrefix)
		if !ok || strings.Contains(name, "/") {
			continue
		}
		def, ok := defs[name].(map[string]any)
		if !ok {
			continue
		}
		body := map[string]any{}
		for k, v := range def {
			if k == "title" || k == "description" {
				continue
			}
			body[k] = v
		}
		raw, err := json.Marshal(body)
		if err != nil {
			continue
		}
		byShape[string(raw)] = append(byShape[string(raw)], name)
	}
	var dupes []string
	for _, names := range byShape {
		if len(names) > 1 {
			dupes = append(dupes, names...)
		}
	}
	sort.Strings(dupes)
	return dupes
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

	writeDoc(e, body, typeName, schema)
	// "Exactly one field is set" describes the Go struct, not the schema.
	// Under oneOf the schema demands exclusivity too, and Validate enforces
	// it; under anyOf the input may satisfy several alternatives and the
	// first that validates is the one held. Saying "exactly one" for both
	// would read as an exclusivity claim anyOf does not make.
	fmt.Fprintf(body, "//\n// %s is a closed %s union: one field is set, holding the\n", typeName, keyword)
	if keyword == "oneOf" {
		body.WriteString("// alternative the input matched. The schema requires that exactly one\n// alternative match.\n")
	} else {
		body.WriteString("// first alternative that accepted the input. The schema permits more\n// than one to match.\n")
	}

	// oneOf means "exactly one", which is only decidable when the
	// alternatives can be told apart. ucp.json's synthesized metadata union
	// has three members — the cart, catalog and order response profiles —
	// whose bodies are identical apart from their titles, so any instance
	// matching one matches all three and the union can never be satisfied.
	// Enforcing exclusivity there would make Cart, Checkout and Order
	// permanently invalid, since `ucp` is required on all three.
	//
	// Where the schema is unsatisfiable as written, the exclusivity check is
	// dropped and the union behaves as anyOf. The deviation is emitted into
	// the generated source rather than left implicit.
	exclusive := keyword == "oneOf"
	if dupes := indistinguishableMembers(e, members); exclusive && len(dupes) > 0 {
		exclusive = false
		fmt.Fprintf(body, "//\n// NOTE: this schema declares oneOf, but these alternatives are\n// structurally identical:\n//\n")
		for _, name := range dupes {
			fmt.Fprintf(body, "//   - %s\n", name)
		}
		body.WriteString("//\n// No input can satisfy exactly one of them, so the schema is\n// unsatisfiable as written. Exclusivity is therefore not enforced for\n// this union, which behaves as anyOf.\n")
	}

	fmt.Fprintf(body, "type %s struct {\n", typeName)
	for _, f := range fields {
		fmt.Fprintf(body, "\t%s *%s `json:\"-\"`\n", f.field, f.typ)
	}
	if exclusive {
		body.WriteString("\n\t// matched counts the alternatives the decoded input satisfied.\n")
		body.WriteString("\t// oneOf permits exactly one, so more than one is a violation that\n")
		body.WriteString("\t// only decoding can observe. Zero means this value was never\n")
		body.WriteString("\t// decoded from JSON.\n")
		body.WriteString("\tmatched int\n")
	}
	body.WriteString("}\n\n")

	// A member that merely decodes is not a match: Go's decoder accepts any
	// object into a struct whose fields are all optional, so the first
	// member would almost always win regardless of what the JSON says. The
	// member that also validates is the one the schema means, and decoding
	// falls back to the first that parsed only so that the bytes are not
	// lost — Validate then reports why they are wrong.
	fmt.Fprintf(body, "// UnmarshalJSON decodes the union member that accepts the input,\n// preferring one that also validates.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	fmt.Fprintf(body, "\tvar matched, fallback %s\n\tmatches := 0\n\tparsed := false\n", typeName)
	for _, f := range fields {
		fmt.Fprintf(body, "\tvar as%s %s\n\tif err := json.Unmarshal(data, &as%s); err == nil {\n", f.field, f.typ, f.field)
		fmt.Fprintf(body, "\t\tif as%s.Validate() == nil {\n\t\t\tif matches == 0 {\n\t\t\t\tmatched = %s{%s: &as%s}\n\t\t\t}\n\t\t\tmatches++\n\t\t}\n", f.field, typeName, f.field, f.field)
		fmt.Fprintf(body, "\t\tif !parsed {\n\t\t\tfallback, parsed = %s{%s: &as%s}, true\n\t\t}\n\t}\n", typeName, f.field, f.field)
	}
	body.WriteString("\tif matches > 0 {\n\t\t*v = matched\n")
	if exclusive {
		body.WriteString("\t\tv.matched = matches\n")
	}
	body.WriteString("\t\treturn nil\n\t}\n")
	body.WriteString("\tif parsed {\n\t\t*v = fallback\n")
	if exclusive {
		// Zero would mean "never decoded", which this value was; one match
		// is what a caller must reach, and the member's own Validate is what
		// reports why it did not.
		body.WriteString("\t\tv.matched = 1\n")
	}
	body.WriteString("\t\treturn nil\n\t}\n")
	fmt.Fprintf(body, "\treturn errors.New(%q)\n}\n\n", typeName+": no union member accepted the input")

	// MarshalJSON: emit whichever member is set.
	fmt.Fprintf(body, "// MarshalJSON encodes whichever union member is set.\nfunc (v %s) MarshalJSON() ([]byte, error) {\n", typeName)
	for _, f := range fields {
		fmt.Fprintf(body, "\tif v.%s != nil {\n\t\treturn json.Marshal(v.%s)\n\t}\n", f.field, f.field)
	}
	fmt.Fprintf(body, "\treturn nil, errors.New(%q)\n}\n\n", typeName+": no union member is set")

	// A union is valid exactly when the alternative it holds is, so Validate
	// delegates. Checking nothing here would accept any object at all: the
	// members are the only place the union's constraints live.
	fmt.Fprintf(body, "// Validate reports the first constraint violation, or nil.\nfunc (v *%s) Validate() error {\n", typeName)
	if exclusive {
		fmt.Fprintf(body, "\tif v.matched > 1 {\n\t\treturn errors.New(%q)\n\t}\n",
			typeName+": input satisfies more than one alternative, and oneOf permits exactly one")
	}
	for _, f := range fields {
		fmt.Fprintf(body, "\tif v.%s != nil {\n\t\treturn v.%s.Validate()\n\t}\n", f.field, f.field)
	}
	// A decoded union always holds a member, so reaching here means the
	// value was built in Go and left empty — indistinguishable from one
	// that was never provided, and judged the same way: not at all.
	body.WriteString("\treturn nil\n}\n\n")
	return nil
}

// writeDoc writes the doc comment for a named type.
//
// Most schemas describe themselves and those words are used verbatim. 44
// types in the corpus carry no description at all; rather than leave them
// bare in godoc, the fallback names the schema they came from, which is
// where a reader has to go for the meaning anyway.
func writeDoc(e *fileEmitter, body *strings.Builder, typeName string, schema map[string]any) {
	if desc, _ := schema["description"].(string); desc != "" {
		fmt.Fprintf(body, "// %s %s\n", typeName, strings.ReplaceAll(desc, "\n", "\n// "))
		return
	}
	fmt.Fprintf(body, "// %s is generated from %s.\n", typeName, e.rel)
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

	// Types are resolved for every property first, because the constraint
	// compiler needs to know whether a field is a pointer before it can
	// emit a check against it — and the constraints in turn have to be
	// compiled before any doc comment is written, so that a comment reports
	// only the keywords no check ended up covering.
	seenFields := map[string]string{}
	fieldNames := make(map[string]string, len(names))
	fieldTypes := make(map[string]string, len(names))
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

		typ, err := e.goTypeExpr(prop, name)
		if err != nil {
			return err
		}
		// omitzero, not omitempty: on a slice or map, omitempty omits both
		// nil and empty, so an absent field and a present-but-empty one
		// serialize identically and the distinction is lost. omitzero omits
		// only the zero value, which recovers it — verified: []string(nil)
		// omits, []string{} emits "[]".
		//
		// That makes a pointer unnecessary for types that are already
		// nilable. Slices, maps and json.RawMessage keep their natural shape
		// so callers write c.Links[0] rather than (*c.Links)[0]; scalars
		// still need the pointer, since a non-pointer string cannot
		// distinguish absent from "".
		if !required[name] && !isNilableType(typ) {
			typ = "*" + typ
		}
		fieldTypes[name] = typ
	}

	// UCP is an extension-first protocol: an open object exists so that
	// extensions can contribute keys the base schema never lists. Without a
	// catch-all, decoding drops every such key and re-encoding cannot
	// restore it — signals.json's own description says the type exists so
	// "multiple extensions contribute to the shared namespace".
	open := isOpenObject(schema)

	var c constraintSet
	for _, name := range names {
		prop := props[name].(map[string]any)
		if err := compileConstraints(e, &c, typeName, name, "v."+fieldNames[name],
			accessFor(fieldTypes[name], required[name]), prop); err != nil {
			return err
		}
	}
	fields := make([]structField, 0, len(names))
	for _, name := range names {
		fields = append(fields, structField{jsonName: name, goName: fieldNames[name], required: required[name]})
		compileNested(&c, fieldNames[name], fieldTypes[name])
	}
	if err := compileObjectSelf(e, &c, typeName, schema, fields, open); err != nil {
		return err
	}

	writeDoc(e, body, typeName, schema)
	if keyword, members := unionMembers(schema); len(members) > 0 {
		fmt.Fprintf(body, "//\n// The schema also declares %d %s variants that narrow or replace\n// these fields; they are not modeled as distinct types yet (phase 4),\n// so this type reflects only the shared base.\n", len(members), keyword)
	}
	if kws := e.unenforcedKeywords(schema); len(kws) > 0 {
		e.unenforced[typeName] = schema
		fmt.Fprintf(body, "//\n// Not enforced yet (phase 4) on the object itself: %s.\n", strings.Join(kws, ", "))
	}
	fmt.Fprintf(body, "type %s struct {\n", typeName)

	for _, name := range names {
		prop := props[name].(map[string]any)
		if desc, _ := prop["description"].(string); desc != "" {
			fmt.Fprintf(body, "\t// %s\n", strings.ReplaceAll(desc, "\n", "\n\t// "))
		}
		if kws := e.unenforcedKeywords(prop); len(kws) > 0 {
			e.unenforced[name] = prop
			fmt.Fprintf(body, "\t//\n\t// Not enforced yet (phase 4): %s.\n", strings.Join(kws, ", "))
		}
		if ref, hasRef := prop["$ref"].(string); hasRef {
			if was, degraded := e.degradedRefs[ref]; degraded {
				fmt.Fprintf(body, "\t//\n\t// Carried as raw JSON rather than %s: typing it would make\n\t// this package import the root package, which already imports\n\t// this one (Go forbids import cycles).\n", was)
			}
		}
		tag := name
		if !required[name] {
			tag = name + ",omitzero"
		}
		fmt.Fprintf(body, "\t%s %s `json:%q`\n", fieldNames[name], fieldTypes[name], tag)
	}
	if open {
		e.imports["encoding/json"] = "json"
		body.WriteString("\n\t// Extra holds properties the schema does not name. The schema is\n\t// open (additionalProperties is not false), so extension keys are\n\t// preserved here and re-emitted on marshal rather than dropped.\n")
		body.WriteString("\tExtra map[string]json.RawMessage `json:\"-\"`\n")
	}
	req := requiredNames(fields)
	if len(req) > 0 {
		e.imports["encoding/json"] = "json"
		renderPresenceField(body)
	}
	fmt.Fprintf(body, "}\n")

	switch {
	case open:
		// One UnmarshalJSON per type: an open object's decoder carries the
		// presence capture rather than getting a second one of its own.
		renderExtraCodec(body, typeName, names, fieldNames, req)
	case len(req) > 0:
		renderPresenceCodec(body, typeName, req)
	}

	compilePresenceChecks(e, &c, req)
	renderValidate(body, typeName, &c)
	return nil
}

// isNilableType reports whether a Go type expression already has a nil
// zero value, making a pointer redundant for optionality.
func isNilableType(typ string) bool {
	return strings.HasPrefix(typ, "[]") ||
		strings.HasPrefix(typ, "map[") ||
		typ == "json.RawMessage" ||
		typ == "any"
}

// isOpenObject reports whether a schema admits properties it does not
// name. JSON Schema objects are open by default: only an explicit
// additionalProperties:false closes them. A schema-valued
// additionalProperties is a map type and is handled elsewhere.
func isOpenObject(schema map[string]any) bool {
	switch ap := schema["additionalProperties"].(type) {
	case bool:
		return ap
	case map[string]any:
		return false
	default:
		return true
	}
}

// renderExtraCodec emits UnmarshalJSON/MarshalJSON that route unknown keys
// through Extra. The struct's own fields are decoded via an alias type,
// which drops the custom methods and so avoids infinite recursion.
func renderExtraCodec(body *strings.Builder, typeName string, names []string, fieldNames map[string]string, required []string) {
	alias := typeName + "Alias"
	fmt.Fprintf(body, "\n// UnmarshalJSON decodes the named properties and keeps everything else\n// in Extra.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	fmt.Fprintf(body, "\ttype %s %s\n\tvar named %s\n\tif err := json.Unmarshal(data, &named); err != nil {\n\t\treturn err\n\t}\n\t*v = %s(named)\n\n", alias, typeName, alias, typeName)
	body.WriteString("\tvar all map[string]json.RawMessage\n\tif err := json.Unmarshal(data, &all); err != nil {\n\t\treturn err\n\t}\n")
	// Presence is captured before the named keys are removed below, since
	// that is what makes a key disappear from `all`.
	if len(required) > 0 {
		renderPresenceCapture(body, required)
	}
	for _, name := range names {
		fmt.Fprintf(body, "\tdelete(all, %q)\n", name)
	}
	body.WriteString("\tif len(all) > 0 {\n\t\tv.Extra = all\n\t}\n\treturn nil\n}\n")

	fmt.Fprintf(body, "\n// MarshalJSON emits the named properties alongside anything held in\n// Extra.\nfunc (v %s) MarshalJSON() ([]byte, error) {\n", typeName)
	fmt.Fprintf(body, "\ttype %s %s\n\tnamed, err := json.Marshal(%s(v))\n\tif err != nil {\n\t\treturn nil, err\n\t}\n", alias, typeName, alias)
	body.WriteString("\tif len(v.Extra) == 0 {\n\t\treturn named, nil\n\t}\n")
	body.WriteString("\tvar merged map[string]json.RawMessage\n\tif err := json.Unmarshal(named, &merged); err != nil {\n\t\treturn nil, err\n\t}\n")
	body.WriteString("\tif merged == nil {\n\t\tmerged = map[string]json.RawMessage{}\n\t}\n")
	body.WriteString("\tfor k, val := range v.Extra {\n\t\tif _, named := merged[k]; !named {\n\t\t\tmerged[k] = val\n\t\t}\n\t}\n\treturn json.Marshal(merged)\n}\n")
}

// renderValidate emits the pattern vars and Validate method for a struct
// from constraints its caller has already compiled. Validate is always
// emitted, even with zero checks, so callers can rely on a uniform
// interface{ Validate() error } across every generated type.
func renderValidate(body *strings.Builder, typeName string, c *constraintSet) {
	body.WriteString("\n")
	if c.vars.Len() > 0 {
		body.WriteString(c.vars.String())
	}
	body.WriteString("// Validate reports the first constraint violation, or nil.\n")
	fmt.Fprintf(body, "func (v *%s) Validate() error {\n", typeName)
	body.WriteString(c.presence.String())
	body.WriteString(c.checks.String())
	body.WriteString(c.nested.String())
	body.WriteString("\treturn nil\n}\n\n")
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
