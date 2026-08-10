package preprocess

import "testing"

func TestLoadSchemas(t *testing.T) {
	set, err := LoadSchemas("testdata/schemas")
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	s, ok := set.Files["test/link.json"]
	if !ok {
		t.Fatalf("missing test/link.json; got keys %v", keys(set.Files))
	}
	if got := s["title"]; got != "Link" {
		t.Errorf("title = %v, want Link", got)
	}
}

func TestLoadSchemasSkipsRequestFiles(t *testing.T) {
	set, err := LoadSchemas("testdata/schemas")
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	// python-sdk skips *_request.json by basename at load time
	// (preprocess_schemas.py:653): these are generated variant outputs
	// (Task 8 writes them into the set later), and a real-tree load must
	// never re-ingest generated output as if it were source.
	if _, ok := set.Files["test/foo_create_request.json"]; ok {
		t.Error("LoadSchemas must skip *_request.json files, got test/foo_create_request.json in the set")
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
