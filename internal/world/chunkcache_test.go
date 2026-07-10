package world

import (
	"bufio"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"tachyne/internal/worldgen"
)

func TestChunkCodecRoundTrip(t *testing.T) {
	g := worldgen.NewGenerator(1)
	ch := g.GenerateChunk(3, -7)
	data := encodeChunk(ch)
	if len(data) > 64*1024 {
		t.Fatalf("encoded chunk unexpectedly large: %d bytes", len(data))
	}
	got := decodeChunk(data, len(ch.Sections))
	if got == nil {
		t.Fatal("decode failed on freshly encoded chunk")
	}
	if !got.Equal(ch) {
		t.Fatal("decoded chunk differs from the original")
	}
	t.Logf("encoded size: %d bytes (raw sections are ~400 KB)", len(data))
}

func TestChunkDecodeRejectsGarbage(t *testing.T) {
	if decodeChunk(nil, worldgen.SectionCount) != nil || decodeChunk([]byte{0x00}, worldgen.SectionCount) != nil {
		t.Fatal("bad magic must be rejected")
	}
	ch := worldgen.NewGenerator(1).GenerateChunk(0, 0)
	data := encodeChunk(ch)
	if decodeChunk(data[:len(data)/2], len(ch.Sections)) != nil {
		t.Fatal("truncated data must be rejected")
	}
}

func TestDirCachePersistsChunksAcrossWorlds(t *testing.T) {
	dir := t.TempDir()

	w1 := New(42)
	w1.SetChunkCache(NewDirCache(dir))
	ch1 := w1.generated(5, 5)
	// The put is async — poll (with real sleeps: a loaded CI box can take
	// longer than any spin loop) until it lands.
	key := w1.cacheKey(5, 5)
	var data []byte
	for i := 0; i < 200; i++ {
		if d, ok := w1.chunkCache.Get(key); ok {
			data = d
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if data == nil {
		t.Fatal("chunk was not written to the cache within 1s")
	}
	dec0 := decodeChunk(data, worldgen.SectionCount)
	if dec0 == nil {
		t.Fatal("cached chunk failed to decode")
	}

	// A new world over the same dir must load the SAME chunk from cache. Prove
	// the cache is actually read by poisoning the stored copy with a marker
	// block and seeing it come back out.
	dec := dec0
	dec.Sections[8][123] = 4341 // crafting table where generation put something else
	NewDirCache(dir).Put(key, encodeChunk(dec))

	w2 := New(42)
	w2.SetChunkCache(NewDirCache(dir))
	ch2 := w2.generated(5, 5)
	if ch2.Sections[8][123] != 4341 {
		t.Fatal("second world did not read the persisted chunk (regenerated instead)")
	}
	if ch1.Sections[8][123] == 4341 {
		t.Fatal("sanity: original generation shouldn't have the marker")
	}
}

func TestCorruptCacheEntryRegenerates(t *testing.T) {
	dir := t.TempDir()
	w := New(7)
	cache := NewDirCache(dir)
	cache.Put(w.cacheKey(2, 2), []byte("not a chunk at all"))
	w.SetChunkCache(cache)
	ch := w.generated(2, 2)
	want := worldgen.NewGenerator(7).GenerateChunk(2, 2)
	if !ch.Equal(want) {
		t.Fatal("corrupt cache entry must fall back to clean generation")
	}
}

// fakeValkey is a minimal in-memory RESP2 server: enough of GET/SET/PING to
// exercise the real client over a real TCP socket.
func fakeValkey(t *testing.T) (addr string, store *sync.Map) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	store = &sync.Map{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				readLine := func() string {
					l, err := r.ReadString('\n')
					if err != nil {
						return ""
					}
					return l[:len(l)-2]
				}
				for {
					h := readLine()
					if h == "" || h[0] != '*' {
						return
					}
					n, _ := strconv.Atoi(h[1:])
					args := make([]string, 0, n)
					for i := 0; i < n; i++ {
						bl := readLine() // $len
						ln, _ := strconv.Atoi(bl[1:])
						buf := make([]byte, ln+2)
						for got := 0; got < len(buf); {
							m, err := r.Read(buf[got:])
							if err != nil {
								return
							}
							got += m
						}
						args = append(args, string(buf[:ln]))
					}
					switch args[0] {
					case "PING":
						c.Write([]byte("+PONG\r\n"))
					case "SET":
						store.Store(args[1], args[2])
						c.Write([]byte("+OK\r\n"))
					case "GET":
						if v, ok := store.Load(args[1]); ok {
							s := v.(string)
							c.Write([]byte("$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n"))
						} else {
							c.Write([]byte("$-1\r\n"))
						}
					default:
						c.Write([]byte("-ERR unknown\r\n"))
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String(), store
}

func TestValkeyCacheRoundTrip(t *testing.T) {
	addr, _ := fakeValkey(t)
	c, err := NewValkeyCache(addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("absent key must miss")
	}
	ch := worldgen.NewGenerator(1).GenerateChunk(1, 1)
	data := encodeChunk(ch)
	c.Put("k1", data)
	got, ok := c.Get("k1")
	if !ok {
		t.Fatal("stored key must hit")
	}
	if dec := decodeChunk(got, len(ch.Sections)); dec == nil || !dec.Equal(ch) {
		t.Fatal("chunk did not survive the valkey round-trip")
	}
}

func TestValkeyUnreachableFailsConstruction(t *testing.T) {
	if _, err := NewValkeyCache("127.0.0.1:1"); err == nil {
		t.Fatal("dead server must error at construction (caller falls back)")
	}
}
