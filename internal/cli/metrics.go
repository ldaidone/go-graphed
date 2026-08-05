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

var metricsFile string

// metricsCmd loads a built graph and prints its global centrality ("God
// Node") scores, giving a terminal-only view of the hub documents that the
// MCP get_graph_metrics tool exposes to agents. Hubs come first, then the
// remaining documents ranked by PageRank, so the report reads top-down as
// "what does everything funnel through".
var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Summarize graph centrality metrics",
	Long: `Loads a built graph and prints its global centrality ("God Node")
metrics: degree, weighted degree, and PageRank per document, with hub
documents flagged.`,
	Args:    cobra.NoArgs,
	GroupID: "core",
	RunE: func(cmd *cobra.Command, args []string) error {
		graph, err := exporter.LoadGraph(metricsFile)
		if err != nil {
			return err
		}
		return renderMetricsSummary(os.Stdout, graph)
	},
}

func init() {
	metricsCmd.Flags().StringVarP(
		&metricsFile,
		"file",
		"f",
		"graph.json",
		"Source graph data JSON profile path",
	)

	RootCmd.AddCommand(metricsCmd)
}

// renderMetricsSummary writes a human-readable centrality report to w. Hubs
// are listed first; the rest follow ranked by PageRank with paths breaking
// ties, so the output is deterministic for a given snapshot.
func renderMetricsSummary(w io.Writer, graph *ir.Graph) error {
	if graph.Metrics.Documents == nil {
		_, err := fmt.Fprintln(w, "No centrality metrics found in the graph. Rebuild with a recent kg build.")
		return err
	}

	type ranked struct {
		path string
		dm   ir.DocumentMetrics
	}
	docs := make([]ranked, 0, len(graph.Metrics.Documents))
	for path, dm := range graph.Metrics.Documents {
		docs = append(docs, ranked{path: path, dm: dm})
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].dm.IsHub != docs[j].dm.IsHub {
			return docs[i].dm.IsHub
		}
		if docs[i].dm.PageRank != docs[j].dm.PageRank {
			return docs[i].dm.PageRank > docs[j].dm.PageRank
		}
		return docs[i].path < docs[j].path
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Documents indexed: %d\n", len(graph.Documents)))
	sb.WriteString(fmt.Sprintf("Hub (God Node) count: %d\n\n", graph.Metrics.HubCount))
	sb.WriteString("Ranked by PageRank; hubs first:\n")
	if len(docs) == 0 {
		sb.WriteString("(no documents scored)\n")
	} else {
		for _, d := range docs {
			marker := "    "
			if d.dm.IsHub {
				marker = "[HUB]"
			}
			sb.WriteString(fmt.Sprintf("%5s %-44s degree=%-5d weighted=%-8.2f pagerank=%.5f\n",
				marker, d.path, d.dm.Degree, d.dm.WeightedDegree, d.dm.PageRank))
		}
	}

	_, err := fmt.Fprint(w, sb.String())
	return err
}
