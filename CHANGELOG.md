# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.0.1] - 2026-09-24

### Fixed

- Rebuilds now update the existing snapshot instead of replacing it: freshly parsed documents win, entries for files that still exist but fell outside the scan (`--changed-only`, narrower roots, excludes) are kept, and entries for deleted files are dropped. A corrupt `graph.json` warns and rebuilds fresh. First builds (no snapshot yet) always scan fully, even with `--changed-only`.

### Added

- Opt-in git awareness (`--git-aware`, `--changed-only`, `--git-limit`): pure-Go (go-git, no binary needed) working-tree status + HEAD metadata per document and inferred `co_changed` links from recent commits. Non-repos degrade to a warning.
- MCP `--auto-rebuild` bouncer (off by default): per-request git fingerprint check against `--root`; dirty trees kick one debounced background rebuild (singleflight) while the current snapshot serves, then hot-swap. Shared `git.Fingerprint` helper ready for the future `query` command.
- `kg init` parity: `--git-aware` / `--changed-only` / `--git-limit` passthrough for `--build`, plus `--output`/`-o` as an alias for `--graph-file` so both commands spell the snapshot path the same way.

### Changed

- Upgraded `github.com/odvcencio/gotreesitter` from `v0.47.1` to `v0.53.0` (no extractor fallout; full parser suite green).
- Godoc-compliance fixes for the remaining exported symbols (`cli` command groups).

## [v1.0.0] - 2026-09-22

### Fixed

- Self-exclusion of the `graph.json` output from scans (`kg build .` no longer indexes its own prior output).
- MCP server panic recovery: panicking tool handlers and server goroutine now surface JSON-RPC errors instead of killing the stdio session.

### Changed

- Versioned `graph.json` format (`schema_version: 1`); loader warns leniently on missing (pre-v1.0) or newer versions.
- Quiet-by-default `kg build` output (progress moved to stderr behind `--verbose`/`-v`).
- `kg --version` reports the release version.
- Docs: stable status (see [docs/plan_stable_1.md](docs/plan_stable_1.md)), benchmark results revalidated for v1.0.0.

## [v0.1.0-beta.2] - 2026-09-05

### Changed

- **Vector store now comes from goembedx v0.4.0.** Upgraded `goembedx` from
  `v0.3.0` to `v0.4.0` and replaced the local storage backends with the
  module's `pkg/store` factory (`store.NewSQLite`).
- **Semantic search drives through `embedx.Searcher`.** The MCP server now
  retrieves via `SearchContext(ctx, query, embedx.WithK(k))` instead of the
  legacy `Embedder.Search` path.
- Delayed deps cleanup: `badger/v4` and `modernc.org/sqlite` now resolve as
  indirect dependencies of `goembedx` rather than direct ones.
- Godoc-compliance fixes across parser, analyzer, and MCP code.
- README architecture notes updated to match the storage change.

### Removed

- The entire `internal/utils/vector_store` package (custom Badger and SQLite
  stores, their shared interface, and ~1400 lines of backend/test code) in
  favor of the upstream goembedx implementation.

## [v0.1.0-beta.1] - 2026-08-11

### Added

- **Full build pipeline** wiring scan → parse → analyze → export.
- **Tree-sitter parsing** for Go, Markdown, JSON, YAML, TOML, JavaScript,
  TypeScript/TSX, Python, Rust, SQL, Bash, Java, Kotlin, PHP, C#, Swift, Ruby,
  Elixir, C, C++, Dockerfile, and Makefiles.
- **Binary-format extractors** for PDF and spreadsheets (CSV / XLSX).
- **Cross-file import resolution** for every language with a module system
  (exact → suffix → drop tiers with `extracted`/`inferred` provenance).
- **Cross-file reference resolution** (type mentions → declaring entities with
  import-context disambiguation).
- **Generic package/module aggregation** (Go packages, JVM, PHP/C#, Elixir).
- **God-node metrics**: degree, weighted degree, and PageRank per document with
  hub (`is_hub`) flagging.
- **Subsystem clustering** by directory tree, module, and network coupling
  (greedy modularity).
- **Hybrid topological + semantic context narrowing** (`get_narrowed_context`)
  backed by incremental vector embeddings (unchanged payloads skipped, stale
  vectors pruned).
- **MCP server** (`kg mcp`) exposing 8 tools over stdio.
- **`kg init`** — per-project agent rules + per-client MCP config wiring.
- **`kg install`** — global binary + bundled embedding model (downloaded from
  the repository).
- **`kg visualize`** and **`kg metrics` / `kg clusters`** CLI commands.
- **Viper-based config** loader (JSON/YAML/TOML) with CLI → env → .env →
  config → defaults precedence.
- **Gitignore-aware scanning** with repeatable `--exclude` / `--ignore-file`
  patterns and parallel (worker-pool) parsing.
- **Benchmark harness** under `testdata/benchmark/` and benchmark report.

[Unreleased]: https://github.com/ldaidone/go-graphed/compare/v1.0.1...HEAD
[v1.0.1]: https://github.com/ldaidone/go-graphed/compare/v1.0.0...v1.0.1
[v1.0.0]: https://github.com/ldaidone/go-graphed/compare/v0.1.0-beta.2...v1.0.0
[v0.1.0-beta.2]: https://github.com/ldaidone/go-graphed/compare/v0.1.0-beta.1...v0.1.0-beta.2
[v0.1.0-beta.1]: https://github.com/ldaidone/go-graphed/releases/tag/v0.1.0-beta.1