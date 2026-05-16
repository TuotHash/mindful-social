package server

import "testing"

func TestSameStringSet(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both empty", nil, nil, true},
		{"empty vs empty slice", nil, []string{}, true},
		{"identical order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"reordered", []string{"a", "b", "c"}, []string{"c", "a", "b"}, true},
		{"different element", []string{"a", "b"}, []string{"a", "c"}, false},
		{"different length", []string{"a"}, []string{"a", "b"}, false},
		{"duplicate in a treated as set", []string{"a", "a"}, []string{"a"}, true},
		{"single match", []string{"x"}, []string{"x"}, true},
		{"disjoint same length", []string{"a", "b"}, []string{"c", "d"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameStringSet(c.a, c.b); got != c.want {
				t.Fatalf("sameStringSet(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
