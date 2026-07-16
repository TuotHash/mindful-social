package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

// validateSourceURL accepts empty input (the field is optional) and otherwise
// requires an http or https URL with a host. Returns a flash string on
// rejection. The template path also uses templ.URL to collapse anything else
// to the failed-sanitization sentinel, but the server check is the real gate.
func validateSourceURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "Source URL is not a valid URL."
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "Source URL must start with http:// or https://."
	}
	if u.Host == "" {
		return "Source URL must include a host."
	}
	return ""
}

// resolveNode loads a node by the URL param "id". The param may be either a
// UUID or a slug — UUID is tried first (parses as a uuid.UUID), slug is the
// fallback. On not-found, any other error, or visibility denial the function
// writes the response and returns ok=false; the caller should return
// immediately. Hidden nodes are reported as 404 so existence isn't leaked.
func (s *Server) resolveNode(w http.ResponseWriter, r *http.Request) (db.Node, bool) {
	raw := chiURLParam(r, "id")
	if raw == "" {
		s.notFound(w, r)
		return db.Node{}, false
	}
	var node db.Node
	var err error
	if id, parseErr := uuid.Parse(raw); parseErr == nil {
		node, err = s.queries.GetNode(r.Context(), id)
	} else {
		node, err = s.queries.GetNodeBySlug(r.Context(), raw)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return db.Node{}, false
		}
		s.logger.Error("resolve node", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return db.Node{}, false
	}
	visible, err := s.canViewNode(r.Context(), node, viewerID(r))
	if err != nil {
		s.logger.Error("resolve node: visibility", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return db.Node{}, false
	}
	if !visible {
		s.notFound(w, r)
		return db.Node{}, false
	}
	return node, true
}

// handleLanding serves the public landing page at /. Accessible to everyone
// including logged-in users who want to browse the marketing page. Pulls a
// handful of recent visible nodes for the example section; node_visible_to()
// ensures anonymous visitors only see public content.
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.ListRecentNodesForViewer(r.Context(), db.ListRecentNodesForViewerParams{
		Limit:    3,
		ViewerID: viewerID(r),
	})
	if err != nil {
		s.logger.Error("landing: list recent", "err", err)
		rows = nil
	}
	items := make([]views.FeedItem, len(rows))
	for i, row := range rows {
		items[i] = views.FeedItem{Slug: row.Slug, Type: row.Type, Title: row.Title, Body: row.Body, AuthorUsername: row.AuthorUsername, SupportsCount: row.SupportsCount, OpposesCount: row.OpposesCount, FirstImageURL: views.NodeFirstImageURL(row.Body)}
	}
	render(w, r, views.Landing(viewerFor(currentUser(r)), items))
}

