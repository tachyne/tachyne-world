package world

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMem() *memCache { return &memCache{m: map[string][]byte{}} }
func (c *memCache) Get(k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}
func (c *memCache) Put(k string, v []byte) {
	c.mu.Lock()
	c.m[k] = v
	c.mu.Unlock()
}

// The whole point: a primary that is down at boot comes into service later
// without a restart, and everything written meanwhile is not lost.
func TestReconnectingCacheSwapsInThePrimaryWhenItAppears(t *testing.T) {
	primary := newMem()
	fallback := newMem()
	var attempts atomic.Int32
	var ups atomic.Int32
	dial := func() (ChunkCache, error) {
		if attempts.Add(1) < 3 { // down for the first two tries
			return nil, errors.New("dial tcp: lookup tachyne-cache: i/o timeout")
		}
		return primary, nil
	}
	c := NewReconnecting("valkey", dial, fallback, 5*time.Millisecond, func() { ups.Add(1) })
	rc := c.(*reconnectingCache)
	if rc.Connected() {
		t.Fatal("connected on a failing dial")
	}

	c.Put("k1", []byte("early")) // written while the primary is down
	if v, ok := fallback.Get("k1"); !ok || string(v) != "early" {
		t.Fatal("fallback did not receive the early write")
	}
	if v, ok := c.Get("k1"); !ok || string(v) != "early" {
		t.Fatal("read through fell to the fallback and missed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !rc.Connected() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !rc.Connected() {
		t.Fatal("primary never connected")
	}
	if ups.Load() != 1 {
		t.Errorf("onUp called %d times, want 1", ups.Load())
	}
	c.Put("k2", []byte("late"))
	if v, ok := primary.Get("k2"); !ok || string(v) != "late" {
		t.Error("write after connect did not reach the primary")
	}
	if v, ok := fallback.Get("k2"); !ok || string(v) != "late" {
		t.Error("write after connect stopped warming the fallback")
	}
}

// A primary that is up at boot is used immediately — no background delay.
func TestReconnectingCacheUsesAHealthyPrimaryAtOnce(t *testing.T) {
	primary := newMem()
	c := NewReconnecting("valkey", func() (ChunkCache, error) { return primary, nil }, nil, time.Hour, nil)
	if !c.(*reconnectingCache).Connected() {
		t.Fatal("healthy primary not connected synchronously")
	}
	c.Put("k", []byte("v"))
	if _, ok := primary.Get("k"); !ok {
		t.Error("write did not reach the primary")
	}
}

// With no fallback and no primary yet, reads simply miss (cache semantics).
func TestReconnectingCacheMissesCleanlyWithNothingBehindIt(t *testing.T) {
	c := NewReconnecting("valkey", func() (ChunkCache, error) { return nil, errors.New("down") }, nil, time.Hour, nil)
	if _, ok := c.Get("k"); ok {
		t.Error("hit with nothing behind the cache")
	}
	c.Put("k", []byte("v")) // must not panic
}
