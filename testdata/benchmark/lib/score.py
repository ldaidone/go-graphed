#!/usr/bin/env python3
"""kg benchmark scorer — A/B retrieval, token reduction, and tool comparison.

Runs a golden query corpus against the running kg binary and measures, per
query, the same three things people actually argue about:

  1. Token reduction   — tokens returned by `get_narrowed_context` vs. the
                         tokens of reading the raw source (baseline_raw) or
                         grep-matched files (baseline_grep).
  2. A/B retrieval     — does the tool surface the golden answer file(s)?,
                         scored as recall@1 per query for kg vs. grep.
  3. Tool comparison   — optional: the same queries answered by `graphify
                         query` (when installed), same baseline, same metric.

Token counting uses kg's own heuristic (len(runes)/4, see estimateTokens in
internal/mcp/handlers.go) applied identically to every method, so the ratios
are internally consistent and reproducible. The baseline is deliberately
favorable to grep (files matching ALL query keywords).

Requires a kg build WITH embeddings for the target repo (get_narrowed_context
needs the vector store). Talk to the shipped binary over stdio JSON-RPC (MCP)
— the benchmark tests the real tool, not its internals.

Usage:
    python3 score.py [ROOT] [--queries queries.json] [--graphify]

    ROOT        target project directory (default: cwd) — must contain graph.json
    --kg PATH   kg binary (default: kg on PATH)
    --queries   golden query corpus JSON (default: queries/<name>.json)
    --outdir    directory for the score report
    --output    explicit score report path
    --graphify  also compare against `graphify query` if installed
    --max-tokens N   get_narrowed_context token budget (default: 1500)
"""

import argparse
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import time
import datetime

SCHEMA_VERSION = 1

STOPWORDS = {
    "the", "and", "for", "are", "how", "what", "which", "when", "where", "who",
    "that", "this", "with", "from", "into", "onto", "than", "then", "them",
    "does", "dont", "cant", "have", "has", "was", "were", "been", "being",
    "also", "not", "but", "all", "any", "can", "you", "its", "it's", "our",
    "your", "their", "will", "would", "could", "should", "used", "use", "get",
}


def tokens(text):
    """Match kg's estimateTokens: len(runes)/4."""
    return len(text) // 4


# Source + doc formats the "no tool" baselines consider. Generated dumps
# (html/xml/json/spreadsheet/unstructured dumps of the repo itself) are
# excluded so the baseline reflects reading what an agent would actually read.
# SQL is excluded too: repos commonly ship giant .sql DB dumps that no agent
# would read in full, but whose size would dominate the baseline.
CODE_DOC_FORMATS = {
    "golang", "python", "javascript", "typescript", "java", "kotlin", "rust",
    "csharp", "ruby", "php", "c", "cpp", "swift", "elixir", "bash", "make",
    "dockerfile", "markdown", "toml", "yaml",
}


def source_docs(graph):
    out = []
    for p, d in graph.get("Documents", {}).items():
        if d.get("Format", "other") not in CODE_DOC_FORMATS:
            continue
        base = os.path.basename(p)
        if base in ("graph.json", "graph.html") or base.startswith("kg-bench"):
            continue
        out.append(p)
    return out


# ---------------------------------------------------------------------------
# MCP client (stdio JSON-RPC, newline-delimited)
# ---------------------------------------------------------------------------

class MCPClient:
    def __init__(self, cmd, cwd, timeout=120):
        self.proc = subprocess.Popen(
            cmd, cwd=cwd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, text=True, bufsize=1,
        )
        self.timeout = timeout
        self._id = 0
        # initialize handshake
        self._request("initialize", {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "kg-bench", "version": "0.1"},
        })

    def _read_line(self, deadline):
        line = self.proc.stdout.readline()
        if not line:
            raise RuntimeError("MCP server closed stdout")
        return line

    def _request(self, method, params, notify=False):
        self._id += 1
        msg = {"jsonrpc": "2.0", "id": self._id, "method": method, "params": params}
        if notify:
            del msg["id"]
        self.proc.stdin.write(json.dumps(msg) + "\n")
        self.proc.stdin.flush()
        if notify:
            return None
        deadline = time.monotonic() + self.timeout
        while time.monotonic() < deadline:
            line = self._read_line(deadline)
            try:
                resp = json.loads(line)
            except json.JSONDecodeError:
                continue
            if resp.get("id") == self._id:
                if "error" in resp:
                    raise RuntimeError("MCP error: %s" % resp["error"])
                return resp.get("result")
        raise RuntimeError("timed out waiting for MCP response")

    def tool(self, name, arguments):
        res = self._request("tools/call", {"name": name, "arguments": arguments})
        text = ""
        try:
            for content in (res or {}).get("content", []):
                text += content.get("text", "")
        except Exception:
            pass
        return text

    def close(self):
        try:
            self.proc.terminate()
            self.proc.wait(timeout=5)
        except Exception:
            self.proc.kill()