// handleHome serves the personal feed at /home for logged-in users.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := s.queries.ListRecentNodesForViewer(r.Context(), db.ListRecentNodesForViewerParams{
		Limit:    25,
		ViewerID: viewerID(r),
	})
	if err != nil {
		s.logger.Error("home: list recent", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	items := make([]views.FeedItem, len(rows))
	for i, r := range rows {
		items[i] = views.FeedItem{Slug: r.Slug, Type: r.Type, Title: r.Title, Body: r.Body, AuthorUsername: r.AuthorUsername, SupportsCount: r.SupportsCount, OpposesCount: r.OpposesCount, FirstImageURL: views.NodeFirstImageURL(r.Body)}
	}
	render(w, r, views.Feed(viewerFor(user), items))
}

func (s *Server) handleNodeNew(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	groups, err := s.queries.ListGroupsForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("node new: groups", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	initialParents, err := s.queries.SearchPostParents(r.Context(), db.SearchPostParentsParams{
		TypeFilter: "", // default form type is finding, which can attach to any node
		Query:      "",
		ViewerID:   viewerID(r),
	})
	if err != nil {
		s.logger.Error("node new: search parents", "err", err)
		initialParents = nil
	}
	parentCandidates := parentCandidateRows(initialParents)
	defaultVisibility := string(user.DefaultNodeVisibility)
	if defaultVisibility == "" {
		defaultVisibility = string(db.VisibilityKindPublic)
	}
	if isHTMX(r) {
		render(w, r, views.NodeNewModal("", "finding", "", "", "", "", defaultVisibility, groups, "", "", "root", "related", parentCandidates))
		return
	}
	render(w, r, views.NodeNew(viewerFor(user), "", "finding", "", "", "", "", defaultVisibility, groups, "", "", "root", "related", parentCandidates))
}

// handleParentPicker serves the post form's parent-node picker fragment.
// The selected type drives which candidates are returned: view/topic look
// for a topic to anchor under; finding searches across all node types.
func (s *Server) handleParentPicker(w http.ResponseWriter, r *http.Request) {
	find := strings.TrimSpace(r.URL.Query().Get("find_parent"))
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	filter := string(db.NodeTypeTopic)
	if typ == string(db.NodeTypeFinding) {
		filter = "" // findings can attach to any node type
	}
	rows, err := s.queries.SearchPostParents(r.Context(), db.SearchPostParentsParams{
		TypeFilter: filter,
		Query:      find,
		ViewerID:   viewerID(r),
	})
	if err != nil {
		s.logger.Error("parent picker", "err", err)
		rows = nil
	}
	render(w, r, views.TopicCandidatePicker(find, "", parentCandidateRows(rows)))
}

func parentCandidateRows(rows []db.SearchPostParentsRow) []views.TopicCandidate {
	out := make([]views.TopicCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, views.TopicCandidate{ID: r.ID, Type: r.Type, Title: r.Title})
	}
	return out
}

// handleNodeGenerateForm serves the AI prompt modal (GET /nodes/generate).
// It 404s when AI drafting isn't configured so the route surface matches the
// hidden "Generate with AI" button. The feature is htmx-only; a non-htmx hit
// falls back to the styled New Post page, which still carries the button.
func (s *Server) handleNodeGenerateForm(w http.ResponseWriter, r *http.Request) {
	if s.aiClient == nil {
		s.notFound(w, r)
		return
	}
	if !isHTMX(r) {
		http.Redirect(w, r, "/nodes/new", http.StatusSeeOther)
		return
	}
	render(w, r, views.NodeGenerateModal("", ""))
}

// handleNodeGenerate (POST /nodes/generate) turns a prompt into a draft and
// responds with the normal New Post modal pre-filled with it. Nothing is
// written: the user reviews, picks a parent/visibility, and submits through
// POST /nodes like any other post. On error it re-renders the prompt modal
// with a flash and the user's prompt preserved.
//
// Generation runs synchronously in the request, so it is bounded by the
// router's 30s request timeout (see routes()). That's ample for a short
// single node on a small local model; longer generations would need the
// background-job pattern the audio worker uses.
func (s *Server) handleNodeGenerate(w http.ResponseWriter, r *http.Request) {
	if s.aiClient == nil {
		s.notFound(w, r)
		return
	}
	user := currentUser(r) // requireUser guarantees non-nil
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}
	prompt := strings.TrimSpace(r.PostFormValue("prompt"))
	if prompt == "" {
		render(w, r, views.NodeGenerateModal("Describe the node you want to generate.", ""))
		return
	}
	const maxPromptLen = 2000
	if len(prompt) > maxPromptLen {
		prompt = prompt[:maxPromptLen]
	}

	draft, err := s.aiClient.GenerateNode(r.Context(), prompt)
	if err != nil {
		s.logger.Error("ai: generate node", "err", err, "user_id", user.ID)
		render(w, r, views.NodeGenerateModal("Couldn't generate a draft — try again or rephrase your prompt.", prompt))
		return
	}
	s.logger.Info("ai: node draft generated", "user_id", user.ID, "type", draft.Type)

	// Build the same form arguments handleNodeNew does, then pre-fill type,
	// title, and body from the draft. Everything else stays at its default so
	// the user makes the graph-shaping choices (parent, visibility, tags).
	groups, err := s.queries.ListGroupsForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("ai generate: groups", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	initialParents, err := s.queries.SearchPostParents(r.Context(), db.SearchPostParentsParams{
		TypeFilter: "",
		Query:      "",
		ViewerID:   viewerID(r),
	})
	if err != nil {
		s.logger.Error("ai generate: search parents", "err", err)
		initialParents = nil
	}
	defaultVisibility := string(user.DefaultNodeVisibility)
	if defaultVisibility == "" {
		defaultVisibility = string(db.VisibilityKindPublic)
	}
	render(w, r, views.NodeNewModal("", draft.Type, draft.Title, draft.Body, "", "", defaultVisibility, groups, "", "", "root", "related", parentCandidateRows(initialParents)))
}

func (s *Server) handleNodeCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	rawType := strings.TrimSpace(r.PostFormValue("type"))
	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	rawPin := strings.TrimSpace(r.PostFormValue("pin")) // "" | "supports" | "opposes" | "featured"
	rawTags := r.PostFormValue("tags")
	rawVisibility := strings.TrimSpace(r.PostFormValue("visibility"))
	rawParentNodeID := strings.TrimSpace(r.PostFormValue("parent_node_id"))
	rawFindParent := strings.TrimSpace(r.PostFormValue("find_parent"))
	rawTopicParentMode := strings.TrimSpace(r.PostFormValue("topic_parent_mode")) // "root" | "sub"
	rawFindingEdgeKind := strings.TrimSpace(r.PostFormValue("finding_edge_kind")) // "supports" | "opposes" | "related"
	if rawTopicParentMode != "sub" {
		rawTopicParentMode = "root"
	}
	if rawFindingEdgeKind == "" {
		rawFindingEdgeKind = "related"
	}
	if rawVisibility == "" {
		rawVisibility = "public"
	}

	groups, groupsErr := s.queries.ListGroupsForUser(r.Context(), user.ID)
	if groupsErr != nil {
		s.logger.Error("create node: groups", "err", groupsErr)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	// parentCandidatesForRerender returns a single pre-selected candidate so
	// the picker re-renders with the user's selection intact after a validation
	// error on another field. For view / sub-topic the parent must be a topic;
	// for finding any visible node is acceptable.
	parentCandidatesForRerender := func() []views.TopicCandidate {
		if rawParentNodeID == "" {
			return nil
		}
		pid, err := uuid.Parse(rawParentNodeID)
		if err != nil {
			return nil
		}
		n, err := s.queries.GetNode(r.Context(), pid)
		if err != nil {
			return nil
		}
		if db.NodeType(rawType) != db.NodeTypeFinding && n.Type != db.NodeTypeTopic {
			return nil
		}
		return []views.TopicCandidate{{ID: n.ID, Type: n.Type, Title: n.Title}}
	}

	rerender := func(flash string) {
		tc := parentCandidatesForRerender()
		if isHTMX(r) {
			render(w, r, views.NodeNewModal(flash, rawType, title, body, rawPin, rawTags, rawVisibility, groups, rawFindParent, rawParentNodeID, rawTopicParentMode, rawFindingEdgeKind, tc))
			return
		}
		render(w, r, views.NodeNew(viewerFor(user), flash, rawType, title, body, rawPin, rawTags, rawVisibility, groups, rawFindParent, rawParentNodeID, rawTopicParentMode, rawFindingEdgeKind, tc))
	}

	flash := ""
	nt := db.NodeType(rawType)
	var pinKind db.PinKind
	var parentID uuid.UUID
	var parentNodeID *uuid.UUID
	var parentGroupID *uuid.UUID
	var findingEdgeKind db.EdgeKind
	switch {
	case nt != db.NodeTypeTopic && nt != db.NodeTypeView && nt != db.NodeTypeFinding:
		flash = "Pick a type: Opinion, Topic, or Occurence."
	case title == "":
		flash = "Title is required."
	case len(title) > 200:
		flash = "Title is too long (max 200 characters)."
	}
	// Views and findings always need a parent; topics need one only when the
	// user chose "sub-topic". Root topics ignore any parent selection. The
	// parent for view / sub-topic must be a topic; for finding any visible
	// node is fine.
	needsParent := nt == db.NodeTypeView || nt == db.NodeTypeFinding || (nt == db.NodeTypeTopic && rawTopicParentMode == "sub")
	if flash == "" && needsParent {
		missingMsg := "An opinion must be connected to a parent topic. Search and select one above."
		mustBeTopic := true
		switch nt {
		case db.NodeTypeTopic:
			missingMsg = "A sub-topic must be connected to a parent topic. Search and select one above."
		case db.NodeTypeFinding:
			missingMsg = "An occurence must attach to an existing node. Search and select one above."
			mustBeTopic = false
		}
		if rawParentNodeID == "" {
			flash = missingMsg
		} else {
			pid, err := uuid.Parse(rawParentNodeID)
			if err != nil {
				flash = "Invalid parent selection."
			} else {
				parentNode, err := s.queries.GetNode(r.Context(), pid)
				switch {
				case err != nil:
					flash = "The selected parent was not found."
				case mustBeTopic && parentNode.Type != db.NodeTypeTopic:
					flash = "The selected parent must be a topic."
				default:
					visible, verr := s.canViewNode(r.Context(), parentNode, viewerID(r))
					if verr != nil {
						s.logger.Error("create node: parent visibility", "err", verr)
						flash = "Could not check parent permissions. Please try again."
						break
					}
					if !visible {
						flash = "The selected parent was not found."
						break
					}
					parentID = pid
					parentNodeID = &parentID
					parentGroupID = parentNode.GroupID
					if parentGroupID == nil {
						parentGroupID = parentNode.VisibilityGroupID
					}
				}
			}
		}
	}
	if flash == "" && nt == db.NodeTypeFinding {
		ek := db.EdgeKind(rawFindingEdgeKind)
		if !isUserPickableEdgeKind(ek) {
			flash = "Pick how this occurence relates to its parent."
		} else {
			findingEdgeKind = ek
		}
	}
	if flash == "" && rawPin != "" {
		var pinErr string
		pinKind, pinErr = parsePinKind(rawPin, nt)
		if pinErr != "" {
			flash = pinErr
		}
	}
	visKind, visGroupID, visErr := parseNodeVisibility(rawVisibility, user.ID, groups)
	if flash == "" && visErr != "" {
		flash = visErr
	}
	groupID := visGroupID
	if groupID == nil {
		groupID = parentGroupID
	}
	if flash != "" {
		rerender(flash)
		return
	}

	node, err := s.createNodeWithUniqueSlug(r.Context(), slugify(title), func(slug string) db.CreateNodeParams {
		return db.CreateNodeParams{
			Type:              nt,
			Title:             title,
			Body:              body,
			SourceUrl:         nil,
			CreatedBy:         user.ID,
			Slug:              slug,
			Visibility:        visKind,
			VisibilityGroupID: visGroupID,
			GroupID:           groupID,
			ParentNodeID:      parentNodeID,
		}
	})
	if err != nil {
		s.logger.Error("create node", "err", err)
		rerender("Could not create post. Please try again.")
		return
	}
	s.logger.Info("node created", "node_id", node.ID, "slug", node.Slug, "type", node.Type, "user_id", user.ID, "visibility", node.Visibility)
	// Connect the new node to its parent automatically. View and sub-topic
	// edges go from the new node to its parent topic as `related`. Findings
	// mirror the inline edge-creation flow: the edge goes parent → finding
	// with the kind the user picked (supports / opposes / related).
	if parentID != uuid.Nil {
		edgeFrom := node.ID
		edgeTo := parentID
		edgeKind := db.EdgeKindRelated
		if nt == db.NodeTypeFinding {
			edgeFrom = parentID
			edgeTo = node.ID
			edgeKind = findingEdgeKind
		}
		if _, err := s.queries.CreateEdge(r.Context(), db.CreateEdgeParams{
			FromNode:  edgeFrom,
			ToNode:    edgeTo,
			Kind:      edgeKind,
			CreatedBy: user.ID,
		}); err != nil {
			s.logger.Error("create node: parent edge", "err", err)
		}
	}
	if rawPin != "" {
		if _, err := s.queries.SetPin(r.Context(), db.SetPinParams{
			UserID: user.ID,
			NodeID: node.ID,
			Kind:   pinKind,
		}); err != nil {
			// Node created successfully; the pin failed. Log and continue —
			// user can pin from the detail page.
			s.logger.Error("create node: pin", "err", err)
		}
	}
	tagNames := parseTagsInput(rawTags)
	if len(tagNames) > 0 {
		if err := s.setTagsForNode(r, node.ID, tagNames); err != nil {
			s.logger.Error("create node: set tags", "err", err)
		}
	}
	s.snapshotNodeRevision(r.Context(), node.ID, &user.ID, node.Title, node.Body, "Created.", tagNames)
	s.enqueueAudioForNode(r.Context(), node)
	// Modal submit: ask htmx to do a full navigation to the new post so the
	// modal closes and the page actually changes. Non-htmx submits get the
	// usual 303 redirect.
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/nodes/"+node.Slug)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

func (s *Server) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID

	vid := viewerID(r)
	out, err := s.queries.ListEdgesFromNodeForViewer(r.Context(), db.ListEdgesFromNodeForViewerParams{
		FromNode: id,
		ViewerID: vid,
	})
	if err != nil {
		s.logger.Error("node detail: outgoing edges", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	in, err := s.queries.ListEdgesToNodeForViewer(r.Context(), db.ListEdgesToNodeForViewerParams{
		ToNode:   id,
		ViewerID: vid,
	})
	if err != nil {
		s.logger.Error("node detail: incoming edges", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	feat, err := s.queries.ListHighlightedEdgesForNode(r.Context(), db.ListHighlightedEdgesForNodeParams{
		NodeID:   id,
		ViewerID: vid,
	})
	if err != nil {
		s.logger.Error("node detail: highlighted edges", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	tags, err := s.queries.ListTagsForNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node detail: list tags", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	user := currentUser(r)
	var pin *views.PinInfo
	if user != nil {
		row, ok := s.lookupPin(w, r, user.ID, id)
		if !ok {
			return // error already written
		}
		if row != nil {
			pin = &views.PinInfo{Kind: row.Kind}
		}
	}

	counts, err := s.queries.GetPinCountsForNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node detail: stance counts", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	stance := views.StanceCounts{
		Supports:  counts.SupportsCount,
		Opposes:   counts.OpposesCount,
		Resonates: counts.ResonatesCount,
	}

	author, err := s.queries.GetUser(r.Context(), node.CreatedBy)
	if err != nil {
		s.logger.Error("node detail: author", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	canEdit, err := s.canEditNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node detail: edit policy", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	canDelete, err := s.canDeleteNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node detail: delete policy", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	var topicViews []views.TopicView
	if node.Type == db.NodeTypeTopic {
		rows, err := s.queries.ListViewsForTopic(r.Context(), db.ListViewsForTopicParams{
			TopicID:     id,
			ViewerID:    vid,
			ResultLimit: 50,
		})
		if err != nil {
			s.logger.Error("node detail: topic views", "err", err)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
		topicViews = topicViewsFromRows(rows)
	}

	var comments []views.Comment
	var commentCount int64
	if node.Type != db.NodeTypeComment {
		rows, err := s.queries.ListCommentsForNode(r.Context(), id)
		if err != nil {
			s.logger.Error("node detail: comments", "err", err)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
		comments, commentCount = commentThreadFromRows(rows, user)
	}

	render(w, r, views.NodeDetail(
		viewerFor(user),
		node,
		author.Username,
		highlightedRows(feat),
		displayGroups(out, in),
		topicViews,
		comments,
		commentCount,
		pin,
		stance,
		tags,
		canEdit,
		canDelete,
	))
}

func (s *Server) handleEdgeHighlight(w http.ResponseWriter, r *http.Request) {
	povNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if !s.requireEditPermission(w, r, povNode) {
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	rows, err := s.queries.HighlightEdge(r.Context(), db.HighlightEdgeParams{
		PovNode: povNode.ID,
		EdgeID:  edgeID,
	})
	if err != nil {
		s.logger.Error("highlight edge", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, "/nodes/"+povNode.Slug, http.StatusSeeOther)
}

func (s *Server) handleEdgeUnhighlight(w http.ResponseWriter, r *http.Request) {
	povNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if !s.requireEditPermission(w, r, povNode) {
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	rows, err := s.queries.UnhighlightEdge(r.Context(), db.UnhighlightEdgeParams{
		PovNode: povNode.ID,
		EdgeID:  edgeID,
	})
	if err != nil {
		s.logger.Error("unhighlight edge", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, "/nodes/"+povNode.Slug, http.StatusSeeOther)
}

// requireEditPermission writes a 403 and returns false if the current
// viewer isn't allowed to edit `node`. Bundled so edge-curation handlers
// (highlight/unhighlight/disconnect from a page's POV) can early-return
// cleanly.
func (s *Server) requireEditPermission(w http.ResponseWriter, r *http.Request, node db.Node) bool {
	allowed, err := s.canEditNode(r.Context(), node, currentUser(r))
	if err != nil {
		s.logger.Error("policy check", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return false
	}
	if !allowed {
		http.Error(w, "You don't have permission to curate this node.", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) handleNodeDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID
	allowed, err := s.canDeleteNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node delete confirm: delete policy", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if !allowed {
		s.renderError(w, r, http.StatusForbidden)
		return
	}
	edgeCount, err := s.queries.CountEdgesForNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node delete confirm: count edges", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	otherPinCount, err := s.queries.CountOtherUserPinsForNode(r.Context(), db.CountOtherUserPinsForNodeParams{
		NodeID: id,
		UserID: user.ID,
	})
	if err != nil {
		s.logger.Error("node delete confirm: count pins", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	render(w, r, views.NodeDelete(viewerFor(user), node, edgeCount, otherPinCount))
}

func (s *Server) handleNodeDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	allowed, err := s.canDeleteNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node delete: delete policy", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if !allowed {
		s.renderError(w, r, http.StatusForbidden)
		return
	}
	if err := s.queries.DeleteNode(r.Context(), node.ID); err != nil {
		s.logger.Error("node delete", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	s.logger.Info("node deleted", "node_id", node.ID, "slug", node.Slug, "type", node.Type, "user_id", user.ID)
	// Authors land back on their own profile. Site staff deleting other
	// people's content go to /admin where the rest of the moderation
	// surface lives. Group editors (non-author, non-staff) deleting a
	// hosted node go back to the group; if the node had no group, fall
	// back to /home.
	dest := "/users/" + user.Username
	if node.CreatedBy != user.ID {
		switch {
		case isStaff(user):
			dest = "/admin"
		case node.GroupID != nil:
			if g, err := s.queries.GetGroup(r.Context(), *node.GroupID); err == nil {
				dest = "/groups/" + g.Slug
			} else {
				dest = "/home"
			}
		default:
			dest = "/home"
		}
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) handleEdgeDelete(w http.ResponseWriter, r *http.Request) {
	// pageNode is the node whose page hosted the form. Disconnect is treated
	// as an edit action on that page node: if the viewer can curate this
	// node, they can also detach edges that touch it.
	pageNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if !s.requireEditPermission(w, r, pageNode) {
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	rows, err := s.queries.DeleteEdge(r.Context(), db.DeleteEdgeParams{
		EdgeID:     edgeID,
		PageNodeID: pageNode.ID,
	})
	if err != nil {
		s.logger.Error("delete edge", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, "/nodes/"+pageNode.Slug, http.StatusSeeOther)
}

func (s *Server) handleNodeEdit(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	allowed, err := s.canEditNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node edit: policy check", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "You don't have permission to edit this node.", http.StatusForbidden)
		return
	}
	id := node.ID
	src := ""
	if node.SourceUrl != nil {
		src = *node.SourceUrl
	}
	tags, err := s.queries.ListTagsForNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node edit: list tags", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	groups, err := s.queries.ListGroupsForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("node edit: groups", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	isAuthor := node.CreatedBy == user.ID
	canChangeVisibility := isAuthor || isStaff(user)
	// Groups belong to the author's account, so when staff are moderating
	// someone else's node we hide them from the picker — they'd map to
	// the moderator's own groups, which makes no sense as a visibility
	// target. The picker keeps public/connections/private either way.
	visGroups := groups
	if !isAuthor {
		visGroups = nil
	}
	render(w, r, views.NodeEdit(viewerFor(user), node, "", node.Title, node.Body, src, joinTagNames(tags), formatNodeVisibility(node.Visibility, node.VisibilityGroupID), visGroups, isAuthor, canChangeVisibility, string(node.EditPolicy)))
}

// joinTagNames flattens a tag list into the comma-separated string the form
// field expects.
func joinTagNames(tags []db.Tag) string {
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func (s *Server) handleNodeUpdate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	allowed, err := s.canEditNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node update: policy check", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "You don't have permission to edit this node.", http.StatusForbidden)
		return
	}
	isAuthor := node.CreatedBy == user.ID
	canChangeVisibility := isAuthor || isStaff(user)
	id := node.ID
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	sourceURL := strings.TrimSpace(r.PostFormValue("source_url"))
	rawTags := r.PostFormValue("tags")
	rawVisibility := strings.TrimSpace(r.PostFormValue("visibility"))
	if rawVisibility == "" || !canChangeVisibility {
		// Editors without visibility rights keep the existing setting;
		// an empty submission also falls through unchanged.
		rawVisibility = formatNodeVisibility(node.Visibility, node.VisibilityGroupID)
	}
	rawEditPolicy := strings.TrimSpace(r.PostFormValue("edit_policy"))

	groups, groupsErr := s.queries.ListGroupsForUser(r.Context(), user.ID)
	if groupsErr != nil {
		s.logger.Error("update node: groups", "err", groupsErr)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	flash := ""
	switch {
	case title == "":
		flash = "Title is required."
	case len(title) > 200:
		flash = "Title is too long (max 200 characters)."
	}
	if flash == "" {
		if msg := validateSourceURL(sourceURL); msg != "" {
			flash = msg
		}
	}
	visKind := node.Visibility
	visGroupID := node.VisibilityGroupID
	groupID := node.GroupID
	if canChangeVisibility {
		// Non-authors (staff moderators) can only pick the
		// owner-independent scopes — public/connections/private — so we
		// validate against an empty groups slice, matching what the
		// form rendered. The author keeps the full picker.
		pickGroups := groups
		if !isAuthor {
			pickGroups = nil
		}
		var visErr string
		visKind, visGroupID, visErr = parseNodeVisibility(rawVisibility, node.CreatedBy, pickGroups)
		if flash == "" && visErr != "" {
			flash = visErr
		}
		groupID = visGroupID
	}
	// edit_policy is author-only: non-author edits silently keep the existing
	// value, so the form re-renders with the author's settings even after
	// a no-op submission from an editor.
	editPolicy := node.EditPolicy
	if isAuthor {
		if v, ok := parseActionPolicy(rawEditPolicy, node.EditPolicy); ok || rawEditPolicy == "" {
			editPolicy = v
		}
	}
	if flash != "" {
		formGroups := groups
		if !isAuthor {
			formGroups = nil
		}
		render(w, r, views.NodeEdit(viewerFor(user), node, flash, title, body, sourceURL, rawTags, rawVisibility, formGroups, isAuthor, canChangeVisibility, string(editPolicy)))
		return
	}

	var srcPtr *string
	if sourceURL != "" {
		srcPtr = &sourceURL
	}

	updated, err := s.queries.UpdateNode(r.Context(), db.UpdateNodeParams{
		ID:                id,
		Title:             title,
		Body:              body,
		SourceUrl:         srcPtr,
		Visibility:        visKind,
		VisibilityGroupID: visGroupID,
		GroupID:           groupID,
		EditPolicy:        editPolicy,
	})
	if err != nil {
		s.logger.Error("update node", "err", err)
		formGroups := groups
		if !isAuthor {
			formGroups = nil
		}
		render(w, r, views.NodeEdit(viewerFor(user), node, "Could not save changes. Please try again.", title, body, sourceURL, rawTags, rawVisibility, formGroups, isAuthor, canChangeVisibility, string(editPolicy)))
		return
	}
	s.logger.Info("node updated", "node_id", id, "slug", node.Slug, "type", node.Type, "user_id", user.ID, "is_author", isAuthor)
	if updated.Title != node.Title || updated.Body != node.Body {
		s.enqueueAudioForNode(r.Context(), updated)
	}
	newTags := parseTagsInput(rawTags)
	if err := s.setTagsForNode(r, id, newTags); err != nil {
		s.logger.Error("update node: set tags", "err", err)
	}
	// Skip the snapshot when nothing meaningful changed (e.g. user re-saved
	// the form without edits). GetLatestNodeRevision can't fail in normal
	// operation thanks to the migration backfill; if it does, snapshot anyway
	// so we don't silently lose a real edit.
	latest, lerr := s.queries.GetLatestNodeRevision(r.Context(), id)
	if lerr != nil || latest.Title != title || latest.Body != body || !sameStringSet(latest.TagNames, newTags) {
		s.snapshotNodeRevision(r.Context(), id, &user.ID, title, body, "", newTags)
	}
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/nodes/"+node.Slug)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

func (s *Server) handleEdgeNew(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if !s.requireEditPermission(w, r, node) {
		return
	}
	id := node.ID
	find := strings.TrimSpace(r.URL.Query().Get("find"))
	candidates, err := s.searchEdgeCandidates(r, id, find)
	if err != nil {
		s.logger.Error("edge new: search candidates", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	// htmx-driven Connect button opens the form in a modal; direct URL
	// hits (and no-JS clients) get the standalone full page.
	if isHTMX(r) {
		render(w, r, views.EdgeNewModal(node, "", "", "existing", find, "", candidates))
		return
	}
	render(w, r, views.EdgeNew(viewerFor(currentUser(r)), node, "", "", "existing", find, "", candidates))
}

// handleEdgePicker returns just the candidate-picker fragment, used by HTMX
// for live search-as-you-type. The full form lives at /edges/new; this
// endpoint serves only the part that needs to update on each keystroke.
func (s *Server) handleEdgePicker(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if !s.requireEditPermission(w, r, node) {
		return
	}
	id := node.ID
	find := strings.TrimSpace(r.URL.Query().Get("find"))
	candidates, err := s.searchEdgeCandidates(r, id, find)
	if err != nil {
		s.logger.Error("edge picker fragment: search candidates", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	render(w, r, views.CandidatePicker(find, candidates))
}

// searchEdgeCandidates returns the matches the picker shows. An empty or
// whitespace-only query returns no rows; the form's empty state tells the
// user to type to search. Otherwise the trigram %> operator handles the
// match — pg_trgm takes plain text, so user input goes through unchanged
// (no escaping needed and no operators to inject).
func (s *Server) searchEdgeCandidates(r *http.Request, sourceID uuid.UUID, query string) ([]views.EdgeCandidate, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	rows, err := s.queries.PickerSearchNodes(r.Context(), db.PickerSearchNodesParams{
		Query:    q,
		SourceID: sourceID,
		ViewerID: viewerID(r),
	})
	if err != nil {
		return nil, err
	}
	out := make([]views.EdgeCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.EdgeCandidate{ID: row.ID, Type: row.Type, Title: row.Title})
	}
	return out, nil
}

func (s *Server) handleEdgeCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	fromNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if !s.requireEditPermission(w, r, fromNode) {
		return
	}
	fromID := fromNode.ID
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	rawKind := strings.TrimSpace(r.PostFormValue("kind"))
	rawToID := strings.TrimSpace(r.PostFormValue("to_id"))
	rawToMode := strings.TrimSpace(r.PostFormValue("to_mode"))
	rawNewFindingTitle := strings.TrimSpace(r.PostFormValue("new_finding_title"))
	if rawToMode != "new" {
		rawToMode = "existing"
	}

	find := strings.TrimSpace(r.PostFormValue("find"))
	// rerender re-displays the form with a flash. When the request came
	// from the modal (HX-Request), respond with the modal fragment so
	// htmx swaps it back into #modal in place; otherwise re-render the
	// full page.
	rerender := func(flash string) {
		candidates, lerr := s.searchEdgeCandidates(r, fromID, find)
		if lerr != nil {
			s.logger.Error("edge create: search candidates", "err", lerr)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
		if isHTMX(r) {
			render(w, r, views.EdgeNewModal(fromNode, flash, rawKind, rawToMode, find, rawNewFindingTitle, candidates))
			return
		}
		render(w, r, views.EdgeNew(viewerFor(user), fromNode, flash, rawKind, rawToMode, find, rawNewFindingTitle, candidates))
	}

	ek := db.EdgeKind(rawKind)
	if !isUserPickableEdgeKind(ek) {
		rerender("Pick a valid relationship kind.")
		return
	}

	// Branch: pick an existing target, or create a new finding inline.
	// The "new" branch always produces a finding parented to fromNode and
	// inheriting its visibility/group; that scoping is the whole reason
	// we don't expose a free-form node creator at the same spot.
	var toID uuid.UUID
	var toNodeAuthor *uuid.UUID // set only for existing-node branch; used for notification
	if rawToMode == "new" {
		if rawNewFindingTitle == "" {
			rerender("Type a title for the new occurence.")
			return
		}
		if len(rawNewFindingTitle) > 200 {
			rerender("Occurence title is too long (max 200 characters).")
			return
		}
		newNode, createErr := s.createNodeWithUniqueSlug(r.Context(), slugify(rawNewFindingTitle), func(slug string) db.CreateNodeParams {
			return db.CreateNodeParams{
				Type:              db.NodeTypeFinding,
				Title:             rawNewFindingTitle,
				Body:              "",
				SourceUrl:         nil,
				CreatedBy:         user.ID,
				Slug:              slug,
				Visibility:        fromNode.Visibility,
				VisibilityGroupID: fromNode.VisibilityGroupID,
				GroupID:           fromNode.GroupID,
				ParentNodeID:      &fromID,
			}
		})
		if createErr != nil {
			s.logger.Error("edge create: new finding", "err", createErr)
			rerender("Could not create the occurence. Please try again.")
			return
		}
		s.logger.Info("node created", "node_id", newNode.ID, "slug", newNode.Slug, "type", newNode.Type, "user_id", user.ID, "via", "edge_inline", "parent_node_id", fromID)
		toID = newNode.ID
	} else {
		parsed, parseErr := uuid.Parse(rawToID)
		if parseErr != nil {
			rerender("Select a target node.")
			return
		}
		if parsed == fromID {
			rerender("A node cannot connect to itself.")
			return
		}
		toNode, getErr := s.queries.GetNode(r.Context(), parsed)
		if getErr != nil {
			rerender("Target node not found.")
			return
		}
		if visible, err := s.canViewNode(r.Context(), toNode, &user.ID); err != nil {
			s.logger.Error("edge create: target visibility", "err", err)
			rerender("Could not create connection. Please try again.")
			return
		} else if !visible {
			rerender("Target node not found.")
			return
		}
		author := toNode.CreatedBy
		toNodeAuthor = &author
		toID = parsed
	}

	_, err := s.queries.CreateEdge(r.Context(), db.CreateEdgeParams{
		FromNode:  fromID,
		ToNode:    toID,
		Kind:      ek,
		CreatedBy: user.ID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			rerender("That connection already exists.")
			return
		}
		s.logger.Error("edge create", "err", err)
		rerender("Could not create connection. Please try again.")
		return
	}
	if toNodeAuthor != nil {
		s.notifyBestEffort(r.Context(), *toNodeAuthor, user.ID, notifKindEdgeOnNode, &toID)
	}
	// On success from htmx, ask htmx to do a full page navigation back to
	// the node — that closes the modal and shows the new edge in the
	// connections list. Non-htmx submits get the usual 303 redirect.
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/nodes/"+fromNode.Slug)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/nodes/"+fromNode.Slug, http.StatusSeeOther)
}

// edgeOrder fixes the canonical kind sequence for the legend and pins active
// vs. passive labels for each kind. 'related' is symmetric so its passive
// form matches its active one and the two directions render in a single bucket.
// 'comments_on' is intentionally absent — comment edges are created by the
// comment flow, not the manual edge picker.
var edgeOrder = []struct {
	kind         db.EdgeKind
	activeLabel  string
	passiveLabel string
}{
	{db.EdgeKindSupports, "Supports", "Supported by"},
	{db.EdgeKindOpposes, "Opposes", "Opposed by"},
	{db.EdgeKindRelated, "Related", "Related"},
}

// isUserPickableEdgeKind reports whether the form value names an edge kind
// users may choose in the connection picker. comments_on is excluded
// because it is owned by the comment-creation path; allowing it here
// would let any user fabricate a comment edge without writing a comment.
func isUserPickableEdgeKind(k db.EdgeKind) bool {
	switch k {
	case db.EdgeKindSupports, db.EdgeKindOpposes, db.EdgeKindRelated:
		return true
	}
	return false
}

// displayGroups combines outgoing and incoming edges into one ordered list
// of legend sections. For asymmetric kinds the two directions become two
// separate sections with active and passive labels ("Supports" /
// "Supported by"). For 'related' (symmetric) both directions merge into a
// single "Relates to" section. Featured outgoing edges are excluded — they
// render inline in the "Key findings" section above the legend.
func displayGroups(out []db.ListEdgesFromNodeForViewerRow, in []db.ListEdgesToNodeForViewerRow) []views.EdgeGroup {
	var groups []views.EdgeGroup
	for _, ek := range edgeOrder {
		outRows := outgoingRowsOfKind(out, ek.kind)
		inRows := incomingRowsOfKind(in, ek.kind)

		if ek.kind == db.EdgeKindRelated {
			merged := append(append([]views.EdgeRow{}, outRows...), inRows...)
			if len(merged) > 0 {
				groups = append(groups, views.EdgeGroup{
					Kind:  ek.kind,
					Label: ek.activeLabel,
					Rows:  merged,
				})
			}
			continue
		}
		if len(outRows) > 0 {
			groups = append(groups, views.EdgeGroup{Kind: ek.kind, Label: ek.activeLabel, Rows: outRows})
		}
		if len(inRows) > 0 {
			groups = append(groups, views.EdgeGroup{Kind: ek.kind, Label: ek.passiveLabel, Rows: inRows})
		}
	}
	return groups
}

func outgoingRowsOfKind(rows []db.ListEdgesFromNodeForViewerRow, kind db.EdgeKind) []views.EdgeRow {
	var out []views.EdgeRow
	for _, row := range rows {
		// Skip edges this node has already highlighted from its FROM side
		// — they're rendered inline above the legend.
		if row.Kind != kind || row.Position != nil {
			continue
		}
		out = append(out, views.EdgeRow{
			EdgeID:   row.ID,
			Kind:     row.Kind,
			Outgoing: true,
			ID:       row.ToID,
			Slug:     row.ToSlug,
			Type:     row.ToType,
			Title:    row.ToTitle,
		})
	}
	return out
}

func incomingRowsOfKind(rows []db.ListEdgesToNodeForViewerRow, kind db.EdgeKind) []views.EdgeRow {
	var out []views.EdgeRow
	for _, row := range rows {
		// Same idea as outgoingRowsOfKind: hide edges that already appear
		// inline in the highlights section above.
		if row.Kind != kind || row.ToPosition != nil {
			continue
		}
		out = append(out, views.EdgeRow{
			EdgeID:   row.ID,
			Kind:     row.Kind,
			Outgoing: false,
			ID:       row.FromID,
			Slug:     row.FromSlug,
			Type:     row.FromType,
			Title:    row.FromTitle,
		})
	}
	return out
}

// highlightedRows turns the DB projection into the view-layer FeaturedRow,
// picking the active or passive label from edgeOrder based on the edge's
// direction relative to the current node so the card reads naturally
// ("Supports …" outgoing, "Supported by …" incoming).
func highlightedRows(rows []db.ListHighlightedEdgesForNodeRow) []views.FeaturedRow {
	active := make(map[db.EdgeKind]string, len(edgeOrder))
	passive := make(map[db.EdgeKind]string, len(edgeOrder))
	for _, ek := range edgeOrder {
		active[ek.kind] = ek.activeLabel
		passive[ek.kind] = ek.passiveLabel
	}
	out := make([]views.FeaturedRow, 0, len(rows))
	for _, row := range rows {
		label := active[row.Kind]
		if row.Direction == "incoming" {
			label = passive[row.Kind]
		}
		out = append(out, views.FeaturedRow{
			EdgeID: row.ID,
			Kind:   row.Kind,
			Label:  label,
			ID:     row.OtherID,
			Slug:   row.OtherSlug,
			Type:   row.OtherType,
			Title:  row.OtherTitle,
			Body:   row.OtherBody,
		})
	}
	return out
}
