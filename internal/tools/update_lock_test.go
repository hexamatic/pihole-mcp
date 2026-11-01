package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/hexamatic/pihole-mcp/internal/pihole/piholefake"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestDomainsUpdate_ConcurrentPartialUpdatesKeepBothChanges reproduces the
// lost update the per-entry lock prevents. One call disables a rule, another
// edits its comment. Each must read the entry to keep the field it was not
// given, and with slow reads both used to read before either wrote, so the
// second write put back what the first had changed.
func TestDomainsUpdate_ConcurrentPartialUpdatesKeepBothChanges(t *testing.T) {
	f := piholefake.New()
	t.Cleanup(f.Close)
	f.AddDomain("deny", "exact", "ads.example.com", "original", true)

	inner := f.Handler()
	slowReads := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/domains/deny/exact/ads.example.com" {
			time.Sleep(100 * time.Millisecond)
		}
		inner.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(slowReads)
	t.Cleanup(srv.Close)
	reg := pihole.SingleRegistry(pihole.New(srv.URL, "x"))
	h := domainsUpdateHandler(reg)

	var wg sync.WaitGroup
	for _, args := range []map[string]any{
		{"type": "deny", "kind": "exact", "domain": "ads.example.com", "enabled": false},
		{"type": "deny", "kind": "exact", "domain": "ads.example.com", "comment": "edited"},
	} {
		wg.Add(1)
		go func(args map[string]any) {
			defer wg.Done()
			req := mcp.CallToolRequest{}
			req.Params.Arguments = args
			if res, err := h(context.Background(), req); err != nil || res.IsError {
				t.Errorf("update %v failed: %v %v", args, err, res)
			}
		}(args)
	}
	wg.Wait()

	var got pihole.DomainsResponse
	if err := pihole.New(f.URL(), "x").Get(context.Background(), "/domains/deny/exact/ads.example.com", &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Domains) != 1 {
		t.Fatalf("expected one rule, got %+v", got.Domains)
	}
	if d := got.Domains[0]; d.Enabled || d.Comment != "edited" {
		t.Errorf("after a concurrent disable and comment edit the rule is enabled=%v comment=%q; want false and \"edited\"",
			d.Enabled, d.Comment)
	}
}

func TestKeyedMutex_SerialisesOneKeyOnly(t *testing.T) {
	var k keyedMutex
	unlockA := k.lock("a")

	other := make(chan struct{})
	go func() { k.lock("b")(); close(other) }()
	select {
	case <-other:
	case <-time.After(time.Second):
		t.Fatal("a lock on one key blocked a different key")
	}

	same := make(chan struct{})
	go func() { k.lock("a")(); close(same) }()
	select {
	case <-same:
		t.Fatal("a second lock on a held key did not wait")
	case <-time.After(50 * time.Millisecond):
	}
	unlockA()
	<-same

	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.held) != 0 {
		t.Errorf("released keys are still held: %v", k.held)
	}
}
