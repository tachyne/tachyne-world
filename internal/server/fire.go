package server

import (
	"encoding/binary"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"tachyne/internal/worldgen"
)

// Fire + TNT. Flint & steel lights fire blocks (which hurt, set players
// burning, and burn out on their own — no spread yet) or primes TNT. Burning
// is a real status: seconds of afterburn from lava/fire that tick damage and
// render the flame overlay, put out by water or rain. Primed TNT is an entity
// with a live fuse; its blast shares the creeper's crater code, respects
// blast resistance, and chain-primes other TNT it uncovers.

const (
	fireDamagePerSec = 1  // standing in a fire block (vanilla)
	fireContactSecs  = 8  // afterburn from touching fire
	lavaFireSecs     = 15 // afterburn from lava (vanilla)

	tntFuseTicks     = 80 // 4 s (vanilla)
	tntRadius        = 4  // TNT is power 4 (creeper 3)
	tntMaxDamage     = 40
	metaIndexTNTFuse = 8 // primed-TNT metadata: fuse ticks (VarInt)

	blastResistCap = 100 // blocks at/above this blast resistance survive (obsidian 1200)
)

var (
	fireStateMin = worldgen.BlockBase("fire") // minecraft:fire state range (1.21.5)
	fireStateMax = worldgen.BlockBase("fire") + 511
	fireDefault  = worldgen.BlockBase("fire") + 31
	soulFire     = worldgen.BlockBase("soul_fire")
	tntStateMin  = worldgen.BlockBase("tnt")
	tntStateMax  = worldgen.BlockBase("tnt") + 1
)

var (
	itemFlintSteel = itemByName["flint_and_steel"]
	itemTNTBlock   = itemByName["tnt"]
	entityTNT      = entityID("tnt") // minecraft:entity_type "tnt" (1.21.5)
)

func isFire(state uint32) bool {
	return (state >= fireStateMin && state <= fireStateMax) || state == soulFire
}

func isTNT(state uint32) bool { return state >= tntStateMin && state <= tntStateMax }

// primedTNT is a lit charge counting down to the bang.
type primedTNT struct {
	eid     int32
	x, y, z float64
	fuse    int
}

// useFlintSteel handles a flint-&-steel click on (x,y,z): prime TNT, or set
// the face-adjacent air cell alight. Returns whether the click was consumed.
func (s *Server) useFlintSteel(p *player, x, y, z, dx, dy, dz int, seq int32) bool {
	target := s.worldFor(p).Block(x, y, z)
	if isTNT(target) {
		s.hub.post(evPrimeTNT{x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, target, seq)
		return true
	}
	fx, fy, fz := x+dx, y+dy, z+dz
	if s.worldFor(p).Block(x, y, z) == worldgen.Obsidian && s.lightPortal(p, fx, fy, fz) {
		s.hub.post(evToolWear{eid: p.eid, slot: p.held})
		s.sendBlockChange(p, x, y, z, s.worldFor(p).Block(x, y, z), seq)
		return true
	}
	if s.worldFor(p).Block(fx, fy, fz) != worldgen.Air {
		s.sendBlockChange(p, fx, fy, fz, s.worldFor(p).Block(fx, fy, fz), seq)
		return true
	}
	s.putBlock(p, fx, fy, fz, fireDefault, true, seq)
	s.hub.post(evToolWear{eid: p.eid, slot: p.held})
	return true
}

type evPrimeTNT struct{ x, y, z int }

func (evPrimeTNT) isHubEvent() {}

// primeTNT swaps a TNT block for the ticking entity.
func (h *hub) primeTNT(players map[int32]*tracked, x, y, z int, fuse int) {
	h.setBlock(players, blockPos{x, y, z}, worldgen.Air)
	eid := h.allocEID()
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(eid))
	cx, cy, cz := float64(x)+0.5, float64(y), float64(z)+0.5
	h.tnt = append(h.tnt, &primedTNT{eid: eid, x: cx, y: cy, z: cz, fuse: fuse})
	h.toNearbyEv(players, 0, cx, cz, entAdd(eid, entityTNT, uuid, cx, cy, cz, 0, 0))
	b := protocol.AppendVarInt(nil, eid) // fuse metadata: the client renders the flash timing
	b = protocol.AppendU8(b, metaIndexTNTFuse)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(fuse))
	h.toNearbyEv(players, 0, cx, cz, metaEv(protocol.AppendU8(b, itemMetaEnd)))
	h.playSound(players, "minecraft:entity.tnt.primed", sndBlock, cx, cy, cz, 1, 1)
}

