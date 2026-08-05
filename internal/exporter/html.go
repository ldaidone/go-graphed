package exporter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// visualizeMinWeight drops near-noise links from the visualizer so the
// rendered graph shows the coupling backbone instead of a hairball of
// low-confidence keyword matches. It mirrors the traversal threshold the
// analyzer uses for network clustering and centrality scoring.
const visualizeMinWeight = 0.5

// htmlData is the compact, document-level projection of the graph that the
// self-contained visualizer renders. Entity anchors collapse onto their
// owning file and package nodes expand onto every member file, matching the
// projection used for clustering and centrality -- the HTML view therefore
// agrees with the terminal metrics about what "connected" means.
type htmlData struct {
	Nodes []htmlNode `json:"nodes"`
	Links []htmlLink `json:"links"`
	Stats htmlStats  `json:"stats"`
}

type htmlNode struct {
	ID       string  `json:"id"`
	Format   string  `json:"format"`
	Degree   int     `json:"degree"`
	Weighted float64 `json:"weighted"`
	PageRank float64 `json:"pagerank"`
	IsHub    bool    `json:"isHub"`
}

// htmlLink indexes into htmlData.Nodes so the payload stays compact even for
// graphs with thousands of documents.
type htmlLink struct {
	Source     int     `json:"source"`
	Target     int     `json:"target"`
	Type       string  `json:"type"`
	SourceType string  `json:"sourceType"`
	Weight     float64 `json:"weight"`
}

type htmlStats struct {
	Documents int      `json:"documents"`
	Links     int      `json:"links"`
	Hubs      int      `json:"hubs"`
	Formats   []string `json:"formats"`
	Root      string   `json:"root"`
}

// HTML renders the graph as a self-contained, dependency-free HTML file:
// a single document with inline CSS and vanilla-JS force-directed layout.
// No assets are fetched at view time, so the file works offline and can be
// committed next to graph.json. Creating the output directory mirrors the
// guarantee JSON() gives callers.
func HTML(graph ir.Graph, output string) error {
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	payload, err := json.MarshalIndent(buildHTMLData(&graph), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal visualizer data: %w", err)
	}
	// "</script>" inside the payload would terminate the inline script;
	// escaping the slash is JSON-safe and parse-equivalent in JS.
	escaped := strings.ReplaceAll(string(payload), "</", "<\\/")

	html := strings.Replace(htmlVisualizerTemplate, htmlDataPlaceholder, escaped, 1)
	if err := os.WriteFile(output, []byte(html), 0644); err != nil {
		return fmt.Errorf("failed to write visualizer file to %s: %w", output, err)
	}
	return nil
}

