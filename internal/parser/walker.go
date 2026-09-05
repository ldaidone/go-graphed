package parser

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
)

// LanguageConfig drives the shared two-pass tree-sitter walker used by
// the language families that fit the "register declarations, then emit
// entities + calls links" shape.  A config is a table of the grammar's
// node types mapped onto entity kinds plus the language's import and
// call-expression shapes; languages that cannot express themselves in
// the tables can hook into the passes directly.
type LanguageConfig struct {
	// Language is the tree-sitter grammar the walker parses with.
	Language *sitter.Language

	// Declarations lists every node type that defines a named entity
	// (function, class, method, table, ...).
	Declarations []DeclarationSpec

	// Imports lists the node types that pull in another module.
	Imports []ImportSpec

	// Calls describes the language's call-expression node types.  Empty
	// disables within-file "calls" links.
	Calls []CallSpec

	// RefNodeTypes lists node types whose text is a type/identifier
	// mention worth resolving across files (e.g. "type_identifier" for
	// Kotlin/Java/Swift/C++).  During the extraction pass every such node
	// also emits a "reference" entity so the analyzer can resolve it
	// against the global entity registry and lift cross-file "references"
	// edges.  Empty disables reference emission.
	RefNodeTypes []string

	// SkipNodeTypes prunes recursion into node types that cannot contain
	// declarations or call expressions (comments, literals).  Safe
	// pruning keeps the walk over large files fast.
	SkipNodeTypes map[string]bool

	// RegisterHook runs during the registration pass, after the
	// table-driven declarations, for symbol kinds that do not fit a
	// table row.
	RegisterHook func(w *Walker, n *sitter.Node)

	// InspectHook runs during the extraction pass, after the current
	// node's entity/call handling and before recursion, for entities and
	// links that do not fit a table row (e.g. SQL reference links).
	InspectHook func(w *Walker, n *sitter.Node)
}

// DeclarationSpec maps a grammar node type onto a named entity.
type DeclarationSpec struct {
	// NodeType is the tree-sitter node type, e.g. "function_definition".
	NodeType string

	// Kind is the entity type, e.g. "function" or "class".
	Kind string

	// NameField is the child field holding the declaration name,
	// e.g. "name".  Mutually exclusive with NameNodeType.
	NameField string

	// NameNodeType names the descendant node type that holds the
	// declaration name when the grammar does not use a field, e.g.
	// "identifier" for SQL's CREATE TABLE <name> statements.  The first
	// matching descendant is used.  Mutually exclusive with NameField.
	NameNodeType string

	// NameFieldPath navigates field-by-field from the declaration node
	// to the node whose text is the declaration name, e.g.
	// ["declarator", "declarator"] for C/C++ function_definition, where
	// the name sits in the declarator's declarator.  Mutually exclusive
	// with NameField and NameNodeType.
	NameFieldPath []string

	// ClassScope marks type/class declarations: a scope frame is pushed
	// while the body is inspected so nested declarations can be named
	// "Outer.Inner".  Used by Rust impl blocks and traits.
	ClassScope bool

	// FunctionScope marks callable declarations: the entity becomes the
	// current caller while its body is inspected, so call expressions
	// inside resolve with a caller ID.
	FunctionScope bool

	// MethodInClass, for function-like declarations, renames a
	// declaration nested inside a class scope to a "method" entity named
	// "Class.name" (Python, Rust).  Plain functions keep their bare
	// name.
	MethodInClass bool

	// ScopeOnly marks declarations that push a scope frame (and recurse)
	// but emit no entity themselves -- e.g. Rust's impl blocks, which
	// exist only to give their methods a "Type.method" name.
	ScopeOnly bool

	// KindByChild switches the entity kind when the declaration contains
	// a direct child node of one of the given types (Kotlin's class,
	// interface, and enum declarations share one node type,
	// distinguished by keyword children).
	KindByChild map[string]string

	// Package marks package/namespace clause declarations (Java's
	// package_declaration, PHP's namespace_definition, ...).  The emitted
	// entity carries a "package_path" metadata equal to the package text
	// so the analyzer can aggregate every file declaring the same
	// package into one module unit regardless of language.
	Package bool
}

