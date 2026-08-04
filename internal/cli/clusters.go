package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/spf13/cobra"
)

var (
	clustersFile string
	clustersKind string
)

// clustersCmd loads a built graph and prints its node clusters, giving a
// terminal-only view of the subsystem groupings that the MCP cluster tools
// expose to agents. The --kind flag narrows the report to a single
// derivation strategy (directory, module, or network).
var clustersCmd = &cobra.Command{
	Use:   "clusters",
	Short: "Summarize graph node clusters",
	Long: `Loads a built graph and prints its node clusters grouped by
directory tree, Go module, or network coupling.`,
	Args:    cobra.NoArgs,
	GroupID: "core",
	RunE: func(cmd *cobra.Command, args []string) error {
		graph, err := exporter.LoadGraph(clustersFile)
		if err != nil {
			return err
		}

		var clusters []ir.Cluster
		for _, c := range graph.Clusters {
			if clustersKind == "" || strings.EqualFold(c.Kind, clustersKind) {
				clusters = append(clusters, c)
			}
		}
		return renderClusterSummary(os.Stdout, clusters)
	},
}

func init() {
	clustersCmd.Flags().StringVarP(
		&clustersFile,
		"file",
		"f",
		"graph.json",
		"Source graph data JSON profile path",
	)

	clustersCmd.Flags().StringVar(
		&clustersKind,
		"kind",
		"",
		"Filter by cluster kind: directory, module, or network (empty = all)",
	)

	RootCmd.AddCommand(clustersCmd)
}

// renderClusterSummary writes a human-readable cluster report to w, grouped
// by kind and sorted by name for deterministic output.
func renderClusterSummary(w io.Writer, clusters []ir.Cluster) error {
	if len(clusters) == 0 {
		_, err := fmt.Fprintln(w, "No clusters found in the graph.")
		return err
	}

	sorted := append([]ir.Cluster(nil), clusters...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind < sorted[j].Kind
		}
		return sorted[i].Name < sorted[j].Name
	})

	var sb strings.Builder
	currentKind := ""
	for _, c := range sorted {
		if c.Kind != currentKind {
			currentKind = c.Kind
			sb.WriteString(fmt.Sprintf("\n## %s\n", currentKind))
		}
		sb.WriteString(fmt.Sprintf("* **%s** (%d files) — %s\n", c.Name, c.Size, c.ID))
	}

	_, err := fmt.Fprint(w, sb.String())
	return err
}
