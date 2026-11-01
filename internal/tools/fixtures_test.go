package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole/wiretest"
)

// fixtureVersions are the FTL releases with captured fixtures, oldest first.
// Each is a directory under testdata/fixtures holding the same set of API
// responses, captured by scripts/refresh-fixtures.sh from a live Pi-hole of
// that version. Responses differ between releases in ways that matter (v6.7
// sends [] where v6.8 sends {}, and keys its client history by row ID), so a
// decoder is tested against every version this server supports.
var fixtureVersions = []string{"v6.7", "v6.8"}

// fixturesDir resolves to <repo>/testdata/fixtures regardless of the package
// the test is invoked from.
func fixturesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot resolve fixtures directory")
	}
	// thisFile lives at internal/tools/fixtures_test.go — climb three levels
	// to reach the repo root and then descend into testdata/fixtures.
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "fixtures")
}

// loadFixture reads the newest version's capture of the named API response
// and decodes it into a generic value (map, slice, primitive). Test code
// passes the result directly to the piholeHandler routes map so the same
// canonical response is served by httptest.
func loadFixture(t *testing.T, name string) any {
	t.Helper()
	return loadVersionFixture(t, fixtureVersions[len(fixtureVersions)-1], name)
}

// loadVersionFixture reads testdata/fixtures/<version>/<name>.json.
func loadVersionFixture(t *testing.T, version, name string) any {
	t.Helper()
	path := filepath.Join(fixturesDir(t), version, name+".json")
	data, err := os.ReadFile(path) //nolint:gosec // path resolved from repo-relative fixtures dir; tests only
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	return v
}

// eachFixture runs fn as a subtest, named after the version, once for every
// version's capture of the named API response.
func eachFixture(t *testing.T, name string, fn func(t *testing.T, version string, fixture any)) {
	t.Helper()
	for _, version := range fixtureVersions {
		t.Run(version, func(t *testing.T) {
			fn(t, version, loadVersionFixture(t, version, name))
		})
	}
}

// fixtureValue returns the value at a dotted path in a decoded fixture, so a
// test can assert what the tool rendered against what Pi-hole sent rather
// than against a copy that goes stale when the fixtures are refreshed.
func fixtureValue(t *testing.T, fixture any, dotted string) any {
	t.Helper()
	m, ok := fixture.(map[string]any)
	if !ok {
		t.Fatalf("fixture is %T, not an object", fixture)
	}
	v, ok := wiretest.LookupField(m, dotted)
	if !ok {
		t.Fatalf("fixture has no %s", dotted)
	}
	return v
}

func fixtureString(t *testing.T, fixture any, dotted string) string {
	t.Helper()
	s, ok := fixtureValue(t, fixture, dotted).(string)
	if !ok {
		t.Fatalf("fixture %s is not a string", dotted)
	}
	return s
}

func fixtureInt(t *testing.T, fixture any, dotted string) int {
	t.Helper()
	f, ok := fixtureValue(t, fixture, dotted).(float64)
	if !ok {
		t.Fatalf("fixture %s is not a number", dotted)
	}
	return int(f)
}
