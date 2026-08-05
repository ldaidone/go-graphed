// MCP server configuration generation for "kg init".
//
// Rule files (AGENTS.md, CLAUDE.md, ...) only describe the kg MCP tools; they
// do not make a client actually launch the server. Each agent/assistant/IDE
// needs its own config file with a "kg" entry pointing at `kg mcp`. This file
// renders and merges those per-client config files.
//
// Merging is additive and never clobbers user content: only the "kg" entry is
// written or replaced, every other key in the target config file is preserved,
// and files we cannot parse are left untouched (with a warning) rather than
// overwritten.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// opencodeSchema is the schema URL opencode config files point at.
const opencodeSchema = "https://opencode.ai/config.json"

// mcpConfig describes one per-project config file that wires the kg MCP server
// into a client. kind selects the document shape:
//
//   - "opencode": opencode.json(c) with "mcp.kg" (command array, cwd, enabled)
//   - "json":     "mcpServers.kg" with command + args (Claude, Cursor, Cline, Gemini)
//   - "copilot":  "mcpServers.kg" plus type "local" and tools ["*"]
//   - "codex":    TOML "[mcp_servers.kg]" table
//
// paths lists project-relative candidate files; the first existing file is
// merged into, otherwise the first entry is created.
type mcpConfig struct {
	paths []string
	kind  string
	cwd   bool // include cwd "." so the server runs relative to the project root
}

// mcpServerName is the key under which the kg MCP server is registered in every
// client config.
const mcpServerName = "kg"

// mcpEntry builds the value stored under the "kg" key. Paths (the graph file,
// the working directory) are project-root-relative so the generated configs
// stay portable; the launch command itself is never an absolute path.
func mcpEntry(mc mcpConfig, command, graphPath string) map[string]any {
	if mc.kind == "opencode" {
		return map[string]any{
			"type":    "local",
			"command": []string{command, "mcp", "--file", graphPath},
			"cwd":     ".",
			"enabled": true,
		}
	}
	entry := map[string]any{
		"command": command,
		"args":    []string{"mcp", "--file", graphPath},
	}
	if mc.kind == "copilot" {
		entry["type"] = "local"
		entry["tools"] = []any{"*"}
	}
	if mc.cwd {
		entry["cwd"] = "."
	}
	return entry
}

// writeMCPConfig writes mc into dir, merging into the existing config file when
// one is present. It returns a status string for reporting: "created",
// "updated", or "unchanged" (a no-op re-run).
func writeMCPConfig(dir string, mc mcpConfig, command, graphPath string) (string, error) {
	abs := filepath.Join(dir, filepath.FromSlash(mc.paths[0]))
	for _, rel := range mc.paths {
		candidate := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(candidate); err == nil {
			abs = candidate
			break
		}
	}

	data, err := os.ReadFile(abs)
	switch {
	case err == nil:
		merged, mergeErr := mergeMCPConfig(mc, data, command, graphPath)
		if mergeErr != nil {
			return "", fmt.Errorf("%s: %w (leaving file untouched)", abs, mergeErr)
		}
		if bytes.Equal(bytes.TrimSpace(merged), bytes.TrimSpace(data)) {
			return "unchanged", nil
		}
		if err := os.WriteFile(abs, merged, 0644); err != nil {
			return "", fmt.Errorf("failed to write %s: %w", abs, err)
		}
		return "updated", nil
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", filepath.Dir(abs), err)
		}
		if err := os.WriteFile(abs, freshMCPConfig(mc, command, graphPath), 0644); err != nil {
			return "", fmt.Errorf("failed to write %s: %w", abs, err)
		}
		return "created", nil
	default:
		return "", fmt.Errorf("failed to read %s: %w", abs, err)
	}
}

