package web

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"", ""}, {"/", ""}, {" /board/ ", "/board"}, {"/board", "/board"}} {
		got, err := NormalizeBasePath(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeBasePath(%q)=%q,%v want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"relative", "//", "///", "//evil.test", "https://evil.test", "/a?b", "/a#b", "/a\\b", "/a/../b", "/a//b", "/a/./b", "/a%2fb", "/a\nb"} {
		if got, err := NormalizeBasePath(in); err == nil {
			t.Errorf("accepted invalid mount %q as %q", in, got)
		}
	}
}
