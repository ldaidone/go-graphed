#!/usr/bin/env python3
"""kg benchmark collector.

Runs inside a target project (any language), builds the kg graph for it, and
emits a normalized JSON report containing:

  * target metadata (git rev, file counts per format)
  * build timing / peak RSS (via the RUSAGE_CHILDREN trick, no /usr/bin/time)
  * graph stats read back from graph.json (docs, entities, links split by
    provenance, hubs, packages, clusters per kind)
  * the raw `kg metrics` / `kg clusters` terminal output for eyeballing
  * best-effort ground-truth import lists per file per language, used later
    by the scoring harness to compute precision/recall of kg's import
    resolution

This script is intentionally dependency-free (stdlib only) and best-effort:
any step that fails is recorded in the report instead of aborting it.

Usage:
    python3 bench.py [ROOT] [options]

    ROOT   target project directory (default: current directory)

Options:
    --kg PATH              kg binary (default: 'kg' from PATH)
    --model PATH           embedding model; when set, kg build embeds
    --build-flags STR      extra flags passed to `kg build` (shell-split)
    --skip-build           reuse an existing graph.json instead of building
    --no-ground-truth      skip per-file import extraction
    --output PATH          report file (default: <outdir>/kg-bench-<name>.json)
    --outdir DIR           directory for the report file (default: target root)
"""

import argparse
import ast
import json
import os
import re
import resource
import shlex
import shutil
import subprocess
import sys
import time
import datetime

SCHEMA_VERSION = 1

SKIP_DIRS = {
    ".git", ".hg", ".svn", ".github", ".gitlab", ".idea", ".vscode", ".vscode-server",
    "node_modules", "vendor", "third_party", "dist", "build", "bin", "obj", "out",
    "coverage", ".venv", "venv", "env", "__pycache__", ".gradle", ".mvn", ".terraform",
    "Pods", "DerivedData", ".next", ".nuxt", ".cache", "target", "generated",
}

SKIP_FILES = {
    "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "Gopkg.lock",
    "Cargo.lock", "poetry.lock", "Pipfile.lock", "Gemfile.lock", "composer.lock",
    "Podfile.lock", "npm-shrinkwrap.json",
}

MAX_SOURCE_BYTES = 2 * 1024 * 1024  # skip oversized "source" files

# kg outputs / benchmark artifacts that must never be indexed as source even
# when they land inside the scanned root (a build indexes its own graph.json).
DEFAULT_EXCLUDES = ["graph.json", "kg-bench*"]

EXT_FORMAT = {
    ".go": "golang", ".py": "python", ".js": "javascript", ".jsx": "javascript",
    ".mjs": "javascript", ".cjs": "javascript", ".ts": "typescript", ".tsx": "typescript",
    ".java": "java", ".kt": "kotlin", ".kts": "kotlin", ".rs": "rust", ".cs": "csharp",
    ".rb": "ruby", ".php": "php", ".c": "c", ".h": "c", ".cpp": "cpp", ".cc": "cpp",
    ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp", ".swift": "swift", ".ex": "elixir",
    ".exs": "elixir", ".md": "markdown", ".markdown": "markdown", ".json": "json",
    ".yaml": "yaml", ".yml": "yaml", ".toml": "toml", ".sql": "sql", ".sh": "bash",
    ".bash": "bash", ".zsh": "bash", ".xml": "xml", ".html": "html", ".css": "css",
    ".scss": "scss", ".sass": "sass", ".vue": "vue", ".svelte": "svelte", ".proto": "proto",
}

# formats whose source we know how to lex for import ground truth
EXTRACTOR_FORMATS = (
    "golang", "python", "javascript", "typescript", "java", "kotlin",
    "rust", "csharp", "ruby", "php", "c", "cpp", "swift",
)


