package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// server.json is the MCP Registry manifest published by
// .github/workflows/publish-mcp.yml. Four things about it can silently rot, and
// each one fails at publish time — after a tag has gone out — or, worse, ships a
// listing that misleads users:
//
//  1. `name` must equal the io.modelcontextprotocol.server.name label on the
//     published image, or the registry rejects the publish as unverified.
//  2. String fields must respect the schema's length caps. The registry answers
//     an oversized field with a 422, which is what happened to the first v0.8.0
//     publish attempt.
//  3. The version must agree across `.version`, `.packages[].version` and the
//     tag in `.packages[].identifier`, because the publish job rewrites all
//     three and a mismatch here means one was missed.
//  4. Every environment variable it advertises must actually be read by this
//     package. A registry listing is the install instructions for anyone
//     arriving from an MCP client; an obsolete variable there is worse than no
//     documentation at all.
//
// Mostly these are checks a schema cannot make — the published JSON Schema
// validates shape, the registry validates ownership, and neither knows what our
// code reads. The length caps are the exception, and are duplicated here purely
// because nothing else in CI validates server.json against the schema.

type serverManifest struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Packages    []struct {
		RegistryType         string `json:"registryType"`
		Identifier           string `json:"identifier"`
		Version              string `json:"version"`
		EnvironmentVariables []struct {
			Name       string `json:"name"`
			IsRequired bool   `json:"isRequired"`
			IsSecret   bool   `json:"isSecret"`
		} `json:"environmentVariables"`
	} `json:"packages"`
}

func loadServerManifest(t *testing.T) serverManifest {
	t.Helper()
	raw, err := os.ReadFile("../../server.json")
	if err != nil {
		t.Fatalf("read server.json: %v", err)
	}
	var m serverManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse server.json: %v", err)
	}
	return m
}

// TestServerJSONNameMatchesImageLabel guards the ownership-verification link
// between server.json and the image the registry inspects.
func TestServerJSONNameMatchesImageLabel(t *testing.T) {
	m := loadServerManifest(t)

	dockerfile, err := os.ReadFile("../../Dockerfile.goreleaser")
	if err != nil {
		t.Fatalf("read Dockerfile.goreleaser: %v", err)
	}

	label := regexp.MustCompile(`(?m)^LABEL\s+io\.modelcontextprotocol\.server\.name="([^"]+)"`)
	match := label.FindSubmatch(dockerfile)
	if match == nil {
		t.Fatal("Dockerfile.goreleaser has no io.modelcontextprotocol.server.name label — MCP Registry publishing will fail ownership verification")
	}
	if got := string(match[1]); got != m.Name {
		t.Errorf("image label = %q, server.json name = %q — these must be identical", got, m.Name)
	}

	// GitHub-based namespaces are the only ones we can prove ownership of.
	if !strings.HasPrefix(m.Name, "io.github.hexamatic/") {
		t.Errorf("name = %q, want the io.github.hexamatic/ prefix (GitHub OIDC only grants the repository owner's namespace)", m.Name)
	}
}

// TestServerJSONFieldLimits enforces the published schema's string length caps.
//
// The registry rejects an oversized field with a 422 at publish time — i.e. after
// a tag has already gone out. The v0.8.0 tag hit exactly that: a 138-character
// description against a documented `maxLength: 100`. `goreleaser check` cannot
// see server.json and the drift tests above only compared fields against each
// other, so nothing caught it until the registry did.
func TestServerJSONFieldLimits(t *testing.T) {
	m := loadServerManifest(t)

	// From https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json
	// — ServerDetail.properties.{name,title,description}.
	limits := []struct {
		field string
		value string
		max   int
	}{
		{"description", m.Description, 100},
		{"title", m.Title, 100},
		{"name", m.Name, 200},
	}

	for _, l := range limits {
		if l.value == "" {
			t.Errorf("%s is empty; the schema requires minLength 1", l.field)
			continue
		}
		// Count runes, not bytes: the schema counts characters, and these
		// strings have historically contained em dashes.
		if n := len([]rune(l.value)); n > l.max {
			t.Errorf("%s is %d characters, schema maximum is %d — the registry will reject this with a 422 at publish time:\n  %q", l.field, n, l.max, l.value)
		}
	}
}

// TestServerJSONVersionsAgree checks the three places a version appears are
// consistent, so the release job's rewrite of one cannot leave another stale.
func TestServerJSONVersionsAgree(t *testing.T) {
	m := loadServerManifest(t)
	if len(m.Packages) != 1 {
		t.Fatalf("packages = %d, want exactly 1 (the ghcr.io image)", len(m.Packages))
	}
	pkg := m.Packages[0]

	if pkg.RegistryType != "oci" {
		t.Errorf("registryType = %q, want %q", pkg.RegistryType, "oci")
	}
	if pkg.Version != m.Version {
		t.Errorf("packages[0].version = %q, server version = %q", pkg.Version, m.Version)
	}

	wantIdentifier := fmt.Sprintf("ghcr.io/hexamatic/pihole-mcp:%s", m.Version)
	if pkg.Identifier != wantIdentifier {
		t.Errorf("packages[0].identifier = %q, want %q", pkg.Identifier, wantIdentifier)
	}
}

// TestServerJSONEnvVarsAreReal asserts every advertised variable is one this
// package actually reads, and that the two genuinely required ones are marked
// required with the password marked secret.
func TestServerJSONEnvVarsAreReal(t *testing.T) {
	m := loadServerManifest(t)

	source, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}

	wantRequired := map[string]bool{"PIHOLE_URL": true, "PIHOLE_PASSWORD": true}
	seen := make(map[string]bool)

	for _, env := range m.Packages[0].EnvironmentVariables {
		seen[env.Name] = true

		// PIHOLE_URL and PIHOLE_PASSWORD are read via the instance prefix
		// (prefix + "_URL"), so they never appear as literals in config.go.
		if !wantRequired[env.Name] && !strings.Contains(string(source), `"`+env.Name+`"`) {
			t.Errorf("server.json advertises %s but internal/config never reads it", env.Name)
		}
		if wantRequired[env.Name] && !env.IsRequired {
			t.Errorf("%s must be marked isRequired — clients will not prompt for it otherwise", env.Name)
		}
		if env.Name == "PIHOLE_PASSWORD" && !env.IsSecret {
			t.Error("PIHOLE_PASSWORD must be marked isSecret so clients do not echo or log it")
		}
	}

	for name := range wantRequired {
		if !seen[name] {
			t.Errorf("server.json does not advertise %s, which the server cannot start without", name)
		}
	}
}
