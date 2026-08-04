package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_Dockerfile(t *testing.T) {
	tmp := t.TempDir()
	src := `FROM node:20
RUN npm ci
COPY . .
ENV FOO=bar
EXPOSE 8080
CMD ["node","index.js"]
`
	path := filepath.Join(tmp, "Dockerfile")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "dockerfile", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "dockerfile-extractor" {
		t.Errorf("Metadata[processor] = %q, want dockerfile-extractor", doc.Metadata["processor"])
	}

	if len(doc.Entities) != 6 {
		t.Fatalf("expected 6 instruction entities, got %d", len(doc.Entities))
	}

	seen := map[string]string{}
	baseImages := map[string]string{}
	for _, e := range doc.Entities {
		if e.Type != "instruction" {
			t.Errorf("entity %q type = %q, want instruction", e.Name, e.Type)
		}
		seen[e.Name] = e.Name
		if bi, ok := e.Metadata["base_image"]; ok {
			baseImages[e.Name] = bi
		}
	}

	for _, kw := range []string{"FROM", "RUN", "COPY", "ENV", "EXPOSE", "CMD"} {
		if seen[kw] == "" {
			t.Errorf("missing instruction entity %q", kw)
		}
	}
	if baseImages["FROM"] != "node:20" {
		t.Errorf("FROM base_image = %q, want node:20", baseImages["FROM"])
	}
}

func TestParse_Dockerfile_MultiStage(t *testing.T) {
	tmp := t.TempDir()
	src := `FROM golang:1.24 AS build
RUN go build -o /out .

FROM alpine:3.20
COPY --from=build /out /bin/app
`
	path := filepath.Join(tmp, "Dockerfile")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "dockerfile", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["FROM"] != "instruction" {
		t.Errorf("FROM type = %q, want instruction", byName["FROM"])
	}
	if byName["RUN"] != "instruction" {
		t.Errorf("RUN type = %q, want instruction", byName["RUN"])
	}
	if byName["COPY"] != "instruction" {
		t.Errorf("COPY type = %q, want instruction", byName["COPY"])
	}
}
