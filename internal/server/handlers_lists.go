package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const maxListNameLen = 60

func (s *Server) handleListsIndex(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := s.queries.ListAudienceLists(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("lists index", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	summaries, err := s.summarizeLists(r, rows)
	if err != nil {
		s.logger.Error("lists index: summarise", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.ListsIndex(viewerFor(user), "", summaries))
}

// handleListCreate creates a custom list. Trusted lists exist already and
// can't be created from the form.
func (s *Server) handleListCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	rerender := func(flash string) {
		rows, err := s.queries.ListAudienceLists(r.Context(), user.ID)
		if err != nil {
			s.logger.Error("lists create rerender", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		summaries, err := s.summarizeLists(r, rows)
		if err != nil {
			s.logger.Error("lists create rerender: summarise", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		render(w, r, views.ListsIndex(viewerFor(user), flash, summaries))
	}
	switch {
	case name == "":
		rerender("Pick a name for the list.")
		return
	case len(name) > maxListNameLen:
		rerender("List name is too long.")
		return
	case strings.EqualFold(name, "Trusted"):
		rerender("Trusted is reserved for the built-in list.")
		return
	}
	if _, err := s.queries.CreateAudienceList(r.Context(), db.CreateAudienceListParams{
		OwnerID:   user.ID,
		Name:      name,
		IsTrusted: false,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			rerender("You already have a list with that name.")
			return
		}
		s.logger.Error("lists create", "err", err)
		rerender("Could not create list. Please try again.")
		return
	}
	http.Redirect(w, r, "/lists", http.StatusSeeOther)
}

// handleListDetail shows members of a list with a form to add by username.
func (s *Server) handleListDetail(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list, ok := s.resolveOwnedList(w, r, user)
	if !ok {
		return
	}
	members, err := s.queries.ListListMembers(r.Context(), list.ID)
	if err != nil {
		s.logger.Error("list detail: members", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.ListDetail(viewerFor(user), "", list, members))
}

func (s *Server) handleListAddMember(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list, ok := s.resolveOwnedList(w, r, user)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	rerender := func(flash string) {
		members, err := s.queries.ListListMembers(r.Context(), list.ID)
		if err != nil {
			s.logger.Error("list add member rerender: members", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		render(w, r, views.ListDetail(viewerFor(user), flash, list, members))
	}
	if username == "" {
		rerender("Enter a username.")
		return
	}
	target, err := s.queries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			rerender("No user with that username.")
			return
		}
		s.logger.Error("list add member: lookup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if target.ID == user.ID {
		rerender("You can't add yourself to your own list.")
		return
	}
	if err := s.queries.AddListMember(r.Context(), db.AddListMemberParams{
		ListID:       list.ID,
		MemberUserID: target.ID,
	}); err != nil {
		s.logger.Error("list add member", "err", err)
		rerender("Could not add member. Please try again.")
		return
	}
	http.Redirect(w, r, "/lists/"+list.ID.String(), http.StatusSeeOther)
}

func (s *Server) handleListRemoveMember(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list, ok := s.resolveOwnedList(w, r, user)
	if !ok {
		return
	}
	memberID, err := uuid.Parse(chiURLParam(r, "userID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.RemoveListMember(r.Context(), db.RemoveListMemberParams{
		ListID:       list.ID,
		MemberUserID: memberID,
	}); err != nil {
		s.logger.Error("list remove member", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/lists/"+list.ID.String(), http.StatusSeeOther)
}

func (s *Server) handleListDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list, ok := s.resolveOwnedList(w, r, user)
	if !ok {
		return
	}
	if list.IsTrusted {
		http.Error(w, "trusted list cannot be deleted", http.StatusBadRequest)
		return
	}
	if err := s.queries.DeleteAudienceList(r.Context(), db.DeleteAudienceListParams{
		ID:      list.ID,
		OwnerID: user.ID,
	}); err != nil {
		// Foreign-key restriction: list is referenced by a node's visibility.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			http.Error(w, "list is in use as a node's visibility — change the node first", http.StatusConflict)
			return
		}
		s.logger.Error("list delete", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/lists", http.StatusSeeOther)
}

// resolveOwnedList parses the {id} URL parameter and verifies that the
// list belongs to the current user. Returns ok=false (response written) on
// any failure path, including ownership mismatch.
func (s *Server) resolveOwnedList(w http.ResponseWriter, r *http.Request, user *db.User) (db.AudienceList, bool) {
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return db.AudienceList{}, false
	}
	list, err := s.queries.GetAudienceList(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return db.AudienceList{}, false
		}
		s.logger.Error("resolve list", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return db.AudienceList{}, false
	}
	if list.OwnerID != user.ID {
		http.NotFound(w, r) // hide existence from non-owners
		return db.AudienceList{}, false
	}
	return list, true
}

// summarizeLists attaches member counts to each list row so the index page
// can show "Trusted — 4 members" without an N+1.
func (s *Server) summarizeLists(r *http.Request, rows []db.AudienceList) ([]views.ListSummary, error) {
	out := make([]views.ListSummary, 0, len(rows))
	for _, l := range rows {
		count, err := s.queries.CountListMembers(r.Context(), l.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, views.ListSummary{List: l, MemberCount: count})
	}
	return out, nil
}