// freshMCPConfig renders a standalone config document for a client that has no
// config file yet.
func freshMCPConfig(mc mcpConfig, command, graphPath string) []byte {
	switch mc.kind {
	case "opencode":
		doc, _ := marshalJSON(map[string]any{
			"$schema": opencodeSchema,
			"mcp":     map[string]any{mcpServerName: mcpEntry(mc, command, graphPath)},
		})
		return doc
	case "codex":
		doc, err := toml.Marshal(map[string]any{
			"mcp_servers": map[string]any{mcpServerName: mcpEntry(mc, command, graphPath)},
		})
		if err != nil {
			return nil
		}
		return doc
	default:
		doc, _ := marshalJSON(map[string]any{
			"mcpServers": map[string]any{mcpServerName: mcpEntry(mc, command, graphPath)},
		})
		return doc
	}
}

// mergeMCPConfig folds the "kg" entry into an existing config document.
func mergeMCPConfig(mc mcpConfig, data []byte, command, graphPath string) ([]byte, error) {
	switch mc.kind {
	case "opencode":
		return mergeOpenCode(data, command, graphPath)
	case "codex":
		return mergeTOML(data, mc, command, graphPath)
	default:
		return mergeJSONServers(data, mc, command, graphPath)
	}
}

// mergeJSONServers merges into a strict-JSON "mcpServers" document, preserving
// every key except the "kg" entry under mcpServers.
func mergeJSONServers(data []byte, mc mcpConfig, command, graphPath string) ([]byte, error) {
	root, err := parseJSON(data)
	if err != nil {
		return nil, err
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	entry := mcpEntry(mc, command, graphPath)
	if reflect.DeepEqual(servers[mcpServerName], normalizeJSON(entry)) {
		return data, nil
	}
	servers[mcpServerName] = entry
	root["mcpServers"] = servers
	return marshalJSON(root)
}

// mergeOpenCode merges into an opencode config file. Strict JSON is merged
// structurally; a JSONC file (comments, trailing commas) that encoding/json
// cannot parse is merged textually so its formatting and comments survive.
func mergeOpenCode(data []byte, command, graphPath string) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return freshMCPConfig(mcpConfig{kind: "opencode", paths: []string{"opencode.json"}}, command, graphPath), nil
	}
	if root, err := parseJSON(data); err == nil {
		mcp, _ := root["mcp"].(map[string]any)
		if mcp == nil {
			mcp = map[string]any{}
		}
		entry := mcpEntry(mcpConfig{kind: "opencode"}, command, graphPath)
		if reflect.DeepEqual(mcp[mcpServerName], normalizeJSON(entry)) {
			return data, nil
		}
		mcp[mcpServerName] = entry
		root["mcp"] = mcp
		return marshalJSON(root)
	}
	out, err := mergeOpenCodeJSONC(string(data), command, graphPath)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// mergeTOML merges into a Codex config.toml, preserving everything except the
// "kg" table under mcp_servers.
func mergeTOML(data []byte, mc mcpConfig, command, graphPath string) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("invalid TOML: %w", err)
		}
	}
	servers, _ := root["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	entry := mcpEntry(mc, command, graphPath)
	if reflect.DeepEqual(servers[mcpServerName], normalizeJSON(entry)) {
		return data, nil
	}
	servers[mcpServerName] = entry
	root["mcp_servers"] = servers
	return toml.Marshal(root)
}

// parseJSON unmarshals data into a generic object, rejecting anything that is
// not a strict-JSON top-level object.
func parseJSON(data []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return root, nil
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return root, nil
}

// normalizeJSON round-trips a value through JSON so it can be compared against
// values already decoded from a config file ([]any vs []string and friends).
func normalizeJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return v
	}
	return out
}

