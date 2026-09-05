package analyzer

import (
	"math"
	"sort"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// Global centrality ("God Node") metric tuning knobs. The values are simple
// and deterministic so two builds of the same snapshot always flag the same
// hubs.
const (
	// hubFraction is the share of indexed documents flagged as hubs, i.e.
	// the top composite (degree + PageRank) score. 5% keeps the report to
	// the true architectural touchpoints without flooding consumers.
	hubFraction = 0.05

	// hubMinDegree is the minimum number of distinct neighbors a document
	// needs before it can be flagged a hub. Requiring at least two edges
	// stops a lone pair of files from self-certifying as "God Nodes".
	hubMinDegree = 2

	// pagerankDamping is the standard PageRank damping factor: a random
	// walker teleports to a uniformly random document with probability
	// 1-damping instead of following an edge.
	pagerankDamping = 0.85

	// pagerankIterations bounds the power-method pass for pathological
	// graphs that converge slowly.
	pagerankIterations = 100

	// pagerankTolerance stops the power method early once the scores have
	// converged, so typical graphs finish far sooner than the iteration cap.
	pagerankTolerance = 1e-9
)

// annotateMetrics computes global centrality ("God Node") metrics over the
// document-level coupling graph and stores them on Graph.Metrics. Hub
// documents -- the top hubFraction by composite degree + PageRank score --
// also get an "is_hub" flag in their Metadata so graph.json consumers can
// prioritize core infrastructure without parsing the metrics table.
func annotateMetrics(graph *ir.Graph) {
	metrics := ir.Metrics{
		Documents: make(map[string]ir.DocumentMetrics, len(graph.Documents)),
	}
	// Every indexed document gets a row so consumers can look up any path
	// (isolated documents simply keep zero-valued scores).
	for path := range graph.Documents {
		metrics.Documents[path] = ir.DocumentMetrics{}
	}

	// The coupling graph reuses the same projection as network clustering:
	// entity anchors collapse onto their owning file and package nodes
	// expand onto every member file. Scoring and clustering therefore agree
	// on what "connected" means.
	if adj := networkAdjacency(graph); len(adj) > 0 {
		ranks := pagerank(adj)
		for node := range adj {
			dm := metrics.Documents[node]
			dm.Degree = len(adj[node])
			dm.WeightedDegree = float64(weightedDegree(adj, node))
			dm.PageRank = ranks[node]
			metrics.Documents[node] = dm
		}
	}

	metrics.HubCount = flagHubs(&metrics)

	// Persist the flag into each hub document's metadata so the exported
	// graph.json carries the literal "is_hub": "true" marker, independent
	// of the structured metrics table.
	for path, dm := range metrics.Documents {
		if !dm.IsHub {
			continue
		}
		doc, ok := graph.Documents[path]
		if !ok {
			continue
		}
		if doc.Metadata == nil {
			doc.Metadata = make(map[string]string)
		}
		doc.Metadata[ir.MetadataHubFlag] = "true"
	}

	graph.Metrics = metrics
}

// flagHubs marks the top hubFraction of indexed documents by a composite
// centrality score -- degree and PageRank, each normalized to [0,1] by its
// graph-wide maximum so the two signals contribute equally -- as hubs. Ties
// break on the document path, keeping the assignment deterministic. Returns
// the number of hubs flagged.
func flagHubs(metrics *ir.Metrics) int {
	if len(metrics.Documents) == 0 {
		return 0
	}

	maxDegree := 0
	maxRank := 0.0
	for _, dm := range metrics.Documents {
		if dm.Degree > maxDegree {
			maxDegree = dm.Degree
		}
		if dm.PageRank > maxRank {
			maxRank = dm.PageRank
		}
	}
	if maxDegree == 0 {
		return 0
	}

	type scored struct {
		path  string
		score float64
	}
	candidates := make([]scored, 0, len(metrics.Documents))
	for path, dm := range metrics.Documents {
		if dm.Degree < hubMinDegree {
			continue
		}
		score := float64(dm.Degree) / float64(maxDegree)
		if maxRank > 0 {
			score += dm.PageRank / maxRank
		}
		candidates = append(candidates, scored{path: path, score: score})
	}
	if len(candidates) == 0 {
		return 0
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].path < candidates[j].path
	})

	count := min(int(math.Ceil(float64(len(metrics.Documents))*hubFraction)), len(candidates))
	for _, c := range candidates[:count] {
		dm := metrics.Documents[c.path]
		dm.IsHub = true
		metrics.Documents[c.path] = dm
	}
	return count
}

// pagerank runs the power method over the weighted, undirected document
// coupling graph. Each document spreads a share of its rank to its
// neighbors in proportion to the edge weights; a damping term teleports the
// walker back to a uniformly random document. Because the graph is
// undirected, the converged scores are proportional to weighted degree,
// which makes PageRank a normalized global centrality that still exposes
// the nodes everything funnels through.
func pagerank(adj networkGraph) map[string]float64 {
	nodes := make([]string, 0, len(adj))
	for node := range adj {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	n := len(nodes)
	if n == 0 {
		return nil
	}
	index := make(map[string]int, n)
	for i, node := range nodes {
		index[node] = i
	}

	// Weighted out-degree: each node splits its rank across its incident
	// edges in proportion to their weight.
	out := make([]float64, n)
	for i, node := range nodes {
		out[i] = float64(weightedDegree(adj, node))
	}

	rank := make([]float64, n)
	teleport := (1 - pagerankDamping) / float64(n)
	for i := range rank {
		rank[i] = 1.0 / float64(n)
	}

	for range pagerankIterations {
		next := make([]float64, n)
		for i, node := range nodes {
			if out[i] == 0 {
				continue
			}
			share := rank[i] * pagerankDamping / out[i]
			for nb, w := range adj[node] {
				next[index[nb]] += share * float64(w)
			}
		}
		diff := 0.0
		for i := range next {
			next[i] += teleport
			diff += math.Abs(next[i] - rank[i])
		}
		rank = next
		if diff < pagerankTolerance {
			break
		}
	}

	ranks := make(map[string]float64, n)
	for i, node := range nodes {
		ranks[node] = rank[i]
	}
	return ranks
}