def walk_files(root):
    """Yield (relpath, basename, ext) for indexable files under root."""
    root = os.path.abspath(root)
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for name in filenames:
            if name in SKIP_FILES:
                continue
            full = os.path.join(dirpath, name)
            try:
                if os.path.getsize(full) > MAX_SOURCE_BYTES:
                    continue
            except OSError:
                continue
            rel = os.path.relpath(full, root)
            _, ext = os.path.splitext(name)
            yield rel, name, ext.lower()


def detect(root):
    """Count files/bytes per detected format."""
    counts = {}
    total_files = 0
    total_bytes = 0
    for rel, name, ext in walk_files(root):
        fmt = EXT_FORMAT.get(ext)
        if fmt is None:
            if name == "Dockerfile":
                fmt = "dockerfile"
            elif name in ("Makefile", "makefile", "GNUmakefile") or name == "CMakeLists.txt":
                fmt = "make"
        counts.setdefault(fmt or "other", {"files": 0, "bytes": 0})
        counts[fmt or "other"]["files"] += 1
        try:
            counts[fmt or "other"]["bytes"] += os.path.getsize(os.path.join(root, rel))
        except OSError:
            pass
        total_files += 1
        try:
            total_bytes += os.path.getsize(os.path.join(root, rel))
        except OSError:
            pass
    languages = [
        {"format": fmt, **c}
        for fmt, c in sorted(counts.items(), key=lambda kv: -kv[1]["files"])
    ]
    return languages, total_files, total_bytes


def git_info(root):
    info = {}
    for flag, args in (("rev", ["rev-parse", "HEAD"]), ("branch", ["rev-parse", "--abbrev-ref", "HEAD"])):
        try:
            out = subprocess.run(
                ["git", "-C", root] + args,
                capture_output=True, text=True, timeout=15,
            )
            info[flag] = out.stdout.strip() if out.returncode == 0 else None
        except Exception:
            info[flag] = None
    if not any(info.values()):
        return None
    return info


# ---------------------------------------------------------------------------
# Ground-truth import extraction (best-effort lexical, stdlib only)
# ---------------------------------------------------------------------------

def _strip_line_comments(text):
    return re.sub(r"//[^\n]*", "", text)


def imports_golang(text):
    text = _strip_line_comments(text)
    out = []
    for block in re.findall(r"\bimport\s*\(([^)]*)\)", text, re.S):
        out.extend(re.findall(r'"([^"]+)"', block))
    singles = re.sub(r"\bimport\s*\([^)]*\)", "", text, flags=re.S)
    out.extend(re.findall(r'\bimport\s+(?:_\s+|\.\s+|\w+\s+)?("(?:[^"]+)")', singles))
    out.extend(x[1] for x in re.findall(r"\bimport\s+(?:_\s+|\.\s+|\w+\s+)?(['\"])([^'\"]+)\1", singles))
    return [_clean(x) for x in out if _clean(x)]


def imports_python(text):
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return []
    out = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            out.extend(a.name for a in node.names)
        elif isinstance(node, ast.ImportFrom):
            out.append("." * node.level + (node.module or ""))
    return out


def imports_java_kotlin(text):
    return re.findall(r"^\s*import\s+(?:static\s+)?([A-Za-z0-9_$.*]+)", text, re.M)


def imports_js_ts(text):
    out = []
    out += re.findall(r"\bimport\s+[\"']([^\"']+)[\"']", text)
    out += re.findall(r'\bimport\s+[^"\';\n]*?\bfrom\s+["\']([^"\']+)["\']', text, re.S)
    out += re.findall(r'\bexport\s+[^"\';\n]*?\bfrom\s+["\']([^"\']+)["\']', text, re.S)
    out += re.findall(r"\brequire\s*\(\s*[\"']([^\"']+)[\"']", text)
    return out


def imports_rust(text):
    out = []
    for m in re.findall(r"^\s*use\s+([A-Za-z0-9_]+(?:::[A-Za-z0-9_]+)*)", text, re.M):
        out.append(m)
    return out


def imports_csharp(text):
    return re.findall(r"^\s*using\s+(?!static\b)(?![\w.]+\s*=)([A-Za-z0-9_.]+)", text, re.M)


