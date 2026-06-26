package pihole

import "testing"

func testRegistry() *Registry {
	return NewRegistry([]InstanceConfig{
		{Name: "primary", URL: "http://one", Password: "p1"},
		{Name: "secondary", URL: "http://two", Password: "p2"},
	})
}

func TestRegistry_DefaultIsFirst(t *testing.T) {
	r := testRegistry()
	if r.Default().Name() != "primary" {
		t.Errorf("Default().Name() = %q, want primary", r.Default().Name())
	}
}

func TestRegistry_GetKnownAndUnknown(t *testing.T) {
	r := testRegistry()

	c, err := r.Get("secondary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Name() != "secondary" {
		t.Errorf("Get(secondary).Name() = %q", c.Name())
	}

	if _, err := r.Get("nope"); err == nil {
		t.Fatal("expected error for unknown instance")
	}
}

func TestRegistry_NamesPreserveOrder(t *testing.T) {
	r := testRegistry()
	names := r.Names()
	if len(names) != 2 || names[0] != "primary" || names[1] != "secondary" {
		t.Errorf("Names() = %v, want [primary secondary]", names)
	}
	// Names must be a copy — mutating it must not affect the registry.
	names[0] = "mutated"
	if r.Names()[0] != "primary" {
		t.Error("Names() returned a mutable view of internal state")
	}
}

func TestRegistry_LenAndAll(t *testing.T) {
	r := testRegistry()
	if r.Len() != 2 {
		t.Errorf("Len() = %d, want 2", r.Len())
	}
	if got := r.All(); len(got) != 2 || got[0].Name() != "primary" {
		t.Errorf("All() unexpected: %v", got)
	}
}

func TestSingleRegistry(t *testing.T) {
	c := New("http://x", "pw", WithName("solo"))
	r := SingleRegistry(c)
	if r.Len() != 1 || r.Default().Name() != "solo" {
		t.Errorf("SingleRegistry unexpected: len=%d default=%q", r.Len(), r.Default().Name())
	}

	// Unnamed clients fall back to "primary".
	r2 := SingleRegistry(New("http://y", "pw"))
	if r2.Default().Name() != "primary" {
		t.Errorf("unnamed SingleRegistry default = %q, want primary", r2.Default().Name())
	}
}
