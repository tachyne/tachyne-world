package world

import (
	"log"
	"sync/atomic"
	"time"
)

// A shared cache that is not there at boot should not be gone for the life of
// the process. On the cluster the Valkey dial has failed on a DNS timeout in
// the first seconds after a pod start — CoreDNS not answering yet — and the
// engine then ran for a day on its local-directory fallback, regenerating
// every chunk the gateways asked for, with the fact recorded in exactly one
// log line at boot.
//
// ReconnectingCache keeps the fallback in front, dials the primary in the
// background until it answers, and swaps it in atomically. Reads try the
// primary first and fall through; writes go to both so the fallback stays
// warm if the primary drops again later.

type reconnectingCache struct {
	primary  atomic.Pointer[ChunkCache]
	fallback ChunkCache // may be nil
	name     string
	onUp     func() // optional: called once when the primary connects
}

// NewReconnecting wraps dial so a primary cache that is unreachable now is
// retried every `every` until it connects. fallback (which may be nil) serves
// in the meantime and stays as a second tier afterwards. The first dial is
// attempted synchronously so a healthy primary is in place before the first
// chunk is served; only a failure goes to the background.
func NewReconnecting(name string, dial func() (ChunkCache, error), fallback ChunkCache, every time.Duration, onUp func()) ChunkCache {
	rc := &reconnectingCache{fallback: fallback, name: name, onUp: onUp}
	// onUp runs BEFORE the primary is published (here and below): anything
	// that observes Connected() then also sees whatever onUp recorded — the
	// backend label on /debug/vars, in practice — rather than a moment where
	// the cache is in place and the label still says otherwise.
	if c, err := dial(); err == nil {
		if onUp != nil {
			onUp()
		}
		rc.primary.Store(&c)
		return rc
	} else {
		log.Printf("chunk cache: %s unavailable (%v) — using fallback and retrying every %s", name, err, every)
	}
	go func() {
		failures := 1
		for {
			time.Sleep(every)
			c, err := dial()
			if err == nil {
				log.Printf("chunk cache: %s connected after %d failed attempt(s)", name, failures)
				if onUp != nil {
					onUp()
				}
				rc.primary.Store(&c)
				return
			}
			failures++
			if failures%20 == 0 { // one line per ~10 min at a 30 s cadence, not one per try
				log.Printf("chunk cache: %s still unavailable after %d attempts (%v)", name, failures, err)
			}
		}
	}()
	return rc
}

func (r *reconnectingCache) Get(k string) ([]byte, bool) {
	if p := r.primary.Load(); p != nil {
		if v, ok := (*p).Get(k); ok {
			return v, true
		}
	}
	if r.fallback != nil {
		return r.fallback.Get(k)
	}
	return nil, false
}

func (r *reconnectingCache) Put(k string, v []byte) {
	if p := r.primary.Load(); p != nil {
		(*p).Put(k, v)
	}
	if r.fallback != nil {
		r.fallback.Put(k, v)
	}
}

// Connected reports whether the primary is currently in place.
func (r *reconnectingCache) Connected() bool { return r.primary.Load() != nil }
