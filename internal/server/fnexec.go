package server

import (
	"log"
	"sync"
	"time"
)

// Running functions: vanilla's ExecutionContext, reduced to what the
// engine's dispatcher can carry.
//
// One execution context runs one /function (every function of a tag), or
// one of the server's own calls (the load and tick tags, a scheduled
// function). Its lines go through the ordinary dispatcher, one after
// another, with the context's source as the caller. The context holds the
// budget — max_command_sequence_length: each function call and each
// command costs one, and at nothing left the whole context stops — and the
// stack of frames /return acts on. A /function inside a function joins the
// running context rather than starting its own.
//
// Sources. A player's /function runs as that player (at their permission
// level, which /function already needs to be 2). The server's own calls run
// as the server — the console's identity, capped at the function
// permission level (2) — with its output suppressed, like vanilla's
// getGameLoopSender. A function's commands never tell their source
// anything (FunctionCommand.modifySenderForExecution silences it); the
// caller hears "Running function …" before and "Function … returned …"
// after.
//
// Timing. The dispatcher runs a command's session half at once and posts
// its hub half, so a function's commands take effect in order but a few
// hub events later than they were read, not inside the tick that started
// them. The load and tick tags and the scheduled functions are handed off
// by the hub each tick to one runner goroutine, which runs them in that
// order, one tick's worth at a time.
//
// For /execute: a command that runs other commands for another source
// (execute as …) inside a function calls shareFunctionContext so the lines
// it runs count against the same budget and a /return among them acts on
// the running function. fnLimitsFor gives it max_command_forks.

// fnLimits are the gamerules an execution context reads when it starts.
type fnLimits struct {
	commands int // max_command_sequence_length
	forks    int // max_command_forks
}

func (h *hub) fnLimitsNow() fnLimits {
	return fnLimits{commands: h.rules.MaxCmdSeq, forks: h.rules.MaxCmdForks}
}

// fnFrame is one function call on the stack: whether a /return ended it
// and what it returned.
type fnFrame struct {
	id       string
	done     bool // the rest of the function is discarded
	returned bool // a value came back (return <n>, return fail, return run)
	success  bool
	value    int
}

// fnExec is one execution context.
type fnExec struct {
	src     *player
	lim     fnLimits
	quota   int
	stopped bool
	frames  []*fnFrame
	// returnRun marks a `return run function …` in flight: the call it
	// makes hands its result to the frame that ran the return.
	returnRun bool
}

func (e *fnExec) top() *fnFrame {
	if len(e.frames) == 0 {
		return nil
	}
	return e.frames[len(e.frames)-1]
}

// spend takes one command from the budget; false once it is used up,
// which stops the whole context.
func (e *fnExec) spend() bool {
	if e.stopped {
		return false
	}
	if e.quota <= 0 {
		log.Printf("Command execution stopped due to limit (executed %d commands)", e.lim.commands)
		e.stopped = true
		return false
	}
	e.quota--
	return true
}

// fnInstance is a function ready to run: its id and instantiated lines.
type fnInstance struct {
	id    string
	lines []string
}

// execFor is the execution context a source's commands are running in
// (nil when none).
func (s *Server) execFor(p *player) *fnExec {
	s.fnMu.Lock()
	defer s.fnMu.Unlock()
	return s.fnExecs[p]
}

// bindExec makes e the context p's commands run in until release.
func (s *Server) bindExec(p *player, e *fnExec) (release func()) {
	s.fnMu.Lock()
	defer s.fnMu.Unlock()
	if s.fnExecs == nil {
		s.fnExecs = map[*player]*fnExec{}
	}
	prev, had := s.fnExecs[p]
	s.fnExecs[p] = e
	return func() {
		s.fnMu.Lock()
		defer s.fnMu.Unlock()
		if had {
			s.fnExecs[p] = prev
		} else {
			delete(s.fnExecs, p)
		}
	}
}

// shareFunctionContext lets a command running inside a function run lines
// for another source (execute as/at …) in the same context: the same
// budget, the same frames. A no-op when from is not running a function.
func (s *Server) shareFunctionContext(from, to *player) (release func()) {
	e := s.execFor(from)
	if e == nil || from == to {
		return func() {}
	}
	return s.bindExec(to, e)
}

// fnLimitsFor is the running context's limits for p, or ok false.
func (s *Server) fnLimitsFor(p *player) (fnLimits, bool) {
	if e := s.execFor(p); e != nil {
		return e.lim, true
	}
	return fnLimits{}, false
}

// fnCall is CallFunction: one call on the stack, its lines run in order
// until they end, a /return discards the rest, or the budget runs out.
func (s *Server) fnCall(e *fnExec, fi fnInstance) *fnFrame {
	f := &fnFrame{id: fi.id}
	if !e.spend() {
		return f
	}
	e.frames = append(e.frames, f)
	defer func() { e.frames = e.frames[:len(e.frames)-1] }()
	for _, line := range fi.lines {
		if f.done || !e.spend() {
			break
		}
		s.handleCommand(e.src, line)
	}
	return f
}