// ImportSpec maps a grammar node type onto an import entity.  The
// module name is read from SourceField when present, otherwise from the
// first direct child matching SourceChildTypes, otherwise from the
// first descendant of SourceNodeType.
type ImportSpec struct {
	NodeType string

	// SourceField is the child field holding the module path,
	// e.g. "module_name" or "source".
	SourceField string

	// SourceChildTypes names the direct child node types that can hold
	// the module path, e.g. C# "identifier"/"qualified_name".  The first
	// direct child matching any of them supplies the name.
	SourceChildTypes []string

	// SourceNodeType is the descendant node type holding the module
	// path when no field or direct child does, e.g. "dotted_name" or
	// "scoped_identifier".
	SourceNodeType string
}

// CallSpec describes one of a language's call-expression node types.
type CallSpec struct {
	// NodeType is the tree-sitter node type, e.g. "call_expression",
	// "invocation_expression", or "command" (Bash).
	NodeType string

	// FunctionField is the child field naming the called expression,
	// e.g. "function" or "name".  The node's text is the callee.
	FunctionField string

	// CalleeNodeTypes is used when the grammar has no function field
	// (Kotlin, PHP object creation): the callee is the first direct
	// child matching any of these node types.
	CalleeNodeTypes []string

	// ObjectField, when set, names a receiver field whose presence marks
	// the call as a member invocation (Java "this.helper()",
	// "obj.method()"): such calls are skipped, never resolving to a bare
	// within-file symbol.
	ObjectField string
}

// Walker runs the shared two-pass extraction over one parsed file.
// Pass 1 registers every declaration symbol so forward references
// resolve; pass 2 emits entities, imports, and calls links.  The Go
// and JS-family extractors follow the same shape; this walker makes it
// table-driven so new languages only need a LanguageConfig.
type Walker struct {
	content []byte
	path    string
	cfg     *LanguageConfig

	// declared maps every symbol to its entity ID so call sites resolve
	// even when they appear earlier in the file.
	declared map[string]string
	// scopeStack names the enclosing class(es) so methods are named
	// "Class.method".
	scopeStack []string
	// currentFunc tracks the entity ID of the enclosing function or
	// method, so call-expression nodes know who the caller is.
	currentFunc []string

	entities []ir.Entity
	links    []ir.Link
}

// extractWithConfig is the shared entry point behind every
// config-driven extractor.  It reads the file, parses it with the
// config's grammar, and runs the two passes.
func extractWithConfig(path string, cfg *LanguageConfig) ([]ir.Entity, []ir.Link, error) {
	var err error
	var content []byte
	var tree *sitter.Tree

	content, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read file: %w", err)
	}

	parser := sitter.NewParser(cfg.Language)
	tree, err = parser.Parse(content)
	if err != nil {
		return nil, nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	w := &Walker{
		content:  content,
		path:     path,
		cfg:      cfg,
		declared: make(map[string]string),
		entities: make([]ir.Entity, 0),
		links:    make([]ir.Link, 0),
	}
	w.register(tree.RootNode())
	w.inspect(tree.RootNode())
	return w.entities, w.links, nil
}

// skip reports whether the walker should prune recursion into n.
func (w *Walker) skip(n *sitter.Node) bool {
	if n == nil {
		return true
	}
	if len(w.cfg.SkipNodeTypes) == 0 {
		return false
	}
	_, ok := w.cfg.SkipNodeTypes[n.Type(w.cfg.Language)]
	return ok
}

// lang is a shorthand for the configured grammar.
func (w *Walker) lang() *sitter.Language {
	return w.cfg.Language
}

// declarationFor returns the spec matching n's node type, or nil.
func (w *Walker) declarationFor(n *sitter.Node) *DeclarationSpec {
	typ := n.Type(w.lang())
	for i := range w.cfg.Declarations {
		if w.cfg.Declarations[i].NodeType == typ {
			return &w.cfg.Declarations[i]
		}
	}
	return nil
}

