package server

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	"github.com/tachyne/tachyne-world/plugin"
)

// /execute (ExecuteCommand): run a command as and at other entities, under
// conditions, and store what it returned.
//
// Vanilla runs every command from a CommandSourceStack — an executor
// entity (or none), a position, a rotation, a level, an anchor, a
// permission level and the output its answers go to. /execute's
// subcommands are modifiers on that source: as, at, positioned, rotated,
// facing, align, anchored, in, on and summon change it (as, at, on and the
// "… as" forms fork it, one source per entity), if/unless keep or drop
// it, and store hangs a callback on it that receives the command's result.
// The whole chain is evaluated first, stage by stage over every source,
// and only then does the command after `run` execute, once per source.
//
// Here the chain is evaluated on the hub, which owns everything a
// modifier or a condition reads. The command after `run` is the
// ordinary dispatcher's: for each source it runs as a stand-in player —
// its position, rotation and dimension the source's, its permission level
// the runner's, its @s the executor — which stands in the hub's player map
// only while its own command's events are handled (execEnter), and which
// every selector and broadcast passes over. What the stand-in is told is
// captured (player.exec) and handed to the one who ran /execute, as
// vanilla's source keeps its original output; once a fork has happened,
// the failures of a command that failed are dropped, as a forked
// CommandSourceStack.handleError drops them. The command's result, which
// store writes, is what it reported (setCmdResult) or else 1 for a
// command that succeeded and 0 for one that failed.
//
// Functions run through a seam the function runtime binds
// (execRunFunctions and the rest); unbound, `if function` finds no
// function. Not here: `if data`, `if slots`, and storing into block,
// entity or storage NBT, which need /data's NBT access; item predicates
// past an item id, an item #tag or `*`; and local (^) coordinates inside the
// command after `run` measured from the eyes of an `anchored eyes` source
// (the chain's own coordinates honour the anchor).

// execForkLimit is the max_command_fork_count game rule's default.
const execForkLimit = 65536

// execMaxBlocks is ExecuteCommand's MAX_TEST_AREA for `if blocks`.
const execMaxBlocks = 32768

// execWait bounds how long /execute waits on the hub.
const execWait = 30 * time.Second

// execEvalEID is the evaluation stand-in's entity id; execEIDBase starts
// the run stand-ins' — both far below any minted id and apart from the
// console's.
const (
	execEvalEID = math.MinInt32 + 2
	execEIDBase = math.MinInt32 + 4096
)

const execIncomplete = "Unknown or incomplete command. See below for error"

// execNeedsData refuses the NBT forms until /data's NBT access lands.
const execNeedsData = "execute can't read or write block, entity or storage data yet: that needs /data, which this server does not have"

// execCtx rides a stand-in player: who it answers to, who its executor
// is, and what its command said and returned.
type execCtx struct {
	origin  *player   // who ran /execute: the command's output is theirs
	self    cmdEntity // the executor (@s); zero for none
	display string    // the source's display name (sourceName)
	forked  bool      // a fork came before: a failure is not reported
	discard bool      // the evaluation stand-in: it hears nothing

	mu        sync.Mutex
	lines     []attachproto.Chat
	successes int
	result    int
	resultSet bool
}

// capture keeps a chat line sent to the stand-in; nothing else it is sent
// reaches anybody.
func (c *execCtx) capture(ev any) {
	ch, ok := ev.(attachproto.Chat)
	if !ok || c.discard {
		return
	}
	c.mu.Lock()
	c.lines = append(c.lines, ch)
	c.mu.Unlock()
}

// noteSuccess records a sendSuccess (cmdSuccess).
func (c *execCtx) noteSuccess() {
	c.mu.Lock()
	c.successes++
	c.mu.Unlock()
}

// outcome is the (success, result) pair store receives.
func (c *execCtx) outcome() (bool, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.resultSet:
		return true, c.result
	case c.successes > 0:
		return true, 1
	}
	return false, 0
}

// forward hands what the command said to whoever ran /execute. A failed
// command in a forked chain says nothing (handleError with forked set).
func (c *execCtx) forward() {
	c.mu.Lock()
	lines := c.lines
	c.lines = nil
	ok := c.resultSet || c.successes > 0
	c.mu.Unlock()
	if c.forked && !ok {
		return
	}
	for _, l := range lines {
		c.origin.trySendEv(l)
	}
}

// setCmdResult reports a command's return value when it runs for
// /execute (anywhere else it is a no-op).
func setCmdResult(p *player, v int) {
	if p == nil || p.exec == nil {
		return
	}
	p.exec.mu.Lock()
	p.exec.result, p.exec.resultSet = v, true
	p.exec.mu.Unlock()
}

// execStore is one `store` callback, its target resolved when the chain
// reached it.
type execStore struct {
	result  bool     // store result (else success)
	holders []string // score: the holders…
	obj     string   // …and the objective
	bossbar string   // a bossbar id ("" for a score)
	max     bool     // the bossbar's max (else its value)
}

// execSource is one CommandSourceStack.
type execSource struct {
	self       cmdEntity // the executor; zero for none
	x, y, z    float64
	yaw, pitch float32
	dim        int
	eyes       bool // anchored eyes
	stores     []execStore

	// Filled once the chain is done: the name the stand-in shows and the
	// word a command's implicit "me" becomes (execSelfForm).
	display, selfRef string
}

// execStep is one parsed subcommand.
type execStep struct {
	op   string
	args []string
	fork bool // a fork (as, at, on, if, unless, the "as" forms): its failures are silent
}

// parseExecute reads the chain: the steps, then the command after run
// ("" when the chain ends on a condition instead).
func parseExecute(args []string) ([]execStep, string, string) {
	var steps []execStep
	i := 0
	for i < len(args) {
		op := args[i]
		i++
		st := execStep{op: op}
		take := func(n int) bool {
			if n < 0 || i+n > len(args) {
				return false
			}
			st.args = args[i : i+n]
			i += n
			return true
		}
		next := func(k int) string {
			if i+k < len(args) {
				return args[i+k]
			}
			return ""
		}
		ok := true
		switch op {
		case "run":
			if i >= len(args) {
				return nil, "", execIncomplete
			}
			if args[i] == "execute" { // run execute … is the same chain going on
				i++
				continue
			}
			return steps, strings.Join(args[i:], " "), ""
		case "as", "at", "on":
			st.fork = true
			ok = take(1)
		case "positioned":
			switch next(0) {
			case "as":
				st.fork = true
				ok = take(2)
			case "over":
				ok = take(2)
			default:
				ok = take(3)
			}
		case "rotated":
			st.fork = next(0) == "as"
			ok = take(2)
		case "facing":
			st.fork = next(0) == "entity"
			ok = take(3)
		case "align", "anchored", "in", "summon":
			ok = take(1)
		case "store":
			if k := next(0); k != "result" && k != "success" {
				return nil, "", execIncomplete
			}
			switch next(1) {
			case "score", "bossbar":
				ok = take(4)
			case "block", "entity", "storage":
				return nil, "", execNeedsData
			default:
				ok = false
			}
		case "if", "unless":
			st.fork = true
			n := execConditionArity(args[i:])
			if next(0) == "data" {
				return nil, "", execNeedsData
			}
			ok = take(n)
		default:
			ok = false
		}
		if !ok {
			return nil, "", execIncomplete
		}
		steps = append(steps, st)
	}
	// Without run, only a condition can end the chain (it alone executes).
	if len(steps) == 0 || (steps[len(steps)-1].op != "if" && steps[len(steps)-1].op != "unless") {
		return nil, "", execIncomplete
	}
	return steps, "", ""
}