// buildHTMLData projects the IR graph onto the document-level payload the
// visualizer renders. Links whose weight falls below the visualization
// threshold, and edges to unindexed files, are dropped; parallel edges
// between the same pair of documents collapse onto the highest-weight one.
// Output is deterministic (nodes by path, links by endpoint index) so two
// builds of the same snapshot produce byte-identical files.
func buildHTMLData(graph *ir.Graph) htmlData {
	paths := make([]string, 0, len(graph.Documents))
	for p := range graph.Documents {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	index := make(map[string]int, len(paths))
	nodes := make([]htmlNode, 0, len(paths))
	formatSet := make(map[string]bool)
	hubCount := 0
	for i, p := range paths {
		doc := graph.Documents[p]
		formatSet[doc.Format] = true
		node := htmlNode{ID: p, Format: doc.Format}
		if dm, ok := graph.Metrics.Documents[p]; ok {
			node.Degree = dm.Degree
			node.Weighted = dm.WeightedDegree
			node.PageRank = dm.PageRank
			node.IsHub = dm.IsHub
		} else if doc.Metadata[ir.MetadataHubFlag] == "true" {
			node.IsHub = true
		}
		if node.IsHub {
			hubCount++
		}
		index[p] = i
		nodes = append(nodes, node)
	}

	// Aggregate parallel document-level edges onto the heaviest one.
	type edgeKey struct{ a, b int }
	best := make(map[edgeKey]htmlLink)
	for _, link := range graph.Links {
		if link.Weight < visualizeMinWeight {
			continue
		}
		srcs := filterIndexedDocs(graph, docPaths(graph, link.SourceID))
		tgts := filterIndexedDocs(graph, docPaths(graph, link.TargetID))
		for _, s := range srcs {
			for _, t := range tgts {
				a, b := index[s], index[t]
				if a == b {
					continue
				}
				key := edgeKey{a, b}
				cur, ok := best[key]
				if !ok || link.Weight > cur.Weight {
					best[key] = htmlLink{
						Source:     a,
						Target:     b,
						Type:       link.Type,
						SourceType: ir.NormalizeLinkSource(link.SourceType),
						Weight:     link.Weight,
					}
				}
			}
		}
	}

	links := make([]htmlLink, 0, len(best))
	for _, l := range best {
		links = append(links, l)
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Source != links[j].Source {
			return links[i].Source < links[j].Source
		}
		return links[i].Target < links[j].Target
	})

	formats := make([]string, 0, len(formatSet))
	for f := range formatSet {
		formats = append(formats, f)
	}
	sort.Strings(formats)

	return htmlData{
		Nodes: nodes,
		Links: links,
		Stats: htmlStats{
			Documents: len(nodes),
			Links:     len(links),
			Hubs:      hubCount,
			Formats:   formats,
			Root:      commonRoot(paths),
		},
	}
}

// docPaths resolves a graph node ID onto the document paths it represents:
// entity IDs collapse to their owning file and package nodes expand to every
// indexed member file. Mirrors the analyzer's clusterDocPaths so the
// visualizer, clustering, and centrality agree on the document projection.
func docPaths(graph *ir.Graph, id string) []string {
	if strings.HasPrefix(id, ir.PackageNodePrefix) {
		dir := strings.TrimPrefix(id, ir.PackageNodePrefix)
		if pkg, ok := graph.Packages[dir]; ok {
			return pkg.Files
		}
		return nil
	}
	return []string{strings.SplitN(id, "#", 2)[0]}
}

