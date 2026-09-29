package server

import (
	"errors"
	"log"
	"math"
	"strings"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The server console: vanilla's MinecraftServer command source, reached
// over the bus (mc.cmd.run). It runs any command a player could, at the
// highest permission level, from the world spawn in the spawn's dimension,
// and hands back the lines the command answered with.
//
// Commands here are written for a player caller: their hub half looks the
// caller up in the players map. The console therefore has a tracked entry
// of its own, but it sits in that map ONLY while a command closure runs
// (runHubCmd) — never across a tick, so nothing simulates, streams chunks
// to, saves, lists or targets it.

// consoleName is the console source's name. It carries a character no
// Minecraft or Floodgate username can, so no player can share it — and
// with it the console's operator rights.
const consoleName = "@server"

// consoleDisplay is the name operators see the console by, as vanilla's
// "[Server: …]" admin lines show it.
const consoleDisplay = "Server"

// consoleEID is the console's entity id: far outside the minted range.
const consoleEID = math.MinInt32 + 1

// sourceName is a command caller's name as feedback shows it.
func sourceName(p *player) string {
	if p.name == consoleName {
		return consoleDisplay
	}
	return p.name
}

// runHubCmd runs a command's hub half. While a console command is in
// flight the console's entry is in the players map for exactly the length
// of the closure, and a panic in it is contained: a command written for a
// real player may reach for state the console does not carry.
func (h *hub) runHubCmd(players map[int32]*tracked, fn func(map[int32]*tracked)) {
	c := h.console
	if c == nil {
		fn(players)
		return
	}
	players[c.p.eid] = c
	defer delete(players, c.p.eid)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("console: command panicked: %v", r)
			c.p.trySendEv(chatEv("An unexpected error occurred trying to execute that command"))
		}
	}()
	fn(players)
}

// consoleSource builds the console's tracked entry at the world spawn.
func (h *hub) consoleSource(p *player) *tracked {
	x, y, z := h.worldSpawn()
	t := &tracked{living: living{attrs: newPlayerAttributes()}, p: p, x: x, y: y, z: z, dim: h.spawnDim(), gamemode: 1, hudOn: true}
	initSurvival(t)
	return t
}

// errConsoleBusy is runAsConsole's answer when the hub would not take the
// command in time.
var errConsoleBusy = errors.New("the world is busy; try again")

// runAsConsole runs one command line (with or without its leading slash)
// as the console and returns what it said. Console commands run one at a
// time.
func (s *Server) runAsConsole(line string) ([]string, error) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "/"))
	if line == "" {
		return nil, errors.New("run requires a command")
	}
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	h := s.hub
	p := newPlayer(consoleEID, consoleName, [16]byte{})
	done := make(chan struct{})
	collected := make(chan []string, 1)
	go func() {
		var lines []string
		take := func(pk outPkt) {
			if c, ok := pk.ev.(attachproto.Chat); ok && !c.ActionBar && c.Text != "" {
				lines = append(lines, c.Text)
			}
		}
		for {
			select {
			case pk := <-p.out:
				take(pk)
			case <-done:
				for {
					select {
					case pk := <-p.out:
						take(pk)
					default:
						collected <- lines
						return
					}
				}
			}
		}
	}()
	if !h.postTimeout(evRunOnHub{fn: func() { h.console = h.consoleSource(p) }}, foreignPostTimeout) {
		close(done)
		<-collected
		return nil, errConsoleBusy
	}
	log.Printf("console: /%s", line)
	s.handleCommand(p, line)
	// FIFO: this runs after every hub half the command posted.
	h.post(evRunOnHub{fn: func() { h.console = nil; close(done) }})
	select {
	case lines := <-collected:
		p.disconnect()
		return lines, nil
	case <-time.After(30 * time.Second):
		p.disconnect()
		return nil, errConsoleBusy
	}
}
