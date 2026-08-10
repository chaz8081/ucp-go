package preprocess

import (
	"path"
	"sort"
	"strings"
)

// RequiredOps scans a schema's properties for ucp_request markers and
// returns the operations needing distinct request variants
// (preprocess_schemas.py:374-391). A string marker implies create+update;
// a dict marker contributes its keys. Order is unspecified; callers sort.
func RequiredOps(schema map[string]any) []string {
	props, _ := schema["properties"].(map[string]any)
	seen := map[string]bool{}
	for _, v := range props {
		data, ok := v.(map[string]any)
		if !ok {
			continue
		}
		switch m := data["ucp_request"].(type) {
		case string:
			seen["create"], seen["update"] = true, true
		case map[string]any:
			for op := range m {
				seen[op] = true
			}
		}
	}
	var out []string
	for op := range seen {
		out = append(out, op)
	}
	return out
}

// EvalPropInclusion decides whether a property is included and required for
// one operation, per the ucp_request rules (preprocess_schemas.py:394-423):
// string markers — "omit" excludes, "required"/"optional" override the base
// required list; dict markers — the op's value applies, and a missing op
// key or "omit" excludes.
func EvalPropInclusion(name string, data map[string]any, op string, baseRequired []any) (include, required bool) {
	inBase := false
	for _, r := range baseRequired {
		if r == name {
			inBase = true
			break
		}
	}
	if data == nil {
		return true, inBase
	}
	include, required = true, inBase
	switch m := data["ucp_request"].(type) {
	case string:
		switch m {
		case "omit":
			include = false
		case "required":
			required = true
		case "optional":
			required = false
		}
	case map[string]any:
		v, present := m[op]
		if v == "omit" || !present || v == nil {
			include = false
		} else if v == "required" {
			required = true
		} else if v == "optional" {
			required = false
		}
		// any other value: include stays true, required stays base — python parity
	}
	return include, required
}

// DiscoverVariantNeeds maps each schema path to the ops it explicitly
// requests via ucp_request markers (preprocess_schemas.py:697-700).
// ucp.json and generated _request.json files are skipped, matching the
// python pass-1c file filter.
func DiscoverVariantNeeds(set *SchemaSet) map[string]map[string]bool {
	needs := map[string]map[string]bool{}
	for _, rel := range set.Paths() {
		if strings.Contains(rel, "ucp.json") || strings.Contains(rel, "_request.json") {
			continue
		}
		ops := RequiredOps(set.Files[rel])
		if len(ops) > 0 {
			m := map[string]bool{}
			for _, op := range ops {
				m[op] = true
			}
			needs[rel] = m
		}
	}
	return needs
}

// externalPropertyRefs finds (propertyName, targetPath) pairs for every
// relative external file ref under a schema's properties
// (preprocess_schemas.py:584-598).
func externalPropertyRefs(rel string, schema map[string]any) [][2]string {
	var out [][2]string
	props, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, n := range IterNodes(props[name]) {
			m, ok := n.(map[string]any)
			if !ok {
				continue
			}
			if ref, ok := m["$ref"].(string); ok && !strings.Contains(ref, "#") {
				out = append(out, [2]string{name, path.Join(path.Dir(rel), ref)})
			}
		}
	}
	return out
}

// PropagateNeeds spreads variant needs transitively to fixpoint: a parent
// needing an op propagates it to every child schema referenced by a
// property that is included for that op (preprocess_schemas.py:601-630).
// ucp.json is skipped as a ref SOURCE (python pass 1c never extracts its
// refs, :691-695) but can receive needs — e.g. order.json's ucp property,
// truncated to a file-only ref by NormalizeMetadata, propagates ops onto
// ucp.json, from which python generates ucp_*_request.json variants.
func PropagateNeeds(set *SchemaSet, needs map[string]map[string]bool) {
	refs := map[string][][2]string{}
	for _, rel := range set.Paths() {
		if strings.Contains(rel, "ucp.json") || strings.Contains(rel, "_request.json") {
			continue
		}
		refs[rel] = externalPropertyRefs(rel, set.Files[rel])
	}
	for changed := true; changed; {
		changed = false
		for _, rel := range set.Paths() {
			ops, ok := needs[rel]
			if !ok {
				continue
			}
			schema := set.Files[rel]
			baseReq, _ := schema["required"].([]any)
			props, _ := schema["properties"].(map[string]any)
			opNames := make([]string, 0, len(ops))
			for op := range ops {
				opNames = append(opNames, op)
			}
			sort.Strings(opNames)
			for _, op := range opNames {
				for _, pair := range refs[rel] {
					propName, child := pair[0], pair[1]
					if _, exists := set.Files[child]; !exists {
						continue
					}
					data, _ := props[propName].(map[string]any)
					if include, _ := EvalPropInclusion(propName, data, op, baseReq); !include {
						continue
					}
					if needs[child] == nil {
						needs[child] = map[string]bool{}
					}
					if !needs[child][op] {
						needs[child][op] = true
						changed = true
					}
				}
			}
		}
	}
}

