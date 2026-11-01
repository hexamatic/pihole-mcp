package config

import (
	"strings"
	"testing"
)

func TestRedactURL_HidesCredentials(t *testing.T) {
	for raw, want := range map[string]string{
		"http://admin:s3cret@pi.hole":         "http://admin:xxxxx@pi.hole",
		"https://admin@pi.hole:8443/admin":    "https://admin@pi.hole:8443/admin",
		"admin:s3cret@pi.hole":                "xxxxx@pi.hole",
		"http://pi.hole/path@not-credentials": "http://pi.hole/path@not-credentials",
		"http://192.168.1.2":                  "http://192.168.1.2",
	} {
		if got := RedactURL(raw); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestLoadInstance_ErrorsDoNotPrintThePassword(t *testing.T) {
	// A URL with credentials but no scheme fails validation, and the error
	// quoted it whole.
	t.Setenv("PIHOLE_URL", "admin:s3cret@pi.hole")
	t.Setenv("PIHOLE_PASSWORD", "x")
	_, err := loadInstance("PIHOLE", "primary")
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("err = %v, want an error without the password", err)
	}
}

func TestValidateInstanceName(t *testing.T) {
	for name, ok := range map[string]bool{
		"primary":               true,
		"Living Room":           true, // escaped in resource URIs, so it keeps working
		"pi-hole.2":             true,
		"all":                   false, // instance=all means every instance
		"ALL":                   false,
		"clients":               false, // pihole://clients/status would match a template
		" padded":               false,
		"tab\there":             false,
		strings.Repeat("x", 65): false,
	} {
		if err := validateInstanceName(name); (err == nil) != ok {
			t.Errorf("validateInstanceName(%q) = %v, want ok=%v", name, err, ok)
		}
	}
}

func TestLoad_RefusesAnInstanceNamedAll(t *testing.T) {
	t.Setenv("PIHOLE_1_URL", "http://a.example")
	t.Setenv("PIHOLE_1_PASSWORD", "x")
	t.Setenv("PIHOLE_1_NAME", "all")
	if _, err := loadNumberedInstances(); err == nil || !strings.Contains(err.Error(), "PIHOLE_1_NAME") {
		t.Errorf("err = %v, want PIHOLE_1_NAME refused", err)
	}
}
