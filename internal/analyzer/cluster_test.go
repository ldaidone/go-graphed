package analyzer

import (
	"reflect"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestBuild_PopulatesAllClusterKinds(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "internal/ir/types.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "internal/ir/types.go#package", Type: "package", Name: "ir", Metadata: map[string]string{"package_path": "internal/ir"}},
				{ID: "internal/ir/types.go#Graph", Type: "struct", Name: "Graph"},
			},
		},
		{
			Path:     "internal/ir/helpers.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "internal/ir/helpers.go#package", Type: "package", Name: "ir", Metadata: map[string]string{"package_path": "internal/ir"}},
			},
		},
		{
			Path:     "internal/analyzer/analyzer.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "internal/analyzer/analyzer.go#package", Type: "package", Name: "analyzer", Metadata: map[string]string{"package_path": "internal/analyzer"}},
				{ID: "internal/analyzer/analyzer.go#import:github.com/ldaidone/go-graphed/internal/ir", Type: "import", Name: "github.com/ldaidone/go-graphed/internal/ir"},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	kinds := map[string]int{}
	for _, c := range graph.Clusters {
		kinds[c.Kind]++
	}
	if kinds[ir.ClusterKindDirectory] == 0 {
		t.Error("expected directory clusters")
	}
	if kinds[ir.ClusterKindModule] != 2 {
		t.Errorf("module clusters = %d, want 2", kinds[ir.ClusterKindModule])
	}
	if kinds[ir.ClusterKindNetwork] == 0 {
		t.Error("expected network clusters")
	}
}

func TestBuild_EmptyInputHasNoClusters(t *testing.T) {
	graph, err := Build(nil)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if len(graph.Clusters) != 0 {
		t.Errorf("expected no clusters for empty graph, got %d", len(graph.Clusters))
	}
}

func TestClusterByDirectory_GroupByDirectoryTree(t *testing.T) {
	graph := &ir.Graph{Documents: map[string]*ir.Document{
		"internal/ir/types.go":   {},
		"internal/ir/helpers.go": {},
		"internal/analyzer/a.go": {},
		"README.md":              {},
	}}

	clusters := clusterByDirectory(graph)
	if len(clusters) != 3 {
		t.Fatalf("expected 3 directory clusters, got %d: %+v", len(clusters), clusters)
	}

	byName := map[string]ir.Cluster{}
	for _, c := range clusters {
		if c.Kind != ir.ClusterKindDirectory {
			t.Errorf("cluster %s kind = %q, want directory", c.ID, c.Kind)
		}
		if c.ID != ir.ClusterID(ir.ClusterKindDirectory, c.Name) {
			t.Errorf("cluster ID %q does not match kind:name convention", c.ID)
		}
		if c.Size != len(c.Members) {
			t.Errorf("cluster %s Size = %d, len(Members) = %d", c.ID, c.Size, len(c.Members))
		}
		byName[c.Name] = c
	}

	irC := byName["internal/ir"]
	if len(irC.Members) != 2 || irC.Members[0] != "internal/ir/helpers.go" || irC.Members[1] != "internal/ir/types.go" {
		t.Errorf("internal/ir members = %v, want sorted [helpers.go types.go]", irC.Members)
	}
	if len(byName["internal/analyzer"].Members) != 1 {
		t.Errorf("internal/analyzer members = %v, want 1", byName["internal/analyzer"].Members)
	}
	root := byName["."]
	if len(root.Members) != 1 || root.Members[0] != "README.md" {
		t.Errorf("root cluster members = %v, want [README.md]", root.Members)
	}
}

func TestClusterByDirectory_StripsCommonRoot(t *testing.T) {
	graph := &ir.Graph{Documents: map[string]*ir.Document{
		"/tmp/proj/internal/ir/types.go":   {},
		"/tmp/proj/internal/analyzer/a.go": {},
		"/tmp/proj/README.md":              {},
	}}

	clusters := clusterByDirectory(graph)
	names := map[string]bool{}
	for _, c := range clusters {
		names[c.Name] = true
	}
	for _, want := range []string{"internal/ir", "internal/analyzer", "."} {
		if !names[want] {
			t.Errorf("expected directory cluster named %q, got %v", want, names)
		}
	}
}

