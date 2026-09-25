package server

import (
	"encoding/binary"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
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
	metaIndexTNTFuse = 8  // primed-TNT metadata: fuse ticks (VarInt)

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

// fireContactDamage is BaseFireBlock's fireDamage for whichever of the two
// cells is burning: 2 for soul fire, 1 for fire (0 when neither is).
func fireContactDamage(a, b uint32) float64 {
	switch {
	case a == soulFire || b == soulFire:
		return 2
	case isFire(a) || isFire(b):
		return fireDamagePerSec
	}
	return 0
}

// soulFireBase is #soul_fire_base_blocks: fire lit over it is soul fire,
// and soul fire lasts only while it stays (BaseFireBlock.getState,
// SoulFireBlock.canSurvive).
func soulFireBase(s uint32) bool { return s == worldgen.SoulSand || s == soulSoilBase }

// fireStateOver is BaseFireBlock.getState for a fire lit above `below`.
func fireStateOver(below uint32) uint32 {
	if soulFireBase(below) {
		return soulFire
	}
	return fireDefault
}

func isFire(state uint32) bool {
	return (state >= fireStateMin && state <= fireStateMax) || state == soulFire
}

func isTNT(state uint32) bool { return state >= tntStateMin && state <= tntStateMax }

// primedTNT is a lit charge counting down to the bang.
type primedTNT struct {
	eid        int32
	dim        int
	x, y, z    float64
	vx, vy, vz float64 // PrimedTnt's own motion: the hop when lit, gravity, what blasts push it
	onGround   bool
	fuse       int
	owner      int32 // who lit it (PrimedTnt.owner): a player or a mob, 0 for none
}

// useFlintSteel handles a flint-&-steel click on (x,y,z): prime TNT, or set
// the face-adjacent air cell alight. Returns whether the click was consumed.
func (s *Server) useFlintSteel(p *player, off bool, x, y, z, dx, dy, dz int, seq int32) bool {
	target := s.worldFor(p).Block(x, y, z)
	if canLightBlock(target) { // an unlit candle, candle cake or campfire
		s.hub.post(evLightBlock{eid: p.eid, x: x, y: y, z: z, sound: sndFlintSteelUse})
		s.hub.post(evToolWear{eid: p.eid, slot: int(p.handSlot(off))})
		s.sendBlockChange(p, x, y, z, target, seq)
		return true
	}
	if isTNT(target) { // TntBlock.useItemOn: the lighter wears a point once the TNT is lit (the hub decides)
		s.hub.post(evPrimeTNT{dim: p.dim, x: x, y: y, z: z, by: p.eid, item: itemFlintSteel, slot: p.handSlot(off)})
		s.sendBlockChange(p, x, y, z, target, seq)
		return true
	}
	fx, fy, fz := x+dx, y+dy, z+dz
	if s.worldFor(p).Block(x, y, z) == worldgen.Obsidian && s.lightPortal(p, fx, fy, fz) {
		s.hub.post(evToolWear{eid: p.eid, slot: int(p.handSlot(off))})
		s.sendBlockChange(p, x, y, z, s.worldFor(p).Block(x, y, z), seq)
		return true
	}
	if s.worldFor(p).Block(fx, fy, fz) != worldgen.Air {
		s.sendBlockChange(p, fx, fy, fz, s.worldFor(p).Block(fx, fy, fz), seq)
		return true
	}
	s.putBlock(p, fx, fy, fz, fireStateOver(s.worldFor(p).At(fx, fy-1, fz)), true, seq)
	s.hub.post(evToolWear{eid: p.eid, slot: int(p.handSlot(off))})
	return true
}

// useFireCharge is FireChargeItem.useOn: lights a candle/candle cake/campfire
// in place or starts a fire in the empty cell in front, spending the charge.
func (s *Server) useFireCharge(p *player, off bool, x, y, z, dx, dy, dz int, seq int32) {
	target := s.worldFor(p).Block(x, y, z)
	if isTNT(target) { // TntBlock.useItemOn: a fire charge lights it too, and is spent once it has
		s.hub.post(evPrimeTNT{dim: p.dim, x: x, y: y, z: z, by: p.eid, item: itemFireCharge, slot: p.handSlot(off)})
		s.sendBlockChange(p, x, y, z, target, seq)
		return
	}
	if canLightBlock(target) {
		s.hub.post(evLightBlock{eid: p.eid, x: x, y: y, z: z, sound: sndFireChargeUse})
		s.hub.post(evConsume{eid: p.eid, slot: p.handSlot(off)})
		s.sendBlockChange(p, x, y, z, target, seq)
		return
	}
	fx, fy, fz := x+dx, y+dy, z+dz
	if s.worldFor(p).Block(fx, fy, fz) != worldgen.Air {
		s.sendBlockChange(p, fx, fy, fz, s.worldFor(p).Block(fx, fy, fz), seq)
		return
	}
	s.putBlock(p, fx, fy, fz, fireStateOver(s.worldFor(p).At(fx, fy-1, fz)), true, seq)
	s.hub.post(evConsume{eid: p.eid, slot: p.handSlot(off)})
}

type evPrimeTNT struct {
	dim, x, y, z int
	by           int32 // the player who lit it
	item         int32 // the lighter: flint and steel or a fire charge (0: none)
	slot         int32 // the hand slot it is in
}

func (evPrimeTNT) isHubEvent() {}

// onPrimeTNT is TntBlock.useItemOn with a lighter: once the TNT is primed the
// flint and steel wears a point (a fire charge is spent) and ITEM_USED
// counts; with tnt_explodes off nothing is lit or spent, and the player is
// told why.
func (h *hub) onPrimeTNT(players map[int32]*tracked, e evPrimeTNT) {
	t := players[e.by]
	if h.primeTNTBy(players, e.dim, e.x, e.y, e.z, tntFuseTicks, e.by) == nil {
		if t != nil && e.item != 0 && !h.rules.TNTExplodes {
			t.p.trySendEv(actionBarEv("TNT explosions are disabled")) // block.minecraft.tnt.disabled
		}
		return
	}
	if t == nil || e.item == 0 {
		return
	}
	h.incStat(t, attachproto.StatUsed, e.item, 1)
	if e.item == itemFlintSteel {
		h.applyToolWear(t, int(e.slot), 1)
		return
	}
	if sl := t.handStack(int(e.slot)); sl != nil && sl.count > 0 && t.gamemode != gmCreative {
		if sl.count--; sl.count == 0 {
			sl.item = 0
		}
		h.sendHandSlot(t, int(e.slot))
	}
}

// primeTNT swaps a TNT block for the ticking entity, in the dimension the
// block simulation is running in (redstone, fire, dispensers). It lit the
// overworld's coordinates until 2026-09-24, so Nether TNT went off at home.
func (h *hub) primeTNT(players map[int32]*tracked, x, y, z int, fuse int) {
	h.primeTNTIn(players, h.rsDim, x, y, z, fuse)
}

// primeTNTIn lights TNT in the dimension it actually stands in. A chain
// reaction inside an explosion has to carry the dimension through.
func (h *hub) primeTNTIn(players map[int32]*tracked, dim, x, y, z int, fuse int) *primedTNT {
	return h.primeTNTBy(players, dim, x, y, z, fuse, 0)
}

// primeTNTBy is TntBlock.prime with an igniter and the block's removal: the
// player or mob it names is the owner the blast is blamed on (and credited
// with what it kills). With tnt_explodes off prime fails and the block stays
// (nil).
func (h *hub) primeTNTBy(players map[int32]*tracked, dim, x, y, z int, fuse int, owner int32) *primedTNT {
	pt := h.tntPrime(players, dim, x, y, z, fuse, owner)
	if pt != nil {
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	}
	return pt
}

// tntPrime is TntBlock.prime alone: the lit charge, its sound and the
// PRIME_FUSE game event, leaving the block to the caller (a fire that eats
// TNT has already replaced it). Nothing when tnt_explodes is off.
func (h *hub) tntPrime(players map[int32]*tracked, dim, x, y, z int, fuse int, owner int32) *primedTNT {
	if !h.rules.TNTExplodes {
		return nil
	}
	pt := h.spawnPrimedTNT(players, dim, x, y, z, fuse)
	pt.owner = owner
	h.vib(dim, freqPrimeFuse, x, y, z, owner)
	return pt
}

// spawnPrimedTNT adds the lit charge entity centred on a cell, touching no
// block (a dispenser's TNT: the cell ahead may hold anything).
func (h *hub) spawnPrimedTNT(players map[int32]*tracked, dim, x, y, z int, fuse int) *primedTNT {
	eid := h.allocEID()
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(eid))
	cx, cy, cz := float64(x)+0.5, float64(y), float64(z)+0.5
	rot := h.rng.Float64() * 2 * math.Pi // PrimedTnt(): a small hop in a random direction
	pt := &primedTNT{eid: eid, dim: dim, x: cx, y: cy, z: cz,
		vx: -math.Sin(rot) * tntHopH, vy: tntHopV, vz: -math.Cos(rot) * tntHopH, fuse: fuse}
	h.tnt = append(h.tnt, pt)
	h.toNearbyEv(players, dim, cx, cz, entAdd(eid, entityTNT, uuid, cx, cy, cz, 0, 0))
	b := protocol.AppendVarInt(nil, eid) // fuse metadata: the client renders the flash timing
	b = protocol.AppendU8(b, metaIndexTNTFuse)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(fuse))
	h.toNearbyEv(players, dim, cx, cz, metaEv(protocol.AppendU8(b, itemMetaEnd)))
	h.playSoundDim(players, dim, "minecraft:entity.tnt.primed", sndBlock, cx, cy, cz, 1, 1)
	return pt
}

