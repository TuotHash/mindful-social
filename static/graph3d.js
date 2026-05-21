(function () {
  "use strict";

  var KIND_LINK = {
    supports:    { distance: 200, strength: 0.28 },
    opposes:     { distance: 255, strength: 0.25 },
    refines:     { distance: 160, strength: 0.50 },
    cites:       { distance: 195, strength: 0.40 },
    related:     { distance: 230, strength: 0.28 },
    comments_on: { distance: 155, strength: 0.45 },
  };

  var NODE_COLOR = {
    topic:   "#7c6fd4",
    view:    "#5a9fd4",
    finding: "#4dab8a",
    comment: "#8b8b8b",
  };

  var EDGE_COLOR = {
    supports:    "#4dab8a",
    opposes:     "#d45a5a",
    refines:     "#d4a85a",
    cites:       "#5a8fd4",
    related:     "#8b8b8b",
    comments_on: "#8b8b8b",
  };

  // Z-axis positions for type gravity — mirrors the 2D x-column bias.
  // Topics sit at the back, findings at the front.
  var TYPE_Z = { topic: -280, view: 0, finding: 280, comment: 0 };
  var TYPE_Z_STRENGTH = 0.006;

  var BG_COLOR = "#0d0d14";

  function dimHex(hex, factor) {
    var r = parseInt(hex.slice(1, 3), 16);
    var g = parseInt(hex.slice(3, 5), 16);
    var b = parseInt(hex.slice(5, 7), 16);
    var br = 13, bg = 13, bb = 20; // BG_COLOR
    r = Math.round(r * factor + br * (1 - factor));
    g = Math.round(g * factor + bg * (1 - factor));
    b = Math.round(b * factor + bb * (1 - factor));
    return "#" + r.toString(16).padStart(2, "0") + g.toString(16).padStart(2, "0") + b.toString(16).padStart(2, "0");
  }

  function initArgumentGraph3D(graphEl) {
    if (graphEl.dataset.argumentGraph3dReady === "true") return;
    graphEl.dataset.argumentGraph3dReady = "true";

    var container = graphEl.querySelector("[data-graph3d-container]");
    if (!container || typeof ForceGraph3D === "undefined") return;

    // ── Initial data ──────────────────────────────────────────────────────────
    var dataEl    = graphEl.querySelector("[data-argument-graph-data]");
    var filtersEl = graphEl.querySelector("[data-argument-graph-filters]");

    var rawData = { nodes: [], edges: [] };
    try { rawData = JSON.parse(dataEl && dataEl.textContent || "{}") || rawData; } catch (e) {}

    var initialFilters = { query: "", author: "", group: "", tags: [], since: "", sourced: "", visibility: "" };
    if (filtersEl) {
      try {
        var pf = JSON.parse(filtersEl.textContent || "{}") || {};
        Object.keys(initialFilters).forEach(function (k) {
          if (pf[k] != null) initialFilters[k] = pf[k];
        });
        if (!Array.isArray(initialFilters.tags)) initialFilters.tags = [];
      } catch (e) {}
    }

    // ── Normalised graph state ────────────────────────────────────────────────
    var data      = { nodes: [], edges: [] };
    var nodesByID = {};
    var edges     = [];
    var adjacency = {};

    function normalizeGraphData(next) {
      data = next || { nodes: [], edges: [] };
      data.nodes = Array.isArray(data.nodes) ? data.nodes : [];
      data.edges = Array.isArray(data.edges) ? data.edges : [];

      nodesByID = {};
      data.nodes.forEach(function (n) { nodesByID[n.id] = n; });

      edges = data.edges.filter(function (e) {
        if (e.kind === "relates_to") e.kind = "related";
        return nodesByID[e.from] && nodesByID[e.to];
      });

      adjacency = {};
      edges.forEach(function (e) {
        adjacency[e.from] = adjacency[e.from] || {};
        adjacency[e.to]   = adjacency[e.to]   || {};
        adjacency[e.from][e.to] = true;
        adjacency[e.to][e.from] = true;
      });
    }

    normalizeGraphData(rawData);

    // ── DOM refs ──────────────────────────────────────────────────────────────
    var searchEl       = graphEl.querySelector("[data-graph-search]");
    var authorInput    = graphEl.querySelector("[data-graph-author]");
    var authorPinBtn   = graphEl.querySelector("[data-graph-author-pin]");
    var authorPinLabel = graphEl.querySelector("[data-graph-author-pin-label]");
    var groupInput     = graphEl.querySelector("[data-graph-group]");
    var tagChipsEl     = graphEl.querySelector("[data-graph-tag-chips]");
    var tagInput       = graphEl.querySelector("[data-graph-tag-input]");
    var typeInputs     = Array.from(graphEl.querySelectorAll("[data-graph-type]"));
    var kindInputs     = Array.from(graphEl.querySelectorAll("[data-graph-kind]"));
    var sinceButtons   = Array.from(graphEl.querySelectorAll("[data-graph-since]"));
    var sourcedButtons = Array.from(graphEl.querySelectorAll("[data-graph-sourced]"));
    var visButtons     = Array.from(graphEl.querySelectorAll("[data-graph-visibility]"));
    var depthInput     = graphEl.querySelector("[data-graph-depth]");
    var depthValueEl   = graphEl.querySelector("[data-graph-depth-value]");
    var visibleCountEl = graphEl.querySelector("[data-graph-visible-count]");
    var titleEl        = graphEl.querySelector("[data-graph-title]");
    var metaEl         = graphEl.querySelector("[data-graph-meta]");
    var openEl         = graphEl.querySelector("[data-graph-open]");
    var graphEndpoint  = graphEl.dataset.argumentGraphEndpoint || "/graph/data";

    // ── Filter state ──────────────────────────────────────────────────────────
    var activeTags       = initialFilters.tags.slice();
    var sinceValue       = initialFilters.since      || "";
    var sourcedValue     = initialFilters.sourced    || "";
    var visibilityValue  = initialFilters.visibility || "";

    // server* mirrors the last filters actually sent to the server so we can
    // tell whether the server response's match flags are still current.
    var serverQuery      = initialFilters.query;
    var serverAuthor     = initialFilters.author;
    var serverGroup      = initialFilters.group;
    var serverTags       = initialFilters.tags.slice();
    var serverSince      = initialFilters.since;
    var serverSourced    = initialFilters.sourced;
    var serverVisibility = initialFilters.visibility;

    var searchTimer = null;
    var searchSeq   = 0;
    var selectedID  = "";

    // ── Helpers ───────────────────────────────────────────────────────────────
    function currentQuery()  { return ((searchEl  && searchEl.value)  || "").trim().toLowerCase(); }
    function currentAuthor() { return ((authorInput && authorInput.value) || "").trim(); }
    function currentGroup()  { return ((groupInput  && groupInput.value)  || "").trim(); }
    function currentTags()   { return activeTags.slice(); }
    function currentDepth()  {
      if (!depthInput) return 2;
      var v = parseInt(depthInput.value, 10);
      return isNaN(v) || v < 0 ? 0 : v;
    }
    function activeTypes() {
      var m = {};
      typeInputs.forEach(function (i) { m[i.value] = i.checked; });
      return m;
    }
    function activeKinds() {
      var m = {};
      kindInputs.forEach(function (i) { m[i.value] = i.checked; });
      return m;
    }
    function tagsEqual(a, b) {
      if (a.length !== b.length) return false;
      var as = a.slice().sort(), bs = b.slice().sort();
      for (var i = 0; i < as.length; i++) { if (as[i] !== bs[i]) return false; }
      return true;
    }
    function matchesQuery(node, q) {
      var hay = [node.title || "", node.body || "", node.authorUsername || "", node.type || ""].join(" ").toLowerCase();
      return hay.indexOf(q) >= 0;
    }
    function nodeDisplayLabel(n) {
      if (!n) return "";
      return n.type === "comment" ? (n.body || "").replace(/\s+/g, " ").trim() : (n.title || "");
    }
    function nodeHref(n) {
      if (!n) return "#";
      if (n.type === "comment" && n.parentSlug) return "/nodes/" + n.parentSlug + "#comment-" + n.id;
      return "/nodes/" + n.slug;
    }
    function truncate(s, max) {
      s = s || "";
      return s.length <= max ? s : s.slice(0, max - 1) + "…";
    }
    function normalizeTagInput(raw) {
      return String(raw || "").toLowerCase().replace(/[^a-z0-9_]+/g, "-").replace(/^-+|-+$/g, "");
    }

    // ── Tag chips ─────────────────────────────────────────────────────────────
    function renderTagChips() {
      if (!tagChipsEl) return;
      tagChipsEl.replaceChildren();
      activeTags.forEach(function (tag) {
        var chip = document.createElement("span");
        chip.className = "tag-chip argument-graph-tag-chip";
        var label = document.createElement("span");
        label.textContent = "#" + tag;
        chip.appendChild(label);
        var remove = document.createElement("button");
        remove.type = "button";
        remove.className = "argument-graph-tag-chip-remove";
        remove.setAttribute("aria-label", "Remove tag " + tag);
        remove.textContent = "×";
        remove.addEventListener("click", function () {
          activeTags = activeTags.filter(function (t) { return t !== tag; });
          renderTagChips();
          updateGraph();
          queueServerSearch();
        });
        chip.appendChild(remove);
        tagChipsEl.appendChild(chip);
      });
      if (tagInput) {
        tagInput.placeholder = activeTags.length === 0 ? "Tags" : "Add another tag";
      }
    }

    function addTag(name) {
      var normalized = normalizeTagInput(name);
      if (!normalized) return false;
      if (activeTags.indexOf(normalized) >= 0) return false;
      activeTags.push(normalized);
      renderTagChips();
      updateGraph();
      queueServerSearch();
      return true;
    }

    renderTagChips();

    // ── Filter pipeline ───────────────────────────────────────────────────────
    function filteredNodes() {
      var active  = activeTypes();
      var query   = currentQuery();
      var author  = currentAuthor().toLowerCase();
      var group   = currentGroup();
      var tags    = currentTags();
      var anyServer = !!(query || author || group || tags.length || sinceValue || sourcedValue || visibilityValue);

      if (!anyServer) {
        return data.nodes.filter(function (n) { return !!active[n.type]; });
      }

      var useMatchFlags = query  === (serverQuery  || "").toLowerCase()
        && author === (serverAuthor || "").toLowerCase()
        && group  === serverGroup
        && tagsEqual(tags, serverTags)
        && sinceValue      === serverSince
        && sourcedValue    === serverSourced
        && visibilityValue === serverVisibility;

      var keep = {}, frontier = [];
      data.nodes.forEach(function (n) {
        if (!active[n.type]) return;
        var seed;
        if (useMatchFlags) {
          seed = n.match === true;
        } else {
          seed = (!query  || matchesQuery(n, query))
              && (!author || (n.authorUsername || "").toLowerCase() === author);
        }
        if (seed) { keep[n.id] = true; frontier.push(n.id); }
      });

      var activeKindMap = activeKinds();
      var liveAdj = {};
      edges.forEach(function (e) {
        if (activeKindMap[e.kind] === false) return;
        liveAdj[e.from] = liveAdj[e.from] || {};
        liveAdj[e.to]   = liveAdj[e.to]   || {};
        liveAdj[e.from][e.to] = true;
        liveAdj[e.to][e.from] = true;
      });

      var depth = currentDepth();
      for (var hop = 0; hop < depth && frontier.length; hop++) {
        var next = [];
        frontier.forEach(function (id) {
          var nbs = liveAdj[id];
          if (!nbs) return;
          Object.keys(nbs).forEach(function (nid) {
            if (keep[nid]) return;
            var nb = nodesByID[nid];
            if (!nb || !active[nb.type]) return;
            keep[nid] = true;
            next.push(nid);
          });
        });
        frontier = next;
      }

      return data.nodes.filter(function (n) { return !!active[n.type] && !!keep[n.id]; });
    }

    function filteredEdges(nodeSet) {
      var inSet = {};
      nodeSet.forEach(function (n) { inSet[n.id] = true; });
      var activeKindMap = activeKinds();
      return edges.filter(function (e) {
        return inSet[e.from] && inSet[e.to] && activeKindMap[e.kind] !== false;
      });
    }

    // ── Colour function (reads selectedID so it must be called by name) ───────
    function nodeColorFn(n) {
      var base = NODE_COLOR[n.type] || "#888";
      if (!selectedID) return base;
      if (n.id === selectedID || (adjacency[selectedID] && adjacency[selectedID][n.id])) return base;
      return dimHex(base, 0.18);
    }

    // ── 3d-force-graph init ───────────────────────────────────────────────────
    var Graph = ForceGraph3D()(container)
      .backgroundColor(BG_COLOR)
      .nodeId("id")
      .nodeLabel(function (n) { return truncate(nodeDisplayLabel(n), 60); })
      .nodeColor(nodeColorFn)
      .nodeVal(function (n) {
        var r = { topic: 10, view: 8, finding: 7, comment: 5 }[n.type] || 7;
        return r * r;
      })
      .linkSource("from")
      .linkTarget("to")
      .linkColor(function (e) { return EDGE_COLOR[e.kind] || "#888"; })
      .linkWidth(1.5)
      .linkOpacity(0.6)
      .linkDirectionalArrowLength(8)
      .linkDirectionalArrowRelPos(1)
      .onNodeClick(handleNodeClick)
      .onBackgroundClick(clearSelection);

    // Tune the built-in forces to mirror the 2D KIND_LINK constants.
    var linkForce = Graph.d3Force("link");
    if (linkForce) {
      linkForce
        .distance(function (e) { return (KIND_LINK[e.kind] || {}).distance || 200; })
        .strength(function (e)  { return (KIND_LINK[e.kind] || {}).strength || 0.28; });
    }
    var chargeForce = Graph.d3Force("charge");
    if (chargeForce) chargeForce.strength(-180).distanceMax(200);

    // Z-axis type gravity: topics at back, findings at front.
    Graph.d3Force("typeZ", (function () {
      var nodes;
      function force(alpha) {
        (nodes || []).forEach(function (n) {
          var tz = TYPE_Z[n.type];
          if (tz == null) return;
          n.vz = (n.vz || 0) + (tz - (n.z || 0)) * TYPE_Z_STRENGTH * alpha;
        });
      }
      force.initialize = function (_nodes) { nodes = _nodes; };
      return force;
    })());

    // ── Cursor-based zoom ─────────────────────────────────────────────────────
    // OrbitControls' default zoom moves the camera along its forward axis toward
    // a fixed target point, so the perceived pivot drifts to wherever the target
    // happens to be. We replace it with a wheel handler that scales both the
    // camera position AND the orbit target around the cursor's 3D world point,
    // making the scene expand/contract exactly under the pointer.
    var orbitControls = Graph.controls();
    if (orbitControls) {
      orbitControls.enableZoom = false;

      var glCanvas = Graph.renderer().domElement;
      glCanvas.addEventListener("wheel", function (e) {
        e.preventDefault();
        e.stopPropagation();

        var camera   = Graph.camera();
        var controls = Graph.controls();
        var rect     = glCanvas.getBoundingClientRect();

        // Cursor in normalised device coordinates (NDC: −1..1 on each axis).
        var ndcX = ((e.clientX - rect.left)  / rect.width)  *  2 - 1;
        var ndcY = ((e.clientY - rect.top)   / rect.height) * -2 + 1;

        // World-space direction from camera through cursor.
        // camera.position.clone() gives us a THREE.Vector3 without importing THREE.
        var dir = camera.position.clone()
          .set(ndcX, ndcY, 0.5)
          .unproject(camera)
          .sub(camera.position)
          .normalize();

        // Project cursor onto a plane at the current camera→target distance,
        // giving a stable world-space pivot point directly under the pointer.
        var dist  = camera.position.distanceTo(controls.target);
        var pivot = camera.position.clone().addScaledVector(dir, dist);

        // Zoom factor: < 1 pulls in, > 1 pushes out.
        var factor = e.deltaY > 0 ? 1.15 : 1 / 1.15;

        // Scale both camera and orbit target around the pivot.
        var camDelta    = camera.position.clone().sub(pivot);
        var targetDelta = controls.target.clone().sub(pivot);
        camera.position.copy(pivot).addScaledVector(camDelta,    factor);
        controls.target.copy(pivot).addScaledVector(targetDelta, factor);
        controls.update();
      }, { passive: false });
    }

    // The library sets camera.position.z = 1000 at init. Multiply by 3 now,
    // before any data loads, so the default view is 3× further out.
    // onEngineStop fires only after cooldownTime (15 s default) — too late to
    // be useful — so we set the position synchronously here instead.
    Graph.camera().position.z *= 1.6;

    // ── Graph data update ─────────────────────────────────────────────────────
    function updateGraph() {
      var nodes = filteredNodes();
      var links = filteredEdges(nodes);
      Graph.graphData({ nodes: nodes, links: links });
      if (visibleCountEl) {
        var n = nodes.length;
        visibleCountEl.textContent = (n === 1 ? "1 node" : n + " nodes") + " visible";
      }
    }

    updateGraph();

    // ── Inspector ─────────────────────────────────────────────────────────────
    function handleNodeClick(node) {
      if (!node) return;
      selectedID = node.id;
      Graph.nodeColor(nodeColorFn);

      if (titleEl) titleEl.textContent = truncate(nodeDisplayLabel(node), 80);

      if (metaEl) {
        metaEl.replaceChildren();
        var typeChip = document.createElement("span");
        typeChip.className = "chip " + node.type;
        typeChip.textContent = node.type;
        metaEl.appendChild(typeChip);

        var authorChip = document.createElement("span");
        authorChip.className = "chip";
        authorChip.textContent = "@" + node.authorUsername;
        metaEl.appendChild(authorChip);

        var nb = Object.keys(adjacency[node.id] || {}).length;
        var countSpan = document.createElement("span");
        countSpan.className = "muted";
        countSpan.textContent = nb + " connection" + (nb === 1 ? "" : "s");
        metaEl.appendChild(countSpan);
      }

      if (openEl) {
        openEl.href = nodeHref(node);
        openEl.removeAttribute("hidden");
      }

      if (authorPinBtn && node.authorUsername) {
        authorPinBtn.removeAttribute("hidden");
        if (authorPinLabel) authorPinLabel.textContent = "Filter by @" + node.authorUsername;
        authorPinBtn.onclick = function () {
          if (authorInput) authorInput.value = node.authorUsername;
          queueServerSearch();
        };
      }
    }

    function clearSelection() {
      selectedID = "";
      Graph.nodeColor(nodeColorFn);
      if (titleEl) titleEl.textContent = "Choose a node";
      if (metaEl) metaEl.replaceChildren();
      if (openEl) openEl.setAttribute("hidden", "");
      if (authorPinBtn) authorPinBtn.setAttribute("hidden", "");
    }

    // ── Server fetch ──────────────────────────────────────────────────────────
    function graphDataURL() {
      var params = new URLSearchParams();
      var q      = currentQuery();
      var author = currentAuthor();
      var group  = currentGroup();
      var tags   = currentTags();
      if (q)          params.set("q", q);
      if (author)     params.set("author", author);
      if (group)      params.set("group", group);
      tags.forEach(function (t) { params.append("tag", t); });
      if (sinceValue)      params.set("since",      sinceValue);
      if (sourcedValue)    params.set("sourced",    sourcedValue);
      if (visibilityValue) params.set("visibility", visibilityValue);
      var s = params.toString();
      return graphEndpoint + (s ? "?" + s : "");
    }

    function snapshotFilters() {
      serverQuery      = currentQuery();
      serverAuthor     = currentAuthor();
      serverGroup      = currentGroup();
      serverTags       = currentTags();
      serverSince      = sinceValue;
      serverSourced    = sourcedValue;
      serverVisibility = visibilityValue;
    }

    function queueServerSearch() {
      clearTimeout(searchTimer);
      var seq = ++searchSeq;
      searchTimer = setTimeout(function () {
        snapshotFilters();
        fetch(graphDataURL(), { headers: { Accept: "application/json" } })
          .then(function (r) { return r.ok ? r.json() : null; })
          .then(function (json) {
            if (seq !== searchSeq || !json) return;
            normalizeGraphData(json);
            updateGraph();
          })
          .catch(function () {});
      }, 220);
      updateGraph();
    }

    // ── Zoom ──────────────────────────────────────────────────────────────────
    graphEl.querySelectorAll("[data-graph-zoom]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        var cam = Graph.camera();
        switch (btn.dataset.graphZoom) {
          case "in":
            Graph.cameraPosition({ x: cam.position.x * 0.8, y: cam.position.y * 0.8, z: cam.position.z * 0.8 }, undefined, 200);
            break;
          case "out":
            Graph.cameraPosition({ x: cam.position.x * 1.25, y: cam.position.y * 1.25, z: cam.position.z * 1.25 }, undefined, 200);
            break;
          case "reset":
            Graph.zoomToFit(400);
            break;
        }
      });
    });

    // ── Reshuffle ─────────────────────────────────────────────────────────────
    var reshuffleBtn = graphEl.querySelector("[data-graph-reshuffle]");
    if (reshuffleBtn) {
      reshuffleBtn.addEventListener("click", function () {
        selectedID = "";
        Graph.nodeColor(nodeColorFn);
        Graph.d3ReheatSimulation();
      });
    }

    // ── Depth slider ──────────────────────────────────────────────────────────
    if (depthInput) {
      if (depthValueEl) depthValueEl.textContent = String(currentDepth());
      depthInput.addEventListener("input", function () {
        if (depthValueEl) depthValueEl.textContent = depthInput.value;
        updateGraph();
      });
    }

    // ── Type / kind checkboxes ────────────────────────────────────────────────
    typeInputs.forEach(function (input) { input.addEventListener("change", updateGraph); });
    kindInputs.forEach(function (input) { input.addEventListener("change", updateGraph); });

    // ── Text inputs ───────────────────────────────────────────────────────────
    if (searchEl)    searchEl.addEventListener("input",    function () { updateGraph(); queueServerSearch(); });
    if (authorInput) authorInput.addEventListener("input", queueServerSearch);
    if (groupInput)  groupInput.addEventListener("input",  queueServerSearch);

    // ── Toggle-button groups ──────────────────────────────────────────────────
    function bindToggleGroup(buttons, getter, setter) {
      buttons.forEach(function (btn) {
        btn.addEventListener("click", function () {
          var value = btn.dataset[getter] !== undefined ? btn.dataset[getter] : "";
          setter(value);
          buttons.forEach(function (b) {
            var v = b.dataset[getter] !== undefined ? b.dataset[getter] : "";
            if (v === value) { b.classList.add("is-active"); b.classList.remove("ghost"); }
            else             { b.classList.remove("is-active"); b.classList.add("ghost"); }
          });
          queueServerSearch();
        });
      });
    }

    bindToggleGroup(sinceButtons,   "graphSince",      function (v) { sinceValue      = v; });
    bindToggleGroup(sourcedButtons, "graphSourced",    function (v) { sourcedValue    = v; });
    bindToggleGroup(visButtons,     "graphVisibility", function (v) { visibilityValue = v; });

    // ── Tag input ─────────────────────────────────────────────────────────────
    if (tagInput) {
      tagInput.addEventListener("keydown", function (event) {
        if (event.key === "Enter" || event.key === ",") {
          event.preventDefault();
          if (addTag(tagInput.value)) tagInput.value = "";
        } else if (event.key === "Backspace" && tagInput.value === "" && activeTags.length > 0) {
          activeTags.pop();
          renderTagChips();
          updateGraph();
          queueServerSearch();
        }
      });
      tagInput.addEventListener("blur", function () {
        if (tagInput.value.trim() !== "" && addTag(tagInput.value)) tagInput.value = "";
      });
    }

    // ── Tag suggest clicks (data-graph-tag-add) ───────────────────────────────
    graphEl.addEventListener("click", function (event) {
      var btn = event.target.closest && event.target.closest("[data-graph-tag-add]");
      if (!btn || !graphEl.contains(btn)) return;
      if (addTag(btn.dataset.fillValue || "")) {
        if (tagInput) tagInput.value = "";
      }
      var suggest = btn.closest(".search-suggest");
      if (suggest) { suggest.innerHTML = ""; suggest.hidden = true; }
    });
  }

  function initAll(root) {
    root.querySelectorAll("[data-argument-graph-3d]").forEach(function (el) {
      initArgumentGraph3D(el);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { initAll(document); });
  } else {
    initAll(document);
  }
})();
