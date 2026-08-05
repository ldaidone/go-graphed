// Package analyzer takes the flat list of parsed Documents and
// assembles them into a Graph, including cross-reference links
// inferred by heuristic rules.  Keeping heuristics here (rather
// than in each parser) means the same link logic applies
// regardless of which language extracted the entities.
package analyzer

import (
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// Build maps the parsed document slice into the final ir.Graph
// structure and connects them with inferred links.  Returning a
// Graph (rather than writing to a file) keeps the analyzer
// side-effect-free so it can be tested with in-memory fixtures.
func Build(docs []ir.Document) (ir.Graph, error) {
	graph := ir.Graph{
		Documents: make(map[string]*ir.Document),
		Links:     make([]ir.Link, 0),
	}

	// 1. Map all documents by their Path so the analyzer can
	//    resolve source/target references in O(1).
	for i := range docs {
		doc := &docs[i]
		graph.Documents[doc.Path] = doc
	}

	// 2. Build a global registry of all discovered Entities.
	//    Keying by Name (rather than ID) lets us do substring
	//    matching for the naming-convention heuristic below.
	globalEntities := make(map[string]ir.Entity)
	for _, doc := range graph.Documents {
		for _, entity := range doc.Entities {
			globalEntities[entity.Name] = entity
		}
	}

	// 3. Connect the Dots -- run every heuristic pass over the
	//    full document set.  Each pass is self-contained so new
	//    rules can be added without touching existing ones.
	for _, doc := range graph.Documents {

		// A. Implicit Code-to-Code Links: Does a struct name end
		//    with an interface name?  This is a rough heuristic for
		//    Go's implicit interface satisfaction -- e.g., a struct
		//    called "GraphBuilder" likely implements an interface
		//    called "Builder".  The 0.8 weight signals confidence
		//    is high but not absolute (naming coincidences exist).
		for _, entity := range doc.Entities {
			if entity.Type == "struct" {
				for globalName, globalEntity := range globalEntities {
					if globalEntity.Type == "interface" && strings.HasSuffix(entity.Name, globalName) {
						graph.Links = append(graph.Links, ir.Link{
							SourceID:   entity.ID,
							TargetID:   globalEntity.ID,
							Type:       "implements",
							Weight:     0.8,
							SourceType: ir.LinkSourceInferred,
							Metadata: map[string]string{
								"rule": "naming_convention_heuristic",
							},
						})
					}
				}
			}
		}

		// B. Unstructured Context Links: If a Markdown or PDF
		//    document's file path contains an entity name, link
		//    them as "documents".  This is a stand-in for full-text
		//    search -- when file content parsing is added, the same
		//    pattern can scan body text instead of just the path.
		if doc.Format == "markdown" || doc.Format == "pdf" {
			for name, entity := range globalEntities {
				if strings.Contains(strings.ToLower(doc.Path), strings.ToLower(name)) {
					// A filename substring match is a heuristic, not a
					// parsed fact: it gets a low weight and an "inferred"
					// provenance so consumers rank it below AST edges.
					graph.Links = append(graph.Links, ir.Link{
						SourceID:   doc.Path,
						TargetID:   entity.ID,
						Type:       "documents",
						Weight:     0.6,
						SourceType: ir.LinkSourceInferred,
						Metadata: map[string]string{
							"rule": "path_keyword_match",
						},
					})
				}
			}
		}

		// C. Document-internal links: parsers can attach links they
		// discovered inside a single file (e.g. a Go call graph).
		// These are lifted onto the graph as-is; the analyzer does
		// not re-infer or re-weight them.
		graph.Links = append(graph.Links, doc.Links...)
	}

	// 4. Cross-file package indexing: aggregate Go files that declare the
	// same package and resolve "imports" between indexed directories.
	// This pass only fires for documents the parser actually tagged with
	// a "package" entity, so hand-built fixtures stay untouched.
	graph.Packages = make(map[string]*ir.Package)

	// Package dirs are absolute on disk while import statements use
	// module-relative paths, so derive the shared project root to translate
	// between the two spellings during import resolution.
	root := goPackageRoot(graph.Documents)

	// First pass: group files by the package clause the parser extracted.
	for _, doc := range graph.Documents {
		if doc.Format != "golang" {
			continue
		}
		pkgDir, pkgName := goPackageDir(doc)
		if pkgDir == "" {
			continue
		}
		pkg, ok := graph.Packages[pkgDir]
		if !ok {
			pkg = &ir.Package{Name: pkgName, Path: pkgDir}
			graph.Packages[pkgDir] = pkg
		}
		if pkg.Name == "" {
			pkg.Name = pkgName
		}
		pkg.Files = append(pkg.Files, doc.Path)
		graph.Links = append(graph.Links, ir.Link{
			SourceID:   doc.Path,
			TargetID:   ir.PackageNodeID(pkgDir),
			Type:       "part_of",
			Weight:     1.0,
			SourceType: ir.LinkSourceExtracted,
			Metadata:   map[string]string{"rule": "package_clause"},
		})
	}

	// Second pass: resolve each import statement onto an indexed package
	// directory. The import statement itself is a parsed fact, but mapping
	// it to a local package dir uses suffix matching, so the edge is tagged
	// inferred with a lower weight than a direct AST edge.
	for _, doc := range graph.Documents {
		if doc.Format != "golang" {
			continue
		}
		srcDir, _ := goPackageDir(doc)
		srcPkg := graph.Packages[srcDir]
		for _, entity := range doc.Entities {
			if entity.Type != "import" {
				continue
			}
			depDir := resolveImportDir(entity.Name, graph.Packages, root)
			if depDir == "" {
				continue
			}
			if depPkg := graph.Packages[depDir]; depPkg != nil && depPkg.ImportPath == "" {
				depPkg.ImportPath = entity.Name
			}
			if srcPkg != nil && !containsString(srcPkg.Imports, depDir) {
				srcPkg.Imports = append(srcPkg.Imports, depDir)
			}
			graph.Links = append(graph.Links, ir.Link{
				SourceID:   doc.Path,
				TargetID:   ir.PackageNodeID(depDir),
				Type:       "imports",
				Weight:     0.8,
				SourceType: ir.LinkSourceInferred,
				Metadata: map[string]string{
					"rule":        "import_resolution",
					"import_path": entity.Name,
				},
			})
		}
	}

	// 4b. Cross-file JS/TS module resolution: map import specifiers emitted
	// by the JS-family parsers onto indexed files so a JS project gets real
	// cross-file "imports" edges (the Go pass above does the equivalent via
	// package dirs). Relative specifiers resolve by exact path with the full
	// JS-family extension set; bare specifiers use src-root and longest-suffix
	// matching to cover tsconfig "paths"-style aliases. Specifiers that do
	// not resolve to an indexed file (e.g. node_modules packages) are left
	// unlinked rather than emitting dangling edges.
	jsIndex := make(map[string]string)
	for docPath, doc := range graph.Documents {
		if !isJSFamily(doc.Format) {
			continue
		}
		jsIndex[stripJSExt(docPath)] = docPath
	}
	for _, doc := range graph.Documents {
		if !isJSFamily(doc.Format) {
			continue
		}
		for _, entity := range doc.Entities {
			if entity.Type != "import" {
				continue
			}
			target, exact := resolveJSImport(entity.Name, doc.Path, jsIndex)
			if target == "" || target == doc.Path {
				continue
			}
			// Exact relative resolution follows documented module semantics
			// (extension order + index fallback), so it is a parsed fact with
			// full weight. Alias/suffix matches are heuristics and weigh less.
			weight := 0.8
			sourceType := ir.LinkSourceInferred
			resolution := "suffix_match"
			if exact {
				weight = 1.0
				sourceType = ir.LinkSourceExtracted
				resolution = "exact"
			}
			graph.Links = append(graph.Links, ir.Link{
				SourceID:   doc.Path,
				TargetID:   target,
				Type:       "imports",
				Weight:     weight,
				SourceType: sourceType,
				Metadata: map[string]string{
					"rule":        "module_resolution",
					"import_path": entity.Name,
					"resolution":  resolution,
				},
			})
		}
	}

	// 5. Enforce the provenance invariant: any link that reached the
	// final graph without an explicit SourceType (e.g. lifted from a
	// fixture or a hand-built document) is defaulted to "extracted".
	// Heuristic links tagged "inferred" by the passes above are
	// left untouched.
	graph.Links = ir.NormalizeLinks(graph.Links)

	// 6. Group documents into higher-level domain clusters: by directory
	// tree, by Go package, and by network coupling. The clusters are a
	// side table (not graph nodes) so topological traversal stays unchanged
	// while consumers get a subsystem-level view.
	annotateClusters(&graph)

	// 7. Compute global centrality ("God Node") metrics over the same
	// document-level coupling graph and flag hub documents. Like clusters,
	// the metrics are a side table, so traversal stays unchanged while
	// consumers get a per-document view of architectural importance.
	annotateMetrics(&graph)

	// Record when the snapshot was assembled so consumers can detect
	// documents modified after the build.
	graph.BuiltAt = time.Now()

	return graph, nil
}

// goPackageDir extracts the package directory and name a document belongs
// to from the parser-emitted "package" entity. Returns "", "" when the
// document carries no such entity (e.g. hand-built fixtures).
func goPackageDir(doc *ir.Document) (string, string) {
	for _, entity := range doc.Entities {
		if entity.Type == "package" {
			dir := entity.Metadata["package_path"]
			if dir == "" {
				dir = filepath.Dir(doc.Path)
			}
			return dir, entity.Name
		}
	}
	return "", ""
}

// goPackageRoot returns the longest directory prefix shared by every Go
// document that carries a package entity. Absolute scanner paths collapse
// onto the project root; relative fixture paths collapse onto their common
// ancestor (possibly ""). The root is used to express package dirs in their
// module-relative form so they can be matched against import statements.
func goPackageRoot(documents map[string]*ir.Document) string {
	root := ""
	first := true
	for _, doc := range documents {
		if doc.Format != "golang" {
			continue
		}
		dir, _ := goPackageDir(doc)
		if dir == "" {
			continue
		}
		if first {
			root = dir
			first = false
			continue
		}
		root = commonDirPrefix(root, dir)
	}
	return root
}

// commonDirPrefix returns the longest common path prefix of a and b,
// aligned on "/" segment boundaries.
func commonDirPrefix(a, b string) string {
	as := strings.Split(a, "/")
	bs := strings.Split(b, "/")
	i := 0
	for i < len(as) && i < len(bs) && as[i] == bs[i] {
		i++
	}
	return strings.Join(as[:i], "/")
}

// relPath expresses dir relative to root when dir lives under it; otherwise
// dir is returned unchanged.
func relPath(dir, root string) string {
	if root == "" || root == dir {
		return dir
	}
	return strings.TrimPrefix(dir, root+"/")
}

// resolveImportDir maps an import path onto an indexed package directory.
// Exact matches win; otherwise the longest matching path suffix is used so
// e.g. "github.com/org/proj/internal/ir" resolves to the locally indexed
// "internal/ir" directory. Both on-disk and module-relative spellings of the
// package dir are considered.
func resolveImportDir(importPath string, packages map[string]*ir.Package, root string) string {
	if importPath == "" {
		return ""
	}
	if _, ok := packages[importPath]; ok {
		return importPath
	}
	bestDir, bestLen := "", 0
	for dir := range packages {
		candidates := []string{dir}
		if rel := relPath(dir, root); rel != "" && rel != dir {
			candidates = append(candidates, rel)
		}
		for _, c := range candidates {
			if len(c) > bestLen && strings.HasSuffix(importPath, "/"+c) {
				bestDir, bestLen = dir, len(c)
			}
		}
	}
	return bestDir
}

// containsString reports whether s is present in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// jsExtensions are the file suffixes the JS-family import resolver tries, in
// priority order, when a relative specifier omits one.
var jsExtensions = []string{
	".js", ".jsx", ".mjs", ".cjs",
	".ts", ".tsx", ".mts", ".cts",
}

// isJSFamily reports whether a format key belongs to the JS/TS family whose
// parsers emit "import" entities with the raw module specifier as the name.
func isJSFamily(format string) bool {
	switch format {
	case "javascript", "typescript", "tsx":
		return true
	}
	return false
}

// stripJSExt removes the trailing JS-family extension from a file path,
// leaving the path in a form that import specifiers can be matched against
// ("/src/components/App.js" -> "/src/components/App").
func stripJSExt(p string) string {
	base := p
	for _, ext := range jsExtensions {
		if strings.HasSuffix(p, ext) {
			base = p[:len(p)-len(ext)]
			break
		}
	}
	return base
}

// resolveJSImport maps an import specifier onto an indexed JS-family file.
// Relative specifiers ("./x", "../y") resolve exactly against the importing
// file's directory, trying every JS-family extension plus index-file
// fallbacks; the second return value reports whether that resolution was
// exact (relative) versus heuristic. Bare/alias specifiers try a "src/" root,
// the specifier itself, then a longest-suffix match so "components/App"
// reaches "src/components/App.tsx" without needing tsconfig parsing -- these
// are heuristic and report exact=false.
func resolveJSImport(specifier, srcPath string, jsIndex map[string]string) (string, bool) {
	if specifier == "" {
		return "", false
	}
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
		base := path.Join(path.Dir(srcPath), specifier)
		for _, ext := range jsExtensions {
			if target, ok := jsIndex[stripJSExt(base+ext)]; ok {
				return target, true
			}
		}
		for _, ext := range jsExtensions {
			if target, ok := jsIndex[stripJSExt(path.Join(base, "index"+ext))]; ok {
				return target, true
			}
		}
		return "", false
	}

	// Bare/alias specifiers: "@/components/App" and "~/lib/x" are common
	// tsconfig alias forms -- strip the leading sigil and try the remainder.
	trimmed := specifier
	for _, sigil := range []string{"@/", "~/", "#/"} {
		if strings.HasPrefix(specifier, sigil) {
			trimmed = strings.TrimPrefix(specifier, sigil)
			break
		}
	}
	for _, candidate := range []string{path.Join("src", trimmed), trimmed} {
		if target, ok := jsIndex[stripJSExt(candidate)]; ok {
			return target, false
		}
	}
	best, bestLen := "", 0
	for key, target := range jsIndex {
		if len(key) > bestLen && strings.HasSuffix(key, "/"+trimmed) {
			best, bestLen = target, len(key)
		}
	}
	if best == "" {
		return "", false
	}
	return best, false
}
