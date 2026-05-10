package views

// site-wide flags read by templates. These are set once at server startup
// from config; templates call the accessors so we don't have to thread a
// SiteFlags struct through every template signature.

var signupEnabledFlag = true

// SetSignupEnabled is called from server.New with the resolved config value.
// Templates check the result via SignupEnabled().
func SetSignupEnabled(b bool) { signupEnabledFlag = b }

// SignupEnabled reports whether the email+password signup form should be
// rendered. OAuth/SSO is unaffected by this flag.
func SignupEnabled() bool { return signupEnabledFlag }
