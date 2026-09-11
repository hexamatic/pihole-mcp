package format

import (
	"encoding/csv"
	"slices"
	"strings"
	"testing"
	"time"

	// Embed the IANA timezone database so exact-output timestamp tests
	// pass on runners without system zoneinfo.
	_ "time/tzdata"
)

func TestNumber(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{45231, "45,231"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}
	for _, tt := range tests {
		if got := Number(tt.input); got != tt.want {
			t.Errorf("Number(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPercent(t *testing.T) {
	if got := Percent(28.4132); got != "28.4%" {
		t.Errorf("Percent(28.4132) = %q, want %q", got, "28.4%")
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{45, "45s"},
		{90, "1m 30s"},
		{3661, "1h 1m"},
	}
	for _, tt := range tests {
		if got := Duration(tt.input); got != tt.want {
			t.Errorf("Duration(%f) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTimestamp(t *testing.T) {
	if got := Timestamp(0); got != "never" {
		t.Errorf("Timestamp(0) = %q, want %q", got, "never")
	}
	if got := Timestamp(1580000000); !strings.Contains(got, "2020") {
		t.Errorf("Timestamp(1580000000) = %q, expected year 2020", got)
	}
}

func TestTimestampIn(t *testing.T) {
	adelaide, err := time.LoadLocation("Australia/Adelaide")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	tests := []struct {
		name string
		unix float64
		loc  *time.Location
		want string
	}{
		{"summer DST", 1580000000, adelaide, "26 Jan 2020, 11:23 AM ACDT"},
		{"winter standard", 1591000000, adelaide, "1 Jun 2020, 5:56 PM ACST"},
		{"utc", 1580000000, time.UTC, "26 Jan 2020, 12:53 AM UTC"},
		{"zero", 0, time.UTC, "never"},
		{"negative", -5, time.UTC, "never"},
	}
	for _, tt := range tests {
		if got := TimestampIn(tt.unix, tt.loc); got != tt.want {
			t.Errorf("%s: TimestampIn(%v) = %q, want %q", tt.name, tt.unix, got, tt.want)
		}
	}
}

func TestTimestampIn_FixedZoneOffsetFallback(t *testing.T) {
	got := TimestampIn(1580000000, time.FixedZone("", 34200))
	if got != "26 Jan 2020, 10:23 AM +0930" {
		t.Errorf("TimestampIn(fixed zone) = %q, want %q", got, "26 Jan 2020, 10:23 AM +0930")
	}
}

func TestTimestamp_UsesSetLocation(t *testing.T) {
	adelaide, err := time.LoadLocation("Australia/Adelaide")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	SetLocation(adelaide)
	t.Cleanup(func() { SetLocation(nil) })

	if got, want := Timestamp(1580000000), "26 Jan 2020, 11:23 AM ACDT"; got != want {
		t.Errorf("Timestamp(1580000000) = %q, want %q", got, want)
	}
	if got := Timestamp(0); got != "never" {
		t.Errorf("Timestamp(0) = %q, want %q", got, "never")
	}
}

func TestCSV(t *testing.T) {
	got := CSV([]string{"A", "B"}, [][]string{{"1", "2"}, {"3", "4"}})
	if !strings.Contains(got, "A,B\n") {
		t.Errorf("CSV missing header: %q", got)
	}
	if !strings.Contains(got, "1,2\n") {
		t.Errorf("CSV missing row: %q", got)
	}
}

// A comment is free text, so a comma, a double quote or a newline in one used
// to splice itself straight into the row and change its column count. The
// round trip is the assertion that matters: what a reader parses back has to
// be the fields that went in.
func TestCSV_QuotesAndRoundTrips(t *testing.T) {
	headers := []string{"Domain", "Comment"}
	rows := [][]string{
		{"ads.example.com", "blocked, permanently"},
		{"cdn.example.com", `he said "no"`},
		{"api.example.com", "line one\nline two"},
		{"plain.example.com", "nothing special"},
	}

	got := CSV(headers, rows)

	if !strings.Contains(got, `"blocked, permanently"`) {
		t.Errorf("comma field not quoted: %q", got)
	}
	if !strings.Contains(got, `"he said ""no"""`) {
		t.Errorf("double quote not doubled: %q", got)
	}

	records, err := csv.NewReader(strings.NewReader(got)).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v\n%s", err, got)
	}
	if len(records) != len(rows)+1 {
		t.Fatalf("parsed %d records, want %d (header plus %d rows)", len(records), len(rows)+1, len(rows))
	}
	if !slices.Equal(records[0], headers) {
		t.Errorf("header round-tripped as %q, want %q", records[0], headers)
	}
	for i, want := range rows {
		if !slices.Equal(records[i+1], want) {
			t.Errorf("row %d round-tripped as %q, want %q", i, records[i+1], want)
		}
	}
}

func TestCSV_Empty(t *testing.T) {
	if got := CSV([]string{"A"}, nil); got != "No data" {
		t.Errorf("CSV(empty) = %q, want %q", got, "No data")
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate(10, 247); !strings.Contains(got, "10 of 247") {
		t.Errorf("Truncate(10, 247) = %q, expected '10 of 247'", got)
	}
	if got := Truncate(10, 10); got != "" {
		t.Errorf("Truncate(10, 10) = %q, want empty", got)
	}
}

func TestBool(t *testing.T) {
	if Bool(true) != "Yes" {
		t.Error("Bool(true) != Yes")
	}
	if Bool(false) != "No" {
		t.Error("Bool(false) != No")
	}
}

func TestStringOr(t *testing.T) {
	s := "hello"
	if StringOr(&s, "fb") != "hello" {
		t.Error("StringOr(&s) should return s")
	}
	if StringOr(nil, "fb") != "fb" {
		t.Error("StringOr(nil) should return fallback")
	}
	empty := ""
	if StringOr(&empty, "fb") != "fb" {
		t.Error("StringOr(&empty) should return fallback")
	}
}

func TestValueOr(t *testing.T) {
	if ValueOr("hello", "fb") != "hello" {
		t.Error("ValueOr non-empty should return value")
	}
	if ValueOr("", "fb") != "fb" {
		t.Error("ValueOr empty should return fallback")
	}
}

func TestBytes(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{500, "500 B"},
		{1536, "1.5 KB"},
		{543304, "530.6 KB"},
		{543304 * 1024, "530.6 MB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
	}
	for _, tt := range tests {
		if got := Bytes(tt.input); got != tt.want {
			t.Errorf("Bytes(%f) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSizeWithUnit(t *testing.T) {
	if got := SizeWithUnit(100, "MB"); got != "100.0 MB" {
		t.Errorf("SizeWithUnit with unit = %q, want %q", got, "100.0 MB")
	}
	if got := SizeWithUnit(543304, ""); !strings.Contains(got, "KB") {
		t.Errorf("SizeWithUnit empty unit = %q, expected auto-bytes", got)
	}
}

func TestQueryParams(t *testing.T) {
	got := QueryParams(map[string]string{"a": "1", "b": ""})
	if !strings.Contains(got, "a=1") {
		t.Errorf("QueryParams missing a=1: %q", got)
	}
	if strings.Contains(got, "b=") {
		t.Errorf("QueryParams should skip empty: %q", got)
	}
	if got := QueryParams(map[string]string{}); got != "" {
		t.Errorf("QueryParams(empty) = %q, want empty", got)
	}
}

// A filter value is user data. Spliced in raw, a '#' ends the request at the
// fragment and an '&' adds parameters of its own, so pihole_queries_search
// with upstream=8.8.8.8#53 asked FTL about 8.8.8.8 and reported the answer as
// though it were the one requested.
func TestQueryParams_EscapesValuesAndOrdersKeys(t *testing.T) {
	got := QueryParams(map[string]string{"upstream": "8.8.8.8#53", "length": "3"})
	if want := "?length=3&upstream=8.8.8.8%2353"; got != want {
		t.Errorf("QueryParams = %q, want %q", got, want)
	}
}

func TestQueryParams_ValueCannotAddAParameter(t *testing.T) {
	got := QueryParams(map[string]string{"domain": "example.com&length=9999"})
	if want := "?domain=example.com%26length%3D9999"; got != want {
		t.Errorf("QueryParams = %q, want %q", got, want)
	}
}

// Ranging over a map is deliberately unordered, so identical filters produced
// a different URL on every call: nothing downstream can cache or compare them.
func TestQueryParams_IsDeterministic(t *testing.T) {
	params := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5", "f": "6"}
	first := QueryParams(params)
	for range 100 {
		if got := QueryParams(params); got != first {
			t.Fatalf("QueryParams returned %q then %q for the same filters", first, got)
		}
	}
}
