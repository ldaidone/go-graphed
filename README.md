# go-graphed

Language-agnostic knowledge graph generator for source code.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![GitHub stars](https://img.shields.io/github/stars/ldaidone/go-graphed.svg)](https://github.com/ldaidone/go-graphed/stargazers)

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/leodaido)

## Overview

`go-graphed` scans a directory of source code files, extracts semantic entities (structs, interfaces, headings, tables, etc.), and produces a JSON knowledge graph describing the codebase's structure as documents, entities, and relationships. It follows a clean **scanner → parser → analyzer → exporter** pipeline, making it easy to add new languages or output formats without touching the rest of the system.

## Features

- **Language-agnostic pipeline**: Scanner, parser, analyzer, and exporter are fully decoupled — add a new language by writing one extractor function
- **Tree-sitter powered**: Uses tree-sitter for Go and Markdown parsing, resilient to syntax errors and extensible to other grammars
- **Cross-reference heuristics**: Automatically infers `implements` links between structs and interfaces via naming conventions
- **Document-to-code linking**: Connects Markdown/PDF documentation to the code entities they mention
- **Flexible IR**: Intermediate representation supports documents, entities, and weighted links with metadata
- **Extensible exporters**: JSON output today; GraphML, RDF, or Cypher can be added as new exporter files
- **Go-style path support**: Accepts `./...` wildcard syntax familiar to Go developers
- **Gitignore-aware scanning**: Honors `.gitignore` files plus repeatable `--exclude` and `--ignore-file` patterns
- **Agent-ready setup**: `kg init` emits per-project agent rules and `kg install` puts the binary + model on PATH globally

## Installation

```bash
go get github.com/ldaidone/go-graphed
```

Or build from source:

```bash
git clone https://github.com/ldaidone/go-graphed.git
cd go-graphed
go build -o kg ./cmd/kg
```

## Quick Start

### CLI Usage

```bash
# Build a knowledge graph from the current directory
./kg build .

# Specify output file and format
./kg build ./internal --output graph.json --format json

# Use Go-style wildcard paths
./kg build ./...

# Honor gitignore and skip build artifacts
./kg build . --exclude vendor/ --exclude '*.gen.go'

# Parallelize parsing across 8 workers
./kg build . --jobs 8
```

### Agent Rules and Global Install

```bash
# Generate per-project agent rules wiring the MCP server into the agent loop
# so tools query the local graph before calling cloud models.
#
# Tools are detected from the project's existing rule files (CLAUDE.md,
# GEMINI.md, .github/copilot-instructions.md, .cursor/rules/, ...) and
# AGENTS.md is always written as the default rule. Existing files are never
# overwritten: the kg block is appended, and re-runs replace it in place.
./kg init

# Override detection and emit rules for specific ecosystems
./kg init --targets claude,copilot,gemini
./kg init --all   # every ecosystem: agents, claude, gemini, copilot, codex, cursor, cline, windsurf

# Build the graph first so the snapshot the rules reference exists
./kg init --build

# If the graph snapshot is missing, kg init prints a reminder to run kg build.

# Install the binary and the bundled embedding model for global use
# (binary into GOBIN/GOPATH/bin, model into ~/.config/graphed/models)
./kg install
```

### Configuration

Settings resolve through an ordered chain (highest precedence first):

`CLI flags → environment (GRAPHEAD_*) → .env file → config file → defaults`

