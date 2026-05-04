package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mindful-social/mindful-social/internal/db"
	"github.com/mindful-social/mindful-social/internal/views"
)

// handleCommitForm renders the "Commit to this view" page: confirmation,
// optional reasoning picker drawn from the user's authored reasoning nodes.
func (s *Server) handleCommitForm(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	view, err := s.queries.GetNode(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("commit form: get node", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if view.Type != db.NodeTypeView {
		http.Error(w, "commitments only apply to view nodes", http.StatusBadRequest)
		return
	}

	reasonings, err := s.queries.ListReasoningsAuthoredBy(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("commit form: list reasonings", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.CommitForm(viewerFor(user), view, "", reasoningOptions(reasonings)))
}

func (s *Server) handleCommitCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	view, err := s.queries.GetNode(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("commit create: get node", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if view.Type != db.NodeTypeView {
		http.Error(w, "commitments only apply to view nodes", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rawReasoningID := strings.TrimSpace(r.PostFormValue("reasoning_id"))
	var reasoningPtr *uuid.UUID
	if rawReasoningID != "" {
		rid, err := uuid.Parse(rawReasoningID)
		if err != nil {
			s.rerenderCommitForm(w, r, user, view, "Pick a valid reasoning, or none.")
			return
		}
		reasoningPtr = &rid
	}

	_, err = s.queries.CreateCommitment(r.Context(), db.CreateCommitmentParams{
		UserID:      user.ID,
		ViewID:      id,
		ReasoningID: reasoningPtr,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Already committed — treat as success and bounce back to the view.
			http.Redirect(w, r, "/nodes/"+id.String(), http.StatusSeeOther)
			return
		}
		s.logger.Error("commit create", "err", err)
		s.rerenderCommitForm(w, r, user, view, "Could not save your commitment. Please try again.")
		return
	}
	http.Redirect(w, r, "/nodes/"+id.String(), http.StatusSeeOther)
}

func (s *Server) handleUncommit(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.DeleteCommitment(r.Context(), db.DeleteCommitmentParams{
		UserID: user.ID,
		ViewID: id,
	}); err != nil {
		s.logger.Error("uncommit", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+id.String(), http.StatusSeeOther)
}

func (s *Server) rerenderCommitForm(w http.ResponseWriter, r *http.Request, user *db.User, view db.Node, flash string) {
	reasonings, err := s.queries.ListReasoningsAuthoredBy(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("commit form rerender: list reasonings", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.CommitForm(viewerFor(user), view, flash, reasoningOptions(reasonings)))
}

func reasoningOptions(rows []db.ListReasoningsAuthoredByRow) []views.ReasoningOption {
	out := make([]views.ReasoningOption, 0, len(rows))
	for _, r := range rows {
		out = append(out, views.ReasoningOption{ID: r.ID, Title: r.Title})
	}
	return out
}
