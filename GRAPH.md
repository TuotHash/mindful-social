# Argument Graph — Developer Reference

A quick map of every file, function, and constant involved in the `/graph` page so changes are fast to locate and reason about.

---

## File map

| File | Role |
|---|---|
| `internal/server/handlers_graph.go` | HTTP handlers, filter parsing, data assembly |
| `internal/views/graph.templ` | HTML structure, data attributes, HTMX wiring |
| `internal/views/suggest.templ` | Suggest-dropdown fragments (author, group, tag, nodes) |
| `queries/graph.sql` | Raw SQL for the five graph queries |
| `internal/db/graph.sql.go` | sqlc-generated Go wrappers (do not edit) |
| `static/app.js` | Force simulation, rendering, interaction (one closure) |
| `static/app.css` | Graph layout and visual styles (search `.argument-graph-`) |

---

## Backend data flow

```
GET /graph
  → handleArgumentGraph          handlers_graph.go:110
      → loadArgumentGraph        handlers_graph.go:164
          if filters active:
            → searchArgumentGraph  handlers_graph.go:201
                SearchNodes (tsvector + trigram)
                ListArgumentGraphSeeds (author/group/tags/since/sourced/visibility)
                ListArgumentGraphNeighborhood (recursive CTE, up to 5 hops)
                ListArgumentGraphEdgesForNodeIDs
          else (no filters):
            ListArgumentGraphNodesForViewer  (cap: 120 nodes)
            ListArgumentGraphEdgesForViewer  (cap: 320 edges)
      → views.ArgumentGraph(viewer, data, filters)

GET /graph/data   (same logic, returns JSON instead of HTML)
  → handleArgumentGraphData      handlers_graph.go:121
```

**Caps** — `argumentGraphNodeLimit = 120`, `argumentGraphEdgeLimit = 320`, `argumentGraphSearchMaxHops = 5` (all in `handlers_graph.go:17-31`).

**Filters parsed** in `parseGraphFilters` (`handlers_graph.go:71`): `q`, `author`, `group`, `tag[]`, `since` (7d/30d/90d), `sourced` (yes/no), `visibility`. All filters are AND-combined server-side; unknown values are silently dropped.

---

## SQL queries (`queries/graph.sql`)

| Query | Purpose |
|---|---|
| `ListArgumentGraphNodesForViewer` | Full unfiltered load (no filters active) |
| `ListArgumentGraphEdgesForViewer` | Edges for the full load, scoped to the node list |
| `ListArgumentGraphSeeds` | Returns node IDs matching non-text filters |
| `ListArgumentGraphNeighborhood` | Recursive CTE expanding seed IDs outward by hop count |
| `ListArgumentGraphEdgesForNodeIDs` | Edges connecting a given set of node IDs |

`SearchNodes` (in `queries/nodes.sql`) handles the free-text path; results are intersected with `ListArgumentGraphSeeds` when both a text query and other filters are active.

---

## Template (`internal/views/graph.templ`)

The page root is `<div data-argument-graph data-argument-graph-endpoint="/graph/data">`.
All JS selectors use `data-*` attributes — never class names.

Key data attributes:
```
data-argument-graph-data     JSON blob of initial nodes/edges (inline <script type="application/json">)
data-argument-graph-filters  JSON blob of active filters (same technique)
data-graph-svg               The <svg> element
data-graph-search            Free-text search input (name="q")
data-graph-author            Author filter input (name="q" → /users/suggest)
data-graph-group             Group filter input  (name="q" → /groups/suggest)
data-graph-tag-input         Tag filter input    (name="q" → /tags/suggest)
data-graph-type              Node-type checkboxes
data-graph-kind              Edge-kind checkboxes
data-graph-since             Since toggle buttons
data-graph-sourced           Sourced toggle buttons
data-graph-visibility        Visibility toggle buttons
data-graph-depth             Depth range slider
data-graph-depth-value       Depth display span
data-graph-visible-count     "N nodes visible" counter
data-graph-zoom              Zoom buttons (values: in / out / reset)
data-graph-reshuffle         Shuffle button (clears sim cache, re-runs)
data-graph-title             Inspector heading
data-graph-meta              Inspector metadata row
data-graph-open              Inspector "Open node" link
data-graph-author-pin        Inspector "Filter by author" button
data-graph-author-pin-label  Label inside the pin button
```

