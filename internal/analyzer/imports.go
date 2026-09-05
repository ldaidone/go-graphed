package analyzer

import (
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// importShape describes how one language family spells cross-file
// imports so the generic resolver can turn an import entity name into
// on-disk file paths without language-specific passes.  The shapes are
// the only per-language knowledge the resolver needs; the resolution
// algorithm itself is shared by every format.
type importShape struct {
	// ext lists the file extensions tried when a candidate path lacks
	// one, in preference order.
	ext []string

	// delims are the separators that split module-style import names
	// (Java/Kotlin dots, Rust "::", PHP backslashes) into path segments.
	delims []string

	// sigils are bare-specifier prefixes stripped before resolution
	// (JS/TS "…/" aliases), e.g. "@/", "~/".
	sigils []string

	// roots are virtual roots tried for bare specifiers before falling
	// back to suffix matching, e.g. "src" for tsconfig-style aliases.
	roots []string

	// entryFiles are module-index file names whose directory can stand
	// for the import target (index.js, __init__.py, mod.rs).
	entryFiles []string

	// dropLast marks imports whose final segment is a symbol rather than
	// a file or module (Rust "use crate::shapes::Circle"): the resolver
	// additionally tries the segments minus the last.
	dropLast bool

	// snake snake_cases the segments before matching, for grammars whose
	// module names use PascalCase but whose files are snake_case
	// (Elixir "Geometry.Shapes" lives in geometry/shapes.ex).
	snake bool
}

// jsFamilyExts is the shared extension set for the JS/TS family; a
// .js file may import a .tsx module, so every JS-family format tries the
// whole set.
var jsFamilyExts = []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"}

// jsFamilyEntryFiles are the module-index files whose directory stands
// for an import target.
var jsFamilyEntryFiles = []string{
	"index.js", "index.jsx", "index.mjs", "index.cjs",
	"index.ts", "index.tsx", "index.mts", "index.cts",
}

// importShapes keys every format that participates in cross-file import
// resolution to its import shape.  Formats without an entry (Go, which
// resolves through its package index; Markdown/PDF, which link by path
// keywords; and the markup/config formats with no module system) are
// skipped by the generic pass.
var importShapes = map[string]importShape{
	"javascript": {ext: jsFamilyExts, sigils: []string{"@/", "~/", "#/"}, roots: []string{"src"}, entryFiles: jsFamilyEntryFiles},
	"typescript": {ext: jsFamilyExts, sigils: []string{"@/", "~/", "#/"}, roots: []string{"src"}, entryFiles: jsFamilyEntryFiles},
	"tsx":        {ext: jsFamilyExts, sigils: []string{"@/", "~/", "#/"}, roots: []string{"src"}, entryFiles: jsFamilyEntryFiles},
	"python":     {ext: []string{".py"}, delims: []string{"."}},
	"kotlin":     {ext: []string{".kt", ".kts"}, delims: []string{"."}},
	"java":       {ext: []string{".java"}, delims: []string{"."}},
	"csharp":     {ext: []string{".cs"}, delims: []string{"."}},
	"php":        {ext: []string{".php"}, delims: []string{"\\"}},
	"ruby":       {ext: []string{".rb"}, delims: []string{"."}},
	"elixir":     {ext: []string{".ex", ".exs"}, delims: []string{"."}, snake: true},
	"rust":       {ext: []string{".rs"}, delims: []string{"::"}, dropLast: true},
	"swift":      {ext: []string{".swift"}, delims: []string{"."}},
	"c":          {ext: []string{".c", ".h"}},
	"cpp":        {ext: []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx", ".h"}},
}

// fileIndex maps every indexed document onto the path keys the generic
// resolver matches against: the exact path and every "/"-separated
// suffix of its extension-stripped form.
type fileIndex struct {
	// exact maps a full (cleaned) path -- extension included -- to the
	// document path, so imports that name a file literally
	// ("shapes.h", "./data.json") resolve directly.
	exact map[string]string

	// keyed maps a path suffix (extension stripped) to the documents
	// that end with it, so "service/OrderService" reaches
	// "…/service/OrderService.kt" and "package" reaches
	// "package/__init__.py".
	keyed map[string][]string
}

// buildFileIndex indexes every graph document for path-key resolution.
func buildFileIndex(graph *ir.Graph) *fileIndex {
	idx := &fileIndex{
		exact: make(map[string]string, len(graph.Documents)),
		keyed: make(map[string][]string, len(graph.Documents)),
	}
	for p := range graph.Documents {
		clean := path.Clean(p)
		idx.exact[clean] = p
		stripped := strings.TrimSuffix(clean, path.Ext(clean))
		if stripped == "" || stripped == clean {
			continue
		}
		segs := strings.Split(stripped, "/")
		for i := range segs {
			key := strings.Join(segs[i:], "/")
			idx.keyed[key] = append(idx.keyed[key], p)
		}
	}
	return idx
}

// pick returns the deterministic best document for a stripped-key hit,
// preferring paths whose extension belongs to the import shape.
func (idx *fileIndex) pick(paths []string, shape importShape) string {
	inShape := make([]string, 0, len(paths))
	for _, p := range paths {
		if extIn(path.Ext(p), shape.ext) {
			inShape = append(inShape, p)
		}
	}
	pool := paths
	if len(inShape) > 0 {
		pool = inShape
	}
	sort.Strings(pool)
	return pool[0]
}

// tryCandidate resolves one candidate relative path against the index:
// an exact path wins; otherwise a candidate without an extension matches
// the suffix keys (with extension fill-in happening implicitly through
// the stripped-key index).
func (idx *fileIndex) tryCandidate(rel string, shape importShape) string {
	if t, ok := idx.exact[rel]; ok {
		return t
	}
	if path.Ext(rel) != "" {
		return ""
	}
	if paths, ok := idx.keyed[rel]; ok {
		return idx.pick(paths, shape)
	}
	return ""
}

// resolveRelative resolves a "./x"-style import against the importing
// file's directory, trying the exact path, then each extension, then
// module-index entry files.
func (idx *fileIndex) resolveRelative(base string, shape importShape) string {
	if t, ok := idx.exact[base]; ok {
		return t
	}
	for _, e := range shape.ext {
		if t, ok := idx.exact[base+e]; ok {
			return t
		}
	}
	for _, ef := range shape.entryFiles {
		if t, ok := idx.exact[path.Join(base, ef)]; ok {
			return t
		}
	}
	return ""
}

// splitSegments normalizes an import name into path segments, replacing
// the shape's delimiters with "/" and dropping empties.
func splitSegments(name string, shape importShape) []string {
	s := name
	for _, d := range shape.delims {
		s = strings.ReplaceAll(s, d, "/")
	}
	raw := strings.Split(s, "/")
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// snakeCase converts PascalCase/snake_mixed identifiers to snake_case
// ("Geometry" -> "geometry", "OrderService" -> "order_service").
func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func snakeAll(segs []string) []string {
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = snakeCase(s)
	}
	return out
}

// resolveNamespace resolves a module-style import name (dotted,
// delimited, or bare) by longest-suffix matching against the index.
// Sigils are stripped, optional virtual roots are tried, and the final
// segment may be dropped for symbol-bearing imports (Rust).
func (idx *fileIndex) resolveNamespace(name string, shape importShape) string {
	raw := name
	for _, sigil := range shape.sigils {
		if after, ok := strings.CutPrefix(raw, sigil); ok {
			raw = after
			break
		}
	}
	segs := splitSegments(raw, shape)
	if len(segs) == 0 {
		return ""
	}

	lists := [][]string{}
	for _, root := range shape.roots {
		lists = append(lists, append([]string{root}, segs...))
	}
	lists = append(lists, segs)
	if shape.dropLast && len(segs) > 1 {
		lists = append(lists, segs[:len(segs)-1])
	}

	for _, list := range lists {
		for i := range list {
			if shape.snake {
				if t := idx.tryCandidate(path.Join(snakeAll(list[i:])...), shape); t != "" {
					return t
				}
			}
			if t := idx.tryCandidate(path.Join(list[i:]...), shape); t != "" {
				return t
			}
		}
	}
	return ""
}

// resolveLocalInclude resolves a C/C++ quoted header that did not sit
// next to the importing file ("geom/circle.hpp" reached via an include
// directory).  The name is a path, not a namespace: its extension is
// stripped, the remainder is split into path segments, and the longest
// suffix matching an indexed header wins.  Extensionless identifier
// forms (CONFIG_PATH) are matched as a single key.
func (idx *fileIndex) resolveLocalInclude(name string, shape importShape) string {
	stripped := name
	if e := path.Ext(name); e != "" {
		stripped = strings.TrimSuffix(name, e)
	}
	segs := splitSegments(stripped, shape)
	for i := range segs {
		if t := idx.tryCandidate(path.Join(segs[i:]...), shape); t != "" {
			return t
		}
	}
	if paths, ok := idx.keyed[stripped]; ok {
		return idx.pick(paths, shape)
	}
	return ""
}

// extIn reports whether ext is present in exts.
func extIn(ext string, exts []string) bool {
	return slices.Contains(exts, ext)
}

// resolveCrossFileImports is the generic cross-file import pass: every
// import entity of a format with an import shape is resolved onto an
// indexed document and lifted as an "imports" link.  Relative and local
// include imports resolve exactly (extracted, weight 1.0); namespace
// and bare imports match by longest suffix (inferred, weight 0.8).
// Unresolvable system/external imports are dropped, never dangling.
// It returns the per-document map of resolved targets so the reference
// pass can disambiguate name mentions against the same files.
func resolveCrossFileImports(graph *ir.Graph) map[string][]string {
	idx := buildFileIndex(graph)
	resolved := make(map[string][]string)
	for _, doc := range graph.Documents {
		shape, ok := importShapes[doc.Format]
		if !ok {
			continue
		}
		for _, ent := range doc.Entities {
			if ent.Type != "import" {
				continue
			}
			name := strings.TrimSpace(ent.Name)
			if name == "" {
				continue
			}
			srcDir := path.Dir(doc.Path)

			var target string
			var exact bool
			switch {
			case strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../"):
				target = idx.resolveRelative(path.Join(srcDir, name), shape)
				exact = target != ""
			case ent.Metadata["include_kind"] == "local":
				target = idx.resolveRelative(path.Join(srcDir, name), shape)
				exact = target != ""
				if target == "" {
					// Quoted headers often live under an include
					// directory rather than next to the importing file;
					// fall back to suffix matching on the header path.
					target = idx.resolveLocalInclude(name, shape)
				}
			default:
				target = idx.resolveNamespace(name, shape)
			}
			if target == "" || target == doc.Path {
				continue
			}

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
					"import_path": ent.Name,
					"resolution":  resolution,
				},
			})
			resolved[doc.Path] = append(resolved[doc.Path], target)
		}
	}
	return resolved
}
