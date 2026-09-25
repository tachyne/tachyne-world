package server

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Three small commands: /random (RandomCommand's value and roll — anyone may
// use them — and a gamemaster's named sequences and reset), /swing
// (SwingCommand) and /teammsg, alias /tm (TeamMsgCommand).

// ---- /random ------------------------------------------------------------------

type evRandomCmd struct {
	by       int32
	name     string
	min, max int
	announce bool // roll: everyone hears it
	// seq draws from a named sequence (RandomSequences.get) instead of the
	// level's random.
	seq string
	// reset is `/random reset`: seq names the sequence ("*" = all of them),
	// and withSeed carries the salt and the two include flags.
	reset                bool
	withSeed             bool
	salt                 int32
	worldSeed, includeID bool
}

func (evRandomCmd) isHubEvent() {}

// parseIntRange is MinMaxBounds.Ints: "a..b", "a..", "..b" or "a". A missing
// end is open (the int range's own limit).
func parseIntRange(s string) (lo, hi int, msg string) {
	lo, hi = math.MinInt32, math.MaxInt32
	a, b, ranged := strings.Cut(s, "..")
	if !ranged {
		b = a
	}
	if a == "" && b == "" {
		return 0, 0, "Expected value or range of values"
	}
	parse := func(v string, into *int) string {
		if v == "" {
			return ""
		}
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			if _, ferr := strconv.ParseFloat(v, 64); ferr == nil {
				return "Only whole numbers are allowed; not decimals"
			}
			return fmt.Sprintf("Invalid integer '%s'", v)
		}
		*into = int(n)
		return ""
	}
	if m := parse(a, &lo); m != "" {
		return 0, 0, m
	}
	if m := parse(b, &hi); m != "" {
		return 0, 0, m
	}
	if lo > hi {
		return 0, 0, "Min cannot be bigger than max"
	}
	return lo, hi, ""
}

func (s *Server) cmdRandom(p *player, args []string) {
	if len(args) >= 1 && args[0] == "reset" {
		s.cmdRandomReset(p, args[1:])
		return
	}
	if len(args) < 2 || len(args) > 3 || (args[0] != "value" && args[0] != "roll") {
		p.tell("Usage: /random value|roll <range> [<sequence>]")
		return
	}
	e := evRandomCmd{by: p.eid, name: p.name, announce: args[0] == "roll"}
	if len(args) == 3 { // <sequence>: a gamemaster's
		if !s.isOp(p.name) {
			p.tell("You don't have permission.")
			return
		}
		id, ok := parseResourceID(args[2])
		if !ok {
			p.tell(fmt.Sprintf("Invalid identifier '%s'", args[2]))
			return
		}
		e.seq = id
	}
	lo, hi, msg := parseIntRange(args[1])
	if msg != "" {
		p.tell(msg)
		return
	}
	switch span := int64(hi) - int64(lo); {
	case span == 0:
		p.tell("The range of the random value must be at least 1")
		return
	case span >= math.MaxInt32:
		p.tell("The range of the random value must be at most 2147483647")
		return
	}
	e.min, e.max = lo, hi
	s.hub.post(e)
}

// cmdRandomReset is `/random reset (*|<sequence>) [<seed> [<includeWorldSeed>
// [<includeSequenceId>]]]`.
func (s *Server) cmdRandomReset(p *player, args []string) {
	if !s.isOp(p.name) { // reset: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	usage := "Usage: /random reset (*|<sequence>) [<seed> [<includeWorldSeed> [<includeSequenceId>]]]"
	if len(args) < 1 || len(args) > 4 {
		p.tell(usage)
		return
	}
	e := evRandomCmd{by: p.eid, name: p.name, reset: true, seq: "*", worldSeed: true, includeID: true}
	if args[0] != "*" {
		id, ok := parseResourceID(args[0])
		if !ok {
			p.tell(fmt.Sprintf("Invalid identifier '%s'", args[0]))
			return
		}
		e.seq = id
	}
	if len(args) >= 2 {
		n, err := strconv.ParseInt(args[1], 10, 32)
		if err != nil {
			p.tell(fmt.Sprintf("Invalid integer '%s'", args[1]))
			return
		}
		e.withSeed, e.salt = true, int32(n)
	}
	for i, into := range []*bool{&e.worldSeed, &e.includeID} {
		if len(args) < 3+i {
			break
		}
		switch args[2+i] {
		case "true":
			*into = true
		case "false":
			*into = false
		default:
			p.tell(fmt.Sprintf("Invalid boolean, expected 'true' or 'false' but found '%s'", args[2+i]))
			return
		}
	}
	s.hub.post(e)
}

// parseResourceID is IdentifierArgument: a namespaced id, the namespace
// defaulting to minecraft, in the characters an Identifier allows.
func parseResourceID(s string) (string, bool) {
	ns, path, ok := strings.Cut(s, ":")
	if !ok {
		ns, path = "minecraft", s
	}
	if ns == "" {
		ns = "minecraft"
	}
	if path == "" {
		return "", false
	}
	for _, c := range ns {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return "", false
		}
	}
	for _, c := range path {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == '/') {
			return "", false
		}
	}
	return ns + ":" + path, true
}

