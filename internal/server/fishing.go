package server

import (
	"encoding/binary"
	"math"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Fishing — the rod casts a bobber projectile that flies, lands on water and
// bobs, and runs vanilla's three-timer catch sequence (wait → a fish
// approaches → nibble window); reeling during the nibble rolls the vanilla
// fish/junk/treasure loot pools. Lure shortens the wait (-100 ticks/level),
// Luck of the Sea shifts the pool weights via each pool's quality, and
// treasure only ever bites in OPEN WATER (the vanilla 5×5×4 clearance test).
// Hooking a mob mid-flight and reeling yanks it toward the player.

const (
	bobberFlying  = 0 // airborne after the cast
	bobberBobbing = 1 // floating on water, fishing
	bobberHooked  = 2 // stuck in a mob

	bobberMaxDist2   = 1024 // vanilla: >32 blocks from the owner snaps the line
	bobberGroundLife = 1200 // vanilla: a beached bobber despawns after 60 s

	// Vanilla FishingHook physics.
	bobberGravity = 0.03 // per-tick sink when airborne
	bobberDrag    = 0.92 // per-tick velocity scale

	// Catch-sequence bounds (ticks, all vanilla).
	fishWaitMin, fishWaitMax = 100, 600 // timeUntilLured roll (Lure subtracts 100/level)
	fishHookMin, fishHookMax = 20, 80   // timeUntilHooked: the approach
	fishNibbleMin            = 20       // nibble window: reel now or lose it
	fishNibbleMax            = 40

	enchLuckOfTheSea = 20 // our declared enchantment-registry order
	enchLure         = 21
	enchFlame        = 12 // treasure-enchant pools (bow/book rewards)
	enchInfinity     = 16
	enchPower        = 25
	enchPunch        = 28
	enchMending      = 22
)

var (
	itemFishingRod = itemByName["fishing_rod"]
	entityBobber   = entityID("fishing_bobber")
)

type evFishUse struct{ eid int32 } // right-clicked holding a fishing rod

func (evFishUse) isHubEvent() {}

// bobberEntity is one player's cast bobber (at most one each, keyed by owner).
type bobberEntity struct {
	eid        int32
	uuid       [16]byte
	owner      int32 // the caster's eid
	dim        int
	x, y, z    float64
	vx, vy, vz float64
	sx, sy, sz float64 // last broadcast position

	state    int
	hooked   int32 // mob eid while state == bobberHooked
	grounded bool  // came to rest on land (reel = 2 durability, despawn timer runs)
	life     int   // grounded ticks toward bobberGroundLife

	// Catch sequence (vanilla names: timeUntilLured / timeUntilHooked / nibble).
	wait       int
	hookT      int
	nibble     int
	fishAngle  float64 // degrees: where the approaching fish's wake shows
	openWater  bool    // 5×5×4 clearance held for the whole sequence → treasure eligible
	outOfWater int

	luck      int // Luck of the Sea level at cast time
	lureSpeed int // Lure level × 100 ticks off each wait roll
}

// useRod is the rod right-click: reel in an existing bobber, or cast one.
func (h *hub) useRod(players map[int32]*tracked, t *tracked) {
	if b := h.bobbers[t.p.eid]; b != nil {
		h.reelBobber(players, t, b)
		return
	}
	h.castBobber(players, t)
}

// castBobber launches the bobber with vanilla's throw kinematics: spawned a
// third of a block behind the eye line, aimed down the look vector with the
// pitch component clamped, sped 0.6/|v| + triangle(0.5, ~0.0103) per axis.
func (h *hub) castBobber(players map[int32]*tracked, t *tracked) {
	yawR := float64(t.yaw) * math.Pi / 180
	pitchR := float64(t.pitch) * math.Pi / 180
	yCos, ySin := math.Cos(-yawR-math.Pi), math.Sin(-yawR-math.Pi)
	xCos, xSin := -math.Cos(-pitchR), math.Sin(-pitchR)

	x, y, z := t.x-ySin*0.3, t.y+1.5, t.z-yCos*0.3
	vx, vy, vz := -ySin, clampF(-(xSin/xCos), -5, 5), -yCos
	d := math.Sqrt(vx*vx + vy*vy + vz*vz)
	if d < 1e-9 {
		return
	}
	tri := func() float64 { return 0.6/d + 0.5 + 0.0103365*(h.rng.Float64()-h.rng.Float64()) }
	vx, vy, vz = vx*tri(), vy*tri(), vz*tri()

	st := heldStack(t)
	eid := h.allocEID()
	b := &bobberEntity{eid: eid, owner: t.p.eid, dim: t.dim, x: x, y: y, z: z,
		vx: vx, vy: vy, vz: vz, sx: x, sy: y, sz: z, openWater: true,
		luck: st.enchLvl(enchLuckOfTheSea), lureSpeed: st.enchLvl(enchLure) * 100}
	binary.BigEndian.PutUint32(b.uuid[12:], uint32(eid))
	h.bobbers[t.p.eid] = b

	add := entAdd(eid, int(entityBobber), b.uuid, x, y, z, t.yaw, t.pitch)
	add.Data = t.p.eid // the client attaches the line to this entity (and discards ownerless bobbers)
	add.VX, add.VY, add.VZ = vx, vy, vz
	h.toNearbyEv(players, b.dim, x, z, add)

	h.playSoundDim(players, b.dim, "minecraft:entity.fishing_bobber.throw", sndNeutral,
		t.x, t.y, t.z, 0.5, 0.4/(h.rng.Float32()*0.4+0.8))
	h.incStat(t, attachproto.StatUsed, itemFishingRod, 1)
}

// waterHeight is how much of the cell the water fills (0 = not water):
// source 8/9, flowing (8-level)/9, a falling column the whole cell.
func waterHeight(st uint32) float64 {
	if !worldgen.IsWater(st) {
		return 0
	}
	lvl := st - worldgen.WaterBase
	if lvl >= 8 {
		return 1
	}
	return float64(8-lvl) / 9
}

// updateBobbers runs every bobber one tick: line-break checks, the flight/
// float state machine, the catch sequence, movement, and the move broadcast.
func (h *hub) updateBobbers(players map[int32]*tracked) {
	for owner, b := range h.bobbers {
		t := players[owner]
		if t == nil || t.dead || t.dim != b.dim || t.p.heldItem() != itemFishingRod ||
			(t.x-b.x)*(t.x-b.x)+(t.y-b.y)*(t.y-b.y)+(t.z-b.z)*(t.z-b.z) > bobberMaxDist2 {
			h.discardBobber(players, b) // owner gone / switched away / too far: the line snaps
			continue
		}
		if b.grounded {
			if b.life++; b.life >= bobberGroundLife {
				h.discardBobber(players, b)
				continue
			}
		} else {
			b.life = 0
		}

		w := h.worldFor(b.dim)
		bx, by, bz := int(math.Floor(b.x)), int(math.Floor(b.y)), int(math.Floor(b.z))
		liquid := waterHeight(w.At(bx, by, bz))
		inWater := liquid > 0

		switch b.state {
		case bobberFlying:
			if inWater {
				b.vx, b.vy, b.vz = b.vx*0.3, b.vy*0.2, b.vz*0.3
				b.state = bobberBobbing
			}
		case bobberHooked:
			m := h.mobs[b.hooked]
			if m == nil || m.dying > 0 || m.dim != b.dim {
				b.hooked, b.state = 0, bobberFlying
			} else {
				b.x, b.y, b.z = m.x, m.y+1.0, m.z
				h.broadcastBobberMove(players, b)
				continue
			}
		case bobberBobbing:
			// Bob around the water surface: spring toward the surface line.
			force := b.y + b.vy - float64(by) - liquid
			if math.Abs(force) < 0.01 {
				force += math.Copysign(0.1, force)
			}
			b.vx *= 0.9
			b.vy -= force * h.rng.Float64() * 0.2
			b.vz *= 0.9
			if b.nibble <= 0 && b.hookT <= 0 {
				b.openWater = true
			} else {
				b.openWater = b.openWater && b.outOfWater < 10 && h.bobberOpenWater(b)
			}
			if inWater {
				b.outOfWater = max(0, b.outOfWater-1)
				if b.nibble > 0 { // the fish drags the float under
					b.vy -= 0.1 * h.rng.Float64() * h.rng.Float64()
				}
				h.catchingFish(players, b)
			} else {
				b.outOfWater = min(10, b.outOfWater+1)
			}
		}

		if !inWater && !b.grounded {
			b.vy -= bobberGravity
		}
		// Step with a midpoint sample (cast speeds can cross a block a tick);
		// mobs hook the bobber mid-flight, blocks beach it.
		for _, f := range [2]float64{0.5, 1.0} {
			px, py, pz := b.x+b.vx*f, b.y+b.vy*f, b.z+b.vz*f
			if b.state == bobberFlying {
				if m := h.bobberHitsMob(b, px, py, pz); m != nil {
					b.hooked, b.state = m.eid, bobberHooked
					b.vx, b.vy, b.vz = 0, 0, 0
					break
				}
			}
			if worldgen.Collides(w.At(int(math.Floor(px)), int(math.Floor(py)), int(math.Floor(pz)))) {
				if b.state == bobberFlying {
					b.grounded = true
				}
				b.vx, b.vy, b.vz = 0, 0, 0
				break
			}
			b.x, b.y, b.z = px, py, pz
		}
		b.vx, b.vy, b.vz = b.vx*bobberDrag, b.vy*bobberDrag, b.vz*bobberDrag
		h.broadcastBobberMove(players, b)
	}
}

func (h *hub) broadcastBobberMove(players map[int32]*tracked, b *bobberEntity) {
	if b.x == b.sx && b.y == b.sy && b.z == b.sz {
		return
	}
	b.sx, b.sy, b.sz = b.x, b.y, b.z
	h.toNearbyEv(players, b.dim, b.x, b.z, entMove(b.eid, b.x, b.y, b.z, 0, 0, false))
}

// bobberHitsMob finds a living mob whose hitbox contains the sample point
// (same cylinder the arrows use).
func (h *hub) bobberHitsMob(b *bobberEntity, px, py, pz float64) *mob {
	for _, m := range h.mobs {
		if m.dying > 0 || m.dim != b.dim {
			continue
		}
		dx, dz := px-m.x, pz-m.z
		if dx*dx+dz*dz <= arrowHitRadius*arrowHitRadius && py >= m.y-0.1 && py <= m.y+2 {
			return m
		}
	}
	return nil
}

// catchingFish advances the vanilla three-timer sequence while the bobber
// floats: wait (Lure-shortened) → hook approach (wake particles closing in) →
// nibble (splash; reel NOW). Rain over the bobber speeds it up, no sky above
// slows it down.
func (h *hub) catchingFish(players map[int32]*tracked, b *bobberEntity) {
	bx, by, bz := int(math.Floor(b.x)), int(math.Floor(b.y)), int(math.Floor(b.z))
	speed := 1
	if h.rng.Float32() < 0.25 && h.isRainingAt(bx, by+1, bz) {
		speed++
	}
	if h.rng.Float32() < 0.5 && !h.skyExposedColumn(bx, bz) {
		speed--
	}

	switch {
	case b.nibble > 0:
		b.nibble--
		if b.nibble <= 0 { // it got away — start over
			b.wait, b.hookT = 0, 0
		}
	case b.hookT > 0:
		b.hookT -= speed
		if b.hookT > 0 {
			// The fish wake spirals toward the bobber.
			b.fishAngle += 9.188 * (h.rng.Float64() - h.rng.Float64())
			rad := b.fishAngle * math.Pi / 180
			fx := b.x + math.Sin(rad)*float64(b.hookT)*0.1
			fz := b.z + math.Cos(rad)*float64(b.hookT)*0.1
			fy := math.Floor(b.y) + 1
			if worldgen.IsWater(h.worldFor(b.dim).At(int(math.Floor(fx)), int(fy)-1, int(math.Floor(fz)))) {
				if h.rng.Float32() < 0.15 {
					h.spawnParticles(players, particleBubble, fx, fy-0.1, fz, 0.05, 0, 1)
				}
				h.spawnParticles(players, particleFishing, fx, fy, fz, 0.05, 0.01, 2)
			}
		} else { // BITE
			h.playSoundDim(players, b.dim, "minecraft:entity.fishing_bobber.splash", sndNeutral,
				b.x, b.y, b.z, 0.25, 1+(h.rng.Float32()-h.rng.Float32())*0.4)
			h.spawnParticles(players, particleBubble, b.x, b.y+0.5, b.z, 0.25, 0.2, 6)
			h.spawnParticles(players, particleFishing, b.x, b.y+0.5, b.z, 0.25, 0.2, 6)
			b.nibble = fishNibbleMin + h.rng.Intn(fishNibbleMax-fishNibbleMin+1)
			b.vy -= 0.4 * (0.6 + 0.4*h.rng.Float64()) // the float ducks under
		}
	case b.wait > 0:
		b.wait -= speed
		tease := float32(0.15)
		switch {
		case b.wait < 20:
			tease += float32(20-b.wait) * 0.05
		case b.wait < 40:
			tease += float32(40-b.wait) * 0.02
		case b.wait < 60:
			tease += float32(60-b.wait) * 0.01
		}
		if h.rng.Float32() < tease {
			rad := h.rng.Float64() * 2 * math.Pi
			dist := 2.5 + h.rng.Float64()*3.5
			fx, fz := b.x+math.Sin(rad)*dist, b.z+math.Cos(rad)*dist
			fy := math.Floor(b.y) + 1
			if worldgen.IsWater(h.worldFor(b.dim).At(int(math.Floor(fx)), int(fy)-1, int(math.Floor(fz)))) {
				h.spawnParticles(players, particleSplash, fx, fy, fz, 0.1, 0, int32(2+h.rng.Intn(2)))
			}
		}
		if b.wait <= 0 {
			b.fishAngle = h.rng.Float64() * 360
			b.hookT = fishHookMin + h.rng.Intn(fishHookMax-fishHookMin+1)
		}
	default:
		b.wait = fishWaitMin + h.rng.Intn(fishWaitMax-fishWaitMin+1) - b.lureSpeed
	}
}

// bobberOpenWater is vanilla's treasure gate: every 5×5 layer from one below
// the bobber to two above must be uniformly water (sources) or uniformly open
// air, water never above air.
func (h *hub) bobberOpenWater(b *bobberEntity) bool {
	bx, by, bz := int(math.Floor(b.x)), int(math.Floor(b.y)), int(math.Floor(b.z))
	const (
		owAbove = iota
		owWater
		owBad
	)
	prev := owBad // (vanilla starts the sequence at INVALID: the bottom layer must be water)
	for dy := -1; dy <= 2; dy++ {
		layer := -1
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				st := h.worldFor(b.dim).At(bx+dx, by+dy, bz+dz)
				kind := owBad
				if st == 0 || st == worldgen.LilyPad {
					kind = owAbove
				} else if worldgen.IsWater(st) && st-worldgen.WaterBase == 0 {
					kind = owWater
				}
				if layer == -1 {
					layer = kind
				} else if layer != kind {
					layer = owBad // a mixed layer disqualifies
				}
			}
		}
		switch layer {
		case owAbove:
			if prev == owBad {
				return false
			}
		case owWater:
			if prev == owAbove { // water above air is not open water
				return false
			}
		case owBad:
			return false
		}
		prev = layer
	}
	return true
}