// updateTNT ticks the fuses (every tick).
func (h *hub) updateTNT(players map[int32]*tracked) {
	if len(h.tnt) == 0 {
		return
	}
	// Detonations chain-prime more TNT (primeTNT appends to h.tnt), so swap in
	// a FRESH slice before iterating — rebuilding in place would alias the
	// backing array and silently drop the newly-primed charges.
	current := h.tnt
	h.tnt = nil
	for _, t := range current {
		if t.fuse--; t.fuse <= 0 {
			h.toNearbyEv(players, 0, t.x, t.z, entGone(t.eid))
			h.explodeAt(players, t.x, t.y+0.5, t.z, tntRadius, tntMaxDamage)
		} else {
			h.tnt = append(h.tnt, t)
		}
	}
}

// explodeAt is the shared blast: crater (respecting blast resistance,
// chain-priming TNT), boom + particle, and falloff damage with knockback for
// players and mobs. Creepers and TNT both detonate through here.
func (h *hub) explodeAt(players map[int32]*tracked, cx, cy, cz float64, radius, maxDamage int) {
	h.playSound(players, "minecraft:entity.generic.explode", sndBlock, cx, cy, cz, 4, 0.9)
	h.spawnParticles(players, particleExplosionEmitter, cx, cy, cz, 0, 0, 1)

	bx, by, bz := int(math.Floor(cx)), int(math.Floor(cy)), int(math.Floor(cz))
	for dx := -radius; dx <= radius; dx++ {
		for dy := -radius; dy <= radius; dy++ {
			for dz := -radius; dz <= radius; dz++ {
				if dx*dx+dy*dy+dz*dz > radius*radius {
					continue
				}
				x, y, z := bx+dx, by+dy, bz+dz
				state := h.world.At(x, y, z)
				if state == worldgen.Air || state == worldgen.Bedrock ||
					worldgen.IsWater(state) || worldgen.IsLava(state) ||
					worldgen.Resistance(state) >= blastResistCap {
					continue
				}
				if isTNT(state) { // chain reaction: light it, don't vaporize it
					h.primeTNT(players, x, y, z, 10+h.rng.Intn(20))
					continue
				}
				h.world.SetBlock(x, y, z, worldgen.Air)
				h.broadcastBlock(players, x, y, z, worldgen.Air)
				h.spillContainer(players, x, y, z, worldgen.Air)
				h.scheduleAround(blockPos{x, y, z}, 1)
				if h.rng.Intn(100) < blastDropChance && worldgen.HarvestableBy(state, 0) {
					for _, d := range h.rollDrops(state) {
						h.spawnBlockDrop(players, d.item, d.count, x, y, z)
					}
				}
			}
		}
	}

	rangeF := float64(radius) + 2
	if radius <= 0 {
		rangeF = blastRange // no crater, full hurt (mobGriefing off)
	}
	for _, t := range players {
		d := dist3(t.x, t.y, t.z, cx, cy, cz)
		if d >= rangeF {
			continue
		}
		dmg := float32(maxDamage) * float32(1-d/rangeF)
		h.damage(players, t, t.armorReduce(dmg))
		h.wearArmor(players, t, dmg)
		h.knockback(t, cx, cz)
	}
	for _, om := range h.mobs {
		d := dist3(om.x, om.y, om.z, cx, cy, cz)
		if d >= rangeF || om.dying > 0 {
			continue
		}
		om.hurt(float64(maxDamage) * (1 - d/rangeF)) // explosions respect armor
		if om.health <= 0 {
			h.killMob(players, om)
		}
	}
	h.bus.publish("explosion", map[string]any{"x": cx, "y": cy, "z": cz})
}

