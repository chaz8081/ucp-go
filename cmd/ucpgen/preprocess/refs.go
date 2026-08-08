package preprocess

import (
	"fmt"
	"strings"
)

// ResolveLocalRef resolves a same-document JSON pointer such as
// "#/$defs/money" against root and returns the referenced object. The
// returned map is the live node inside root, not a copy: callers that
// mutate it mutate root's tree in place.
func ResolveLocalRef(ref string, root map[string]any) (map[string]any, error) {
	frag, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil, fmt.Errorf("not a local ref: %q", ref)
	}
	node := any(root)
	for _, part := range strings.Split(frag, "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		obj, ok := node.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q: %q is not an object", ref, part)
		}
		node, ok = obj[part]
		if !ok {
			return nil, fmt.Errorf("%q: missing segment %q", ref, part)
		}
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q: target is not an object", ref)
	}
	return obj, nil
}