// updateTNT ticks the fuses (every tick).
func (h *hub) updateTNT(players map[int32]*tracked) {
	if len(h.tnt) == 0 {
		return
	}
	// Detonations chain-prime more TNT (primeTNT appends to h.tnt), so swap in
	// a FRESH slice before iterating — rebuilding in place would alias the
	// backing array and silently drop the newly-primed charges.
	// The survivors are re-listed BEFORE anything detonates, so a blast's
	// push reaches every other charge in the air (TNT cannons).
	current := h.tnt
	h.tnt = nil
	var due []*primedTNT
	for _, t := range current {
		h.tntStep(players, t)
		if t.fuse--; t.fuse <= 0 {
			due = append(due, t)
		} else {
			h.tnt = append(h.tnt, t)
		}
	}
	for _, t := range due {
		h.entityGone(players, t.dim, t.eid)
		if !h.rules.TNTExplodes {
			continue // gamerule tnt_explodes: the fuse burns out and nothing happens
		}
		// DamageSources.explosion(tnt, owner): lit by someone, the blast is
		// theirs — its damage type, its death message and its kills.
		by, cause := h.blastCauseOf(players, t.owner)
		h.explodeBy(players, t.dim, t.x, t.y+tntBlastYOffset, t.z, tntRadius, tntRadius, blastTNT, by,
			withHiveRelease(), withBlastDirect(entityTNT), cause)
	}
}