// applyRandomCommand draws on the hub's random source (randomBetweenInclusive),
// or on a named sequence, which it creates on first use.
func (h *hub) applyRandomCommand(players map[int32]*tracked, e evRandomCmd) int {
	if e.reset {
		h.applyRandomReset(players, e)
		return 0
	}
	var v int
	if e.seq != "" {
		r := h.randomSequence(e.seq)
		v = e.min + int(r.nextIntN(int32(e.max-e.min+1)))
		h.saveRandomSequence(e.seq, r)
	} else {
		v = e.min + h.rng.Intn(e.max-e.min+1)
	}
	if e.announce {
		h.broadcastChat(players, fmt.Sprintf("%s rolled %d (from %d to %d)", e.name, v, e.min, e.max))
	} else {
		h.cmdInfo(players, e.by)(fmt.Sprintf("Randomized value: %d", v))
	}
	return v
}

// applyRandomReset is RandomCommand's resetSequence and resetAllSequences.
func (h *hub) applyRandomReset(players map[int32]*tracked, e evRandomCmd) {
	rs := h.randomSeqs()
	if e.seq == "*" {
		if e.withSeed { // resetAllSequencesAndSetNewDefaults
			rs.Salt, rs.NoWorldSeed, rs.NoSequenceID = e.salt, !e.worldSeed, !e.includeID
		}
		n := len(rs.Sequences)
		rs.Sequences = nil
		h.saveRules()
		h.cmdInfo(players, e.by)(fmt.Sprintf("Reset %d random sequence(s)", n))
		return
	}
	salt, ws, id := rs.Salt, !rs.NoWorldSeed, !rs.NoSequenceID
	if e.withSeed {
		salt, ws, id = e.salt, e.worldSeed, e.includeID
	}
	h.saveRandomSequence(e.seq, newRandomSequence(e.seq, h.world.Seed(), salt, ws, id))
	h.cmdInfo(players, e.by)("Reset random sequence " + e.seq)
}

// randomSequencesSave is RandomSequences: the defaults new sequences are
// seeded with and every sequence's Xoroshiro state. The include flags are
// stored inverted so that an absent field is vanilla's default (true).
type randomSequencesSave struct {
	Salt         int32                `json:"salt,omitempty"`
	NoWorldSeed  bool                 `json:"noWorldSeed,omitempty"`
	NoSequenceID bool                 `json:"noSequenceId,omitempty"`
	Sequences    map[string][2]uint64 `json:"sequences,omitempty"`
}

func (h *hub) randomSeqs() *randomSequencesSave {
	if h.rules.RandomSequences == nil {
		h.rules.RandomSequences = &randomSequencesSave{}
	}
	return h.rules.RandomSequences
}

// randomSequence is RandomSequences.get: the named sequence, created from
// the defaults on first use.
func (h *hub) randomSequence(id string) *xoroshiro {
	rs := h.randomSeqs()
	if st, ok := rs.Sequences[id]; ok {
		return &xoroshiro{st[0], st[1]}
	}
	return newRandomSequence(id, h.world.Seed(), rs.Salt, !rs.NoWorldSeed, !rs.NoSequenceID)
}

// saveRandomSequence records a sequence's state (the saved data is dirtied by
// every draw) and writes it out.
func (h *hub) saveRandomSequence(id string, r *xoroshiro) {
	rs := h.randomSeqs()
	if rs.Sequences == nil {
		rs.Sequences = map[string][2]uint64{}
	}
	rs.Sequences[id] = [2]uint64{r.lo, r.hi}
	h.saveRules()
}

