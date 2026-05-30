package audio

import (
	"strings"
	"testing"
)

func TestPlanChunks_ShortPost(t *testing.T) {
	text := "A short reflection. Nothing special, just a sentence or two."
	chunks := PlanChunks(text)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for a short post, got %d", len(chunks))
	}
	if chunks[0].CharStart != 0 || chunks[0].CharEnd != len(text) {
		t.Fatalf("chunk should cover whole text: got [%d,%d) of %d",
			chunks[0].CharStart, chunks[0].CharEnd, len(text))
	}
}

func TestPlanChunks_LongPostSplits(t *testing.T) {
	para := strings.Repeat("word ", 200) // ~80s of audio per paragraph
	text := strings.Join([]string{para, para, para, para, para, para, para}, "\n\n")
	chunks := PlanChunks(text)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for a long post, got %d", len(chunks))
	}
	// First chunk should be roughly 2 min — that's the target.
	if chunks[0].EstMs < chunkTargetMs {
		t.Fatalf("first chunk under target: %dms < %dms", chunks[0].EstMs, chunkTargetMs)
	}
}

func TestPlanChunks_FoldsTinyTail(t *testing.T) {
	// 4:02-ish: build a post that without folding would emit a tiny tail.
	// 7 paragraphs of 50 words each = 350 words ≈ 2:20 (mid-zone) so just
	// above the 5-min threshold... actually we need to be above 5 min.
	long := strings.Repeat(strings.Repeat("word ", 100)+"\n\n", 8) // 800w ≈ 5:20
	// Append a tiny tail paragraph: 5 words ≈ 2s.
	tail := "Just one more thought."
	text := long + tail

	chunks := PlanChunks(text)
	for _, c := range chunks {
		if c.EstMs < chunkMinTailMs && c.Index != 0 {
			t.Fatalf("found a tail chunk under %dms: index=%d est=%dms",
				chunkMinTailMs, c.Index, c.EstMs)
		}
	}
}

func TestPlanChunks_EmptyText(t *testing.T) {
	if got := PlanChunks(""); got != nil {
		t.Fatalf("empty text should produce no chunks, got %d", len(got))
	}
	if got := PlanChunks("   \n\n  "); got != nil {
		t.Fatalf("whitespace-only should produce no chunks, got %d", len(got))
	}
}

func TestPlanChunks_OffsetsExact(t *testing.T) {
	text := "Para one with several words to count.\n\n" +
		strings.Repeat("filler ", 400) + "\n\n" +
		strings.Repeat("more filler ", 400)
	chunks := PlanChunks(text)
	for _, c := range chunks {
		if c.Text != text[c.CharStart:c.CharEnd] {
			t.Fatalf("chunk %d text doesn't match its offsets", c.Index)
		}
	}
}

func TestReadTextJoined_AddsPeriodWhenTitleLacksTerminator(t *testing.T) {
	cases := []struct {
		title string
		body  string
		want  string
	}{
		{"Audio TTS smoke test", "Hello world.", "Audio TTS smoke test.\n\nHello world."},
		{"Already ends with period.", "More.", "Already ends with period.\n\nMore."},
		{"What about a question?", "Body.", "What about a question?\n\nBody."},
		{"Exclaim!", "Body.", "Exclaim!\n\nBody."},
		{"  Trim whitespace  ", "Body.", "Trim whitespace.\n\nBody."},
		// No body — title is returned as-is, no period appended.
		{"Lonely title", "", "Lonely title"},
	}
	for _, tc := range cases {
		got := ReadText{Title: tc.title, Body: tc.body}.Joined()
		if got != tc.want {
			t.Errorf("Joined(title=%q, body=%q) = %q, want %q",
				tc.title, tc.body, got, tc.want)
		}
	}
}

func TestIsShortPost(t *testing.T) {
	short := strings.Repeat("word ", 100) // ~40s
	long := strings.Repeat("word ", 1000) // ~6:40
	if !IsShortPost(short) {
		t.Errorf("100-word post should be short")
	}
	if IsShortPost(long) {
		t.Errorf("1000-word post should not be short")
	}
}
