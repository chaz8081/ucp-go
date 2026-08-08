// Package preprocess normalizes UCP JSON Schemas ahead of code generation,
// porting the transformations of the official python-sdk preprocessor.
package preprocess

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SchemaSet holds every schema file keyed by its path relative to the
// schemas root, using forward slashes (e.g. "shopping/checkout.json").
type SchemaSet struct {
	Root  string
	Files map[string]map[string]any
}

// LoadSchemas reads every .json file under root into a SchemaSet.
func LoadSchemas(root string) (*SchemaSet, error) {
	set := &SchemaSet{Root: root, Files: map[string]map[string]any{}}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		set.Files[filepath.ToSlash(rel)] = doc
		return nil
	})
	if err != nil {
		return nil, err
	}
	return set, nil
}