**HTMX suggests** — author, group, tag, and node-search inputs all fire `hx-get` on input with a 200 ms debounce. Each input must have `name="q"` so HTMX sends `?q=<value>`. The handlers all read `?q=` (`handleUsersSuggest`, `handleGroupsSuggest`, `handleTagsSuggest`, `handleGraphNodesSuggest` in `handlers_search.go`). Clicking a suggestion uses `data-fill-target` / `data-fill-value` which the global click handler in `app.js` wires up.

---

## Frontend JS (`static/app.js`)

Everything lives in `initArgumentGraphs` (line 569), called once per `[data-argument-graph]` element. It is a self-contained closure; variables do not leak.

### Data normalisation

`normalizeGraphData(next)` — called on load and on every server refresh. Populates:
- `data` — raw `{nodes, edges}` object
- `nodesByID` — `{id → node}` map
- `edges` — filtered to edges where both endpoints exist; `relates_to` is renamed `related`
- `adjacency` — `{id → {id → true}}` for O(1) neighbour lookup

### Filter pipeline

```
filteredNodes()
  reads: activeTypes(), activeKinds(), currentQuery(), currentAuthor(),
         currentDepth(), sinceValue, sourcedValue, visibilityValue
  returns: subset of data.nodes

  if no server filter active → filter by type checkbox only
  else:
    seed = nodes matching query + author (client-side approximation)
         OR nodes with match:true flag (from server response, once settled)
    BFS out from seeds using liveAdjacency up to currentDepth() hops
    only crosses edges of checked kinds (kindOK map)
```

`render()` calls `filteredNodes()` on every repaint. It does NOT re-fetch from the server; the server is only called by `fetchServerGraph()` (debounced at 220 ms) when a server-side filter changes.

### Force simulation

**Entry point** — `layout(nodes)` (line 1081)

`layout()` computes `simKey = sorted node IDs joined by "|"`. If the key changed (different visible node set), it calls `runForceSimulation(nodes, simEdges)` and caches the result in `simPositions`. If the key is unchanged, it restores positions from cache — no re-simulation on node selection, edge-kind toggles, etc.

`simPositions = {}` and `simKey = ""` are cleared by the shuffle button, forcing a fresh run.

**`runForceSimulation(nodes, simEdges)`** — 220 iterations, alpha decays 1 → 0.001.

**Initialisation (before the loop):**
1. Nodes already in `simPositions` get their cached x/y (warm start).
2. Uncached nodes are grouped into connected components (BFS on `simEdges`).
3. Each component gets its own starting sector. The largest component seeds at the canvas centre; smaller ones and isolated nodes ring it at radius `0.38 × min(W, H)`.
   — This prevents isolated nodes from starting inside unrelated clusters.

**Forces (each iteration, scaled by alpha):**

| # | Force | Formula | Purpose |
|---|---|---|---|
| ① | Charge | `1/d²` at d<100, `1/d` at d≥100 | Short-range inverse-square prevents close stacking; long-range linear keeps distant nodes apart. Continuous at d=100. `k_short=20000`, `k_long=200`, min distance cap 30 px. |
| ② | Link | Spring toward kind's rest distance | Pulls connected nodes to their target gap. Strength and distance vary per edge kind (see `KIND_LINK`). |
| ③ | Kind affinity | Pull toward centroid of same-kind participants | Nodes sharing an edge kind drift toward each other's centre of mass, forming soft semantic clusters. Strength 0.025. Skipped for groups of < 3. |
| ④ | Centering | `0.015 × (centre − pos)` | Keeps the graph near the canvas centre so nodes don't drift off-screen. |
| ⑤ | Type gravity | Pull toward kind's natural x column | `topic→x=220, view→x=600, finding→x=980`. Biases the layout to match the topic→view→finding argument flow, reducing edge crossings. Strength 0.006 (intentionally weak). |

