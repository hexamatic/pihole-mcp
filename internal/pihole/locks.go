package pihole

import (
	"slices"
	"strings"
	"sync"
)

// Some changes are a read followed by a write that depends on it: an update
// reads an entry to keep the fields it was not given, and removing one name
// from a local DNS record writes back the whole setting it read. Two such
// sequences against the same entry or setting at once can both read before
// either writes, and the second write then undoes the first while both
// report success. MCP clients do issue tool calls in parallel, and a sync
// writes local DNS records too. The locks below let every writer in this
// server take turns on one entry or setting. Writers outside it, such as the
// web interface, are not covered; FTL offers no conditional write that could.

// LockEntry locks the list entry at the API path on this Pi-hole, and returns
// the unlock.
func (c *Client) LockEntry(path string) func() {
	return c.locks.lock("entry " + path)
}

// LockSettings locks the config settings named by the dotted elements on this
// Pi-hole, and returns the unlock. Settings are told apart as FTL tells them
// apart, so "DNS.hosts" and "dns.hosts" are one setting. They are locked in
// sorted order, which keeps two calls that lock several from deadlocking.
func (c *Client) LockSettings(elements ...string) func() {
	keys := make([]string, 0, len(elements))
	for _, e := range elements {
		keys = append(keys, "setting "+NormaliseConfigPath(e))
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	unlocks := make([]func(), 0, len(keys))
	for _, k := range keys {
		unlocks = append(unlocks, c.locks.lock(k))
	}
	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
}

// NormaliseConfigPath reduces a dotted config path to one spelling. Case,
// surrounding whitespace, empty segments and slash separators are all ways
// of writing a path that FTL may still resolve to the same setting.
func NormaliseConfigPath(p string) string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(p)), func(r rune) bool {
		return r == '.' || r == '/'
	})
	return strings.Join(parts, ".")
}

// keyedMutex is a set of mutexes created on demand, one per key, and dropped
// once nothing holds or waits for them.
type keyedMutex struct {
	mu   sync.Mutex
	held map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.held == nil {
		k.held = make(map[string]*keyedLock)
	}
	l := k.held[key]
	if l == nil {
		l = &keyedLock{}
		k.held[key] = l
	}
	l.refs++
	k.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(k.held, key)
		}
		k.mu.Unlock()
	}
}
