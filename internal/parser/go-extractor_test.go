package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGoData_Structs(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

type Server struct {
	Addr string
	Port int
}

type Config struct {
	Verbose bool
}
`
	path := filepath.Join(tmp, "structs.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	entities, err := extractGoData(path)
	if err != nil {
		t.Fatalf("extractGoData returned unexpected error: %v", err)
	}

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	byName := make(map[string]string)
	for _, e := range entities {
		byName[e.Name] = e.Type
	}

	if byName["Server"] != "struct" {
		t.Errorf("Server type = %q, want %q", byName["Server"], "struct")
	}
	if byName["Config"] != "struct" {
		t.Errorf("Config type = %q, want %q", byName["Config"], "struct")
	}
}

func TestExtractGoData_Interfaces(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

type Reader interface {
	Read(p []byte) (int, error)
}

type Writer interface {
	Write(p []byte) (int, error)
}
`
	path := filepath.Join(tmp, "ifaces.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	entities, err := extractGoData(path)
	if err != nil {
		t.Fatalf("extractGoData returned unexpected error: %v", err)
	}

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	byName := make(map[string]string)
	for _, e := range entities {
		byName[e.Name] = e.Type
	}

	if byName["Reader"] != "interface" {
		t.Errorf("Reader type = %q, want %q", byName["Reader"], "interface")
	}
	if byName["Writer"] != "interface" {
		t.Errorf("Writer type = %q, want %q", byName["Writer"], "interface")
	}
}

func TestExtractGoData_MixedTypes(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

type Store interface {
	Get(key string) (any, error)
}

type MemStore struct {
	data map[string]any
}
`
	path := filepath.Join(tmp, "mixed.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	entities, err := extractGoData(path)
	if err != nil {
		t.Fatalf("extractGoData returned unexpected error: %v", err)
	}

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	for _, e := range entities {
		switch e.Name {
		case "Store":
			if e.Type != "interface" {
				t.Errorf("Store type = %q, want %q", e.Type, "interface")
			}
		case "MemStore":
			if e.Type != "struct" {
				t.Errorf("MemStore type = %q, want %q", e.Type, "struct")
			}
		default:
			t.Errorf("unexpected entity %q", e.Name)
		}
	}
}

func TestExtractGoData_EntityIDs(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

type Widget struct{}
`
	path := filepath.Join(tmp, "ids.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	entities, err := extractGoData(path)
	if err != nil {
		t.Fatalf("extractGoData returned unexpected error: %v", err)
	}

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	wantID := path + "#Widget"
	if entities[0].ID != wantID {
		t.Errorf("ID = %q, want %q", entities[0].ID, wantID)
	}
}

func TestExtractGoData_LineMetadata(t *testing.T) {
	tmp := t.TempDir()
	// Line 3 is the type declaration.
	src := `package demo

type Foo struct{}
`
	path := filepath.Join(tmp, "lines.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	entities, err := extractGoData(path)
	if err != nil {
		t.Fatalf("extractGoData returned unexpected error: %v", err)
	}

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	if entities[0].Metadata["start_line"] != "3" {
		t.Errorf("start_line = %q, want %q", entities[0].Metadata["start_line"], "3")
	}
	if entities[0].Metadata["end_line"] != "3" {
		t.Errorf("end_line = %q, want %q", entities[0].Metadata["end_line"], "3")
	}
}

func TestExtractGoData_NoTypes(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

func hello() string {
	return "world"
}
`
	path := filepath.Join(tmp, "notypes.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	entities, err := extractGoData(path)
	if err != nil {
		t.Fatalf("extractGoData returned unexpected error: %v", err)
	}

	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestExtractGoData_NonexistentFile(t *testing.T) {
	_, err := extractGoData("/nonexistent/file.go")
	if err == nil {
		t.Error("extractGoData with nonexistent file should return an error")
	}
}

func TestExtractGoData_FuncTypesIgnored(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

type MyFunc func(int) error

type MyChan chan string
`
	path := filepath.Join(tmp, "ignored.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	entities, err := extractGoData(path)
	if err != nil {
		t.Fatalf("extractGoData returned unexpected error: %v", err)
	}

	// func types and chan types are not struct_type or interface_type, so ignored.
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}
