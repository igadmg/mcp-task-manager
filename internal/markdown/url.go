package markdown

import "strings"

// allowedSchemes are the only URL schemes that may become an href or a src.
// Everything else loses its tag and keeps its visible text.
var allowedSchemes = map[string]bool{
	"http":   true,
	"https":  true,
	"mailto": true,
}

// safeURL reports whether raw may be emitted as an attribute value and
// returns the URL to emit (raw, trimmed). The rule, in order:
//
//  1. trim leading and trailing ASCII whitespace and control bytes;
//  2. refuse an empty remainder;
//  3. refuse any control byte left inside - this is what stops
//     "java\tscript:" and newline-split schemes. Percent-encoded bytes are
//     unaffected, they are already ASCII;
//  4. refuse a protocol-relative "//host" prefix: off-site, and
//     indistinguishable from a path to a reader;
//  5. refuse an '&' appearing before the first '/', '?' or '#'. No
//     legitimate scheme or leading path segment holds one - a query
//     string's '&' always comes later - and this is what stops
//     "java&#9;script:", whose '#' would otherwise read as the start of a
//     fragment;
//  6. accept anything whose first colon falls after the first '/', '?' or
//     '#', or which has no colon at all - relative paths, in-page anchors
//     and the likes of "notes.md:12";
//  7. otherwise the part before the colon must be a well-formed scheme and,
//     ASCII-lowercased, one of allowedSchemes.
//
// Entity tricks are stopped by this rule together with the escaper, not by
// this rule alone: the attribute writer escapes the '&', so even an entity
// that got this far would reach the browser as literal text rather than as
// something it could decode back into a scheme.
func safeURL(raw string) (string, bool) {
	url := strings.TrimFunc(raw, func(r rune) bool {
		return r <= ' ' || r == 0x7f
	})
	if url == "" {
		return "", false
	}
	for i := 0; i < len(url); i++ {
		if url[i] < 0x21 || url[i] == 0x7f {
			return "", false
		}
	}
	if strings.HasPrefix(url, "//") {
		return "", false
	}
	for i := 0; i < len(url); i++ {
		switch url[i] {
		case '&':
			return "", false
		case ':':
			scheme := url[:i]
			if !isScheme(scheme) || !allowedSchemes[strings.ToLower(scheme)] {
				return "", false
			}
			return url, true
		case '/', '?', '#':
			return url, true
		}
	}
	return url, true
}

// isScheme reports whether s has the shape of a URL scheme: a letter
// followed by letters, digits, '+', '-' or '.'.
func isScheme(s string) bool {
	if s == "" || !isASCIILetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if isASCIILetter(c) || isASCIIDigit(c) || c == '+' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func isASCIILetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}
