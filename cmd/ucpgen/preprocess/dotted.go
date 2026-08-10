package preprocess

import (
	"path"
	"sort"
	"strings"
)

// FlattenDottedDefs renames $defs keys containing '.' (UCP reverse-DNS
// extension mount points) so downstream tooling doesn't treat dots as path
// separators (preprocess_schemas.py:325-368). Prefers the bare last dotted
// component; falls back to dot->underscore on collision; leaves the key
// unrenamed if both candidates collide. Local refs are rewritten; returns
// the rename map for the cross-file pass.
func FlattenDottedDefs(schema map[string]any) map[string]string {
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		return nil
	}
	existing := map[string]bool{}
	for k := range defs {
		existing[k] = true
	}
	renameMap := map[string]string{}
	names := make([]string, 0, len(defs))
	for k := range defs {
		names = append(names, k)
	}
	sort.Strings(names) // deterministic rename resolution order
	for _, old := range names {
		if !strings.Contains(old, ".") {
			continue
		}
		tail := old[strings.LastIndex(old, ".")+1:]
		var newName string
		if tail != "" && !existing[tail] {
			newName = tail
		} else {
			cand := strings.ReplaceAll(old, ".", "_")
			if existing[cand] {
				continue // both candidates collide; leave as-is (:356-357)
			}
			newName = cand
		}
		renameMap[old] = newName
		delete(existing, old)
		existing[newName] = true
	}
	for old, newName := range renameMap {
		defs[newName] = defs[old]
		delete(defs, old)
	}
	if len(renameMap) > 0 {
		rewriteLocalDefsRefs(schema, renameMap)
	}
	return renameMap
}

const defsPrefix = "#/$defs/"

func rewriteLocalDefsRefs(root map[string]any, renameMap map[string]string) {
	for _, n := range IterNodes(root) {
		m, ok := n.(map[string]any)
		if !ok {
			continue
		}
		ref, ok := m["$ref"].(string)
		if !ok || !strings.HasPrefix(ref, defsPrefix) {
			continue
		}
		rest := ref[len(defsPrefix):]
		name, tail, hasTail := strings.Cut(rest, "/")
		if newName, ok := renameMap[name]; ok {
			if hasTail {
				m["$ref"] = defsPrefix + newName + "/" + tail
			} else {
				m["$ref"] = defsPrefix + newName
			}
		}
	}
}

// RewriteExternalDefsRefs rewrites cross-file $defs refs whose target key
// was renamed in its home file (preprocess_schemas.py:288-322). renames is
// keyed by the target file's slash-relative path within the SchemaSet.
func RewriteExternalDefsRefs(set *SchemaSet, renames map[string]map[string]string) {
	for _, rel := range set.Paths() {
		schema := set.Files[rel]
		for _, n := range IterNodes(schema) {
			m, ok := n.(map[string]any)
			if !ok {
				continue
			}
			ref, ok := m["$ref"].(string)
			if !ok || !strings.Contains(ref, "#") {
				continue
			}
			filePart, frag, _ := strings.Cut(ref, "#")
			if filePart == "" || !strings.HasSuffix(filePart, ".json") {
				continue
			}
			const p = "/$defs/"
			if !strings.HasPrefix(frag, p) {
				continue
			}
			target := path.Join(path.Dir(rel), filePart)
			renameMap, ok := renames[target]
			if !ok {
				continue
			}
			rest := frag[len(p):]
			name, tail, hasTail := strings.Cut(rest, "/")
			if newName, ok := renameMap[name]; ok {
				newFrag := p + newName
				if hasTail {
					newFrag += "/" + tail
				}
				m["$ref"] = filePart + "#" + newFrag
			}
		}
	}
}