// importFor returns the import spec matching n's node type, or nil.
func (w *Walker) importFor(n *sitter.Node) *ImportSpec {
	typ := n.Type(w.lang())
	for i := range w.cfg.Imports {
		if w.cfg.Imports[i].NodeType == typ {
			return &w.cfg.Imports[i]
		}
	}
	return nil
}

// scopeName resolves the raw name of a declaration node from its
// NameField, NameFieldPath, or NameNodeType.
func (w *Walker) scopeName(n *sitter.Node, spec *DeclarationSpec) string {
	var nameNode *sitter.Node
	switch {
	case spec.NameField != "":
		nameNode = n.ChildByFieldName(spec.NameField, w.lang())
	case len(spec.NameFieldPath) > 0:
		nameNode = n
		for _, field := range spec.NameFieldPath {
			nameNode = nameNode.ChildByFieldName(field, w.lang())
			if nameNode == nil {
				return ""
			}
		}
	case spec.NameNodeType != "":
		nameNode = w.firstDescendantOfType(n, spec.NameNodeType)
	}
	if nameNode == nil {
		return ""
	}
	return strings.TrimSpace(string(w.content[nameNode.StartByte():nameNode.EndByte()]))
}

// declarationName resolves a declaration node to its full (scope-
// qualified) name and entity kind.  Class-scoped callables become
// "Class.method" methods when the spec opts in via MethodInClass;
// class-scoped types become "Outer.Inner" when nested.  KindByChild can
// override the entity kind (Kotlin classes/interfaces).
func (w *Walker) declarationName(n *sitter.Node, spec *DeclarationSpec) (string, string) {
	base := w.scopeName(n, spec)
	if base == "" {
		return "", spec.Kind
	}
	kind := spec.Kind
	for childType, mapped := range spec.KindByChild {
		if w.childOfType(n, childType) != nil {
			kind = mapped
			break
		}
	}
	if spec.MethodInClass && len(w.scopeStack) > 0 {
		return w.scopeStack[len(w.scopeStack)-1] + "." + base, "method"
	}
	if spec.ClassScope && len(w.scopeStack) > 0 {
		return w.scopeStack[len(w.scopeStack)-1] + "." + base, kind
	}
	return base, kind
}

// declarationEntityID returns the entity ID a declaration node maps to,
// or "" when the node is not an entity-producing declaration.
func (w *Walker) declarationEntityID(n *sitter.Node) string {
	spec := w.declarationFor(n)
	if spec == nil || spec.ScopeOnly {
		return ""
	}
	fullName, kind := w.declarationName(n, spec)
	if fullName == "" {
		return ""
	}
	return fmt.Sprintf("%s#%s:%s", w.path, kind, fullName)
}

// register is pass 1: record every declaration symbol so forward
// references produce links.  Class scopes are tracked so methods
// register under their qualified name.
func (w *Walker) register(n *sitter.Node) {
	if w.skip(n) {
		return
	}
	if spec := w.declarationFor(n); spec != nil {
		fullName := ""
		if !spec.ScopeOnly {
			if name, kind := w.declarationName(n, spec); name != "" {
				fullName = name
				w.declared[fullName] = fmt.Sprintf("%s#%s:%s", w.path, kind, fullName)
			}
		}
		if spec.ClassScope {
			if fullName == "" {
				fullName, _ = w.declarationName(n, spec)
			}
			if fullName != "" {
				w.scopeStack = append(w.scopeStack, fullName)
			}
		}
		defer func() {
			if spec.ClassScope && fullName != "" {
				w.scopeStack = w.scopeStack[:len(w.scopeStack)-1]
			}
		}()
	}

	if w.cfg.RegisterHook != nil {
		w.cfg.RegisterHook(w, n)
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		w.register(n.Child(i))
	}
}