// GenerateVariants adds "<stem>_<op>_request.json" entries to the set for
// every needed (path, op) pair (preprocess_schemas.py:426-529). Sources are
// deep-copied; ucp_request markers stripped from included properties; refs
// into files that also need the op are rewritten to the variant filename.
// Never re-runs DistributeToBranches — python filters the deep copy without
// re-distributing, and re-distribution after filtering leaks stale base
// properties into branches.
func GenerateVariants(set *SchemaSet, needs map[string]map[string]bool) {
	paths := make([]string, 0, len(needs))
	for p := range needs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		ops := make([]string, 0, len(needs[rel]))
		for op := range needs[rel] {
			ops = append(ops, op)
		}
		sort.Strings(ops)
		for _, op := range ops {
			variant := CopyTree(set.Files[rel]).(map[string]any)
			applyVariantIdentity(variant, op, stemOf(rel))
			if variant["type"] == "array" {
				if items, ok := variant["items"].(map[string]any); ok {
					for _, n := range IterNodes(items) {
						if m, ok := n.(map[string]any); ok {
							if _, has := m["properties"]; has {
								applyRequestRules(m, op, rel, needs)
							}
						}
					}
				}
			} else if _, hasProps := variant["properties"]; hasProps || variant["type"] == "object" {
				applyRequestRules(variant, op, rel, needs)
			}
			out := strings.TrimSuffix(rel, ".json") + "_" + op + "_request.json"
			set.Files[out] = variant
		}
	}
}

func stemOf(rel string) string {
	base := path.Base(rel)
	return strings.TrimSuffix(base, path.Ext(base))
}

// applyVariantIdentity retitles the variant and renames its $id file part
// (preprocess_schemas.py:426-441).
func applyVariantIdentity(variant map[string]any, op, stem string) {
	baseTitle, _ := variant["title"].(string)
	if baseTitle == "" {
		baseTitle = stem
	}
	variant["title"] = baseTitle + " " + strings.ToUpper(op[:1]) + op[1:] + " Request"
	if id, ok := variant["$id"].(string); ok && strings.Contains(id, "/") {
		slash := strings.LastIndex(id, "/")
		name := id[slash+1:]
		stemPart, ext, hasExt := strings.Cut(name, ".")
		if !hasExt {
			ext = "json"
		}
		variant["$id"] = id[:slash+1] + stemPart + "_" + op + "_request." + ext
	}
}

// applyRequestRules filters an object schema's properties for one op,
// strips markers, and rewrites child refs to variants
// (preprocess_schemas.py:464-490, 444-461).
func applyRequestRules(obj map[string]any, op, rel string, needs map[string]map[string]bool) {
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return
	}
	baseReq, _ := obj["required"].([]any)
	newProps := map[string]any{}
	newReq := []any{}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data, _ := props[name].(map[string]any)
		include, required := EvalPropInclusion(name, data, op, baseReq)
		if !include {
			continue
		}
		if data != nil {
			delete(data, "ucp_request")
			rewriteRefsToVariants(data, op, rel, needs)
		}
		newProps[name] = props[name]
		if required {
			newReq = append(newReq, name)
		}
	}
	obj["properties"] = newProps
	obj["required"] = newReq
}

func rewriteRefsToVariants(root map[string]any, op, rel string, needs map[string]map[string]bool) {
	for _, n := range IterNodes(root) {
		m, ok := n.(map[string]any)
		if !ok {
			continue
		}
		ref, ok := m["$ref"].(string)
		if !ok || strings.Contains(ref, "#") {
			continue
		}
		target := path.Join(path.Dir(rel), ref)
		if ops, ok := needs[target]; ok && ops[op] {
			m["$ref"] = strings.TrimSuffix(ref, ".json") + "_" + op + "_request.json"
		}
	}
}