def imports_ruby(text):
    return re.findall(r"\brequire(?:_relative)?\s*[\"']([^\"']+)[\"']", text)


def imports_php(text):
    return re.findall(r"^\s*use\s+(?:function\s+|const\s+)?([A-Za-z0-9_\\]+)", text, re.M)


def imports_c(text):
    return re.findall(r"^\s*#\s*include\s*[<\"]([^>\"]+)[>\"]", text, re.M)


def imports_cpp(text):
    return re.findall(r"^\s*#\s*include\s*[<\"]([^>\"]+)[>\"]", text, re.M)


def imports_swift(text):
    return re.findall(r"^\s*@testable\s+import\s+([A-Za-z0-9_.]+)", text, re.M) + \
           re.findall(r"^\s*import\s+([A-Za-z0-9_.]+)", text, re.M)


EXTRACTORS = {
    "golang": imports_golang,
    "python": imports_python,
    "javascript": imports_js_ts,
    "typescript": imports_js_ts,
    "java": imports_java_kotlin,
    "kotlin": imports_java_kotlin,
    "rust": imports_rust,
    "csharp": imports_csharp,
    "ruby": imports_ruby,
    "php": imports_php,
    "c": imports_c,
    "cpp": imports_cpp,
    "swift": imports_swift,
}

EXT_EXTRACTOR = {
    ".go": "golang", ".py": "python",
    ".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
    ".ts": "typescript", ".tsx": "typescript",
    ".java": "java", ".kt": "kotlin", ".kts": "kotlin",
    ".rs": "rust", ".cs": "csharp", ".rb": "ruby", ".php": "php",
    ".c": "c", ".h": "c", ".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp",
    ".hpp": "cpp", ".hh": "cpp", ".swift": "swift",
}


def _clean(s):
    return s.strip()


def extract_ground_truth(root, formats):
    """Return a list of {language, method, files_imports:[{file, imports}]}."""
    results = []
    wanted = {f for f in formats if f in EXTRACTORS}
    for fmt in sorted(wanted):
        entries = []
        total_imports = 0
        for rel, name, ext in walk_files(root):
            if ext not in EXT_EXTRACTOR or EXT_EXTRACTOR[ext] != fmt:
                continue
            path = os.path.join(root, rel)
            try:
                with open(path, "r", encoding="utf-8", errors="replace") as fh:
                    text = fh.read()
            except OSError:
                continue
            imports = [i for i in EXTRACTORS[fmt](text) if i]
            if not imports:
                continue
            entries.append({"file": rel, "imports": imports})
            total_imports += len(imports)
        results.append({
            "language": fmt,
            "method": "ast" if fmt == "python" else "lexical",
            "files": len(entries),
            "imports_total": total_imports,
            "files_imports": entries,
        })
    return results


# ---------------------------------------------------------------------------
# Graph stats read back from graph.json
# ---------------------------------------------------------------------------

