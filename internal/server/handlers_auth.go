package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/TuotHash/mindful-social/internal/auth"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	oauthStateKey   = "oauth_state"
	oauthReturnKey  = "oauth_return"
)

// oauthButtons projects the configured providers into the small struct
// templates render. Computed per-request so the template never sees the
// auth.Provider interface.
func (s *Server) oauthButtons() []views.OAuthButton {
	all := s.oauth.All()
	if len(all) == 0 {
		return nil
	}
	out := make([]views.OAuthButton, 0, len(all))
	for _, p := range all {
		out = append(out, views.OAuthButton{Key: p.Key(), Label: p.Label()})
	}
	return out
}

func (s *Server) handleSignupGet(w http.ResponseWriter, r *http.Request) {
	render(w, r, views.Signup("", s.oauthButtons(), "", ""))
}

func (s *Server) handleSignupPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.PostFormValue("username")
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")

	user, err := s.authSvc.SignupWithPassword(r.Context(), username, email, password)
	if err != nil {
		render(w, r, views.Signup(humanizeAuthErr(err), s.oauthButtons(), username, email))
		return
	}

	if err := auth.LoginUser(r.Context(), s.sessions, user.ID); err != nil {
		s.logger.Error("signup: login", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	render(w, r, views.Login("", s.oauthButtons(), ""))
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")

	id, err := s.authSvc.AuthenticatePassword(r.Context(), email, password)
	if err != nil {
		render(w, r, views.Login(humanizeAuthErr(err), s.oauthButtons(), email))
		return
	}

	if err := auth.LoginUser(r.Context(), s.sessions, id); err != nil {
		s.logger.Error("login: session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := auth.LogoutUser(r.Context(), s.sessions); err != nil {
		s.logger.Warn("logout", "err", err)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleOAuthStart kicks off the redirect-to-IdP dance. We stash a CSRF state
// in the session (signed cookie via scs) so the callback can verify the user
// is the one who started the flow.
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	key := chiURLParam(r, "provider")
	prov, ok := s.oauth.Get(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, err := auth.NewState()
	if err != nil {
		s.logger.Error("oauth start: state", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.sessions.Put(r.Context(), oauthStateKey, state)
	http.Redirect(w, r, prov.AuthURL(state), http.StatusSeeOther)
}

// handleOAuthCallback validates state, exchanges the code, and either logs in
// the matching user or creates one. The Identity → user mapping (link by
// email, fall through to fresh signup) lives in auth.Service.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	key := chiURLParam(r, "provider")
	prov, ok := s.oauth.Get(key)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		s.logger.Warn("oauth callback: provider error", "provider", key, "error", errParam, "desc", r.URL.Query().Get("error_description"))
		render(w, r, views.Login("Sign-in was cancelled or failed at the provider.", s.oauthButtons(), ""))
		return
	}

	wantState := s.sessions.GetString(r.Context(), oauthStateKey)
	gotState := r.URL.Query().Get("state")
	s.sessions.Remove(r.Context(), oauthStateKey)
	if wantState == "" || wantState != gotState {
		render(w, r, views.Login("OAuth state mismatch. Please try again.", s.oauthButtons(), ""))
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		render(w, r, views.Login("OAuth response was missing a code.", s.oauthButtons(), ""))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), oauthExchangeTimeout)
	defer cancel()
	ident, err := prov.Identify(ctx, code)
	if err != nil {
		s.logger.Error("oauth callback: identify", "provider", key, "err", err)
		render(w, r, views.Login("Could not complete sign-in with that provider.", s.oauthButtons(), ""))
		return
	}

	user, err := s.authSvc.FindOrCreateOAuthUser(r.Context(), key, ident)
	if err != nil {
		s.logger.Error("oauth callback: find/create user", "provider", key, "err", err)
		render(w, r, views.Login("Could not finish sign-in. Please try again.", s.oauthButtons(), ""))
		return
	}

	if err := auth.LoginUser(r.Context(), s.sessions, user.ID); err != nil {
		s.logger.Error("oauth callback: login", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// humanizeAuthErr maps known auth errors to user-facing copy. Anything we
// don't recognise becomes a generic message — the real error is already
// logged by the caller path.
func humanizeAuthErr(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidLogin):
		return "Invalid email or password."
	case errors.Is(err, auth.ErrUserExists):
		return "A user with that email or username already exists."
	case errors.Is(err, auth.ErrInvalidUsername):
		return "Username must be 3–32 characters: letters, digits, dot, dash, underscore."
	case errors.Is(err, auth.ErrInvalidEmail):
		return "That doesn't look like a valid email."
	case errors.Is(err, auth.ErrPasswordTooShort):
		return "Password must be at least 12 characters."
	case errors.Is(err, auth.ErrPasswordTooLong):
		return "Password is too long."
	}
	return "Something went wrong. Please try again."
}
