package world

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// valkeyCache stores generated chunks in a Valkey/Redis server over a minimal
// hand-rolled RESP2 client (GET/SET/PING — stdlib only, no driver dependency).
// Chunks are immutable per (GenVersion, seed) so entries never need expiry;
// run the server with `maxmemory` + `allkeys-lru` and it self-manages as a
// bounded cache. Every operation carries a short deadline and every error is
// swallowed into a miss/dropped write: a slow or dead Valkey must never stall
// chunk streaming — the world just generates locally instead.
type valkeyCache struct {
	addr  string
	conns chan *valkeyConn // small pool; dial on demand
}

const (
	valkeyPoolSize = 4
	valkeyTimeout  = 250 * time.Millisecond
	valkeyPrefix   = "tachyne:chunk:"
)

type valkeyConn struct {
	c  net.Conn
	r  *bufio.Reader
	wb []byte
}

// NewValkeyCache connects to a Valkey/Redis server, verifying it with a PING.
// Returns an error if the server is unreachable so the caller can log and
// fall back — after that, individual op failures are silent.
func NewValkeyCache(addr string) (ChunkCache, error) {
	v := &valkeyCache{addr: addr, conns: make(chan *valkeyConn, valkeyPoolSize)}
	vc, err := v.dial()
	if err != nil {
		return nil, err
	}
	if _, err := vc.roundTrip(valkeyTimeout, "PING"); err != nil {
		vc.c.Close()
		return nil, fmt.Errorf("valkey ping: %w", err)
	}
	v.release(vc)
	return v, nil
}

func (v *valkeyCache) dial() (*valkeyConn, error) {
	c, err := net.DialTimeout("tcp", v.addr, valkeyTimeout)
	if err != nil {
		return nil, err
	}
	return &valkeyConn{c: c, r: bufio.NewReaderSize(c, 64*1024)}, nil
}

func (v *valkeyCache) acquire() (*valkeyConn, error) {
	select {
	case vc := <-v.conns:
		return vc, nil
	default:
		return v.dial()
	}
}

func (v *valkeyCache) release(vc *valkeyConn) {
	select {
	case v.conns <- vc:
	default:
		vc.c.Close() // pool full
	}
}

func (v *valkeyCache) Get(key string) ([]byte, bool) {
	vc, err := v.acquire()
	if err != nil {
		return nil, false
	}
	val, err := vc.roundTrip(valkeyTimeout, "GET", valkeyPrefix+key)
	if err != nil {
		vc.c.Close()
		return nil, false
	}
	v.release(vc)
	return val, val != nil
}

func (v *valkeyCache) Put(key string, val []byte) {
	vc, err := v.acquire()
	if err != nil {
		return
	}
	if _, err := vc.roundTrip(valkeyTimeout, "SET", valkeyPrefix+key, string(val)); err != nil {
		vc.c.Close()
		return
	}
	v.release(vc)
}

// roundTrip sends one RESP command array and reads one reply. Returns the
// bulk-string payload for $ replies (nil for the null bulk $-1), nil for + / :
// replies, and an error for - replies or protocol trouble.
func (vc *valkeyConn) roundTrip(timeout time.Duration, args ...string) ([]byte, error) {
	vc.c.SetDeadline(time.Now().Add(timeout))
	b := vc.wb[:0]
	b = append(b, '*')
	b = strconv.AppendInt(b, int64(len(args)), 10)
	b = append(b, '\r', '\n')
	for _, a := range args {
		b = append(b, '$')
		b = strconv.AppendInt(b, int64(len(a)), 10)
		b = append(b, '\r', '\n')
		b = append(b, a...)
		b = append(b, '\r', '\n')
	}
	vc.wb = b
	if _, err := vc.c.Write(b); err != nil {
		return nil, err
	}

	line, err := vc.readLine()
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, errors.New("valkey: empty reply")
	}
	switch line[0] {
	case '+', ':':
		return nil, nil
	case '-':
		return nil, errors.New("valkey: " + string(line[1:]))
	case '$':
		n, err := strconv.Atoi(string(line[1:]))
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil // null bulk — key absent
		}
		buf := make([]byte, n+2) // payload + trailing \r\n
		if _, err := io.ReadFull(vc.r, buf); err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
	return nil, errors.New("valkey: unexpected reply " + string(line[:1]))
}

func (vc *valkeyConn) readLine() ([]byte, error) {
	line, err := vc.r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) >= 2 && line[len(line)-2] == '\r' {
		return line[:len(line)-2], nil
	}
	return nil, errors.New("valkey: malformed line")
}
