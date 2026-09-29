package vcs

import "testing"

func TestSanitizeUser(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"dev", "dev"},
		{"Igor.Cwer+x", "igor.cwer-x"},
		{"a..b.lock", "a.b"},
		{"-.x.-", "x"},
		{"a  b", "a-b"},
		{"under_score", "under_score"},
		{"jürgen", "j-rgen"},
		{"a-.lock", "a"},
		{"x.lock.lock", "x"},
		{"...", ""},
		{"Игорь", ""},
		{"", ""},
	} {
		if got := sanitizeUser(tc.in); got != tc.want {
			t.Errorf("sanitizeUser(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