# ---------------------------------------------------------------------------
# Baselines
# ---------------------------------------------------------------------------

def load_graph(root):
    path = os.path.join(root, "graph.json")
    with open(path, "r", encoding="utf-8") as fh:
        return json.load(fh)


def file_text(root, rel):
    try:
        with open(os.path.join(root, rel), "r", encoding="utf-8", errors="replace") as fh:
            return fh.read()
    except OSError:
        return ""


def baseline_raw(root, graph):
    """Tokens of reading every source/doc file ('no tool' baseline)."""
    total = 0
    for path in source_docs(graph):
        total += tokens(file_text(root, path))
    return total


def query_keywords(query, explicit=None):
    if explicit:
        return explicit
    words = re.findall(r"[A-Za-z_][A-Za-z0-9_]{2,}", query)
    return [w for w in words if w.lower() not in STOPWORDS and w.lower() != "kg"]


def baseline_grep(root, graph, keywords, top_k=10):
    """Grep baseline: rank source/docs by keyword-hit count, read top-K whole.

    This is the 'agent without a graph' workflow: grep, then open the most
    promising files in full. Returns (top_files, tokens_of_top_files,
    total_matched_files).
    """
    if not keywords:
        return [], 0, 0
    scored = []
    for path in source_docs(graph):
        text = file_text(root, path)
        hits = sum(text.count(k) for k in keywords)
        if hits:
            scored.append((hits, tokens(text), path))
    scored.sort(key=lambda x: (-x[0], -x[1]))
    top = scored[:top_k]
    matches = [p for _, _, p in top]
    total = sum(tok for _, tok, _ in top)
    return matches, total, len(scored)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def recall(needle_files, haystack):
    if not needle_files:
        return None
    hit = sum(1 for f in needle_files if f in haystack)
    return hit / len(needle_files)


def run_queries(root, graph, kg_bin, queries, max_tokens):
    client = MCPClient([kg_bin, "mcp", "--file", "graph.json"], cwd=root)
    results = []
    try:
        for q in queries:
            row = {"id": q.get("id", "?"), "query": q.get("query", ""),
                   "entry": q.get("entry", ""), "answers": q.get("answers", [])}
            keywords = query_keywords(q.get("query", ""), q.get("keywords"))

            # method 1: kg get_narrowed_context
            try:
                text = client.tool("get_narrowed_context", {
                    "entryPath": q.get("entry", ""),
                    "searchQuery": q.get("query", ""),
                    "maxTokens": max_tokens,
                })
                row["kg_narrowed"] = {
                    "tokens": tokens(text),
                    "recall": recall(row["answers"], text),
                    "text": text,
                }
            except Exception as e:
                row["kg_narrowed"] = {"error": str(e)}

            # method 2: kg get_document_details + get_document_links (topology only)
            try:
                text = client.tool("get_document_details", {"path": q.get("entry", "")})
                text += client.tool("get_document_links", {"path": q.get("entry", "")})
                row["kg_topology"] = {
                    "tokens": tokens(text),
                    "recall": recall(row["answers"], text),
                }
            except Exception as e:
                row["kg_topology"] = {"error": str(e)}

            # method 3: grep baseline (top-K by keyword hits)
            matches, gtoks, gm = baseline_grep(root, graph, keywords)
            row["grep"] = {
                "files": matches,
                "tokens": gtoks,
                "recall": recall(row["answers"], " ".join(matches)),
                "matched_total": gm,
            }

            results.append(row)
    finally:
        client.close()
    return results


def run_graphify(root, queries):
    """Best-effort: same queries through `graphify query`, same token metric."""
    results = []
    for q in queries:
        row = {"id": q.get("id", "?"), "query": q.get("query", "")}
        try:
            out = subprocess.run(
                ["graphify", "query", q.get("query", "")],
                cwd=root, capture_output=True, text=True, timeout=180,
            )
            text = out.stdout + out.stderr
            row["tokens"] = tokens(text)
            row["recall"] = recall(q.get("answers", []), text)
            row["exit"] = out.returncode
        except FileNotFoundError:
            return None
        except subprocess.TimeoutExpired:
            row["tokens"] = None
            row["error"] = "timeout"
        except Exception as e:
            row["tokens"] = None
            row["error"] = str(e)
        results.append(row)
    return results


