// Package format provides helpers for formatting Pi-hole API data
// as compact Markdown and CSV for MCP tool responses.
package format

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
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

// Table renders a Markdown table from headers and rows.
func Table(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return "_No data_"
	}

	var b strings.Builder

	b.WriteString("| ")
	b.WriteString(strings.Join(headers, " | "))
	b.WriteString(" |\n")

	b.WriteString("|")
	for range headers {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	for _, row := range rows {
		b.WriteString("| ")
		b.WriteString(strings.Join(row, " | "))
		b.WriteString(" |\n")
	}

	return b.String()
}

// CSV renders comma-separated values from headers and rows.
// ~29% fewer tokens than Markdown tables for tabular data.
func CSV(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return "No data"
	}

	var b strings.Builder
	b.WriteString(strings.Join(headers, ","))
	b.WriteString("\n")
	for _, row := range rows {
		b.WriteString(strings.Join(row, ","))
		b.WriteString("\n")
	}
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

// ResponseTime formats a response time in milliseconds.
func ResponseTime(ms float64) string {
	if ms < 0 {
		return "N/A"
	}
	if ms < 1 {
		return fmt.Sprintf("%.2fms", ms*1000)
	}
	return fmt.Sprintf("%.1fms", ms)
}

// QueryParams builds a URL query string from non-empty key-value pairs.
func QueryParams(params map[string]string) string {
	var parts []string
	for k, v := range params {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}
