package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseCSharp(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "csharp", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_CSharpFile_EmptyFile(t *testing.T) {
	doc := parseCSharp(t, "Empty.cs", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty C# file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "csharp-extractor" {
		t.Errorf("processor = %q, want csharp-extractor", doc.Metadata["processor"])
	}
}

func TestParse_CSharpFile_Usings(t *testing.T) {
	src := `using System;
using System.Collections.Generic;
using static System.Math;
using Project = My.App.Project;
`
	doc := parseCSharp(t, "Usings.cs", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"System", "System.Collections.Generic", "System.Math", "My.App.Project"} {
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

func TestParse_CSharpFile_TypesAndMethods(t *testing.T) {
	src := `namespace Geometry;

public interface IShape {
    double Area();
}

public abstract class ShapeBase : IShape {
    public virtual double Area() => 0;

    protected int Helper() => 1;
}

public sealed class Circle : ShapeBase {
    private double _radius = 1.0;

    public override double Area() => 3.14 * _radius;

    public record Point(int X, int Y);

    public enum Color { Red, Green, Blue }
}

public struct Vec3 { public double X; }
`
	doc := parseCSharp(t, "Shapes.cs", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["IShape"] != "interface" {
		t.Errorf("IShape type = %q, want interface", byName["IShape"])
	}
	if byName["ShapeBase"] != "class" {
		t.Errorf("ShapeBase type = %q, want class", byName["ShapeBase"])
	}
	if byName["Circle"] != "class" {
		t.Errorf("Circle type = %q, want class", byName["Circle"])
	}
	if byName["Circle.Point"] != "record" {
		t.Errorf("Circle.Point type = %q, want record", byName["Circle.Point"])
	}
	if byName["Circle.Color"] != "enum" {
		t.Errorf("Circle.Color type = %q, want enum", byName["Circle.Color"])
	}
	if byName["Vec3"] != "struct" {
		t.Errorf("Vec3 type = %q, want struct", byName["Vec3"])
	}
	if byName["Circle.Area"] != "method" {
		t.Errorf("Circle.Area type = %q, want method", byName["Circle.Area"])
	}
	if byName["ShapeBase.Helper"] != "method" {
		t.Errorf("ShapeBase.Helper type = %q, want method", byName["ShapeBase.Helper"])
	}
	if byName["IShape.Area"] != "method" {
		t.Errorf("IShape.Area type = %q, want method", byName["IShape.Area"])
	}
	if byName["Area"] != "" {
		t.Errorf("method should not be registered under bare name, got %q", byName["Area"])
	}
}

func TestParse_CSharpFile_Calls(t *testing.T) {
	src := `using System;

class Reporter {
    public static string Format(int n) => n.ToString();
}

class Main {
    private Reporter _reporter = new Reporter();

    public string Format(int n) => n.ToString();

    public string Go(int n) {
        return Format(n);
    }

    public static Reporter Make() {
        return new Reporter();
    }
}
`
	doc := parseCSharp(t, "Calls.cs", src)

	names := nameByID(doc)
	calls := map[string]bool{}
	for _, l := range doc.Links {
		if l.Type == "calls" {
			calls[names[l.SourceID]+"->"+names[l.TargetID]] = true
		}
	}
	// The bare invocation Format(n) inside Main.Go resolves to the
	// class-scoped method "Main.Format"; "new Reporter()" resolves to
	// the class entity.
	if !calls["Main.Go->Main.Format"] {
		t.Errorf("expected Main.Go -> Main.Format call, got %v", calls)
	}
	if !calls["Main.Make->Reporter"] {
		t.Errorf("expected Main.Make -> Reporter construction, got %v", calls)
	}
}

func TestParse_CSharpFile_NoMemberCallResolution(t *testing.T) {
	src := `class Service {
    public int Helper() => 1;

    public int Run() {
        return this.Helper();
    }
}
`
	doc := parseCSharp(t, "MemberCalls.cs", src)

	for _, l := range doc.Links {
		if l.Type == "calls" {
			t.Errorf("this.Helper() is a member invocation and must not resolve, got link %+v", l)
		}
	}
}

func TestParse_CSharpFile_SyntacticallyInvalid(t *testing.T) {
	src := `class Broken {
    public void Run( {
        return @@@;
    }
}
`
	doc := parseCSharp(t, "Broken.cs", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
