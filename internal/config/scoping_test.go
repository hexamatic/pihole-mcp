package config

import (
	"strings"
	"testing"
)

func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PIHOLE_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_PASSWORD", "test")
}

func TestLoad_ReadOnlyDefault(t *testing.T) {
	baseEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReadOnly {
		t.Error("ReadOnly = true with PIHOLE_READ_ONLY unset, want false")
	}
}

func TestLoad_ReadOnly(t *testing.T) {
	baseEnv(t)
	t.Setenv("PIHOLE_READ_ONLY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.ReadOnly {
		t.Error("ReadOnly = false, want true")
	}
}

func TestLoad_ReadOnlyRejectsNonBoolean(t *testing.T) {
	baseEnv(t)
	t.Setenv("PIHOLE_READ_ONLY", "maybe")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for a non-boolean PIHOLE_READ_ONLY")
	}
	if !strings.Contains(err.Error(), "PIHOLE_READ_ONLY") {
		t.Errorf("error does not name the variable: %v", err)
	}
}

func TestLoad_ToolsetsParsing(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  bool
		raw  string
		want []string
	}{
		{"unset means every tool", false, "", nil},
		// An empty value reaches Load from a client form, a compose file or a
		// ConfigMap where somebody skipped an optional field. Treating it as
		// "expose nothing", or as an error, would break installs asking for the
		// default.
		{"empty means every tool", true, "", nil},
		{"single name", true, "dns", []string{"dns"}},
		{"whitespace and empties are dropped", true, " dns , , stats ", []string{"dns", "stats"}},
		{"all is preserved for Resolve to interpret", true, "all", []string{"all"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			if tc.set {
				t.Setenv("PIHOLE_TOOLSETS", tc.raw)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.Toolsets) != len(tc.want) {
				t.Fatalf("Toolsets = %v, want %v", cfg.Toolsets, tc.want)
			}
			for i, w := range tc.want {
				if cfg.Toolsets[i] != w {
					t.Errorf("Toolsets[%d] = %q, want %q", i, cfg.Toolsets[i], w)
				}
			}
		})
	}
}

func TestLoad_UnknownToolsetFails(t *testing.T) {
	baseEnv(t)
	t.Setenv("PIHOLE_TOOLSETS", "dns,doamins")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for an unknown toolset name")
	}
	// A stdio server that exits at startup often shows the user nothing but
	// "disconnected", so this one line has to carry both the typo and the way
	// out of it.
	for _, want := range []string{"PIHOLE_TOOLSETS", `"doamins"`, "domains", "stats"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q: %v", want, err)
		}
	}
}
