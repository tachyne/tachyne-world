package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The conduit. A player-built block entity: surround one with a frame of
// prismarine and it grants Conduit Power to swimmers in range, and at a full
// frame it hunts hostile mobs in the water around it.
//
// The Conduit Power EFFECT landed with the missing effects; this is the source
// that makes it reachable without a command.

const (
	conduitMinActive = 16  // MIN_ACTIVE_SIZE: frame blocks before it lights at all
	conduitMinKill   = 42  // MIN_KILL_SIZE: a full frame also attacks
	conduitKillRange = 8.0 // KILL_RANGE
	conduitRangeStep = 16  // effectRange = activeBlocks/7 * 16
	conduitPowerSecs = 13  // EFFECT_DURATION, refreshed while the conduit runs
	conduitAttackDmg = 4   // vanilla's conduit beam damage
)

var conduitState = worldgen.BlockBase("conduit")

// conduitFrameBlocks are the four block types a frame may be built from.
var conduitFrameBlocks = func() map[uint32]bool {
	set := map[uint32]bool{}
	for _, n := range []string{"prismarine", "prismarine_bricks", "sea_lantern", "dark_prismarine"} {
		lo, hi := worldgen.BlockRange(n)
		for s := lo; s <= hi; s++ {
			set[s] = true
		}
	}
	return set
}()

// conduitActiveBlocks counts a conduit's frame, or reports 0 if it cannot run.
//
// Two vanilla conditions, in order: the 3x3x3 around the conduit must be ALL
// water (that is why one dug into a wall goes dark), and then the frame is the
// prismarine sitting on the 5x5x5 shell's edge-centre lines — the shape that
// makes the familiar open cage rather than a solid box.
func (h *hub) conduitActiveBlocks(dim int, pos blockPos) int {
	w := h.worldFor(dim)
	for ox := -1; ox <= 1; ox++ {
		for oy := -1; oy <= 1; oy++ {
			for oz := -1; oz <= 1; oz++ {
				if ox == 0 && oy == 0 && oz == 0 {
					continue // the conduit itself
				}
				if !worldgen.IsWater(w.At(pos.x+ox, pos.y+oy, pos.z+oz)) {
					return 0
				}
			}
		}
	}
	n := 0
	for ox := -2; ox <= 2; ox++ {
		for oy := -2; oy <= 2; oy++ {
			for oz := -2; oz <= 2; oz++ {
				ax, ay, az := abs(ox), abs(oy), abs(oz)
				if ax <= 1 && ay <= 1 && az <= 1 {
					continue
				}
				// The frame positions: on one of the three axis-aligned rings.
				onRing := (ox == 0 && (ay == 2 || az == 2)) ||
					(oy == 0 && (ax == 2 || az == 2)) ||
					(oz == 0 && (ax == 2 || ay == 2))
				if !onRing {
					continue
				}
				if conduitFrameBlocks[w.At(pos.x+ox, pos.y+oy, pos.z+oz)] {
					n++
				}
			}
		}
	}
	if n < conduitMinActive {
		return 0
	}
	return n
}

// noteConduitBlock keeps the conduit registry in step with the world. Conduits
// are ONLY ever player-built — no structure generates one — so remembering
// where they were placed is both cheaper and more complete than scanning
// blocks around players looking for them.
func (h *hub) noteConduitBlock(dim int, pos blockPos, state uint32) {
	key := simPos{dim: dim, blockPos: pos}
	if state == conduitState {
		if h.conduits == nil {
			h.conduits = map[simPos]bool{}
		}
		h.conduits[key] = true
		return
	}
	delete(h.conduits, key)
}

// updateConduits runs every known conduit that has a player near enough to
// care. Anything whose block has gone is forgotten as it is found.
func (h *hub) updateConduits(players map[int32]*tracked) {
	for key := range h.conduits {
		if !h.worldFor(key.dim).Ticking(int32(key.x>>4), int32(key.z>>4)) {
			continue // only in a loaded chunk; never generate one here
		}
		if h.worldFor(key.dim).At(key.x, key.y, key.z) != conduitState {
			delete(h.conduits, key) // mined out, or the position was never one
			delete(h.conduitRuns, key)
			continue
		}
		near := false
		for _, t := range players {
			if t.dim == key.dim && !t.dead &&
				dist3(t.x, t.y, t.z, float64(key.x), float64(key.y), float64(key.z)) <= conduitWakeRange {
				near = true
				break
			}
		}
		if near {
			h.runConduit(players, key.dim, key.blockPos)
		}
	}
}

// conduitWakeRange is how close a player must be for a conduit to bother
// running. Generous: the effect itself reaches 96 blocks at a full frame.
const conduitWakeRange = 128.0

// conduitRun is a conduit block entity's live state: whether it was active
// at its last shape check, the mob it is hunting, and when its short
// ambient hum is next due.
type conduitRun struct {
	active    bool
	target    int32
	nextShort uint64
}

