package server

import (
	"encoding/binary"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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

	blastResistCap = 100 // (unused since the ray model — kept for the drop-chance callers)

	// Vanilla's explosion ray cast: a 16x16x16 grid of directions, stepped
	// 0.3 blocks at a time, each step costing a flat 0.225 of the ray's power.
	explodeRays  = 16
	explodeStep  = 0.3
	explodeDrain = 0.22500001
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
	dim     int
	x, y, z float64
	fuse    int
}

// useFlintSteel handles a flint-&-steel click on (x,y,z): prime TNT, or set
// the face-adjacent air cell alight. Returns whether the click was consumed.
func (s *Server) useFlintSteel(p *player, x, y, z, dx, dy, dz int, seq int32) bool {
	target := s.worldFor(p).Block(x, y, z)
	if canLightBlock(target) { // an unlit candle, candle cake or campfire
		s.hub.post(evLightBlock{eid: p.eid, x: x, y: y, z: z, sound: sndFlintSteelUse})
		s.hub.post(evToolWear{eid: p.eid, slot: p.held})
		s.sendBlockChange(p, x, y, z, target, seq)
		return true
	}
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

// useFireCharge is FireChargeItem.useOn: lights a candle/candle cake/campfire
// in place or starts a fire in the empty cell in front, spending the charge.
func (s *Server) useFireCharge(p *player, x, y, z, dx, dy, dz int, seq int32) {
	target := s.worldFor(p).Block(x, y, z)
	if canLightBlock(target) {
		s.hub.post(evLightBlock{eid: p.eid, x: x, y: y, z: z, sound: sndFireChargeUse})
		s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
		s.sendBlockChange(p, x, y, z, target, seq)
		return
	}
	fx, fy, fz := x+dx, y+dy, z+dz
	if s.worldFor(p).Block(fx, fy, fz) != worldgen.Air {
		s.sendBlockChange(p, fx, fy, fz, s.worldFor(p).Block(fx, fy, fz), seq)
		return
	}
	s.putBlock(p, fx, fy, fz, fireDefault, true, seq)
	s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
}

type evPrimeTNT struct{ x, y, z int }

func (evPrimeTNT) isHubEvent() {}

// primeTNT swaps a TNT block for the ticking entity.
func (h *hub) primeTNT(players map[int32]*tracked, x, y, z int, fuse int) {
	h.primeTNTIn(players, 0, x, y, z, fuse)
}

// primeTNTIn lights TNT in the dimension it actually stands in. The overworld
// wrapper above is what the redstone and dispenser paths use; a chain reaction
// inside an explosion has to carry the dimension through, or nether TNT would
// go off in the overworld.
func (h *hub) primeTNTIn(players map[int32]*tracked, dim, x, y, z int, fuse int) {
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	eid := h.allocEID()
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(eid))
	cx, cy, cz := float64(x)+0.5, float64(y), float64(z)+0.5
	h.tnt = append(h.tnt, &primedTNT{eid: eid, dim: dim, x: cx, y: cy, z: cz, fuse: fuse})
	h.toNearbyEv(players, dim, cx, cz, entAdd(eid, entityTNT, uuid, cx, cy, cz, 0, 0))
	b := protocol.AppendVarInt(nil, eid) // fuse metadata: the client renders the flash timing
	b = protocol.AppendU8(b, metaIndexTNTFuse)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(fuse))
	h.toNearbyEv(players, dim, cx, cz, metaEv(protocol.AppendU8(b, itemMetaEnd)))
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
			h.toNearbyEv(players, t.dim, t.x, t.z, entGone(t.eid))
			if !h.rules.TNTExplodes {
				continue // gamerule tnt_explodes: the fuse burns out and nothing happens
			}
			h.explodeIn(players, t.dim, t.x, t.y+0.5, t.z, tntRadius, tntMaxDamage)
		} else {
			h.tnt = append(h.tnt, t)
		}
	}
}

