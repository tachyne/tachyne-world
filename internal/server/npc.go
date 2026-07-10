package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"
)

// LLM-driven NPCs — the differentiator. An NPC is a villager mob whose actions
// come from a language model instead of a hardcoded behavior. The slow model call
// runs OFF the tick loop (a goroutine); its decision is applied back on the hub
// goroutine via an event, so the 20-TPS world never blocks on the network. Smooth
// per-tick movement toward a chosen target reuses the mob steering.

const (
	npcDecisionInterval = 100 // ticks between decisions (~5 s) — NOT 20 Hz
	npcThinkTimeout     = 45 * time.Second
	npcArriveDist       = 1.5 // how close counts as "reached the target"
	npcHistoryMax       = 16  // conversation turns kept in context (session memory)
	npcMemoryMax        = 40  // durable remembered facts (persisted to a file)
	npcFaceRange        = 16  // face the nearest player within this many blocks
	npcThinkRange       = 32  // only NPCs with a player this close make decisions
	npcMaxThinking      = 2   // cap concurrent model calls (one GPU serves them all)
)

var (
	entityVillager = entityID("villager") // minecraft:entity_type "villager" (1.21.5)
)

// npc is the brain + identity attached to a villager mob (by entity id).
type npc struct {
	eid          int32
	name         string
	persona      string
	mob          *mob // the villager entity it drives (movement/rendering)
	targetX      float64
	targetZ      float64
	hasTarget    bool
	inFlight     bool     // a model call is in progress (no overlap)
	lastDecision uint64   // tick of the last decision
	history      []string // recent conversation turns (session memory)
	memory       []string // durable remembered facts (loaded/saved to a file)
}

// npcAction is the JSON the model returns for its next move.
type npcAction struct {
	Action string   `json:"action"`           // say | move_to | goto_player | wander | idle
	Text   string   `json:"text,omitempty"`   // for "say"
	X      *float64 `json:"x,omitempty"`      // for "move_to"
	Z      *float64 `json:"z,omitempty"`      // for "move_to"
	Target string   `json:"target,omitempty"` // player name, for "goto_player"
}

// npcBehavior steers a villager toward the target its brain last chose. With no
// target it stands still (the brain decides when to move). Slots into the same
// Behavior interface as herd/wander.
type npcBehavior struct{}

func (npcBehavior) name() string { return "npc" }
func (npcBehavior) steer(h *hub, m *mob) (float64, float64) {
	n := h.npcs[m.eid]
	if n == nil || !n.hasTarget {
		return 0, 0
	}
	dx, dz := n.targetX-m.x, n.targetZ-m.z
	if d := math.Hypot(dx, dz); d > npcArriveDist {
		return dx / d * mobSpeed, dz / d * mobSpeed // full walk speed toward target
	}
	n.hasTarget = false // arrived
	return 0, 0
}

// spawnNPC creates a villager driven by the model.
func (h *hub) spawnNPC(players map[int32]*tracked, name, persona string, x, z float64) *npc {
	y := float64(h.world.SurfaceFeet(int(math.Floor(x)), int(math.Floor(z))))
	m := h.spawnMob(players, entityVillager, x, y, z)
	m.behavior = npcBehavior{}
	m.usesDoors = true // LLM villagers open wooden doors they walk up to
	n := &npc{eid: m.eid, name: name, persona: persona, mob: m, memory: loadNPCMemory(name)}
	saveNPCMemory(name, n.memory) // rewrite the file deduped (self-heals old spam)
	h.npcs[m.eid] = n
	return n
}

// runNPCs checks each NPC and, when one is due for a decision and not already
// thinking, snapshots its perception (on this goroutine) and fires the model call
// asynchronously. Only runs when someone is online to see it.
func (h *hub) runNPCs(players map[int32]*tracked) {
	if h.llm == nil || len(players) == 0 {
		return
	}
	now := h.tick.Load()
	inflight := 0 // how many model calls are already running (shared GPU)
	for _, n := range h.npcs {
		if n.inFlight {
			inflight++
		}
	}
	for _, n := range h.npcs {
		if inflight >= npcMaxThinking {
			break // protect the GPU: don't pile on more concurrent calls
		}
		if n.inFlight || now-n.lastDecision < npcDecisionInterval {
			continue
		}
		if h.nearestPlayer(players, n.mob.x, n.mob.z, npcThinkRange) == nil {
			continue // nobody near this villager — no need to think
		}
		n.inFlight = true
		n.lastDecision = now
		inflight++
		go h.npcThink(n, h.npcSystemPrompt(n), h.npcPerceive(players, n))
	}
}

