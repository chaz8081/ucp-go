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
	// Unenforced records validation-only JSON Schema keywords this schema
	// declares that the generated code does not check but should, keyed by
	// "Type" or "Type.field-path". It makes the coverage gap machine
	// readable rather than only a comment in the generated source.
	Unenforced map[string][]string `json:"unenforced,omitempty"`
	// NotAsserted records keywords this schema declares that the corpus's
	// dialect defines as ANNOTATIONS, so not checking them is conformant.
	// Separate from Unenforced on purpose: merging the two would report
	// correct behaviour as a shortfall, and `format` outnumbers the real
	// gap by more than twenty to one.
	NotAsserted map[string][]string `json:"not_asserted,omitempty"`
}

const modulePath = "github.com/chaz8081/ucp-go"

func run(schemaDir, outDir, specRef string) (*Manifest, error) {
	// Variants are content here, not output to be regenerated: emit's input
	// is the already-normalized tree, where *_request.json files are 67 of
	// the corpus's 145 schemas and carry the protocol's entire request
	// surface. Skipping them (as a raw-spec load must) drops them with no
	// error, and the coverage check below cannot notice because they were
	// never in the set.
	set, err := preprocess.LoadSchemasIncludingVariants(schemaDir)
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

	// The index must cover the whole corpus before any file renders, so a
	// cross-file $ref can resolve to a type that has not been emitted yet.
	idx, err := emit.BuildTypeIndex(set.Files, modulePath)
	if err != nil {
		return nil, err
	}

	// Go forbids import cycles; decide up front which reference edges must
	// be carried as raw JSON so the emitted packages form a DAG.
	breaks := emit.CycleBreaks(emit.BuildPackageGraph(set.Files, idx, modulePath), set.Files, modulePath)

	m := &Manifest{SpecRef: specRef, Schemas: map[string]ManifestEntry{}}
	for _, rel := range rels {
		// Input contract: schemas are already normalized. The `preprocess`
		// subcommand owns every transform now, so emit does no merging of
		// its own — a root-level MergeAllOf here would be redundant with
		// PreprocessDocument's whole-document walk and would silently
		// diverge from it for nested nodes.
		schema := set.Files[rel]
		pkg, importPath := emit.PackageForSchema(rel, modulePath)
		src, err := emit.EmitFileWithBreaks(idx, modulePath, rel, schema, specRef, breaks[importPath], set.Files)
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
			// Relative to outDir, not goPath's absolute/outDir-prefixed
			// form: regenerating into a scratch dir and diffing the
			// resulting MANIFEST.json against a committed one must not
			// spuriously fail just because outDir's location differs.
			File:        filepath.ToSlash(strings.TrimSuffix(rel, ".json") + ".go"),
			Fields:      len(props),
			Unenforced:  emit.LastUnenforced(),
			NotAsserted: emit.LastNotAsserted(),
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

// writeCanonicalSet writes every schema in the set to outDir as canonical
// JSON, preserving the set's relative paths.
func writeCanonicalSet(set *preprocess.SchemaSet, outDir string) error {
	for _, rel := range set.Paths() {
		raw, err := preprocess.CanonicalJSON(set.Files[rel])
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		dst := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, append(raw, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// runPreprocess loads the raw spec schemas, normalizes the whole set, and
// writes the canonical result — the input to the emit stage and the form
// compared against the committed goldens.
func runPreprocess(schemaDir, outDir string) error {
	set, err := preprocess.LoadSchemas(schemaDir)
	if err != nil {
		return err
	}
	if err := preprocess.Preprocess(set); err != nil {
		return err
	}
	return writeCanonicalSet(set, outDir)
}

// runCanonicalize rewrites an already-preprocessed schema tree through the
// same canonical encoder without transforming it. It exists so goldens
// produced by the official python preprocessor can be compared against Go
// output without formatting differences masquerading as content ones.
func runCanonicalize(schemaDir, outDir string) error {
	// Variants are included here: the input is an already-preprocessed tree
	// whose *_request.json files are content to canonicalize, not generated
	// output to be skipped.
	set, err := preprocess.LoadSchemasIncludingVariants(schemaDir)
	if err != nil {
		return err
	}
	return writeCanonicalSet(set, outDir)
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: ucpgen <preprocess|emit|canonicalize> [flags]")
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "preprocess":
		fs := flag.NewFlagSet("preprocess", flag.ExitOnError)
		schemas := fs.String("schemas", "", "path to spec schemas root")
		outSchemas := fs.String("out-schemas", "", "directory to write normalized schemas")
		fs.Parse(args)
		if *schemas == "" || *outSchemas == "" {
			log.Fatal("-schemas and -out-schemas are required")
		}
		if err := runPreprocess(*schemas, *outSchemas); err != nil {
			log.Fatal(err)
		}
	case "emit":
		fs := flag.NewFlagSet("emit", flag.ExitOnError)
		schemas := fs.String("schemas", "", "path to normalized schemas root")
		out := fs.String("out", ".", "output root")
		specRef := fs.String("spec-ref", "unknown", "spec branch@sha provenance")
		fs.Parse(args)
		if *schemas == "" {
			log.Fatal("-schemas is required")
		}
		if _, err := run(*schemas, *out, *specRef); err != nil {
			log.Fatal(err)
		}
	case "canonicalize":
		fs := flag.NewFlagSet("canonicalize", flag.ExitOnError)
		schemas := fs.String("schemas", "", "path to already-preprocessed schemas")
		outSchemas := fs.String("out-schemas", "", "directory to write canonical copies")
		fs.Parse(args)
		if *schemas == "" || *outSchemas == "" {
			log.Fatal("-schemas and -out-schemas are required")
		}
		if err := runCanonicalize(*schemas, *outSchemas); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown subcommand %q (want preprocess, emit, or canonicalize)", cmd)
	}
}