Config files are read with [viper](https://github.com/spf13/viper) in JSON, YAML,
or TOML. Discovery order: `GRAPHEAD_CONFIG_FILE`, then `./graphed.*`, then
`./.graphed.*`, then `~/.config/graphed/config.*`.

```yaml
# ~/.config/graphed/config.yaml
model_path: /absolute/path/to/get-small.gtemodel
db_root: /absolute/path/to/store
dimensions: 384   # 0 = auto-detect from the model
```

Environment variables: `GRAPHEAD_MODEL_PATH`, `GRAPHEAD_DB_ROOT`,
`GRAPHEAD_DIMENSIONS`, `GRAPHEAD_CONFIG_FILE`.

### Programmatic Usage

```go
package main

import (
    "fmt"
    "github.com/ldaidone/go-graphed/pkg/graphed"
)

func main() {
    opts := graphed.BuildOptions{
        Root:   "./my-project",
        Output: "graph.json",
        Format: "json",
    }

    if err := graphed.Build(opts); err != nil {
        panic(err)
    }
    fmt.Println("Knowledge graph written to graph.json")
}
```

### Example Output

```json
{
    "Documents": {
        "internal/parser/parser.go": {
            "Path": "internal/parser/parser.go",
            "Format": "golang",
            "Entities": [
                {
                    "ID": "internal/parser/parser.go#Parse",
                    "Type": "struct",
                    "Name": "Parse"
                }
            ]
        }
    },
    "Links": [
        {
            "SourceID": "internal/parser/parser.go#GraphBuilder",
            "TargetID": "internal/scanner/scanner.go#Scanner",
            "Type": "implements",
            "Weight": 0.8
        }
    ]
}
```

## Architecture

### Pipeline

```
┌──────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐
│ Scanner  │ ──▶ │  Parser  │ ──▶ │ Analyzer │ ──▶ │ Exporter │
│          │     │          │     │          │     │          │
│ Walks    │     │ Extracts │     │ Builds   │     │ Writes   │
│ files &  │     │ entities │     │ links &  │     │ graph to │
│ detects  │     │ per-lang │     │ cross-   │     │ disk     │
│ languages│     │          │     │ refs     │     │          │
└──────────┘     └──────────┘     └──────────┘     └──────────┘
```

### Core Components

- **Scanner** (`internal/scanner/`): Walks the filesystem and detects file types (golang, pdf, markdown, spreadsheet, unstructured) based on extensions. The `Scanner` interface allows swapping in alternative implementations (e.g., git-aware traversal).
- **Parser** (`internal/parser/`): Dispatches to language-specific extractors. Currently Go and Markdown are fully implemented via tree-sitter; PDF and spreadsheet extractors are stubbed.
- **Analyzer** (`internal/analyzer/`): Assembles documents into a `Graph`, builds a global entity registry, and runs heuristic passes to infer cross-reference links (naming conventions, path keyword matching).
- **Exporter** (`internal/exporter/`): Serializes the IR graph to a concrete output format. JSON is the only format today, but the package structure lets others be added as separate files.
- **IR** (`internal/ir/`): Shared intermediate representation (`Graph`, `Document`, `Entity`, `Link`) that all pipeline stages agree on.

### Supported Languages

| Language   | Status        | Extractor           |
|------------|---------------|---------------------|
| Go         | Implemented   | tree-sitter         |
| Markdown   | Implemented   | tree-sitter         |
| PDF        | Stubbed       | TODO                |
| Spreadsheet| Stubbed       | TODO                |
| Other      | Fallback      | Generic unstructured|

## Development

### Prerequisites

- Go 1.22+
- C compiler (required by tree-sitter CGo bindings)

### Building

```bash
go build -o kg ./cmd/kg
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with race detection
go test -race ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Static Analysis

```bash
go vet ./...
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Run `go vet ./...` and `go test -race ./...`
6. Submit a pull request

### Adding a New Language

1. Add a language key to `detectLanguage()` in `internal/scanner/filesystem.go`
2. Write an `extract*` function in `internal/parser/`
3. Add a case to the `switch` in `Parse()` in `internal/parser/parser.go`

## License

Apache 2.0 License - see the [LICENSE](LICENSE) file for details.

## Support

If you find this tool useful, consider [buying me a coffee](https://www.buymeacoffee.com/leodaido)!

## Acknowledgments

- [go-tree-sitter](https://github.com/smacker/go-tree-sitter) for resilient Go AST parsing
- [cobra](https://github.com/spf13/cobra) for the CLI framework
- Inspired by the need to make codebases machine-readable for AI-assisted navigation and documentation generation
