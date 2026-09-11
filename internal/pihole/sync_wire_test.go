package pihole_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/hexamatic/pihole-mcp/internal/pihole/wiretest"
)

// pihole_instance_sync writes to every configured Pi-hole at once, which makes
// it the most destructive surface in the server and, until now, the least
// observable one. Every test above asserts on state the fake ended up in, so a
// request that reached the right row by luck, carried a stray parameter, or
// dropped a field the fake happens to default reads exactly like a correct one.
//
// These tests assert on the requests themselves.

// recordedTarget returns a sync target whose traffic is recorded. The fake's
// own server is bypassed in favour of one wrapping the recorder, so every
// request ApplyPlan makes is captured before the emulator sees it.
func recordedTarget(t *testing.T) (*piholefake.Fake, *pihole.Client, *wiretest.Recorder) {
	t.Helper()
	f := piholefake.New()
	t.Cleanup(f.Close)
	rec := wiretest.Proxy(f.Handler())
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	c := pihole.New(srv.URL, "test", pihole.WithName("secondary"), pihole.WithRetry(0, time.Second))
	return f, c, rec
}

func TestApplyPlan_PutsTheRightRequestsOnTheWire(t *testing.T) {
	srcFake := piholefake.New()
	t.Cleanup(srcFake.Close)
	src := pihole.New(srcFake.URL(), "test", pihole.WithName("primary"), pihole.WithRetry(0, time.Second))
	seedSource(srcFake)

	tgtFake, tgt, rec := recordedTarget(t)
	// A differing comment makes the domain an update rather than an add, and a
	// group the source does not have makes the prune path run.
	tgtFake.AddDomain("deny", "exact", "ads.example.com", "different comment", true)
	tgtFake.AddGroup("stale", "", true)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	if res := pihole.ApplyPlan(context.Background(), tgt, d, true); res.Failed != 0 {
		t.Fatalf("ApplyPlan failed ops: %d (%+v)", res.Failed, res.Ops)
	}

	t.Run("domain update always sends enabled", func(t *testing.T) {
		// FTL treats PUT as a full replacement and defaults enabled to true for
		// any request that omits it, so a comment-only update silently
		// re-enables a disabled rule. sync.go sends enabled on every PUT to
		// avoid that. Drop the field and this assertion is the only thing in
		// the repo that notices.
		req := rec.Only(t, "PUT", "/domains/deny/exact/ads.example.com")
		req.AssertBodyKeys(t, "comment", "enabled")
		req.AssertField(t, "enabled", true)
		req.AssertField(t, "comment", "blocked")
	})

	t.Run("list add carries type in the query, not the body", func(t *testing.T) {
		// FTL rejects a /lists write that does not name its type, and it reads
		// the type from the query string on POST. Moving it into the body is a
		// 400 that no state assertion would attribute to the right cause.
		req := rec.Only(t, "POST", "/lists")
		req.AssertQuery(t, "type", "block")
		req.AssertBodyKeys(t, "address", "comment", "enabled")
		req.AssertField(t, "address", "https://lists.example.com/ads.txt")
	})

	t.Run("group add sends name comment and enabled", func(t *testing.T) {
		req := rec.Only(t, "POST", "/groups")
		req.AssertBodyKeys(t, "name", "comment", "enabled")
		req.AssertField(t, "name", "iot")
	})

	t.Run("client add sends no enabled field", func(t *testing.T) {
		// A client has no enabled column. Sending one is not a silent no-op
		// here, it is a field FTL does not recognise, and the three sibling
		// categories all send it, which makes this the easy one to get wrong.
		req := rec.Only(t, "POST", "/clients")
		req.AssertBodyKeys(t, "client", "comment")
		req.AssertNoField(t, "enabled")
	})

	t.Run("prune deletes by name and sends no body", func(t *testing.T) {
		req := rec.Only(t, "DELETE", "/groups/stale")
		req.AssertNoBody(t)
	})

	t.Run("local DNS records are escaped into the path", func(t *testing.T) {
		// A host record is "<ip> <name>", so the value carries a space and the
		// escaped spelling is what goes on the wire. Verified against Pi-hole
		// v6 2026.07.2 that FTL accepts it.
		//
		// Honest about what this does NOT catch: removing url.PathEscape at
		// sync.go:245 leaves this green, because net/http encodes a bare space
		// to %20 on its own. Escaping earns its place for '#', '?' and '+',
		// none of which net/http rewrites and all of which change which row
		// FTL finds. Those belong in the escaping session, against a live
		// Pi-hole, not here.
		req := rec.Only(t, "PUT", "/config/dns/hosts/192.168.1.2 pi.hole")
		req.AssertRawPath(t, "/config/dns/hosts/192.168.1.2%20pi.hole")
		req.AssertNoBody(t)
	})

	t.Run("no write went anywhere unexpected", func(t *testing.T) {
		// A sync that writes a category it was not asked to touch is the worst
		// failure this surface has, and it is invisible in the ApplyResult.
		for _, m := range []string{"POST", "PUT", "DELETE"} {
			for _, req := range rec.Select(m, "") {
				switch {
				case req.Path == "/lists", req.Path == "/groups", req.Path == "/clients":
				case len(req.Path) > 9 && req.Path[:9] == "/domains/":
				case len(req.Path) > 8 && req.Path[:8] == "/groups/":
				case len(req.Path) > 7 && req.Path[:7] == "/lists/":
				case len(req.Path) > 9 && req.Path[:9] == "/clients/":
				case len(req.Path) > 12 && req.Path[:12] == "/config/dns/":
				default:
					t.Errorf("unexpected write: %s", req.Describe())
				}
			}
		}
	})
}

// The escaping case sync_wire_test could not cover before the production fix:
// url.PathEscape leaves '+' alone, and FTL reads a bare plus as a space, so a
// regex rule containing one syncs to a path that addresses a different row.
// Against FTL v6.7 the same rule answered 404 spelled with '+' and 204 spelled
// with %2B.
func TestApplyPlan_EscapesPlusInRuleNames(t *testing.T) {
	const rule = `^ads[0-9]+\.example\.com`

	srcFake := piholefake.New()
	t.Cleanup(srcFake.Close)
	src := pihole.New(srcFake.URL(), "test", pihole.WithName("primary"), pihole.WithRetry(0, time.Second))
	srcFake.AddDomain("deny", "regex", rule, "blocked", true)
	srcFake.AddGroup("Kids #2", "after school", true)

	tgtFake, tgt, rec := recordedTarget(t)
	// Matching identities with differing comments make both an update, which
	// is the operation that puts the name into the path.
	tgtFake.AddDomain("deny", "regex", rule, "different comment", true)
	tgtFake.AddGroup("Kids #2", "different comment", true)

	d, err := pihole.ComputeDiff(context.Background(), src, tgt, pihole.DefaultSyncCategories)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	if res := pihole.ApplyPlan(context.Background(), tgt, d, true); res.Failed != 0 {
		t.Fatalf("ApplyPlan failed ops: %d (%+v)", res.Failed, res.Ops)
	}

	rec.Only(t, "PUT", "/domains/deny/regex/"+rule).
		AssertRawPath(t, `/domains/deny/regex/%5Eads%5B0-9%5D%2B%5C.example%5C.com`)
	rec.Only(t, "PUT", "/groups/Kids #2").AssertRawPath(t, "/groups/Kids%20%232")
}
