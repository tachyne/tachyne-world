package server

import (
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The decorated pot: a container holding exactly ONE stack
// (ContainerSingleItem). It is not a chest with a smaller window — vanilla
// gives it no menu at all. A right click with something puts ONE of it in,
// a right click with anything else (or an empty hand) only rocks the pot;
// what is inside comes back when the pot is broken, and not before.

var decoratedPotMin, decoratedPotMax = worldgen.BlockRange("decorated_pot")

var itemBrick = int32(itemByName["brick"])

// Wobble styles (DecoratedPotBlockEntity.WobbleStyle, block event action 1):
// the pot leans in when it takes something, back when it refuses.
const (
	potWobblePositive = 0
	potWobbleNegative = 1
)

// isDecoratedPot reports whether a state is a decorated pot.
func isDecoratedPot(s uint32) bool { return s >= decoratedPotMin && s <= decoratedPotMax }

// usePot is DecoratedPotBlock.useItemOn: one item goes in if the pot is
// empty or already holds the same thing with room left; anything else is a
// refusal. Reports whether the pot handled the interaction.
func (h *hub) usePot(players map[int32]*tracked, t *tracked, pos blockPos) bool {
	if t.inv == nil {
		return false
	}
	if h.pots == nil {
		h.pots = map[simPos]invStack{}
	}
	key := simPos{dim: t.dim, blockPos: pos}
	h.ensurePotLoot(key)
	held := usedStack(t)
	stored := h.pots[key]
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5

	fits := stored.item == 0 || (sameItemComponents(stored, held) && stored.count < stackCap(stored.item))
	if held.item == 0 || held.count == 0 || !fits {
		// useWithoutItem: the pot rocks back and refuses. Nothing comes out —
		// the only way to empty a pot is to break it.
		h.playSoundDim(players, t.dim, "minecraft:block.decorated_pot.insert_fail", sndBlock, cx, cy, cz, 1, 1)
		h.potWobble(players, key, potWobbleNegative)
		h.vib(t.dim, freqBlockChange, pos.x, pos.y, pos.z, t.p.eid) // useWithoutItem: BLOCK_CHANGE
		return true
	}
	one := held
	one.count = 1
	if stored.item == 0 {
		stored = one
	} else {
		stored.count++
	}
	h.pots[key] = stored
	slot := t.useSlot()
	if t.gamemode != gmCreative {
		if s := t.handStack(slot); s != nil { // the hand the item came from
			if s.count--; s.count <= 0 {
				*s = invStack{}
			}
			h.sendHandSlot(t, slot)
		}
	}
	h.incStat(t, attachproto.StatUsed, held.item, 1)
	// The insert sound rises with how full the pot is, so you can hear a pot
	// filling up without opening anything.
	fill := float32(stored.count) / float32(stackCap(stored.item))
	h.playSoundDim(players, t.dim, "minecraft:block.decorated_pot.insert", sndBlock, cx, cy, cz, 1, 0.7+0.5*fill)
	// DecoratedPotBlock.useItemOn: seven dust motes puff off the rim. Sent in
	// the pot's own dimension, not the overworld.
	h.toNearbyEv(players, key.dim, cx, cz, attachproto.Particles{
		PID: particleDustPlume, X: cx, Y: float64(key.y) + 1.2, Z: cz, Count: potPlumeCount})
	h.potWobble(players, key, potWobblePositive)
	// setChanged → a comparator reading the pot sees it fill; and the
	// insert is a BLOCK_CHANGE for sculk.
	h.inDim(t.dim, func() { h.updateNeighbourForOutputSignal(players, pos); h.nbRun(players) })
	h.vib(t.dim, freqBlockChange, pos.x, pos.y, pos.z, t.p.eid)
	return true
}

const (
	particleDustPlume = 104 // canonical-770 minecraft:dust_plume
	potPlumeCount     = 7   // sendParticles(..., 7, 0, 0, 0, 0)
)

// potWobble sends the pot's block event; every client animates the lean.
func (h *hub) potWobble(players map[int32]*tracked, pos simPos, style uint8) {
	id, ok := worldgen.BlockRegistryID("decorated_pot")
	if !ok {
		return
	}
	h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), attachproto.BlockEvent{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Action: 1, Param: style, Block: int32(id)})
}

// spillPot scatters a pot's contents when the block goes.
func (h *hub) spillPot(players map[int32]*tracked, dim int, pos blockPos, newState uint32) {
	if isDecoratedPot(newState) {
		return
	}
	key := simPos{dim: dim, blockPos: pos}
	h.ensurePotLoot(key)
	if sh, ok := h.potSherds.get(dim, pos.x, pos.y, pos.z); ok && !sh.empty() {
		h.lastPotPos, h.lastPotSherds = key, sh // held for the drop that follows the removal
	}
	h.potSherds.remove(key) // the faces go with the block; the DROP carries them
	st, ok := h.pots[key]
	if !ok {
		return
	}
	delete(h.pots, key)
	if st.item != 0 && st.count > 0 {
		if it := h.spawnItemIn(players, dim, st.item, st.count,
			float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5); it != nil {
			it.dmg, it.ench, it.name = st.dmg, st.ench, st.name
			h.refreshItemMeta(players, it)
		}
	}
}

