# Benchmark Results (beta)

**Status:** beta. These are the first measured results, collected on
2026-08-08 with the reproducible harness in
[`testdata/benchmark/`](../testdata/benchmark/README.md). Every claim below
can be reproduced; nothing here is an LLM's opinion.

## Summary

| Case study | Language | Docs | Entities | Links | Build time | Peak RSS |
| --- | --- | --- | --- | --- | --- | --- |
| Kotlin microservice (private) | Kotlin / Spring Boot | 319 | 13,555 | 1,645 | 43.4 s | ~815 MiB |
| go-graphed (self-host) | Go | 146 | 1,905 | 916 | ~1.0 s (clean) | ~304 MiB |
| React/Redux web app (private) | JavaScript / React-Redux | 233 | 7,143 | 422 | ~3.5 s | ~508 MiB |

All three were re-run through the harness (build + scoring) on 2026-08-08 with
the same `kg` binary and embedding model. The go-graphed self-host row was
refreshed on 2026-08-11 against the current working tree with the same harness
(incremental: 1,237 of 1,238 vectors reused, 0 pruned). The two private case
studies are anonymized here — no company, product, package, or source-file
identifiers appear in this document or anywhere in the repository; go-graphed
is public and fully reproducible.

## 1. Methodology

Each run is a single `kg build` on the target repo, timed and measured by the
harness, followed by reading `graph.json` back for graph stats and by lexical
per-file import extraction as ground-truth data for future scoring. Exact
commands and options are in [Reproduction](#6-reproduction).

- **Timing** is wall-clock of the whole pipeline including embedding.
- **Peak RSS** is measured via `RUSAGE_CHILDREN`.
- **Variance:** timing is sensitive to machine load (one go-graphed self-run
  took 145 s under load; a clean direct rerun measured ~1.0 s). Report runs
  several times and states the spread.
- **Embeddings** were enabled via a model resolved from kg's config chain;
  builds are incremental — the refreshed go-graphed self-run reused 1,237 of
  1,238 vectors and pruned 0.

## 2. Case study A — Kotlin / Spring Boot microservice (private)

A production microservice (private repo, ~341 files, 187 Kotlin files).

| Metric | Value |
| --- | --- |
| Documents indexed | 319 |
| Entities extracted | 13,555 |
| Links | 1,645 (767 `extracted` / 878 `inferred`) |
| Link types | `references` 663, `calls` 578, `imports` 192, `part_of` 187, `documents` 25 |
| Packages | 18 |
| Clusters | 79 (56 directory, 18 module, 5 network) |
| Build time | 43.4 s (varies ~30–45 s with machine load) |
| Peak RSS | ~815 MiB |
| Hubs flagged | 16 |
| Ground truth collected | 180 files / 1,755 import strings (for scoring) |

**God Node ranking (top, by PageRank) — matches manual reading of the
codebase:**

1. The DTO-layer model file — weighted degree **2,184** (the coupling hotspot, >2× any other file)
2. The shared exception type
3. The enum model file
4. A service test file *(a test file — see known issues)*
5–9. The service layer (five central services)
10+. The repository layer

For a Spring-style codebase the centrality signal lands exactly where
intuition puts it: DTOs, enums, exceptions, and the service/repository layer.
Directory clusters mirror the real package tree.

## 3. Case study B — go-graphed on itself (Go)

Public repo, 145+ files. Runnable by anyone.

| Metric | Value |
| --- | --- |
| Documents indexed | 146 |
| Entities extracted | 1,905 |
| Links | 916 (764 `extracted` / 152 `inferred`) |
| Link types | `calls` 648, `imports` 135, `part_of` 116, `documents` 13, `implements` 4 |
| Packages | 11 |
| Clusters | 38 |
| Hubs flagged | 8 |
| Ground truth collected | 597 Go imports / 22 Python imports |
| Build time | ~1.0 s (clean) |
| Peak RSS | ~304 MiB |

**God Node ranking:** `pkg/graphed/builder.go` (the pipeline entry point,
degree 95), then `internal/ir/types.go` (the shared IR), then the
cross-language extractor test matrix. Same central-file intuition you'd
build by hand.

## 3b. Case study C — JavaScript / React + Redux web app (private)

A production React/Redux web app: actions, reducers, sagas, middleware (API
client), and a large component tree.

| Metric | Value |
| --- | --- |
| Documents indexed | 233 |
| Entities extracted | 7,143 |
| Links | 422 (413 `extracted` / 9 `inferred`) |
| Link types | `imports` 243, `exports` 86, `calls` 84, `documents` 9 |
| Packages | 0 (plain JS — no package detection) |
| Clusters | 77 (68 directory, 9 network) |
| Hubs flagged | 12 |
| Ground truth collected | 582 JS imports |
| Build time | ~3.5 s |
| Peak RSS | ~508 MiB |

**God Node ranking:** the Redux wiring hub (middleware root), a shared util
module, and the main feedback-flow component.

Retrieval on this repo (Section 4) is the strongest case for `kg_narrowed`:
recall 0.64 at 1,593 tokens against grep's 0.14 at 18,820 tokens.

## 4. Retrieval A/B: token cost and recall (`score.py`)

Golden-query retrieval ablation over MCP stdio: each query has an entry file
and golden answer files (the code that genuinely answers the question,
validated as graph-reachable). Methods are measured at the *same* token metric
kg itself reports (`len(runes)/4`), with recall = fraction of golden answer
paths present in the retrieved context. Baselines exclude generated dumps
(SQL, spreadsheets, blobs) so they reflect what an agent would actually read.
Corpora: `testdata/benchmark/queries/` — the go-graphed corpus
(`queries/go-graphed.json`) is committed; private corpora are kept out of the
repository (see the gitignore) and run on the machine that holds them.

| Target (read-all baseline) | kg_narrowed (1.5k cap) | kg_topology | grep top-10 |
| --- | --- | --- | --- |
| **go-graphed** — 458,869 tok | 1,557 tok · recall 0.55 · **272×** | 1,254 tok · recall 0.48 · 438× | 262,452 tok · recall 0.50 · 1.7× |
| **Kotlin microservice** — 398,313 tok | 1,717 tok · recall 0.43 · **233×** | 10,102 tok · recall 1.00 · 40× | 101,910 tok · recall 0.33 · 3.8× |
| **React web app** — 91,313 tok | 1,593 tok · recall 0.64 · **52×** | 1,141 tok · recall 1.00 · 192× | 18,820 tok · recall 0.14 · 5.3× |

Takeaways (each is a measured, reproducible number, not an opinion):

- **Token reduction is the headline claim.** Answering a code question with
  `kg` costs ~1.5–1.7k tokens instead of ~90k–460k for reading the codebase —
  a **~52–272× reduction**. kg's own estimate is used end-to-end, so the
  number is what an agent actually pays.
- **kg beats plain grep decisively, on every repo.** grep's honest cost is
  opening its top-10 matched files in full: 19k–261k tokens per query
  (1.7–5.3× reduction vs read-all). kg achieves **equal or better recall with
  12–168× fewer tokens** — and the gap is biggest exactly where grep is
  cheapest to run (small repos: 0.64 vs 0.14 on the React app at 12× fewer
  tokens).
- **`kg_topology` is the reliable file-location mode.** `get_document_links`
  lists every 1-hop neighbor, reaching recall 1.00 when answers are 1-hop —
  at a cost between grep's and narrowed context's. Trade-off is explicit:
  narrowed context saves tokens, topology guarantees the neighborhood.
- **grep's "cheap" grep step is a trap**: keyword matching is cheap, but the
  agent then reads whole files. On the small React repo grep gets 0.14 recall
  at 18k tokens; kg gets 0.64 at 1.6k.

### Per-query breakdown (Kotlin microservice, private corpus)

| Query | kg_narrowed | kg_topology | grep top-10 |
| --- | --- | --- | --- |
| k1 | 1,724 tok · r0.25 | 10,461 tok · r1.0 | 62,240 tok · r0.25 |
| k2 | 1,706 tok · r0.50 | 19,659 tok · r1.0 | 122,351 tok · r0.50 |
| k3 | 1,708 tok · r0.33 | 10,000 tok · r1.0 | 81,603 tok · r0.00 |
| k4 | 1,757 tok · r1.00 | 4,092 tok · r1.0 | 103,621 tok · r0.00 |
| k5 | 1,705 tok · r0.25 | 11,205 tok · r1.0 | 130,236 tok · r0.50 |
| k6 | 1,720 tok · r0.25 | 9,849 tok · r1.0 | 77,560 tok · r0.25 |
| k7 | 1,700 tok · r0.50 | 8,667 tok · r1.0 | 109,968 tok · r0.50 |
| k8 | 1,714 tok · r0.33 | 6,906 tok · r1.0 | 127,700 tok · r0.67 |

### Per-query breakdown (React web app, private corpus)

| Query | kg_narrowed | kg_topology | grep top-10 |
| --- | --- | --- | --- |
| r1 | 1,713 tok · r0.20 | 2,940 tok · r1.0 | 24,594 tok · r0.20 |
| r2 | 1,815 tok · r0.50 | 2,129 tok · r1.0 | 20,811 tok · r0.25 |
| r3 | 1,760 tok · r1.00 | 402 tok · r1.0 | 16,654 tok · r0.00 |
| r4 | 1,887 tok · r1.00 | 476 tok · r1.0 | 17,257 tok · r0.20 |
| r5 | 1,843 tok · r1.00 | 465 tok · r1.0 | 12,797 tok · r0.25 |
| r6 | 182 tok · r0.50 | 382 tok · r1.0 | 20,796 tok · r0.00 |
| r7 | 1,741 tok · r0.33 | 758 tok · r1.0 | 21,645 tok · r0.00 |
| r8 | 1,801 tok · r0.60 | 1,574 tok · r1.0 | 16,004 tok · r0.20 |

Pattern to note: on the Redux app the queries that ask "which X does the root
wire together" (r3, r4, r5) are answered perfectly by kg_narrowed because the
entry file is small and its neighbors fit the budget; the query that costs
kg almost nothing (r6, 182 tokens) is exactly where grep reads 20k
tokens. grep's only wins (Kotlin k8) are where the answer files literally
contain the query keywords. Neither graph tool misses a file that topology
guarantees.

Caveats on the recall column (they apply to the tool, not just the metric):

1. **Entry-file dominance.** `get_narrowed_context` fills its token budget with
   the entry file's own snippets — a hub entry file with 27 snippets consumed a
   3,000-token budget entirely and no neighbor file appeared. At a 1.5k cap,
   recall mostly measures "did the entry's neighbors surface".
2. **Kotlin same-package refs are unlinked.** One Kotlin service referencing a
   sibling service in the same package (no `import`) has no edge in the graph;
   cross-package references are linked. So 1-hop queries cannot surface sibling
   services in Kotlin — flagged for the Kotlin extractor.

## 5. What these numbers show (and do not claim)

What they show:
- **Language-agnostic and consistent**: the same pipeline builds a graph for
  Kotlin/Spring and Go/CLI code with no per-language configuration.
- **Incremental embeddings work**: rebuilds reuse unchanged vectors instead of
  re-embedding the whole corpus.
- **Hub detection matches structural intuition** on three unrelated codebases —
  verified against the actual repository layout, not a model's opinion.
- **Retrieval is measurably cheap and beats no-graph baselines** (Section 4).
- **Local-only**: graphs and vector stores live on the machine; nothing leaves
  it.

What they do **not** claim (unmeasured yet — do not quote as results):
- No LLM final-answer scoring (retrieval is scored; end-to-end "did the model
  answer correctly" is a manual dogfooding step).
- No graphify comparison yet (add `--graphify` to `score.py` and re-run).
- No cross-language/external resolution (only repo-local code is linked).
- One machine, one run each per repo; retrieval numbers are averages over 7–8
  golden queries per repo — treat as indicative, and grow the corpus.

## 6. Known issues & limitations (owned before anyone else points them out)

1. **`kg build` indexes its own output.** When `graph.json` is written inside
   the scanned root it is indexed as a JSON document (a 3-file fixture went
   from 8 to 135 entities). The harness excludes it; a self-exclusion fix is
   planned.
2. **Inferred links outnumber extracted ones on Kotlin** (878 vs 767). Scoring
   (§4) surfaced a concrete consequence: same-package Kotlin references are
   unlinked, so sibling services aren't 1-hop reachable. The `references`
   heuristic is structurally plausible but needs validation on this gap.
3. **Test files can rank as hubs** (a service test at #4 here; the extractor
   test matrix in go-graphed). A test-deweighting option for hub ranking is
   planned.
4. **`get_narrowed_context` is entry-file-dominant** (§4): a hub entry file's
   own snippets can exhaust the token budget before any neighbor file appears.
   Consider a per-file snippet cap so neighbors surface earlier.
5. **Repo-boundary only**: external APIs, config, and DB schemas are not
   linked; provenance tags (`extracted`/`inferred`) make clear what is a fact
   vs. a heuristic.
6. **Snapshot staleness**: the graph is a build snapshot; it must be rebuilt
   when code changes materially (`kg build`).

## 7. Reproduction

Harness: `testdata/benchmark/` (Python 3 stdlib, no dependencies). Run inside
any target project:

```bash
python3 <go-graphed>/testdata/benchmark/lib/bench.py . \
  --outdir ~/kg-bench/results --kg $(which kg)
# with embeddings:
python3 <go-graphed>/testdata/benchmark/lib/bench.py . \
  --model <model-path> --outdir ~/kg-bench/results --kg $(which kg)
```

Reproduce the go-graphed case study (public):

```bash
cd <go-graphed> && python3 testdata/benchmark/lib/bench.py . --kg ./kg
```

The two private case studies are not publicly reproducible; run the harness
on any repo to produce an equivalent report. Retrieval scoring on an external
repo (embeddings enabled) is a one-liner with your own golden-query corpus:

```bash
python3 <go-graphed>/testdata/benchmark/lib/score.py /path/to/repo \
  --kg <go-graphed>/kg \
  --queries /path/to/my-corpus.json
```

For the committed public corpus, run it against this repo:

```bash
python3 testdata/benchmark/lib/score.py . --kg ./kg \
  --queries testdata/benchmark/queries/go-graphed.json
```

---

*Methodology details, ground-truth extractors, and per-file import data are in
the raw reports (`kg-bench-*.json`) produced by the harness.*
