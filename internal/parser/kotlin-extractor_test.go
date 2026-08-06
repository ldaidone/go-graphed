package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseKotlin(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "kotlin", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_KotlinFile_EmptyFile(t *testing.T) {
	doc := parseKotlin(t, "Empty.kt", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Kotlin file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "kotlin-extractor" {
		t.Errorf("processor = %q, want kotlin-extractor", doc.Metadata["processor"])
	}
}

func TestParse_KotlinFile_Imports(t *testing.T) {
	src := `import kotlin.math.max
import kotlin.collections.List as L
import foo.bar.Baz
`
	doc := parseKotlin(t, "Imports.kt", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"kotlin.math.max", "foo.bar.Baz"} {
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

func TestParse_KotlinFile_ClassesAndMethods(t *testing.T) {
	src := `interface Shape {
    fun area(): Double
}

enum class Color {
    RED, GREEN
}

abstract class Base {
    protected fun helper(): Int = 1
}

class Circle(
    private val radius: Double = 1.0
) : Shape, Base() {
    override fun area(): Double = 3.14 * helper()

    inner class Builder {
        fun build(): Builder = this
    }
}
`
	doc := parseKotlin(t, "Shapes.kt", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Shape"] != "interface" {
		t.Errorf("Shape type = %q, want interface", byName["Shape"])
	}
	if byName["Color"] != "enum" {
		t.Errorf("Color type = %q, want enum", byName["Color"])
	}
	if byName["Base"] != "class" {
		t.Errorf("Base type = %q, want class", byName["Base"])
	}
	if byName["Circle"] != "class" {
		t.Errorf("Circle type = %q, want class", byName["Circle"])
	}
	if byName["Circle.area"] != "method" {
		t.Errorf("Circle.area type = %q, want method", byName["Circle.area"])
	}
	if byName["Base.helper"] != "method" {
		t.Errorf("Base.helper type = %q, want method", byName["Base.helper"])
	}
	if byName["Circle.Builder"] != "class" {
		t.Errorf("nested Circle.Builder type = %q, want class", byName["Circle.Builder"])
	}
	if byName["Circle.Builder.build"] != "method" {
		t.Errorf("nested method type = %q, want method", byName["Circle.Builder.build"])
	}
	if byName["area"] != "" {
		t.Errorf("method should not be registered under bare name, got %q", byName["area"])
	}
}

func TestParse_KotlinFile_ObjectDeclaration(t *testing.T) {
	src := `object Singleton {
    fun go(): Int {
        return helper()
    }
    private fun helper(): Int = 42
}
`
	doc := parseKotlin(t, "Object.kt", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["Singleton"] != "object" {
		t.Errorf("Singleton type = %q, want object", byName["Singleton"])
	}
	if byName["Singleton.go"] != "method" {
		t.Errorf("Singleton.go type = %q, want method", byName["Singleton.go"])
	}
	if byName["Singleton.helper"] != "method" {
		t.Errorf("Singleton.helper type = %q, want method", byName["Singleton.helper"])
	}

	names := nameByID(doc)
	found := false
	for _, l := range doc.Links {
		if l.Type == "calls" && names[l.SourceID] == "Singleton.go" && names[l.TargetID] == "Singleton.helper" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Singleton.go -> Singleton.helper call, got %v", doc.Links)
	}
}

func TestParse_KotlinFile_TopLevelFunctionsAndCalls(t *testing.T) {
	src := `fun helper(): Int = 42

fun top(): Int {
    return helper()
}

fun unused(): Int = 0
`
	doc := parseKotlin(t, "Calls.kt", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["helper"] != "function" {
		t.Errorf("helper type = %q, want function", byName["helper"])
	}
	if byName["top"] != "function" {
		t.Errorf("top type = %q, want function", byName["top"])
	}

	topID := ""
	for _, e := range doc.Entities {
		if e.Name == "top" {
			topID = e.ID
		}
	}
	if topID == "" {
		t.Fatal("expected a function entity named top")
	}

	found := false
	for _, l := range doc.Links {
		if l.Type == "calls" && l.SourceID == topID {
			if !strings.HasSuffix(l.TargetID, "#function:helper") {
				t.Errorf("top call target = %q, want ...#function:helper", l.TargetID)
			}
			if l.SourceType != ir.LinkSourceExtracted {
				t.Errorf("call source type = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected a calls link from top to helper, got %v", doc.Links)
	}
}

func TestParse_KotlinFile_NoMemberCallResolution(t *testing.T) {
	src := `class Service {
    fun helper(): Int = 1

    fun run(): Int {
        return this.helper()
    }
}
`
	doc := parseKotlin(t, "MemberCalls.kt", src)

	for _, l := range doc.Links {
		if l.Type == "calls" {
			t.Errorf("this.helper() is a member call and must not resolve, got link %+v", l)
		}
	}
}

func TestParse_KotlinFile_SyntacticallyInvalid(t *testing.T) {
	src := `fun broken( {
    return @@@
}
`
	doc := parseKotlin(t, "Broken.kt", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
