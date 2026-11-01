package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
)

// wrappedTargetRegistry is twoFakeRegistry with the secondary's requests
// passed through wrap first.
func wrappedTargetRegistry(t *testing.T, wrap func(inner http.Handler) http.Handler) (*pihole.Registry, *piholefake.Fake, *piholefake.Fake) {
	t.Helper()
	f1 := piholefake.New()
	t.Cleanup(f1.Close)
	f2 := piholefake.New()
	t.Cleanup(f2.Close)
	srv := httptest.NewServer(wrap(f2.Handler()))
	t.Cleanup(srv.Close)
	reg := pihole.NewRegistry([]pihole.InstanceConfig{
		{Name: "primary", URL: f1.URL(), Password: "x"},
		{Name: "secondary", URL: srv.URL, Password: "x"},
	})
	t.Cleanup(reg.Close)
	return reg, f1, f2
}

func TestSync_TokenCoversTheValuesItWrites(t *testing.T) {
	// The token covered which entries a plan touched but not the values it
	// wrote, so a comment changed on the source after planning was applied
	// under a token issued for the old one.
	reg, f1, f2 := twoFakeRegistry(t)
	f1.AddDomain("deny", "exact", "ads.example.com", "planned", true)
	f2.AddDomain("deny", "exact", "ads.example.com", "old", true)

	res := callRaw(t, instanceSyncHandler(reg), map[string]any{"target": "secondary", "categories": "domains"})
	if res.IsError {
		t.Fatalf("plan failed: %s", resultText(res))
	}
	if text := resultText(res); !strings.Contains(text, "comment=old → enabled=true;comment=planned") {
		t.Errorf("the plan should show what the update replaces, got:\n%s", text)
	}
	token := res.StructuredContent.(InstanceSyncOutput).ConfirmToken

	f1.AddDomain("deny", "exact", "ads.example.com", "changed after planning", true)
	res = callRaw(t, instanceSyncHandler(reg), map[string]any{
		"target": "secondary", "categories": "domains", "mode": "apply", "confirm_token": token, "snapshot": false,
	})
	if !res.IsError || !strings.Contains(resultText(res), "does not match") {
		t.Fatalf("a token for different values should be refused, got: %s", resultText(res))
	}
}

func TestSync_RefusesAPruneThatWouldEmptyACategory(t *testing.T) {
	// A source with no rules pruned against a target with some deleted every
	// one of them.
	reg, _, f2 := twoFakeRegistry(t)
	f2.AddDomain("deny", "exact", "keep-1.example.com", "", true)
	f2.AddDomain("deny", "exact", "keep-2.example.com", "", true)

	res := callRaw(t, instanceSyncHandler(reg), map[string]any{"target": "secondary", "categories": "domains", "prune": true})
	if !res.IsError || !strings.Contains(resultText(res), "Refusing to prune") || !strings.Contains(resultText(res), "2 ") {
		t.Fatalf("expected the prune to be refused, got: %s", resultText(res))
	}
	if f2.DomainCount() != 2 {
		t.Errorf("the target lost rules: %d left", f2.DomainCount())
	}

	// Without prune there is nothing to delete, so the sync runs.
	if out := syncOutput(t, reg, map[string]any{"target": "secondary", "categories": "domains"}); !out.InSync {
		t.Errorf("without prune an empty source adds nothing, so the target is in sync: %+v", out)
	}
}

func TestSync_AReplyWithoutItsListIsAnError(t *testing.T) {
	// Read as an empty list, a reply missing the list made a pruning sync
	// delete everything on the other side.
	reg, f1, _ := wrappedTargetRegistry(t, func(inner http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/domains" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
				return
			}
			inner.ServeHTTP(w, r)
		})
	})
	seedSource(f1)

	res := callRaw(t, instanceDiffHandler(reg), map[string]any{"target": "secondary", "categories": "domains"})
	if !res.IsError || !strings.Contains(resultText(res), `no "domains" list`) {
		t.Fatalf("expected a missing list to be an error, got: %s", resultText(res))
	}
}

func TestSync_InSyncAfterACleanApply(t *testing.T) {
	reg, f1, _ := twoFakeRegistry(t)
	seedSource(f1)
	plan := syncOutput(t, reg, map[string]any{"target": "secondary", "snapshot": false})
	out := syncOutput(t, reg, map[string]any{"target": "secondary", "mode": "apply", "confirm_token": plan.ConfirmToken, "snapshot": false})
	if out.Failed != 0 || !out.InSync {
		t.Errorf("after every step applied, in_sync should be true: %+v", out)
	}
}

func TestSync_WhenEveryStepFailsTheResultsAreKept(t *testing.T) {
	// The error result that replaced a sync with no successes carried only
	// text, so a client reading structured results lost the per-step errors.
	reg, f1, _ := wrappedTargetRegistry(t, func(inner http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.URL.Path != "/api/auth" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"key":"bad_request","message":"refused"}}`))
				return
			}
			inner.ServeHTTP(w, r)
		})
	})
	f1.AddGroup("kids", "", true)

	plan := syncOutput(t, reg, map[string]any{"target": "secondary", "categories": "groups"})
	res := callRaw(t, instanceSyncHandler(reg), map[string]any{
		"target": "secondary", "categories": "groups", "mode": "apply", "confirm_token": plan.ConfirmToken, "snapshot": false,
	})
	if !res.IsError {
		t.Fatalf("a sync where nothing applied is an error: %s", resultText(res))
	}
	out, ok := res.StructuredContent.(InstanceSyncOutput)
	if !ok || out.Failed != 1 || len(out.Ops) != 1 || out.Ops[0].Err == "" || out.InSync {
		t.Errorf("expected the failed step in the structured result, got %T %+v", res.StructuredContent, res.StructuredContent)
	}
}
