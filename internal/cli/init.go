package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/pkg/graphed"
	"github.com/spf13/cobra"
)

var (
	initGraphFile string
	initAll       bool
	initTargets   []string
	initBuild     bool
	initNoMCP     bool
	initMCPCmd    string
)

// Markers that wrap the kg rules inside a rule file. They make kg init
// idempotent: a second run finds the block between the markers and replaces
// it in place instead of appending a duplicate.
const (
	rulesStart = "<!-- kg-rules-start -->"
	rulesEnd   = "<!-- kg-rules-end -->"
)

// ruleTarget describes one agent/assistant/IDE ecosystem we can emit rules
// for. Each target writes one or more files; the content is the shared
// guidance body, wrapped for targets that need special formatting (e.g.
// Cursor-style .mdc frontmatter). mcp lists the per-project config files that
// actually wire the kg MCP server into that client.
type ruleTarget struct {
	name   string
	paths  []string
	format string // "" = plain markdown, "mdc" = Cursor/Windsurf rule
	mcp    []mcpConfig
}

// ruleTargets registers every supported ecosystem. Names are the accepted
// --targets values.
var ruleTargets = []ruleTarget{
	{name: "agents", paths: []string{"AGENTS.md"}},
	{name: "opencode", paths: []string{"AGENTS.md"}, mcp: []mcpConfig{{paths: []string{"opencode.json", "opencode.jsonc"}, kind: "opencode"}}},
	{name: "claude", paths: []string{"CLAUDE.md"}, mcp: []mcpConfig{{paths: []string{".mcp.json"}, kind: "json"}}},
	{name: "gemini", paths: []string{"GEMINI.md", "CODEASSIST.md"}, mcp: []mcpConfig{{paths: []string{".gemini/settings.json"}, kind: "json", cwd: true}}},
	{name: "copilot", paths: []string{".github/copilot-instructions.md"}, mcp: []mcpConfig{{paths: []string{".github/mcp.json"}, kind: "copilot"}}},
	// Codex reads AGENTS.md, so it maps onto the same file as "agents".
	{name: "codex", paths: []string{"AGENTS.md"}, mcp: []mcpConfig{{paths: []string{".codex/config.toml"}, kind: "codex", cwd: true}}},
	{name: "cursor", paths: []string{".cursor/rules/kg.mdc"}, format: "mdc", mcp: []mcpConfig{{paths: []string{".cursor/mcp.json"}, kind: "json"}}},
	{name: "cline", paths: []string{".clinerules/kg.md"}, mcp: []mcpConfig{{paths: []string{".cline/mcp.json"}, kind: "json"}}},
	// Windsurf only reads MCP servers from a user-global file, so there is no
	// project config to write; the generated rule documents the one-time step.
	{name: "windsurf", paths: []string{".windsurf/rules/kg.mdc"}, format: "mdc"},
}

