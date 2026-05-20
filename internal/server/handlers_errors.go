package server

import (
	"net/http"
	"strings"

	"github.com/TuotHash/mindful-social/internal/views"
)

// errorCopy is the short headline + supporting body rendered on the styled
// error page. Kept beside the page so the wording stays consistent across
// every caller — handlers shouldn't be inventing 404 microcopy.
type errorCopy struct {
	headline string
	body     string
}

var errorCopies = map[int]errorCopy{
	http.StatusBadRequest: {
		"Something looked off in that request.",
		"The form data or URL didn't quite parse. Try again — and if it keeps happening, the link may be broken.",
	},
	http.StatusUnauthorized: {
		"You'll need to sign in for that.",
		"This corner of the site is only visible to signed-in members. Sign in and we'll bring you right back.",
	},
	http.StatusForbidden: {
		"That's not yours to open.",
		"You're signed in, but this page or action isn't available to your account. If you think that's a mistake, the author or an admin can adjust the audience.",
	},
	http.StatusNotFound: {
		"We couldn't find that page.",
		"The link may be old, the node may have been removed, or it might never have existed. The graph keeps growing — try browsing for what you were after.",
	},
	http.StatusMethodNotAllowed: {
		"That action isn't allowed here.",
		"The URL exists, but it doesn't accept this kind of request. If you got here from a form or button, please report it.",
	},
	http.StatusGone: {
		"This page is gone.",
		"The content used to live here, but it has been removed for good. Nothing else to see — try the homepage or the graph.",
	},
	http.StatusTooManyRequests: {
		"Slow down for a moment.",
		"You've made a lot of requests in a short time. Give it a minute and try again.",
	},
	http.StatusInternalServerError: {
		"Something went wrong on our side.",
		"This one is on us — the server hit an unexpected error. The team has been notified; please try again in a moment.",
	},
	http.StatusBadGateway: {
		"We can't reach part of the service right now.",
		"An upstream dependency isn't responding. Please try again in a minute.",
	},
	http.StatusServiceUnavailable: {
		"The service is taking a quick break.",
		"We're either under maintenance or briefly overloaded. Try again in a moment.",
	},
	http.StatusGatewayTimeout: {
		"That took too long.",
		"The server didn't get a response in time. Please retry — and if it keeps timing out, something deeper may be wrong.",
	},
}

// errorCopyFor returns wording for an HTTP status, falling back to a sensible
// generic message so unusual codes still render a coherent page.
func errorCopyFor(status int) errorCopy {
	if c, ok := errorCopies[status]; ok {
		return c
	}
	if status >= 500 {
		return errorCopies[http.StatusInternalServerError]
	}
	return errorCopies[http.StatusBadRequest]
}

// renderError writes the styled error page. For htmx requests we return a
// short plain-text response so a failed swap doesn't paint a full error page
// inside a partial. Same for the /uploads/, /static/, and other non-HTML
// endpoints, which never accept text/html in practice.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int) {
	if wantsPlainError(r) {
		http.Error(w, http.StatusText(status), status)
		return
	}
	c := errorCopyFor(status)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := views.ErrorPage(viewerFor(currentUser(r)), status, c.headline, c.body).Render(r.Context(), w); err != nil {
		s.logger.Error("render error page", "err", err, "status", status, "path", r.URL.Path)
	}
}

// wantsPlainError reports whether the caller is better served by a plain
// http.Error response than by the styled HTML page — htmx swaps and clients
// that explicitly ask for JSON both fall under this.
func wantsPlainError(r *http.Request) bool {
	if isHTMX(r) {
		return true
	}
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	// Cheap check — if the client clearly wants JSON and not HTML, don't
	// shove a full HTML document at it.
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

// notFound renders the styled 404. Replaces http.NotFound at HTML call sites.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound)
}

// forbidden renders the styled 403. Used when an authenticated user lacks
// permission for a resource they can see exists.
func (s *Server) forbidden(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusForbidden)
}

// badRequest renders the styled 400 for malformed input that gets past basic
// parsing but is otherwise unusable.
func (s *Server) badRequest(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusBadRequest)
}

// methodNotAllowed renders the styled 405. Chi wires this to its router for
// requests that match a path but use the wrong method.
func (s *Server) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusMethodNotAllowed)
}

// internalError logs the underlying error and renders the styled 500. The
// op label flows into the structured log so the source of the failure is
// recoverable even though it isn't surfaced to the user.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	s.logger.Error(op, "err", err, "path", r.URL.Path)
	s.renderError(w, r, http.StatusInternalServerError)
}
