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

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