// initCmd generates per-project agent/assistant/IDE rules that wire the
// graphed MCP server (and especially get_narrowed_context) into the agent
// loop, so tools query the local graph for a narrowed context before calling
// cloud models. For every target that supports a project-scoped config file
// it also writes/merges the MCP server entry ("kg" -> kg mcp) so the client
// actually launches the server, not just reads about it in the rules.
//
// Targets are detected from the project (which rule files already exist), and
// AGENTS.md is always written as the default rule. Existing rule files are
// never overwritten: the kg block is appended, or replaced in place on
// re-runs. Existing MCP config files are merged additively: only the "kg"
// entry is touched, other keys are preserved, and files that cannot be parsed
// are left untouched with a warning.
var initCmd = &cobra.Command{
	Use:     "init [dir]",
	Short:   "Generate per-project agent rules for the graphed MCP server",
	Args:    cobra.MaximumNArgs(1),
	GroupID: "setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}

		graphFile := initGraphFile
		if graphFile == "" {
			graphFile = "graph.json"
		}

		mcpCommand := initMCPCmd
		if mcpCommand == "" {
			mcpCommand = "kg"
		}

		targets, err := resolveTargets(initTargets, initAll, dir)
		if err != nil {
			return err
		}

		// Optionally ensure the graph the rules reference actually exists.
		if initBuild {
			if err := runInitBuild(dir, graphFile); err != nil {
				return err
			}
		}

		// De-duplicate by target file path (e.g. "agents" and "codex" both
		// produce AGENTS.md).
		type write struct {
			path   string
			data   string
			status string
		}
		seen := map[string]bool{}
		var writes []write
		for _, name := range targets {
			for _, rel := range targetByName(name).paths {
				abs := filepath.Join(dir, filepath.FromSlash(rel))
				if seen[abs] {
					continue
				}
				seen[abs] = true
				sectionName := sectionNameFor(targets, rel)

				existing, readErr := os.ReadFile(abs)
				switch {
				case readErr == nil:
					writes = append(writes, write{
						path:   abs,
						data:   appendRules(string(existing), rulesSection(sectionName, initGraphFile)),
						status: "updated",
					})
				case os.IsNotExist(readErr):
					writes = append(writes, write{
						path:   abs,
						data:   fullContent(sectionName, initGraphFile),
						status: "created",
					})
				default:
					return fmt.Errorf("failed to read %s: %w", abs, readErr)
				}
			}
		}

		for _, w := range writes {
			if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(w.path), err)
			}
			if err := os.WriteFile(w.path, []byte(w.data), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", w.path, err)
			}
			fmt.Printf("%s: %s\n", w.status, w.path)
		}

		// Wire the kg MCP server into each target's own config file so the
		// client actually launches it, not just reads about it in the rules.
		if !initNoMCP {
			if err := writeMCPConfigs(dir, targets, mcpCommand, initGraphFile); err != nil {
				return err
			}
		}

		graphAbs := filepath.Join(dir, graphFile)
		if _, err := os.Stat(graphAbs); err != nil {
			fmt.Printf("note: graph snapshot %s not found; run \"kg build . -o %s\" (or \"kg init --build\") to generate it\n", graphAbs, graphFile)
		}
		return nil
	},
}

// writeMCPConfigs writes the MCP config file for each selected target,
// deduplicating targets that share a config file. A config that cannot be
// merged (e.g. an existing file that is not valid JSON) is left untouched and
// reported as a warning rather than aborting the whole init.
func writeMCPConfigs(dir string, targets []string, command, graphPath string) error {
	seen := map[string]bool{}
	for _, name := range targets {
		for _, mc := range targetByName(name).mcp {
			abs := filepath.Join(dir, filepath.FromSlash(mc.paths[0]))
			if seen[abs] {
				continue
			}
			seen[abs] = true

			status, err := writeMCPConfig(dir, mc, command, graphPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: mcp config for %s not written: %v\n", name, err)
				continue
			}
			fmt.Printf("mcp %s: %s\n", status, abs)
		}
	}
	return nil
}

// runInitBuild runs the full pipeline against dir so the graph snapshot the
// rules reference exists before the rule files are written. Embeddings are
// only produced when a model is configured; a structural graph is enough for
// init to succeed.
func runInitBuild(dir, graphFile string) error {
	cfg, err := config.Load(config.Overrides{})
	if err != nil {
		return err
	}

	opts := graphed.BuildOptions{
		Root:       dir,
		Output:     filepath.Join(dir, graphFile),
		Format:     "json",
		ModelPath:  cfg.ModelPath,
		Dimensions: cfg.Dimensions,
		DBRoot:     cfg.DBRoot,
	}
	if err := graphed.Build(opts); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
}

// resolveTargets decides which targets to emit rules for. An explicit
// --targets list is validated; otherwise targets are detected from the
// project's existing rule files. AGENTS.md (the default rule) is always
// included.
func resolveTargets(targets []string, all bool, dir string) ([]string, error) {
	if all {
		return allTargetNames(), nil
	}

	var flat []string
	if len(targets) > 0 {
		for _, t := range targets {
			for part := range strings.SplitSeq(t, ",") {
				if name := strings.TrimSpace(part); name != "" {
					flat = append(flat, name)
				}
			}
		}
	} else {
		flat = detectTargets(dir)
	}

	if !contains(flat, "agents") {
		flat = append([]string{"agents"}, flat...)
	}

	known := map[string]bool{}
	for _, rt := range ruleTargets {
		known[rt.name] = true
	}
	for _, name := range flat {
		if !known[name] {
			return nil, fmt.Errorf("unknown target %q (supported: %s)", name, strings.Join(allTargetNames(), ", "))
		}
	}
	return flat, nil
}

// detectTargets inspects dir for existing rule files / tool directories and
// returns the targets in use there. "agents" and "codex" are deliberately
// skipped: AGENTS.md is always included anyway.
func detectTargets(dir string) []string {
	var found []string
	for _, rt := range ruleTargets {
		if rt.name == "agents" || rt.name == "codex" {
			continue
		}
		if anyMarker(dir, rt) {
			found = append(found, rt.name)
		}
	}
	return found
}