// reelBobber is the second right-click: pull a hooked mob, land a bite's
// loot, or just wind in — with vanilla's rod wear for each (5 / 1 / 2 beached).
func (h *hub) reelBobber(players map[int32]*tracked, t *tracked, b *bobberEntity) {
	wear := 0
	switch {
	case b.state == bobberHooked && h.mobs[b.hooked] != nil:
		m := h.mobs[b.hooked]
		if !m.noKB { // vanilla pullEntity: velocity += (owner − hook) · 0.1
			m.vx, m.vz, m.kb, m.reroute = (t.x-m.x)*0.1, (t.z-m.z)*0.1, 3, 0
			h.mobKnockVelocity(players, m)
		}
		wear = 5
	case b.nibble > 0:
		st, isFish := h.rollFishingLoot(t, b)
		if st.count > 0 {
			changed, left := t.inv.addStack(st)
			for _, sl := range changed {
				h.sendSlot(t, sl)
			}
			if left > 0 { // inventory full — the catch lands at the player's feet
				if it := h.spawnItemIn(players, t.dim, st.item, left, t.x, t.y, t.z); it != nil {
					it.dmg, it.ench = st.dmg, st.ench
				}
			}
			h.playSound(players, "minecraft:entity.item.pickup", sndPlayer, t.x, t.y, t.z, 0.4, 1.5)
		}
		h.spawnXPOrb(players, h.rng.Intn(6)+1, t.x, t.y+0.5, t.z+0.5)
		if isFish {
			h.incCustom(t, "fish_caught", 1)
			h.advance(players, t, "fishing_rod_hooked", advMatch{item: st.item})
		}
		wear = 1
	}
	if b.grounded {
		wear = 2
	}
	if wear > 0 {
		h.applyToolWear(t, t.p.heldSlot(), wear)
	}
	h.playSoundDim(players, b.dim, "minecraft:entity.fishing_bobber.retrieve", sndNeutral,
		t.x, t.y, t.z, 1, 0.4/(h.rng.Float32()*0.4+0.8))
	h.discardBobber(players, b)
}

