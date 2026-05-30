package audio

import "testing"

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"english", "This is a passage of English text about software engineering and ideas.", "en"},
		{"spanish", "Este es un texto en español sobre ingeniería de software y filosofía.", "es"},
		{"french", "Ceci est un texte en français à propos de l'ingénierie logicielle.", "fr"},
		{"italian", "Questo è un testo in italiano sull'ingegneria del software e la filosofia.", "it"},
		{"german", "Dies ist ein deutscher Text über Softwareentwicklung und Philosophie.", "de"},
		{"too short", "hi", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectLanguage(tc.text)
			if got != tc.want {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
