package preprocess

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRefNotFound classifies a $ref that could not be located at all:
// either it isn't a same-document pointer (no "#/" prefix — an external
// file ref) or the pointer path doesn't lead anywhere (a missing
// segment, or a segment that requires indexing into a non-object).
// Callers should treat this as python-sdk treats an unresolved ref: not
// an error condition, but something to defer to a later cross-file pass
// (preprocess_schemas.py:100-102).
var ErrRefNotFound = errors.New("ref not found")

// ErrRefNotObject classifies a $ref whose pointer path fully resolves,
// but to a JSON value that isn't an object (string, number, array,
// bool, null). python-sdk deep-copies the resolved value and then
// silently drops the branch when it isn't a dict rather than treating
// it as unresolved (preprocess_schemas.py:104-105); ErrRefNotObject is
// what lets callers tell the two cases apart.
var ErrRefNotObject = errors.New("ref target is not an object")

// ResolveLocalRef resolves a same-document JSON pointer such as
// "#/$defs/money" against root and returns the referenced object. The
// returned map is the live node inside root, not a copy: callers that
// mutate it mutate root's tree in place. Failures are always wrapped in
// either ErrRefNotFound or ErrRefNotObject so callers can distinguish
// "keep this ref around for later" from "this ref resolved but isn't
// mergeable" — see the two sentinels' docs.
func ResolveLocalRef(ref string, root map[string]any) (map[string]any, error) {
	frag, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil, fmt.Errorf("not a local ref: %q: %w", ref, ErrRefNotFound)
	}
	node := any(root)
	for _, part := range strings.Split(frag, "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		obj, ok := node.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q: %q is not an object: %w", ref, part, ErrRefNotFound)
		}
		node, ok = obj[part]
		if !ok {
			return nil, fmt.Errorf("%q: missing segment %q: %w", ref, part, ErrRefNotFound)
		}
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q: target is not an object: %w", ref, ErrRefNotObject)
	}
	return obj, nil
}

// ResolveLocalRefs recursively inlines same-document ("#/…") $refs inside
// fragment, in place, resolving each against root. It is the port of
// python-sdk's resolve_local_refs (d650f0b, PR #79, fixing python-sdk#72).
//
// It exists because entity inlining copies a definition's body into other
// documents. Any "#/…" ref inside that body is resolved against whatever
// document it currently sits in, so copying it silently re-points it —
// upstream left 24 refs to "#/$defs/version" dangling in capability.json,
// payment_handler.json and service.json. Resolving the body's own refs
// once, while it still sits in ucp.json, makes it self-contained and safe
// to copy anywhere.
//
// Three details are faithful to python rather than to Go taste, because
// goldens are byte-compared against that implementation's output:
//
//   - Keys alongside the $ref override the resolved target's keys, so a
//     local "description" survives inlining.
//   - An unresolvable ref is left untouched, not an error. python's
//     resolve_local_ref returns None there and the caller skips it.
//   - After substitution the walk continues into the new contents with the
//     CALLER's seen set, not the extended one. That is python's control
//     flow. It means a ref blocked as cyclic on the way down can be
//     resolved again on the way out, so a genuinely cyclic local ref would
//     recurse without bound — in python too. The entity body has no cycles,
//     and a cycle would hang the upstream preprocessor first, so mirroring
//     the behaviour keeps parity rather than quietly diverging from it.
func ResolveLocalRefs(fragment any, root map[string]any, seen map[string]bool) {
	switch t := fragment.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok && strings.HasPrefix(ref, "#/") && !seen[ref] {
			// ErrRefNotObject is skipped along with ErrRefNotFound: python
			// would deep-copy the non-object and then fail assigning into
			// it, so no corpus can rely on that path succeeding.
			if target, err := ResolveLocalRef(ref, root); err == nil {
				resolved := CopyTree(target).(map[string]any)
				next := make(map[string]bool, len(seen)+1)
				for k := range seen {
					next[k] = true
				}
				next[ref] = true
				ResolveLocalRefs(resolved, root, next)
				for k, v := range t {
					if k != "$ref" {
						resolved[k] = v
					}
				}
				clear(t)
				for k, v := range resolved {
					t[k] = v
				}
			}
		}
		for _, v := range t {
			ResolveLocalRefs(v, root, seen)
		}
	case []any:
		for _, item := range t {
			ResolveLocalRefs(item, root, seen)
		}
	}
}
