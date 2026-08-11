package graphed_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/pkg/graphed"
)

// TestBuild_CrossFileResolutionAllLanguages is the acceptance harness for
// the generic cross-file resolution passes: a mixed Kotlin/C++/Elixir
// corpus must produce file-to-file imports, type references, package
// nodes, and live PageRank/hub metrics -- the same guarantees the Go/JS
// families already had.  This guards against regressing back to an empty
// link graph for non-Go languages (the original evaluation finding).
func TestBuild_CrossFileResolutionAllLanguages(t *testing.T) {
	root := filepath.Join(t.TempDir(), "corpus")

	files := map[string]string{
		// Kotlin controller -> service -> repo (constructor injection).
		"kotlin/src/main/kotlin/com/acme/orders/controller/OrderController.kt": `package com.acme.orders.controller

import com.acme.orders.service.OrderService
import com.acme.orders.repo.OrderRepository

class OrderController(private val service: OrderService)
`,
		"kotlin/src/main/kotlin/com/acme/orders/service/OrderService.kt": `package com.acme.orders.service

import com.acme.orders.repo.OrderRepository

class OrderService(private val repo: OrderRepository)
`,
		"kotlin/src/main/kotlin/com/acme/orders/repo/OrderRepository.kt": `package com.acme.orders.repo

class OrderRepository
`,
		// C++ draw.cpp pulls in a shared header via an include directory
		// and uses its type.
		"cpp/include/geom/circle.hpp": `namespace geom {

class Circle {
public:
    double area();
};

}
`,
		"cpp/src/draw.cpp": `#include "geom/circle.hpp"

namespace geom {
void draw() {
    Circle c;
    c.area();
}
}
`,
		// Elixir app aliases the shapes module living in a snake_case file.
		"elixir/lib/geometry/shapes.ex": `defmodule Geometry.Shapes do
  def area(r) do
    r * r
  end
end
`,
		"elixir/lib/app.ex": `alias Geometry.Shapes
`,
	}
	for rel, src := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}

	output := filepath.Join(t.TempDir(), "graph.json")
	if err := graphed.Build(graphed.BuildOptions{
		Root:        root,
		Output:      output,
		Format:      "json",
		NoGitIgnore: true,
	}); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var graph ir.Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(graph.Documents) != 7 {
		t.Errorf("documents = %d, want 7: %v", len(graph.Documents), keys(graph.Documents))
	}

	// 1. Kotlin imports resolve to files via dotted-package suffix match.
	// The scanner records absolute paths, so edges are matched on their
	// relative suffixes.
	hasImport := func(src, dst string) bool {
		for _, link := range graph.Links {
			if link.Type == "imports" &&
				strings.HasSuffix(link.SourceID, src) &&
				strings.HasSuffix(link.TargetID, dst) {
				return true
			}
		}
		return false
	}
	for _, want := range [][2]string{
		{"kotlin/src/main/kotlin/com/acme/orders/controller/OrderController.kt", "kotlin/src/main/kotlin/com/acme/orders/service/OrderService.kt"},
		{"kotlin/src/main/kotlin/com/acme/orders/controller/OrderController.kt", "kotlin/src/main/kotlin/com/acme/orders/repo/OrderRepository.kt"},
		{"kotlin/src/main/kotlin/com/acme/orders/service/OrderService.kt", "kotlin/src/main/kotlin/com/acme/orders/repo/OrderRepository.kt"},
		{"cpp/src/draw.cpp", "cpp/include/geom/circle.hpp"},
		{"elixir/lib/app.ex", "elixir/lib/geometry/shapes.ex"},
	} {
		if !hasImport(want[0], want[1]) {
			t.Errorf("missing imports edge %s -> %s", want[0], want[1])
		}
	}

	// The C++ header import is an exact local include: extracted, full weight.
	for _, link := range graph.Links {
		if link.SourceID == "cpp/src/draw.cpp" && link.Type == "imports" {
			if link.SourceType != ir.LinkSourceExtracted || link.Weight != 1.0 {
				t.Errorf("cpp header import SourceType=%q Weight=%v, want extracted 1.0", link.SourceType, link.Weight)
			}
		}
	}

	// 2. Type references: Kotlin ctor injection and C++ Circle mention.
	refs := map[string]bool{}
	for _, link := range graph.Links {
		if link.Type == "references" {
			refs[link.SourceID+" -> "+link.TargetID] = true
		}
	}
	foundKotlinRef := false
	foundCppRef := false
	for pair := range refs {
		if strings.Contains(pair, "OrderController.kt") && strings.Contains(pair, "service/OrderService.kt") {
			foundKotlinRef = true
		}
		if strings.Contains(pair, "draw.cpp") && strings.Contains(pair, "circle.hpp") {
			foundCppRef = true
		}
	}
	if !foundKotlinRef {
		t.Errorf("expected Kotlin reference OrderController -> OrderService, got %v", refs)
	}
	if !foundCppRef {
		t.Errorf("expected C++ reference draw -> Circle in circle.hpp, got %v", refs)
	}

	// 3. Packages aggregate per language: JVM package clauses and the
	// Elixir top-level module segment.
	for _, want := range []string{
		"com.acme.orders.controller",
		"com.acme.orders.service",
		"com.acme.orders.repo",
		"Geometry",
	} {
		if graph.Packages[want] == nil {
			t.Errorf("missing package %q, got %v", want, keysP(graph.Packages))
		}
	}

	// 4. PageRank and hubs are alive -- the original bug was all-zero
	// metrics because the coupling graph was empty for non-Go code.
	if graph.Metrics.HubCount == 0 {
		t.Error("expected at least one hub document for a coupled corpus")
	}
	nonzero := 0
	for path, dm := range graph.Metrics.Documents {
		if dm.PageRank > 0 {
			nonzero++
		}
		if dm.Degree == 0 && strings.Contains(path, "OrderRepository.kt") {
			t.Errorf("repo document %s has degree 0 despite imports", path)
		}
	}
	if nonzero < 5 {
		t.Errorf("expected most documents with non-zero PageRank, got %d", nonzero)
	}
}

func keys(m map[string]*ir.Document) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysP(m map[string]*ir.Package) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
