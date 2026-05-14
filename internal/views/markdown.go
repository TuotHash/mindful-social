package views

import (
	"bytes"
	"html"
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
	goldmark.WithRendererOptions(rendererhtml.WithHardWraps()),
)

var nodeMarkdownPolicy = newNodeMarkdownPolicy()

func newNodeMarkdownPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.AllowAttrs("href", "title").OnElements("a")
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	// Inline images come from the editor's upload pipeline (relative
	// /uploads/topics/<root>/<file>.<ext>) or any http(s) source the
	// author types in. Width/height stay locked to CSS; only src, alt
	// and title are exposed.
	p.AllowAttrs("src", "alt", "title").OnElements("img")
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