// blastCauseOf names a blast's indirect source for its death message and
// carries it into the blast, if that entity is still in the world (vanilla
// resolves the owner when it goes off: a player who left, or a creeper that
// has already blown up, is no one).
func (h *hub) blastCauseOf(players map[int32]*tracked, eid int32) (string, blastOpt) {
	if eid != 0 {
		if t := players[eid]; t != nil {
			return t.p.name, withBlastCause(eid, false)
		}
		if m := h.mobs[eid]; m != nil && m.dying == 0 {
			return mobDisplayName(m.etype), withBlastCause(eid, true)
		}
	}
	return "", func(*blastCfg) {}
}

// blastKind is what set the blast off, for the drop-decay rules (Level.
// ExplosionInteraction): a TNT blast drops everything by default, a mob's
// or a block's one in `radius`.
type blastKind int

const (
	blastTNT   blastKind = iota // TNT, a TNT cart
	blastMob                    // a creeper, the wither, a fireball
	blastBlock                  // a bed or anchor, an end crystal
)

// dropDecay reports whether this kind's blast keeps only 1/radius of the
// drops (the *_explosion_drop_decay rules).
func (h *hub) dropDecay(kind blastKind) bool {
	switch kind {
	case blastTNT:
		return h.rules.TNTDropDecay
	case blastMob:
		return h.rules.MobDropDecay
	}
	return h.rules.BlockDropDecay
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
//
// radius is the crater's power (0: no crater — mobGriefing off, or a purely
// cosmetic blast); power is the explosion's vanilla radius for what it does
// to entities (TNT 4, a creeper 3, a bed 5; 0 hurts nothing).
func (h *hub) explodeIn(players map[int32]*tracked, dim int, cx, cy, cz float64, radius int, power float64, kind blastKind, opts ...blastOpt) {
	h.explodeTyped(players, dim, cx, cy, cz, radius, power, kind, dtExplosion, deathCause{}, opts...)
}

// explodeBy is an explosion something is to blame for. Vanilla splits these:
// DamageSources.explosion hands back player_explosion when there is a causing
// entity and plain explosion when there is not, which is the difference
// between "was blown up by Creeper" and "blew up". The attributed type also
// always scales with the difficulty.
func (h *hub) explodeBy(players map[int32]*tracked, dim int, cx, cy, cz float64,
	radius int, power float64, kind blastKind, by string, opts ...blastOpt) {
	if by == "" {
		h.explodeTyped(players, dim, cx, cy, cz, radius, power, kind, dtExplosion, deathCause{}, opts...)
		return
	}
	h.explodeTyped(players, dim, cx, cy, cz, radius, power, kind,
		dtPlayerExplosion, deathCause{by: by}, opts...)
}

// explodeTyped is explodeIn with the damage type and death message spelled out
// — a bed detonating in the Nether is bad_respawn_point, not a generic blast,
// and the two carry different protection maths and different death messages.
func (h *hub) explodeTyped(players map[int32]*tracked, dim int, cx, cy, cz float64,
	radius int, power float64, kind blastKind, dt dmgType, cause deathCause, opts ...blastOpt) {
	cfg := blastCfg{resistCap: math.Inf(1)}
	for _, o := range opts {
		o(&cfg)
	}
	h.playSoundDim(players, dim, "minecraft:entity.generic.explode", sndBlock, cx, cy, cz, 4, 0.9)
	h.vibAt(dim, freqExplode, cx, cy, cz, 0)
	h.spawnParticles(players, dim, particleExplosionEmitter, cx, cy, cz, 0, 0, 1)

	// ServerLevel.explode: a MOB interaction (a creeper, a ghast's fireball,
	// the wither and its skulls) keeps every block when mobGriefing is off;
	// the blast still hurts what stands in it.
	if kind == blastMob && !h.rules.MobGriefing {
		radius = 0
	}
	w := h.worldFor(dim)
	var cleared, hives []blockPos
	h.explosionHurtsItems(players, dim, cx, cy, cz, power)
	if w != nil && radius > 0 {
		hit := h.blastPositionsCapped(w, cx, cy, cz, float64(radius), cfg.resistCap)
		for pos := range hit {
			st := w.At(pos.x, pos.y, pos.z)
			if h.blastSpareRails && (isAnyRail(st) || isAnyRail(w.At(pos.x, pos.y+1, pos.z))) {
				continue // a primed TNT cart's blast leaves the track and its bed alone
			}
			if isTNT(st) && h.primeTNTBy(players, dim, pos.x, pos.y, pos.z, 10+h.rng.Intn(20), cfg.causer) != nil {
				continue // chain reaction: lit, not vaporized (owned by the blast's cause); with tnt_explodes off it just goes
			}
			h.setBlockAt(players, dim, pos, worldgen.Air)
			h.scheduleIn(dim, pos, 1)
			h.dropExploded(players, dim, pos, st, radius, kind)
			// BlockBehaviour.onExplosionHit: spawnAfterBreak drops the block's
			// experience when a PLAYER set the blast off (TNT they lit or
			// shot), as if they had mined it — ore blown up by a creeper
			// gives none.
			if t := players[cfg.causer]; t != nil && !cfg.causerMob && h.rules.DoTileDrops {
				if xp := xpForBlock(st, h.rng.Intn); xp > 0 {
					h.spawnXPOrbIn(players, dim, xp, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5)
				}
			}
			cleared = append(cleared, pos)
			if isBeeHome(st) {
				hives = append(hives, pos)
			}
		}
		if cfg.fire {
			h.lightBlastFires(players, dim, cleared)
		}
	}
	prevSrc := h.blastSrc
	h.blastSrc = cfg
	h.explodeHurt(players, dim, cx, cy, cz, power, dt, cause)
	h.blastSrc = prevSrc
	// Vanilla hurts entities before it touches blocks, so bees a hive lets
	// out are not caught in the blast that freed them.
	for _, pos := range hives {
		h.explodedHive(players, dim, pos, cfg.hiveRelease)
		h.angerNearbyBees(players, dim, pos) // BeehiveBlock.onExplosionHit
	}
}

// lightBlastFires is ServerExplosion.createFire: one cell in three of what the
// blast cleared catches, if it is air now and whatever is under it is solid
// enough to hold a fire.
func (h *hub) lightBlastFires(players map[int32]*tracked, dim int, cleared []blockPos) {
	w := h.worldFor(dim)
	for _, pos := range cleared {
		if h.rng.Intn(3) != 0 {
			continue
		}
		if w.At(pos.x, pos.y, pos.z) != worldgen.Air || !isSolidRender(w.At(pos.x, pos.y-1, pos.z)) {
			continue
		}
		// Explosion.createFire: BaseFireBlock.getState — soul fire over soul
		// sand or soul soil, which neither ages nor spreads.
		fire := fireStateOver(w.At(pos.x, pos.y-1, pos.z))
		h.setBlockAt(players, dim, pos, fire)
		if fire == fireDefault {
			h.fireAge[simPos{dim: dim, blockPos: pos}] = 0
			h.inDim(dim, func() { h.armFire(pos) })
		}
	}
}

// blastPositions is the crater ray-cast on its own: the set of blocks an
// explosion of the given power reaches, before anything is done to them.
// A TNT blast destroys them; a wind charge only triggers them.
func (h *hub) blastPositions(w *world.World, cx, cy, cz, radius float64) map[blockPos]bool {
	return h.blastPositionsCapped(w, cx, cy, cz, radius, math.Inf(1))
}

// blastCfg is what a caller may vary about one explosion.
type blastCfg struct {
	// fire lights what the blast cleared. Vanilla sets it for a bed or a
	// respawn anchor detonating where it must not, and for a ghast's fireball
	// when mobGriefing allows — never for TNT, a creeper or an end crystal.
	fire bool
	// resistCap is the most any one block is allowed to resist, which is how
	// a blue wither skull chews through what an ordinary blast cannot:
	// WitherSkull.getBlockExplosionResistance answers min(0.8, resistance)
	// for everything the wither is allowed to destroy at all.
	resistCap float64
	// hiveRelease turns a destroyed hive's bees out instead of losing them
	// (BeehiveBlock.getDrops names the sources: primed TNT, a TNT cart, a
	// creeper, the wither and its skulls).
	hiveRelease bool
	// causer is the blast's indirect source entity (Explosion.
	// getIndirectSourceEntity: whoever lit the TNT, shot the fireball, or the
	// creeper itself), and causerMob says it is a mob rather than a player.
	causer    int32
	causerMob bool
	// direct is the blast's direct source entity type (the TNT, the cart,
	// the crystal, the fireball): the killing blow's direct entity.
	direct int
}

type blastOpt func(*blastCfg)

// withBlastFire leaves fires in the crater (ServerExplosion.createFire).
func withBlastFire() blastOpt {
	return func(c *blastCfg) { c.fire = true }
}

// withHiveRelease marks a blast whose source lets bees out of the hives it
// destroys.
func withHiveRelease() blastOpt {
	return func(c *blastCfg) { c.hiveRelease = true }
}

// withBlastCause names the blast's indirect source: a player or a mob.
func withBlastCause(eid int32, mob bool) blastOpt {
	return func(c *blastCfg) { c.causer, c.causerMob = eid, mob }
}

// withBlastDirect names the entity type that went off.
func withBlastDirect(etype int) blastOpt {
	return func(c *blastCfg) { c.direct = etype }
}

// withResistCap bounds every block's resistance for one explosion.
func withResistCap(cap float64) blastOpt {
	return func(c *blastCfg) { c.resistCap = cap }
}

// blastPositionsCapped is blastPositions with that bound applied.
func (h *hub) blastPositionsCapped(w *world.World, cx, cy, cz, radius, resistCap float64) map[blockPos]bool {
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
							r := float64(worldgen.Resistance(st))
							if r > resistCap && !witherImmuneBlock(st) {
								r = resistCap
							}
							power -= (r + 0.3) * 0.3
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
	h.armFire(pos)
	if !h.canSpreadFireAround(players, pos) {
		// ServerLevel.canSpreadFireAround: out of every player's reach, fire
		// neither spreads nor burns out — it just sits there.
		return
	}

	below := h.rsWorld().Block(pos.x, pos.y-1, pos.z)
	// FireBlock.canSurvive: a sturdy floor or something burnable beside it.
	if !worldgen.IsSolidFull(below) && !h.validFireLocation(pos) {
		h.removeFire(players, pos, false)
		return
	}
	infiniburn := h.fireInfiniburn(below) // eternal fire on the dimension's #infiniburn_*
	n := h.fireAge[h.rsKey(pos)]

	// Rain douse (scales with age); doesn't happen on infiniburn.
	if !infiniburn && h.raining && h.fireNearRain(pos) &&
		h.rng.Float32() < 0.2+float32(n)*0.03 {
		h.removeFire(players, pos, true)
		return
	}

	// Age up: min(15, n + rand(3)/2) — increases by 0 or 1.
	if n2 := min(15, n+h.rng.Intn(3)/2); n2 != n {
		n = n2
		h.fireAge[h.rsKey(pos)] = n
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

	// Consume flammable neighbours (the six faces have different
	// resilience); a biome with INCREASED_FIRE_BURNOUT burns them faster
	// and spreads less.
	burnout := h.increasedFireBurnout(pos)
	extra := 0
	if burnout {
		extra = -50
	}
	h.checkBurnOut(players, blockPos{pos.x + 1, pos.y, pos.z}, 300+extra, n)
	h.checkBurnOut(players, blockPos{pos.x - 1, pos.y, pos.z}, 300+extra, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y - 1, pos.z}, 250+extra, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y + 1, pos.z}, 250+extra, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y, pos.z - 1}, 300+extra, n)
	h.checkBurnOut(players, blockPos{pos.x, pos.y, pos.z + 1}, 300+extra, n)

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
				if burnout {
					chance /= 2
				}
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
	state := h.rsWorld().Block(pos.x, pos.y, pos.z)
	_, burn := worldgen.Flammability(state)
	if h.rng.Intn(resilience) >= int(burn) {
		return
	}
	// The block turns to fire or goes (isRainingAt: rain on the cell itself
	// puts it out), and TNT that burns is primed either way.
	if h.rng.Intn(srcAge+10) < 5 && !h.rainingOn(h.rsDim, pos.x, pos.y, pos.z) {
		h.igniteFire(players, pos, min(srcAge+h.rng.Intn(5)/4, 15))
	} else {
		h.rsSet(players, pos, worldgen.Air)
		h.scheduleAroundIn(h.rsDim, pos, 1) // sand above falls, fluid flows into the gap
	}
	if isTNT(state) {
		// Direct call, NOT h.post: this runs on the hub goroutine, and the hub
		// is the only consumer of h.events — a self-post with a full queue
		// blocks forever and takes the whole server with it (see postFromHub).
		// The cell is already fire or air: prime alone, no removal.
		h.tntPrime(players, h.rsDim, pos.x, pos.y, pos.z, tntFuseTicks, 0)
	}
}