func marshalJSON(v any) ([]byte, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// mergeOpenCodeJSONC folds the "kg" entry into an opencode.jsonc document that
// is not strict JSON, preserving the file byte-for-byte except for the injected
// entry. If the "mcp" object exists its "kg" key is set; otherwise a fresh
// "mcp" object is inserted before the final top-level brace.
func mergeOpenCodeJSONC(content, command, graphPath string) (string, error) {
	if strings.Contains(content, `"`+mcpServerName+`"`) {
		return content, nil
	}

	entry := openCodeEntryText(command, graphPath)

	open, close := findTopLevelValueBrace(content, "mcp")
	if open < 0 {
		end := lastTopLevelBrace(content)
		if end < 0 {
			return "", fmt.Errorf("could not locate the top-level JSON object")
		}
		block := "  \"mcp\": {\n" + entry + "  }"
		before := strings.TrimRight(content[:end], " \t\r\n")
		if !strings.HasSuffix(before, "{") {
			block = ",\n" + block
		}
		return before + "\n" + block + "\n" + content[end:], nil
	}

	if strings.Contains(content[open:close], `"`+mcpServerName+`"`) {
		return content, nil
	}
	inner := content[open+1 : close]
	trimmed := strings.TrimSpace(inner)
	var insertion string
	switch {
	case trimmed == "":
		insertion = "\n" + entry
	case strings.HasSuffix(trimmed, ","):
		// The last member already carries a trailing comma; do not add another.
		insertion = "\n" + entry
	default:
		insertion = ",\n" + entry
	}
	return content[:open+1] + inner + insertion + content[close:], nil
}

// openCodeEntryText renders the JSONC block for the "kg" entry, indented for
// insertion inside an "mcp" object.
func openCodeEntryText(command, graphPath string) string {
	return fmt.Sprintf(`    "kg": {
      "type": "local",
      "command": [%q, "mcp", "--file", %q],
      "cwd": ".",
      "enabled": true
    },
`, command, graphPath)
}

// findTopLevelValueBrace returns the range of the object value for a top-level
// key (e.g. "mcp": { ... }), or (-1, -1) when the key is absent. Strings and
// JSONC comments are ignored while scanning.
func findTopLevelValueBrace(s, key string) (open, close int) {
	keyToken := `"` + key + `"`
	depth := 0
	i := 0
	for i < len(s) {
		switch {
		case s[i] == '"':
			end := skipJSONString(s, i)
			if depth == 1 && s[i:end] == keyToken {
				j := end
				for j < len(s) && isJSONSpace(s[j]) {
					j++
				}
				if j < len(s) && s[j] == ':' {
					j++
					for j < len(s) && isJSONSpace(s[j]) {
						j++
					}
					if j < len(s) && s[j] == '{' {
						return j, findMatchingBrace(s, j)
					}
				}
			}
			i = end
		case s[i] == '{':
			depth++
			i++
		case s[i] == '}':
			depth--
			i++
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
		default:
			i++
		}
	}
	return -1, -1
}

// lastTopLevelBrace returns the index of the final brace that closes the
// top-level object, or -1 when none is found.
func lastTopLevelBrace(s string) int {
	depth := 0
	last := -1
	i := 0
	for i < len(s) {
		switch {
		case s[i] == '"':
			i = skipJSONString(s, i)
		case s[i] == '{':
			depth++
			i++
		case s[i] == '}':
			depth--
			if depth == 0 {
				last = i
			}
			i++
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
		default:
			i++
		}
	}
	return last
}

// findMatchingBrace returns the index of the brace closing the object opened at
// open, or -1 when the document is malformed.
func findMatchingBrace(s string, open int) int {
	depth := 0
	i := open
	for i < len(s) {
		switch {
		case s[i] == '"':
			i = skipJSONString(s, i)
		case s[i] == '{':
			depth++
			i++
		case s[i] == '}':
			depth--
			if depth == 0 {
				return i
			}
			i++
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
		default:
			i++
		}
	}
	return -1
}

// skipJSONString returns the index just past the string literal starting at i
// (which must point at a double quote), honoring backslash escapes.
func skipJSONString(s string, i int) int {
	i++
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '"':
			return i + 1
		}
		i++
	}
	return i
}

func isJSONSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}