// explodeAt is the shared blast: crater (respecting blast resistance,
// chain-priming TNT), boom + particle, and falloff damage with knockback for
// players and mobs. Creepers and TNT both detonate through here.
func (h *hub) explodeAt(players map[int32]*tracked, cx, cy, cz float64, radius, maxDamage int) {
	h.explodeIn(players, 0, cx, cy, cz, radius, maxDamage)
}

// explodeIn is the explosion, in the dimension it actually happened in.
//
// The crater is vanilla's, not a sphere: 1352 rays leave the centre, each with
// its own randomised power, and each is worn down by what it passes through —
// (blast resistance + 0.3) x 0.3 per block plus a flat 0.225 per step. That is
// why a real explosion is ragged, why it stops dead at obsidian and eats
// through dirt, and why sand and gravel shield what is behind them. A sphere
// with a resistance cap could not express any of that: every block inside the
// radius went, every block outside survived, and a wall of obsidian protected
// nothing beyond its own cell.
func (h *hub) explodeIn(players map[int32]*tracked, dim int, cx, cy, cz float64, radius, maxDamage int) {
	h.explodeTyped(players, dim, cx, cy, cz, radius, maxDamage, dtExplosion, deathCause{key: causeExplosion})
}

// explodeTyped is explodeIn with the damage type and death message spelled out
// — a bed detonating in the Nether is bad_respawn_point, not a generic blast,
// and the two carry different protection maths and different death messages.
func (h *hub) explodeTyped(players map[int32]*tracked, dim int, cx, cy, cz float64,
	radius, maxDamage int, dt dmgType, cause deathCause) {
	h.playSoundDim(players, dim, "minecraft:entity.generic.explode", sndBlock, cx, cy, cz, 4, 0.9)
	h.spawnParticles(players, particleExplosionEmitter, cx, cy, cz, 0, 0, 1)

	w := h.worldFor(dim)
	if w != nil && radius > 0 {
		hit := h.blastPositions(w, cx, cy, cz, float64(radius))
		for pos := range hit {
			st := w.At(pos.x, pos.y, pos.z)
			if h.blastSpareRails && (isAnyRail(st) || isAnyRail(w.At(pos.x, pos.y+1, pos.z))) {
				continue // a primed TNT cart's blast leaves the track and its bed alone
			}
			if isTNT(st) { // chain reaction: light it, don't vaporize it
				h.primeTNTIn(players, dim, pos.x, pos.y, pos.z, 10+h.rng.Intn(20))
				continue
			}
			h.setBlockAt(players, dim, pos, worldgen.Air)
			h.scheduleIn(dim, pos, 1)
			// vanilla yields 1/radius of the block's drops.
			if h.rng.Intn(max(1, radius)) == 0 && worldgen.HarvestableBy(st, 0) {
				for _, d := range h.rollDrops(st) {
					h.spawnItemIn(players, dim, d.item, d.count,
						float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5)
				}
			}
		}
	}
	h.explodeHurt(players, dim, cx, cy, cz, radius, maxDamage, dt, cause)
}

// blastPositions is the crater ray-cast on its own: the set of blocks an
// explosion of the given power reaches, before anything is done to them.
// A TNT blast destroys them; a wind charge only triggers them.
func (h *hub) blastPositions(w *world.World, cx, cy, cz, radius float64) map[blockPos]bool {
	hit := map[blockPos]bool{}
	{
		for sx := 0; sx < explodeRays; sx++ {
			for sy := 0; sy < explodeRays; sy++ {
				for sz := 0; sz < explodeRays; sz++ {
					// Only the shell of the cube: those are the ray directions.
					if sx != 0 && sx != explodeRays-1 && sy != 0 && sy != explodeRays-1 &&
						sz != 0 && sz != explodeRays-1 {
						continue
					}
					dx := float64(sx)/(explodeRays-1)*2 - 1
					dy := float64(sy)/(explodeRays-1)*2 - 1
					dz := float64(sz)/(explodeRays-1)*2 - 1
					l := math.Sqrt(dx*dx + dy*dy + dz*dz)
					if l == 0 {
						continue
					}
					dx, dy, dz = dx/l*explodeStep, dy/l*explodeStep, dz/l*explodeStep
					power := radius * (0.7 + h.rng.Float64()*0.6)
					px, py, pz := cx, cy, cz
					for power > 0 {
						pos := blockPos{int(math.Floor(px)), int(math.Floor(py)), int(math.Floor(pz))}
						st := w.At(pos.x, pos.y, pos.z)
						if st != worldgen.Air {
							power -= (float64(worldgen.Resistance(st)) + 0.3) * 0.3
						}
						if power > 0 && st != worldgen.Air && st != worldgen.Bedrock &&
							!worldgen.IsWater(st) && !worldgen.IsLava(st) {
							hit[pos] = true
						}
						px, py, pz = px+dx, py+dy, pz+dz
						power -= explodeDrain
					}
				}
			}
		}
	}
	return hit
}