func (h *hub) discardBobber(players map[int32]*tracked, b *bobberEntity) {
	delete(h.bobbers, b.owner)
	h.toNearbyEv(players, b.dim, b.x, b.z, entGone(b.eid))
}

// ---- The vanilla fishing loot tables (gameplay/fishing + sub-pools) ----

// rollFishingLoot picks the pool (fish 85 / junk 10 / treasure 5, each
// shifted by quality × luck; treasure only in open water), then the entry.
// Returns the reward stack and whether it counts as a caught FISH.
func (h *hub) rollFishingLoot(t *tracked, b *bobberEntity) (invStack, bool) {
	luck := b.luck
	junkW := max(0, 10-2*luck) // quality -2
	fishW := max(0, 85-luck)   // quality -1
	treasureW := 0
	if b.openWater {
		treasureW = 5 + 2*luck // quality +2
	}
	r := h.rng.Intn(junkW + fishW + treasureW)
	switch {
	case r < fishW:
		return h.rollFish(), true
	case r < fishW+junkW:
		return h.rollFishJunk(b), false
	default:
		return h.rollFishTreasure(), false
	}
}

func one(name string) invStack { return invStack{item: itemByName[name], count: 1} }

func (h *hub) rollFish() invStack {
	r := h.rng.Intn(100)
	switch {
	case r < 60:
		return one("cod")
	case r < 85:
		return one("salmon")
	case r < 98:
		return one("pufferfish")
	default:
		return one("tropical_fish")
	}
}