// updateFire is the fire block's scheduled step — a reimplementation of the
// vanilla 1.21.5 FireBlock.tick (formulas transcribed, not copied). Fire
// ages (side-mapped, see hub.fireAge), consumes flammable neighbours, and
// spreads to nearby air whose neighbours are flammable; rain and old age put
// it out. Overworld-only, like the rest of the block sim. Block-eating
// (burnout + spread) is gated on the mobGriefing gamerule; the fire itself
// still ages and dies without it, so /gamerule mobGriefing false keeps a lit
// fire as a pure hazard that never eats a build.
func (h *hub) updateFire(players map[int32]*tracked, pos blockPos) {
	// Reschedule next tick (vanilla getFireTickDelay: 30 + rand(10)).
	h.schedule(pos, uint64(30+h.rng.Intn(10)))

	below := h.world.Block(pos.x, pos.y-1, pos.z)
	infiniburn := below == worldgen.Netherrack // eternal fire on netherrack
	n := h.fireAge[pos]

	// Rain douse (scales with age); doesn't happen on infiniburn.
	if !infiniburn && h.raining && h.fireNearRain(pos) &&
		h.rng.Float32() < 0.2+float32(n)*0.03 {
		h.removeFire(players, pos, true)
		return
	}

	// Age up: min(15, n + rand(3)/2) — increases by 0 or 1.
	if n2 := min(15, n+h.rng.Intn(3)/2); n2 != n {
		n = n2
		h.fireAge[pos] = n
	}

	if !infiniburn {
		if !h.validFireLocation(pos) { // nothing burnable adjacent
			belowSturdy := worldgen.IsSolidFull(below)
			if !belowSturdy || n > 3 {
				h.removeFire(players, pos, false)
			}
			return
		}
		if n == 15 && h.rng.Intn(4) == 0 && !isFlammable(below) {
			h.removeFire(players, pos, false)
			return
		}
	}

	// The wildfire half — eats blocks — is mobGriefing-gated.
	if !h.rules.MobGriefing {
		return
	}

	// Consume flammable neighbours (the six faces have different resilience).
	h.checkBurnOut(players, blockPos{pos.x + 1, pos.y, pos.z}, 300, n)
	h.checkBurnOut(players, blockPos{pos.x - 1, pos.y, pos.z}, 300, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y - 1, pos.z}, 250, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y + 1, pos.z}, 250, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y, pos.z - 1}, 300, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y, pos.z + 1}, 300, n)

	// Spread to air in a 3×3 column from one below to four above.
	diff := int(h.rules.Difficulty)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			for dy := -1; dy <= 4; dy++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				bound := 100
				if dy > 1 {
					bound += (dy - 1) * 100
				}
				np := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
				ig := h.igniteOddsAt(np)
				if ig <= 0 {
					continue
				}
				chance := (ig + 40 + diff*7) / (n + 30)
				if chance <= 0 || h.rng.Intn(bound) > chance ||
					(h.raining && h.fireNearRain(np)) {
					continue
				}
				h.igniteFire(players, np, min(15, n+h.rng.Intn(5)/4))
			}
		}
	}
}

// checkBurnOut lets a fire consume the flammable block at pos: with odds set by
// the block's burn value it either turns into a fresh fire or (more often) to
// air. resilience is the vanilla denominator (250 vertical / 300 horizontal;
// lower = catches easier). Priming any TNT it eats.
func (h *hub) checkBurnOut(players map[int32]*tracked, pos blockPos, resilience, srcAge int) {
	if !h.inWorldY(pos.y) {
		return
	}
	state := h.world.Block(pos.x, pos.y, pos.z)
	_, burn := worldgen.Flammability(state)
	if h.rng.Intn(resilience) >= int(burn) {
		return
	}
	if isTNT(state) {
		h.setBlock(players, pos, worldgen.Air)
		h.post(evPrimeTNT{x: pos.x, y: pos.y, z: pos.z})
		return
	}
	if h.rng.Intn(srcAge+10) < 5 && !(h.raining && h.fireNearRain(pos)) {
		h.igniteFire(players, pos, min(srcAge+h.rng.Intn(5)/4, 15))
	} else {
		h.setBlock(players, pos, worldgen.Air)
		h.scheduleAround(pos, 1) // sand above falls, fluid flows into the gap
	}
}

// igniteFire places a fire block of the given age and schedules its first tick.
func (h *hub) igniteFire(players map[int32]*tracked, pos blockPos, age int) {
	h.setBlock(players, pos, fireDefault)
	h.fireAge[pos] = age
	h.schedule(pos, uint64(30+h.rng.Intn(10)))
}