// filterIndexedDocs keeps only the document paths that exist in the index,
// so links referencing unindexed files cannot create phantom visualizer
// nodes.
func filterIndexedDocs(graph *ir.Graph, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := graph.Documents[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

// commonRoot returns the longest common directory prefix across the document
// paths (in forward-slash form), used as the visualizer's title. It returns
// an empty string when there are no documents or no common prefix.
func commonRoot(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	split := func(p string) []string {
		return strings.Split(filepath.ToSlash(p), "/")
	}
	parts := split(paths[0])
	for _, p := range paths[1:] {
		other := split(p)
		i := 0
		for i < len(parts) && i < len(other) && parts[i] == other[i] {
			i++
		}
		parts = parts[:i]
		if len(parts) == 0 {
			break
		}
	}
	return strings.Join(parts, "/")
}

// htmlDataPlaceholder is replaced with the JSON payload when HTML renders
// the template. Kept on its own line so tests can extract the payload.
const htmlDataPlaceholder = "__GRAPH_DATA__"

// htmlVisualizerTemplate is the self-contained page. It intentionally pulls
// no external assets: the force layout, pan/zoom, filtering, and detail
// panel are all hand-rolled in vanilla JS so the artifact works offline.
const htmlVisualizerTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Knowledge Graph Visualizer</title>
<style>
  :root {
    --bg: #0d1117;
    --panel: #161b22;
    --border: #30363d;
    --text: #e6edf3;
    --muted: #8b949e;
    --accent: #58a6ff;
    --hub: #ffd700;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; height: 100%; overflow: hidden; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background: var(--bg); color: var(--text); }
  header { height: 56px; display: flex; align-items: center; gap: 16px; padding: 0 16px; border-bottom: 1px solid var(--border); background: var(--panel); }
  header h1 { font-size: 15px; margin: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  header .stats { display: flex; gap: 14px; margin-left: auto; color: var(--muted); font-size: 12px; white-space: nowrap; }
  header .stats b { color: var(--text); font-weight: 600; }
  #controls { height: 44px; display: flex; align-items: center; gap: 12px; padding: 0 16px; border-bottom: 1px solid var(--border); background: var(--panel); font-size: 12px; }
  #controls input[type=text] { background: var(--bg); border: 1px solid var(--border); color: var(--text); border-radius: 6px; padding: 5px 10px; width: 260px; font-size: 12px; }
  #controls input[type=text]:focus, #controls select:focus { outline: none; border-color: var(--accent); }
  #controls select { background: var(--bg); border: 1px solid var(--border); color: var(--text); border-radius: 6px; padding: 5px 8px; font-size: 12px; }
  #controls label { display: flex; align-items: center; gap: 5px; color: var(--muted); cursor: pointer; }
  #controls button { background: var(--bg); border: 1px solid var(--border); color: var(--text); border-radius: 6px; padding: 5px 12px; cursor: pointer; font-size: 12px; }
  #controls button:hover { border-color: var(--accent); }
  #controls .hint { margin-left: auto; color: var(--muted); }
  #viz { display: block; width: 100%; height: calc(100% - 100px); cursor: grab; }
  #viz:active { cursor: grabbing; }
  #tooltip { position: fixed; display: none; max-width: 420px; padding: 8px 10px; background: rgba(22,27,34,0.95); border: 1px solid var(--border); border-radius: 6px; font-size: 12px; pointer-events: none; z-index: 20; box-shadow: 0 4px 16px rgba(0,0,0,0.4); }
  #tooltip .hub { color: var(--hub); font-weight: 700; }
  #panel { position: fixed; top: 100px; right: 0; bottom: 0; width: 340px; overflow: auto; background: var(--panel); border-left: 1px solid var(--border); padding: 16px; font-size: 12px; z-index: 15; display: none; }
  #panel h3 { margin: 0 0 10px; font-size: 13px; word-break: break-all; }
  #panel h4 { margin: 14px 0 6px; font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.05em; }
  #panel dl { display: grid; grid-template-columns: 110px 1fr; gap: 4px 8px; margin: 0; }
  #panel dt { color: var(--muted); }
  #panel dd { margin: 0; word-break: break-all; }
  #panel ul { margin: 0; padding-left: 16px; }
  #panel li { margin: 2px 0; word-break: break-all; }
  #panel li.hub { color: var(--hub); }
  #panel .close { position: sticky; top: 0; float: right; background: none; border: none; color: var(--muted); font-size: 16px; cursor: pointer; line-height: 1; }
  #legend { position: fixed; left: 16px; bottom: 12px; background: rgba(22,27,34,0.9); border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; font-size: 11px; color: var(--muted); z-index: 10; }
  #legend .row { margin: 2px 0; }
  #legend .swatch { display: inline-block; width: 9px; height: 9px; border-radius: 50%; margin-right: 6px; vertical-align: middle; }
  #legend .hub-note { margin-top: 6px; color: var(--hub); }
  #legend .line-note { color: var(--muted); }
  #empty { display: none; position: fixed; inset: 0; align-items: center; justify-content: center; color: var(--muted); font-size: 14px; }
</style>
</head>
<body>
<header>
  <h1>Knowledge Graph &mdash; <span id="root">&hellip;</span></h1>
  <div class="stats">
    <span>Documents <b id="stat-docs">0</b></span>
    <span>Links <b id="stat-links">0</b></span>
    <span>Hubs <b id="stat-hubs">0</b></span>
  </div>
</header>
<div id="controls">
  <input type="text" id="search" placeholder="Filter documents&hellip;">
  <select id="format"><option value="all">all formats</option></select>
  <label><input type="checkbox" id="hubs"> hubs only</label>
  <label><input type="checkbox" id="inferred"> hide inferred</label>
  <button id="relayout" title="Re-run the force layout">relayout</button>
  <span class="hint">drag to move &middot; scroll to zoom &middot; click a node for details</span>
</div>
<canvas id="viz"></canvas>
<div id="empty">No documents in this graph snapshot. Rebuild with kg build .</div>
<div id="tooltip"></div>
<div id="panel"><button class="close" id="close-panel" title="close">&times;</button><div id="panel-body"></div></div>
<div id="legend"></div>
<script>
  var data = __GRAPH_DATA__;

  var canvas = document.getElementById('viz');
  var ctx = canvas.getContext('2d');
  var tooltip = document.getElementById('tooltip');
  var panel = document.getElementById('panel');
  var panelBody = document.getElementById('panel-body');

  var W = 0, H = 0;
  function resize() {
    W = canvas.width = window.innerWidth;
    H = canvas.height = window.innerHeight;
  }
  resize();
  window.addEventListener('resize', function () { resize(); draw(); });

  var PALETTE = {
    golang: '#00c8ff', javascript: '#f0db4f', typescript: '#3178c6',
    tsx: '#3178c6', markdown: '#5dbcd2', json: '#8bc34a', yaml: '#cddc39',
    toml: '#a1887f', dockerfile: '#2496ed', make: '#e5544d', pdf: '#f44336',
    spreadsheet: '#4caf50', unstructured: '#8d99ae'
  };
  function colorOf(format) { return PALETTE[format] || '#b0bec5'; }

  function radius(n) {
    var base = 3 + Math.sqrt(Math.max(n.degree, 0)) * 1.6;
    return Math.min(base, 20) + (n.isHub ? 2 : 0);
  }

  var nodes = [];
  var neighbors = [];
  data.nodes.forEach(function (n, i) {
    nodes.push({
      id: n.id, format: n.format, degree: n.degree, weighted: n.weighted,
      pagerank: n.pagerank, isHub: n.isHub, i: i,
      x: 0, y: 0, vx: 0, vy: 0
    });
    neighbors.push([]);
  });

  var links = [];
  data.links.forEach(function (l) {
    var a = nodes[l.source], b = nodes[l.target];
    if (!a || !b) return;
    links.push({ source: a, target: b, type: l.type, sourceType: l.sourceType, weight: l.weight });
    neighbors[a.i].push(b);
    neighbors[b.i].push(a);
  });

  // ---- force simulation ----
  var TICK = 0.04, REPULSION = 2400, SPRING = 0.02, REST = 70, CENTER = 0.004;

  function seed() {
    var n = nodes.length;
    nodes.forEach(function (node, i) {
      var a = (i / Math.max(n, 1)) * Math.PI * 2;
      var r = Math.min(W, H) * 0.40;
      node.x = W / 2 + Math.cos(a) * r;
      node.y = H / 2 + Math.sin(a) * r;
      node.vx = 0; node.vy = 0;
    });
  }

  function kinetic() {
    var e = 0, i;
    for (i = 0; i < nodes.length; i++) e += nodes[i].vx * nodes[i].vx + nodes[i].vy * nodes[i].vy;
    return e;
  }

  function step() {
    var i, j, n = nodes.length;
    for (i = 0; i < n; i++) {
      var a = nodes[i];
      a.vx *= 0.85; a.vy *= 0.85;
      a.vx += (W / 2 - a.x) * CENTER; a.vy += (H / 2 - a.y) * CENTER;
    }
    for (i = 0; i < n; i++) {
      var a = nodes[i];
      for (j = i + 1; j < n; j++) {
        var b = nodes[j];
        var dx = a.x - b.x, dy = a.y - b.y;
        var d2 = dx * dx + dy * dy;
        if (d2 < 1) d2 = 1;
        var d = Math.sqrt(d2);
        var f = REPULSION / d2;
        var fx = dx / d * f, fy = dy / d * f;
        a.vx += fx; a.vy += fy; b.vx -= fx; b.vy -= fy;
      }
    }
    links.forEach(function (l) {
      var a = l.source, b = l.target;
      var dx = b.x - a.x, dy = b.y - a.y;
      var d = Math.sqrt(dx * dx + dy * dy) || 1;
      var f = SPRING * (d - REST) * l.weight;
      var fx = dx / d * f, fy = dy / d * f;
      a.vx += fx; a.vy += fy; b.vx -= fx; b.vy -= fy;
    });
    for (i = 0; i < n; i++) {
      var c = nodes[i];
      c.x += c.vx * TICK; c.y += c.vy * TICK;
    }
  }

  function warmup() {
    var t;
    for (t = 0; t < 600; t++) {
      var e = kinetic();
      step();
      if (t > 20 && e < 0.5) break;
    }
    draw();
  }

  // ---- viewport ----
  var scale = 1, ox = 0, oy = 0;
  function inv(x, y) { return { x: (x - ox) / scale, y: (y - oy) / scale }; }

  // ---- interaction state ----
  var filters = { format: 'all', hubsOnly: false, hideInferred: false, query: '' };
  var hover = null, selected = null;
  var dragging = null, panning = false, panStart = null, dragOffset = { x: 0, y: 0 };

  function matchFilters(n) {
    if (filters.format !== 'all' && n.format !== filters.format) return false;
    if (filters.hubsOnly && !n.isHub) return false;
    if (filters.query && n.id.indexOf(filters.query) === -1) return false;
    return true;
  }

  function edgeMatch(l) {
    if (filters.hideInferred && l.sourceType === 'inferred') return false;
    if (!matchFilters(l.source) || !matchFilters(l.target)) return false;
    if (filters.hubsOnly && !(l.source.isHub && l.target.isHub)) return false;
    return true;
  }

  function hitTest(x, y) {
    var best = null, bestD = Infinity, i;
    for (i = 0; i < nodes.length; i++) {
      var n = nodes[i];
      if (!matchFilters(n)) continue;
      var dx = n.x - x, dy = n.y - y;
      var d = Math.sqrt(dx * dx + dy * dy);
      var r = radius(n) + 3;
      if (d <= r && d < bestD) { best = n; bestD = d; }
    }
    return best;
  }

  function draw() {
    ctx.clearRect(0, 0, W, H);
    ctx.save();
    ctx.translate(ox, oy);
    ctx.scale(scale, scale);

    var i;
    links.forEach(function (l) {
      if (!edgeMatch(l)) return;
      var active = (hover && (l.source === hover || l.target === hover)) || (selected && (l.source === selected || l.target === selected));
      if (hover && !active) return;
      var a = 0.15 + 0.55 * l.weight;
      if (l.sourceType === 'inferred') ctx.strokeStyle = 'rgba(255,152,0,' + a + ')';
      else ctx.strokeStyle = 'rgba(150,180,200,' + a + ')';
      if (active) ctx.strokeStyle = 'rgba(255,255,255,0.9)';
      ctx.lineWidth = 1 / scale;
      ctx.beginPath();
      ctx.moveTo(l.source.x, l.source.y);
      ctx.lineTo(l.target.x, l.target.y);
      ctx.stroke();
    });

    var nb = hover ? neighbors[hover.i] : null;
    nodes.forEach(function (n) {
      if (!matchFilters(n)) return;
      var dim = false;
      if (hover && n !== hover && nb) {
        var linked = false, k;
        for (k = 0; k < nb.length; k++) { if (nb[k] === n) { linked = true; break; } }
        if (!linked && !n.isHub) dim = true;
      }
      if (selected && n !== selected) {
        var snb = neighbors[selected.i], sl = false, k2;
        for (k2 = 0; k2 < snb.length; k2++) { if (snb[k2] === n) { sl = true; break; } }
        if (!sl) dim = true;
      }
      var r = radius(n);
      if (dim) ctx.globalAlpha = 0.22;
      if (n.isHub) {
        ctx.beginPath();
        ctx.arc(n.x, n.y, r + 3, 0, Math.PI * 2);
        ctx.fillStyle = 'rgba(255,215,0,0.85)';
        ctx.fill();
      }
      ctx.beginPath();
      ctx.arc(n.x, n.y, r, 0, Math.PI * 2);
      ctx.fillStyle = colorOf(n.format);
      ctx.fill();
      if (n === hover || n === selected) {
        ctx.strokeStyle = '#ffffff';
        ctx.lineWidth = 2 / scale;
        ctx.stroke();
      }
      ctx.globalAlpha = 1;

      if (n.isHub || n === hover || n === selected) {
        ctx.fillStyle = n.isHub ? '#ffe97a' : '#e6edf3';
        ctx.font = (n.isHub ? '600 ' : '') + (12 / scale) + 'px sans-serif';
        ctx.textAlign = 'center';
        var label = n.id;
        if (label.length > 40) label = label.slice(0, 37) + '…';
        ctx.fillText(label, n.x, n.y + r + 4 + 10 / scale);
      }
    });
    ctx.restore();
  }

  // ---- pointer interaction ----
  canvas.addEventListener('mousedown', function (e) {
    var p = inv(e.clientX, e.clientY);
    var n = hitTest(p.x, p.y);
    if (n) {
      dragging = n;
      dragOffset = { x: n.x - p.x, y: n.y - p.y };
    } else {
      panning = true;
      panStart = { x: e.clientX - ox, y: e.clientY - oy };
    }
  });

  canvas.addEventListener('mousemove', function (e) {
    if (dragging) {
      var p = inv(e.clientX, e.clientY);
      dragging.x = p.x + dragOffset.x;
      dragging.y = p.y + dragOffset.y;
      dragging.vx = 0; dragging.vy = 0;
      draw();
      return;
    }
    if (panning) {
      ox = e.clientX - panStart.x; oy = e.clientY - panStart.y;
      draw();
      return;
    }
    var q = inv(e.clientX, e.clientY);
    var n = hitTest(q.x, q.y);
    if (n !== hover) {
      hover = n;
      if (n) showTooltip(e.clientX, e.clientY, n); else hideTooltip();
      draw();
    } else if (n) {
      positionTooltip(e.clientX, e.clientY);
    }
  });

  canvas.addEventListener('mouseup', function (e) {
    if (dragging && hitTest(inv(e.clientX, e.clientY).x, inv(e.clientX, e.clientY).y) === dragging) {
      selected = dragging;
      showDetails(dragging);
      draw();
    }
    dragging = null; panning = false;
  });

  canvas.addEventListener('mouseleave', function () { hideTooltip(); hover = null; draw(); });

  canvas.addEventListener('wheel', function (e) {
    e.preventDefault();
    var f = e.deltaY < 0 ? 1.1 : 0.9;
    var mx = e.clientX, my = e.clientY;
    var px = (mx - ox) / scale, py = (my - oy) / scale;
    scale = Math.max(0.05, Math.min(10, scale * f));
    ox = mx - px * scale; oy = my - py * scale;
    draw();
  }, { passive: false });

  // ---- controls ----
  document.getElementById('search').addEventListener('input', function (e) {
    filters.query = e.target.value.trim().toLowerCase();
    draw();
  });
  document.getElementById('format').addEventListener('change', function (e) {
    filters.format = e.target.value;
    draw();
  });
  document.getElementById('hubs').addEventListener('change', function (e) {
    filters.hubsOnly = e.target.checked;
    draw();
  });
  document.getElementById('inferred').addEventListener('change', function (e) {
    filters.hideInferred = e.target.checked;
    draw();
  });
  document.getElementById('relayout').addEventListener('click', function () {
    seed(); warmup();
  });
  document.getElementById('close-panel').addEventListener('click', function () {
    selected = null;
    panel.style.display = 'none';
    draw();
  });
  window.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      selected = null;
      panel.style.display = 'none';
      draw();
    }
  });

  // ---- tooltip / panel ----
  function escapeHtml(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function showTooltip(x, y, n) {
    tooltip.innerHTML = '<b>' + escapeHtml(n.id) + '</b><br>' + escapeHtml(n.format) +
      ' &mdash; deg ' + n.degree + (n.isHub ? ' &mdash; <span class="hub">HUB (God Node)</span>' : '');
    positionTooltip(x, y);
    tooltip.style.display = 'block';
  }
  function positionTooltip(x, y) {
    tooltip.style.left = (x + 12) + 'px';
    tooltip.style.top = (y + 12) + 'px';
  }
  function hideTooltip() { tooltip.style.display = 'none'; }

  function showDetails(n) {
    var related = [];
    links.forEach(function (l) {
      if (l.source === n && related.indexOf(l.target) === -1) related.push(l.target);
      if (l.target === n && related.indexOf(l.source) === -1) related.push(l.source);
    });
    related.sort(function (a, b) { return b.degree - a.degree || (a.id < b.id ? -1 : 1); });
    var html = '<h3>' + escapeHtml(n.id) + '</h3>';
    html += '<dl>';
    html += '<dt>Format</dt><dd>' + escapeHtml(n.format) + '</dd>';
    html += '<dt>Degree</dt><dd>' + n.degree + '</dd>';
    html += '<dt>Weighted degree</dt><dd>' + n.weighted.toFixed(2) + '</dd>';
    html += '<dt>PageRank</dt><dd>' + n.pagerank.toFixed(5) + '</dd>';
    html += '<dt>Hub (God Node)</dt><dd>' + (n.isHub ? '<span class="hub">YES</span>' : 'no') + '</dd>';
    html += '</dl>';
    html += '<h4>Connected documents (' + related.length + ')</h4><ul>';
    var max = Math.min(related.length, 25);
    var i;
    for (i = 0; i < max; i++) {
      html += '<li class="' + (related[i].isHub ? 'hub' : '') + '">' + escapeHtml(related[i].id) + '</li>';
    }
    if (related.length > max) html += '<li>&hellip; ' + (related.length - max) + ' more</li>';
    html += '</ul>';
    panelBody.innerHTML = html;
    panel.style.display = 'block';
  }

  // ---- chrome: header, legend, format options ----
  function populateChrome() {
    var stats = data.stats;
    document.getElementById('root').textContent = stats.root || '(project root unknown)';
    document.getElementById('stat-docs').textContent = stats.documents;
    document.getElementById('stat-links').textContent = stats.links;
    document.getElementById('stat-hubs').textContent = stats.hubs;

    var formatSel = document.getElementById('format');
    stats.formats.forEach(function (f) {
      var o = document.createElement('option');
      o.value = f; o.textContent = f;
      formatSel.appendChild(o);
    });

    var legend = document.getElementById('legend');
    var html = '';
    stats.formats.forEach(function (f) {
      html += '<div class="row"><span class="swatch" style="background:' + colorOf(f) + '"></span>' + escapeHtml(f) + '</div>';
    });
    html += '<div class="hub-note"><span class="swatch" style="background:rgba(255,215,0,0.85)"></span>hub (God Node)</div>';
    html += '<div class="line-note"><span class="swatch" style="background:rgba(255,152,0,0.8)"></span>inferred edge</div>';
    legend.innerHTML = html;
  }

  if (data.nodes.length === 0) {
    canvas.style.display = 'none';
    document.getElementById('empty').style.display = 'flex';
    populateChrome();
  } else {
    seed();
    populateChrome();
    warmup();
  }
</script>
</body>
</html>
`