// potInsert is the hopper's side of the pot (ContainerSingleItem accepts one
// item at a time from above). Reports whether the item went in.
func (h *hub) potInsert(pos simPos, st invStack) bool {
	h.ensurePotLoot(pos)
	stored := h.pots[pos]
	switch {
	case stored.item == 0:
		one := st
		one.count = 1
		h.pots[pos] = one
	case sameItemComponents(stored, st) && stored.count < stackCap(stored.item):
		stored.count++
		h.pots[pos] = stored
	default:
		return false
	}
	return true
}

// potExtract is the hopper underneath: one item out of the pot at a time.
func (h *hub) potExtract(pos simPos) (invStack, bool) {
	h.ensurePotLoot(pos)
	stored, ok := h.pots[pos]
	if !ok || stored.item == 0 || stored.count <= 0 {
		return invStack{}, false
	}
	one := stored
	one.count = 1
	if stored.count--; stored.count <= 0 {
		// The entry stays, empty: an emptied pot is a KNOWN-empty pot, so a
		// structure pot is never restocked (the same rule a looted chest
		// follows).
		h.pots[pos] = invStack{}
	} else {
		h.pots[pos] = stored
	}
	return one, true
}

// evUsePot asks the hub to run a right-click on a decorated pot.
type evUsePot struct {
	eid     int32
	x, y, z int
	off     bool // used from the offhand (the packet's InteractionHand)
}

func (evUsePot) isHubEvent() {}

// Sherd decorations. A decorated pot made from four sherds (or bricks) wears
// them on its four sides — back, left, right, front, in the order the recipe
// grid reads them — and carries them on the item when it is picked up again.
// The faces are what the whole block is for, so a pot with none is the plain
// brick one.
//
// The decorations live in their own guarded store rather than beside the pot's
// contents, because the chunk packet's block-entity section is composed on the
// parallel chunk workers; that is the same reason signs, campfires, banners
// and shelves have stores of their own.

// potSherds is one pot's four faces as item ids, in vanilla's order:
// back, left, right, front. A zero face is a plain brick side.
type potSherds [4]int32

// empty reports whether nothing was ever set — a plain pot.
func (p potSherds) empty() bool { return p == potSherds{} }

// names is the four faces as the registry names the block entity speaks —
// names, not ids, because that is what the sherds tag carries and it is the
// one form that does not renumber between versions.
func (p potSherds) names() [4]string {
	var out [4]string
	for i, id := range p {
		if n, ok := itemNameOfID[id]; ok {
			out[i] = "minecraft:" + n
		}
	}
	return out
}

// itemNameOfID inverts the generated item table.
var itemNameOfID = func() map[int32]string {
	m := make(map[int32]string, len(itemByName))
	for name, id := range itemByName {
		m[id] = name
	}
	return m
}()

type potSherdStore struct {
	mu sync.Mutex
	m  map[string]potSherds
}

func newPotSherdStore() *potSherdStore { return &potSherdStore{m: map[string]potSherds{}} }

func (s *potSherdStore) get(dim, x, y, z int) (potSherds, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[simKey(simPos{dim: dim, blockPos: blockPos{x, y, z}})]
	return v, ok
}

func (s *potSherdStore) set(pos simPos, v potSherds) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.empty() {
		delete(s.m, simKey(pos))
		return
	}
	s.m[simKey(pos)] = v
}

func (s *potSherdStore) remove(pos simPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, simKey(pos))
}

// snapshot is what the save file records.
func (s *potSherdStore) snapshot() map[string]potSherds {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]potSherds, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

func (s *potSherdStore) restore(m map[string]potSherds) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]potSherds, len(m))
	for k, v := range m {
		if !v.empty() {
			s.m[k] = v
		}
	}
}

// breaksDecoratedPots is #breaks_decorated_pots.
var breaksDecoratedPots = func() map[int32]bool {
	out := map[int32]bool{itemByName["trident"]: true, itemByName["mace"]: true}
	for name, id := range itemByName {
		for _, kind := range []string{"_sword", "_axe", "_pickaxe", "_shovel", "_hoe"} {
			if strings.HasSuffix(name, kind) {
				out[id] = true
			}
		}
	}
	return out
}()

// potCracksUnder is DecoratedPotBlock.playerWillDestroy's test: a tool from
// #breaks_decorated_pots with nothing from
// #prevents_decorated_pot_shattering (Silk Touch) on it.
func potCracksUnder(held invStack) bool {
	return breaksDecoratedPots[held.item] && held.enchLvl(enchSilkTouch) == 0
}

// dropPotShards is the cracked pot's drop: its four faces in order (back,
// left, right, front), a brick for every plain side.
func (h *hub) dropPotShards(players map[int32]*tracked, dim int, pos blockPos, sh potSherds) {
	if !h.rules.DoTileDrops {
		return
	}
	for _, face := range sh {
		if face == 0 {
			face = itemByName["brick"]
		}
		h.spawnBlockDrop(players, dim, face, 1, pos.x, pos.y, pos.z)
	}
}