// inspect is pass 2: emit entities, imports, within-file calls links,
// and run the InspectHook for language-specific extras.  Entering a
// declaration pushes the scope/caller frames it declares; the deferred
// exit pops them when the subtree is done.
func (w *Walker) inspect(n *sitter.Node) {
	if w.skip(n) {
		return
	}

	entered := w.enter(n)
	defer func() {
		if entered {
			w.exit(n)
		}
	}()

	if imp := w.importFor(n); imp != nil {
		w.emitImport(n, imp)
		return
	}

	if w.callSpecFor(n) != nil {
		w.emitCall(n)
	}

	if w.isRefNode(n) {
		w.emitReference(n)
	}

	if w.cfg.InspectHook != nil {
		w.cfg.InspectHook(w, n)
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		w.inspect(n.Child(i))
	}
}

// enter emits the entity for a declaration node (unless ScopeOnly) and
// pushes its scope/caller frames.  It reports whether frames were
// pushed so the caller can schedule the matching exit.
func (w *Walker) enter(n *sitter.Node) bool {
	spec := w.declarationFor(n)
	if spec == nil {
		return false
	}
	fullName, kind := w.declarationName(n, spec)
	if fullName == "" {
		return false
	}
	id := fmt.Sprintf("%s#%s:%s", w.path, kind, fullName)
	if !spec.ScopeOnly {
		w.entities = append(w.entities, ir.Entity{
			ID:       id,
			Type:     kind,
			Name:     fullName,
			Metadata: lineMetadata(n),
		})
		if spec.Package {
			w.entities[len(w.entities)-1].Metadata["package_path"] = fullName
		}
	}
	if spec.ClassScope {
		w.scopeStack = append(w.scopeStack, fullName)
	}
	if spec.FunctionScope {
		w.currentFunc = append(w.currentFunc, id)
	}
	return true
}

// exit pops the scope/caller frames a declaration pushed.
func (w *Walker) exit(n *sitter.Node) {
	spec := w.declarationFor(n)
	if spec == nil {
		return
	}
	if spec.FunctionScope {
		w.currentFunc = w.currentFunc[:len(w.currentFunc)-1]
	}
	if spec.ClassScope {
		w.scopeStack = w.scopeStack[:len(w.scopeStack)-1]
	}
}

// emitImport emits an "import" entity for a module-pulling node.
func (w *Walker) emitImport(n *sitter.Node, spec *ImportSpec) {
	var module string
	if spec.SourceField != "" {
		if field := n.ChildByFieldName(spec.SourceField, w.lang()); field != nil {
			module = strings.TrimSpace(string(w.content[field.StartByte():field.EndByte()]))
		}
	}
	if module == "" && len(spec.SourceChildTypes) > 0 {
		for _, t := range spec.SourceChildTypes {
			if field := w.childOfType(n, t); field != nil {
				module = strings.TrimSpace(string(w.content[field.StartByte():field.EndByte()]))
				break
			}
		}
	}
	if module == "" && spec.SourceNodeType != "" {
		if field := w.firstDescendantOfType(n, spec.SourceNodeType); field != nil {
			module = strings.TrimSpace(string(w.content[field.StartByte():field.EndByte()]))
		}
	}
	if module == "" {
		return
	}
	w.entities = append(w.entities, ir.Entity{
		ID:       fmt.Sprintf("%s#import:%s", w.path, module),
		Type:     "import",
		Name:     module,
		Metadata: lineMetadata(n),
	})
}

// callSpecFor returns the call spec matching n's node type, or nil.
func (w *Walker) callSpecFor(n *sitter.Node) *CallSpec {
	typ := n.Type(w.lang())
	for i := range w.cfg.Calls {
		if w.cfg.Calls[i].NodeType == typ {
			return &w.cfg.Calls[i]
		}
	}
	return nil
}

// isRefNode reports whether n's node type is configured as a reference
// (cross-file-resolvable identifier mention).
func (w *Walker) isRefNode(n *sitter.Node) bool {
	if len(w.cfg.RefNodeTypes) == 0 {
		return false
	}
	typ := n.Type(w.lang())
	return slices.Contains(w.cfg.RefNodeTypes, typ)
}