// npcThink (goroutine) calls the model and posts the decision back to the hub.
func (h *hub) npcThink(n *npc, system, perception string) {
	ctx, cancel := context.WithTimeout(context.Background(), npcThinkTimeout)
	defer cancel()
	reply, err := h.llm.complete(ctx, system, perception)
	if err != nil {
		h.post(evNPCDecision{eid: n.eid}) // clears inFlight; no action
		return
	}
	var a npcAction
	if json.Unmarshal([]byte(extractJSON(reply)), &a) != nil {
		h.post(evNPCDecision{eid: n.eid})
		return
	}
	h.post(evNPCDecision{eid: n.eid, action: &a})
}

// npcAct applies a decided action on the hub goroutine.
func (h *hub) npcAct(players map[int32]*tracked, n *npc, a npcAction) {
	switch strings.ToLower(a.Action) {
	case "say":
		if a.Text == "" {
			return
		}
		body := chatEv(fmt.Sprintf("<%s> %s", n.name, a.Text))
		for _, t := range players {
			t.p.trySendEv(body)
		}
		n.history = appendCap(n.history, n.name+": "+a.Text, npcHistoryMax)
		h.bus.publish("npc_say", map[string]any{"name": n.name, "text": a.Text})
		log.Printf("npc %s: say %q", n.name, a.Text)
	case "remember":
		// Dedup: a reworded restatement of an existing fact is dropped, so the
		// model can't spam its memory by re-remembering the same thing each tick.
		if mem, added := addMemory(n.memory, a.Text); added {
			n.memory = mem
			saveNPCMemory(n.name, n.memory)
			log.Printf("npc %s: remember %q", n.name, a.Text)
		} else {
			log.Printf("npc %s: remember (already known, skipped)", n.name)
		}
	case "move_to":
		if a.X != nil && a.Z != nil {
			n.targetX, n.targetZ, n.hasTarget = *a.X, *a.Z, true
		}
	case "goto_player":
		for _, t := range players {
			if strings.EqualFold(t.p.name, a.Target) {
				n.targetX, n.targetZ, n.hasTarget = t.x, t.z, true
				log.Printf("npc %s: goto_player %s", n.name, a.Target)
				break
			}
		}
	case "wander":
		ang := h.rng.Float64() * 2 * math.Pi
		n.targetX = n.mob.x + math.Cos(ang)*8
		n.targetZ = n.mob.z + math.Sin(ang)*8
		n.hasTarget = true
		log.Printf("npc %s: wander", n.name)
	case "idle":
		n.hasTarget = false
	default:
		log.Printf("npc %s: action %q", n.name, a.Action)
	}
}

// npcSystemPrompt is the NPC's persona + the action contract.
func (h *hub) npcSystemPrompt(n *npc) string {
	return fmt.Sprintf(`You are %s, %s. You live in a Minecraft village and can move around, talk, and watch the players.

Reply with ONE JSON object for your next action and NOTHING else. Valid forms:
{"action":"say","text":"<short in-character line>"}   - speak to nearby players
{"action":"goto_player","target":"<player name>"}    - walk over to a player
{"action":"wander"}                                  - stroll around a little
{"action":"remember","text":"<fact>"}                - save something to long-term memory
{"action":"idle"}                                    - wait and watch

Rules:
- If a player is speaking to you, REPLY with "say" and stay nearby — do not wander off mid-conversation.
- Most turns should be "say" or "idle". Only use "remember" for a genuinely NEW fact that is NOT already in "What you remember" below — never re-save something you already know. After remembering once, go back to talking.
- Keep spoken lines to one short, in-character sentence. Output only the JSON.`, n.name, n.persona)
}