// anyMarker reports whether a target's rule file or companion directory
// exists under dir.
func anyMarker(dir string, rt ruleTarget) bool {
	for _, rel := range rt.paths {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return true
		}
	}

	switch rt.name {
	case "claude":
		return dirExists(dir, ".claude")
	case "copilot":
		return dirExists(dir, filepath.Join(".github", "copilot"))
	case "cursor":
		return dirExists(dir, filepath.Join(".cursor", "rules"))
	case "cline":
		// .clinerules may be a single file or a directory of rule files.
		return pathExists(dir, ".clinerules")
	case "opencode":
		return pathExists(dir, "opencode.json") || pathExists(dir, "opencode.jsonc") || dirExists(dir, ".opencode")
	case "windsurf":
		return dirExists(dir, filepath.Join(".windsurf", "rules"))
	}
	return false
}

func dirExists(dir, rel string) bool {
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil && info.IsDir()
}

func pathExists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func contains(xs []string, want string) bool {
	return slices.Contains(xs, want)
}

// sectionNameFor returns the target name whose MCP wiring note should be
// documented for the rule file rel. Targets share files ("agents", "codex" and
// "opencode" all produce AGENTS.md), so the first selected target that also
// wires the MCP server into that client wins; otherwise the first selected
// target producing the file is used.
func sectionNameFor(targets []string, rel string) string {
	var fallback string
	for _, name := range targets {
		t := targetByName(name)
		if !contains(t.paths, rel) {
			continue
		}
		if fallback == "" {
			fallback = name
		}
		if len(t.mcp) > 0 {
			return name
		}
	}
	return fallback
}

func allTargetNames() []string {
	names := make([]string, 0, len(ruleTargets))
	for _, rt := range ruleTargets {
		names = append(names, rt.name)
	}
	sort.Strings(names)
	return names
}

func targetByName(name string) ruleTarget {
	for _, rt := range ruleTargets {
		if rt.name == name {
			return rt
		}
	}
	return ruleTarget{name: name, paths: []string{"AGENTS.md"}}
}

// fullContent renders a complete standalone rule file (header + rules block)
// for a target that has no file yet.
func fullContent(name, graphPath string) string {
	var header string
	if targetByName(name).format == "mdc" {
		header = "---\n" +
			"description: Query the local kg knowledge graph (get_narrowed_context) for a narrowed code context before calling cloud models.\n" +
			"---"
	} else {
		header = "# Project Guidance for AI Agents"
	}
	return header + "\n\n" + rulesSection(name, graphPath)
}

// rulesSection is the block kg appends to, or creates inside, a rule file.
// The heading and body all live between the markers, so re-runs replace the
// whole block in place (including the heading) instead of duplicating it,
// and developer-written content elsewhere in the file is preserved.
func rulesSection(name, graphPath string) string {
	return fmt.Sprintf("%s\n## kg knowledge graph\n\n%s\n%s\n", rulesStart, sectionBody(name, graphPath), rulesEnd)
}

// appendRules merges the kg block into an existing rule file: replacing the
// marked block when already present, otherwise appending it at the end.
func appendRules(existing, section string) string {
	start, end := findSection(existing)
	if start >= 0 {
		head := existing[:start]
		tail := existing[end:]
		tail = strings.TrimPrefix(tail, "\n")
		if head != "" && !strings.HasSuffix(head, "\n") {
			head += "\n"
		}
		return head + strings.TrimRight(section, "\n") + "\n" + tail
	}

	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	return existing + "\n" + section
}

// findSection returns the byte range of the marked kg block, or (-1, -1).
func findSection(s string) (start, end int) {
	start = strings.Index(s, rulesStart)
	if start < 0 {
		return -1, -1
	}
	end = strings.Index(s[start:], rulesEnd)
	if end < 0 {
		return -1, -1
	}
	return start, start + end + len(rulesEnd)
}