func TestClusterByModule_UsesPackageIndex(t *testing.T) {
	graph := &ir.Graph{
		Packages: map[string]*ir.Package{
			"internal/ir": {Name: "ir", Path: "internal/ir", Files: []string{"internal/ir/types.go", "internal/ir/helpers.go"}},
			"internal/x":  {Name: "x", Path: "internal/x", Files: []string{"internal/x/x.go"}},
		},
	}

	clusters := clusterByModule(graph)
	if len(clusters) != 2 {
		t.Fatalf("expected 2 module clusters, got %d", len(clusters))
	}

	byName := map[string]ir.Cluster{}
	for _, c := range clusters {
		if c.Kind != ir.ClusterKindModule {
			t.Errorf("cluster %s kind = %q, want module", c.ID, c.Kind)
		}
		byName[c.Name] = c
	}

	irC := byName["internal/ir"]
	if len(irC.Members) != 2 || irC.Members[0] != "internal/ir/helpers.go" || irC.Members[1] != "internal/ir/types.go" {
		t.Errorf("module members = %v, want sorted package files", irC.Members)
	}
}

func TestClusterByModule_NoPackages(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"main.go": {},
		},
	}
	clusters := clusterByModule(graph)
	if len(clusters) != 0 {
		t.Errorf("expected no module clusters without a package index, got %d", len(clusters))
	}
}

func TestClusterByNetwork_CouplesConnectedDocuments(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"internal/store/store.go":    {},
			"internal/store/memstore.go": {},
			"isolated.go":                {},
		},
		Links: []ir.Link{
			{SourceID: "internal/store/memstore.go#MemStore", TargetID: "internal/store/store.go#Store", Type: "implements", Weight: 0.8},
		},
	}

	clusters := clusterByNetwork(graph)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 network cluster, got %d: %+v", len(clusters), clusters)
	}
	c := clusters[0]
	if c.Kind != ir.ClusterKindNetwork {
		t.Errorf("cluster kind = %q, want network", c.Kind)
	}
	if c.Size != 2 {
		t.Errorf("cluster size = %d, want 2", c.Size)
	}
	for _, want := range []string{"internal/store/store.go", "internal/store/memstore.go"} {
		found := false
		for _, m := range c.Members {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Errorf("network cluster missing member %q: %v", want, c.Members)
		}
	}
	// The isolated document must not leak into any network cluster.
	for _, m := range c.Members {
		if m == "isolated.go" {
			t.Errorf("isolated document should not be a network cluster member")
		}
	}
}

func TestClusterByNetwork_SkipsLowWeightLinks(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {},
			"b.go": {},
		},
		Links: []ir.Link{
			{SourceID: "a.go", TargetID: "b.go", Type: "references", Weight: 0.4},
		},
	}
	clusters := clusterByNetwork(graph)
	if len(clusters) != 0 {
		t.Errorf("expected no network clusters for weak links, got %d", len(clusters))
	}
}

func TestClusterByNetwork_DropsSingletonGroups(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {},
			"b.go": {},
		},
		Links: []ir.Link{},
	}
	clusters := clusterByNetwork(graph)
	if len(clusters) != 0 {
		t.Errorf("expected no network clusters for isolated docs, got %d", len(clusters))
	}
}

