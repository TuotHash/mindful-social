package server

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeTag(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"climate", "climate"},
		{"  Climate  ", "climate"},
		{"Sugar Tax!!", "sugar-tax"},
		{"food/drink", "food-drink"},
		{"MIDDLE--EAST", "middle-east"},
		{"---hyphens---", "hyphens"},
		{"with_underscore", "with_underscore"},
		{"123numbers456", "123numbers456"},
		{"only !@#$%", "only"}, // letters survive, punctuation+whitespace fold
		{"ÉCO", "co"}, // accents are stripped (ASCII-only)
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := normalizeTag(c.in)
			if got != c.want {
				t.Fatalf("normalizeTag(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestParseTagsInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty input", "", nil},
		{"single tag", "climate", []string{"climate"}},
		{"trim whitespace", "  climate  ,  energy ", []string{"climate", "energy"}},
		{"dedup case-insensitive", "Climate, climate, CLIMATE", []string{"climate"}},
		{"normalize punctuation", "Sugar Tax!!, food/drink", []string{"sugar-tax", "food-drink"}},
		{"drop empties", "climate,, , energy", []string{"climate", "energy"}},
		{"drop unprintable-only", "climate, !!!, energy", []string{"climate", "energy"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseTagsInput(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("parseTagsInput(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseTagsInput_caps(t *testing.T) {
	t.Run("caps tag length", func(t *testing.T) {
		long := strings.Repeat("a", 100)
		got := parseTagsInput(long)
		if len(got) != 1 {
			t.Fatalf("want 1 tag, got %d (%v)", len(got), got)
		}
		if rune := []rune(got[0]); len(rune) != maxTagLength {
			t.Fatalf("tag length = %d, want %d", len(rune), maxTagLength)
		}
	})
	t.Run("caps tag count", func(t *testing.T) {
		var b strings.Builder
		for i := 0; i < maxTagsPerNode+5; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("t")
			b.WriteString(strings.Repeat("x", i+1))
		}
		got := parseTagsInput(b.String())
		if len(got) != maxTagsPerNode {
			t.Fatalf("len = %d, want %d", len(got), maxTagsPerNode)
		}
	})
}