// runFunctions runs fns in one new execution context with src as the
// source and reports each call's frame. The caller has silenced src.
func (s *Server) runFunctions(src *player, fns []fnInstance, lim fnLimits) []*fnFrame {
	e := &fnExec{src: src, lim: lim, quota: lim.commands}
	release := s.bindExec(src, e)
	defer release()
	out := make([]*fnFrame, 0, len(fns))
	for _, fi := range fns {
		out = append(out, s.fnCall(e, fi))
	}
	return out
}

// hubSync runs fn on the hub goroutine and waits for it. Never call it
// from the hub goroutine.
func (s *Server) hubSync(fn func()) bool {
	h := s.hub
	done := make(chan struct{})
	if !h.postTimeout(evRunOnHub{fn: func() { fn(); close(done) }}, foreignPostTimeout) {
		return false
	}
	select {
	case <-done:
		return true
	case <-h.stop:
		return false
	case <-time.After(30 * time.Second):
		log.Printf("functions: the hub did not answer in time")
		return false
	}
}

// fnJob is one tick's calls for the runner: the load tag after a reload,
// the tick tag, and the scheduled functions that fell due.
type fnJob struct {
	load bool
	tick bool
	due  []schedCallback
}

// functionsTick is ServerFunctionManager.tick and the scheduled events'
// tick, on the hub: what is due this tick goes to the runner.
func (h *hub) functionsTick(age uint64) {
	var j fnJob
	if lib := h.functions.Load(); lib != nil {
		j.load = h.fnPostReload.Swap(false)
		j.tick = len(lib.tag(tagTick)) > 0
	}
	j.due = h.sched.popDue(age)
	if (j.load || j.tick || len(j.due) > 0) && h.fnKick != nil {
		h.fnKick(j)
	}
}

// startFunctionRunner starts the goroutine that runs the server's own
// function calls and hooks the hub up to it.
func (s *Server) startFunctionRunner() {
	jobs := make(chan fnJob, 64)
	var warned bool
	s.hub.fnKick = func(j fnJob) { // on the hub goroutine: never block it
		select {
		case jobs <- j:
			warned = false
		default:
			if !warned {
				log.Printf("functions: the runner is behind; skipping this tick's function calls")
				warned = true
			}
		}
	}
	go func() {
		for j := range jobs {
			s.runFunctionJob(j)
		}
	}()
}

// runFunctionJob runs one tick's calls, each in its own execution context
// as ServerFunctionManager.execute gives it, all as the server.
func (s *Server) runFunctionJob(j fnJob) {
	lib := s.hub.functions.Load()
	if lib == nil {
		return
	}
	var fns []*mcFunction
	if j.load {
		fns = append(fns, lib.tag(tagLoad)...)
	}
	if j.tick {
		fns = append(fns, lib.tag(tagTick)...)
	}
	for _, cb := range j.due {
		if cb.Tag {
			fns = append(fns, lib.tag(cb.ID)...)
		} else if f := lib.function(cb.ID); f != nil {
			fns = append(fns, f)
		}
	}
	if len(fns) == 0 {
		return
	}
	s.asFunctionServer(func(src *player, lim fnLimits) {
		for _, f := range fns {
			lines, err := f.instantiate(nil)
			if err != nil {
				continue // a macro needs arguments the server does not give: nothing runs
			}
			s.runFunctions(src, []fnInstance{{id: f.id, lines: lines}}, lim)
		}
	})
}

// functionSource is the server's game-loop source: the console's identity
// capped at the function permission level, its output suppressed.
func (s *Server) functionSource() *player {
	s.fnSrcOnce.Do(func() {
		p := newPlayer(consoleEID, consoleName, [16]byte{})
		p.permCap = functionPermission
		p.fnSilent.Store(1)
		go func() { // nobody reads what it is told
			for {
				select {
				case <-p.out:
				case <-p.quit:
					return
				}
			}
		}()
		s.fnSrc = p
	})
	return s.fnSrc
}

// asFunctionServer runs fn with the server source in the players map for
// the hub halves of its commands, one console at a time.
func (s *Server) asFunctionServer(fn func(src *player, lim fnLimits)) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	src := s.functionSource()
	h := s.hub
	var lim fnLimits
	if !s.hubSync(func() { h.console = h.consoleSource(src); lim = h.fnLimitsNow() }) {
		h.post(evRunOnHub{fn: func() { h.console = nil }})
		return
	}
	fn(src, lim)
	s.hubSync(func() { h.console = nil })
}

// fnServerState is the Server's function bookkeeping.
type fnServerState struct {
	fnMu      sync.Mutex
	fnExecs   map[*player]*fnExec
	fnSrcOnce sync.Once
	fnSrc     *player
	packMu    sync.Mutex // loads run one at a time
}