// fireInfiniburn is the dimension type's infiniburn tag: netherrack and
// magma blocks everywhere (#infiniburn_overworld), and bedrock too in the
// End (#infiniburn_end).
func (h *hub) fireInfiniburn(below uint32) bool {
	return below == worldgen.Netherrack || below == magmaBlockState ||
		(h.rsDim == dimEnd && below == worldgen.Bedrock)
}

// fireBurnoutBiomes carry EnvironmentAttributes.INCREASED_FIRE_BURNOUT.
var fireBurnoutBiomes = map[string]bool{
	"minecraft:jungle": true, "minecraft:bamboo_jungle": true, "minecraft:mushroom_fields": true,
	"minecraft:swamp": true, "minecraft:mangrove_swamp": true, "minecraft:dappled_forest": true,
	"minecraft:snowy_slopes": true,
}

// increasedFireBurnout reads the fire's biome for INCREASED_FIRE_BURNOUT.
func (h *hub) increasedFireBurnout(pos blockPos) bool {
	w := h.rsWorld()
	return w != nil && fireBurnoutBiomes[w.BiomeAt3D(pos.x, pos.y, pos.z)]
}

// rainingOn is Level.isRainingAt: raining, nothing that blocks motion (or
// holds a fluid) above the cell, and the biome rains at that height. Only
// the overworld has weather.
func (h *hub) rainingOn(dim, x, y, z int) bool {
	return dim == dimOverworld && h.raining && h.motionBlockingTop(dim, x, z) <= y &&
		worldgen.PrecipitationAt(h.world.BiomeAt(x, z), y) == worldgen.PrecipRain
}