// wornStack is a set_damage roll: remain is the surviving-durability range;
// the wear lands anywhere in the rest of the bar.
func (h *hub) wornStack(name string, remainLo, remainHi float64) invStack {
	st := one(name)
	remain := remainLo + h.rng.Float64()*(remainHi-remainLo)
	st.dmg = int(math.Floor((1 - remain) * float64(itemMaxDurability[st.item])))
	return st
}

func (h *hub) rollFishJunk(b *bobberEntity) invStack {
	type entry struct {
		w    int
		make func() invStack
	}
	entries := []entry{
		{17, func() invStack { return one("lily_pad") }},
		{10, func() invStack { return h.wornStack("leather_boots", 0, 0.9) }},
		{10, func() invStack { return one("leather") }},
		{10, func() invStack { return one("bone") }},
		{10, func() invStack { return potionStack(potWater) }},
		{5, func() invStack { return one("string") }},
		{2, func() invStack { return h.wornStack("fishing_rod", 0, 0.9) }},
		{10, func() invStack { return one("bowl") }},
		{5, func() invStack { return one("stick") }},
		{1, func() invStack { return invStack{item: itemByName["ink_sac"], count: 10} }},
		{10, func() invStack { return one("tripwire_hook") }},
		{10, func() invStack { return one("rotten_flesh") }},
	}
	if strings.Contains(h.worldFor(b.dim).BiomeAt(int(b.x), int(b.z)), "jungle") {
		entries = append(entries, entry{10, func() invStack { return one("bamboo") }})
	}
	total := 0
	for _, e := range entries {
		total += e.w
	}
	r := h.rng.Intn(total)
	for _, e := range entries {
		if r < e.w {
			return e.make()
		}
		r -= e.w
	}
	return invStack{}
}

