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
