package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseElixir(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "elixir", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_ElixirFile_EmptyFile(t *testing.T) {
	doc := parseElixir(t, "empty.ex", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Elixir file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "elixir-extractor" {
		t.Errorf("processor = %q, want elixir-extractor", doc.Metadata["processor"])
	}
}

func TestParse_ElixirFile_Directives(t *testing.T) {
	src := `defmodule Geometry.Circle do
  alias Geometry.Shapes, as: S
  import Math, only: [sqrt: 1]
  require Ecto.Query
  use Phoenix.Component
end
`
	doc := parseElixir(t, "directives.ex", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"Geometry.Shapes", "Math", "Ecto.Query", "Phoenix.Component"} {
		if !names[want] {
			t.Errorf("expected import %q, got %v", want, names)
		}
	}
	for _, e := range doc.Entities {
		if e.Type == "import" && (e.ID == "" || e.Name == "") {
			t.Errorf("import entity has empty ID or Name: %+v", e)
		}
	}
}

func TestParse_ElixirFile_ModuleAndFunctions(t *testing.T) {
	src := `defmodule Geometry.Circle do
  def area(radius) do
    helper(radius)
  end

  defp helper(radius) do
    radius * radius
  end

  defmacro debug(x) do
    x
  end
end

defmodule Geometry.Square do
  def area(side) do
    side * side
  end
end
`
	doc := parseElixir(t, "shapes.ex", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Geometry.Circle"] != "module" {
		t.Errorf("Geometry.Circle type = %q, want module", byName["Geometry.Circle"])
	}
	if byName["Geometry.Square"] != "module" {
		t.Errorf("Geometry.Square type = %q, want module", byName["Geometry.Square"])
	}
	if byName["Geometry.Circle.area"] != "function" {
		t.Errorf("Geometry.Circle.area type = %q, want function", byName["Geometry.Circle.area"])
	}
	if byName["Geometry.Circle.helper"] != "function" {
		t.Errorf("Geometry.Circle.helper type = %q, want function", byName["Geometry.Circle.helper"])
	}
	if byName["Geometry.Circle.debug"] != "function" {
		t.Errorf("Geometry.Circle.debug (macro) type = %q, want function", byName["Geometry.Circle.debug"])
	}
	if byName["area"] != "" {
		t.Errorf("function should not be registered under bare name, got %q", byName["area"])
	}
}

func TestParse_ElixirFile_Calls(t *testing.T) {
	src := `defmodule Math do
  def double(x) do
    add(x, x)
  end

  defp add(a, b) do
    a + b
  end
end
`
	doc := parseElixir(t, "calls.ex", src)

	names := nameByID(doc)
	calls := map[string]bool{}
	for _, l := range doc.Links {
		if l.Type == "calls" {
			calls[names[l.SourceID]+"->"+names[l.TargetID]] = true
		}
	}
	if !calls["Math.double->Math.add"] {
		t.Errorf("expected Math.double -> Math.add call, got %v", calls)
	}
}

func TestParse_ElixirFile_NoQualifiedCallResolution(t *testing.T) {
	src := `defmodule A do
  def run do
    Other.run()
    x.run()
  end
end
`
	doc := parseElixir(t, "qualified.ex", src)

	for _, l := range doc.Links {
		if l.Type == "calls" {
			t.Errorf("qualified calls must not resolve, got link %+v", l)
		}
	}
}

func TestParse_ElixirFile_SyntacticallyInvalid(t *testing.T) {
	src := `defmodule Broken do
  def run( do
    @@@
  end
end
`
	doc := parseElixir(t, "broken.ex", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}

func TestParse_ElixirFile_CallLinkMetadata(t *testing.T) {
	src := `defmodule A do
  def go do
    util()
  end

  def util do
    :ok
  end
end
`
	doc := parseElixir(t, "meta.ex", src)

	names := nameByID(doc)
	for _, l := range doc.Links {
		if l.Type != "calls" {
			continue
		}
		if names[l.TargetID] != "A.util" {
			t.Errorf("unexpected call target %q, want A.util", names[l.TargetID])
		}
		if l.SourceType != ir.LinkSourceExtracted {
			t.Errorf("call source type = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
		}
		if l.Weight != 1.0 {
			t.Errorf("call weight = %v, want 1.0", l.Weight)
		}
	}
}
