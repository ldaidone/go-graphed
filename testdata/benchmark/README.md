# kg benchmark

Scripts to run **inside a target project** (of any supported language) to
collect a normalized, machine-readable result for the beta evaluation of
`kg`. They are standalone and dependency-free (Python 3 stdlib only) — they do
not touch the kg CLI, its Go code, or this repo's build. Everything lives under
`testdata/`, so the Go toolchain ignores it.

> Status: **v1 — data collection (bench.py) + scoring (score.py).** `bench.py`
> gathers raw evidence (timing, graph stats, ground-truth imports); `score.py`
> runs golden-query retrieval ablations over MCP and measures token cost and
> recall against baselines. A future `kg benchmark` command will bundle both.

## What a run produces

`kg-bench-<target>.json` (or `kg-benchmark-report.json` when no `--outdir`):

| Section | Contents |
| --- | --- |
| `target` | git rev/branch, file counts and bytes per detected format |
| `build` | `kg build` wall time, peak RSS (`RUSAGE_CHILDREN`), flags, exit code, log tail |
| `graph` | read back from `graph.json`: docs, entities, links split by `extracted`/`inferred` provenance, link types, hub list (top 20 by PageRank), packages, clusters per kind |
| `metrics_text` / `clusters_text` | raw `kg metrics` / `kg clusters` terminal output for eyeballing |
| `ground_truth` | per-file per-language import lists, extracted lexically with stdlib (`ast` for Python) — the future scoring harness compares kg's import resolution against these |

