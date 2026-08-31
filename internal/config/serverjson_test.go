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
	WebsiteURL  string `json:"websiteUrl"`
	Version     string `json:"version"`
	Icons       []struct {
		Src      string   `json:"src"`
		MIMEType string   `json:"mimeType"`
		Sizes    []string `json:"sizes"`
	} `json:"icons"`
	Packages []struct {
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
//
// Both Dockerfiles are checked, not just the one that ships. Dockerfile.goreleaser
// builds the published image and is the file the registry actually reads the
// label off; the root Dockerfile is what a contributor gets from `docker build .`
// and carries a comment promising it is kept in step. Until this test read it,
// that comment was an assertion nothing enforced, and the two could drift apart
// with every check still green.
func TestServerJSONNameMatchesImageLabel(t *testing.T) {
	m := loadServerManifest(t)

	label := regexp.MustCompile(`(?m)^LABEL\s+io\.modelcontextprotocol\.server\.name="([^"]+)"`)

	// Read with literal paths rather than building them in the loop: gosec's
	// G304 fires on any non-constant argument to os.ReadFile, and a suppression
	// comment is a worse trade than two explicit reads.
	releaseDockerfile, err := os.ReadFile("../../Dockerfile.goreleaser")
	if err != nil {
		t.Fatalf("read Dockerfile.goreleaser: %v", err)
	}
	rootDockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}

	for _, df := range []struct {
		name    string
		content []byte
	}{
		{"Dockerfile.goreleaser", releaseDockerfile},
		{"Dockerfile", rootDockerfile},
	} {
		name := df.name
		t.Run(name, func(t *testing.T) {
			match := label.FindSubmatch(df.content)
			if match == nil {
				t.Fatalf("%s has no io.modelcontextprotocol.server.name label — MCP Registry publishing will fail ownership verification", name)
			}
			if got := string(match[1]); got != m.Name {
				t.Errorf("%s label = %q, server.json name = %q — these must be identical", name, got, m.Name)
			}
		})
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

	// An OCI package must carry no `version`; the tag inside `identifier` is the
	// version. The registry rejects it with "OCI packages must not have 'version'
	// field" — a rule the published JSON Schema does not encode, since it permits
	// `version` on Package for the npm and pypi cases. Cost the v0.8.0 listing a
	// second publish attempt.
	if pkg.Version != "" {
		t.Errorf("packages[0].version = %q, want it absent — the registry rejects a version on an OCI package (put it in the identifier tag)", pkg.Version)
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

// TestServerJSONIdentityMatchesConstants pins the registry listing to the
// identity the running server advertises over the protocol.
//
// These are two separate publication routes to the same user: server.json is
// what someone browsing the MCP Registry reads, and the constants are what the
// handshake carries to a client that already has the binary. Nothing links
// them at build time, so without this test a change to one silently leaves the
// other describing an older release.
func TestServerJSONIdentityMatchesConstants(t *testing.T) {
	m := loadServerManifest(t)

	if m.Title != ServerTitle {
		t.Errorf("server.json title = %q, ServerTitle = %q — these must be identical", m.Title, ServerTitle)
	}
	if m.Description != ServerDescription {
		t.Errorf("server.json description = %q, ServerDescription = %q — these must be identical", m.Description, ServerDescription)
	}
	if m.WebsiteURL != ServerWebsiteURL {
		t.Errorf("server.json websiteUrl = %q, ServerWebsiteURL = %q — these must be identical", m.WebsiteURL, ServerWebsiteURL)
	}
}

// TestServerJSONIconsMatchConstants checks the icon sets agree entry for entry,
// and that a raster icon is offered first.
//
// The MCP specification tells clients to prefer PNG or JPEG and warns that an
// SVG may carry executable content, so a client that follows it either renders
// nothing or takes a risk when the SVG is all we advertise.
func TestServerJSONIconsMatchConstants(t *testing.T) {
	m := loadServerManifest(t)

	if len(m.Icons) != len(ServerIcons) {
		t.Fatalf("server.json declares %d icons, ServerIcons has %d", len(m.Icons), len(ServerIcons))
	}
	for i, want := range ServerIcons {
		got := m.Icons[i]
		if got.Src != want.Src || got.MIMEType != want.MIMEType {
			t.Errorf("icon %d: server.json = %q (%s), ServerIcons = %q (%s)", i, got.Src, got.MIMEType, want.Src, want.MIMEType)
		}
		if strings.Join(got.Sizes, ",") != strings.Join(want.Sizes, ",") {
			t.Errorf("icon %d sizes: server.json = %v, ServerIcons = %v", i, got.Sizes, want.Sizes)
		}
	}

	if len(ServerIcons) == 0 || ServerIcons[0].MIMEType != "image/png" {
		t.Error("the first advertised icon must be a PNG — the MCP specification tells clients to prefer PNG or JPEG and warns that an SVG may carry executable content")
	}
}

// TestServerIconAssetsExist checks every advertised icon is a file actually
// committed to assets/, since an icon URL is served from the repository and a
// missing file gives clients a 404 with nothing to fall back on.
func TestServerIconAssetsExist(t *testing.T) {
	for _, icon := range ServerIcons {
		name := icon.Src[strings.LastIndex(icon.Src, "/")+1:]
		if _, err := os.Stat("../../assets/" + name); err != nil {
			t.Errorf("icon %s is advertised but assets/%s is missing: %v", icon.Src, name, err)
		}
	}
}

// TestConfigVarsReachServerJSON is the reverse of TestServerJSONEnvVarsAreReal.
// That one catches an advertised variable the code never reads; this one
// catches a variable the code reads that the registry listing never mentions,
// which is how PIHOLE_RATE_LIMIT and four others came to be absent without
// anyone deciding they should be.
//
// Honest about what this is: with the allowlist seeded as it ships, it passes
// both before and after the change that introduced it. It is a drift guard in
// the same category as e2eSkips in cmd/toolsdoc, not a bug guard. Omission is
// fine, but only when it is named and reasoned.
func TestConfigVarsReachServerJSON(t *testing.T) {
	// Deliberately not advertised, with the reason. A registry client renders
	// this block as a first-run form, so every entry is a question asked of
	// every person who installs from the listing.
	//nolint:gosec // G101: these are environment variable NAMES and their reasons for being absent from a public manifest, not values
	deliberatelyAbsent := map[string]string{
		"PIHOLE_RATE_LIMIT":           "HTTP and SSE only; the sole listed package declares a stdio transport",
		"PIHOLE_ALLOWED_ORIGINS":      "HTTP and SSE only",
		"PIHOLE_HTTP_AUTH_TOKEN":      "HTTP and SSE only",
		"PIHOLE_HTTP_AUTH_TOKEN_FILE": "HTTP and SSE only",
		"PIHOLE_TRUSTED_PROXIES":      "HTTP and SSE only",
		"PIHOLE_TOOLSETS":             "free text with no choices key available, so a form would show a box whose valid values are invisible and a typo fails startup; documented in the README instead",
		"PIHOLE_1_URL":                "the numbered multi-instance form is documented in the README; a registry form cannot express a repeating group",
	}

	source, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("reading config.go: %v", err)
	}

	advertised := map[string]bool{}
	for _, env := range loadServerManifest(t).Packages[0].EnvironmentVariables {
		advertised[env.Name] = true
	}

	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`"(PIHOLE_[A-Z0-9_]+)"`).FindAllStringSubmatch(string(source), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if advertised[name] || deliberatelyAbsent[name] != "" {
			continue
		}
		t.Errorf("internal/config reads %s but server.json never advertises it. Add it to the "+
			"environmentVariables block, or to deliberatelyAbsent in this test with the reason.", name)
	}

	// A stale reason is as misleading as a missing one.
	for name := range deliberatelyAbsent {
		if !seen[name] {
			t.Errorf("deliberatelyAbsent names %s, which internal/config no longer reads", name)
		}
	}
}
