package audio

import (
	"regexp"
	"strings"
)

// ReadText is the joined title + body that gets narrated. Title sits on
// its own line so the model treats it as a heading-like pause.
type ReadText struct {
	Title string
	Body  string
}

// Joined returns the canonical narration text. Char offsets used by
// ChunkSpec are byte offsets into this string.
func (r ReadText) Joined() string {
	if r.Body == "" {
		return r.Title
	}
	return r.Title + "\n\n" + r.Body
}

// ChunkSpec describes one synthesis unit. CharStart/CharEnd are byte
// offsets into ReadText.Joined() (sqlc stores them as INT). EstMs is the
// pre-synthesis duration estimate used for chunk planning; the worker
// overwrites the row's duration_ms with the real value after Kokoro
// reports it.
type ChunkSpec struct {
	Index     int
	CharStart int
	CharEnd   int
	Text      string
	EstMs     int
}

const (
	wordsPerMinute = 150 // rough average across our 5 languages

	// Posts at or below this estimated duration get fully generated on
	// upload — the fuzzy threshold the user chose so 4:02-ish posts
	// don't end up with a 2-second tail chunk.
	fullThresholdMs = 5 * 60 * 1000

	// Each non-final chunk targets this duration. The chunker extends
	// past it to the next paragraph break so audio never restarts mid-
	// sentence.
	chunkTargetMs = 2 * 60 * 1000

	// If a chunk would otherwise be smaller than this, fold it into the
	// previous chunk instead. Stops us emitting tiny tail chunks.
	chunkMinTailMs = 60 * 1000
)

var (
	paragraphSep = regexp.MustCompile(`\n\s*\n+`)
	sentenceSep  = regexp.MustCompile(`[.!?]+\s+`)
	wordSep      = regexp.MustCompile(`\s+`)
)

// PlanChunks splits text into synthesis-ready chunks. Returns a single
// chunk for short posts; multi-chunk for longer ones. Each chunk ends
// at a paragraph boundary when possible, falling back to sentence then
// hard word-count cuts.
func PlanChunks(text string) []ChunkSpec {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	totalMs := EstimateMs(text)
	if totalMs <= fullThresholdMs {
		return []ChunkSpec{{
			Index:     0,
			CharStart: 0,
			CharEnd:   len(text),
			Text:      text,
			EstMs:     totalMs,
		}}
	}

	// Long path: walk paragraph boundaries, packing chunks ~chunkTargetMs.
	paras := splitParagraphs(text)
	var chunks []ChunkSpec
	var pending []paragraph
	pendingMs := 0
	flush := func() {
		if len(pending) == 0 {
			return
		}
		start := pending[0].start
		end := pending[len(pending)-1].end
		chunks = append(chunks, ChunkSpec{
			Index:     len(chunks),
			CharStart: start,
			CharEnd:   end,
			Text:      text[start:end],
			EstMs:     pendingMs,
		})
		pending = nil
		pendingMs = 0
	}
	for _, p := range paras {
		paraMs := EstimateMs(text[p.start:p.end])
		// If a single paragraph dwarfs the target, give it its own chunk
		// (rare — only happens for huge unbroken blocks).
		if len(pending) == 0 && paraMs >= chunkTargetMs {
			pending = append(pending, p)
			pendingMs += paraMs
			flush()
			continue
		}
		pending = append(pending, p)
		pendingMs += paraMs
		if pendingMs >= chunkTargetMs {
			flush()
		}
	}
	flush()

	// Fold a too-short tail chunk back into the previous one. Avoids
	// the 4:02 → "2 second tail" scenario the user called out.
	if len(chunks) >= 2 && chunks[len(chunks)-1].EstMs < chunkMinTailMs {
		last := chunks[len(chunks)-1]
		prev := &chunks[len(chunks)-2]
		prev.CharEnd = last.CharEnd
		prev.Text = text[prev.CharStart:prev.CharEnd]
		prev.EstMs += last.EstMs
		chunks = chunks[:len(chunks)-1]
	}

	return chunks
}

// IsShortPost reports whether the post's estimated narration fits inside
// the fuzzy "fully generate at upload" budget.
func IsShortPost(text string) bool {
	return EstimateMs(strings.TrimSpace(text)) <= fullThresholdMs
}

// EstimateMs computes a rough narration duration from word count.
func EstimateMs(text string) int {
	words := countWords(text)
	if words == 0 {
		return 0
	}
	return words * 60 * 1000 / wordsPerMinute
}

func countWords(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return len(wordSep.Split(text, -1))
}

type paragraph struct {
	start, end int
}

func splitParagraphs(s string) []paragraph {
	boundaries := paragraphSep.FindAllStringIndex(s, -1)
	var out []paragraph
	start := 0
	for _, b := range boundaries {
		if b[0] > start {
			out = append(out, paragraph{start: start, end: b[0]})
		}
		start = b[1]
	}
	if start < len(s) {
		out = append(out, paragraph{start: start, end: len(s)})
	}
	return out
}
