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

func TestRenderNodeMarkdownRendersUploadedImage(t *testing.T) {
	html := renderNodeMarkdown(`![caption](/uploads/topics/abc/def.jpg)`)
	for _, want := range []string{
		`<img`,
		`src="/uploads/topics/abc/def.jpg"`,
		`alt="caption"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered markdown missing %q in %q", want, html)
		}
	}
}

func TestRenderNodeMarkdownStripsUnsafeImageScheme(t *testing.T) {
	html := renderNodeMarkdown(`![x](javascript:alert(1))`)
	if strings.Contains(strings.ToLower(html), "javascript:") {
		t.Fatalf("rendered markdown should not contain a javascript: image src: %q", html)
	}
}
