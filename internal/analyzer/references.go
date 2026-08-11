package analyzer

import (
	"github.com/ldaidone/go-graphed/internal/ir"
)

// entityRef pairs a declaration entity with the document that declares it,
// so the reference pass can disambiguate name mentions by import context.
type entityRef struct {
	entity ir.Entity
	doc    string
}

// referenceTargetKinds are the entity kinds a "reference" mention may
// resolve to.  References are type/identifier mentions, so only type-like
// declarations qualify (classes, structs, interfaces, enums, namespaces,
// ...); functions and methods are never referenced by their type name.
var referenceTargetKinds = map[string]bool{
	"class":     true,
	"struct":    true,
	"interface": true,
	"enum":      true,
	"object":    true,
	"protocol":  true,
	"actor":     true,
	"namespace": true,
	"module":    true,
	"union":     true,
	"typedef":   true,
	"type":      true,
}

// referenceTargetKind reports whether an entity kind can be the target of
// a cross-file reference edge.
func referenceTargetKind(kind string) bool {
	return referenceTargetKinds[kind]
}

// resolveReferences lifts every "reference" entity onto a "references"
// edge pointing at the declaration it mentions.  The edge is inferred and
// low-weight: the mention is a parsed fact, but choosing which of possibly
// several same-named declarations was intended is a heuristic.
func resolveReferences(graph *ir.Graph, resolvedImports map[string][]string, index map[string][]entityRef) {
	for _, doc := range graph.Documents {
		importTargets := resolvedImports[doc.Path]
		for _, entity := range doc.Entities {
			if entity.Type != "reference" {
				continue
			}
			target := pickReferenceTarget(entity.Name, doc.Path, importTargets, index)
			if target == nil {
				continue
			}
			graph.Links = append(graph.Links, ir.Link{
				SourceID:   entity.ID,
				TargetID:   target.entity.ID,
				Type:       "references",
				Weight:     0.6,
				SourceType: ir.LinkSourceInferred,
				Metadata: map[string]string{
					"rule": "global_reference_resolution",
				},
			})
		}
	}
}

// pickReferenceTarget chooses the declaration a bare name mention refers
// to, or nil when no confident choice exists.  Candidates declared in the
// referencing document itself are self-references and excluded.  Among the
// remaining candidates, one declared in a document the referencing file
// imports wins (import-context disambiguation); otherwise a globally
// unique name is accepted; multiple candidates with no import context are
// left unresolved rather than guessed.
func pickReferenceTarget(name, sourceDoc string, importTargets []string, index map[string][]entityRef) *entityRef {
	var external []entityRef
	for _, c := range index[name] {
		if c.doc != sourceDoc {
			external = append(external, c)
		}
	}
	if len(external) == 0 {
		return nil
	}
	for i := range external {
		if containsString(importTargets, external[i].doc) {
			return &external[i]
		}
	}
	if len(external) == 1 {
		return &external[0]
	}
	return nil
}
