package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parsePHP(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "php", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_PHPFile_EmptyFile(t *testing.T) {
	doc := parsePHP(t, "empty.php", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty PHP file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "php-extractor" {
		t.Errorf("processor = %q, want php-extractor", doc.Metadata["processor"])
	}
}

func TestParse_PHPFile_NamespaceImports(t *testing.T) {
	src := `<?php
namespace App\Service;

use App\Models\User;
use App\Util as Helpers;
use App\Lib\Something\{
    One,
    Two as Alias,
};

class Handler {}
`
	doc := parsePHP(t, "imports.php", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"App\\Models\\User", "App\\Util", "App\\Lib\\Something\\One", "App\\Lib\\Something\\Two"} {
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

func TestParse_PHPFile_ClassesAndMethods(t *testing.T) {
	src := `<?php
interface ShapeInterface {
    public function area(): float;
}

trait Labelable {
    public function label(): string {
        return "x";
    }
}

class Circle {
    use Labelable;

    private float $radius = 1.0;

    public function area(): float {
        return 3.14 * $this->radius;
    }

    public function helper(): int {
        return 42;
    }
}
`
	doc := parsePHP(t, "classes.php", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["ShapeInterface"] != "interface" {
		t.Errorf("ShapeInterface type = %q, want interface", byName["ShapeInterface"])
	}
	if byName["Labelable"] != "trait" {
		t.Errorf("Labelable type = %q, want trait", byName["Labelable"])
	}
	if byName["Circle"] != "class" {
		t.Errorf("Circle type = %q, want class", byName["Circle"])
	}
	if byName["Circle.area"] != "method" {
		t.Errorf("Circle.area type = %q, want method", byName["Circle.area"])
	}
	if byName["Circle.helper"] != "method" {
		t.Errorf("Circle.helper type = %q, want method", byName["Circle.helper"])
	}
	if byName["Labelable.label"] != "method" {
		t.Errorf("Labelable.label type = %q, want method", byName["Labelable.label"])
	}
	if byName["area"] != "" {
		t.Errorf("method should not be registered under bare name, got %q", byName["area"])
	}
}

func TestParse_PHPFile_TopLevelFunction(t *testing.T) {
	src := `<?php
function square(int $n): int {
    return $n * $n;
}

echo square(4);
`
	doc := parsePHP(t, "funcs.php", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["square"] != "function" {
		t.Errorf("square type = %q, want function", byName["square"])
	}
}

func TestParse_PHPFile_Calls(t *testing.T) {
	src := `<?php
namespace App;

class Reporter {
    public static function format(int $n): string {
        return (string) $n;
    }
}

class Main {
    public function go(): string {
        return format($this->n());
    }

    public function n(): int {
        return 7;
    }

    public function format(int $v): string {
        return (string) $v;
    }

    public function build(): Reporter {
        return new Reporter();
    }
}
`
	doc := parsePHP(t, "calls.php", src)

	names := nameByID(doc)
	calls := map[string]bool{}
	for _, l := range doc.Links {
		if l.Type == "calls" {
			calls[names[l.SourceID]+"->"+names[l.TargetID]] = true
		}
	}
	// The bare call format(...) inside Main.go resolves to the
	// class-scoped method "Main.format"; "new Reporter()" resolves to
	// the class entity.  "$this->n()" is a member call and must not
	// resolve.
	if !calls["Main.go->Main.format"] {
		t.Errorf("expected Main.go -> Main.format call, got %v", calls)
	}
	if !calls["Main.build->Reporter"] {
		t.Errorf("expected Main.build -> Reporter construction, got %v", calls)
	}
	if calls["Main.go->Main.n"] {
		t.Errorf("$this->n() is a member call and must not resolve, got %v", calls)
	}
}

func TestParse_PHPFile_NoMemberCallResolution(t *testing.T) {
	src := `<?php
class Service {
    public function helper(): int {
        return 1;
    }

    public function run(): int {
        return $this->helper();
    }
}
`
	doc := parsePHP(t, "member_calls.php", src)

	for _, l := range doc.Links {
		if l.Type == "calls" {
			t.Errorf("$this->helper() is a member call and must not resolve, got link %+v", l)
		}
	}
}

func TestParse_PHPFile_SyntacticallyInvalid(t *testing.T) {
	src := `<?php
class Broken {
    public function run( {
        return @@@;
    }
}
`
	doc := parsePHP(t, "broken.php", src)

	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
