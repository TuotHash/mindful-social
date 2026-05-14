package views

import (
	"strings"
	"testing"
)

func TestRenderNodeMarkdownFormatsCommonMarkdown(t *testing.T) {
	html := renderNodeMarkdown("**bold** and *italic*\n\n- one\n- two")
	for _, want := range []string{
		"<strong>bold</strong>",
		"<em>italic</em>",
		"<ul>",
		"<li>one</li>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered markdown missing %q in %q", want, html)
		}
	}
}

func TestRenderNodeMarkdownSanitizesUnsafeHTML(t *testing.T) {
	html := renderNodeMarkdown(`<script>alert(1)</script> [bad](javascript:alert(1))`)
	for _, bad := range []string{"<script", "javascript:"} {
		if strings.Contains(strings.ToLower(html), bad) {
			t.Fatalf("rendered markdown should not contain %q: %q", bad, html)
		}
	}
}
