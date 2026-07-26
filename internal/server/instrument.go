package server

// Goat horns. Eight instruments, each its own call, audible across a very
// large radius — vanilla's range is 256 blocks, which is why the volume it
// plays at (range/16) is so far above 1.
//
// The horn a player holds carries its instrument on the stack; nothing in
// this world drops one yet (that is the goat's ram, tracked separately), so
// in practice a horn comes from creative and sounds Ponder.

var itemGoatHorn = itemByName["goat_horn"]

// instrumentSounds is the goat_horn_instrument registry in order — the
// number in the sound event is the index, so the two never drift.
var instrumentSounds = []string{
	"minecraft:item.goat_horn.sound.0", // ponder
	"minecraft:item.goat_horn.sound.1", // sing
	"minecraft:item.goat_horn.sound.2", // seek
	"minecraft:item.goat_horn.sound.3", // feel
	"minecraft:item.goat_horn.sound.4", // admire
	"minecraft:item.goat_horn.sound.5", // call
	"minecraft:item.goat_horn.sound.6", // yearn
	"minecraft:item.goat_horn.sound.7", // dream
}

const (
	hornRange    = 256.0 // vanilla Instrument.range for every goat horn
	hornUseSecs  = 7.0   // Instrument.useDuration; the cooldown is the same
	hornCooldown = int(hornUseSecs * 20)
)

type evUseHorn struct{ eid int32 }

func (evUseHorn) isHubEvent() {}

// tootHorn plays the held horn and puts it on cooldown. The sound carries far
// enough that it is broadcast to the whole dimension rather than the usual
// nearby radius.
func (h *hub) tootHorn(players map[int32]*tracked, t *tracked) {
	if t.dead || t.inv == nil {
		return
	}
	held := heldStack(t)
	if held.item != itemGoatHorn {
		return
	}
	if h.onCooldown(t, itemGoatHorn) {
		return
	}
	h.setCooldown(t, itemGoatHorn, hornCooldown)
	i := int(held.instrument)
	if i < 0 || i >= len(instrumentSounds) {
		i = 0
	}
	h.playSoundDim(players, t.dim, instrumentSounds[i], sndRecord, t.x, t.y, t.z, hornRange/16, 1)
}

// onCooldown / setCooldown are vanilla's per-item use cooldown, kept
// server-side. The client draws its own sweep from the packet vanilla sends;
// this is the half that decides whether a use counts.
func (h *hub) onCooldown(t *tracked, item int32) bool {
	return t.cooldowns != nil && h.tick.Load() < t.cooldowns[item]
}

func (h *hub) setCooldown(t *tracked, item int32, ticks int) {
	if t.cooldowns == nil {
		t.cooldowns = map[int32]uint64{}
	}
	t.cooldowns[item] = h.tick.Load() + uint64(ticks)
}
