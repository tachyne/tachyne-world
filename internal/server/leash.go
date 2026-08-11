package server

import (
	"encoding/binary"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Leads — tying a mob to you, and to a fence.
//
// Ported from Leashable + LeadItem + LeashFenceKnotEntity. Vanilla's model
// lives on the LEASHED entity: Leashable.LeashData names its holder, which is
// either a player or the invisible leash_knot entity a lead makes when it is
// tied to a fence. That direction is what the attach frame carries, so the
// engine's state is the same shape as vanilla's.
//
// The knot is a real entity with an entity id, not a block property: the
// client draws the rope between two entity ids, so a fence-tied mob needs
// something at the fence to draw to.

const (
	// Leashable.LEASH_TOO_FAR_DIST: past this the lead snaps, drops as an
	// item and the mob is free.
	leashSnapDist = 12.0
	// Leashable.LEASH_ELASTIC_DIST: inside the snap distance but past this,
	// the lead pulls the mob after its holder.
	leashElasticDist = 6.0
	// Leashable.STIFFNESS, the spring constant behind that pull.
	leashStiffness = 0.11
	// A knot sits at the middle of the fence block's top half, which is where
	// vanilla places LeashFenceKnotEntity (offset 0.5/0.5/0.5 with a 0.375
	// vertical bias baked into its hitbox).
	leashKnotYOffset = 0.5
)

// leashKnot is the entity a lead makes when tied to a fence. It has no
// behaviour: it exists to be the far end of a rope and to be cut down.
type leashKnot struct {
	eid  int32
	uuid [16]byte
	pos  blockPos
	dim  int
}

// noLeashSpecies are the mobs vanilla refuses a lead on even though they are
// not hostile — WaterAnimal and its kin say no outright (Turtle and
// AgeableWaterCreature override to false as well). Dolphins and axolotls are
// absent on purpose: both override canBeLeashed back to true.
var noLeashSpecies = map[int]bool{
	entitySquid: true, entityGlowSquid: true, entityCod: true, entitySalmon: true,
	entityTropicalFish: true, entityPufferfish: true, entityTadpole: true,
	entityTurtle: true,
}

// originalHostiles are the hand-tuned hostile species that predate the roster
// table, so speciesOf finds nothing for them. Everything newer carries an
// archetype and is classified by that instead.
var originalHostiles = map[int]bool{
	entityZombie: true, entitySkeleton: true, entityCreeper: true, entitySpider: true,
	entityEnderman: true, entityWitch: true, entitySlime: true, entityHusk: true,
	entityStray: true, entityDrowned: true, entityMagmaCube: true, entityBlaze: true,
	entityZombifiedPiglin: true, entityEnderDragon: true,
}

// isEnemyType is vanilla's `instanceof Enemy`, decided by SPECIES.
//
// The mob's own m.hostile flag is not enough on its own: it is set by the
// spawn path (spawnHostileY, applySpecies) rather than by what the mob is, so
// a zombie that arrived some other way would read as peaceful and take a lead.
func isEnemyType(etype int) bool {
	if speciesOf(etype) != nil {
		return !isRosterPassive(etype) // hostile, ranged, water-hostile, flyer-hostile, static
	}
	return originalHostiles[etype]
}

// canBeLeashed is Mob.canBeLeashed: everything except the hostiles (vanilla
// tests `!(this instanceof Enemy)`) and the water animals that refuse.
func canBeLeashed(m *mob) bool {
	if m == nil || m.dying > 0 || m.hostile || isEnemyType(m.etype) {
		return false
	}
	return !noLeashSpecies[m.etype]
}

// leashHolderPos resolves where the far end of a mob's lead is, and whether it
// still exists at all. A holder that has gone — a player who logged out, a
// knot whose fence was broken — means the leash must be dropped.
func (h *hub) leashHolderPos(players map[int32]*tracked, m *mob) (x, y, z float64, dim int, ok bool) {
	if m.leash == 0 {
		return 0, 0, 0, 0, false
	}
	if k := h.knots[m.leash]; k != nil {
		return float64(k.pos.x) + 0.5, float64(k.pos.y) + leashKnotYOffset,
			float64(k.pos.z) + 0.5, k.dim, true
	}
	if t := players[m.leash]; t != nil {
		return t.x, t.y, t.z, t.dim, true
	}
	return 0, 0, 0, 0, false
}

// setLeash ties a mob to a holder and tells everyone watching. Holder 0 unties.
func (h *hub) setLeash(players map[int32]*tracked, m *mob, holder int32) {
	m.leash = holder
	// The knot's BLOCK is what survives a restart (entity ids do not), so it is
	// recorded here rather than looked up at save time — the two are set
	// together and cannot drift apart.
	if k := h.knots[holder]; k != nil {
		m.leashPos = k.pos
	} else {
		m.leashPos = blockPos{}
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.EntityLink{Leashed: m.eid, Holder: holder})
}

// dropLeash is Leashable.dropLeash: cut the lead, tell the watchers, and pop
// the item back out unless this is the creative/"removeLeash" path.
func (h *hub) dropLeash(players map[int32]*tracked, m *mob, dropItem bool) {
	if m.leash == 0 {
		return
	}
	holder := m.leash
	h.setLeash(players, m, 0)
	if dropItem {
		h.spawnItemIn(players, m.dim, itemLead, 1, m.x, m.y, m.z)
	}
	h.knotIfUnused(players, holder)
}

// knotIfUnused removes a fence knot once nothing is tied to it — vanilla
// discards a LeashFenceKnotEntity the moment its last leash goes.
func (h *hub) knotIfUnused(players map[int32]*tracked, eid int32) {
	k := h.knots[eid]
	if k == nil {
		return
	}
	for _, m := range h.mobs {
		if m.leash == eid {
			return // still holding something
		}
	}
	delete(h.knots, eid)
	h.toDimEv(players, k.dim, entGone(k.eid))
}

// knotAt finds the knot on a fence, or makes one. LeashFenceKnotEntity.getKnot
// then createKnot.
func (h *hub) knotAt(players map[int32]*tracked, dim int, pos blockPos) *leashKnot {
	for _, k := range h.knots {
		if k.dim == dim && k.pos == pos {
			return k
		}
	}
	k := &leashKnot{eid: h.allocEID(), pos: pos, dim: dim}
	binary.BigEndian.PutUint32(k.uuid[12:], uint32(k.eid)) // as every other entity
	h.knots[k.eid] = k
	h.toDimEv(players, dim, entAdd(k.eid, entityLeashKnot, k.uuid,
		float64(pos.x)+0.5, float64(pos.y)+leashKnotYOffset, float64(pos.z)+0.5, 0, 0))
	return k
}

// tryLeash is the lead half of right-clicking a mob (Entity.interact).
//
// Order matters and is vanilla's: untying comes FIRST, so a player holding a
// stack of leads can click a mob they have already leashed to get it back
// rather than tying a second one.
func (h *hub) tryLeash(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.leash != 0 && m.leash == t.p.eid {
		// A player with infinite materials keeps the lead (removeLeash);
		// everyone else gets the item back (dropLeash).
		h.dropLeash(players, m, t.gamemode == gmSurvival)
		h.playSound(players, "minecraft:entity.lead.untied", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	}
	if heldStack(t).item != itemLead || !canBeLeashed(m) {
		return false
	}
	// Vanilla refuses to steal a mob off another PLAYER's lead; one tied to a
	// fence may be taken.
	if _, heldByPlayer := players[m.leash]; heldByPlayer && m.leash != 0 {
		return false
	}
	if m.leash != 0 {
		h.dropLeash(players, m, true) // re-tying returns the old lead
	}
	h.setLeash(players, m, t.p.eid)
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.playSound(players, "minecraft:entity.lead.tied", sndNeutral, m.x, m.y, m.z, 1, 1)
	return true
}

// leashToFence is LeadItem.bindPlayerMobs: clicking a fence with a lead moves
// every mob this player is holding onto a knot there. The lead item is not
// consumed — it is already spent on the mobs being carried.
func (h *hub) leashToFence(players map[int32]*tracked, t *tracked, pos blockPos) bool {
	var carried []*mob
	for _, m := range h.mobs {
		if m.leash == t.p.eid && m.dim == t.dim && canBeLeashed(m) {
			carried = append(carried, m)
		}
	}
	if len(carried) == 0 {
		return false
	}
	k := h.knotAt(players, t.dim, pos)
	for _, m := range carried {
		h.setLeash(players, m, k.eid)
	}
	h.playSound(players, "minecraft:entity.leash_knot.place", sndNeutral,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	return true
}

// cutLeashesAt drops every leash tied to a knot — shears on the knot, or the
// fence under it being broken.
func (h *hub) cutLeashesAt(players map[int32]*tracked, dim int, pos blockPos) {
	for _, k := range h.knots {
		if k.dim != dim || k.pos != pos {
			continue
		}
		for _, m := range h.mobs {
			if m.leash == k.eid {
				h.dropLeash(players, m, true)
			}
		}
		h.knotIfUnused(players, k.eid)
	}
}

// updateLeashes is Leashable.tickLeash for every leashed mob: a holder that is
// gone or in another dimension cuts the lead, too far snaps it, and past the
// elastic distance the mob is pulled after its holder.
func (h *hub) updateLeashes(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.leash == 0 {
			continue
		}
		if m.dying > 0 {
			h.dropLeash(players, m, true)
			continue
		}
		hx, hy, hz, hdim, ok := h.leashHolderPos(players, m)
		if !ok || hdim != m.dim {
			// The holder logged out, died, or walked through a portal. Vanilla
			// drops the lead when the two can no longer see each other.
			h.dropLeash(players, m, true)
			continue
		}
		d := dist3(m.x, m.y, m.z, hx, hy, hz)
		if d > leashSnapDist {
			h.playSound(players, "minecraft:entity.lead.break", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.dropLeash(players, m, true)
			continue
		}
		if d <= leashElasticDist {
			continue // slack: the mob wanders as it likes
		}
		// The spring: pull along the rope, proportional to how far past the
		// slack it has stretched. Vanilla's full model is a damped spring with
		// angular momentum; this is its translational half, which is the part
		// that actually drags a mob along behind you.
		pull := (d - leashElasticDist) * leashStiffness
		if d > 1e-6 {
			m.vx += (hx - m.x) / d * pull
			m.vz += (hz - m.z) / d * pull
		}
	}
}

// leashedTo reports the holder to (re)send to a viewer who has just started
// tracking this mob. A leash is state, not an event: a player walking into
// range must be told about a rope that was tied long before they arrived.
func (h *hub) sendLeashesTo(t *tracked) {
	for _, k := range h.knots {
		if k.dim != t.dim {
			continue
		}
		t.p.trySendEv(entAdd(k.eid, entityLeashKnot, k.uuid,
			float64(k.pos.x)+0.5, float64(k.pos.y)+leashKnotYOffset, float64(k.pos.z)+0.5, 0, 0))
	}
	for _, m := range h.mobs {
		if m.leash != 0 && m.dim == t.dim {
			t.p.trySendEv(attachproto.EntityLink{Leashed: m.eid, Holder: m.leash})
		}
	}
}

// leashDistTo is exported for tests: how far a mob is from its holder.
func (h *hub) leashDistTo(players map[int32]*tracked, m *mob) float64 {
	hx, hy, hz, _, ok := h.leashHolderPos(players, m)
	if !ok {
		return math.Inf(1)
	}
	return dist3(m.x, m.y, m.z, hx, hy, hz)
}

var (
	entityLeashKnot = entityID("leash_knot")
	itemLead        = itemByName["lead"]
)

// fenceRanges is the #minecraft:fences tag — #wooden_fences plus
// nether_brick_fence, read from the datapack rather than guessed from the
// name. Walls and fence gates are deliberately absent: vanilla's LeadItem
// checks BlockTags.FENCES, which excludes both, so you cannot tie a mob to a
// cobblestone wall.
var fenceRanges = func() [][2]uint32 {
	names := []string{
		"oak_fence", "spruce_fence", "birch_fence", "jungle_fence", "acacia_fence",
		"dark_oak_fence", "pale_oak_fence", "mangrove_fence", "cherry_fence",
		"bamboo_fence", "crimson_fence", "warped_fence", "nether_brick_fence",
	}
	out := make([][2]uint32, 0, len(names))
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			out = append(out, [2]uint32{lo, hi})
		}
	}
	return out
}()

// isFence reports whether a block state is in #minecraft:fences.
func isFence(s uint32) bool {
	for _, r := range fenceRanges {
		if s >= r[0] && s <= r[1] {
			return true
		}
	}
	return false
}

// evLeashFence is a player right-clicking a block with a lead in hand.
type evLeashFence struct {
	eid int32
	pos blockPos
}

func (evLeashFence) isHubEvent() {}

// leashSavePos is the knot block to persist for a mob's lead, or nil when it
// is free or held by a player (see savedMob.LeashPos for why).
func leashSavePos(m *mob) *[3]int {
	if m.leash == 0 || m.leashPos == (blockPos{}) {
		return nil
	}
	return &[3]int{m.leashPos.x, m.leashPos.y, m.leashPos.z}
}

// restoreLeash re-ties a loaded mob to its fence knot, making the knot if this
// is the first mob to ask for it. A fence that is no longer there means the
// lead is simply gone.
func (h *hub) restoreLeash(players map[int32]*tracked, m *mob, saved *[3]int) {
	if saved == nil {
		return
	}
	pos := blockPos{saved[0], saved[1], saved[2]}
	if !isFence(h.worldFor(m.dim).At(pos.x, pos.y, pos.z)) {
		return
	}
	m.leash = h.knotAt(players, m.dim, pos).eid
}

// interactKnot is LeashFenceKnotEntity.interact: clicking a knot ties whatever
// you are towing to it, and — if you were towing nothing — takes everything on
// it onto your own lead instead. That second half is how a fence full of
// animals gets collected again, so it is the more useful of the two.
//
// Sneaking suppresses the pick-up, as vanilla's isSecondaryUseActive does.
func (h *hub) interactKnot(players map[int32]*tracked, t *tracked, k *leashKnot, sneak bool) bool {
	tied := false
	for _, m := range h.mobs {
		if m.leash == t.p.eid && canBeLeashed(m) {
			h.setLeash(players, m, k.eid)
			tied = true
		}
	}
	if !tied && !sneak {
		for _, m := range h.mobs {
			if m.leash == k.eid && canBeLeashed(m) {
				h.setLeash(players, m, t.p.eid)
				tied = true
			}
		}
		h.knotIfUnused(players, k.eid)
	}
	if tied {
		h.playSound(players, "minecraft:entity.lead.tied", sndNeutral,
			float64(k.pos.x)+0.5, float64(k.pos.y)+0.5, float64(k.pos.z)+0.5, 1, 1)
	}
	return tied
}
