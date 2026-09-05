package analyzer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// networkGraph is the document-level coupling graph used by the network
// clustering passes. Edges are weighted: intra-package "part_of" edges are
// heavier than cross-package "imports" edges, so modularity can tell "same
// subsystem" apart from "imports another subsystem" even when every package
// depends on every other. Weights are link weights scaled to integer tenths
// (1.0 -> 10, 0.8 -> 8) to keep the modularity comparison in integer math.
type networkGraph map[string]map[string]int64

// Clustering thresholds shared across every grouping pass.
const (
	// clusterNetworkMinWeight drops weak links from the coupling graph so
	// near-noise edges (weight < 0.5) cannot drag unrelated files into the
	// same network cluster. It mirrors the traversal threshold the MCP
	// layer applies when walking link topology.
	clusterNetworkMinWeight = 0.5

	// clusterNetworkMaxNodes bounds the greedy-modularity pass, which is
	// super-linear. Larger coupling graphs fall back to connected
	// components so a pathological monorepo cannot stall the analyzer.
	clusterNetworkMaxNodes = 2000
)

// annotateClusters groups the graph's documents by directory tree, Go
// package, and network coupling, storing the result on Graph.Clusters. It
// runs after the link passes so every grouping can consume the final,
// normalized link set.
func annotateClusters(graph *ir.Graph) {
	clusters := make([]ir.Cluster, 0, len(graph.Documents))
	clusters = append(clusters, clusterByDirectory(graph)...)
	clusters = append(clusters, clusterByModule(graph)...)
	clusters = append(clusters, clusterByNetwork(graph)...)
	graph.Clusters = clusters
}

// clusterByDirectory groups documents by the local directory tree. Each
// document's parent directory becomes a cluster; the shared project root
// (when derivable) is stripped so names stay readable. Documents sitting
// at the root itself land in a "." cluster.
func clusterByDirectory(graph *ir.Graph) []ir.Cluster {
	if len(graph.Documents) == 0 {
		return nil
	}
	root := commonDocRoot(graph.Documents)
	groups := make(map[string][]string)
	for path := range graph.Documents {
		name := relPath(filepath.Dir(path), root)
		// Documents sitting directly in the project root are their own
		// cluster named ".".
		if root != "" && filepath.Dir(path) == root {
			name = "."
		}
		groups[name] = append(groups[name], path)
	}

	clusters := make([]ir.Cluster, 0, len(groups))
	for name, members := range groups {
		sort.Strings(members)
		clusters = append(clusters, ir.Cluster{
			ID:      ir.ClusterID(ir.ClusterKindDirectory, name),
			Name:    name,
			Kind:    ir.ClusterKindDirectory,
			Members: members,
			Size:    len(members),
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Name < clusters[j].Name
	})
	return clusters
}

// clusterByModule groups documents by their Go package. Each indexed
// package directory (the module's per-directory unit) becomes a cluster
// whose members are the package's files. Projects without Go packages
// produce no module clusters.
func clusterByModule(graph *ir.Graph) []ir.Cluster {
	clusters := make([]ir.Cluster, 0, len(graph.Packages))
	for dir, pkg := range graph.Packages {
		members := append([]string(nil), pkg.Files...)
		sort.Strings(members)
		clusters = append(clusters, ir.Cluster{
			ID:      ir.ClusterID(ir.ClusterKindModule, dir),
			Name:    dir,
			Kind:    ir.ClusterKindModule,
			Members: members,
			Size:    len(members),
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Name < clusters[j].Name
	})
	return clusters
}

// clusterByNetwork groups documents by structural coupling. A greedy
// modularity pass runs over the document-level graph derived from the
// link set: every document starts in its own community, then the pair of
// communities with the largest positive modularity gain is repeatedly
// merged until no merge improves the partition. Unlike raw label
// propagation -- which collapses a single connected component into one
// label -- modularity keeps tightly-coupled cliques (e.g. a Go package)
// together while leaving loosely-coupled subsystems separate, which is
// exactly the "auth_subsystem / billing_domain" view agents want.
// Documents with no edges (or that stay in a singleton community) do not
// produce network clusters.
func clusterByNetwork(graph *ir.Graph) []ir.Cluster {
	adj := networkAdjacency(graph)
	if len(adj) < 2 {
		return nil
	}

	var labels map[string]string
	if len(adj) <= clusterNetworkMaxNodes {
		labels = greedyModularity(adj)
	} else {
		labels = connectedComponents(adj)
	}

	groups := make(map[string][]string)
	for node, label := range labels {
		groups[label] = append(groups[label], node)
	}

	clusters := make([]ir.Cluster, 0, len(groups))
	used := make(map[string]int)
	for label, members := range groups {
		if len(members) < 2 {
			continue
		}
		sort.Strings(members)
		name := networkClusterName(members)
		if name == "" {
			name = label
		}
		// A community's shared-ancestor label can collide with another's
		// (two coupling groups both deepest under "src"), so disambiguate
		// with a numeric suffix instead of silently merging names.
		if n, taken := used[name]; taken {
			used[name] = n + 1
			name = fmt.Sprintf("%s #%d", name, n+1)
		} else {
			used[name] = 1
		}
		clusters = append(clusters, ir.Cluster{
			ID:      ir.ClusterID(ir.ClusterKindNetwork, label),
			Name:    name,
			Kind:    ir.ClusterKindNetwork,
			Members: members,
			Size:    len(members),
		})
	}
	// Largest, most-coupled clusters first so consumers see the "hub"
	// subsystems at the top; ties break alphabetically.
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Size != clusters[j].Size {
			return clusters[i].Size > clusters[j].Size
		}
		return clusters[i].Name < clusters[j].Name
	})
	return clusters
}