// execConditionArity is how many words a condition takes, its kind
// included; -1 for an unknown one.
func execConditionArity(a []string) int {
	if len(a) == 0 {
		return -1
	}
	switch a[0] {
	case "block", "biome":
		return 5
	case "blocks":
		return 11
	case "entity", "dimension", "predicate", "function":
		return 2
	case "loaded":
		return 4
	case "stopwatch":
		return 3
	case "score":
		if len(a) > 3 && a[3] == "matches" {
			return 5
		}
		return 6
	case "items":
		if len(a) > 1 && a[1] == "block" {
			return 7
		}
		return 5
	}
	return -1
}

// The function runtime's seam. /execute runs functions two ways: `run
// function …`, which is an ordinary command line and needs nothing here,
// and `if|unless function …`, which runs the functions for each source and
// keeps the sources whose functions returned (non-)zero. Both run inside
// whatever function context the one running /execute is in. The runtime
// binds these when it is built in; unbound, no function exists and a
// function context never does.
var (
	// execShareContext puts a stand-in in its runner's function context
	// (the same command budget, the same /return frames).
	execShareContext func(s *Server, from, to *player) (release func())
	// execForkLimitFor is the running function context's max_command_forks.
	execForkLimitFor func(s *Server, p *player) (int, bool)
	// execRunFunctions runs the functions arg names (an id, or #tag) as
	// src, silenced; returned says whether one of them returned a value.
	execRunFunctions func(s *Server, src *player, arg string) (result int, returned bool, fail string)
)

// execSegment is what one pass on the hub hands back: the sources after
// the modifiers it ran, and whether a fork has happened.
type execSegment struct {
	sources []execSource
	forked  bool
	stop    bool // the chain ended: an error, a fork limit, or a terminal condition
}

// isFunctionCond reports whether a step is `if|unless function`, which
// runs commands and so is evaluated off the hub.
func isFunctionCond(st execStep) bool {
	return (st.op == "if" || st.op == "unless") && len(st.args) > 0 && st.args[0] == "function"
}

// cmdExecute is /execute's session half. The chain is evaluated on the
// hub, a stretch at a time between the function conditions (which run
// commands, so run from here); then the command after run executes once
// per source, each run finished (and its stores written) before the next
// begins.
func (s *Server) cmdExecute(p *player, args []string) {
	steps, run, msg := parseExecute(args)
	if msg != "" {
		p.tell(msg)
		return
	}
	if run == "" && isFunctionCond(steps[len(steps)-1]) {
		p.tell(execIncomplete) // `if function` forks and never executes
		return
	}
	limit := execForkLimit
	if execForkLimitFor != nil {
		if n, ok := execForkLimitFor(s, p); ok && n > 0 {
			limit = n
		}
	}
	level := s.opLevel(p.name)
	var sources []execSource
	forked, first := false, true
	for i := 0; ; {
		j := i
		for j < len(steps) && !isFunctionCond(steps[j]) {
			j++
		}
		seg, last := steps[i:j], j == len(steps)
		terminal := last && run == ""
		in, fk, fresh := sources, forked, first
		reply := make(chan execSegment, 1)
		s.onHub(func(players map[int32]*tracked) {
			reply <- s.hub.evalSegment(players, p, fresh, in, seg, fk, terminal, limit)
		})
		var out execSegment
		select {
		case out = <-reply:
		case <-time.After(execWait):
			p.tell(errConsoleBusy.Error())
			return
		}
		sources, forked, first = out.sources, out.forked, false
		if out.stop || len(sources) == 0 {
			return
		}
		if last {
			break
		}
		forked = true // a condition is a fork
		var ok bool
		if sources, ok = s.execFunctionFilter(p, steps[j], sources, level); !ok || len(sources) == 0 {
			return
		}
		i = j + 1
	}
	for i := range sources {
		src := &sources[i]
		ctx, ok := s.runStandIn(p, src, forked, level, true, func(proxy *player) {
			s.runAsSource(proxy, run, src.selfRef)
		})
		if !ok {
			return
		}
		ctx.forward()
	}
}

// execFunctionFilter is ExecuteIfFunctionCustomModifier: the functions run
// for each source (as a stand-in, its output silenced and its stores left
// behind), and the sources kept whose functions returned a value that is
// non-zero for if, zero for unless. ok is false when the chain ends here.
func (s *Server) execFunctionFilter(origin *player, st execStep, sources []execSource, level int) ([]execSource, bool) {
	arg, expected := st.args[1], st.op == "if"
	if execRunFunctions == nil {
		// No function runtime: no function exists. The failure is a
		// forked one, so nobody hears it (handleError, forked).
		return nil, false
	}
	var kept []execSource
	for i := range sources {
		src := sources[i]
		var result int
		var returned bool
		var fail string
		bare := src
		bare.stores = nil // source.clearCallbacks()
		_, ok := s.runStandIn(origin, &bare, true, level, false, func(proxy *player) {
			result, returned, fail = execRunFunctions(s, proxy, arg)
		})
		if !ok || fail != "" {
			return nil, false
		}
		if returned && (result != 0) == expected {
			kept = append(kept, src)
		}
	}
	return kept, true
}

