package views

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	rendererhtml "github.com/yuin/goldmark/renderer/html"
)

var nodeMarkdown = goldmark.New(
	goldmark.WithExtensions(
		extension.Linkify,
		extension.Strikethrough,
		extension.Table,
	),
	goldmark.WithRendererOptions(
		rendererhtml.WithHardWraps(),
		// Let raw HTML through so `<video controls src="…">` from the
		// editor's video-upload pipeline survives goldmark. The
		// bluemonday policy below is the actual safety net — only the
		// explicit allow-list of tags and attributes makes it out.
		rendererhtml.WithUnsafe(),
	),
)

var nodeMarkdownPolicy = newNodeMarkdownPolicy()

// strictTextPolicy strips every tag and attribute, keeping only the inner
// text. Used by NodePlainExcerpt to turn a markdown body into a clean
// teaser snippet.
var strictTextPolicy = bluemonday.StrictPolicy()

func newNodeMarkdownPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.AllowAttrs("href", "title").OnElements("a")
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	// Inline images come from the editor's upload pipeline (relative
	// /uploads/topics/... or /uploads/drafts/...) or any http(s) source
	// the author types in. Width/height stay locked to CSS; only src,
	// alt and title are exposed.
	p.AllowAttrs("src", "alt", "title").OnElements("img")
	// Inline videos come from the editor's video-upload pipeline, which
	// produces /uploads/topics/... or /uploads/drafts/... mp4s.
	// Restricting the attribute surface keeps the rendered HTML close to
	// what the editor inserts and blocks drive-by exploits via crafted
	// markdown.
	p.AllowAttrs("src", "controls", "playsinline", "preload", "poster").OnElements("video")
	p.AllowElements(
		"a",
		"blockquote",
		"br",
		"code",
		"del",
		"em",
		"h2",
		"h3",
		"h4",
		"h5",
		"h6",
		"hr",
		"img",
		"li",
		"ol",
		"p",
		"pre",
		"strong",
		"table",
		"tbody",
		"td",
		"th",
		"thead",
		"tr",
		"ul",
		"video",
	)
	return p
}

func nodeMarkdownHasText(source string) bool {
	return strings.TrimSpace(source) != ""
}

func renderNodeMarkdown(source string) string {
	if !nodeMarkdownHasText(source) {
		return ""
	}
	var buf bytes.Buffer
	if err := nodeMarkdown.Convert([]byte(source), &buf); err != nil {
		return "<p>" + html.EscapeString(source) + "</p>"
	}
	return nodeMarkdownPolicy.Sanitize(buf.String())
}

func NodeMarkdown(source string) templ.Component {
	return templ.Raw(renderNodeMarkdown(source))
}

// mdImageRE matches inline markdown images: ![alt](url) or ![alt](url "title").
// Group 1 captures the URL (no whitespace, no closing paren).
var mdImageRE = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)`)

// NodeFirstImageURL returns the URL of the first inline image in the markdown
// source, or "" if none is found. It matches directly against the raw markdown
// so no rendering pass is needed.
func NodeFirstImageURL(source string) string {
	m := mdImageRE.FindStringSubmatch(source)
	if m == nil {
		return ""
	}
	u := m[1]
	if !strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return ""
	}
	return u
}

// NodePlainExcerpt renders the markdown body to HTML, strips every tag, and
// collapses whitespace so callers get a plain-text snippet suitable for a
// teaser line. The result is truncated to maxRunes (counted as runes, not
// bytes) so a multi-byte UTF-8 body doesn't get cut mid-codepoint, and an
// ellipsis is appended when truncation actually happens. Visual line-clamp
// still happens in CSS — the server-side cap is only there to avoid sending
// a megabyte of body across the wire for a small card.
func NodePlainExcerpt(source string, maxRunes int) string {
	if !nodeMarkdownHasText(source) {
		return ""
	}
	var buf bytes.Buffer
	rendered := source
	if err := nodeMarkdown.Convert([]byte(source), &buf); err == nil {
		rendered = buf.String()
	}
	text := strictTextPolicy.Sanitize(rendered)
	text = html.UnescapeString(text)
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return strings.TrimRight(string(runes[:maxRunes]), " ") + "…"
}
