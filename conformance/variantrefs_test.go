package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Upstream python-sdk#34, fixed in python-sdk 51bf73c (PR #83) and ported
// here: variant generation used to rewrite external $refs only inside
// `properties`, so a schema whose alternatives sit in a top-level
// oneOf/anyOf/allOf kept refs pointing at the base (response) files and the
// generated request variant wrapped response types.
//
// This test used to pin the broken set so the fix would arrive as a build
// failure. It did exactly that, and now guards the other direction: no
// variant may reference a base schema that has a request variant of its
// own. A regression in ref rewriting, or a new schema shaped like
// fulfillment_destination, fails here.
//
// **The differential harness cannot cover this.** Validate and the oracle
// read the same preprocessed schema, so a wrong ref makes both wrong
// identically and they agree. Zero disagreements says nothing about it.
// Only comparison against the source spec — preprocessor parity — sees it,
// and parity reports a match whenever we faithfully reproduce upstream,
// defect and all. That is why this check is written out separately.
func TestVariantRefsPointAtRequestVariants(t *testing.T) {
	var offenders []string
	root := filepath.Join("..", "goldens", goldenVersion)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, "_request.json") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		op := "update"
		if strings.Contains(rel, "_create_request") {
			op = "create"
		}
		for _, kw := range []string{"oneOf", "anyOf", "allOf"} {
			branches, _ := doc[kw].([]any)
			for _, b := range branches {
				bm, ok := b.(map[string]any)
				if !ok {
					continue
				}
				ref, _ := bm["$ref"].(string)
				if !strings.HasSuffix(ref, ".json") || strings.Contains(ref, "_request.json") {
					continue
				}
				// Pointing at a base is only wrong when the matching
				// variant exists to be pointed at.
				variant := strings.TrimSuffix(ref, ".json") + "_" + op + "_request.json"
				if _, err := os.Stat(filepath.Join(filepath.Dir(path), variant)); err != nil {
					continue
				}
				offenders = append(offenders, filepath.ToSlash(rel)+": "+kw+" -> "+ref+" (want "+variant+")")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(offenders)
	for _, o := range offenders {
		t.Errorf("request variant references a base schema that has its own variant:\n  %s\n"+
			"This is python-sdk#34 recurring. A request variant wrapping a response type "+
			"forces callers to supply server-assigned fields the request form drops.", o)
	}
}
