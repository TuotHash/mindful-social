package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const maxCommentBodyRunes = 5000

func (s *Server) handleCommentCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if node.Type != db.NodeTypeView {
		http.Error(w, "comments can only be posted on views", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	if msg := validateCommentBody(body); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	parentID, ok := parseOptionalCommentParent(w, r)
	if !ok {
		return
	}

	comment, err := s.queries.CreateComment(r.Context(), db.CreateCommentParams{
		NodeID:   node.ID,
		ParentID: parentID,
		AuthorID: user.ID,
		Body:     body,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "invalid comment target", http.StatusBadRequest)
			return
		}
		s.logger.Error("create comment", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug+"#comment-"+comment.ID.String(), http.StatusSeeOther)
}

func (s *Server) handleCommentEdit(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, comment, ok := s.resolveComment(w, r)
	if !ok {
		return
	}
	if !canEditComment(comment.AuthorID, comment.DeletedAt, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	render(w, r, views.CommentEdit(viewerFor(user), node, commentForEdit(comment), "", comment.Body))
}

func (s *Server) handleCommentUpdate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, comment, ok := s.resolveComment(w, r)
	if !ok {
		return
	}
	if !canEditComment(comment.AuthorID, comment.DeletedAt, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	if msg := validateCommentBody(body); msg != "" {
		render(w, r, views.CommentEdit(viewerFor(user), node, commentForEdit(comment), msg, body))
		return
	}
	updated, err := s.queries.UpdateComment(r.Context(), db.UpdateCommentParams{
		ID:       comment.ID,
		NodeID:   node.ID,
		AuthorID: user.ID,
		Body:     body,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("update comment", "err", err)
		render(w, r, views.CommentEdit(viewerFor(user), node, commentForEdit(comment), "Could not save comment. Please try again.", body))
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug+"#comment-"+updated.ID.String(), http.StatusSeeOther)
}

func (s *Server) handleCommentDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, comment, ok := s.resolveComment(w, r)
	if !ok {
		return
	}
	if !canDeleteComment(comment.AuthorID, comment.DeletedAt, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var (
		rows int64
		err  error
	)
	if isStaff(user) {
		rows, err = s.queries.HardDeleteComment(r.Context(), db.HardDeleteCommentParams{
			ID:     comment.ID,
			NodeID: node.ID,
		})
	} else {
		rows, err = s.queries.SoftDeleteComment(r.Context(), db.SoftDeleteCommentParams{
			ID:       comment.ID,
			NodeID:   node.ID,
			AuthorID: user.ID,
		})
	}
	if err != nil {
		s.logger.Error("delete comment", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug+"#comments", http.StatusSeeOther)
}

func (s *Server) resolveComment(w http.ResponseWriter, r *http.Request) (db.Node, db.GetCommentForNodeRow, bool) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return db.Node{}, db.GetCommentForNodeRow{}, false
	}
	commentID, err := uuid.Parse(chiURLParam(r, "commentID"))
	if err != nil {
		http.NotFound(w, r)
		return db.Node{}, db.GetCommentForNodeRow{}, false
	}
	comment, err := s.queries.GetCommentForNode(r.Context(), db.GetCommentForNodeParams{
		ID:     commentID,
		NodeID: node.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return db.Node{}, db.GetCommentForNodeRow{}, false
		}
		s.logger.Error("resolve comment", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return db.Node{}, db.GetCommentForNodeRow{}, false
	}
	return node, comment, true
}

func parseOptionalCommentParent(w http.ResponseWriter, r *http.Request) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(r.PostFormValue("parent_id"))
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, "invalid parent comment", http.StatusBadRequest)
		return nil, false
	}
	return &id, true
}

func validateCommentBody(body string) string {
	switch {
	case body == "":
		return "Comment is required."
	case len([]rune(body)) > maxCommentBodyRunes:
		return "Comment is too long (max 5000 characters)."
	}
	return ""
}

func canEditComment(authorID uuid.UUID, deletedAt pgtype.Timestamptz, user *db.User) bool {
	return user != nil && user.ID == authorID && !deletedAt.Valid
}

func canDeleteComment(authorID uuid.UUID, deletedAt pgtype.Timestamptz, user *db.User) bool {
	if user == nil {
		return false
	}
	if isStaff(user) {
		return true
	}
	return user.ID == authorID && !deletedAt.Valid
}

func commentThreadFromRows(rows []db.ListCommentsForNodeRow, user *db.User) ([]views.Comment, int64) {
	byID := make(map[uuid.UUID]views.Comment, len(rows))
	var rootOrder []uuid.UUID
	repliesByParent := make(map[uuid.UUID][]uuid.UUID)
	var visibleCount int64

	for _, row := range rows {
		if !row.DeletedAt.Valid {
			visibleCount++
		}
		byID[row.ID] = views.Comment{
			ID:                     row.ID,
			AuthorID:               row.AuthorID,
			AuthorUsername:         row.AuthorUsername,
			AuthorProfileImagePath: row.AuthorProfileImagePath,
			Body:                   row.Body,
			CreatedAt:              row.CreatedAt,
			EditedAt:               row.EditedAt,
			DeletedAt:              row.DeletedAt,
			CanEdit:                canEditComment(row.AuthorID, row.DeletedAt, user),
			CanDelete:              canDeleteComment(row.AuthorID, row.DeletedAt, user),
		}
		if row.ParentID == nil {
			rootOrder = append(rootOrder, row.ID)
			continue
		}
		repliesByParent[*row.ParentID] = append(repliesByParent[*row.ParentID], row.ID)
	}

	out := make([]views.Comment, 0, len(rootOrder))
	for _, id := range rootOrder {
		root, ok := byID[id]
		if !ok {
			continue
		}
		for _, replyID := range repliesByParent[id] {
			if reply, ok := byID[replyID]; ok {
				root.Replies = append(root.Replies, reply)
			}
		}
		out = append(out, root)
	}
	return out, visibleCount
}

func commentForEdit(row db.GetCommentForNodeRow) views.Comment {
	return views.Comment{
		ID:                     row.ID,
		AuthorID:               row.AuthorID,
		AuthorUsername:         row.AuthorUsername,
		AuthorProfileImagePath: row.AuthorProfileImagePath,
		Body:                   row.Body,
		CreatedAt:              row.CreatedAt,
		EditedAt:               row.EditedAt,
		DeletedAt:              row.DeletedAt,
	}
}

func topicViewsFromRows(rows []db.ListViewsForTopicRow) []views.TopicView {
	out := make([]views.TopicView, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.TopicView{
			ID:                     row.ID,
			Slug:                   row.Slug,
			Title:                  row.Title,
			Body:                   row.Body,
			CreatedAt:              row.CreatedAt,
			AuthorUsername:         row.AuthorUsername,
			AuthorProfileImagePath: row.AuthorProfileImagePath,
			CommentCount:           row.CommentCount,
		})
	}
	return out
}
