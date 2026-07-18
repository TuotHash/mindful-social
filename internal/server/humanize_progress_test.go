package server

import "testing"

func TestHumanizeProgress(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{
			"complete",
			`{"type":"topic","title":"Should cities cap rent?","body":"A debate on rent control."}`,
			"Should cities cap rent?\n\nA debate on rent control.",
		},
		{
			"title only, body not started",
			`{"type":"view","title":"Rent caps help tenants"`,
			"Rent caps help tenants",
		},
		{
			"body streaming, unterminated",
			`{"type":"view","title":"Rent caps","body":"Proponents argue it keeps`,
			"Rent caps\n\nProponents argue it keeps",
		},
		{
			"escapes decoded",
			`{"title":"A \"quoted\" line","body":"one\ntwo"}`,
			"A \"quoted\" line\n\none\ntwo",
		},
		{
			"no field yet falls back to raw",
			`{"type":"to`,
			`{"type":"to`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanizeProgress(tc.raw); got != tc.want {
				t.Errorf("humanizeProgress(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
