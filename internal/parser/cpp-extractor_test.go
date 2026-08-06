package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseCpp(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "cpp", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_CppFile_EmptyFile(t *testing.T) {
	doc := parseCpp(t, "empty.cpp", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty C++ file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "cpp-extractor" {
		t.Errorf("processor = %q, want cpp-extractor", doc.Metadata["processor"])
	}
}

func TestParse_CppFile_Includes(t *testing.T) {
	src := `#include <vector>
#include "util.h"
`
	doc := parseCpp(t, "includes.cpp", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"vector", "util.h"} {
		if !names[want] {
			t.Errorf("expected import %q, got %v", want, names)
		}
	}
}

func TestParse_CppFile_TypesAndMethods(t *testing.T) {
	src := `namespace geometry {
class Circle {
public:
    double area() {
        return helper() * 3.14;
    }

    int helper() {
        return 42;
    }

    struct Inner {
        int nested() {
            return 1;
        }
    };
};
}

struct Box {
    int size() {
        return 7;
    }
};

enum class Dir { N, S };

typedef unsigned int u32;
`
	doc := parseCpp(t, "shapes.cpp", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["geometry"] != "namespace" {
		t.Errorf("geometry type = %q, want namespace", byName["geometry"])
	}
	if byName["geometry.Circle"] != "class" {
		t.Errorf("geometry.Circle type = %q, want class", byName["geometry.Circle"])
	}
	if byName["Box"] != "struct" {
		t.Errorf("Box type = %q, want struct", byName["Box"])
	}
	if byName["Dir"] != "enum" {
		t.Errorf("Dir type = %q, want enum", byName["Dir"])
	}
	if byName["u32"] != "typedef" {
		t.Errorf("u32 type = %q, want typedef", byName["u32"])
	}
	if byName["geometry.Circle.area"] != "method" {
		t.Errorf("geometry.Circle.area type = %q, want method", byName["geometry.Circle.area"])
	}
	if byName["geometry.Circle.helper"] != "method" {
		t.Errorf("geometry.Circle.helper type = %q, want method", byName["geometry.Circle.helper"])
	}
	if byName["Box.size"] != "method" {
		t.Errorf("Box.size type = %q, want method", byName["Box.size"])
	}
	if byName["geometry.Circle.Inner"] != "struct" {
		t.Errorf("nested geometry.Circle.Inner type = %q, want struct", byName["geometry.Circle.Inner"])
	}
	if byName["geometry.Circle.Inner.nested"] != "method" {
		t.Errorf("nested method type = %q, want method", byName["geometry.Circle.Inner.nested"])
	}
	if byName["area"] != "" {
		t.Errorf("method should not be registered under bare name, got %q", byName["area"])
	}
}

func TestParse_CppFile_FreeFunctionsAndCalls(t *testing.T) {
	src := `double compute() {
    return helper() * 2.0;
}

double helper() {
    return 3.14;
}
`
	doc := parseCpp(t, "calls.cpp", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["compute"] != "function" {
		t.Errorf("compute type = %q, want function", byName["compute"])
	}
	if byName["helper"] != "function" {
		t.Errorf("helper type = %q, want function", byName["helper"])
	}

	computeID := ""
	for _, e := range doc.Entities {
		if e.Name == "compute" {
			computeID = e.ID
		}
	}
	if computeID == "" {
		t.Fatal("expected a function entity named compute")
	}
	found := false
	for _, l := range doc.Links {
		if l.Type == "calls" && l.SourceID == computeID {
			if !strings.HasSuffix(l.TargetID, "#function:helper") {
				t.Errorf("compute call target = %q, want ...#function:helper", l.TargetID)
			}
			if l.SourceType != ir.LinkSourceExtracted {
				t.Errorf("call source type = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected a calls link from compute to helper, got %v", doc.Links)
	}
}

func TestParse_CppFile_ClassMethodCallResolves(t *testing.T) {
	src := `class Widget {
public:
    int size() {
        return inner();
    }

    int inner() {
        return 5;
    }
};
`
	doc := parseCpp(t, "class_calls.cpp", src)

	names := nameByID(doc)
	calls := map[string]bool{}
	for _, l := range doc.Links {
		if l.Type == "calls" {
			calls[names[l.SourceID]+"->"+names[l.TargetID]] = true
		}
	}
	if !calls["Widget.size->Widget.inner"] {
		t.Errorf("expected Widget.size -> Widget.inner call, got %v", calls)
	}
}

func TestParse_CppFile_NoMemberCallResolution(t *testing.T) {
	src := `class Widget {
public:
    double area() {
        return 0;
    }
};

double compute() {
    Widget w;
    return w.area();
}
`
	doc := parseCpp(t, "member_calls.cpp", src)

	for _, l := range doc.Links {
		if strings.HasSuffix(l.TargetID, "#method:Widget.area") {
			t.Errorf("w.area() is a member call and must not resolve, got link %+v", l)
		}
	}
}

func TestParse_CppFile_SyntacticallyInvalid(t *testing.T) {
	src := `class Broken {
    void run( {
        return @@@;
    }
};
`
	doc := parseCpp(t, "broken.cpp", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