// newRandomSequence is RandomSequences.createSequence and RandomSequence's
// constructor: the world seed (or not) xor the salt, upgraded to 128 bits
// unmixed, xored with the MD5 of the id (or not), then mixed.
func newRandomSequence(id string, worldSeed int64, salt int32, includeWorldSeed, includeID bool) *xoroshiro {
	var seed int64
	if includeWorldSeed {
		seed = worldSeed
	}
	seed ^= int64(salt)
	lo := uint64(seed) ^ xoroSilver
	hi := lo + xoroGolden
	if includeID {
		sum := md5.Sum([]byte(id))
		lo ^= binary.BigEndian.Uint64(sum[:8])
		hi ^= binary.BigEndian.Uint64(sum[8:])
	}
	return newXoroshiroPair(mixStafford13(lo), mixStafford13(hi))
}

// ---- /swing -------------------------------------------------------------------

type evSwingCmd struct {
	by     int32
	target string // "@s" when none was named
	hand   int32  // 0 main, 1 off
}

func (evSwingCmd) isHubEvent() {}

func (s *Server) cmdSwing(p *player, args []string) {
	if !s.isOp(p.name) { // SwingCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	e := evSwingCmd{by: p.eid, target: "@s"}
	switch len(args) {
	case 0:
	case 1, 2:
		e.target = args[0]
		if len(args) == 2 {
			switch args[1] {
			case "mainhand":
			case "offhand":
				e.hand = 1
			default:
				p.tell("Usage: /swing [<targets> [mainhand|offhand]]")
				return
			}
		}
	default:
		p.tell("Usage: /swing [<targets> [mainhand|offhand]]")
		return
	}
	s.hub.post(e)
}

// applySwingCommand is LivingEntity.swing(hand, updateSelf=true) for each
// target: everyone watching sees the arm go, a player's own client included.
func (h *hub) applySwingCommand(players map[int32]*tracked, e evSwingCmd) {
	tell := cmdTeller(players, e.by)
	okTell := h.cmdOK(players, e.by) // sendSuccess(…, true)
	ens := h.commandEntities(players, e.by, e.target)
	if len(ens) == 0 {
		tell("No living entities were found to swing")
		return
	}
	for _, en := range ens {
		sw := attachproto.Swing{EID: en.eid(), Hand: e.hand}
		if t := en.t; t != nil {
			t.p.trySendEv(sw)
			h.toOthersNear(players, t.p.eid, t.dim, t.x, t.z, sw)
		} else {
			h.toTracking(players, en.m.eid, en.m.dim, en.m.x, en.m.z, sw)
		}
	}
	if len(ens) == 1 {
		okTell("Made " + ens[0].name() + " swing an arm")
	} else {
		okTell(fmt.Sprintf("Made %d entities swing their arms", len(ens)))
	}
}

// ---- /teammsg -----------------------------------------------------------------

type evTeamMsg struct {
	from int32
	text string
}

func (evTeamMsg) isHubEvent() {}

func (s *Server) cmdTeamMsg(p *player, args []string) {
	if len(args) == 0 {
		p.tell("Usage: /teammsg <message>")
		return
	}
	s.hub.post(evTeamMsg{from: p.eid, text: strings.Join(args, " ")})
}

// applyTeamMsg sends the line to the sender's team: "[Team] [name] text" to
// the others, "-> [Team] [name] text" back to the sender (chat.type.team.text and
// .sent). The name is bracketed as the room's chat lines are, not in vanilla's
// <name>: a system line shaped "<name> …" is what the client's secure-chat
// check hides from everyone but the sender on an offline-mode server.
func (h *hub) applyTeamMsg(players map[int32]*tracked, e evTeamMsg) {
	from := players[e.from]
	if from == nil {
		return
	}
	team := h.teamOf(from.p.name)
	if team == "" {
		from.p.trySendEv(chatEv("You must be on a team to message your team"))
		return
	}
	title := team
	if t := h.sb.Teams[team]; t != nil && t.Title != "" {
		title = t.Title
	}
	for _, t := range players {
		switch {
		case t == from:
			t.p.trySendEv(chatEv(fmt.Sprintf("-> [%s] [%s] %s", title, from.p.name, e.text)))
		case h.teamOf(t.p.name) == team:
			t.p.trySendEv(chatEv(fmt.Sprintf("[%s] [%s] %s", title, from.p.name, e.text)))
		}
	}
}
