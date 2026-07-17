package views

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/a-h/templ"

	mindfulsocial "github.com/TuotHash/mindful-social"
)

// site-wide flags read by templates. These are set once at server startup
// from config; templates call the accessors so we don't have to thread a
// SiteFlags struct through every template signature.

var signupEnabledFlag = true
var aiEnabledFlag = false
var webSearchEnabledFlag = false
var staticAssetCache sync.Map

// SetSignupEnabled is called from server.New with the resolved config value.
// Templates check the result via SignupEnabled().
func SetSignupEnabled(b bool) { signupEnabledFlag = b }

// SignupEnabled reports whether the email+password signup form should be
// rendered. OAuth/SSO is unaffected by this flag.
func SignupEnabled() bool { return signupEnabledFlag }

// SetAIEnabled is called from server.New; true when an AI endpoint is
// configured. Templates check the result via AIEnabled() to show or hide the
// "Generate with AI" entry point.
func SetAIEnabled(b bool) { aiEnabledFlag = b }

// AIEnabled reports whether AI-assisted node drafting is available.
func AIEnabled() bool { return aiEnabledFlag }

// SetWebSearchEnabled is called from server.New; true when a SearXNG instance
// is configured. Templates check WebSearchEnabled() to show or hide the
// "Search the web" option on the generate form.
func SetWebSearchEnabled(b bool) { webSearchEnabledFlag = b }

// WebSearchEnabled reports whether AI drafts can be grounded in web search.
func WebSearchEnabled() bool { return webSearchEnabledFlag }

// Version returns the embedded build version string for display in the UI.
func Version() string { return mindfulsocial.Version }

// SourceURL is the canonical public location of the project source code,
// surfaced in the footer.
const SourceURL = "https://github.com/TuotHash/mindful-social"

// StaticAsset appends a content hash to mutable embedded assets so browsers
// don't keep using an old app.js/app.css after a deploy.
func StaticAsset(path string) templ.SafeURL {
	if cached, ok := staticAssetCache.Load(path); ok {
		return templ.SafeURL(cached.(string))
	}
	versioned := versionStaticAsset(path)
	actual, _ := staticAssetCache.LoadOrStore(path, versioned)
	return templ.SafeURL(actual.(string))
}

func versionStaticAsset(path string) string {
	name := strings.TrimPrefix(path, "/")
	b, err := mindfulsocial.StaticFS.ReadFile(name)
	if err != nil {
		return path
	}
	sum := sha256.Sum256(b)
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "v=" + hex.EncodeToString(sum[:])[:12]
}