// runStandIn runs fn as a stand-in player for one source — its position,
// rotation and dimension the source's, its level the runner's, its @s the
// executor — standing in the hub's player map, and waits until the hub
// has handled everything fn posted; then the source's stores receive the
// outcome when apply is set. ok is false when the hub did not answer in
// time.
func (s *Server) runStandIn(origin *player, src *execSource, forked bool, level int, apply bool, fn func(proxy *player)) (*execCtx, bool) {
	h := s.hub
	n := s.execSeq.Add(1)
	eid := execEIDBase + (n & 0xFFFFF)
	proxy := newPlayer(eid, "@exec:"+strconv.Itoa(int(n)), [16]byte{})
	proxy.x, proxy.y, proxy.z = src.x, src.y, src.z
	proxy.yaw, proxy.pitch, proxy.dim = src.yaw, src.pitch, src.dim
	ctx := &execCtx{origin: origin, self: src.self, display: src.display, forked: forked}
	proxy.exec = ctx
	t := &tracked{living: living{attrs: newPlayerAttributes()}, p: proxy, x: src.x, y: src.y, z: src.z, dim: src.dim, gamemode: gmCreative, hudOn: true}
	t.yaw, t.pitch = src.yaw, src.pitch
	initSurvival(t)
	s.execLevels.Store(proxy.name, level)
	defer s.execLevels.Delete(proxy.name)
	defer proxy.disconnect()
	h.post(evRunOnHub{fn: func() {
		if h.execProxies == nil {
			h.execProxies = map[int32]*tracked{}
		}
		h.execProxies[eid] = t
	}})
	func() {
		if execShareContext != nil {
			release := execShareContext(s, origin, proxy)
			defer release()
		}
		fn(proxy)
	}()
	var stores []execStore
	if apply {
		stores = src.stores
	}
	done := make(chan struct{})
	// FIFO: this runs after every event the command posted.
	h.post(evRunOnHub{fn: func() {
		delete(h.execProxies, eid)
		delete(h.playersRef, eid)
		ok, v := ctx.outcome()
		h.applyStores(h.playersRef, stores, ok, v)
		close(done)
	}})
	select {
	case <-done:
		return ctx, true
	case <-time.After(execWait):
		return ctx, false
	}
}

// execNeedsPlayer are the commands that act on the connection running them
// — the engine's own and those vanilla requires a player source for with
// no target to name instead. A stand-in has no connection.
var execNeedsPlayer = map[string]bool{
	"hud": true, "refresh": true, "rescue": true, "where": true, "nether": true, "end": true,
	"bug": true, "plugin": true, "trigger": true, "teammsg": true, "tm": true,
}

// runAsSource dispatches one command line for a stand-in.
func (s *Server) runAsSource(proxy *player, line, selfRef string) {
	fields := commandFields(line)
	if len(fields) == 0 {
		return
	}
	if execNeedsPlayer[fields[0]] {
		proxy.tell("A player is required to run this command here")
		return
	}
	if l, ok := execSelfForm(fields, selfRef); ok {
		line = l
	}
	s.handleCommand(proxy, line)
}

// execSelfForm writes out the target a command leaves implicit — the
// source's entity (getEntityOrException) — for the commands whose
// dispatcher would otherwise act on the one who typed them: /tp with only
// a destination, /kill, /gamemode and /clear with no target, /spawnpoint
// with none. The executor is named when it is a player (the forms then
// read as they would for them), @s otherwise.
func execSelfForm(fields []string, selfRef string) (string, bool) {
	if selfRef == "" {
		selfRef = "@s"
	}
	with := func(at int) (string, bool) {
		out := make([]string, 0, len(fields)+1)
		out = append(out, fields[:at]...)
		out = append(out, selfRef)
		out = append(out, fields[at:]...)
		return strings.Join(out, " "), true
	}
	switch fields[0] {
	case "tp", "teleport":
		if len(fields) == 2 || (len(fields) == 4 && isCoordWord(fields[1])) {
			return with(1)
		}
	case "kill", "clear", "spawnpoint":
		if len(fields) == 1 {
			return with(1)
		}
	case "gamemode", "gm":
		if len(fields) == 2 {
			return with(2)
		}
	}
	return "", false
}

// isCoordWord reports whether a word is a coordinate (a number, ~ or ^).
func isCoordWord(w string) bool {
	if strings.HasPrefix(w, "~") || strings.HasPrefix(w, "^") {
		return true
	}
	_, err := strconv.ParseFloat(w, 64)
	return err == nil
}

// ---- the hub half ----------------------------------------------------------

// execEnter stands the run stand-ins in the player map for one event, so
// the command's hub half finds its caller where every command looks for
// one. A join or a leave is handled without them: nobody should see a
// stand-in in their player list.
func (h *hub) execEnter(players map[int32]*tracked, ev hubEvent) []int32 {
	if len(h.execProxies) == 0 {
		return nil
	}
	switch ev.(type) {
	case evJoin, evLeave:
		return nil
	}
	ids := make([]int32, 0, len(h.execProxies))
	for id, t := range h.execProxies {
		players[id] = t
		ids = append(ids, id)
	}
	return ids
}

// execLeave takes them out again.
func (h *hub) execLeave(players map[int32]*tracked, ids []int32) {
	for _, id := range ids {
		delete(players, id)
	}
}

// execSelf is a source's executor if it is still there.
func (h *hub) execSelf(players map[int32]*tracked, en cmdEntity) (cmdEntity, bool) {
	switch {
	case en.t != nil:
		return en, players[en.t.p.eid] == en.t && !en.t.dead
	case en.m != nil:
		return en, h.mobs[en.m.eid] == en.m && en.m.dying == 0
	case en.o != nil:
		return en, true
	}
	return en, false
}

// execFrom is the tracked entry a source's selectors read from: its
// position, rotation and dimension, and (through the evaluation
// stand-in) its executor as @s.
func (h *hub) execFrom(s *execSource) *tracked {
	if h.execEval == nil {
		h.execEval = &player{eid: execEvalEID, name: "@exec", exec: &execCtx{discard: true}}
	}
	h.execEval.exec.self = s.self
	t := &tracked{p: h.execEval, x: s.x, y: s.y, z: s.z, dim: s.dim, gamemode: gmCreative}
	t.yaw, t.pitch = s.yaw, s.pitch
	return t
}

// execWith runs fn with the source standing in the player map, for the
// helpers that find their caller by eid.
func (h *hub) execWith(players map[int32]*tracked, s *execSource, fn func(from *tracked)) {
	from := h.execFrom(s)
	players[from.p.eid] = from
	defer delete(players, from.p.eid)
	fn(from)
}

// execSelect is an EntityArgument read from a source.
func (h *hub) execSelect(players map[int32]*tracked, s *execSource, arg string) ([]cmdEntity, string) {
	spec, ok := parseTargetSpec(arg)
	if !ok {
		return nil, "Invalid name or UUID"
	}
	return h.selectEntitiesAll(players, h.execFrom(s), spec, true, true, true), ""
}

// entityRot is an entity's rotation (a mob keeps no pitch).
func entityRot(en cmdEntity) (float32, float32) {
	switch {
	case en.t != nil:
		return en.t.yaw, en.t.pitch
	case en.m != nil:
		return en.m.yaw, 0
	case en.o != nil:
		return en.o.yaw, en.o.pitch
	}
	return 0, 0
}