// npcPerceive snapshots what the NPC currently senses, as the user turn.
func (h *hub) npcPerceive(players map[int32]*tracked, n *npc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are at x=%.0f z=%.0f. It is %s.\n", n.mob.x, n.mob.z, daytimeName(h.dayTime.Load()))
	var near []string
	for _, t := range players {
		d := math.Hypot(t.x-n.mob.x, t.z-n.mob.z)
		if d <= 40 {
			near = append(near, fmt.Sprintf("%s is %.0f blocks away at x=%.0f z=%.0f", t.p.name, d, t.x, t.z))
		}
	}
	if len(near) == 0 {
		b.WriteString("No players are nearby.\n")
	} else {
		b.WriteString("Nearby: " + strings.Join(near, "; ") + ".\n")
	}
	if len(n.memory) > 0 {
		b.WriteString("What you remember:\n- " + strings.Join(n.memory, "\n- ") + "\n")
	}
	if len(n.history) > 0 {
		b.WriteString("Recent conversation:\n" + strings.Join(n.history, "\n") + "\n")
	}
	b.WriteString("What do you do next?")
	return b.String()
}

// nearestPlayer returns the closest player within maxDist blocks of (x,z), or nil.
func (h *hub) nearestPlayer(players map[int32]*tracked, x, z, maxDist float64) *tracked {
	var best *tracked
	bestD2 := maxDist * maxDist
	for _, t := range players {
		if d2 := (t.x-x)*(t.x-x) + (t.z-z)*(t.z-z); d2 < bestD2 {
			best, bestD2 = t, d2
		}
	}
	return best
}

// npcsHear adds a chat line to every NPC's conversation memory.
func (h *hub) npcsHear(line string) {
	for _, n := range h.npcs {
		n.history = appendCap(n.history, line, npcHistoryMax)
	}
}

// appendCap appends to a rolling slice with a maximum length.
func appendCap(s []string, v string, max int) []string {
	s = append(s, v)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

// npcMemPath is the durable-memory file for an NPC (in the server's work dir).
func npcMemPath(name string) string { return "npc-mem-" + strings.ToLower(name) + ".txt" }

func loadNPCMemory(name string) []string {
	data, err := os.ReadFile(npcMemPath(name))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out, _ = addMemory(out, l) // collapse any duplicates already on disk
		}
	}
	return out
}

// addMemory appends fact unless it restates something already remembered (so a
// reworded duplicate is dropped). Returns the (possibly unchanged) memory and
// whether it was actually added.
func addMemory(mem []string, fact string) ([]string, bool) {
	if fact = strings.TrimSpace(fact); fact == "" {
		return mem, false
	}
	for _, m := range mem {
		if memSimilar(m, fact) {
			return mem, false
		}
	}
	return appendCap(mem, fact, npcMemoryMax), true
}

// memSimilar reports whether two remembered facts are basically the same, by
// word-set overlap (Jaccard) over content words.
func memSimilar(a, b string) bool {
	wa, wb := contentWords(a), contentWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	return float64(inter)/float64(len(wa)+len(wb)-inter) >= 0.35
}

var memStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "their": true, "they": true,
	"have": true, "has": true, "had": true, "that": true, "this": true, "when": true,
	"who": true, "are": true, "was": true, "his": true, "her": true, "she": true,
	"him": true, "you": true, "your": true, "would": true, "could": true, "hoping": true,
	"mentioned": true, "really": true, "very": true, "someday": true,
}

// contentWords lowercases, drops punctuation, short words, and common filler.
func contentWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(w) >= 3 && !memStopWords[w] {
			out[w] = true
		}
	}
	return out
}

func saveNPCMemory(name string, mem []string) {
	tmp := npcMemPath(name) + ".tmp"
	if os.WriteFile(tmp, []byte(strings.Join(mem, "\n")+"\n"), 0o644) == nil {
		os.Rename(tmp, npcMemPath(name))
	}
}

// daytimeName describes the time of day for the prompt.
func daytimeName(dayTime uint64) string {
	switch t := dayTime % dayLengthTicks; {
	case t < 1000 || t >= 23000:
		return "dawn"
	case t < 6000:
		return "morning"
	case t < 12000:
		return "afternoon"
	case t < 13000:
		return "dusk"
	default:
		return "night"
	}
}