// igniteFire places a fire block of the given age and schedules its first tick.
func (h *hub) igniteFire(players map[int32]*tracked, pos blockPos, age int) {
	if soulFireBase(h.rsWorld().At(pos.x, pos.y-1, pos.z)) {
		h.rsSet(players, pos, soulFire) // soul fire neither ages nor spreads
		return
	}
	h.rsSet(players, pos, fireDefault)
	h.fireAge[h.rsKey(pos)] = age
	h.armFire(pos)
}

// armFire books a fire's next tick (FireBlock.onPlace and tick:
// getFireTickDelay, 30 + rand(10)) and remembers when it is due: the
// simulation queue also reaches a fire for every neighbour change, and
// those only check that it can stay (updateShape).
func (h *hub) armFire(pos blockPos) {
	delay := uint64(30 + h.rng.Intn(10))
	h.fireDue[h.rsKey(pos)] = h.tick.Load() + delay
	h.rsSchedule(pos, delay)
}

// fireUpdate is the simulation queue reaching a fire: its own due tick runs
// FireBlock.tick; a fire with no tick booked (a player's, a command's) books
// one as onPlace does; any other update is updateShape — a fire that can no
// longer survive goes out.
func (h *hub) fireUpdate(players map[int32]*tracked, pos blockPos) {
	key := h.rsKey(pos)
	due, armed := h.fireDue[key]
	if armed && h.tick.Load() >= due {
		delete(h.fireDue, key)
		h.updateFire(players, pos)
		return
	}
	below := h.rsWorld().Block(pos.x, pos.y-1, pos.z)
	if !worldgen.IsSolidFull(below) && !h.validFireLocation(pos) {
		h.removeFire(players, pos, false) // FireBlock.canSurvive fails: updateShape gives air
		delete(h.fireDue, key)
		return
	}
	if !armed {
		h.armFire(pos)
	}
}

