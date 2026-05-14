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
	html := renderNodeMarkdown("<script>alert(1)</script>\n\n[bad](javascript:alert(1))")
	lower := strings.ToLower(html)
	// Disallowed tags get stripped wholesale, so any leftover `<script`
	// (whether self-closing, attributed, or partially eaten) is a leak.
	if strings.Contains(lower, "<script") {
		t.Fatalf("rendered markdown should not contain a <script tag: %q", html)
	}
	// The dangerous pattern is `javascript:` reaching an attribute that
	// the browser will execute — href/src/etc. Plain-text occurrences in
	// the document body are inert.
	for _, dangerous := range []string{`href="javascript:`, `src="javascript:`} {
		if strings.Contains(lower, dangerous) {
			t.Fatalf("rendered markdown should not contain %q: %q", dangerous, html)
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
	if strings.Contains(strings.ToLower(html), `src="javascript:`) {
		t.Fatalf("rendered markdown should not contain a javascript: image src: %q", html)
	}
}

func TestRenderNodeMarkdownRendersUploadedVideo(t *testing.T) {
	html := renderNodeMarkdown(`<video controls src="/uploads/topics/abc/def.mp4"></video>`)
	for _, want := range []string{
		`<video`,
		`controls`,
		`src="/uploads/topics/abc/def.mp4"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered markdown missing %q in %q", want, html)
		}
	}
}

func TestRenderNodeMarkdownStripsVideoEventHandlers(t *testing.T) {
	html := renderNodeMarkdown(`<video controls src="/uploads/topics/abc/def.mp4" onerror="alert(1)"></video>`)
	if strings.Contains(strings.ToLower(html), "onerror") {
		t.Fatalf("rendered markdown should not contain onerror: %q", html)
	}
}