// networkAdjacency projects the graph's links onto a weighted,
// document-level undirected adjacency map. Entity anchors collapse onto
// their owning file and package nodes expand onto every member file, so a
// single "imports" edge couples the importer with the whole dependency
// package in one step. Each contributing link adds its weight (scaled to
// integer tenths); intra-package "part_of" edges therefore accumulate twice
// the weight of a plain cross-package import, letting the clustering tell
// same-package cohesion apart from mere import coupling. Only edges whose
// endpoints resolve to indexed documents are kept.
func networkAdjacency(graph *ir.Graph) networkGraph {
	adj := make(networkGraph, len(graph.Documents))
	edge := func(a, b string, w int64) {
		if a == b {
			return
		}
		if adj[a] == nil {
			adj[a] = make(map[string]int64)
		}
		if adj[b] == nil {
			adj[b] = make(map[string]int64)
		}
		adj[a][b] += w
		adj[b][a] += w
	}

	for _, link := range graph.Links {
		if link.Weight < clusterNetworkMinWeight {
			continue
		}
		// Markdown/PDF "documents" edges are path-keyword heuristics: they
		// associate docs with code but do not reflect structural coupling.
		// Excluding them keeps PageRank hubs and network clusters keyed to
		// real source links instead of docs cross-referencing each other.
		if link.Type == "documents" {
			continue
		}
		weight := int64(link.Weight * 10)
		if weight <= 0 {
			continue
		}
		srcs := filterIndexed(graph, clusterDocPaths(graph, link.SourceID))
		tgts := filterIndexed(graph, clusterDocPaths(graph, link.TargetID))
		for _, s := range srcs {
			for _, t := range tgts {
				edge(s, t, weight)
			}
		}
	}
	return adj
}

// clusterDocPaths resolves a graph node ID onto the document paths it
// represents for clustering: entity IDs collapse to their owning file and
// package nodes expand to every indexed member file.
func clusterDocPaths(graph *ir.Graph, id string) []string {
	if after, ok := strings.CutPrefix(id, ir.PackageNodePrefix); ok {
		dir := after
		if pkg, ok := graph.Packages[dir]; ok {
			return pkg.Files
		}
		return nil
	}
	return []string{strings.SplitN(id, "#", 2)[0]}
}

