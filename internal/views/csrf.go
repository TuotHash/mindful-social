package views

import "context"

// CSRFFieldName is the form field name gorilla/csrf looks for. Kept in sync
// with the library default so templates can render the hidden input without
// importing gorilla/csrf.
const CSRFFieldName = "gorilla.csrf.Token"

type csrfCtxKey struct{}

// WithCSRFToken stashes the per-request CSRF token in ctx. The server-side
// bridge middleware calls this so templates can read the token via
// CSRFToken(ctx) without holding onto an *http.Request.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfCtxKey{}, token)
}

// CSRFToken pulls the request's CSRF token out of ctx. Returns "" when no
// token was stashed (e.g. tests rendering templates without the middleware
// chain); templates that depend on it should be reached via the real
// router in production.
func CSRFToken(ctx context.Context) string {
	t, _ := ctx.Value(csrfCtxKey{}).(string)
	return t
}