// removeFire clears a fire block (and its side-mapped age).
func (h *hub) removeFire(players map[int32]*tracked, pos blockPos, doused bool) {
	h.rsSet(players, pos, worldgen.Air)
	delete(h.fireAge, h.rsKey(pos))
	delete(h.fireDue, h.rsKey(pos))
	if doused {
		h.rsSound(players, "minecraft:block.fire.extinguish", sndBlock,
			float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, 0.5, 1.2)
	}
}

// validFireLocation reports whether any of the six neighbours can catch fire.
func (h *hub) validFireLocation(pos blockPos) bool {
	for _, d := range sixDirs {
		if isFlammable(h.rsWorld().Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)) {
			return true
		}
	}
	return false
}

// igniteOddsAt is the ignite weight of the air block at pos: 0 unless it's
// empty, else the max ignite odds among its six neighbours.
func (h *hub) igniteOddsAt(pos blockPos) int {
	if h.rsWorld().Block(pos.x, pos.y, pos.z) != worldgen.Air {
		return 0
	}
	best := 0
	for _, d := range sixDirs {
		if ig, _ := worldgen.Flammability(h.rsWorld().Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)); int(ig) > best {
			best = int(ig)
		}
	}
	return best
}

// fireNearRain is FireBlock.isNearRain: isRainingAt on the fire's cell or any of
// its four horizontal neighbours (a nearby downpour still snuffs it).
func (h *hub) fireNearRain(pos blockPos) bool {
	d := h.rsDim
	return h.rainingOn(d, pos.x, pos.y, pos.z) ||
		h.rainingOn(d, pos.x-1, pos.y, pos.z) || h.rainingOn(d, pos.x+1, pos.y, pos.z) ||
		h.rainingOn(d, pos.x, pos.y, pos.z-1) || h.rainingOn(d, pos.x, pos.y, pos.z+1)
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
		// Fire Resistance leaves the burn to run its course (the flames still
		// show) and only turns the damage away, as LivingEntity.hurtServer does.
		if t.hasEffect(effFireRes) == 0 {
			h.hurtBy(players, t, fireDamagePerSec, dtOnFire, deathCause{}) // the afterburn bypasses armour
		}
	}
	if t.fireSecs <= 0 && !t.dead {
		h.broadcastPlayerFlags(players, t)
	}
}

