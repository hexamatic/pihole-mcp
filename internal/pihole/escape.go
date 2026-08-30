package pihole

import (
	"net/url"
	"strings"
)

// EscapePathSegment percent-encodes one user-supplied value for interpolation
// into a Pi-hole API path.
//
// url.PathEscape on its own is not enough. It leaves '+' alone, because a plus
// is a legal sub-delimiter in a path, but FTL decodes it as a space and so
// looks up a different row: against FTL v6.7 a DELETE of the regex rule
// ^ads[0-9]+\.example\.com answered 404 spelled with a literal '+' and 204
// spelled with %2B, on the same rule.
//
// Escaping '/' is safe; both spellings reach the same row. net/http already
// encodes a bare space, so what this adds over splicing the value in raw is
// '#', '?' and '+', each of which silently changes which row FTL finds.
func EscapePathSegment(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "+", "%2B")
}