// entityEye is an entity's eye height (EntityAnchorArgument.Anchor.EYES);
// 0 for no entity.
func entityEye(en cmdEntity) float64 {
	switch {
	case en.t != nil:
		return en.t.eyeHeight()
	case en.m != nil:
		return mobEyeHeight(en.m)
	case en.o != nil:
		return en.o.h * 0.85
	}
	return 0
}

// execVec3 is a Vec3Argument (center: x and z written as whole numbers
// take the block's centre) read against a source: ~ from its position, ^
// along its rotation from its anchor.
func (h *hub) execVec3(s *execSource, a []string, center bool) (float64, float64, float64, string) {
	if len(a) < 3 {
		return 0, 0, 0, "Incomplete (expected 3 coordinates)"
	}
	locals := 0
	for _, w := range a[:3] {
		if strings.HasPrefix(w, "^") {
			locals++
		}
	}
	if locals != 0 && locals != 3 {
		return 0, 0, 0, "Cannot mix world and local coordinates (everything must either use ^ or not)"
	}
	oy := s.y
	if locals == 3 && s.eyes {
		oy += entityEye(s.self) // Anchor.apply: the eyes of the source's entity, if it has one
	}
	x, y, z, ok := parsePosition(a[:3], s.x, oy, s.z, s.yaw, s.pitch)
	if !ok {
		return 0, 0, 0, "Invalid position: " + strings.Join(a[:3], " ")
	}
	if center && locals == 0 {
		if wholeAbs(a[0]) {
			x += 0.5
		}
		if wholeAbs(a[2]) {
			z += 0.5
		}
	}
	return x, y, z, ""
}

// wholeAbs is WorldCoordinate's centring test: an absolute number written
// without a decimal point.
func wholeAbs(w string) bool {
	return !strings.HasPrefix(w, "~") && !strings.Contains(w, ".")
}

// execBlockPos is a BlockPosArgument read against a source.
func (h *hub) execBlockPos(s *execSource, a []string) (blockPos, string) {
	x, y, z, msg := h.execVec3(s, a, false)
	if msg != "" {
		return blockPos{}, msg
	}
	return blockPos{floorInt(x), floorInt(y), floorInt(z)}, ""
}

// execLoadedPos is BlockPosArgument.getLoadedBlockPos: loaded, and inside
// the world.
func (h *hub) execLoadedPos(s *execSource, a []string) (blockPos, string) {
	pos, msg := h.execBlockPos(s, a)
	if msg != "" {
		return pos, msg
	}
	w := h.worldFor(s.dim)
	if w == nil || !w.Loaded(int32(pos.x>>4), int32(pos.z>>4)) {
		return pos, "That position is not loaded"
	}
	if !h.inWorldYIn(s.dim, pos.y) {
		return pos, "That position is out of this world!"
	}
	return pos, ""
}

// evalSegment runs a stretch of the chain's modifiers over the sources,
// stage by stage (BuildContexts.execute); the first stretch starts from
// the runner's own source. When the chain ends on a condition, that
// condition runs for each source here (terminal).
func (h *hub) evalSegment(players map[int32]*tracked, origin *player, first bool, sources []execSource, steps []execStep, forked, terminal bool, limit int) execSegment {
	if first {
		ot := players[origin.eid]
		if ot == nil {
			return execSegment{stop: true}
		}
		start := execSource{x: ot.x, y: ot.y, z: ot.z, yaw: ot.yaw, pitch: ot.pitch, dim: ot.dim}
		switch {
		case origin.exec != nil:
			start.self = origin.exec.self
		case origin.name != consoleName: // the console source has no entity, and faces 0 0
			start.self = cmdEntity{t: ot}
		default:
			start.yaw, start.pitch = 0, 0
		}
		sources = []execSource{start}
	}
	body := steps
	if terminal {
		body = steps[:len(steps)-1]
	}
	for _, st := range body {
		if st.fork {
			forked = true
		}
		var next []execSource
		for i := range sources {
			out, msg := h.execModify(players, st, &sources[i])
			if msg != "" {
				if !forked {
					cmdFail(origin, msg)
					return execSegment{stop: true}
				}
				continue
			}
			if len(next)+len(out) >= limit {
				if !forked {
					cmdFail(origin, fmt.Sprintf("Maximum number of contexts (%d) reached", limit))
				}
				return execSegment{stop: true}
			}
			next = append(next, out...)
		}
		sources = next
	}
	for i := range sources {
		s := &sources[i]
		s.display, s.selfRef = sourceName(origin), "@s"
		if en, ok := h.execSelf(players, s.self); ok {
			s.display = en.name()
			if en.t != nil {
				s.selfRef = en.t.p.name
			}
		}
	}
	if terminal {
		h.execTerminal(players, origin, steps[len(steps)-1], sources, forked)
		return execSegment{stop: true}
	}
	return execSegment{sources: sources, forked: forked}
}

// execTerminal is a condition at the end of the chain: its executes, with
// "Test passed" / "Test failed" and the value store receives.
func (h *hub) execTerminal(players map[int32]*tracked, origin *player, st execStep, sources []execSource, forked bool) {
	expected := st.op == "if"
	for i := range sources {
		s := &sources[i]
		hit, count, numeric, msg := h.execTest(players, st, s)
		ok, val := false, 0
		switch {
		case msg != "":
		case numeric && expected && hit: // createNumericConditionalHandler / checkIfRegions
			ok, val = true, count
			h.cmdSuccess(players, origin, fmt.Sprintf("Test passed. Count: %d", count), false)
		case numeric && expected:
			msg = "Test failed"
		case numeric && !hit:
			ok, val = true, 1
			h.cmdSuccess(players, origin, "Test passed", false)
		case numeric:
			msg = fmt.Sprintf("Test failed. Count: %d", count)
		case hit == expected: // addConditional's executes
			ok, val = true, 1
			h.cmdSuccess(players, origin, "Test passed", false)
		default:
			msg = "Test failed"
		}
		if !ok && !forked {
			cmdFail(origin, msg)
		}
		h.applyStores(players, s.stores, ok, val)
	}
}