def graph_stats(graph_path):
    with open(graph_path, "r", encoding="utf-8") as fh:
        g = json.load(fh)

    documents = g.get("Documents") or {}
    links = g.get("Links") or []
    packages = g.get("Packages") or {}
    clusters = g.get("Clusters") or []
    metrics = g.get("Metrics") or {}

    formats = {}
    entities = 0
    doc_bytes = 0
    for path, doc in documents.items():
        fmt = doc.get("Format", "other")
        formats[fmt] = formats.get(fmt, 0) + 1
        entities += len(doc.get("Entities") or [])
        doc_bytes += int(doc.get("Size") or 0)

    link_types = {}
    extracted = inferred = 0
    for link in links:
        st = link.get("SourceType")
        if st == "extracted":
            extracted += 1
        elif st == "inferred":
            inferred += 1
        t = link.get("Type", "unknown")
        link_types[t] = link_types.get(t, 0) + 1

    hub_docs = metrics.get("Documents") or {}
    hubs = []
    for path, m in sorted(hub_docs.items(), key=lambda kv: -float(kv[1].get("PageRank", 0))):
        if m.get("IsHub"):
            hubs.append({
                "path": path,
                "degree": m.get("Degree", 0),
                "weighted_degree": m.get("WeightedDegree", 0),
                "page_rank": round(float(m.get("PageRank", 0)), 6),
            })
            if len(hubs) >= 20:
                break

    cluster_kinds = {}
    for c in clusters:
        k = c.get("Kind", "unknown")
        cluster_kinds[k] = cluster_kinds.get(k, 0) + 1

    return {
        "documents": len(documents),
        "entities": entities,
        "doc_bytes": doc_bytes,
        "formats": {k: formats[k] for k in sorted(formats)},
        "links_total": len(links),
        "links_extracted": extracted,
        "links_inferred": inferred,
        "link_types": {k: link_types[k] for k in sorted(link_types)},
        "hubs": hubs,
        "hub_count": int(metrics.get("HubCount") or len(hubs)),
        "packages": len(packages),
        "clusters_total": len(clusters),
        "cluster_kinds": {k: cluster_kinds[k] for k in sorted(cluster_kinds)},
    }


def run_kg_tool(kg, args, graph_path, cwd):
    try:
        out = subprocess.run(
            [kg] + args + ["--file", graph_path],
            capture_output=True, text=True, timeout=120, cwd=cwd,
        )
        return out.stdout.strip() if out.returncode == 0 else None
    except Exception:
        return None


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def build(kg, root, flags, model):
    cmd = [kg, "build", "."] + flags + [
        x for pattern in DEFAULT_EXCLUDES for x in ("--exclude", pattern)
    ]
    if model:
        cmd += ["--model-path", model]
    start = time.monotonic()
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=1800, cwd=root)
        exit_code = proc.returncode
        log = (proc.stdout + proc.stderr).strip()
    except subprocess.TimeoutExpired:
        exit_code = -1
        log = "kg build timed out after 30 minutes"
    except FileNotFoundError:
        exit_code = -2
        log = "kg binary not found"
    duration = round(time.monotonic() - start, 3)

    peak_rss = None
    try:
        ru = resource.getrusage(resource.RUSAGE_CHILDREN)
        if ru.ru_maxrss:
            # macOS reports bytes, Linux reports KiB
            peak_rss = ru.ru_maxrss if sys.platform == "darwin" else ru.ru_maxrss * 1024
    except Exception:
        pass

    return {
        "exit_code": exit_code,
        "duration_seconds": duration,
        "max_rss_bytes": peak_rss,
        "embedded": bool(model),
        "build_flags": " ".join(flags) + " " + " ".join("--exclude %s" % p for p in DEFAULT_EXCLUDES),
        "log_tail": "\n".join(log.splitlines()[-30:]),
    }