// dropExploded is BlockBehaviour.onExplosionHit's drop: the block's loot
// table rolled with an empty tool — the correct-tool rule is a player's,
// not an explosion's, so TNT mines stone into cobblestone and ores into
// their gems — and, for a decaying blast, explosion_decay's one-in-radius
// survival per item. Blocks without a table keep the old roller with the
// decay applied to the block as a whole.
func (h *hub) dropExploded(players map[int32]*tracked, dim int, pos blockPos, st uint32, radius int, kind blastKind) {
	ctx := lootCtx{state: st, rng: h.rng.Intn, randf: h.rng.Float64}
	if h.dropDecay(kind) {
		ctx.explosion = float64(max(1, radius))
	}
	if ds := h.evalBlockLoot(ctx); ds != nil {
		for _, d := range ds {
			h.spawnBlockDrop(players, dim, d.item, d.count, pos.x, pos.y, pos.z)
		}
		return
	}
	if !h.dropDecay(kind) || h.rng.Intn(max(1, radius)) == 0 {
		for _, d := range h.rollDrops(st) {
			h.spawnBlockDrop(players, dim, d.item, d.count, pos.x, pos.y, pos.z)
		}
	}
}

// defaultFireSpreadRadius is vanilla's fire_spread_radius_around_player
// default: 128 blocks.
const defaultFireSpreadRadius = 128

