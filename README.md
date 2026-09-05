# go-graphed

Language-agnostic knowledge graph generator for source code.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![GoDoc](https://godoc.org/github.com/ldaidone/gomemo?status.svg)](https://pkg.go.dev/github.com/ldaidone/go-graphed)
[![GitHub stars](https://img.shields.io/github/stars/ldaidone/go-graphed.svg)](https://github.com/ldaidone/go-graphed/stargazers)
![Beta](https://img.shields.io/badge/status-beta-yellow)

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/leodaido)

## Overview

`go-graphed` scans a directory of source code files, extracts semantic entities (structs, interfaces, headings, tables, etc.), and produces a JSON knowledge graph describing the codebase's structure as documents, entities, and relationships. It follows a clean **scanner → parser → analyzer → exporter** pipeline, making it easy to add new languages or output formats without touching the rest of the system. The built graph powers a **local MCP server** so AI agents can query the codebase structure and get semantically narrowed context without shipping your code to a cloud model.

## Why go-graphed?

AI agents are only as good as the context you give them. Without a graph, answering a question about a codebase means reading **~90k–460k tokens** of source. `go-graphed` turns your repo into a local knowledge graph — entities, cross-file links, hub ("God Node") files, and subsystem clusters — and serves it over MCP so agents pull a **~1.5–1.7k-token, context-bounded slice** instead.

Measured, reproducible numbers ([full write-up](docs/BENCHMARK_RESULTS.md)):

- **52–272× fewer tokens** than reading the whole codebase per question.
- **Equal or better recall than `grep` with 12–168× fewer tokens** — grep's real cost is opening the files it matches.
- **Hub detection matches structural intuition** (DTOs, middleware, and routers surface as the coupling hotspots).
- **Local-only**: graphs and vector stores live on your machine; nothing leaves it.

## Features

- **Language-agnostic pipeline**: Scanner, parser, analyzer, and exporter are fully decoupled — add a new language by writing one extractor function
- **Tree-sitter powered**: Uses pure-Go tree-sitter grammars for Go, Markdown, JSON, YAML, TOML, JS/TS, Python, Rust, SQL, Bash, Java, Kotlin, PHP, C#, Swift, Ruby, Elixir, C, C++, Dockerfile, and Makefiles, resilient to syntax errors
- **Cross-reference heuristics**: Automatically infers `implements` links between structs and interfaces via naming conventions
- **Cross-file import resolution**: One generic resolver maps `import` entities onto indexed files for every language with a module system — JS/TS relative paths and `@/` aliases, Java/Kotlin dotted packages, Python modules, Rust `use` paths, PHP namespaces, C# `using`, C/C++ quoted headers, Ruby requires, Elixir aliases, Swift modules. Exact relative/local imports are tagged `extracted` at full weight; namespace/suffix matches are `inferred`. Unresolvable system or external imports are left unlinked, never dangling
- **Cross-file reference resolution**: Type/identifier mentions (Kotlin/Java/Swift/C++ constructor injection, fields, supertypes, and variable declarations) resolve onto the classes/structs that declare them, disambiguated by the file's imports, so a Spring-style controller → service → repository chain shows real `references` edges
- **Package/module aggregation**: Aggregates files by their declared package (Go package clauses, JVM package headers, PHP/C# namespaces, Elixir's top-level module) into package nodes with `part_of` edges — module clusters, hub ranking, and topological traversal all work for non-Go languages
- **Document-to-code linking**: Connects Markdown/PDF documentation to the code entities they mention
- **Link provenance**: Every edge is tagged `extracted` (parsed directly from source) or `inferred` (derived by heuristics), so consumers can separate facts from guesses
- **Global centrality ("God Node") metrics**: Computes per-document degree, weighted degree, and PageRank over the coupling graph and flags hub documents (`is_hub`) — central DB drivers, middleware, and routers surface automatically. Markdown keyword links are excluded so docs cannot out-rank real source coupling
- **Subsystem clustering**: Groups documents by directory tree, Go package (or any language's package/module), and network coupling (greedy modularity) into higher-level domain units
- **Semantic embeddings**: Optional pure-Go embedding of documents and entities (gte model, no CGO) with incremental rebuilds — unchanged nodes are skipped and stale vectors are pruned. Noisy/structural entity types (imports, links, config data, plain variables) are skipped by default to keep large projects fast; tune with `--embed-skip-types`
- **Flexible IR**: Intermediate representation supports documents, entities, packages, clusters, and weighted links with metadata
- **Extensible exporters**: JSON output plus a self-contained HTML visualizer and a markdown report (`kg visualize`); GraphML, RDF, or Cypher can be added as new exporter files
- **Agent-ready MCP server**: `kg mcp` exposes the graph over stdio with tools for document details, links, entity search, cluster inspection, centrality metrics, and hybrid topological + semantic context narrowing
- **Go-style path support**: Accepts `./...` wildcard syntax familiar to Go developers
- **Gitignore-aware scanning**: Honors `.gitignore` files plus repeatable `--exclude` and `--ignore-file` patterns, and drops VCS internals (`.git`, `.hg`, `.svn`), binary assets (fonts, images, archives, media), and lockfiles by default to keep corpora focused (disable with `--no-default-skip`)
- **Agent-ready setup**: `kg init` emits per-project agent rules **and** per-client MCP configs wiring the server in, and `kg install` puts the binary + model on PATH globally

## Benchmarks

Measured on a real Kotlin/Spring microservice, a React/Redux web app, and
go-graphed itself (all re-run through a reproducible, dependency-free harness —
see [`testdata/benchmark/`](testdata/benchmark/README.md)). Retrieval is scored
on the same token metric agents actually pay, with golden queries whose answer
files were hand-validated as graph-reachable. Nothing here is an LLM's opinion.

| Case study | read-all baseline | `kg_narrowed` (1.5k cap) | `kg_topology` | grep top-10 |
| --- | --- | --- | --- | --- |
| go-graphed (Go) | 458,869 tok | 1,557 tok · recall 0.55 · **272×** | 1,254 tok · recall 0.48 | 262,452 tok · recall 0.50 |
| Kotlin microservice (private) | 398,313 tok | 1,717 tok · recall 0.43 · **233×** | 10,102 tok · recall 1.00 | 101,910 tok · recall 0.33 |
| React/Redux app (private) | 91,313 tok | 1,593 tok · recall 0.64 · **52×** | 1,141 tok · recall 1.00 | 18,820 tok · recall 0.14 |

**What the numbers mean**

- **`kg_narrowed` is the token-saver**: ~1.5–1.7k tokens per question, 52–272× less than reading the codebase. That's the number an agent actually pays.
- **`kg_topology` is the reliable file-locator**: `get_document_links` returns every 1-hop neighbor, reaching recall 1.00 when answers are 1-hop away — at a cost between grep's and narrowed context's.
- **grep loses because of what it costs after the match**: the keyword step is cheap, but the agent then opens whole files. On the smallest repo grep gets recall 0.14 at 18,820 tokens; `kg` gets 0.64 at 1,593.
- Hub/God-Node ranking was cross-checked against the real repository layout on all three codebases.

Full methodology, per-query breakdowns, and known caveats:
[`docs/BENCHMARK_RESULTS.md`](docs/BENCHMARK_RESULTS.md).

## Known limitations (beta)

Owned, not hidden — reproduced and tracked in the benchmark report:

1. **`kg build` can index its own output.** If `graph.json` is written inside the scanned root it is indexed as a JSON document. A self-exclusion fix is planned; the benchmark harness excludes it today.
2. **Kotlin same-package references are unlinked.** A Kotlin service referencing a sibling service in the same package (no `import`) has no graph edge, so it is not 1-hop reachable. Cross-package references work.
3. **Test files can rank as hubs.** A test-deweighting option for hub ranking is planned.
4. **`get_narrowed_context` is entry-file-dominant.** A hub entry file's own snippets can exhaust the token budget before neighbor files appear; a per-file snippet cap is under consideration.
5. **Repo-boundary only.** External APIs, config, and DB schemas are not linked; `extracted`/`inferred` provenance tags make clear what is a fact vs. a heuristic.
6. **Snapshot staleness.** The graph is a build snapshot — rebuild with `kg build` after significant code changes.

## Installation

The `kg` CLI binary:

```bash
go install github.com/ldaidone/go-graphed/cmd/kg@latest
```

The Go library (`graphed.Build`, `graphed.BuildOptions`):

```bash
go get github.com/ldaidone/go-graphed
```

Or build from source:

```bash
git clone https://github.com/ldaidone/go-graphed.git
cd go-graphed
make build           # or: go build -o kg ./cmd/kg
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

# VCS internals (.git), binary assets (fonts, images, archives) and
# lockfiles are skipped by default; pass --no-default-skip to index them
./kg build . --no-default-skip

# Parallelize parsing across 8 workers
./kg build . --jobs 8

# Skip noisy entity types when embedding so large projects build faster.
# Structural/noisy types (import, link, config properties, variables, ...) are
# skipped by default; pass "" to embed every entity type.
./kg build . --embed-skip-types ""
./kg build . --embed-skip-types function,method,class,heading

# Generate a self-contained interactive visualizer (graph.html) and a
# markdown report (GRAPH_REPORT.md) summarizing hubs, coupling, and clusters
./kg visualize

# Customize the artifact paths
./kg visualize --html out/graph.html --report out/GRAPH_REPORT.md

# Print global centrality ("God Node") metrics: hubs first, ranked by PageRank
./kg metrics
./kg metrics --file out/graph.json

# Summarize subsystem clusters (directory, module, or network coupling)
./kg clusters
./kg clusters --kind network

# Serve the graph as an MCP server over stdio (JSON-RPC tools for agents)
./kg mcp --file graph.json
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

# Skip auto-detection and target specific ecosystems explicitly instead.
# Existing rule files are still never overwritten: the kg block is appended,
# or replaced in place between its markers on re-runs.
./kg init --targets claude,copilot,gemini
./kg init --all   # every ecosystem: agents, claude, gemini, copilot, codex, cursor, cline, opencode, windsurf

# Build the graph first so the snapshot the rules reference exists
./kg init --build

# Rules only — skip writing the client MCP config files below
./kg init --no-mcp

# Launch the server through a custom command instead of "kg" on PATH
./kg init --mcp-command /path/to/kg

# If the graph snapshot is missing, kg init prints a reminder to run kg build.
```

Besides the rule files, `kg init` also writes a project-local MCP config that
makes each client actually launch `kg mcp` — rule files alone only describe the
tools. Only the `"kg"` entry is written or replaced; every other key in an
existing config file is preserved, and files that cannot be parsed are left
untouched with a warning.

| Target | Rule file | MCP config |
| --- | --- | --- |
| agents | `AGENTS.md` | — |
| opencode | `AGENTS.md` | `opencode.json` / `opencode.jsonc` |
| claude | `CLAUDE.md` | `.mcp.json` |
| gemini | `GEMINI.md` | `.gemini/settings.json` |
| copilot | `.github/copilot-instructions.md` | `.github/mcp.json` |
| codex | `AGENTS.md` | `.codex/config.toml` |
| cursor | `.cursor/rules/*.mdc` | `.cursor/mcp.json` |
| cline | `.clinerules/` | `.cline/mcp.json` |
| windsurf | `.windsurf/rules/` | global `~/.codeium/windsurf/mcp_config.json` (documented in the rule file; kg never edits files outside the project) |

The generated `kg` entry is project-root relative: it launches `kg mcp --file
graph.json` (or the `--mcp-command` you pass) and runs in the project directory,
so the configs stay portable across machines.

```bash
# Install the binary and the bundled embedding model for global use.
# The model is downloaded from the go-graphed repository by default so the
# installed copy is always complete; --model-path copies a local file instead.
# Binary goes into GOBIN/GOPATH/bin, model into ~/.config/graphed/models
./kg install
./kg install --model-path ./get-small.gtemodel
```

### MCP Server

`kg mcp` starts a Model Context Protocol (MCP) server over standard I/O that
exposes the built graph to AI agents. It reads only the local `graph.json` and
the local vector store — nothing leaves the machine. Wire it into your agent
via its MCP configuration (stdio transport, command `kg mcp`).

```bash
./kg mcp                 # defaults: --file graph.json, model/db from config
./kg mcp --file graph.json --model-path ./get-small.gtemodel
```

Configure the embedding model path and vector-store directory with
`--model-path` / `--db-root` flags or the `GRAPHEAD_MODEL_PATH` /
`GRAPHEAD_DB_ROOT` environment variables. The server exposes these tools:

| Tool | Purpose |
| --- | --- |
| `get_narrowed_context` | Hybrid topological + semantic search returning a focused, token-bounded context slice. `entryPath` accepts a single file or a directory (a directory expands to every indexed file beneath it) |
| `get_document_details` | Metadata, extracted AST entities, centrality scores, and clusters for one file |
| `get_document_links` | Structural incoming/outgoing links for a file |
| `list_documents_by_format` | List files by format (golang, markdown, ...) |
| `find_entities_by_type` | Global search for entity types (struct, interface, heading, ...) |
| `list_clusters` | List graph node clusters by directory tree, Go module, or network coupling |
| `get_cluster` | Members and metadata of one cluster |
| `get_graph_metrics` | Global centrality metrics and hub ("God Node") documents |

`kg init` generates per-project agent rules documenting this workflow and the
tools above, and writes each client's MCP config so `kg mcp` actually launches
(see [Agent Rules and Global Install](#agent-rules-and-global-install)). Agents
then query the local graph before calling cloud models.

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
    "BuiltAt": "2026-08-05T12:00:00Z",
    "Documents": {
        "internal/parser/parser.go": {
            "Path": "internal/parser/parser.go",
            "Format": "golang",
            "Size": 2048,
            "UpdatedAt": "2026-08-05T11:30:00Z",
            "Entities": [
                {
                    "ID": "internal/parser/parser.go#Parse",
                    "Type": "struct",
                    "Name": "Parse",
                    "Metadata": { "start_line": "42", "end_line": "58" }
                }
            ],
            "Metadata": { "is_hub": "true" }
        }
    },
    "Links": [
        {
            "SourceID": "internal/parser/parser.go#GraphBuilder",
            "TargetID": "internal/scanner/scanner.go#Scanner",
            "Type": "implements",
            "Weight": 0.8,
            "SourceType": "inferred"
        }
    ],
    "Packages": {
        "internal/parser": { "Name": "parser", "Files": ["internal/parser/parser.go"] }
    },
    "Clusters": [
        { "ID": "directory:internal/parser", "Name": "internal/parser", "Kind": "directory", "Members": ["internal/parser/parser.go"], "Size": 1 }
    ],
    "Metrics": {
        "HubCount": 1,
        "Documents": {
            "internal/parser/parser.go": { "Degree": 4, "WeightedDegree": 3.2, "PageRank": 0.31, "IsHub": true }
        }
    }
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

- **Scanner** (`internal/scanner/`): Walks the filesystem and detects file types (golang, pdf, markdown, spreadsheet, json, yaml, toml, javascript, typescript, python, rust, sql, bash, java, kotlin, php, csharp, dockerfile, make, unstructured) by extension and convention filenames. The `Scanner` interface allows swapping in alternative implementations (e.g., git-aware traversal).
- **Parser** (`internal/parser/`): Dispatches to language-specific extractors. Go, Markdown, JSON, YAML, TOML, JavaScript, TypeScript/TSX, Python, Rust, SQL, Bash, Java, Kotlin, PHP, C#, Swift, Ruby, Elixir, C, C++, Dockerfile, and Makefiles are implemented via pure-Go tree-sitter; PDF uses a pure-Go text extractor and spreadsheets use the stdlib + excelize. Go extraction additionally produces a within-file call graph. Python, Rust, SQL, Bash, Java, Kotlin, PHP, C#, Swift, Ruby, C, and C++ run on a shared config-driven two-pass walker (`internal/parser/walker.go`) that new languages plug into (it also emits `reference` entities for type mentions and `package` entities for package/namespace clauses); Elixir uses a dedicated two-pass extractor (its grammar expresses modules, functions, and directives as uniform call nodes); migrating Go/JS/TS onto the walker is a later, optional step.
- **Analyzer** (`internal/analyzer/`): Assembles documents into a `Graph`, builds a global entity registry, runs heuristic passes to infer cross-reference links (naming conventions, path keyword matching), lifts parser-produced document links (e.g., within-file `calls` links), resolves cross-file `imports` for every language with an import shape, resolves type `references` against the global declaration index with import-context disambiguation, aggregates packages (Go, JVM, PHP/C#, Elixir) and emits `part_of` edges, groups documents into clusters (directory / module / network coupling via greedy modularity), and computes global centrality ("God Node") metrics (degree, weighted degree, PageRank) that flag hub documents.
- **Exporter** (`internal/exporter/`): Serializes the IR graph to a concrete output format. `JSON` writes graph.json; `HTML` renders a self-contained, dependency-free interactive visualizer (force layout, pan/zoom, filtering, per-node detail panel); `Report` writes a markdown summary mirroring the `kg metrics` and `kg clusters` terminal views. Other formats can be added as separate files.
- **IR** (`internal/ir/`): Shared intermediate representation (`Graph`, `Document`, `Entity`, `Link`, `Package`, `Cluster`, `Metrics`) that all pipeline stages agree on. Links carry a provenance tag (`extracted` vs `inferred`); documents can be flagged as hubs (`is_hub`).
- **MCP** (`internal/mcp/`): The MCP server used by `kg mcp`, exposing the graph as JSON-RPC tools over stdio, including the hybrid topological + semantic `get_narrowed_context` tool backed by the vector store.
- **Vector store**: SQLite-backed store (WAL mode, safe for multiple concurrent MCP servers) for the document/entity embeddings produced at build time when a model is configured. The backend is sourced from the `goembedx/pkg/store` factory (`store.NewSQLite`) rather than a local implementation, and searches go through the module's `embedx.Searcher` interface. Rebuilds are incremental: unchanged payloads are skipped via stored hashes and stale vectors are pruned.

### Supported Languages

| Language    | Status        | Extractor                     |
|-------------|---------------|-------------------------------|
| Go          | Implemented   | tree-sitter + call graph      |
| Markdown    | Implemented   | tree-sitter                   |
| JSON        | Implemented   | tree-sitter                   |
| YAML        | Implemented   | tree-sitter                   |
| TOML        | Implemented   | tree-sitter                   |
| JavaScript  | Implemented   | tree-sitter                   |
| TypeScript/TSX | Implemented | tree-sitter                 |
| Python      | Implemented   | tree-sitter (shared walker)   |
| Rust        | Implemented   | tree-sitter (shared walker)   |
| SQL         | Implemented   | tree-sitter (shared walker)   |
| Shell/Bash  | Implemented   | tree-sitter (shared walker)   |
| Java        | Implemented   | tree-sitter (shared walker)   |
| Kotlin      | Implemented   | tree-sitter (shared walker)   |
| PHP         | Implemented   | tree-sitter (shared walker)   |
| C#          | Implemented   | tree-sitter (shared walker)   |
| Swift       | Implemented   | tree-sitter (shared walker)   |
| Ruby        | Implemented   | tree-sitter (shared walker)   |
| Elixir      | Implemented   | tree-sitter (dedicated)       |
| C           | Implemented   | tree-sitter (shared walker)   |
| C++         | Implemented   | tree-sitter (shared walker)   |
| Dockerfile  | Implemented   | tree-sitter                   |
| Makefile    | Implemented   | tree-sitter                   |
| PDF         | Implemented   | pure-Go text extraction       |
| Spreadsheet | Implemented   | stdlib CSV / excelize XLSX    |
| Other       | Fallback      | Generic unstructured          |

## Development

### Prerequisites

- Go 1.26+
- No C toolchain required: the pipeline is pure-Go and builds with `CGO_ENABLED=0`

### Building

```bash
make build           # or: go build -o kg ./cmd/kg
```

### Running Tests

```bash
# Run all tests
make test            # or: go test ./...

# Run with race detection
make test-race       # or: go test -race ./...

# Run with coverage
make coverage        # or: go test -coverprofile=coverage.out ./...
                     #    go tool cover -html=coverage.out
```

### Static Analysis

```bash
make vet             # or: go vet ./...
```

Other Makefile targets: `make run` / `make run-build` (run kg / kg build with
`ARGS=...`), `make run-mcp` (run `kg mcp` with `ARGS=...`), `make tidy`, and
`make clean`. Run `make help` for the full list.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Run `make vet` and `make test-race` (or `go vet ./...` and `go test -race ./...`)
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
