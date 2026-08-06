package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseJava(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "java", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_JavaFile_EmptyFile(t *testing.T) {
	doc := parseJava(t, "Empty.java", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Java file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "java-extractor" {
		t.Errorf("processor = %q, want java-extractor", doc.Metadata["processor"])
	}
}

func TestParse_JavaFile_Imports(t *testing.T) {
	src := `import java.util.List;
import java.util.Map.Entry;
import static java.lang.Math.*;
`
	doc := parseJava(t, "Imports.java", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"java.util.List", "java.util.Map.Entry", "java.lang.Math"} {
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

func TestParse_JavaFile_ClassesAndMethods(t *testing.T) {
	src := `public interface Shape {
    double area();
}

public abstract class Base implements Shape {
    protected int helper() {
        return 1;
    }
}

public class Circle extends Base {
    private double radius = 1.0;

    public double area() {
        return 3.14 * this.helper();
    }

    public static class Builder {
        public Builder build() {
            return this;
        }
    }
}

enum Color {
    RED, GREEN, BLUE
}
`
	doc := parseJava(t, "Shapes.java", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Shape"] != "interface" {
		t.Errorf("Shape type = %q, want interface", byName["Shape"])
	}
	if byName["Base"] != "class" {
		t.Errorf("Base type = %q, want class", byName["Base"])
	}
	if byName["Circle"] != "class" {
		t.Errorf("Circle type = %q, want class", byName["Circle"])
	}
	if byName["Color"] != "enum" {
		t.Errorf("Color type = %q, want enum", byName["Color"])
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

func TestParse_JavaFile_Calls(t *testing.T) {
	src := `class Reporter {
    public static String format(int n) {
        return "n";
    }
}

class Main {
    String go(int n) {
        return format(n);
    }

    String format(int n) {
        return "n";
    }

    static Reporter make() {
        return new Reporter();
    }
}
`
	doc := parseJava(t, "Calls.java", src)

	names := nameByID(doc)
	calls := map[string]bool{}
	for _, l := range doc.Links {
		if l.Type == "calls" {
			calls[names[l.SourceID]+"->"+names[l.TargetID]] = true
		}
	}
	// The bare method invocation format(n) inside Main.go resolves to
	// the class-scoped method "Main.format"; "new Reporter()" resolves
	// to the class entity.
	if !calls["Main.go->Main.format"] {
		t.Errorf("expected Main.go -> Main.format call, got %v", calls)
	}
	if !calls["Main.make->Reporter"] {
		t.Errorf("expected Main.make -> Reporter construction, got %v", calls)
	}
}

func TestParse_JavaFile_NoMemberCallResolution(t *testing.T) {
	src := `class Service {
    void helper() {
    }

    void run() {
        this.helper();
        new Service().helper();
    }
}
`
	doc := parseJava(t, "MemberCalls.java", src)

	// Member invocations (this.helper(), new Service().helper()) must
	// not resolve.  The "new Service()" expression inside is a
	// construction and may resolve to the class itself.
	for _, l := range doc.Links {
		if strings.HasSuffix(l.TargetID, "#method:Service.helper") {
			t.Errorf("member invocation resolved to a method, got link %+v", l)
		}
	}
}

func TestParse_JavaFile_SyntacticallyInvalid(t *testing.T) {
	src := `public class Broken {
    void run( {
        return ;
    }
`
	doc := parseJava(t, "Broken.java", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}

func TestParse_JavaFile_CallLinkMetadata(t *testing.T) {
	src := `class A {
    int go() {
        return util();
    }
    int util() {
        return 1;
    }
}
`
	doc := parseJava(t, "Meta.java", src)

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
