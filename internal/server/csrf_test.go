package server

import "testing"

func TestSecureCookieForPublicBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "https", url: "https://mindful.example.org", want: true},
		{name: "http", url: "http://127.0.0.1:8080", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := secureCookieForPublicBaseURL(tt.url); got != tt.want {
				t.Fatalf("secureCookieForPublicBaseURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