// applyStores runs a source's store callbacks, in the order they were
// chained: the result, or 1/0 for success.
func (h *hub) applyStores(players map[int32]*tracked, stores []execStore, success bool, result int) {
	for _, st := range stores {
		v := result
		if !st.result {
			v = 0
			if success {
				v = 1
			}
		}
		if st.bossbar != "" { // CustomBossEvent.setValue / setMax
			b := h.rules.Bossbars[st.bossbar]
			if b == nil {
				continue
			}
			if st.max {
				b.Max = v
			} else {
				b.Value = v
			}
			h.saveRules()
			if b.Visible {
				for _, vw := range h.bossbarViewers(players, b) {
					vw.p.trySendEv(bossBarHealth(bossbarUUID(st.bossbar), b.progress()))
				}
			}
			continue
		}
		if h.sb == nil || h.sb.Objectives[st.obj] == nil {
			continue
		}
		for _, owner := range st.holders { // getOrCreatePlayerScore(…).set(value)
			h.sbSetScore(players, owner, st.obj, int32(v))
		}
	}
}

// execModify applies one modifier to one source: the sources it becomes
// (none, one, or one per entity), or the failure.
func (h *hub) execModify(players map[int32]*tracked, st execStep, s *execSource) ([]execSource, string) {
	a := st.args
	one := func(ns execSource) ([]execSource, string) { return []execSource{ns}, "" }
	each := func(arg string, fn func(ns *execSource, en cmdEntity)) ([]execSource, string) {
		ens, msg := h.execSelect(players, s, arg)
		if msg != "" {
			return nil, msg
		}
		out := make([]execSource, 0, len(ens))
		for _, en := range ens {
			ns := *s
			fn(&ns, en)
			out = append(out, ns)
		}
		return out, ""
	}
	switch st.op {
	case "as": // withEntity
		return each(a[0], func(ns *execSource, en cmdEntity) { ns.self = en })
	case "at": // withLevel, withPosition, withRotation
		return each(a[0], func(ns *execSource, en cmdEntity) {
			ns.dim = en.dim()
			ns.x, ns.y, ns.z = en.pos()
			ns.yaw, ns.pitch = entityRot(en)
		})
	case "positioned":
		switch a[0] {
		case "as":
			return each(a[1], func(ns *execSource, en cmdEntity) { ns.x, ns.y, ns.z = en.pos() })
		case "over":
			w := h.worldFor(s.dim)
			if w == nil || !w.Loaded(int32(floorInt(s.x)>>4), int32(floorInt(s.z)>>4)) {
				return nil, "That position is not loaded"
			}
			y, ok := h.execHeightmap(s.dim, a[1], floorInt(s.x), floorInt(s.z))
			if !ok {
				return nil, "Invalid heightmap type " + a[1]
			}
			ns := *s
			ns.y = float64(y)
			return one(ns)
		}
		x, y, z, msg := h.execVec3(s, a, true)
		if msg != "" {
			return nil, msg
		}
		ns := *s
		ns.x, ns.y, ns.z, ns.eyes = x, y, z, false // withPosition, withAnchor(FEET)
		return one(ns)
	case "rotated":
		if a[0] == "as" {
			return each(a[1], func(ns *execSource, en cmdEntity) { ns.yaw, ns.pitch = entityRot(en) })
		}
		yaw, ok1 := parseRotWord(a[0], s.yaw)
		pitch, ok2 := parseRotWord(a[1], s.pitch)
		if !ok1 || !ok2 {
			return nil, "Invalid rotation: " + a[0] + " " + a[1]
		}
		ns := *s
		ns.yaw, ns.pitch = yaw, pitch
		return one(ns)
	case "facing":
		if a[0] == "entity" {
			eyes, ok := parseAnchor(a[2])
			if !ok {
				return nil, "Invalid entity anchor position " + a[2]
			}
			return each(a[1], func(ns *execSource, en cmdEntity) {
				x, y, z := en.pos()
				if eyes {
					y += entityEye(en)
				}
				ns.yaw, ns.pitch = s.facing(x, y, z)
			})
		}
		x, y, z, msg := h.execVec3(s, a, true)
		if msg != "" {
			return nil, msg
		}
		ns := *s
		ns.yaw, ns.pitch = s.facing(x, y, z)
		return one(ns)
	case "align":
		ax, ok := parseSwizzle(a[0])
		if !ok {
			return nil, "Invalid swizzle: expected combination of 'x', 'y' and 'z'"
		}
		ns := *s
		if ax[0] {
			ns.x = math.Floor(ns.x)
		}
		if ax[1] {
			ns.y = math.Floor(ns.y)
		}
		if ax[2] {
			ns.z = math.Floor(ns.z)
		}
		return one(ns)
	case "anchored":
		eyes, ok := parseAnchor(a[0])
		if !ok {
			return nil, "Invalid entity anchor position " + a[0]
		}
		ns := *s
		ns.eyes = eyes
		return one(ns)
	case "in": // withLevel: the position scales with the dimensions' coordinate scale
		dim, ok := parseDimension(a[0])
		if !ok || h.worldFor(dim) == nil {
			return nil, fmt.Sprintf("Unknown dimension '%s'", nsID(a[0]))
		}
		ns := *s
		if dim != s.dim {
			scale := dimType(s.dim).CoordinateScale / dimType(dim).CoordinateScale
			ns.x, ns.z = s.x*scale, s.z*scale
			ns.dim = dim
		}
		return one(ns)
	case "summon":
		en, msg := h.execSummon(players, s, a[0])
		if msg != "" {
			return nil, msg
		}
		ns := *s
		ns.self = en
		return one(ns)
	case "on":
		me, ok := h.execSelf(players, s.self)
		if !ok {
			return nil, ""
		}
		ens, known := h.execRelation(players, a[0], me)
		if !known {
			return nil, execIncomplete
		}
		out := make([]execSource, 0, len(ens))
		for _, en := range ens {
			ns := *s
			ns.self = en
			out = append(out, ns)
		}
		return out, ""
	case "store":
		return h.execStoreStep(players, s, a)
	case "if", "unless":
		hit, count, numeric, msg := h.execTest(players, st, s)
		if msg != "" {
			return nil, msg
		}
		if numeric && st.args[0] != "blocks" {
			hit = count > 0
		}
		if hit == (st.op == "if") {
			return one(*s)
		}
		return nil, ""
	}
	return nil, execIncomplete
}

// facing is CommandSourceStack.facing: the rotation that looks from the
// source's anchor at a point.
func (s *execSource) facing(x, y, z float64) (float32, float32) {
	fy := s.y
	if s.eyes {
		fy += entityEye(s.self)
	}
	yaw, pitch := lookAngles(s.x, fy, s.z, x, y, z)
	return float32(wrapDegrees(yaw)), float32(wrapDegrees(pitch))
}

// parseRotWord is one RotationArgument coordinate: a number, or ~ relative
// to the source's.
func parseRotWord(w string, base float32) (float32, bool) {
	if strings.HasPrefix(w, "^") {
		return 0, false
	}
	v, ok := parseCoord(w, float64(base))
	return float32(v), ok
}

