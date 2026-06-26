package config

import (
	"os"
	"testing"
)

// clearSingleInstanceEnv unsets PIHOLE_URL/PIHOLE_PASSWORD for the duration of
// a test that sets multi-instance variables. Without this, running under
// `just integration` (which exports PIHOLE_URL) causes a false conflict error.
func clearSingleInstanceEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PIHOLE_URL", "PIHOLE_PASSWORD"} {
		if val, ok := os.LookupEnv(key); ok {
			_ = os.Unsetenv(key)
			t.Cleanup(func() { _ = os.Setenv(key, val) })
		}
	}
}

func TestLoad_SingleInstanceNamedPrimary(t *testing.T) {
	t.Setenv("PIHOLE_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_PASSWORD", "test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Instances) != 1 || cfg.Instances[0].Name != "primary" {
		t.Fatalf("expected single 'primary' instance, got %+v", cfg.Instances)
	}
}

func TestLoad_MultiInstance(t *testing.T) {
	clearSingleInstanceEnv(t)
	t.Setenv("PIHOLE_1_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_1_PASSWORD", "one")
	t.Setenv("PIHOLE_2_URL", "http://localhost:8082")
	t.Setenv("PIHOLE_2_PASSWORD", "two")
	t.Setenv("PIHOLE_2_NAME", "secondary")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Instances) != 2 {
		t.Fatalf("Instances len = %d, want 2 (%+v)", len(cfg.Instances), cfg.Instances)
	}
	// Declaration order preserved; first is default.
	if cfg.Instances[0].Name != "instance-1" {
		t.Errorf("Instances[0].Name = %q, want instance-1", cfg.Instances[0].Name)
	}
	if cfg.Instances[1].Name != "secondary" {
		t.Errorf("Instances[1].Name = %q, want secondary", cfg.Instances[1].Name)
	}
	if cfg.Instances[1].URL != "http://localhost:8082" || cfg.Instances[1].Password != "two" {
		t.Errorf("Instances[1] = %+v, want url 8082 / pw two", cfg.Instances[1])
	}
}

func TestLoad_MultiInstanceStopsAtGap(t *testing.T) {
	// PIHOLE_3 is set but PIHOLE_2 is missing — the scan must stop after 1.
	clearSingleInstanceEnv(t)
	t.Setenv("PIHOLE_1_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_1_PASSWORD", "one")
	t.Setenv("PIHOLE_3_URL", "http://localhost:8083")
	t.Setenv("PIHOLE_3_PASSWORD", "three")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Instances) != 1 {
		t.Fatalf("expected scan to stop at the gap (1 instance), got %d: %+v", len(cfg.Instances), cfg.Instances)
	}
}

func TestLoad_SingleAndMultiConflict(t *testing.T) {
	t.Setenv("PIHOLE_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_PASSWORD", "test")
	t.Setenv("PIHOLE_1_URL", "http://localhost:8082")
	t.Setenv("PIHOLE_1_PASSWORD", "two")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when both PIHOLE_URL and PIHOLE_1_URL are set")
	}
}

func TestLoad_MultiInstanceMissingPassword(t *testing.T) {
	t.Setenv("PIHOLE_1_URL", "http://localhost:8081")
	// No PIHOLE_1_PASSWORD set at all.
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing PIHOLE_1_PASSWORD")
	}
}

func TestLoad_MultiInstanceEmptyPasswordAccepted(t *testing.T) {
	clearSingleInstanceEnv(t)
	t.Setenv("PIHOLE_1_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_1_PASSWORD", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("empty password should be accepted: %v", err)
	}
	if cfg.Instances[0].Password != "" {
		t.Errorf("Password = %q, want empty", cfg.Instances[0].Password)
	}
}

func TestLoad_MultiInstanceDuplicateName(t *testing.T) {
	t.Setenv("PIHOLE_1_URL", "http://localhost:8081")
	t.Setenv("PIHOLE_1_PASSWORD", "one")
	t.Setenv("PIHOLE_1_NAME", "dup")
	t.Setenv("PIHOLE_2_URL", "http://localhost:8082")
	t.Setenv("PIHOLE_2_PASSWORD", "two")
	t.Setenv("PIHOLE_2_NAME", "dup")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for duplicate instance names")
	}
}
