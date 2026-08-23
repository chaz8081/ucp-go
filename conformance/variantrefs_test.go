package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Upstream python-sdk#34: variant generation rewrites external $refs only
// inside `properties`, so a schema whose alternatives sit in a top-level
// oneOf/anyOf/allOf keeps refs pointing at the base (response) files. The
// generated request variant then wraps response types.
//
// ucp-go reproduces this exactly, and deliberately: preprocessor parity is
// byte-for-byte, so upstream's preprocessing bugs are ours until upstream
// fixes them. Diverging unilaterally would break the parity that makes the
// committed goldens trustworthy.
//
// **The differential harness cannot catch this class.** Validate and the
// oracle both read the same preprocessed schema, so both are wrong the same
// way and agree. Zero disagreements is true here and says nothing. Only a
// comparison against the SOURCE spec — which is what preprocessor parity
// is — can see it, and parity reports a match because we faithfully
// reproduce the defect.
//
// So this test is the notification mechanism. It pins the exact set, which
// makes upstream's fix arrive as a build failure telling us to port, rather
// than as something we notice on the next manual sweep.
func TestVariantUnionRefsStillPointAtBaseSchemas(t *testing.T) {
	// file -> keyword -> refs, for variant schemas whose top-level union
	// branches reference a base file that HAS a request variant of its own.
	//
	// The six message_* refs are absent from this set because
	// message_error/info/warning have no request variants to point at — but
	// that is a consequence of the same defect, not an exemption from it.
	// Variant need is propagated by scanning for external refs, and that
	// scan skips top-level composition keywords, so nothing ever marks those
	// three as needing a variant. Upstream's fix creates them (fourteen new
	// schemas in all), at which point the os.Stat check below starts
	// matching and this test reports message_create_request and
	// message_update_request as newly affected. That failure is the signal,
	// not a false alarm.
	want := map[string][]string{
		"shopping/types/fulfillment_destination_create_request.json": {
			"oneOf retail_location.json", "oneOf shipping_destination.json",
		},
		"shopping/types/fulfillment_destination_update_request.json": {
			"oneOf retail_location.json", "oneOf shipping_destination.json",
		},
		"shopping/types/shipping_destination_create_request.json": {
			"allOf postal_address.json",
		},
		"shopping/types/shipping_destination_update_request.json": {
			"allOf postal_address.json",
		},
	}

	got := map[string][]string{}
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
				// Only a defect if the referenced base actually has the
				// corresponding variant to point at.
				variant := strings.TrimSuffix(ref, ".json") + "_" + op + "_request.json"
				if _, err := os.Stat(filepath.Join(filepath.Dir(path), variant)); err != nil {
					continue
				}
				got[filepath.ToSlash(rel)] = append(got[filepath.ToSlash(rel)], kw+" "+ref)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for k := range got {
		sort.Strings(got[k])
	}
	for k := range want {
		sort.Strings(want[k])
	}

	for file, refs := range want {
		if _, ok := got[file]; !ok {
			t.Errorf("%s no longer carries base-pointing refs.\n"+
				"If python-sdk#34 has been fixed upstream, re-pin the goldens and "+
				"port it: the generated request variants will start wrapping "+
				"request types, which is a breaking change worth a release note.", file)
			continue
		}
		if strings.Join(got[file], ",") != strings.Join(refs, ",") {
			t.Errorf("%s: refs changed\n  got  %v\n  want %v", file, got[file], refs)
		}
		delete(got, file)
	}
	for file, refs := range got {
		t.Errorf("%s: base-pointing refs appeared in a file not previously affected: %v\n"+
			"Either the corpus grew a new case of python-sdk#34, or variant "+
			"generation regressed.", file, refs)
	}
}
