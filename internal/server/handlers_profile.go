package server

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const profileNodesLimit = 25

// handleProfile renders /users/{username}: a public profile showing the
// user's authored nodes and pinned commitments. Sign-in methods live on
// the account page now — this view is for what the user is publicly
// committed to, nothing private.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	username := chiURLParam(r, "username")
	if username == "" {
		http.NotFound(w, r)
		return
	}
	user, err := s.queries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("profile: get user", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	authored, err := s.queries.ListNodesAuthoredByForViewer(r.Context(), db.ListNodesAuthoredByForViewerParams{
		AuthorID:    user.ID,
		ViewerID:    viewerID(r),
		ResultLimit: profileNodesLimit,
	})
	if err != nil {
		s.logger.Error("profile: list authored", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	pins, err := s.queries.ListPinsByUser(r.Context(), db.ListPinsByUserParams{
		UserID:   user.ID,
		ViewerID: viewerID(r),
	})
	if err != nil {
		s.logger.Error("profile: list pins", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	viewer := currentUser(r)
	isSelf := viewer != nil && viewer.ID == user.ID

	followers, err := s.queries.CountFollowers(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("profile: count followers", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	following, err := s.queries.CountFollowing(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("profile: count following", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	relation := views.FollowRelation{Followers: followers, Following: following}
	if viewer != nil && !isSelf {
		state, err := s.queries.GetFollowState(r.Context(), db.GetFollowStateParams{
			ViewerID:  viewer.ID,
			ProfileID: user.ID,
		})
		if err != nil {
			s.logger.Error("profile: follow state", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		relation.ViewerFollows = state.ViewerFollows
		relation.FollowsViewer = state.FollowsViewer
	}

	pinRowsOut := pinRows(pins)

	render(w, r, views.Profile(
		viewerFor(viewer),
		user,
		isSelf,
		authored,
		pinRowsOut,
		relation,
	))
}

// pinRows turns DB pin rows into view rows. Pins are now stance-only;
// any evidence the pinner wants to show lives in the typed-edge graph.
func pinRows(rows []db.ListPinsByUserRow) []views.PinRow {
	out := make([]views.PinRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.PinRow{
			NodeID:    row.NodeID,
			NodeSlug:  row.NodeSlug,
			NodeType:  row.NodeType,
			NodeTitle: row.NodeTitle,
			Kind:      row.Kind,
		})
	}
	return out
}

// identityLabel turns a provider key into the short, user-friendly name
// shown on the account page (e.g. "password" → "Password", "oidc:work"
// → "Work via OIDC"). Never exposes the raw secret/subject.
func identityLabel(provider string) string {
	switch provider {
	case "password":
		return "Password"
	case "google":
		return "Google"
	case "github":
		return "GitHub"
	}
	if len(provider) >= 5 && provider[:5] == "oidc:" {
		key := provider[5:]
		if key == "" {
			return "OIDC"
		}
		// Title-case the key: "work" → "Work".
		head := key[0]
		if head >= 'a' && head <= 'z' {
			head -= 'a' - 'A'
		}
		return string(head) + key[1:] + " via OIDC"
	}
	return provider
}
