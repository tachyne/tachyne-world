package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// The two primitives every on-disk store shares. They exist because the
// stores had grown sixteen copies of "read the file, decode, ignore the error"
// and twenty copies of "write tmp, rename" — and the copies disagreed in the
// ways that matter on a bad day.
//
// loadStore's contract is the important one: a decode failure must NEVER be
// indistinguishable from an empty file. A store that silently loads as empty
// is written back empty by the next 30-second flush, and the only good copy
// is gone — along with the evidence. So a corrupt file is moved aside with a
// timestamp, loudly, and the store starts empty over PRESERVED bytes that a
// human can restore or diff.

// loadStore decodes the JSON at path into v.
//
//   - A missing file is a legitimately empty store: nil, v untouched.
//   - A file that cannot be READ (permissions, I/O) is returned as an error —
//     starting over it would be a guess.
//   - A file that cannot be DECODED is quarantined to path.corrupt-<UTC stamp>
//     and logged; nil is returned and v is left at its zero value, so the
//     store starts empty while the original bytes survive on disk.
func loadStore(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		bad := fmt.Sprintf("%s.corrupt-%s", path, time.Now().UTC().Format("20060102T150405Z"))
		if rerr := os.Rename(path, bad); rerr != nil {
			// Can't even move it aside: refuse to run over it. Overwriting a
			// file we could not decode is the one outcome this must prevent.
			return fmt.Errorf("%s: %v; and could not quarantine it: %w", path, err, rerr)
		}
		log.Printf("STORE CORRUPT: %s: %v — quarantined to %s and starting this store EMPTY; "+
			"restore from backup or repair the file to recover", path, err, bad)
	}
	return nil
}

// writeAtomic replaces path with data so that a reader — or a crash — sees
// either the whole old file or the whole new one, never a truncated middle.
// The fsync before the rename is what makes that hold across a power loss,
// not just a process death; the stores never had it.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// writeStore is writeAtomic for the flush paths, which have nowhere to return
// an error to: a failed save is logged, not lost silently. It reports success
// so callers that track a dirty flag can clear it only when the bytes landed.
// storeOnDisk remembers, per path, a hash of the bytes last written there, so
// a flush whose content has not changed costs nothing. Several stores rewrite
// themselves WHOLE on a timer rather than when something happens to them, and
// each write is an open, a write, an FSYNC and a rename against the world's
// volume — worth skipping when the file would come out byte-identical.
//
// Safe by construction: the hash is recorded only after a write SUCCEEDS, so
// a failed write leaves nothing cached and the next flush tries again. It
// compares against what this process last wrote, which is what is on disk.
var storeOnDisk sync.Map // path -> [sha256.Size]byte

func writeStore(path string, data []byte) bool {
	sum := sha256.Sum256(data)
	if prev, ok := storeOnDisk.Load(path); ok && prev.([sha256.Size]byte) == sum {
		return true // identical to the file already there
	}
	if err := writeAtomic(path, data); err != nil {
		log.Printf("STORE WRITE FAILED: %s: %v (state kept in memory; will retry next flush)", path, err)
		return false
	}
	storeOnDisk.Store(path, sum)
	return true
}

// bgWriter takes a store's WRITE off the hub goroutine.
//
// The thirty-second save was costing the world a ~115 ms tick — two and a half
// ticks' worth — with nobody online at all. A CPU profile of the live pod put
// it squarely on the save: a third of the samples in json.MarshalIndent, a
// fifth in the content hash, the rest in the file writes. None of that reads
// hub state: record* has already copied everything the store needs into its
// own map, and flush only touches that copy under the store's own mutex. So
// the hub keeps the cheap part (walking the live maps into the snapshot) and
// hands the expensive part to a goroutine.
//
// One write runs at a time and a request that arrives mid-write coalesces into
// a single follow-up run, so a slow disk can never pile up goroutines or write
// an older snapshot over a newer one. An explicit save waits for the
// background write first, which is what keeps shutdown honest.
type bgWriter struct {
	mu      sync.Mutex
	running bool
	pending bool
	done    chan struct{}
}

// run starts write in the background, or marks that it must run again.
func (b *bgWriter) run(write func()) {
	b.mu.Lock()
	if b.running {
		b.pending = true
		b.mu.Unlock()
		return
	}
	b.running, b.done = true, make(chan struct{})
	done := b.done
	b.mu.Unlock()
	go func() {
		for {
			write()
			b.mu.Lock()
			if !b.pending {
				b.running = false
				b.mu.Unlock()
				close(done)
				return
			}
			b.pending = false
			b.mu.Unlock()
		}
	}()
}

// wait blocks until any background write has finished.
func (b *bgWriter) wait() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	done := b.done
	b.mu.Unlock()
	<-done
}
