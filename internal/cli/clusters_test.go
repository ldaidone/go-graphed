package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestRenderClusterSummary_GroupsAndSorts(t *testing.T) {
	clusters := []ir.Cluster{
		{ID: "network:x", Name: "internal", Kind: ir.ClusterKindNetwork, Size: 3},
		{ID: "directory:internal/ir", Name: "internal/ir", Kind: ir.ClusterKindDirectory, Size: 2},
		{ID: "directory:internal/analyzer", Name: "internal/analyzer", Kind: ir.ClusterKindDirectory, Size: 1},
		{ID: "module:internal/ir", Name: "internal/ir", Kind: ir.ClusterKindModule, Size: 2},
	}

	var buf bytes.Buffer
	if err := renderClusterSummary(&buf, clusters); err != nil {
		t.Fatalf("renderClusterSummary returned error: %v", err)
	}
	text := buf.String()

	// Kinds must appear as section headers in stable order.
	dirPos := strings.Index(text, "## directory")
	modPos := strings.Index(text, "## module")
	netPos := strings.Index(text, "## network")
	if dirPos < 0 || modPos < 0 || netPos < 0 {
		t.Fatalf("expected directory/module/network sections:\n%s", text)
	}
	if !(dirPos < modPos && modPos < netPos) {
		t.Errorf("section order = directory(%d) module(%d) network(%d), want dir < mod < net", dirPos, modPos, netPos)
	}

	// Names sort alphabetically within each kind.
	analyzerPos := strings.Index(text, "**internal/analyzer**")
	irDirPos := strings.Index(text, "**internal/ir** (2 files) — directory:internal/ir")
	if analyzerPos < 0 || irDirPos < 0 || analyzerPos > irDirPos {
		t.Errorf("directory clusters out of alphabetical order:\n%s", text)
	}

	// IDs must be present so callers can drill down via get_cluster.
	if !strings.Contains(text, "module:internal/ir") || !strings.Contains(text, "network:x") {
		t.Errorf("expected cluster IDs in report:\n%s", text)
	}
}

func TestRenderClusterSummary_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderClusterSummary(&buf, nil); err != nil {
		t.Fatalf("renderClusterSummary returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "No clusters found") {
		t.Errorf("expected empty-report message, got: %s", buf.String())
	}
}
