package server

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	notifKindCommentOnNode  = "comment_on_node"
	notifKindReplyToComment = "reply_to_comment"
	notifKindEdgeOnNode     = "edge_on_node"
	notifKindPinOnNode      = "pin_on_node"
	notifKindNewFollower    = "new_follower"
)

func (s *Server) handleNotificationsIndex(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	notifs, err := s.queries.ListNotificationsForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("list notifications", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if err := s.queries.MarkAllNotificationsRead(r.Context(), user.ID); err != nil {
		s.logger.Warn("mark all notifications read", "err", err)
	}
	render(w, r, views.NotificationsPage(viewerFor(user), notifItems(notifs)))
}

func (s *Server) handleNotificationsCount(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	count, err := s.queries.CountUnreadNotifications(r.Context(), user.ID)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	render(w, r, views.NotificationBadge(count))
}

// notifyBestEffort inserts a notification after a successful mutation.
// Errors are only logged — the main action has already committed and must
// not be rolled back over a notification failure.
func (s *Server) notifyBestEffort(ctx context.Context, recipientID, actorID uuid.UUID, kind string, nodeID *uuid.UUID) {
	if recipientID == actorID {
		return
	}
	if err := s.queries.CreateNotification(ctx, db.CreateNotificationParams{
		RecipientID: recipientID,
		ActorID:     actorID,
		Kind:        kind,
		NodeID:      nodeID,
	}); err != nil {
		s.logger.Warn("create notification", "err", err, "kind", kind)
	}
}

func notifItems(rows []db.ListNotificationsForUserRow) []views.NotificationItem {
	items := make([]views.NotificationItem, len(rows))
	for i, row := range rows {
		items[i] = views.NotificationItem{
			ID:                    row.ID,
			Kind:                  row.Kind,
			NodeID:                row.NodeID,
			NodeTitle:             row.NodeTitle,
			NodeSlug:              row.NodeSlug,
			ActorID:               row.ActorID,
			ActorUsername:         row.ActorUsername,
			ActorProfileImagePath: row.ActorProfileImagePath,
			ReadAt:                row.ReadAt,
			CreatedAt:             row.CreatedAt,
		}
	}
	return items
}