// explodeHurt is the blast's second half: the TNT carts it lights and the
// falloff damage + shove on everything within reach.
func (h *hub) explodeHurt(players map[int32]*tracked, dim int, cx, cy, cz float64,
	radius, maxDamage int, dt dmgType, cause deathCause) {
	rangeF := float64(radius) + 2
	if radius <= 0 {
		rangeF = blastRange // no crater, full hurt (mobGriefing off)
	}
	for _, v := range h.vehicles { // a blast lights the TNT carts it reaches
		if v.dim == dim && v.etype == entityTntMinecart && v.fuse < 0 && dist3(v.x, v.y, v.z, cx, cy, cz) < rangeF {
			h.primeCart(players, v, h.rng.Intn(20)+h.rng.Intn(20))
		}
	}
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		d := dist3(t.x, t.y, t.z, cx, cy, cz)
		if d >= rangeF {
			continue
		}
		dmg := float32(maxDamage) * float32(1-d/rangeF)
		h.hurtFrom(players, t, dmg, dt, cause, from(cx, cz))
		// Blast Protection braces you against the shove as well as the burn.
		// explosion is tagged no_knockback, which looks like a contradiction
		// and is not: that tag suppresses the ordinary shove a hit gives, and
		// a blast then applies its own, scaled by distance. Both would be
		// double-counting.
		h.knockbackScaled(t, cx, cz, t.explosionKnockScale())
	}
	for _, om := range h.mobs {
		if om.dim != dim {
			continue
		}
		d := dist3(om.x, om.y, om.z, cx, cy, cz)
		if d >= rangeF || om.dying > 0 {
			continue
		}
		om.hurtKind(float64(maxDamage)*(1-d/rangeF), dt)
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
	if !h.rules.DoFireTick {
		return // gamerule doFireTick=false: fire neither spreads nor burns out
	}

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
		// Direct call, NOT h.post: this runs on the hub goroutine, and the hub
		// is the only consumer of h.events — a self-post with a full queue
		// blocks forever and takes the whole server with it (see postFromHub).
		h.primeTNT(players, pos.x, pos.y, pos.z, tntFuseTicks)
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
	t.refreshEnchantAttrs()                            // a helmet swapped this tick still counts
	if secs = t.burnSeconds(secs); secs > t.fireSecs { // Fire Protection: −15%/level
		t.fireSecs = secs
	}
	h.broadcastPlayerFlags(players, t)
}

// tickBurning runs at 1 Hz inside the survival step: afterburn damage, and
// water/rain extinguishing.
func (h *hub) tickBurning(players map[int32]*tracked, t *tracked) {
	if t.fireSecs <= 0 {
		return
	}
	if t.hasEffect(effFireRes) > 0 {
		t.fireSecs = 0 // fire resistance snuffs the burn outright
		h.broadcastPlayerFlags(players, t)
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
		h.hurtBy(players, t, fireDamagePerSec, dtOnFire, deathCause{key: causeFire}) // the afterburn bypasses armour
	}
	if t.fireSecs <= 0 && !t.dead {
		h.broadcastPlayerFlags(players, t)
	}
}