// sectionBody renders the shared guidance body. The graph snapshot path is
// interpolated so the generated rules point at the file the MCP server loads.
// A per-target note documents where this client's MCP config was wired.
func sectionBody(name, graphPath string) string {
	if graphPath == "" {
		graphPath = "graph.json"
	}
	body := fmt.Sprintf(`This project uses **kg** (go-graphed) to maintain a local, queryable
knowledge graph of the codebase. The graph is exposed to this agent as MCP
tools that read only from the local graph and vector store — nothing leaves the
machine until you send context to a cloud model. Query the graph before calling
a large, cloud-hosted model to conserve tokens and ground your analysis in the
actual repository structure.

## Workflow

1. Keep the graph snapshot fresh (a one-time step; repeat when the code changes
   materially):

       kg build . -o %[1]s

2. Use the MCP tools below to investigate the codebase instead of guessing or
   requesting a broad context dump from a cloud model.

## Available tools

| Tool | Purpose |
| --- | --- |
| get_narrowed_context | Hybrid topological + semantic search for a focused context slice (token saver). |
| get_document_details | Metadata and extracted AST entities for one file. |
| get_document_links | Structural incoming/outgoing links for a file. |
| list_documents_by_format | List files by format (golang, markdown, ...). |
| find_entities_by_type | Global search for entity types (struct, interface, heading, ...). |

## When to use each tool

- **get_narrowed_context** — Start here for bug fixes or new features. Pass
  "entryPath" (the file where the investigation starts) and "searchQuery" (the
  semantic intent or feature description). Optional "maxHops" (default 1) and
  "minScore" (default 0.65) tune how much topology and semantic noise to
  include. Use its output as the primary context before consulting a cloud
  model.
- **get_document_details** — Pass "path" of a single file to get its metadata
  and extracted AST entities.
- **get_document_links** — Pass "path" of a single file to get its structural
  incoming/outgoing links (dependencies, callers, callees).
- **list_documents_by_format** — Pass "format" (golang, markdown, ...) to list
  every indexed file of that format.
- **find_entities_by_type** — Pass "type" (struct, interface, heading, ...) to
  scan the global index for entities of that kind across all files.

## Notes

- The graph snapshot is %[1]s; regenerate it with "kg build" whenever the
  code changes materially.`, graphPath)
	if note := mcpWiringNote(name, graphPath); note != "" {
		body += "\n\n" + note
	}
	return body
}

// mcpWiringNote explains where this client's kg MCP server is configured. It is
// empty for targets without a project-scoped config file. Windsurf reads MCP
// servers from a user-global file only, so its note documents the one-time
// manual step instead.
func mcpWiringNote(name, graphPath string) string {
	if name == "windsurf" {
		return fmt.Sprintf(`## MCP server wiring

- Windsurf reads MCP servers from a user-global file, so "kg init" cannot
  configure it per project. Add the "kg" server to
  "~/.codeium/windsurf/mcp_config.json" once:

      {
        "mcpServers": {
          "kg": {
            "command": "kg",
            "args": ["mcp", "--file", "%[1]s"]
          }
        }
      }`, graphPath)
	}

	mc := targetByName(name).mcp
	if len(mc) == 0 {
		return ""
	}
	paths := make([]string, 0, len(mc))
	for _, c := range mc {
		paths = append(paths, "`"+c.paths[0]+"`")
	}
	return fmt.Sprintf(`## MCP server wiring

- "kg init" wired this client to launch the kg MCP server (server name "kg")
  from %s. Approve the project-scoped server and/or restart the client on first
  use if it prompts.`, strings.Join(paths, " or "))
}

func init() {
	initCmd.Flags().StringVar(
		&initGraphFile,
		"graph-file",
		"graph.json",
		"Path to the graph the MCP server loads (interpolated into the rule files)",
	)

	initCmd.Flags().BoolVar(
		&initAll,
		"all",
		false,
		"Emit rules for every supported target",
	)

	initCmd.Flags().BoolVar(
		&initBuild,
		"build",
		false,
		"Build the knowledge graph first so the snapshot the rules reference exists",
	)

	initCmd.Flags().BoolVar(
		&initNoMCP,
		"no-mcp",
		false,
		"Do not write or merge MCP server config files (rules only)",
	)

	initCmd.Flags().StringVar(
		&initMCPCmd,
		"mcp-command",
		"kg",
		"Command used to launch the kg MCP server in generated MCP configs (defaults to kg on PATH)",
	)

	initCmd.Flags().StringArrayVar(
		&initTargets,
		"targets",
		nil,
		"Agent ecosystems to emit rules for (repeatable or comma-separated: agents, opencode, claude, gemini, copilot, codex, cursor, cline, windsurf; default: detected from existing rule files)",
	)

	RootCmd.AddCommand(initCmd)
}