Velocity damping: `0.65` per step.  
Canvas clamp: `[60, W−60]` × `[60, H−60]`.

**`KIND_LINK` constants** (tuning knobs, `app.js` near line 896):
```
supports:    distance 200, strength 0.28
opposes:     distance 255, strength 0.25
refines:     distance 160, strength 0.50
cites:       distance 195, strength 0.40
related:     distance 230, strength 0.28
comments_on: distance 155, strength 0.45
```
Increasing `distance` spreads connected nodes further apart. Increasing `strength` makes the spring pull harder toward that distance.

### Rendering

`render()` flow:
1. Read SVG dimensions → `viewHeight = 1200 × (renderedH / renderedW)` — keeps viewBox matching the visual box with no letterboxing.
2. `filteredNodes()` → `layout(nodes)` → set viewBox `0 0 1200 {viewHeight}`.
3. Rebuild SVG from scratch: `<defs>` with arrowhead markers, edge `<path>` layer, node `<g>` layer, viewport `<g>` with pan/zoom transform.

**Edges** — `edgePath(from, to)` draws a straight line from the source circle's edge to the target circle's edge along the actual angle between nodes. `nodeRadius(node)` is used to offset the endpoints:  topic=29, view=24, comment=14, other=20. Marker `refX="10"` places the arrowhead tip exactly at the path endpoint (target circle boundary).

**Markers** — defined for: `supports`, `opposes`, `related`, `refines`, `cites`, `comments_on`. Add a new kind here if the schema ever gains one.

**Dimming** — when a node is selected, unrelated nodes and edges get class `is-dim` (CSS lowers opacity). Adjacency is looked up from `adjacency[selectedID]`.

### Pan / zoom

- `zoom` (scalar) and `pan` (`{x, y}`) are applied via a CSS matrix transform on the viewport `<g>`.
- Pointer drag on the background pans.
- Wheel event zooms toward the cursor position (`zoomAt()`).
- Zoom clamped to `[0.55, 2.4]`.
- `svgPointFromClient()` converts screen coordinates to SVG viewBox coordinates for accurate zoom anchor.

---

## CSS (`static/app.css`, around line 1650)

| Selector | What it controls |
|---|---|
| `.argument-graph-workspace` | Outer grid: stage (flex-1) + inspector (300 px) |
| `.argument-graph-stage` | Dotted grid background, `overflow: hidden` |
| `.argument-graph-svg` | `width:100%; height:min(72vh,740px)` — controls canvas size |
| `.argument-graph-node` | Node circle + label styles per type |
| `.argument-graph-edge` | Edge stroke colour per kind |
| `.argument-graph-arrow.edge-*` | Arrowhead fill colour per kind |

---

## Common tasks

**Tune force layout** — edit `KIND_LINK` distances/strengths, charge constants (`20000` / `200`), or centering/type-gravity multipliers (`0.015` / `0.006`) directly in `app.js`. No rebuild needed — just reload the page.

**Add a new edge kind** — add it to `KIND_LINK`, add it to the `appendMarkers` list, add CSS for `.argument-graph-arrow.edge-<kind>` and `.argument-graph-edge.edge-<kind>`.

**Resize the canvas** — change `height: min(72vh, 740px)` on `.argument-graph-svg` in `app.css`. The JS reads the rendered size dynamically; no JS change needed.

**Add a new server-side filter** — add the query param to `parseGraphFilters` + `graphFiltersForView` in `handlers_graph.go`, add it to `snapshotFilters()` + `filtersEqual()` + `graphDataURL()` + `fetchServerGraph()` in `app.js`, and add the corresponding `server*` mirror variable alongside `serverQuery`.

**Change suggest endpoints** — all suggest inputs must have `name="q"` for HTMX to send `?q=<value>`. The handlers (`handleUsersSuggest`, `handleGroupsSuggest`, `handleTagsSuggest`, `handleGraphNodesSuggest`) all read `r.URL.Query().Get("q")`.

**Force a re-simulation** — clear `simPositions = {}` and `simKey = ""` in JS (what the shuffle button does). The next `render()` call will re-run from scratch.
