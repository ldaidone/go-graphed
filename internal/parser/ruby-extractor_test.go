package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseRuby(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "ruby", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_RubyFile_EmptyFile(t *testing.T) {
	doc := parseRuby(t, "empty.rb", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Ruby file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "ruby-extractor" {
		t.Errorf("processor = %q, want ruby-extractor", doc.Metadata["processor"])
	}
}

func TestParse_RubyFile_Requires(t *testing.T) {
	src := `require "json"
require_relative "lib/helper"
load "bootstrap.rb"
`
	doc := parseRuby(t, "requires.rb", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"json", "lib/helper", "bootstrap.rb"} {
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

func TestParse_RubyFile_ModulesAndMethods(t *testing.T) {
	src := `module Shapes
  class Circle
    def area
      helper * 3.14
    end

    def helper
      42
    end

    def self.factory
      Circle.new
    end
  end

  class Point
    def initialize(x, y)
      @x = x
      @y = y
    end
  end
end

def top_level
  1
end
`
	doc := parseRuby(t, "shapes.rb", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Shapes"] != "module" {
		t.Errorf("Shapes type = %q, want module", byName["Shapes"])
	}
	if byName["Shapes.Circle"] != "class" {
		t.Errorf("Shapes.Circle type = %q, want class", byName["Shapes.Circle"])
	}
	if byName["Shapes.Point"] != "class" {
		t.Errorf("Shapes.Point type = %q, want class", byName["Shapes.Point"])
	}
	if byName["Shapes.Circle.area"] != "method" {
		t.Errorf("Shapes.Circle.area type = %q, want method", byName["Shapes.Circle.area"])
	}
	if byName["Shapes.Circle.helper"] != "method" {
		t.Errorf("Shapes.Circle.helper type = %q, want method", byName["Shapes.Circle.helper"])
	}
	if byName["Shapes.Circle.factory"] != "method" {
		t.Errorf("Shapes.Circle.factory (singleton) type = %q, want method", byName["Shapes.Circle.factory"])
	}
	if byName["top_level"] != "function" {
		t.Errorf("top_level type = %q, want function", byName["top_level"])
	}
	if byName["area"] != "" {
		t.Errorf("method should not be registered under bare name, got %q", byName["area"])
	}
}

func TestParse_RubyFile_Calls(t *testing.T) {
	src := `class Service
  def helper
    1
  end

  def run
    helper(1)
  end
end

def top
  util(2)
end

def util(x)
  x
end
`
	doc := parseRuby(t, "calls.rb", src)

	names := nameByID(doc)
	calls := map[string]bool{}
	for _, l := range doc.Links {
		if l.Type == "calls" {
			calls[names[l.SourceID]+"->"+names[l.TargetID]] = true
		}
	}
	if !calls["Service.run->Service.helper"] {
		t.Errorf("expected Service.run -> Service.helper call, got %v", calls)
	}
	if !calls["top->util"] {
		t.Errorf("expected top -> util call, got %v", calls)
	}
}

func TestParse_RubyFile_NoMemberCallResolution(t *testing.T) {
	src := `class Service
  def helper
    1
  end

  def run
    self.helper
    self.helper(2)
    Service.new
  end
end
`
	doc := parseRuby(t, "member_calls.rb", src)

	for _, l := range doc.Links {
		if strings.HasSuffix(l.TargetID, "#method:Service.helper") {
			t.Errorf("self.helper is a member call and must not resolve, got link %+v", l)
		}
		if strings.HasSuffix(l.TargetID, "#class:Service") {
			t.Errorf("Service.new must not resolve, got link %+v", l)
		}
	}
}

func TestParse_RubyFile_SyntacticallyInvalid(t *testing.T) {
	src := `class Broken
  def run(
    return @@@
  end
end
`
	doc := parseRuby(t, "broken.rb", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}

func TestParse_RubyFile_CallLinkMetadata(t *testing.T) {
	src := `class A
  def go
    util(1)
  end

  def util(x)
    x
  end
end
`
	doc := parseRuby(t, "meta.rb", src)

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
	}
}
