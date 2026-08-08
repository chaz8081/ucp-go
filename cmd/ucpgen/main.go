// Command ucpgen generates Go models from UCP JSON Schemas.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chaz8081/ucp-go/cmd/ucpgen/emit"
	"github.com/chaz8081/ucp-go/cmd/ucpgen/preprocess"
)

// Manifest maps every consumed schema file to its emitted Go type.
// Generation fails closed: a schema file with no manifest entry is an error.
type Manifest struct {
	SpecRef string                   `json:"spec_ref"`
	Schemas map[string]ManifestEntry `json:"schemas"`
}

type ManifestEntry struct {
	Type    string `json:"type"`
	Package string `json:"package"`
	File    string `json:"file"`
	// Fields is the number of emitted struct properties. Some real spec
	// files (catalog_lookup, catalog_search, attribution,
	// fulfillment_destination, message, pagination) are legitimately
	// content-free at the top level — their content lives entirely in
	// $defs — so a top-level object schema can emit a valid, empty
	// struct. Zero is not an error; it's recorded here so those cases
	// stay visible in the manifest instead of looking identical to a
	// normal type.
	Fields int `json:"fields"`
}

func run(schemaDir, outDir, specRef string) (*Manifest, error) {
	set, err := preprocess.LoadSchemas(schemaDir)
	if err != nil {
		return nil, err
	}

	// Iterate in sorted order: map range order is randomized by the Go
	// runtime, and while the emitted files are independent of each other
	// and MarshalIndent below sorts the manifest's keys regardless, an
	// error path is not independent of order — which file's error
	// surfaces first (and therefore aborts the run) would otherwise be
	// nondeterministic. The project requires deterministic output.
	rels := make([]string, 0, len(set.Files))
	for rel := range set.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	m := &Manifest{SpecRef: specRef, Schemas: map[string]ManifestEntry{}}
	for _, rel := range rels {
		schema := set.Files[rel]
		if err := preprocess.MergeAllOf(schema, schema); err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		pkg := "ucp"
		if dir := filepath.Dir(rel); dir != "." {
			pkg = strings.ReplaceAll(dir, "/", "") // shopping/types -> shoppingtypes; refined in phase 2
		}
		src, err := emit.EmitFile(pkg, rel, schema, specRef)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		goPath := filepath.Join(outDir, strings.TrimSuffix(rel, ".json")+".go")
		if err := os.MkdirAll(filepath.Dir(goPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(goPath, []byte(src), 0o644); err != nil {
			return nil, err
		}
		title, _ := schema["title"].(string)
		// GoName(title) directly, matching EmitFile's own derivation
		// (EmitFile computes typeName := GoName(title) from this same
		// post-MergeAllOf schema) — GoName re-cases and re-splits every
		// part itself, so pre-lowering or pre-underscoring the title
		// first would be redundant, not merely equivalent-by-luck.
		typeName := emit.GoName(title)
		// properties was already validated as either absent or a proper
		// object by EmitFile above (it fails loudly otherwise), so this
		// assertion is safe here without re-checking the ok value.
		props, _ := schema["properties"].(map[string]any)
		m.Schemas[rel] = ManifestEntry{
			Type:    typeName,
			Package: pkg,
			File:    filepath.ToSlash(goPath),
			Fields:  len(props),
		}
	}
	// Fail closed: every loaded schema must have produced an entry. With
	// the loop above, every schema either emits (and gets an entry) or
	// errors out (and aborts the whole run) — so this can't currently
	// trigger. It's kept as a safety net against a future refactor (e.g.
	// a "skip on error" mode) that stops guaranteeing that invariant.
	for rel := range set.Files {
		if _, ok := m.Schemas[rel]; !ok {
			return nil, fmt.Errorf("coverage gap: %s produced no emitted type", rel)
		}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return m, os.WriteFile(filepath.Join(outDir, "MANIFEST.json"), append(raw, '\n'), 0o644)
}

func main() {
	schemas := flag.String("schemas", "", "path to spec schemas root")
	out := flag.String("out", ".", "output root")
	specRef := flag.String("spec-ref", "unknown", "spec branch@sha provenance")
	flag.Parse()
	if *schemas == "" {
		log.Fatal("-schemas is required")
	}
	if _, err := run(*schemas, *out, *specRef); err != nil {
		log.Fatal(err)
	}
}
