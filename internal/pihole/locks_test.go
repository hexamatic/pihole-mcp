package pihole

import (
	"testing"
	"time"
)

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

func TestLockSettings_OneSettingHoweverItIsSpelt(t *testing.T) {
	c := New("http://pihole.invalid", "x")
	unlock := c.LockSettings("dns.hosts")

	same := make(chan struct{})
	go func() { c.LockSettings("DNS/Hosts", "dns.upstreams")(); close(same) }()
	select {
	case <-same:
		t.Fatal("a lock on DNS/Hosts did not wait for the held dns.hosts")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-same:
	case <-time.After(time.Second):
		t.Fatal("the lock was not released")
	}
}
