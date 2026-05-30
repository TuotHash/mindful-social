package audio

import (
	"strings"
	"sync"

	lingua "github.com/pemistahl/lingua-go"
)

var (
	detectorOnce sync.Once
	detector     lingua.LanguageDetector
)

func buildDetector() {
	detector = lingua.NewLanguageDetectorBuilder().
		FromLanguages(
			lingua.English,
			lingua.Spanish,
			lingua.French,
			lingua.Italian,
			lingua.German,
		).
		WithPreloadedLanguageModels().
		Build()
}

// DetectLanguage returns a two-letter code from SupportedLanguages, or
// an empty string if the text is too short / detection is uncertain.
// Empty input → empty result; callers should treat that as "fall back
// to the user's UI locale" or "no audio".
func DetectLanguage(text string) string {
	text = strings.TrimSpace(text)
	if len(text) < 20 {
		// Lingua needs a few words to make a confident call. Anything
		// shorter is just noise — bail out instead of guessing.
		return ""
	}
	detectorOnce.Do(buildDetector)
	lang, ok := detector.DetectLanguageOf(text)
	if !ok {
		return ""
	}
	switch lang {
	case lingua.English:
		return "en"
	case lingua.Spanish:
		return "es"
	case lingua.French:
		return "fr"
	case lingua.Italian:
		return "it"
	case lingua.German:
		return "de"
	}
	return ""
}