// removeFire clears a fire block (and its side-mapped age).
func (h *hub) removeFire(players map[int32]*tracked, pos blockPos, doused bool) {
	h.setBlock(players, pos, worldgen.Air)
	delete(h.fireAge, pos)
	if doused {
		h.playSound(players, "minecraft:block.fire.extinguish", sndBlock,
			float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, 0.5, 1.2)
	}
}

// validFireLocation reports whether any of the six neighbours can catch fire.
func (h *hub) validFireLocation(pos blockPos) bool {
	for _, d := range sixDirs {
		if isFlammable(h.world.Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)) {
			return true
		}
	}
	return false
}

// igniteOddsAt is the ignite weight of the air block at pos: 0 unless it's
// empty, else the max ignite odds among its six neighbours.
func (h *hub) igniteOddsAt(pos blockPos) int {
	if h.world.Block(pos.x, pos.y, pos.z) != worldgen.Air {
		return 0
	}
	best := 0
	for _, d := range sixDirs {
		if ig, _ := worldgen.Flammability(h.world.Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)); int(ig) > best {
			best = int(ig)
		}
	}
	return best
}

// fireNearRain reports whether rain is falling on the fire's column or any of
// its four horizontal neighbours (a nearby downpour still snuffs it).
func (h *hub) fireNearRain(pos blockPos) bool {
	return h.skyExposedColumn(pos.x, pos.z) ||
		h.skyExposedColumn(pos.x-1, pos.z) || h.skyExposedColumn(pos.x+1, pos.z) ||
		h.skyExposedColumn(pos.x, pos.z-1) || h.skyExposedColumn(pos.x, pos.z+1)
}

// isFlammable reports whether a block can catch fire at all (ignite odds > 0).
func isFlammable(state uint32) bool {
	ig, _ := worldgen.Flammability(state)
	return ig > 0
}

// sixDirs are the block's six face neighbours.
var sixDirs = []blockPos{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}

// setBurning flips a player's flame overlay + afterburn clock.
func (h *hub) setBurning(players map[int32]*tracked, t *tracked, secs int) {
	if secs > t.fireSecs {
		t.fireSecs = secs
	}
	h.toNearbyEv(players, 0, t.x, t.z, metaEv(fireMetadata(t.p.eid, true)))
	t.p.trySendEv(metaEv(fireMetadata(t.p.eid, true)))
}

// tickBurning runs at 1 Hz inside the survival step: afterburn damage, and
// water/rain extinguishing.
func (h *hub) tickBurning(players map[int32]*tracked, t *tracked) {
	if t.fireSecs <= 0 {
		return
	}
	if t.hasEffect(effFireRes) > 0 {
		t.fireSecs = 0 // fire resistance snuffs the burn outright
		h.toNearbyEv(players, 0, t.x, t.z, metaEv(fireMetadata(t.p.eid, false)))
		t.p.trySendEv(metaEv(fireMetadata(t.p.eid, false)))
		return
	}
	fx, fz := int(math.Floor(t.x)), int(math.Floor(t.z))
	feet := int(math.Floor(t.y))
	// Still standing in the source: contact damage already applied this second
	// and the clock was refreshed — afterburn only ticks once you're OUT.
	w := h.worldFor(t.dim)
	if worldgen.IsLava(w.At(fx, feet, fz)) || worldgen.IsLava(w.At(fx, feet+1, fz)) ||
		isFire(w.At(fx, feet, fz)) || isFire(w.At(fx, feet+1, fz)) {
		return
	}
	inWater := worldgen.IsWater(w.At(fx, feet, fz)) || worldgen.IsWater(w.At(fx, feet+1, fz))
	rainedOn := t.dim == 0 && h.raining && h.skyExposedAt(fx, feet, fz) // from the player's height — caves and roofs block rain
	if inWater || rainedOn {
		t.fireSecs = 0
	} else {
		t.fireSecs--
		h.damage(players, t, fireDamagePerSec)
	}
	if t.fireSecs <= 0 && !t.dead {
		h.toNearbyEv(players, 0, t.x, t.z, metaEv(fireMetadata(t.p.eid, false)))
		t.p.trySendEv(metaEv(fireMetadata(t.p.eid, false)))
	}
}