// emitReference emits a "reference" entity for an identifier mention.  The
// ID is disambiguated by byte offset so repeated mentions of the same name
// stay distinct, and the analyzer resolves the entity to a declaration
// somewhere else in the graph.  Mentions that resolve to a declaration in
// this same file are skipped -- they are self-references, not cross-file
// references.
func (w *Walker) emitReference(n *sitter.Node) {
	name := strings.TrimSpace(string(w.content[n.StartByte():n.EndByte()]))
	if name == "" {
		return
	}
	if _, ok := w.declared[name]; ok {
		return
	}
	w.entities = append(w.entities, ir.Entity{
		ID:       fmt.Sprintf("%s#reference:%s@%d", w.path, name, n.StartByte()),
		Type:     "reference",
		Name:     name,
		Metadata: lineMetadata(n),
	})
}

// calleeName extracts the callee text from a call node per its spec.
func (w *Walker) calleeName(n *sitter.Node, spec *CallSpec) string {
	var callee *sitter.Node
	if spec.FunctionField != "" {
		callee = n.ChildByFieldName(spec.FunctionField, w.lang())
	}
	if callee == nil {
		for _, t := range spec.CalleeNodeTypes {
			if c := w.childOfType(n, t); c != nil {
				callee = c
				break
			}
		}
	}
	if callee == nil {
		return ""
	}
	return strings.TrimSpace(string(w.content[callee.StartByte():callee.EndByte()]))
}

// emitCall resolves a call expression whose callee is a bare name
// against the declared symbols of this file.  Member/path/attribute
// calls carry qualified text (e.g. "obj.method", "Point::new") that
// never matches a declared bare name.  When a bare call does not match a
// top-level symbol, it falls back to the enclosing class scope so a
// method calling "helper()" resolves to "Class.helper".
func (w *Walker) emitCall(n *sitter.Node) {
	if len(w.currentFunc) == 0 {
		return
	}
	spec := w.callSpecFor(n)
	if spec == nil {
		return
	}
	if spec.ObjectField != "" && n.ChildByFieldName(spec.ObjectField, w.lang()) != nil {
		return
	}
	callee := w.calleeName(n, spec)
	if callee == "" {
		return
	}
	target, ok := w.declared[callee]
	if !ok && len(w.scopeStack) > 0 {
		target, ok = w.declared[w.scopeStack[len(w.scopeStack)-1]+"."+callee]
	}
	if !ok {
		return
	}
	w.links = append(w.links, ir.Link{
		SourceID:   w.currentFunc[len(w.currentFunc)-1],
		TargetID:   target,
		Type:       "calls",
		Weight:     1.0,
		SourceType: ir.LinkSourceExtracted,
	})
}

// firstDescendantOfType returns the first descendant node of the given
// type, or nil when none exists.
func (w *Walker) firstDescendantOfType(n *sitter.Node, nodeType string) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type(w.lang()) == nodeType {
			return child
		}
		if found := w.firstDescendantOfType(child, nodeType); found != nil {
			return found
		}
	}
	return nil
}

// childOfType returns the first direct child node of the given type, or
// nil when none exists.
func (w *Walker) childOfType(n *sitter.Node, nodeType string) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type(w.lang()) == nodeType {
			return child
		}
	}
	return nil
}

// descendantsOfType returns every descendant node of the given type.
func (w *Walker) descendantsOfType(n *sitter.Node, nodeType string) []*sitter.Node {
	var out []*sitter.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type(w.lang()) == nodeType {
			out = append(out, child)
		}
		out = append(out, w.descendantsOfType(child, nodeType)...)
	}
	return out
}

// descendantTextsOfType returns the trimmed text of every descendant of
// the given type, in document order.
func (w *Walker) descendantTextsOfType(n *sitter.Node, nodeType string) []string {
	nodes := w.descendantsOfType(n, nodeType)
	texts := make([]string, 0, len(nodes))
	for _, nd := range nodes {
		texts = append(texts, strings.TrimSpace(string(w.content[nd.StartByte():nd.EndByte()])))
	}
	return texts
}

// lineMetadata builds the standard start/end line metadata map.
func lineMetadata(n *sitter.Node) map[string]string {
	return map[string]string{
		"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
		"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
	}
}