// parseAnchor is EntityAnchorArgument: true for eyes.
func parseAnchor(w string) (bool, bool) {
	switch w {
	case "eyes":
		return true, true
	case "feet":
		return false, true
	}
	return false, false
}

// parseSwizzle is SwizzleArgument: each of x, y, z at most once.
func parseSwizzle(w string) ([3]bool, bool) {
	var ax [3]bool
	if w == "" {
		return ax, false
	}
	for _, c := range w {
		i := strings.IndexRune("xyz", c)
		if i < 0 || ax[i] {
			return ax, false
		}
		ax[i] = true
	}
	return ax, true
}

// execHeightmap is Level.getHeight for a heightmap type: the first y above
// the column's highest block the type counts.
func (h *hub) execHeightmap(dim int, kind string, x, z int) (int, bool) {
	var counts func(uint32) bool
	blocks := func(st uint32) bool { return len(collisionShape(st)) > 0 }
	fluid := func(st uint32) bool {
		if isWaterlogged(st) {
			return true
		}
		n, _ := worldgen.StateName(st)
		return n == "water" || n == "lava" || n == "bubble_column"
	}
	leaves := func(st uint32) bool {
		n, _ := worldgen.StateName(st)
		return strings.HasSuffix(n, "_leaves")
	}
	switch kind {
	case "world_surface":
		counts = func(st uint32) bool { return !isAnyAir(st) }
	case "motion_blocking":
		counts = func(st uint32) bool { return blocks(st) || fluid(st) }
	case "motion_blocking_no_leaves":
		counts = func(st uint32) bool { return (blocks(st) || fluid(st)) && !leaves(st) }
	case "ocean_floor":
		counts = blocks
	default:
		return 0, false
	}
	w := h.worldFor(dim)
	floor := dimType(dim).MinY
	for y := w.Ceiling() - 1; y >= floor; y-- {
		if counts(w.At(x, y, z)) {
			return y + 1, true
		}
	}
	return floor, true
}

// execSummon is `execute summon`: the entity SummonCommand.createEntity
// makes at the source's position, with no data, which becomes the
// executor.
func (h *hub) execSummon(players map[int32]*tracked, s *execSource, arg string) (cmdEntity, string) {
	id, ok := parseResourceID(arg)
	name, vanillaNS := strings.CutPrefix(id, "minecraft:")
	if _, known := entityByName[name]; !ok || !vanillaNS || !known {
		return cmdEntity{}, fmt.Sprintf("Can't find element '%s' of type 'minecraft:entity_type'", id)
	}
	if notSummonable[entityByName[name]] {
		return cmdEntity{}, fmt.Sprintf("Can't summon entity of type %s", id)
	}
	et, ok := summonableType(name)
	if !ok {
		return cmdEntity{}, "Unable to summon entity"
	}
	if !inSpawnableBounds(s.x, s.y, s.z) {
		return cmdEntity{}, "Invalid position for summon"
	}
	mark := int32(atomic.LoadInt64(&h.eidCounter))
	h.withSpawnCause(plugin.SpawnCommand, func() {
		h.summonAt(players, evSummon{etype: et, x: s.x, y: s.y, z: s.z, dim: s.dim, yaw: s.yaw})
	})
	// The entity is the first one minted since: a mob, or anything else.
	var best cmdEntity
	for eid, m := range h.mobs {
		if eid > mark && m.etype == et && m.dying == 0 && (best.m == nil || eid < best.m.eid) {
			best = cmdEntity{m: m}
		}
	}
	if best.m == nil {
		for _, o := range h.otherEntities() {
			if o.eid > mark && (best.o == nil || o.eid < best.o.eid) {
				best = cmdEntity{o: o}
			}
		}
	}
	if best.m == nil && best.o == nil {
		return cmdEntity{}, "Unable to summon entity"
	}
	return best, ""
}

// execEntityByEID is the live entity with an id, of any kind.
func (h *hub) execEntityByEID(players map[int32]*tracked, eid int32) (cmdEntity, bool) {
	if eid == 0 {
		return cmdEntity{}, false
	}
	if t := players[eid]; t != nil && t.p.exec == nil && !t.dead {
		return cmdEntity{t: t}, true
	}
	if m := h.mobs[eid]; m != nil && m.dying == 0 {
		return cmdEntity{m: m}, true
	}
	for _, o := range h.otherEntities() {
		if o.eid == eid {
			return cmdEntity{o: o}, true
		}
	}
	return cmdEntity{}, false
}

// execRelation is `execute on`: the entities related to the executor
// (known false for a relation that does not exist).
func (h *hub) execRelation(players map[int32]*tracked, rel string, en cmdEntity) ([]cmdEntity, bool) {
	var ids []int32
	switch rel {
	case "attacker": // Attackable.getLastAttacker
		switch {
		case en.m != nil:
			ids = append(ids, en.m.lastAttacker)
		case en.t != nil:
			if en.t.lastHurtByMob != 0 {
				ids = append(ids, en.t.lastHurtByMob)
			} else {
				ids = append(ids, en.t.pvpBy)
			}
		}
	case "controller": // getControllingPassenger: the first passenger
		switch {
		case en.m != nil && en.m.rider != 0:
			ids = append(ids, en.m.rider)
		case en.m != nil && len(en.m.riders) > 0:
			ids = append(ids, en.m.riders[0])
		case en.m != nil:
			ids = append(ids, en.m.mobRider)
		case en.o != nil:
			if v := h.vehicles[en.o.eid]; v != nil {
				if v.mobFirst && v.mobRider != 0 {
					ids = append(ids, v.mobRider)
				} else if v.rider != 0 {
					ids = append(ids, v.rider)
				} else {
					ids = append(ids, v.mobRider)
				}
			}
		}
	case "leasher": // Leashable.getLeashHolder
		if en.m != nil {
			ids = append(ids, en.m.leash)
		}
	case "origin": // TraceableEntity.getOwner: a projectile's shooter
		if en.o != nil {
			if a := h.arrows[en.o.eid]; a != nil {
				ids = append(ids, a.shooter)
			}
		}
	case "owner": // OwnableEntity.getOwner
		if en.m != nil && en.m.tamed {
			ids = append(ids, en.m.owner)
		}
	case "passengers":
		switch {
		case en.m != nil:
			ids = append(ids, en.m.rider, en.m.rider2)
			ids = append(ids, en.m.riders...)
			ids = append(ids, en.m.mobRider, en.m.mobRider2)
		case en.o != nil:
			if v := h.vehicles[en.o.eid]; v != nil {
				if v.mobFirst {
					ids = append(ids, v.mobRider, v.rider, v.rider2)
				} else {
					ids = append(ids, v.rider, v.rider2, v.mobRider)
				}
			}
		}
	case "target": // Targeting.getTarget
		if en.m != nil {
			ids = append(ids, en.m.targetEID)
		}
	case "vehicle":
		switch {
		case en.t != nil:
			ids = append(ids, en.t.ridingEID)
		case en.m != nil && en.m.mount != 0:
			ids = append(ids, en.m.mount)
		case en.m != nil:
			for _, v := range h.vehicles {
				if v.mobRider == en.m.eid {
					ids = append(ids, v.eid)
					break
				}
			}
		}
	default:
		return nil, false
	}
	var out []cmdEntity
	seen := map[int32]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if e, ok := h.execEntityByEID(players, id); ok {
			out = append(out, e)
		}
	}
	return out, true
}

