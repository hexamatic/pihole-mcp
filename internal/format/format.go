// Package format provides helpers for formatting Pi-hole API data
// as compact Markdown and CSV for MCP tool responses.
package format

import (
	"encoding/csv"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// Number formats an integer with comma separators (e.g. 45231 → "45,231").
func Number(n int) string {
	if n < 0 {
		return "-" + Number(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var b strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		b.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// Percent formats a float as a percentage (e.g. 28.4132 → "28.4%").
func Percent(n float64) string {
	return fmt.Sprintf("%.1f%%", n)
}

// Duration formats seconds as a human-readable duration.
func Duration(secs float64) string {
	d := time.Duration(secs * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", secs)
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// location is the display timezone for Timestamp. nil means time.Local.
// Stored atomically so concurrent reads stay race-free.
var location atomic.Pointer[time.Location]

// SetLocation sets the timezone used by Timestamp. Call once at startup,
// before serving. A nil loc resets to time.Local.
func SetLocation(loc *time.Location) {
	location.Store(loc)
}

// Timestamp formats a Unix timestamp as a human-readable date in the
// timezone configured via SetLocation (system local time by default).
func Timestamp(unix float64) string {
	loc := location.Load()
	if loc == nil {
		loc = time.Local
	}
	return TimestampIn(unix, loc)
}

// TimestampIn formats a Unix timestamp as a human-readable date in an
// explicit timezone. The zone marker is always included so output is
// unambiguous regardless of where the server runs.
func TimestampIn(unix float64, loc *time.Location) string {
	if unix <= 0 {
		return "never"
	}
	return time.Unix(int64(unix), 0).In(loc).Format("2 Jan 2006, 3:04 PM MST")
}

// Clock formats a Unix timestamp as a time of day (15:04:05) in the
// configured location, for lines whose date the reader already knows.
func Clock(unix float64) string {
	loc := location.Load()
	if loc == nil {
		loc = time.Local
	}
	return time.Unix(int64(unix), 0).In(loc).Format("15:04:05")
}

// maxUntrustedLen bounds one value from the network in a reply. A domain is
// at most 253 characters; this leaves room for a long comment or log line
// while stopping one value from filling a reply.
const maxUntrustedLen = 512

// Untrusted makes text that came from the network safe to place in a reply:
// a queried domain, a DHCP hostname, a client's user agent, a log line, a
// comment. Whoever can make a device query a domain, or name a device,
// chooses that text, and a line break or a backtick in it could start a
// heading, a list item or a code block that reads as the server's own words.
//
// Line breaks and other control characters become spaces, backticks become
// apostrophes, the characters that reorder text on screen are dropped, and a
// very long value is cut short with an ellipsis. Nothing a domain name can
// hold is changed ("_", "-", "." and letters in any script among them), so a
// domain read from a reply can be given back to a write tool as it is.
func Untrusted(s string) string {
	clean := len(s) <= maxUntrustedLen
	for _, r := range s {
		if !clean || needsCleaning(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == maxUntrustedLen {
			b.WriteString("…")
			break
		}
		switch {
		case r == '`':
			b.WriteRune('\'')
		case isBidiControl(r):
			continue
		case unicode.IsControl(r), r == '\u2028', r == '\u2029':
			b.WriteRune(' ')
		default:
			b.WriteRune(r) // invalid UTF-8 arrives as U+FFFD
		}
		n++
	}
	return b.String()
}

func needsCleaning(r rune) bool {
	return r == '`' || r == utf8.RuneError || isBidiControl(r) || unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

// isBidiControl reports the characters that change the direction text is
// shown in, which can make a value read differently from what it is.
func isBidiControl(r rune) bool {
	return (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') || r == '\u200e' || r == '\u200f'
}

// CSV renders comma-separated values from headers and rows.
// ~29% fewer tokens than Markdown tables for tabular data.
//
// Fields go through encoding/csv, so a comment carrying a comma, a double
// quote or a newline is quoted per RFC 4180 rather than splicing itself into
// extra columns. Joining the fields by hand produced a row whose column count
// depended on the data, which a reader has no way to detect.
func CSV(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return "No data"
	}

	var b strings.Builder
	w := csv.NewWriter(&b)
	// A strings.Builder never fails a write, so the only error csv.Writer can
	// report here is one it cannot produce.
	_ = w.Write(headers)
	// Every field passes through Untrusted. Quoting kept a line break inside
	// its field for a CSV parser, but a reader sees a new line, which a
	// hostname or comment from the network could use to pass for a row of
	// its own or for the server's own words.
	for _, row := range rows {
		clean := make([]string, len(row))
		for i, f := range row {
			clean[i] = Untrusted(f)
		}
		_ = w.Write(clean)
	}
	w.Flush()
	return b.String()
}

// Truncate appends a note about hidden results (e.g. "Showing 10 of 247").
func Truncate(shown, total int) string {
	if shown >= total {
		return ""
	}
	return fmt.Sprintf("\n_Showing %d of %s results_", shown, Number(total))
}

// Bool formats a boolean as a human-readable string.
func Bool(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// StringOr returns the dereferenced string or a fallback if nil/empty.
func StringOr(s *string, fallback string) string {
	if s == nil || *s == "" {
		return fallback
	}
	return *s
}

// ValueOr returns s if non-empty, otherwise fallback.
// Use for Pi-hole API fields that return empty strings in some environments.
func ValueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Bytes formats a raw byte count to a human-readable size.
// Handles Pi-hole's Docker quirk where units are sometimes empty.
func Bytes(v float64) string {
	switch {
	case v >= 1<<30:
		return fmt.Sprintf("%.1f GB", v/(1<<30))
	case v >= 1<<20:
		return fmt.Sprintf("%.1f MB", v/(1<<20))
	case v >= 1<<10:
		return fmt.Sprintf("%.1f KB", v/(1<<10))
	default:
		return fmt.Sprintf("%.0f B", v)
	}
}

// SizeWithUnit formats a value with its unit, falling back to auto-bytes if the unit is empty.
func SizeWithUnit(value float64, unit string) string {
	if unit == "" {
		return Bytes(value)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

// ResponseTime formats a time FTL reports in seconds, such as a query's
// reply time or an upstream's average response, in milliseconds. Zero, which
// FTL sends for the cache and the blocklist, and anything negative read as
// N/A. The function used to take milliseconds while every caller passed
// seconds, so a 95 ms reply printed as 0.1ms and a 1.2 s average as 1.2ms.
func ResponseTime(secs float64) string {
	if secs <= 0 {
		return "N/A"
	}
	ms := secs * 1000
	if ms < 1 {
		return fmt.Sprintf("%.2fms", ms)
	}
	return fmt.Sprintf("%.1fms", ms)
}

// Milliseconds renders a time FTL reports in seconds as a bare number of
// milliseconds for a CSV column whose header names the unit, or "" for zero.
func Milliseconds(secs float64) string {
	if secs <= 0 {
		return ""
	}
	return strconv.FormatFloat(secs*1000, 'f', 2, 64)
}

// QueryParams builds a URL query string from non-empty key-value pairs.
//
// Values are percent-encoded, so a filter carrying '#', '&' or '=' reaches
// Pi-hole as the one value it is: spliced in raw, an upstream of 8.8.8.8#53
// ended the request at the fragment and FTL answered about 8.8.8.8 instead.
// Encoding also sorts the keys, so the same filters always build the same URL.
func QueryParams(params map[string]string) string {
	v := make(url.Values, len(params))
	for k, val := range params {
		if val != "" {
			v.Set(k, val)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}
