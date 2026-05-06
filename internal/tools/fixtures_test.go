package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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

// loadFixture reads a JSON fixture from testdata/fixtures/<name>.json and
// decodes it into a generic any value (map, slice, primitive). Test code
// passes the result directly to the piholeHandler routes map so the same
// canonical response is served by httptest.
func loadFixture(t *testing.T, name string) any {
	t.Helper()
	path := filepath.Join(fixturesDir(t), name+".json")
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