// execStoreStep is `store result|success score|bossbar …`: the target is
// resolved now and called with the command's outcome later.
func (h *hub) execStoreStep(players map[int32]*tracked, s *execSource, a []string) ([]execSource, string) {
	st := execStore{result: a[0] == "result"}
	switch a[1] {
	case "score":
		var holders []string
		h.execWith(players, s, func(from *tracked) { holders = h.sbHolders(players, from.p.eid, a[2]) })
		if len(holders) == 0 {
			return nil, "No relevant score holders could be found"
		}
		if h.sb == nil || h.sb.Objectives[a[3]] == nil {
			return nil, fmt.Sprintf("Unknown scoreboard objective '%s'", a[3])
		}
		st.holders, st.obj = holders, a[3]
	case "bossbar":
		id, valid := normBossbarID(a[2])
		if !valid {
			return nil, "Invalid ID: " + a[2]
		}
		if h.rules.Bossbars[id] == nil {
			return nil, fmt.Sprintf("No bossbar exists with the ID '%s'", id)
		}
		switch a[3] {
		case "value":
		case "max":
			st.max = true
		default:
			return nil, execIncomplete
		}
		st.bossbar = id
	default:
		return nil, execNeedsData
	}
	ns := *s
	ns.stores = append(append([]execStore(nil), s.stores...), st)
	return []execSource{ns}, ""
}

// execTest runs one condition for one source: hit is whether it held
// (for a counting condition, whether the count is positive — blocks:
// whether the regions matched), count what it counted, numeric whether it
// counts; msg is the failure.
func (h *hub) execTest(players map[int32]*tracked, st execStep, s *execSource) (hit bool, count int, numeric bool, msg string) {
	a := st.args
	w := h.worldFor(s.dim)
	switch a[0] {
	case "block":
		pos, msg := h.execLoadedPos(s, a[1:4])
		if msg != "" {
			return false, 0, false, msg
		}
		pred, why := parseBlockPredicateNBT(a[4])
		if why != "" {
			return false, 0, false, why
		}
		state := w.At(pos.x, pos.y, pos.z)
		ok := pred.state(state)
		if ok && pred.nbt != nil {
			ok = nbtMatches(pred.nbt, h.blockEntityNBT(simPos{dim: s.dim, blockPos: pos}, state), true)
		}
		return ok, 0, false, ""
	case "blocks":
		ok, n, msg := h.execBlocks(s, a[1:4], a[4:7], a[7:10], a[10])
		return ok, n, true, msg
	case "entity":
		ens, msg := h.execSelect(players, s, a[1])
		return len(ens) > 0, len(ens), true, msg
	case "score":
		return h.execScore(players, s, a)
	case "biome":
		pos, msg := h.execLoadedPos(s, a[1:4])
		if msg != "" {
			return false, 0, false, msg
		}
		if tag, ok := strings.CutPrefix(a[4], "#"); ok { // a biome tag, a data pack's too
			members, found := biomeTagMembers(nsID(tag))
			if !found {
				return false, 0, false, fmt.Sprintf("Can't find tag '%s' of type 'minecraft:worldgen/biome'", nsID(tag))
			}
			return slices.Contains(members, nsID(w.BiomeAt3D(pos.x, pos.y, pos.z))), 0, false, ""
		}
		return nsID(w.BiomeAt3D(pos.x, pos.y, pos.z)) == nsID(a[4]), 0, false, ""
	case "dimension":
		dim, ok := parseDimension(a[1])
		if !ok {
			return false, 0, false, fmt.Sprintf("Unknown dimension '%s'", nsID(a[1]))
		}
		return dim == s.dim, 0, false, ""
	case "loaded":
		pos, msg := h.execBlockPos(s, a[1:4])
		if msg != "" {
			return false, 0, false, msg
		}
		return w != nil && w.Loaded(int32(pos.x>>4), int32(pos.z>>4)), 0, false, ""
	case "predicate": // a data pack's predicate (or vanilla's), or one written inline
		conds, msg := predicateFor(a[1])
		if msg != "" {
			return false, 0, false, msg
		}
		ctx := h.commandLootCtx(s)
		return ctx.condsPass(conds), 0, false, ""
	case "function":
		if tag, ok := strings.CutPrefix(a[1], "#"); ok {
			return false, 0, false, fmt.Sprintf("Unknown function tag '%s'", nsID(tag))
		}
		return false, 0, false, "Unknown function " + nsID(a[1])
	case "items":
		n, msg := h.execCountItems(players, s, a[1:])
		return n > 0, n, true, msg
	case "stopwatch":
		id, ok := parseResourceID(a[1])
		if !ok {
			return false, 0, false, fmt.Sprintf("Invalid identifier '%s'", a[1])
		}
		run, exists := h.stopwatchesLive()[id]
		if !exists {
			return false, 0, false, fmt.Sprintf("Stopwatch '%s' does not exist", id)
		}
		lo, hi, ok := parseSelectorFloatRange(a[2])
		if !ok {
			return false, 0, false, "Expected value or range of values"
		}
		secs := float64(run.elapsedMs(time.Now().UnixMilli())) / 1000
		return secs >= lo && secs <= hi, 0, false, ""
	}
	return false, 0, false, execIncomplete
}