func TestClusterByNetwork_PackageExpansion(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"internal/ir/types.go":   {},
			"internal/ir/helpers.go": {},
			"internal/analyzer/a.go": {},
		},
		Packages: map[string]*ir.Package{
			"internal/ir":       {Name: "ir", Path: "internal/ir", Files: []string{"internal/ir/types.go", "internal/ir/helpers.go"}},
			"internal/analyzer": {Name: "analyzer", Path: "internal/analyzer", Files: []string{"internal/analyzer/a.go"}},
		},
		Links: []ir.Link{
			{SourceID: "internal/ir/types.go", TargetID: ir.PackageNodeID("internal/ir"), Type: "part_of", Weight: 1.0},
			{SourceID: "internal/analyzer/a.go", TargetID: ir.PackageNodeID("internal/ir"), Type: "imports", Weight: 0.8},
		},
	}

	clusters := clusterByNetwork(graph)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 network cluster via package expansion, got %d: %+v", len(clusters), clusters)
	}
	c := clusters[0]
	// The importer couples with the whole dependency package.
	for _, want := range []string{"internal/ir/types.go", "internal/ir/helpers.go", "internal/analyzer/a.go"} {
		found := false
		for _, m := range c.Members {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Errorf("network cluster missing %q after package expansion: %v", want, c.Members)
		}
	}
}

func TestClusterByNetwork_IgnoresUnindexedEndpoints(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {},
		},
		Links: []ir.Link{
			// Ghost target: not present in Documents, must be dropped.
			{SourceID: "a.go", TargetID: "ghost.go#Ghost", Type: "references", Weight: 1.0},
		},
	}
	clusters := clusterByNetwork(graph)
	if len(clusters) != 0 {
		t.Errorf("expected no network clusters when endpoints are unindexed, got %d", len(clusters))
	}
}

func TestClusterByNetwork_Deterministic(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"internal/a/a.go": {},
			"internal/a/b.go": {},
			"internal/b/a.go": {},
			"internal/b/b.go": {},
		},
		Links: []ir.Link{
			{SourceID: "internal/a/a.go#A", TargetID: "internal/a/b.go#B", Type: "calls", Weight: 1.0},
			{SourceID: "internal/a/b.go#B", TargetID: "internal/b/a.go#C", Type: "calls", Weight: 1.0},
			{SourceID: "internal/b/a.go#C", TargetID: "internal/b/b.go#D", Type: "calls", Weight: 1.0},
		},
	}

	first := clusterByNetwork(graph)
	for i := 0; i < 10; i++ {
		again := clusterByNetwork(graph)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("clusterByNetwork is not deterministic:\nfirst: %+v\nagain: %+v", first, again)
		}
	}
}

func TestNetworkClusterName(t *testing.T) {
	cases := []struct {
		name    string
		members []string
		want    string
	}{
		{"single directory", []string{"src/a/f1.go", "src/a/f2.go"}, "src/a"},
		{
			"majority subtree beats shallow ancestor",
			[]string{
				"src/main/kotlin/com/x/service/A.kt",
				"src/main/kotlin/com/x/service/B.kt",
				"src/main/kotlin/com/x/repo/C.kt",
			},
			"src/main/kotlin/com/x/service",
		},
		{
			"stray root file does not rename the community",
			[]string{".gitlab-ci.yml", "src/service/A.kt", "src/service/B.kt"},
			"src/service",
		},
		{"scattered members fall back to shared ancestor", []string{"src/a/1.go", "src/b/1.go", "src/c/1.go"}, "src"},
		{"all root members yield no name", []string{"a.go", "b.go"}, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := networkClusterName(tt.members); got != tt.want {
				t.Errorf("networkClusterName(%v) = %q, want %q", tt.members, got, tt.want)
			}
		})
	}
}

func TestClusterByNetwork_DisambiguatesCollidingNames(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"a/x/1.go": {},
			"a/x/2.go": {},
			"a/x/3.go": {},
			"a/x/4.go": {},
		},
		Links: []ir.Link{
			{SourceID: "a/x/1.go", TargetID: "a/x/2.go", Type: "calls", Weight: 1.0},
			{SourceID: "a/x/3.go", TargetID: "a/x/4.go", Type: "calls", Weight: 1.0},
		},
	}

	clusters := clusterByNetwork(graph)
	if len(clusters) != 2 {
		t.Fatalf("expected 2 disjoint network clusters, got %d: %+v", len(clusters), clusters)
	}
	names := map[string]bool{}
	for _, c := range clusters {
		names[c.Name] = true
	}
	if !names["a/x"] || !names["a/x #2"] {
		t.Errorf("colliding network names should be disambiguated, got %v", names)
	}
}