// runConduit is ConduitBlockEntity.serverTick on the engine's 20-tick
// cadence: the shape check, effects and attack run every 40 ticks as in
// vanilla, with the activate/deactivate sound on a change; while active it
// hums every 80 ticks and gives a short hum every 60–99.
func (h *hub) runConduit(players map[int32]*tracked, dim int, pos blockPos) {
	key := simPos{dim: dim, blockPos: pos}
	if h.conduitRuns == nil {
		h.conduitRuns = map[simPos]*conduitRun{}
	}
	cr := h.conduitRuns[key]
	if cr == nil {
		cr = &conduitRun{}
		h.conduitRuns[key] = cr
	}
	now := h.tick.Load()
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	if (now/survivalTickN)%2 == 0 { // gameTime % 40
		active := h.conduitActiveBlocks(dim, pos)
		if (active > 0) != cr.active {
			snd := "minecraft:block.conduit.deactivate"
			if active > 0 {
				snd = "minecraft:block.conduit.activate"
			}
			h.playSoundDim(players, dim, snd, sndBlock, cx, cy, cz, 1, 1)
		}
		cr.active = active > 0
		if cr.active {
			h.conduitEffects(players, dim, pos, active)
		}
		h.conduitAttack(players, dim, pos, cr, active >= conduitMinKill)
	}
	if cr.active {
		if now%80 < survivalTickN { // gameTime % 80
			h.playSoundDim(players, dim, "minecraft:block.conduit.ambient", sndBlock, cx, cy, cz, 1, 1)
		}
		if now > cr.nextShort {
			cr.nextShort = now + 60 + uint64(h.rng.Intn(40))
			h.playSoundDim(players, dim, "minecraft:block.conduit.ambient.short", sndBlock, cx, cy, cz, 1, 1)
		}
	}
}

// conduitEffects is ConduitBlockEntity.applyEffects: Conduit Power for
// every player whose block is closer than activeSize/7·16 to the conduit's
// and who is in water or in rain (Entity.isInWaterOrRain, at their feet or
// the top of their box).
func (h *hub) conduitEffects(players map[int32]*tracked, dim int, pos blockPos, active int) {
	rangeBlocks := float64(active/7) * conduitRangeStep
	for _, t := range players {
		// Every player in range, not only the ones playing for keeps: a
		// creative build gets Conduit Power too. A spectator is the one
		// exception the engine makes everywhere.
		if t.dim != dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		bx, by, bz := floorInt(t.x), floorInt(t.y), floorInt(t.z)
		if dist3sq(float64(bx), float64(by), float64(bz), float64(pos.x), float64(pos.y), float64(pos.z)) >= rangeBlocks*rangeBlocks {
			continue // BlockPos.closerThan: strictly inside
		}
		if !h.inWaterOrRain(dim, t.x, t.y, t.z, 1.8) {
			continue
		}
		h.applyEffectFrom(players, t, effConduitPower, 0, conduitPowerSecs, true) // Conduit: ambient
	}
}

// conduitAttack is updateAndAttackTarget: a full frame keeps hunting the
// mob it chose while that one lives and stays within 8 blocks, else picks
// a random Enemy in water or rain inside the conduit's cell grown by 8; the
// target takes 4 magic damage with the attack sound at the target.
func (h *hub) conduitAttack(players map[int32]*tracked, dim int, pos blockPos, cr *conduitRun, hunting bool) {
	if !hunting {
		cr.target = 0
		return
	}
	var target *mob
	if m := h.mobs[cr.target]; m != nil && m.dying == 0 && m.dim == dim &&
		dist3sq(float64(floorInt(m.x)), float64(floorInt(m.y)), float64(floorInt(m.z)), float64(pos.x), float64(pos.y), float64(pos.z)) < conduitKillRange*conduitKillRange {
		target = m
	}
	if target == nil {
		var cands []*mob
		for _, m := range h.mobs {
			if m.dim != dim || m.dying > 0 || !isEnemyType(m.etype) {
				continue
			}
			b := m.box()
			if m.x+b.w/2 <= float64(pos.x)-conduitKillRange || m.x-b.w/2 >= float64(pos.x)+1+conduitKillRange ||
				m.z+b.w/2 <= float64(pos.z)-conduitKillRange || m.z-b.w/2 >= float64(pos.z)+1+conduitKillRange ||
				m.y+b.h <= float64(pos.y)-conduitKillRange || m.y >= float64(pos.y)+1+conduitKillRange {
				continue
			}
			if h.inWaterOrRain(dim, m.x, m.y, m.z, b.h) {
				cands = append(cands, m)
			}
		}
		if len(cands) > 0 {
			target = cands[h.rng.Intn(len(cands))]
		}
	}
	cr.target = 0
	if target == nil {
		return
	}
	cr.target = target.eid
	h.playSoundDim(players, dim, "minecraft:block.conduit.attack.target", sndBlock, target.x, target.y, target.z, 1, 1)
	h.hurtMobEffect(players, target, conduitAttackDmg)
}

// inWaterOrRain is Entity.isInWaterOrRain for a box of height ht standing
// at (x, y, z): in water, or rained on at its feet or at its top.
func (h *hub) inWaterOrRain(dim int, x, y, z, ht float64) bool {
	if h.inWater(dim, x, y, z) {
		return true
	}
	if dim != dimOverworld || !h.raining {
		return false
	}
	bx, bz := floorInt(x), floorInt(z)
	return h.rainsOn(bx, floorInt(y), bz) || h.rainsOn(bx, floorInt(y+ht), bz)
}

// rainsOn is Level.isRainingAt for one cell: it sees the sky, nothing
// motion-blocking (a solid, a fluid, glass, leaves) stands above it (the
// heightmap is at or below it), and its biome rains there.
func (h *hub) rainsOn(x, y, z int) bool {
	if !h.canSeeSky(dimOverworld, x, y, z) {
		return false
	}
	for yy := y + 1; h.inWorldYIn(dimOverworld, yy); yy++ {
		if st := h.world.At(x, yy, z); worldgen.Collides(st) || worldgen.IsFluid(st) {
			return false
		}
	}
	return worldgen.PrecipitationAt(h.world.BiomeAt(x, z), y) == worldgen.PrecipRain
}
