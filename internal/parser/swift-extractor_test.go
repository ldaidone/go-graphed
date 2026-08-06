package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseSwift(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "swift", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_SwiftFile_EmptyFile(t *testing.T) {
	doc := parseSwift(t, "Empty.swift", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Swift file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "swift-extractor" {
		t.Errorf("processor = %q, want swift-extractor", doc.Metadata["processor"])
	}
}

func TestParse_SwiftFile_Imports(t *testing.T) {
	src := `import Foundation
import UIKit.UIView
`
	doc := parseSwift(t, "Imports.swift", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"Foundation", "UIKit.UIView"} {
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

func TestParse_SwiftFile_TypesAndMethods(t *testing.T) {
	src := `protocol Greeter {
    func greet() -> String
}

class Circle: Greeter {
    var radius: Double = 1.0

    func helper() -> Int {
        return 42
    }

    func area() -> Double {
        return 3.14 * Double(helper())
    }

    struct Inner {
        func nested() -> Int {
            return 1
        }
    }
}

struct Point {
    var x: Int
    var y: Int
}

enum Color {
    case red, green
}
`
	doc := parseSwift(t, "Shapes.swift", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Greeter"] != "protocol" {
		t.Errorf("Greeter type = %q, want protocol", byName["Greeter"])
	}
	if byName["Circle"] != "class" {
		t.Errorf("Circle type = %q, want class", byName["Circle"])
	}
	if byName["Point"] != "struct" {
		t.Errorf("Point type = %q, want struct", byName["Point"])
	}
	if byName["Color"] != "enum" {
		t.Errorf("Color type = %q, want enum", byName["Color"])
	}
	if byName["Greeter.greet"] != "method" {
		t.Errorf("Greeter.greet type = %q, want method", byName["Greeter.greet"])
	}
	if byName["Circle.helper"] != "method" {
		t.Errorf("Circle.helper type = %q, want method", byName["Circle.helper"])
	}
	if byName["Circle.area"] != "method" {
		t.Errorf("Circle.area type = %q, want method", byName["Circle.area"])
	}
	if byName["Circle.Inner"] != "struct" {
		t.Errorf("nested Circle.Inner type = %q, want struct", byName["Circle.Inner"])
	}
	if byName["Circle.Inner.nested"] != "method" {
		t.Errorf("nested method type = %q, want method", byName["Circle.Inner.nested"])
	}
	if byName["area"] != "" {
		t.Errorf("method should not be registered under bare name, got %q", byName["area"])
	}
}

func TestParse_SwiftFile_Calls(t *testing.T) {
	src := `class Service {
    func helper() -> Int {
        return 1
    }

    func run() -> Int {
        return helper()
    }
}

func top() -> Int {
    return util()
}

func util() -> Int {
    return 2
}
`
	doc := parseSwift(t, "Calls.swift", src)

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

func TestParse_SwiftFile_NoMemberCallResolution(t *testing.T) {
	src := `class Service {
    func helper() -> Int {
        return 1
    }

    func run() -> Int {
        return self.helper()
    }
}
`
	doc := parseSwift(t, "MemberCalls.swift", src)

	for _, l := range doc.Links {
		if strings.HasSuffix(l.TargetID, "#method:Service.helper") {
			t.Errorf("self.helper() is a member call and must not resolve, got link %+v", l)
		}
	}
}

func TestParse_SwiftFile_SyntacticallyInvalid(t *testing.T) {
	src := `class Broken {
    func run( {
        return @@@
    }
}
`
	doc := parseSwift(t, "Broken.swift", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}

func TestParse_SwiftFile_CallLinkMetadata(t *testing.T) {
	src := `class A {
    func go() -> Int {
        return util()
    }
    func util() -> Int {
        return 1
    }
}
`
	doc := parseSwift(t, "Meta.swift", src)

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
		if !strings.HasSuffix(l.TargetID, "#method:A.util") {
			t.Errorf("call target = %q, want ...#method:A.util", l.TargetID)
		}
	}
}