// canSpreadFireAround is ServerLevel.canSpreadFireAround — the gamerule that
// replaced doFireTick in 1.21.9. A radius of -1 means everywhere (the old
// doFireTick=true), 0 means nowhere (the old false), and anything else asks
// whether a player is within that many blocks. Out of reach, a fire neither
// spreads nor burns out, so a far-off forest cannot quietly burn down while
// nobody is there to see it.
func (h *hub) canSpreadFireAround(players map[int32]*tracked, pos blockPos) bool {
	r := h.rules.FireSpreadRadius
	if r < 0 {
		return true
	}
	if r == 0 {
		return false
	}
	r2 := float64(r) * float64(r)
	for _, t := range players {
		if t.dim != h.rsDim {
			continue
		}
		dx, dy, dz := t.x-float64(pos.x), t.y-float64(pos.y), t.z-float64(pos.z)
		if dx*dx+dy*dy+dz*dz <= r2 {
			return true
		}
	}
	return false
}

// isSolidRender is BlockState.isSolidRender: an occluding full cube. Across
// every 26.3 state it is exactly the opaque full cubes except tinted glass,
// which blocks light but is not solid-render.
func isSolidRender(state uint32) bool {
	return fullCube(state) && state != tintedGlass
}

var tintedGlass = worldgen.BlockBase("tinted_glass")