def main(argv=None):
    ap = argparse.ArgumentParser(description="kg benchmark collector")
    ap.add_argument("root", nargs="?", default=".", help="target project directory")
    ap.add_argument("--kg", default="kg", help="kg binary (default: kg on PATH)")
    ap.add_argument("--model", default=None, help="embedding model path")
    ap.add_argument("--build-flags", default="", help="extra `kg build` flags, shell-split")
    ap.add_argument("--skip-build", action="store_true", help="reuse existing graph.json")
    ap.add_argument("--no-ground-truth", action="store_true", help="skip import extraction")
    ap.add_argument("--output", default=None, help="report file path")
    ap.add_argument("--outdir", default=None, help="directory for the report file")
    ap.add_argument("--json-indent", type=int, default=2, help="JSON indent for the report")
    args = ap.parse_args(argv)

    root = os.path.abspath(args.root)
    if not os.path.isdir(root):
        ap.error("target root not found: %s" % root)

    # Resolve kg/model to absolute paths now: the build runs with cwd=root,
    # so a relative --kg/--model must not be interpreted against the target.
    def absolute(path):
        if not path:
            return None
        if os.path.isabs(path):
            return path
        if os.sep in path or (os.altsep and os.altsep in path):
            return os.path.abspath(path)
        found = shutil.which(path)
        return found if found else os.path.abspath(path)

    kg_bin = absolute(args.kg)
    model = absolute(args.model)
    if model and not os.path.isfile(model):
        ap.error("embedding model not found: %s" % args.model)
    if not shutil.which(kg_bin) and not os.path.isfile(kg_bin):
        ap.error("kg binary not found: %s" % args.kg)

    name = os.path.basename(root.rstrip(os.sep)) or root
    graph_path = os.path.join(root, "graph.json")
    if args.skip_build and not os.path.isfile(graph_path):
        ap.error("--skip-build but no graph.json at %s" % graph_path)

    report = {
        "schema_version": SCHEMA_VERSION,
        "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "kg": {
            "bin": kg_bin,
            "flags": "build ."
                     + ((" " + args.build_flags) if args.build_flags else "")
                     + " ".join(" --exclude %s" % p for p in DEFAULT_EXCLUDES),
        },
        "target": {},
        "build": None,
        "graph": None,
        "metrics_text": None,
        "clusters_text": None,
        "ground_truth": [],
    }

    languages, total_files, total_bytes = detect(root)
    report["target"] = {
        "root": root,
        "name": name,
        "git": git_info(root),
        "total_files": total_files,
        "total_bytes": total_bytes,
        "languages": languages,
    }

    flags = shlex.split(args.build_flags)
    if args.skip_build:
        report["build"] = {"exit_code": 0, "duration_seconds": None, "max_rss_bytes": None,
                           "embedded": bool(model),
                           "build_flags": " ".join(flags) + " " + " ".join("--exclude %s" % p for p in DEFAULT_EXCLUDES),
                           "log_tail": "skipped (--skip-build)"}
    else:
        report["build"] = build(kg_bin, root, flags, model)

    if report["build"]["exit_code"] == 0:
        report["graph"] = graph_stats(graph_path)
        report["metrics_text"] = run_kg_tool(kg_bin, ["metrics"], graph_path, root)
        report["clusters_text"] = run_kg_tool(kg_bin, ["clusters"], graph_path, root)
    else:
        report["graph"] = None
        report["build_error"] = report["build"]["log_tail"]

    if not args.no_ground_truth:
        formats = [l["format"] for l in languages]
        report["ground_truth"] = extract_ground_truth(root, formats)

    if args.output:
        out_path = args.output
    elif args.outdir:
        os.makedirs(args.outdir, exist_ok=True)
        out_path = os.path.join(args.outdir, "kg-bench-%s.json" % name)
    else:
        out_path = os.path.join(root, "kg-benchmark-report.json")

    with open(out_path, "w", encoding="utf-8") as fh:
        json.dump(report, fh, indent=args.json_indent)

    # human summary on stdout
    b = report["build"] or {}
    g = report["graph"] or {}
    langs = ", ".join("%s:%d" % (l["format"], l["files"]) for l in languages[:6])
    print("target : %s (%s)" % (name, langs))
    print("build  : %ss exit=%s rss=%s" % (
        b.get("duration_seconds", "?"), b.get("exit_code", "?"),
        ("%.1fMB" % (b.get("max_rss_bytes", 0) / 1e6)) if b.get("max_rss_bytes") else "n/a"))
    print("graph  : docs=%s entities=%s links=%s (extracted=%s inferred=%s) hubs=%s clusters=%s" % (
        g.get("documents", "?"), g.get("entities", "?"), g.get("links_total", "?"),
        g.get("links_extracted", "?"), g.get("links_inferred", "?"),
        g.get("hub_count", "?"), g.get("clusters_total", "?")))
    if report["ground_truth"]:
        gt = ", ".join("%s:%d" % (e["language"], e["imports_total"]) for e in report["ground_truth"])
        print("ground : %s" % gt)
    print("report : %s" % out_path)

    sys.exit(0 if b.get("exit_code") == 0 else 1)


if __name__ == "__main__":
    main()