The hub list and cluster breakdown for well-known repos are deliberately
included so the results are human-sanity-checkable ("is the DTO model file
really the God Node here?") without needing the scoring harness.

## Requirements

- `python3` (stdlib only)
- `kg` binary on `PATH`, or pass `--kg /path/to/kg`

## Usage

Run from inside each target project, pointing every run at one shared output
directory so results across languages accumulate in one place:

```bash
cd /path/to/go-ethereum
python3 /path/to/go-graphed/testdata/benchmark/lib/bench.py . \
  --outdir ~/kg-bench/results

cd /path/to/django
python3 /path/to/go-graphed/testdata/benchmark/lib/bench.py . \
  --outdir ~/kg-bench/results

# JavaScript / TypeScript
python3 /path/to/go-graphed/testdata/benchmark/lib/bench.py ./react \
  --outdir ~/kg-bench/results
```

Or use the wrapper (same options, forwarded to `bench.py`):

```bash
/path/to/go-graphed/testdata/benchmark/run.sh . --outdir ~/kg-bench/results
```

### Options (see `python3 lib/bench.py --help`)

| Flag | Purpose |
| --- | --- |
| `--kg PATH` | kg binary (default: `kg` on PATH) |
| `--model PATH` | embedding model path; when set, `kg build` embeds (semantic retrieval scoring later) |
| `--build-flags STR` | extra `kg build` flags, shell-split (e.g. `--jobs 8 --exclude vendor/`) |
| `--skip-build` | reuse an existing `graph.json` (fast iteration) |
| `--no-ground-truth` | skip import extraction |
| `--outdir DIR` | directory for the report file (recommended) |
| `--output PATH` | explicit report file path |

### Embedding-enabled build

To collect reports that also carry semantic embeddings (needed if the later
retrieval scoring uses `get_narrowed_context`):

```bash
python3 lib/bench.py . \
  --model ~/.config/graphed/models/get-small.gtemodel \
  --outdir ~/kg-bench/results
```

## Suggested corpus (one repo per language)

Known repos let anyone sanity-check the hub/cluster output by hand:

| Language | Target | Ground truth |
| --- | --- | --- |
| Go | go-ethereum | `lexical` |
| Python | django | `ast` |
| JavaScript/TypeScript | react (or a tRPC monorepo) | `lexical` |
| Kotlin | a Spring Boot service | `lexical` |
| Java | elasticsearch | `lexical` |
| Rust | serde / tokio | `lexical` |
| C# | aspnetcore | `lexical` |
| Ruby | rails | `lexical` |
| PHP | laravel/framework | `lexical` |
| C / C++ | sqlite3 / node | `lexical` |

## Scoring (`score.py`)

Golden-query retrieval ablation, driven over MCP stdio (same transport agents
use). For each query it measures **token cost** (kg's own `len(runes)/4`
heuristic, `internal/mcp/handlers.go`) and **recall@files** (fraction of golden
answer files whose path appears in the retrieved context) for three methods:

| Method | Cost model | Recall model |
| --- | --- | --- |
| `kg_narrowed` | `get_narrowed_context` response, capped at `--max-tokens` (default 1500) | golden answer paths found in the response |
| `kg_topology` | `get_document_details` + `get_document_links` for the entry file (unbounded) | golden answer paths found |
| `grep` | top-10 source/doc files ranked by keyword-hit count, read in full | golden answer paths in those files |

Plus a **read-all** baseline (every indexed source/doc file in full). Token
reduction is reported relative to read-all. `--graphify` adds an external
`graphify query` comparison when installed.

```bash
# inside go-graphed (uses ./graph.json + local kg binary)
python3 testdata/benchmark/lib/score.py . --kg ./kg \
  --queries testdata/benchmark/queries/go-graphed.json

# inside an external repo with an embedding-enabled graph.json
python3 .../score.py /path/to/repo --kg /path/to/kg \
  --queries /path/to/my-corpus.json
```

### Golden query corpora

- `queries/go-graphed.json` — 7 questions about this repo (public, committed).
- Private corpora (a Kotlin Spring Boot service, a React/Redux app) are kept
  out of the repository — see the gitignore — and live on the machine that
  collected the measured results below. Answers are the files that genuinely
  answer the question *and* are reachable in the graph (verified file-level
  neighbors, accounting for entity-level `file.kt#entity` link IDs).

### Measured results (Aug 2026)

| Target | read-all | kg_narrowed (1.5k cap) | kg_topology | grep top-10 |
| --- | --- | --- | --- | --- |
| go-graphed (Go, 116 src/docs files) | 458,869 tok | 1,557 tok · recall 0.55 · **272×** | 1,254 tok · recall 0.48 | 262,452 tok · recall 0.50 |
| Kotlin microservice (private, 319 files) | 398,313 tok | 1,717 tok · recall 0.43 · **233×** | 10,102 tok · recall 1.00 | 101,910 tok · recall 0.33 |
| React/Redux app (private, 233 files) | 91,313 tok | 1,593 tok · recall 0.64 · **52×** | 1,141 tok · recall 1.00 | 18,820 tok · recall 0.14 |

`kg_narrowed` delivers **12–168× fewer tokens than grep at equal or better
recall** on every repo; vs read-everything it is a **52–272× reduction**.
See `docs/BENCHMARK_RESULTS.md` for the full write-up.

## What the scripts deliberately do NOT do (yet)

- **Score an LLM's final answer.** The retrieval side is scored (above); end-
  to-end "does the model answer correctly" stays a manual dogfooding step.
- **Run an end-to-end eval across the full suggested corpus.** Add the remaining
  language repos, then `bench.py` + `score.py` per repo and aggregate.
- **Collect human feedback.** Dogfooding stays manual.

## Caveats / known limitations of the ground truth

- `method: "lexical"` imports are extracted with regexes (stdlib only). They
  are reliable for lexical constructs (`import`/`using`/`use`/`#include`
  lines) but not a semantic resolution — treat them as "should resolve" hints,
  not as the analyzer's target.
- Line comments are stripped for Go before import extraction; a `//` inside a
  string literal on an import line is a theoretical false-positive source
  (rare in practice).
- Big vendored trees are excluded via `SKIP_DIRS`/`SKIP_FILES`; a target repo
  with unusual dependency folders should set `--build-flags`/`--exclude`
  accordingly.

## Findings from the scorer runs

1. **grep's real cost is opening files, not grepping.** The keyword match is
   cheap, but the agent reads whole candidate files. Measured that way, grep
   costs 19k–261k tokens per query across the three repos (1.7–5.3× reduction
   vs read-all) and tops out at recall 0.50 — while `kg_narrowed` gets
   0.43–0.64 at ~1.5–1.7k tokens. Reporting grep as "comparable to kg" would
   have been an artifact; the harness now counts the full files it would make
   the agent open.
2. **Same-package references are not linked in Kotlin graphs.** In a private
   Kotlin graph, `OrderService.kt` → `ParcelService.kt` (same
   package, no `import`) has no edge, while cross-package references
   (`PdfClient.kt` → `PdfDto.kt`) are linked. Kotlin classes in the same
   package are reachable only through package-node links, so 1-hop
   `get_narrowed_context` misses sibling services. Worth checking the Kotlin
   extractor.
3. **Entry-file dominance.** `get_narrowed_context` fills the token budget with
   the entry file's own snippets (a hub file with 27 snippets consumed a 3000-
   token budget entirely); neighbor files appear only with a much larger budget
   and are semantic-score-gated (`minScore`). Recall at a 1.5k cap is therefore
   mostly "did the entry's neighbors surface", not "did the right files surface".
4. **`kg_topology` is unbounded but high-recall.** `get_document_links` lists
   every 1-hop neighbor path, so when answers are 1-hop reachable recall is 1.0
   — at 6–13× the token cost of narrowed context. It is the reliable file-location
   tool; narrowed context is the token-saver.
5. **Baseline hygiene matters.** SQL dumps, spreadsheets, unstructured blobs,
   and kg's own `graph.json`/`graph.html` (when self-indexed) dominate token
   baselines and grep hits; the baselines exclude them so numbers reflect what
   an agent would actually read.

## Real issue found while building the harness

`kg build .` **indexes its own freshly written `graph.json`** when the output
file lands inside the scanned root (verified: a 3-file Python fixture jumped
from 8 to 135 entities because `graph.json` was indexed as a JSON document).
The benchmark scripts sidestep this by always passing
`--exclude graph.json --exclude kg-bench*`, so the reported counts reflect the
target codebase only. For real users, kg should self-exclude its own output
when it is written under the scanned root — worth a fix.