// filterIndexed keeps only the document paths that exist in the index, so
// links referencing unindexed files cannot create phantom cluster nodes.
func filterIndexed(graph *ir.Graph, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := graph.Documents[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

// greedyModularity clusters the weighted adjacency map using Newman's
// greedy modularity maximisation. Every node starts in its own community;
// each round the pair of communities whose merge yields the largest
// positive modularity gain is merged, until no merge improves the
// partition. Communities are identified by their smallest member (in
// sorted order) and ties are broken by community id, so the assignment is
// deterministic. The map value for each node is its community id.
func greedyModularity(adj networkGraph) map[string]string {
	nodes := make([]string, 0, len(adj))
	for node := range adj {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	index := make(map[string]int, len(nodes))
	for i, n := range nodes {
		index[n] = i
	}

	// Total undirected weight: each weighted edge is stored twice.
	m := int64(0)
	for _, nbs := range adj {
		for _, w := range nbs {
			m += w
		}
	}
	m /= 2
	if m == 0 {
		labels := make(map[string]string, len(nodes))
		for _, n := range nodes {
			labels[n] = n
		}
		return labels
	}

	// Community id per node; initially every node is its own community.
	community := make([]int, len(nodes))
	for i := range community {
		community[i] = i
	}

	// Merge the pair with the largest positive modularity gain. The gain of
	// merging communities i and j is proportional to
	//
	//	score = 2*m*inter(i,j) - deg(i)*deg(j)
	//
	// where inter(i,j) is the total weight of edges spanning the pair and
	// deg(c) is the sum of weighted degrees inside community c, so the
	// comparison is pure integer arithmetic. Scores <= 0 mean the merge
	// would not increase modularity, so the pass stops.
	for {
		deg := make([]int64, len(nodes))
		for u, n := range nodes {
			deg[community[u]] += weightedDegree(adj, n)
		}
		inter := make(map[[2]int]int64)
		for u := 0; u < len(nodes); u++ {
			for v, w := range adj[nodes[u]] {
				j := index[v]
				a, b := community[u], community[j]
				if a == b {
					continue
				}
				if a > b {
					a, b = b, a
				}
				inter[[2]int{a, b}] += w
			}
		}

		bestScore, bestA, bestB := int64(-1), -1, -1
		for pair, count := range inter {
			score := 2*m*count - deg[pair[0]]*deg[pair[1]]
			if score <= 0 {
				continue
			}
			if bestA == -1 ||
				score > bestScore ||
				(score == bestScore && (pair[0] < bestA || (pair[0] == bestA && pair[1] < bestB))) {
				bestScore, bestA, bestB = score, pair[0], pair[1]
			}
		}
		if bestA == -1 {
			break
		}

		for u := range nodes {
			if community[u] == bestB {
				community[u] = bestA
			}
		}
	}

	labels := make(map[string]string, len(nodes))
	for i, n := range nodes {
		labels[n] = nodes[community[i]]
	}
	return labels
}

// weightedDegree returns the sum of a node's incident edge weights.
func weightedDegree(adj networkGraph, node string) int64 {
	var d int64
	for _, w := range adj[node] {
		d += w
	}
	return d
}

// connectedComponents assigns every node the id of the smallest node in its
// connected component. It is the linear-time fallback used when the
// coupling graph is too large for the super-linear modularity pass.
func connectedComponents(adj networkGraph) map[string]string {
	labels := make(map[string]string, len(adj))
	visited := make(map[string]bool, len(adj))
	nodes := make([]string, 0, len(adj))
	for node := range adj {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	for _, start := range nodes {
		if visited[start] {
			continue
		}
		seed := start
		component := []string{start}
		visited[start] = true
		for i := 0; i < len(component); i++ {
			cur := component[i]
			if cur < seed {
				seed = cur
			}
			for nb := range adj[cur] {
				if visited[nb] {
					continue
				}
				visited[nb] = true
				component = append(component, nb)
			}
		}
		for _, n := range component {
			labels[n] = seed
		}
	}
	return labels
}

// commonDocRoot returns the longest directory prefix shared by every
// document path, so absolute scanner paths collapse onto the project root
// and relative fixture paths onto their common ancestor (possibly "").
func commonDocRoot(documents map[string]*ir.Document) string {
	root := ""
	first := true
	for path := range documents {
		dir := filepath.Dir(path)
		if first {
			root = dir
			first = false
			continue
		}
		root = commonDirPrefix(root, dir)
	}
	return root
}

// commonDocDirPrefix returns the longest directory prefix shared by a set
// of member paths, used to name a network cluster after the subsystem it
// represents. Falls back to the first member's directory when members
// share no ancestor.
func commonDocDirPrefix(members []string) string {
	prefix := ""
	for i, m := range members {
		dir := filepath.Dir(m)
		if i == 0 {
			prefix = dir
			continue
		}
		prefix = commonDirPrefix(prefix, dir)
	}
	return prefix
}

// networkClusterName names a network community after the directory holding
// the most members, so a coupling group that spans several sibling trees
// keeps a specific label instead of collapsing onto a shallow shared
// ancestor (every community under "src" used to be named "src"), and one
// that includes a stray root-level file is not renamed after that file
// (its "." directory used to zero the shared prefix). When no directory
// holds a majority (thinly scattered members), it falls back to the shared
// ancestor, then to "" so the caller falls back to the community id.
func networkClusterName(members []string) string {
	counts := make(map[string]int, len(members))
	for _, m := range members {
		counts[filepath.Dir(m)]++
	}

	modal, best := "", 0
	for dir, c := range counts {
		if best == 0 || c > best ||
			(c == best && (len(dir) > len(modal) || (len(dir) == len(modal) && dir < modal))) {
			modal, best = dir, c
		}
	}
	if best >= 2 && modal != "" && modal != "." {
		return modal
	}
	if prefix := commonDocDirPrefix(members); prefix != "" && prefix != "." {
		return prefix
	}
	return ""
}
