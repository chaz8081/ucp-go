package emit

import (
	"sort"
	"strings"
)

// PackageGraph is a package-level import graph: source package -> the set
// of packages it references.
type PackageGraph map[string]map[string]bool

// BuildPackageGraph derives the package dependency graph the emitted code
// would have, by resolving every $ref in the corpus.
//
// propertyNames subschemas are excluded: they constrain an object's keys,
// which are always Go strings, so they produce no type and therefore no
// import. Including them would invent 12 edges that the generated code
// never has.
func BuildPackageGraph(files map[string]map[string]any, idx *TypeIndex, modulePath string) PackageGraph {
	g := PackageGraph{}
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		// Import paths, not package names: see the note in BuildTypeIndex.
		// A break computed for one "types" package must not be applied to a
		// different directory that happens to share the basename.
		_, src := PackageForSchema(rel, modulePath)
		if g[src] == nil {
			g[src] = map[string]bool{}
		}
		for _, ref := range collectRefs(files[rel]) {
			target, err := ResolveRef(idx, rel, ref)
			if err != nil {
				continue // unresolvable refs are reported during rendering
			}
			if target.ImportPath != src {
				g[src][target.ImportPath] = true
			}
		}
	}
	return g
}

// nonTypeProducingKeys hold subschemas the emitter never turns into a Go
// type, so a $ref inside them creates no import. Including them would
// invent package edges the generated code does not have — and a phantom
// edge that closes a cycle would make CycleBreaks degrade a real,
// perfectly typeable field to raw JSON.
var nonTypeProducingKeys = map[string]bool{
	"$ref":              true, // handled by the caller
	"propertyNames":     true, // constrains map keys, which are always strings
	"if":                true,
	"then":              true,
	"else":              true,
	"not":               true,
	"contains":          true,
	"dependentSchemas":  true,
	"patternProperties": true,
}

// collectRefs walks a schema and returns every $ref it contains, skipping
// subtrees that produce no Go type.
func collectRefs(node any) []string {
	var out []string
	switch t := node.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok {
			out = append(out, ref)
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if nonTypeProducingKeys[k] {
				continue
			}
			out = append(out, collectRefs(t[k])...)
		}
	case []any:
		for _, v := range t {
			out = append(out, collectRefs(v)...)
		}
	}
	return out
}

// packageDepth is the directory nesting of a package, used to decide which
// edge of a cycle to break. "ucp" (the schema-tree root) is depth 0.
func packageDepth(files map[string]map[string]any, modulePath, pkg string) int {
	depth := -1
	for rel := range files {
		_, p := PackageForSchema(rel, modulePath)
		if p != pkg {
			continue
		}
		d := strings.Count(rel, "/")
		if d > depth {
			depth = d
		}
	}
	if depth < 0 {
		return 0
	}
	return depth
}

// CycleBreaks returns the package edges that must be degraded — typed as
// raw JSON rather than as the referenced type — for the emitted package
// graph to be acyclic.
//
// Go forbids import cycles; JSON Schema has no such restriction, so a
// corpus can legitimately describe mutually referencing groups. When a
// cycle exists, the edge running from the more deeply nested package to
// the shallower one is broken, since the shallower package is the more
// natural dependency root. The real corpus contains exactly one cycle:
// shopping/types/error_response.json's `ucp` property points at the root
// metadata union, while the root (through payment_handler.json) points down
// into shopping/types. The broken edge is therefore types -> ucp.
func CycleBreaks(g PackageGraph, files map[string]map[string]any, modulePath string) map[string]map[string]bool {
	breaks := map[string]map[string]bool{}

	srcs := make([]string, 0, len(g))
	for s := range g {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)

	// Repeatedly find a cycle and break its deepest-to-shallowest edge until
	// the graph is acyclic. The corpus has one cycle, but the loop keeps the
	// rule total rather than special-cased.
	for {
		cycle := findCycle(g, srcs)
		if cycle == nil {
			return breaks
		}
		// bestDepth starts below any real depth so the first edge always
		// wins the comparison, making the `from < bestFrom` tie-break
		// meaningful only between genuine peers.
		bestFrom, bestTo, bestDepth := "", "", -1
		for i, from := range cycle {
			to := cycle[(i+1)%len(cycle)]
			d := packageDepth(files, modulePath, from)
			if d > bestDepth || (d == bestDepth && from < bestFrom) {
				bestFrom, bestTo, bestDepth = from, to, d
			}
		}
		if breaks[bestFrom] == nil {
			breaks[bestFrom] = map[string]bool{}
		}
		breaks[bestFrom][bestTo] = true
		delete(g[bestFrom], bestTo)
	}
}

// findCycle returns the packages forming a cycle, or nil when none remains.
func findCycle(g PackageGraph, srcs []string) []string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var found []string

	var visit func(string) bool
	visit = func(n string) bool {
		color[n] = grey
		stack = append(stack, n)
		nexts := make([]string, 0, len(g[n]))
		for m := range g[n] {
			nexts = append(nexts, m)
		}
		sort.Strings(nexts)
		for _, m := range nexts {
			switch color[m] {
			case grey:
				for i, s := range stack {
					if s == m {
						found = append([]string(nil), stack[i:]...)
						break
					}
				}
				return true
			case white:
				if visit(m) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}

	for _, s := range srcs {
		if color[s] == white {
			stack = stack[:0]
			if visit(s) {
				return found
			}
		}
	}
	return nil
}
