package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseC(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "c", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_CFile_EmptyFile(t *testing.T) {
	doc := parseC(t, "empty.c", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty C file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "c-extractor" {
		t.Errorf("processor = %q, want c-extractor", doc.Metadata["processor"])
	}
}

func TestParse_CFile_Includes(t *testing.T) {
	src := `#include <stdio.h>
#include "util.h"
#include CONFIG_PATH
`
	doc := parseC(t, "includes.c", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"stdio.h", "util.h", "CONFIG_PATH"} {
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

func TestParse_CFile_Objects(t *testing.T) {
	src := `struct Point {
    int x;
    int y;
};

union Value {
    int i;
    float f;
};

enum Color { RED, GREEN };

typedef struct Point Point;

typedef unsigned int u32;

int helper(int n) {
    return n + 1;
}
`
	doc := parseC(t, "objects.c", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Point"] != "struct" {
		t.Errorf("Point type = %q, want struct", byName["Point"])
	}
	if byName["Value"] != "union" {
		t.Errorf("Value type = %q, want union", byName["Value"])
	}
	if byName["Color"] != "enum" {
		t.Errorf("Color type = %q, want enum", byName["Color"])
	}
	if byName["u32"] != "typedef" {
		t.Errorf("u32 type = %q, want typedef", byName["u32"])
	}
	if byName["helper"] != "function" {
		t.Errorf("helper type = %q, want function", byName["helper"])
	}
}

func TestParse_CFile_StructReturnTypeName(t *testing.T) {
	src := `struct Point make_point(void) {
    struct Point p;
    return p;
}
`
	doc := parseC(t, "rettype.c", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["make_point"] != "function" {
		t.Errorf("make_point type = %q, want function (return type must not leak into the name)", byName["make_point"])
	}
}

func TestParse_CFile_Calls(t *testing.T) {
	src := `int helper(int n) {
    return n + 1;
}

int main(void) {
    return helper(1);
}
`
	doc := parseC(t, "calls.c", src)

	mainID := ""
	for _, e := range doc.Entities {
		if e.Name == "main" {
			mainID = e.ID
		}
	}
	if mainID == "" {
		t.Fatal("expected a function entity named main")
	}

	found := false
	for _, l := range doc.Links {
		if l.Type == "calls" && l.SourceID == mainID {
			if !strings.HasSuffix(l.TargetID, "#function:helper") {
				t.Errorf("main call target = %q, want ...#function:helper", l.TargetID)
			}
			if l.SourceType != ir.LinkSourceExtracted {
				t.Errorf("call source type = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected a calls link from main to helper, got %v", doc.Links)
	}
}

func TestParse_CFile_NoMemberCallResolution(t *testing.T) {
	src := `struct Service {
    int helper;
};

int run(struct Service *s) {
    return s->helper;
}
`
	doc := parseC(t, "member.c", src)

	for _, l := range doc.Links {
		if l.Type == "calls" {
			t.Errorf("s->helper is a member access and must not resolve, got link %+v", l)
		}
	}
}

func TestParse_CFile_SyntacticallyInvalid(t *testing.T) {
	src := `int broken( {
    return @@@;
}
`
	doc := parseC(t, "broken.c", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
