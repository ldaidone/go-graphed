package cli

import (
	"fmt"

	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/spf13/cobra"
)

var (
	visualizeFile   string
	visualizeHTML   string
	visualizeReport string
)

// visualizeCmd loads a built graph and renders the two human-readable
// artifacts: a self-contained interactive HTML visualizer (graph.html) and a
// markdown summary (GRAPH_REPORT.md). Both mirror the terminal views of
// "kg metrics" and "kg clusters" so committed documentation stays in sync
// with what the CLI prints.
var visualizeCmd = &cobra.Command{
	Use:   "visualize",
	Short: "Generate an HTML visualizer and markdown report",
	Long: `Loads a built graph and generates two human-readable artifacts:
a self-contained interactive HTML visualizer (graph.html) and a markdown
report (GRAPH_REPORT.md) summarizing hub ("God Node") documents, coupling
hotspots, and subsystem clusters.`,
	Args:    cobra.NoArgs,
	GroupID: "core",
	RunE: func(cmd *cobra.Command, args []string) error {
		graph, err := exporter.LoadGraph(visualizeFile)
		if err != nil {
			return err
		}
		if err := exporter.HTML(*graph, visualizeHTML); err != nil {
			return err
		}
		if err := exporter.Report(*graph, visualizeReport); err != nil {
			return err
		}
		fmt.Printf("Wrote %s and %s\n", visualizeHTML, visualizeReport)
		return nil
	},
}

func init() {
	visualizeCmd.Flags().StringVarP(
		&visualizeFile,
		"file",
		"f",
		"graph.json",
		"Source graph data JSON profile path",
	)
	visualizeCmd.Flags().StringVar(
		&visualizeHTML,
		"html",
		"graph.html",
		"Output path for the interactive HTML visualizer",
	)
	visualizeCmd.Flags().StringVar(
		&visualizeReport,
		"report",
		"GRAPH_REPORT.md",
		"Output path for the markdown report",
	)

	RootCmd.AddCommand(visualizeCmd)
}
