package preprocess

import (
	"fmt"
	"strings"
)

// Preprocess normalizes an entire SchemaSet in place, mirroring the pass
// structure of the python-sdk driver (preprocess_schemas.py:636-710):
//
//	Phase 0  NormalizeMetadata          — ucp.json union, ucp-property refs
//	Pass 1a  FlattenDottedDefs +        — per file, ucp.json and generated
//	         PreprocessDocument           _request.json files excluded
//	Pass 1b  RewriteExternalDefsRefs    — cross-file renamed-def refs
//	Pass 1c  DiscoverVariantNeeds       — ucp_request markers
//	Pass 2   PropagateNeeds             — transitive, to fixpoint
//	Pass 3   GenerateVariants           — *_op_request.json into the set
//
// Errors are wrapped with the schema path, which is the only place in the
// preprocessor that knows it: the individual transforms operate on a single
// document and cannot name their own file.
func Preprocess(set *SchemaSet) error {
	NormalizeMetadata(set)

	ucp, ok := set.Files["ucp.json"]
	if !ok {
		return fmt.Errorf("ucp.json not found in schema set")
	}
	defs, _ := ucp["$defs"].(map[string]any)
	entityDef, _ := defs["entity"].(map[string]any)
	if len(entityDef) == 0 {
		return fmt.Errorf("entity definition not found: ucp.json must define $defs.entity")
	}
	// Resolve the entity's own same-document refs once, here, while it is
	// still in ucp.json and they still mean what they say. Every copy made
	// below is then self-contained. Deep-copied first so ucp.json's own
	// $defs.entity keeps its refs (preprocess_schemas.py, python-sdk#72).
	entityDef = CopyTree(entityDef).(map[string]any)
	ResolveLocalRefs(entityDef, ucp, nil)

	renames := map[string]map[string]string{}
	for _, rel := range set.Paths() {
		if skipPath(rel) {
			continue
		}
		if rm := FlattenDottedDefs(set.Files[rel]); len(rm) > 0 {
			renames[rel] = rm
		}
		if err := PreprocessDocument(set.Files[rel], entityDef); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
	}
	RewriteExternalDefsRefs(set, renames)

	needs := DiscoverVariantNeeds(set)
	PropagateNeeds(set, needs)
	GenerateVariants(set, needs)
	return nil
}

// skipPath reports whether a schema is excluded from the per-file
// flattening and document passes. Python uses substring checks on the
// absolute path for both (preprocess_schemas.py:677, :686, :692); ucp.json
// is metadata-normalized only, and generated variants are never reprocessed.
func skipPath(rel string) bool {
	return strings.Contains(rel, "ucp.json") || strings.Contains(rel, "_request.json")
}
