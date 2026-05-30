// Package audio holds the per-node TTS planning logic: language
// detection, chunking, voice selection, and the HTTP client for the
// Python Kokoro sidecar. The background worker (in another file) drives
// the job queue using these primitives.
package audio

// SupportedLanguages lists the ISO-639-1 codes we generate audio for.
// Anything else falls through silently — no audio gets enqueued.
var SupportedLanguages = []string{"en", "es", "fr", "it", "de"}

// DefaultVoice returns the locked-in voice for each language. These were
// chosen to be all-female for consistency (German has only the Martin
// male voice, which is the single voice in the community fine-tune).
func DefaultVoice(lang string) string {
	switch lang {
	case "en":
		return "af_heart"
	case "es":
		return "ef_dora"
	case "fr":
		return "ff_siwis"
	case "it":
		return "if_sara"
	case "de":
		return "martin"
	}
	return ""
}

// IsSupported reports whether we can synthesize audio for this language.
func IsSupported(lang string) bool {
	return DefaultVoice(lang) != ""
}
