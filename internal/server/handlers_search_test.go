package server

import (
	"reflect"
	"testing"

	"github.com/TuotHash/mindful-social/internal/views"
)

func TestParseHighlightedExcerpt(t *testing.T) {
	type part = views.ExcerptPart
	cases := []struct {
		name string
		in   string
		want []part
	}{
		{"empty input", "", nil},
		{"plain text, no markers", "hello world", []part{{Text: "hello world"}}},
		{"single marker", "hello «HL»world«/HL»", []part{
			{Text: "hello "},
			{Text: "world", Match: true},
		}},
		{"multiple markers", "a «HL»B«/HL» c «HL»D«/HL»", []part{
			{Text: "a "},
			{Text: "B", Match: true},
			{Text: " c "},
			{Text: "D", Match: true},
		}},
		{"unclosed marker treats rest as match", "a «HL»rest", []part{
			{Text: "a "},
			{Text: "rest", Match: true},
		}},
		{"html-like body content stays literal (no HTML interpretation)",
			"<script>alert(1)</script> and «HL»safe«/HL»",
			[]part{
				{Text: "<script>alert(1)</script> and "},
				{Text: "safe", Match: true},
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseHighlightedExcerpt(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("parseHighlightedExcerpt(%q):\n  got  %v\n  want %v", c.in, got, c.want)
			}
		})
	}
}