// execScore is `if score`: two scores compared, or one against a range. A
// missing score fails the test.
func (h *hub) execScore(players map[int32]*tracked, s *execSource, a []string) (bool, int, bool, string) {
	holder := func(arg string) (string, string) {
		var hs []string
		h.execWith(players, s, func(from *tracked) { hs = h.sbHolders(players, from.p.eid, arg) })
		switch {
		case arg == "*" || len(hs) > 1:
			return "", "Only one entity is allowed, but the provided selector allows more than one"
		case len(hs) == 0:
			return "", "No entity was found"
		}
		return hs[0], ""
	}
	score := func(owner, obj string) (int32, bool) {
		v, ok := h.sb.Scores[owner][obj]
		return v, ok
	}
	if h.sb == nil {
		return false, 0, false, fmt.Sprintf("Unknown scoreboard objective '%s'", a[2])
	}
	target, msg := holder(a[1])
	if msg != "" {
		return false, 0, false, msg
	}
	if h.sb.Objectives[a[2]] == nil {
		return false, 0, false, fmt.Sprintf("Unknown scoreboard objective '%s'", a[2])
	}
	if a[3] == "matches" {
		lo, hi, ok := parseSelectorIntRange(a[4])
		if !ok {
			return false, 0, false, "Expected value or range of values"
		}
		v, has := score(target, a[2])
		return has && int64(v) >= lo && int64(v) <= hi, 0, false, ""
	}
	source, msg := holder(a[4])
	if msg != "" {
		return false, 0, false, msg
	}
	if h.sb.Objectives[a[5]] == nil {
		return false, 0, false, fmt.Sprintf("Unknown scoreboard objective '%s'", a[5])
	}
	x, hasX := score(target, a[2])
	y, hasY := score(source, a[5])
	var ok bool
	switch a[3] {
	case "<":
		ok = x < y
	case "<=":
		ok = x <= y
	case "=":
		ok = x == y
	case ">":
		ok = x > y
	case ">=":
		ok = x >= y
	default:
		return false, 0, false, execIncomplete
	}
	return hasX && hasY && ok, 0, false, ""
}

// execBlocks is checkRegions: the box from start to end matched block for
// block (and block entity for block entity) at the destination, skipping
// the source's air when masked; the count is the blocks compared.
func (h *hub) execBlocks(s *execSource, startArg, endArg, destArg []string, mode string) (bool, int, string) {
	masked := false
	switch mode {
	case "all":
	case "masked":
		masked = true
	default:
		return false, 0, execIncomplete
	}
	start, msg := h.execLoadedPos(s, startArg)
	if msg != "" {
		return false, 0, msg
	}
	end, msg := h.execLoadedPos(s, endArg)
	if msg != "" {
		return false, 0, msg
	}
	dest, msg := h.execLoadedPos(s, destArg)
	if msg != "" {
		return false, 0, msg
	}
	lo := blockPos{min(start.x, end.x), min(start.y, end.y), min(start.z, end.z)}
	hi := blockPos{max(start.x, end.x), max(start.y, end.y), max(start.z, end.z)}
	area := int64(hi.x-lo.x+1) * int64(hi.y-lo.y+1) * int64(hi.z-lo.z+1)
	if area > execMaxBlocks {
		return false, 0, fmt.Sprintf("Too many blocks in the specified area (maximum %d, but specified %d)", execMaxBlocks, area)
	}
	w := h.worldFor(s.dim)
	dx, dy, dz := dest.x-lo.x, dest.y-lo.y, dest.z-lo.z
	count := 0
	for z := lo.z; z <= hi.z; z++ {
		for y := lo.y; y <= hi.y; y++ {
			for x := lo.x; x <= hi.x; x++ {
				src := w.At(x, y, z)
				if masked && src == worldgen.Air {
					continue
				}
				dst := w.At(x+dx, y+dy, z+dz)
				if src != dst {
					return false, 0, ""
				}
				if sbe := h.blockEntityNBT(simPos{dim: s.dim, blockPos: blockPos{x, y, z}}, src); sbe != nil {
					dbe := h.blockEntityNBT(simPos{dim: s.dim, blockPos: blockPos{x + dx, y + dy, z + dz}}, dst)
					if dbe == nil || !nbtMatches(sbe, dbe, false) || !nbtMatches(dbe, sbe, false) {
						return false, 0, ""
					}
				}
				count++
			}
		}
	}
	return true, count, ""
}

// execCountItems is `if items entity <targets>|block <pos> <slots>
// <predicate>`: the items in the slots the predicate matches, counted.
// Only players' slots are modelled among entities.
func (h *hub) execCountItems(players map[int32]*tracked, s *execSource, a []string) (int, string) {
	var targets []itemTarget
	var rest []string
	switch a[0] {
	case "entity":
		ens, msg := h.execSelect(players, s, a[1])
		if msg != "" {
			return 0, msg
		}
		if len(ens) == 0 {
			return 0, "No entity was found"
		}
		for _, en := range ens {
			if en.t != nil {
				targets = append(targets, h.playerItemTarget(en.t))
			}
		}
		rest = a[2:]
	case "block":
		pos, msg := h.execLoadedPos(s, a[1:4])
		if msg != "" {
			return 0, msg
		}
		tg, ok := h.blockItemTarget(s.dim, pos)
		if !ok {
			return 0, fmt.Sprintf("Source position %d, %d, %d is not a container", pos.x, pos.y, pos.z)
		}
		targets = append(targets, tg)
		rest = a[4:]
	default:
		return 0, execIncomplete
	}
	if len(rest) != 2 {
		return 0, execIncomplete
	}
	ids, ok := slotRanges[rest[0]]
	if !ok {
		return 0, fmt.Sprintf("Unknown slot '%s'", rest[0])
	}
	match, msg := parseItemPredicate(rest[1])
	if msg != "" {
		return 0, msg
	}
	n := 0
	for _, tg := range targets {
		for _, sl := range tg.slots(ids) {
			if st := sl.get(); st.item != 0 && st.count > 0 && match(st) {
				n += st.count
			}
		}
	}
	return n, ""
}

// parseItemPredicate is ItemPredicateArgument for what the engine can
// test: `*`, an item id or an item #tag (vanilla's or a data pack's).
// Component predicates are refused.
func parseItemPredicate(arg string) (func(invStack) bool, string) {
	base, comps := arg, ""
	if i := strings.IndexByte(arg, '['); i >= 0 {
		base, comps = arg[:i], arg[i:]
	}
	if comps != "" && comps != "[]" {
		return nil, "Item predicate components can't be tested yet: " + comps
	}
	if base == "*" {
		return func(invStack) bool { return true }, ""
	}
	if tag, ok := strings.CutPrefix(base, "#"); ok { // an item tag, a data pack's too
		ids, found := itemTagIDs(nsID(tag))
		if !found {
			return nil, fmt.Sprintf("Unknown item tag '%s'", nsID(tag))
		}
		set := make(map[int32]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
		return func(st invStack) bool { return set[st.item] }, ""
	}
	id, ok := itemByName[strings.TrimPrefix(base, "minecraft:")]
	if !ok {
		return nil, fmt.Sprintf("Unknown item '%s'", nsID(base))
	}
	return func(st invStack) bool { return st.item == id }, ""
}