func (h *hub) rollFishTreasure() invStack {
	switch h.rng.Intn(6) {
	case 0:
		return one("name_tag")
	case 1:
		return one("saddle")
	case 2:
		st := h.wornStack("bow", 0, 0.25)
		st.ench = h.fishingTreasureEnch(st.item)
		return st
	case 3:
		st := h.wornStack("fishing_rod", 0, 0.25)
		st.ench = h.fishingTreasureEnch(st.item)
		return st
	case 4:
		st := one("book")
		st.item = itemEnchantedBook // treasure books come pre-enchanted (stored)
		st.ench = h.fishingTreasureEnch(itemBook)
		return st
	default:
		return one("nautilus_shell")
	}
}

// fishingTreasureEnch stands in for vanilla's enchant-with-30-levels roll:
// one near-cap enchantment fitting the item, often with Unbreaking beside it.
func (h *hub) fishingTreasureEnch(item int32) [2]enchApply {
	var pool []int8
	switch item {
	case itemBow:
		pool = []int8{enchPower, enchPunch, enchFlame, enchInfinity, enchUnbreaking}
	case itemFishingRod:
		pool = []int8{enchLure, enchLuckOfTheSea, enchUnbreaking, enchMending}
	default: // books draw from the wide table pool plus the treasure-only ids
		pool = []int8{enchSharpness, enchEfficiency, enchProtection, enchUnbreaking,
			enchFortune, enchLooting, enchPower, enchLure, enchLuckOfTheSea, enchMending}
	}
	primary := pool[h.rng.Intn(len(pool))]
	lvl := max(1, int(enchMaxLvl(primary))-h.rng.Intn(2))
	out := [2]enchApply{{id: primary, lvl: int8(lvl)}}
	if primary != enchUnbreaking && h.rng.Intn(4) == 0 {
		out[1] = enchApply{id: enchUnbreaking, lvl: int8(1 + h.rng.Intn(3))}
	}
	return out
}