def aggregate(results, method, raw_tokens):
    rows = [r for r in results if method in r and "error" not in r[method]]
    if not rows:
        return None
    toks = [r[method]["tokens"] for r in rows]
    recs = [r[method].get("recall") for r in rows]
    recs = [r for r in recs if r is not None]
    mean_tok = sum(toks) / len(toks)
    reductions = [raw_tokens / t for t in toks if t > 0]
    return {
        "queries_scored": len(rows),
        "mean_tokens": round(mean_tok, 1),
        "median_tokens": float(sorted(toks)[len(toks) // 2]),
        "mean_recall": round(sum(recs) / len(recs), 3) if recs else None,
        "median_reduction_vs_raw": round(float(sorted(reductions)[len(reductions) // 2]), 1)
        if reductions else None,
        "min_reduction_vs_raw": round(min(reductions), 1) if reductions else None,
    }


def main(argv=None):
    ap = argparse.ArgumentParser(description="kg benchmark scorer")
    ap.add_argument("root", nargs="?", default=".", help="target project directory")
    ap.add_argument("--kg", default="kg", help="kg binary (default: kg on PATH)")
    ap.add_argument("--queries", default=None, help="golden query corpus JSON")
    ap.add_argument("--max-tokens", type=int, default=1500)
    ap.add_argument("--outdir", default=None, help="directory for the score report")
    ap.add_argument("--output", default=None, help="explicit score report path")
    ap.add_argument("--graphify", action="store_true", help="compare against graphify query")
    args = ap.parse_args(argv)

    root = os.path.abspath(args.root)
    if not os.path.isfile(os.path.join(root, "graph.json")):
        ap.error("no graph.json in %s — run bench.py (with --model) first" % root)

    name = os.path.basename(root.rstrip(os.sep)) or root
    queries_path = args.queries or os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "..", "queries", name + ".json")
    with open(queries_path, "r", encoding="utf-8") as fh:
        corpus = json.load(fh)
    queries = corpus["queries"]

    graph = load_graph(root)
    raw_tokens = baseline_raw(root, graph)

    results = run_queries(root, graph, args.kg, queries, args.max_tokens)

    report = {
        "schema_version": SCHEMA_VERSION,
        "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "target": {"name": name, "root": root},
        "token_count_method": "len(runes)/4 (kg estimateTokens, applied to every method)",
        "baseline": {"method": "read every indexed source file",
                     "tokens": raw_tokens},
        "queries": [],
    }

    # per-query summary rows (omit the raw markdown blobs from the report)
    for r in results:
        summary = {"id": r["id"], "query": r["query"], "entry": r["entry"]}
        for m in ("kg_narrowed", "kg_topology", "grep"):
            if m in r:
                summary[m] = {k: v for k, v in r[m].items() if k != "text"}
        report["queries"].append(summary)

    report["aggregate"] = {
        "kg_narrowed": aggregate(results, "kg_narrowed", raw_tokens),
        "kg_topology": aggregate(results, "kg_topology", raw_tokens),
        "grep": aggregate(results, "grep", raw_tokens),
    }

    if args.graphify:
        gf = run_graphify(root, queries)
        if gf is None:
            report["graphify"] = {"skipped": "graphify binary not installed"}
        else:
            report["graphify"] = {"per_query": gf}
            gtoks = [g["tokens"] for g in gf if g.get("tokens")]
            if gtoks:
                report["graphify"]["aggregate"] = {
                    "mean_tokens": round(sum(gtoks) / len(gtoks), 1),
                    "median_reduction_vs_raw": round(float(sorted(raw_tokens // t for t in gtoks if t)[len(gtoks) // 2]), 1),
                }

    out_path = args.output or os.path.join(
        args.outdir or os.path.join(root, "benchy"), "kg-bench-score-%s.json" % name)
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as fh:
        json.dump(report, fh, indent=2)

    # human table
    def fmt(a):
        if not a:
            return "n/a"
        return "tok=%s recall=%s red=%s×" % (
            a.get("mean_tokens"), a.get("mean_recall"), a.get("median_reduction_vs_raw"))

    print("target      : %s" % name)
    print("baseline    : read-all = %d tokens" % raw_tokens)
    print("kg_narrowed : %s" % fmt(report["aggregate"]["kg_narrowed"]))
    print("kg_topology : %s" % fmt(report["aggregate"]["kg_topology"]))
    print("grep        : %s" % fmt(report["aggregate"]["grep"]))
    if "graphify" in report:
        g = report["graphify"].get("aggregate")
        print("graphify    : %s" % (fmt(g) if g else report["graphify"]))
    print("report      : %s" % out_path)


if __name__ == "__main__":
    main()
