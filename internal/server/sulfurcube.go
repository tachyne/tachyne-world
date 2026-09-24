package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The sulfur cube (26.3): a cube mob of the sulfur caves that never attacks
// and swallows blocks. Empty, it hops about like a slime — size 2 grown,
// size 1 as a baby, four health per size, and on death it splits into two
// babies. Fed a block from #sulfur_cube_swallowable (by hand, or a thrown
// one it hops over to), it stops being a creature and becomes a ball: no
// goals, no speed, no step-up, a physics body whose bounce, friction and
// air drag come from the block's archetype, pushed along by whoever walks
// into it and knocked about by every blow, which it no longer takes as
// damage (#sulfur_cube_with_block_immune_to). Shears pop the block back out;
// an empty bucket scoops the cube, block and all.
//
// Two archetypes do more than roll: TNT (explosive) lights like a TNT block
// — flint and steel, a fire charge, any fire or burning arrow, a redstone
// signal, and a blast lights it short — and goes off with power 3 after 120
// ticks; a magma block (hot) burns whatever it touches for 1 of
// sulfur_cube_hot. A cube taking TNT from a player's hand or picking up TNT
// a player threw is husbandry/uh_oh.

var (
	entitySulfurCube     = entityID("sulfur_cube")
	itemSulfurCubeBucket = itemByName["sulfur_cube_bucket"]

	sulfurSwallowable = itemTagSet("sulfur_cube_swallowable")
	sulfurFood        = itemTagSet("sulfur_cube_food")
)

const (
	sulfurAdultSize   = 2   // SulfurCube.MAX_SIZE: setSpawnSize for a grown cube
	sulfurPickupTicks = 100 // PICKUP_TIMER_DURATION: after a shearing, no swallowing
	sulfurTemptRange  = 8.0 // createSulfurCubeAttributes: TEMPT_RANGE
	sulfurSearchRange = 8.0 // SulfurCubeSearchForItemsGoal: the box inflated by 8
	sulfurPushDist    = 1.3 // PUSH_DISTANCE_THRESHOLD
	sulfurGravity     = 0.08
	sulfurFuse        = 120 // the explosive archetype's ExplosionData.fuse
	sulfurBlastPower  = 3   // …its power (no fire)
	sulfurHotDamage   = 1.0 // the hot archetype's ContactDamage amount

	// The cube's own synced fields, at their 26.x indices. They are emitted
	// as the 26.x layout directly (there is no 1.21.5 sulfur cube to have a
	// canonical index): Entity 0-7, LivingEntity 8-14, Mob 15, AgeableMob
	// 16 (baby) and 17 (age locked), AbstractCubeMob 18 (size), then the
	// cube's MAX_FUSE (19, Integer) and FROM_BUCKET (20, Boolean) in their
	// declaration order — identical in 26.2 and 26.3. The gateway's cube
	// shift moves only a VarInt at 16, so these pass through untouched.
	metaIndexSulfurSize       = 18
	metaIndexSulfurMaxFuse    = 19
	metaIndexSulfurFromBucket = 20
)

// sulfurState is a sulfur cube's own state on its mob.
type sulfurState struct {
	body       invStack // the swallowed block (EquipmentSlot.BODY); item 0 = empty
	fuse       int      // ticks left on a lit fuse
	lit        bool     // isPrimed: the fuse is burning
	maxFuse    int      // MAX_FUSE as last synced (-1 unlit)
	prime      int8     // a hurt asked for a prime this tick (hurtOf has no hub): 1 normal, 2 imminent
	pickup     int      // pickupTimer: ticks before it may swallow a block again
	pushCD     int      // pushSoundCooldown
	vx, vy, vz float64  // motion per tick while it carries a block
	ground     bool     // on the ground after the last physics step
	dirYaw     float64  // CubeMobRandomDirectionGoal.chosenDegrees
	dirNext    int      // …and the ticks before it picks again
	hopping    bool     // mid-bound (the landing squish is due when it comes down)
}

const (
	sulfurPrimeNormal   int8 = 1
	sulfurPrimeImminent int8 = 2
)

// sulfurArchetype is one SulfurCubeArchetype: what a swallowed block makes
// of the cube. The four movement numbers become attribute modifiers
// (archetype(speed, bounce, friction, drag) in SulfurCubeArchetypes).
type sulfurArchetype struct {
	name                          string
	speed, bounce, friction, drag float32
	buoyant                       bool
	explosive                     bool // ExplosionData(power 3, no fire, fuse 120)
	hot                           bool // ContactDamage(sulfur_cube_hot, 1, not attributed)
	kbH, kbV                      float32
	pushThreshold, pushCooldown   float32
	items                         map[int32]bool
}

// sulfurArchetypes in SulfurCubeArchetypes.bootstrap order. No block sits in
// two archetype tags (the tag test checks), so the order never picks one.
var sulfurArchetypes = []sulfurArchetype{
	{name: "regular", speed: 1.0, bounce: 0.5, friction: 0.3, drag: 0.1, buoyant: true, kbH: 0.4125, kbV: 0.09, pushThreshold: 0.2, pushCooldown: 0.5},
	{name: "bouncy", speed: 2.0, bounce: 0.9, friction: 0.3, drag: 0.01, buoyant: true, kbH: 0.4125, kbV: 0.105, pushThreshold: 0.3, pushCooldown: 0.7},
	{name: "slow_bouncy", speed: -0.4, bounce: 0.6, friction: 0.3, drag: 0.05, kbH: 0.4125, kbV: 0.24, pushThreshold: 0.05, pushCooldown: 0.5},
	{name: "slow_flat", speed: -0.5, bounce: 0.4, friction: 0.4, drag: 0.1, kbH: 0.4125, kbV: 0.105, pushThreshold: 0.03, pushCooldown: 0.9},
	{name: "fast_flat", speed: 1.0, bounce: 0.5, friction: 0.2, drag: 0.01, kbH: 0.9125, kbV: 0.09, pushThreshold: 0.03, pushCooldown: 0.9},
	{name: "light", speed: 1.0, bounce: 1.0, friction: 0.3, drag: 1.8, buoyant: true, kbH: 0.4125, kbV: 0.18, pushThreshold: 0.2, pushCooldown: 0.7},
	{name: "fast_sliding", speed: -0.5, bounce: 0.1, friction: 0.05, drag: 0.01, kbH: 0.6625, kbV: 0.09, pushThreshold: 0.05, pushCooldown: 1.0},
	{name: "slow_sliding", speed: -0.8, bounce: 0.1, friction: 0.05, drag: 0.01, kbH: 0.4125, kbV: 0.09, pushThreshold: 0.02, pushCooldown: 1.0},
	{name: "sticky", speed: 2.0, bounce: 0.0, friction: 2.0, drag: 0.01, kbH: 0.4125, kbV: 0.09, pushThreshold: 0.05, pushCooldown: 0.5},
	{name: "high_resistance", speed: -0.7, bounce: 0.2, friction: 1.0, drag: 0.01, kbH: 0.4125, kbV: 0.09, pushThreshold: 0.03, pushCooldown: 0.7},
	{name: "explosive", speed: 1.0, bounce: 0.5, friction: 0.3, drag: 0.3, buoyant: true, explosive: true, kbH: 0.4125, kbV: 0.09, pushThreshold: 0.1, pushCooldown: 0.7},
	{name: "hot", speed: 1.0, bounce: 0.5, friction: 0.3, drag: 0.1, buoyant: true, hot: true, kbH: 0.4125, kbV: 0.09, pushThreshold: 0.2, pushCooldown: 0.7},
}

func init() {
	for i := range sulfurArchetypes {
		sulfurArchetypes[i].items = itemTagSet("sulfur_cube_archetype/" + sulfurArchetypes[i].name)
	}
}

// sulfurArchetypeFor is the archetype a swallowed item gives (nil: none).
func sulfurArchetypeFor(item int32) *sulfurArchetype {
	for i := range sulfurArchetypes {
		if sulfurArchetypes[i].items[item] {
			return &sulfurArchetypes[i]
		}
	}
	return nil
}

// sulfurSounds are the archetype's hit and push sounds.
func (a *sulfurArchetype) hitSound() string {
	return "minecraft:entity.sulfur_cube." + a.name + ".hit"
}
func (a *sulfurArchetype) pushSound() string {
	return "minecraft:entity.sulfur_cube." + a.name + ".push"
}

// sulfurDefaultArchetype is DEFAULT_KNOCKBACK_MODIFIERS / DEFAULT_SOUND_SETTINGS:
// a cube without an archetype hits and pushes like a regular one.
var sulfurDefaultArchetype = sulfurArchetype{name: "regular", kbH: 0.33, kbV: 0.06, pushThreshold: 0.2, pushCooldown: 0.5}

// hasBody reports a block in the cube.
func (m *mob) hasBody() bool { return m.etype == entitySulfurCube && m.cube.body.item != 0 }

// cubeArch is the archetype of the block the cube carries (nil when empty).
func (m *mob) cubeArch() *sulfurArchetype {
	if !m.hasBody() {
		return nil
	}
	return sulfurArchetypeFor(m.cube.body.item)
}

// canExplode is SulfurCube.canExplode: an explosive block, alive, unlit.
func (m *mob) canExplode() bool {
	a := m.cubeArch()
	return a != nil && a.explosive && m.dying == 0 && !m.cube.lit
}

// f32 is a float literal as vanilla holds it: a Java float widened to double.
func f32(v float32) float64 { return float64(v) }

// sulfurModifiers are the archetype's attribute modifiers, named as
// SulfurCubeArchetype.AttributeEntry names them.
func (a *sulfurArchetype) modifiers() []struct {
	id  attr.ID
	mod attr.Modifier
} {
	add := func(id attr.ID, amount float64) struct {
		id  attr.ID
		mod attr.Modifier
	} {
		path := string(id)[len("minecraft:"):]
		return struct {
			id  attr.ID
			mod attr.Modifier
		}{id, attr.Modifier{Source: "minecraft:" + a.name + "_add_" + path, Amount: amount, Op: attr.AddValue}}
	}
	mul := func(id attr.ID, factor float32) struct {
		id  attr.ID
		mod attr.Modifier
	} {
		path := string(id)[len("minecraft:"):]
		return struct {
			id  attr.ID
			mod attr.Modifier
		}{id, attr.Modifier{Source: "minecraft:" + a.name + "_mul_" + path, Amount: f32(factor) - 1, Op: attr.AddMultipliedTotal}}
	}
	return []struct {
		id  attr.ID
		mod attr.Modifier
	}{
		add(attr.KnockbackResistance, f32(-a.speed)),
		add(attr.ExplosionKnockbackResistance, f32(-a.speed)),
		add(attr.Bounciness, f32(a.bounce)),
		mul(attr.FrictionModifier, a.friction),
		mul(attr.AirDragModifier, a.drag),
	}
}

// setCubeBody is the BODY slot changing (collectEquipmentChanges): the old
// block's modifiers come off, the new one's go on, and the goals stop or
// start again with it. The equipment reaches the viewers.
func (h *hub) setCubeBody(players map[int32]*tracked, m *mob, st invStack) {
	if old := sulfurArchetypeFor(m.cube.body.item); old != nil {
		for _, e := range old.modifiers() {
			m.mobAttrs().Get(e.id).RemoveModifier(e.mod.Source)
		}
	}
	had := m.cube.body.item != 0
	if st.item != 0 {
		st.count = 1
	}
	m.cube.body = st
	if a := sulfurArchetypeFor(st.item); a != nil {
		for _, e := range a.modifiers() {
			m.mobAttrs().Get(e.id).AddModifier(e.mod)
		}
	}
	switch {
	case st.item != 0 && !had:
		// removeAllGoals + setSpeed(0): it stops being a creature. Whatever
		// motion the hop had carries on as the ball's.
		m.cube.vx, m.cube.vy, m.cube.vz = m.vx/mobMoveInterval, 0, m.vz/mobMoveInterval
		m.vx, m.vz, m.hopTicks, m.hopDelay, m.cube.hopping = 0, 0, 0, 0, false
		m.cube.ground = true
	case st.item == 0 && had:
		m.cube.vx, m.cube.vy, m.cube.vz = 0, 0, 0 // registerGoals: a hopper again
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, cubeBodyEquip(m))
}

// cubeBodyEquip is the set_equipment frame showing the swallowed block.
func cubeBodyEquip(m *mob) attachproto.Equipment {
	var eq attachproto.Equipment
	eq.EID = m.eid
	eq.Slots[attachproto.EquipBody] = stackEv(m.cube.body)
	return eq
}

// sulfurMeta is the cube's own entity data: size, MAX_FUSE, FROM_BUCKET.
func sulfurMeta(m *mob) []byte {
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexSulfurSize)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(m.size))
	b = protocol.AppendU8(b, metaIndexSulfurMaxFuse)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(m.cube.maxFuse))
	b = protocol.AppendU8(b, metaIndexSulfurFromBucket)
	b = protocol.AppendVarInt(b, metaTypeBool)
	if m.fromBucket {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	return protocol.AppendU8(b, itemMetaEnd)
}

// initSulfurCube is finalizeSpawn's setSpawnSize: a baby is size 1, anything
// else size 2, and size 1 always makes it a baby.
func (h *hub) initSulfurCube(m *mob, baby bool) {
	m.size = sulfurAdultSize
	if baby {
		m.size = 1
	}
	m.applyCubeSize()
	m.baby = m.size == 1
	if m.baby && m.growLeft <= 0 {
		m.growLeft = growUpTicks // AgeableMob.BABY_START_AGE
	}
	m.cube.maxFuse = -1
}

// spawnSulfurCube spawns a configured cube: grown, or a baby of size 1. The
// natural spawner rolls AgeableMob's pack babies on top (spawnNatural).
func (h *hub) spawnSulfurCube(players map[int32]*tracked, dim int, x, y, z float64, baby bool) *mob {
	m := h.spawnMobIn(players, entitySulfurCube, dim, x, y, z)
	if m == nil {
		return nil
	}
	h.applySpecies(players, m)
	h.initSulfurCube(m, baby)
	return m
}

// sulfurVolume is AbstractCubeMob.getSoundVolume: 0.4 × size.
func sulfurVolume(m *mob) float32 { return 0.4 * float32(max(m.size, 1)) }

// sulfurPitch is AbstractCubeMob.getSoundPitch.
func (h *hub) sulfurPitch(m *mob) float32 {
	adj := float32(0.8)
	if m.size <= 1 {
		adj = 1.4
	}
	return ((h.rng.Float32()-h.rng.Float32())*0.2 + 1) * adj
}

func (h *hub) cubeSound(players map[int32]*tracked, m *mob, name string, vol, pitch float32) {
	h.playSoundDim(players, m.dim, name, sndNeutral, m.x, m.y, m.z, vol, pitch)
}

// ---- interactions -------------------------------------------------------

// trySulfurCube is SulfurCube.mobInteract. Returns whether the click was
// spent (the caller then fires player_interacted_with_entity with the item
// that was in hand, which is what uh_oh's give_tnt_directly matches).
func (h *hub) trySulfurCube(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entitySulfurCube || m.dying > 0 {
		return false
	}
	held := heldStack(t)
	c := &m.cube
	if m.baby {
		// The slime ball ages it: a tenth of what is left, in whole seconds.
		if !sulfurFood[held.item] {
			return false
		}
		h.ageUp(m, m.growLeft/200*20)
		if t.gamemode != gmCreative {
			h.consumeHeld(t)
		}
		h.cubeSound(players, m, "minecraft:entity.small_sulfur_cube.eat", sulfurVolume(m), h.voicePitch(m))
		return true
	}
	if c.lit {
		return false // PASS: nothing more to do with a burning cube
	}
	if m.canExplode() && (held.item == itemFlintAndSteel || held.item == itemFireCharge) {
		if !h.rules.TNTExplodes {
			t.p.trySendEv(actionBarEv("TNT explosions are disabled")) // block.minecraft.tnt.disabled
			return false
		}
		h.primeSulfurCube(players, m, false)
		if t.gamemode != gmCreative {
			if held.item == itemFlintAndSteel {
				h.applyToolWear(t, t.p.heldSlot(), 1)
			} else {
				h.consumeHeld(t)
			}
		}
		h.incStat(t, attachproto.StatUsed, held.item, 1)
		return true
	}
	if held.item == itemShears && m.hasBody() {
		st := c.body
		h.shearSulfurCube(players, m)
		h.vibAt(m.dim, freqShear, m.x, m.y, m.z, t.p.eid)
		if t.gamemode == gmSurvival {
			h.applyToolWear(t, t.p.heldSlot(), 1)
		}
		h.advance(players, t, "player_sheared_equipment", advMatch{entity: advEntityName[m.etype], item: st.item})
		return true
	}
	if sulfurSwallowable[held.item] {
		if !h.cubeEquip(players, m, held) {
			return false
		}
		if t.gamemode != gmCreative {
			h.consumeHeld(t)
		}
		return true
	}
	if held.item == itemBucket {
		return h.bucketSulfurCube(players, t, m)
	}
	return false
}

// cubeEquip is SulfurCube.equipItem: a baby never swallows; the same block
// again is refused; a different one pushes the old one out at the top.
func (h *hub) cubeEquip(players map[int32]*tracked, m *mob, st invStack) bool {
	if m.baby {
		return false
	}
	if m.hasBody() {
		if st.item == m.cube.body.item {
			return false
		}
		h.cubeSpit(players, m, m.cube.body)
	}
	st.count = 1
	h.setCubeBody(players, m, st)
	h.cubeSound(players, m, "minecraft:entity.sulfur_cube.absorb", 1, 1)
	return true
}

// cubeSpit drops a stack from the cube's top (spawnAtLocation at the
// passenger attachment), with ItemEntity's default pop.
func (h *hub) cubeSpit(players map[int32]*tracked, m *mob, st invStack) {
	top := m.box().h - 0.015625*float64(m.size)
	it := h.spawnItemAt(players, m.dim, st.item, max(st.count, 1), m.x, m.y+top, m.z,
		h.rng.Float64()*0.2-0.1, 0.2, h.rng.Float64()*0.2-0.1)
	if it != nil {
		it.setFrom(st)
		h.refreshItemMeta(players, it)
	}
}

// shearSulfurCube is SulfurCube.shear: the block comes out, the eject sound
// plays, and it will not swallow again for five seconds.
func (h *hub) shearSulfurCube(players map[int32]*tracked, m *mob) {
	st := m.cube.body
	h.setCubeBody(players, m, invStack{})
	h.cubeSpit(players, m, st)
	h.cubeSound(players, m, "minecraft:entity.sulfur_cube.eject", 1, 1)
	m.cube.pickup = sulfurPickupTicks
}

// bucketSulfurCube is Bucketable.bucketMobPickup with an empty bucket: the
// cube, its block and its age go into a sulfur cube bucket.
func (h *hub) bucketSulfurCube(players map[int32]*tracked, t *tracked, m *mob) bool {
	st := invStack{item: itemSulfurCubeBucket, count: 1,
		cube: cubeContent{item: m.cube.body.item, age: int32(-m.growLeft)}}
	if !m.baby {
		st.cube.age = 0
	}
	h.cubeSound(players, m, "minecraft:item.bucket.fill_sulfur_cube", 1, 1)
	if t.gamemode == gmCreative {
		// createFilledResult in creative keeps the empty bucket and adds the
		// full one when the inventory holds no identical stack — the cube is
		// never lost to a creative scoop.
		have := false
		for _, s := range t.inv.slots {
			if s.count > 0 && sameItemComponents(s, st) {
				have = true
				break
			}
		}
		if !have {
			changed, _ := t.inv.addStack(st)
			for _, sl := range changed {
				h.sendSlot(t, sl)
			}
		}
	} else {
		h.giveFilledStack(players, t, int32(t.p.heldSlot()), st)
	}
	h.advance(players, t, "filled_bucket", advMatch{item: itemSulfurCubeBucket})
	h.dropLeash(players, m, true)
	h.removeMob(players, m)
	return true
}

// releaseSulfurBucket is MobBucketItem.checkExtraContent for the sulfur cube
// bucket: its content is no fluid at all, so the cube comes out alone, with
// its block and its age, and is marked from a bucket (it never despawns).
func (h *hub) releaseSulfurBucket(players map[int32]*tracked, dim int, st invStack, x, y, z int) *mob {
	baby := st.cube.age < 0
	m := h.spawnSulfurCube(players, dim, float64(x)+0.5, float64(y), float64(z)+0.5, baby)
	h.playSoundDim(players, dim, "minecraft:item.bucket.empty_sulfur_cube", sndNeutral,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	h.vib(dim, freqEntityPlace, x, y, z, 0)
	if m == nil {
		return nil
	}
	m.fromBucket = true
	if baby {
		m.growLeft = int(-st.cube.age)
	}
	if st.cube.item != 0 && !m.baby {
		h.setCubeBody(players, m, invStack{item: st.cube.item, count: 1})
	}
	return m
}

// ---- spawning, growth, death --------------------------------------------

// sulfurGrewUp is ageBoundaryReached: a grown cube is size 2 again.
func (h *hub) sulfurGrewUp(players map[int32]*tracked, m *mob) {
	m.size = sulfurAdultSize
	m.applyCubeSize()
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(sulfurMeta(m)))
}

// splitSulfurCube is AbstractCubeMob.remove for a sulfur cube: a grown one
// that dies splits into two babies (none while its fuse burns), placed at
// the corners of half its width and half a block up, facing anywhere.
func (h *hub) splitSulfurCube(players map[int32]*tracked, m *mob) {
	if m.size <= 1 || m.cube.lit {
		return
	}
	off := m.box().w / 2
	for i := 0; i < 2; i++ {
		xd := (float64(i%2) - 0.5) * off
		zd := (float64(i/2) - 0.5) * off
		s := h.spawnMobIn(players, entitySulfurCube, m.dim, m.x+xd, m.y+0.5, m.z+zd)
		if s == nil {
			continue
		}
		h.applySpecies(players, s)
		h.initSulfurCube(s, true)
		s.yaw = h.rng.Float32() * 360
		s.customName = m.customName // ConversionParams keeps the name
		s.persistent = m.persistent
	}
}

// sulfurXP is getBaseExperienceReward: a baby is worth nothing, a grown
// cube 1-2.
func sulfurXP(m *mob, rng func(int) int) int {
	if m.baby {
		return 0
	}
	return 1 + rng(2)
}

// ---- hurt and knockback -------------------------------------------------

// sulfurHurtGate is SulfurCube.hurtServer's front half, on the mob (the one
// damage choke point): a lit cube is invulnerable; a cube with an explosive
// block is lit by fire and, short, by a blast (the hub lights it next tick);
// and a cube carrying any block takes nothing from what the immunity tag
// names. Reports whether the blow was absorbed.
func (m *mob) sulfurHurtGate(dt dmgType) bool {
	if m.etype != entitySulfurCube {
		return false
	}
	if m.cube.lit && !dt.has(tagBypassesInvulnerability) {
		return true // setPermanentlyInvulnerable(true) at priming
	}
	if !m.hasBody() {
		return false
	}
	if m.canExplode() {
		switch {
		case dt.has(tagIsFire):
			m.cube.prime = max(m.cube.prime, sulfurPrimeNormal)
		case dt.has(tagIsExplosion):
			m.cube.prime = sulfurPrimeImminent
		}
	}
	return dt.has(tagSulfurCubeWithBlockImmuneTo)
}

// cubeHitSource is the attacker a SulfurCube.knockback reads: where its eyes
// and feet are and where it is looking.
type cubeHitSource struct {
	eyeX, eyeY, eyeZ    float64
	lookX, lookY, lookZ float64
	feetX, feetY, feetZ float64
}

func playerHitSource(t *tracked) *cubeHitSource {
	lx, ly, lz := lookVector(t.yaw, t.pitch)
	return &cubeHitSource{t.x, t.y + playerEyeHeightStand, t.z, lx, ly, lz, t.x, t.y, t.z}
}

// rotate2 is Vec2.rotate.
func rotate2(x, y, ang float64) (float64, float64) {
	c, s := float64(float32(math.Cos(ang))), float64(float32(math.Sin(ang)))
	return x*c - y*s, y*c + x*s
}

func norm3(x, y, z float64) (float64, float64, float64) {
	n := math.Sqrt(x*x + y*y + z*z)
	if n < 1e-4 {
		return 0, 0, 0
	}
	return x / n, y / n, z / n
}

// clampedMap is Mth.clampedMap.
func clampedMap(v, fromMin, fromMax, toMin, toMax float64) float64 {
	t := (v - fromMin) / (fromMax - fromMin)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return toMin + t*(toMax-toMin)
}

// cubeKnockback is SulfurCube.knockback on a cube carrying a block. With an
// attacker it is its own model: the hit angle bends the direction (×1.6),
// how high on the cube the blow lands trades horizontal power for vertical
// (×0.5), and the height difference rotates the pair (×0.8), all scaled by
// √damage and the archetype's knockback resistance, with the archetype's
// hit sound. Without one it is LivingEntity.knockback on the ball's motion.
func (h *hub) cubeKnockback(players map[int32]*tracked, m *mob, power, xd, zd float64, src *cubeHitSource, damage float64, fromEffect bool) {
	c := &m.cube
	a := m.cubeArch()
	if a == nil {
		a = &sulfurDefaultArchetype
	}
	kbRes := m.kbResist()
	if src == nil {
		power *= 1 - kbRes
		if power <= 0 {
			return
		}
		for xd*xd+zd*zd < 1e-5 {
			xd = (h.rng.Float64() - h.rng.Float64()) * 0.01
			zd = (h.rng.Float64() - h.rng.Float64()) * 0.01
		}
		n := math.Hypot(xd, zd)
		c.vx = c.vx/2 - xd/n*power
		c.vz = c.vz/2 - zd/n*power
		if c.ground {
			c.vy = math.Min(0.4, c.vy/2+power)
		}
		return
	}
	hp, vp := f32(a.kbH), f32(a.kbV)
	ht := m.box().h
	cx, cy, cz := m.x, m.y+ht/2, m.z
	lx, ly, lz := norm3(src.lookX, src.lookY, src.lookZ)
	// applyHorizontalHitAngleScale
	ax, _, az := norm3(cx-src.eyeX, cy-src.eyeY, cz-src.eyeZ)
	diff := math.Atan2(lx*az-lz*ax, lx*ax+lz*az)
	nxd, nzd := rotate2(xd, zd, diff*1.6)
	// applyVerticalHitAnglePowerTransfer
	_, tyTop, _ := norm3(cx-src.eyeX, cy+ht/2-src.eyeY, cz-src.eyeZ)
	_, tyBot, _ := norm3(cx-src.eyeX, cy-ht/2-src.eyeY, cz-src.eyeZ)
	f := clampedMap(ly, tyTop, tyBot, -1, 1)
	ratio := math.Abs(f * 0.5)
	if f < 0 {
		ratio = -ratio
	}
	px, py := hp*(1-ratio), vp*(1+ratio)
	// applyVerticalPositionAnglePowerRotation
	dfx, dfy, dfz := m.x-src.feetX, m.y-src.feetY, m.z-src.feetZ
	vang := math.Atan2(-dfy, math.Hypot(dfx, dfz))
	rx, ry := rotate2(px, py, -vang*0.8)
	hr, vr := 0.0, 0.0
	if hp > 0 {
		hr = math.Abs(rx) / hp
	}
	if vp > 0 {
		vr = math.Abs(ry) / vp
	}
	if mr := math.Max(hr, vr); mr > 1 {
		rx, ry = rx/mr, ry/mr
	}
	mult := math.Sqrt(damage)
	if fromEffect {
		mult *= power * 0.25
	}
	hp, vp = rx*mult*(1-kbRes), ry*mult*(1-kbRes)
	hp = math.Max(-128, math.Min(128, hp*0.4))
	vp = math.Max(-128, math.Min(128, vp))
	if n := math.Hypot(nxd, nzd); n > 1e-9 {
		c.vx -= nxd / n * hp
		c.vz -= nzd / n * hp
	}
	c.vy += vp * 1.2
	h.cubeSound(players, m, a.hitSound(), 1, 1)
}

// cubeStruckByPlayer is a melee blow on a cube carrying a block: the blow
// is taken (or shrugged off) through the ordinary hurt, the default
// knockback runs through the cube's own model, and the extra knockback a
// sprint or the Knockback enchantment adds follows as an effect.
func (h *hub) cubeStruckByPlayer(players map[int32]*tracked, m *mob, t *tracked, dmg float64, dt dmgType) {
	m.hitByPlayer, m.lastAttacker = true, t.p.eid
	before := m.health
	m.hurtOf(dmg, 0, dt)
	if !dt.has(tagNoKnockback) {
		h.cubeKnockback(players, m, 0.4, t.x-m.x, t.z-m.z, playerHitSource(t), dmg, false)
	}
	extra := 0.5 * float64(heldStack(t).enchLvl(enchKnockback))
	if t.sprinting {
		extra += 0.5
	}
	if extra > 0 {
		yaw := float64(t.yaw) * math.Pi / 180
		h.cubeKnockback(players, m, extra, math.Sin(yaw), -math.Cos(yaw), playerHitSource(t), dmg, true)
	}
	if m.health < before {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: m.yaw})
	}
	if m.health <= 0 {
		h.killMob(players, m)
		h.advance(players, t, "player_killed_entity", advMatch{entity: advEntityName[m.etype]})
		h.incStat(t, attachproto.StatKilled, int32(m.etype), 1)
		h.incCustom(t, "mob_kills", 1)
	}
}

// cubeStruckByProjectile is a projectile on a cube carrying a block. A
// burning one lights an explosive cube; the knockback runs along the
// projectile's flight (calculateHorizontalHurtKnockbackDirection), through
// the cube's model when a player shot it. The projectile is spent.
func (h *hub) cubeStruckByProjectile(players map[int32]*tracked, a *arrowEntity, m *mob, dmg float64, dt dmgType) {
	if a.fire && m.canExplode() {
		h.primeSulfurCube(players, m, false)
	}
	if a.playerShot {
		m.hitByPlayer = true
	}
	before := m.health
	m.hurtOf(dmg, 0, dt)
	if !dt.has(tagNoKnockback) {
		var src *cubeHitSource
		if s := players[a.shooter]; s != nil && a.playerShot {
			src = playerHitSource(s)
		}
		h.cubeKnockback(players, m, 0.4, -a.vx, -a.vz, src, dmg, false)
	}
	if m.health < before {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: m.yaw})
	}
	if m.health <= 0 {
		h.killMob(players, m)
	}
}

// cubeBlastPush is ServerExplosion.hurtEntities' shove on a cube carrying a
// block: from its eyes, in three dimensions, less its explosion knockback
// resistance (which the slow archetypes raise).
func (h *hub) cubeBlastPush(m *mob, cx, cy, cz, impact float64) {
	ex, ey, ez := norm3(m.x-cx, m.y+mobEyeHeight(m)-cy, m.z-cz)
	k := impact * (1 - m.mobAttrs().Value(attr.ExplosionKnockbackResistance))
	m.cube.vx += ex * k
	m.cube.vy += ey * k
	m.cube.vz += ez * k
}

// ---- the fuse ------------------------------------------------------------

// primeSulfurCube is SulfurCube.primeTime: the explosive archetype's fuse,
// or a short one (getRandomShortFuse: fuse/8 + rand(fuse/4)) when a blast
// lit it. It becomes invulnerable and tells the clients how long the fuse
// is, so they flash it.
func (h *hub) primeSulfurCube(players map[int32]*tracked, m *mob, imminent bool) bool {
	a := m.cubeArch()
	if a == nil || !a.explosive || m.dying > 0 || m.cube.lit || !h.rules.TNTExplodes {
		return false
	}
	fuse := sulfurFuse
	if imminent {
		fuse = h.rng.Intn(max(1, sulfurFuse/4)) + sulfurFuse/8
	}
	m.cube.lit, m.cube.fuse, m.cube.maxFuse = true, fuse, fuse
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(sulfurMeta(m)))
	h.cubeSound(players, m, "minecraft:entity.tnt.primed", sulfurVolume(m), h.voicePitch(m))
	h.vibAt(m.dim, freqPrimeFuse, m.x, m.y, m.z, m.eid)
	return true
}

// sulfurExplode is tickFuse reaching zero: the cube is gone (no death, no
// split, no drops — the block went off) and the blast is a TNT-kind one of
// power 3, attributed to the cube, cratering only under mob griefing.
func (h *hub) sulfurExplode(players map[int32]*tracked, m *mob) {
	h.dropLeash(players, m, false)
	delete(h.mobs, m.eid)
	h.gridDirty()
	h.entityGone(players, m.dim, m.eid)
	h.shadowGoneAll(m.eid)
	if !h.rules.TNTExplodes {
		return
	}
	radius := sulfurBlastPower
	if !h.rules.MobGriefing {
		radius = 0 // ExplosionInteraction.NONE
	}
	h.explodeBy(players, m.dim, m.x, m.y+0.0625*m.box().h, m.z, radius, sulfurBlastPower, blastTNT, mobDisplayName(m.etype))
}

// ---- the tick ------------------------------------------------------------

// updateSulfurCubes runs every tick: the fuse, the redstone that lights it,
// the timers, and — for a cube carrying a block — its physics, the players
// pushing it and what a hot one burns; for an empty grown one, swallowing a
// swallowable block it is touching.
func (h *hub) updateSulfurCubes(players map[int32]*tracked) {
	var due []*mob
	for _, m := range h.mobs {
		if m.etype != entitySulfurCube || m.dying > 0 {
			continue
		}
		c := &m.cube
		if c.prime != 0 {
			h.primeSulfurCube(players, m, c.prime == sulfurPrimeImminent)
			c.prime = 0
		}
		if c.lit {
			if c.fuse > 0 {
				c.fuse--
			}
			if c.fuse == 0 {
				due = append(due, m)
				continue
			}
		}
		if m.canExplode() && h.cubePowered(m) {
			h.primeSulfurCube(players, m, false)
		}
		if c.pickup > 0 {
			c.pickup--
		}
		if c.pushCD > 0 {
			c.pushCD--
		}
		if m.hasBody() {
			h.cubePhysics(players, m)
			h.cubeTouch(players, m)
			continue
		}
		if !m.baby && c.pickup <= 0 && h.rules.MobGriefing {
			h.cubePickupScan(players, m)
		}
	}
	for _, m := range due {
		h.sulfurExplode(players, m)
	}
}

// cubePowered is getBestOwnOrNeighbourSignal at the cube's block.
func (h *hub) cubePowered(m *mob) bool {
	x, y, z := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	on := false
	h.inDim(m.dim, func() {
		s := h.rsWorld().At(x, y, z)
		on = h.bestNeighborSignal(x, y, z) > 0 || (h.isSignalSource(s) && h.ownSignal(x, y, z, s) > 0)
	})
	return on
}

// cubePickupScan is Mob.aiStep's pickup for an empty grown cube: a
// swallowable block within reach (its box, 1 wider each side) is taken, one
// of the stack. A block a player threw is thrown_item_picked_up_by_entity.
func (h *hub) cubePickupScan(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	b := m.box()
	half := b.w/2 + 1
	for eid, it := range h.items {
		if it.dim != m.dim || it.count <= 0 || now < it.noPickupUntil || !sulfurSwallowable[it.item] {
			continue
		}
		if math.Abs(it.x-m.x) > half || math.Abs(it.z-m.z) > half || it.y < m.y || it.y > m.y+b.h {
			continue
		}
		if t := players[it.thrower]; t != nil {
			h.advance(players, t, "thrown_item_picked_up_by_entity",
				advMatch{entity: advEntityName[m.etype], baby: m.baby, item: it.item})
		}
		st := it.stack()
		st.count = 1
		h.setCubeBody(players, m, st)
		h.cubeSound(players, m, "minecraft:entity.sulfur_cube.absorb", 1, 1)
		h.toTracking(players, eid, it.dim, it.x, it.z, attachproto.Collect{Collected: eid, Collector: m.eid, Count: 1})
		if it.count--; it.count <= 0 {
			delete(h.items, eid)
			h.entityGone(players, it.dim, eid)
		} else {
			h.refreshItemMeta(players, it)
		}
		return
	}
}

// ---- hopping (no block) --------------------------------------------------

// sulfurGoal is the empty cube's LOOK goals in priority order: a player
// tempting it (an adult by any swallowable block, a baby by a slime ball,
// within eight), then a swallowable block lying within eight, both steering
// it "aggressively" (a third of the jump delay). ok=false: wander.
func (h *hub) sulfurGoal(players map[int32]*tracked, m *mob) (gx, gz float64, ok bool) {
	want := sulfurSwallowable
	if m.baby {
		want = sulfurFood
	}
	best := sulfurTemptRange * sulfurTemptRange
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		if !want[heldStack(t).item] && !want[t.offhand.item] {
			continue
		}
		if d2 := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z); d2 < best {
			best, gx, gz, ok = d2, t.x, t.z, true
		}
	}
	if ok || m.baby || m.cube.pickup > 0 {
		return gx, gz, ok
	}
	now := h.tick.Load()
	b := m.box()
	best = math.Inf(1)
	for _, it := range h.items {
		if it.dim != m.dim || it.count <= 0 || now < it.noPickupUntil || !sulfurSwallowable[it.item] {
			continue
		}
		if math.Abs(it.x-m.x) > sulfurSearchRange+b.w/2 || math.Abs(it.z-m.z) > sulfurSearchRange+b.w/2 ||
			it.y < m.y-sulfurSearchRange || it.y > m.y+b.h+sulfurSearchRange {
			continue
		}
		if d2 := dist3sq(it.x, it.y, it.z, m.x, m.y, m.z); d2 < best {
			best, gx, gz, ok = d2, it.x, it.z, true
		}
	}
	return gx, gz, ok
}

// sulfurHop is CubeMobMoveControl for the empty cube: it travels only
// mid-bound, sits still between hops for rand(20)+10 ticks (a third of
// that when tempted or after a block), and a landing squishes.
func (h *hub) sulfurHop(players map[int32]*tracked, m *mob) {
	c := &m.cube
	if m.hopTicks > 0 {
		if m.hopTicks--; m.hopTicks == 0 && c.hopping {
			c.hopping = false
			name := "minecraft:entity.sulfur_cube.squish"
			if m.size <= 1 {
				name = "minecraft:entity.small_sulfur_cube.squish"
			}
			h.cubeSound(players, m, name, sulfurVolume(m), ((h.rng.Float32()-h.rng.Float32())*0.2+1)/0.8)
		}
		return
	}
	m.vx, m.vz = 0, 0
	gx, gz, aggressive := h.sulfurGoal(players, m)
	if c.dirNext -= mobMoveInterval; c.dirNext <= 0 {
		c.dirNext = 40 + h.rng.Intn(60)
		c.dirYaw = float64(h.rng.Intn(360))
	}
	if m.hopDelay--; m.hopDelay > 0 {
		return
	}
	delay := 10 + h.rng.Intn(20)
	if aggressive {
		delay /= 3
	}
	m.hopDelay = max(1, delay/mobMoveInterval)
	m.hopTicks = 4
	c.hopping = true
	var dx, dz float64
	if aggressive {
		dx, dz = gx-m.x, gz-m.z
	} else {
		yaw := c.dirYaw * math.Pi / 180
		dx, dz = -math.Sin(yaw), math.Cos(yaw)
	}
	if d := math.Hypot(dx, dz); d > 1e-6 {
		m.vx, m.vz = dx/d*m.moveSpeed(), dz/d*m.moveSpeed()
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Velocity{
		EID: m.eid, VX: m.vx / mobMoveInterval, VY: 0.42, VZ: m.vz / mobMoveInterval})
	name := "minecraft:entity.sulfur_cube.jump"
	if m.size <= 1 {
		name = "minecraft:entity.small_sulfur_cube.jump"
	}
	h.cubeSound(players, m, name, sulfurVolume(m), h.sulfurPitch(m))
}

// ---- the ball ------------------------------------------------------------

// modifiedFriction is LivingEntity.computeModifiedFriction.
func modifiedFriction(f, mod float64) float64 {
	return math.Max(0, math.Min(1, 1-(1-f)*mod))
}

// cubeClip is how far a box can move along one axis before a block stops
// it (Shapes.collide against full cells; cells it already overlaps are
// ignored, as vanilla ignores shapes it is inside).
func cubeClip(w *world.World, lo, hi [3]float64, axis int, d float64) float64 {
	if d == 0 {
		return 0
	}
	const eps = 1e-7
	cross := func(c int, fn func(x, y, z int) bool) bool {
		a, b := (axis+1)%3, (axis+2)%3
		for i := int(math.Floor(lo[a] + eps)); i <= int(math.Floor(hi[a]-eps)); i++ {
			for j := int(math.Floor(lo[b] + eps)); j <= int(math.Floor(hi[b]-eps)); j++ {
				var p [3]int
				p[axis], p[a], p[b] = c, i, j
				if fn(p[0], p[1], p[2]) {
					return true
				}
			}
		}
		return false
	}
	solid := func(x, y, z int) bool { return worldgen.Collides(w.At(x, y, z)) }
	if d > 0 {
		for c := int(math.Ceil(hi[axis] - eps)); c <= int(math.Ceil(hi[axis]+d-eps))-1; c++ {
			if cross(c, solid) {
				return math.Max(0, math.Min(d, float64(c)-hi[axis]))
			}
		}
		return d
	}
	for c := int(math.Floor(lo[axis]+eps)) - 1; c >= int(math.Floor(lo[axis]+d)); c-- {
		if cross(c, solid) {
			return math.Min(0, math.Max(d, float64(c+1)-lo[axis]))
		}
	}
	return d
}

// cubeMove is Entity.move for the ball: Y first, then the larger of X and Z
// (Direction.axisStepOrder), each clipped against the blocks.
func (h *hub) cubeMove(w *world.World, m *mob, dx, dy, dz float64) (mx, my, mz float64) {
	b := m.box()
	half := b.w / 2
	lo := [3]float64{m.x - half, m.y, m.z - half}
	hi := [3]float64{m.x + half, m.y + b.h, m.z + half}
	step := func(axis int, d float64) float64 {
		d = cubeClip(w, lo, hi, axis, d)
		lo[axis] += d
		hi[axis] += d
		return d
	}
	my = step(1, dy)
	if math.Abs(dx) < math.Abs(dz) {
		mz = step(2, dz)
		mx = step(0, dx)
	} else {
		mx = step(0, dx)
		mz = step(2, dz)
	}
	m.x, m.y, m.z = m.x+mx, m.y+my, m.z+mz
	return mx, my, mz
}

// cubeFluid is whether the ball sits in water or lava, and how deep
// (getFluidHeight: the fluid's surface above its feet).
func cubeFluid(w *world.World, m *mob) (water, lava bool, depth float64) {
	fx, fz := floorInt(m.x), floorInt(m.z)
	top := m.y + m.box().h
	for y := floorInt(m.y); float64(y) < top; y++ {
		s := w.At(fx, y, fz)
		switch {
		case worldgen.IsWater(s) || worldgen.HoldsWater(s):
			water = true
			depth = float64(y) + flowHeight(s) - m.y
		case worldgen.IsLava(s):
			lava = true
			depth = float64(y) + 8.0/9 - m.y
		}
	}
	return water, lava, depth
}

// cubePhysics is one tick of LivingEntity.travel for a cube carrying a
// block — an omnidirectional air mover whose friction, drag and bounce
// are its archetype's — and the restitution Entity.move applies after a
// collision. It is broadcast when it moved.
func (h *hub) cubePhysics(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	c := &m.cube
	// aiStep: motion under 0.003 on an axis stops.
	if math.Abs(c.vx) < 0.003 {
		c.vx = 0
	}
	if math.Abs(c.vy) < 0.003 {
		c.vy = 0
	}
	if math.Abs(c.vz) < 0.003 {
		c.vz = 0
	}
	attrs := m.mobAttrs()
	bounce := attrs.Value(attr.Bounciness)
	fricMod := attrs.Value(attr.FrictionModifier)
	dragMod := attrs.Value(attr.AirDragModifier)
	airDrag := modifiedFriction(0.91, dragMod)
	ox, oy, oz := m.x, m.y, m.z
	wasGround := c.ground
	water, lava, depth := cubeFluid(w, m)
	blockF := 1.0
	if wasGround && !water && !lava {
		blockF = modifiedFriction(blockFriction(w.At(floorInt(m.x), floorInt(m.y-0.5), floorInt(m.z))), fricMod)
	}
	falling := c.vy <= 0
	cur := [3]float64{c.vx, c.vy, c.vz}
	mx, my, mz := h.cubeMove(w, m, c.vx, c.vy, c.vz)
	colX := math.Abs(mx-cur[0]) > 1e-7
	colZ := math.Abs(mz-cur[2]) > 1e-7
	vert := cur[1] != my
	below := vert && cur[1] < 0
	c.ground = below
	// Entity.restituteMovementAfterCollisions.
	if colX {
		c.vx = -cur[0] * bounce
	}
	if colZ {
		c.vz = -cur[2] * bounce
	}
	if vert {
		r := bounce
		on := w.At(floorInt(m.x), floorInt(m.y-0.2), floorInt(m.z))
		if below {
			r = 0
			if -cur[1] > sulfurGravity && !isHoneyBlock(on) {
				r = math.Max(bounce, blockBounce(on))
			}
		}
		gc, drag := 0.0, 1.0
		if r > 0 {
			portion := my / cur[1]
			gc = portion * sulfurGravity
			drag = 1 + portion*(airDrag-1)
		}
		c.vy = (gc - cur[1]) * drag * r
	}
	sf := h.mobSpeedFactor(m) // Entity.move's block speed factor
	c.vx, c.vz = c.vx*sf, c.vz*sf
	switch {
	case water:
		// travelInWater: 0.8 all round, then the slow sink (-gravity/16).
		c.vx, c.vy, c.vz = c.vx*0.8, c.vy*0.8, c.vz*0.8
		if falling && math.Abs(c.vy-0.005) >= 0.003 && math.Abs(c.vy-sulfurGravity/16) < 0.003 {
			c.vy = -0.003
		} else {
			c.vy -= sulfurGravity / 16
		}
	case lava:
		c.vx, c.vy, c.vz = c.vx*0.5, c.vy*0.5, c.vz*0.5
		c.vy -= sulfurGravity / 4
	default:
		// travelInAir: block friction × air drag sideways, air drag (not
		// 0.98: an omnidirectional mover) on the fall.
		c.vy -= sulfurGravity
		c.vx *= blockF * airDrag
		c.vz *= blockF * airDrag
		c.vy *= airDrag
	}
	if (water || lava) && m.cubeArch() != nil && m.cubeArch().buoyant {
		// SulfurCube.travelInFluid: a buoyant block floats it, bobbing.
		vibe := 0.2 * math.Sin(float64(h.tick.Load()-m.spawnTick)*0.4)
		if imm := depth - m.box().h*0.2 + vibe; imm > 0 {
			c.vy += math.Min(1, imm) * 0.04
		}
	}
	if c.ground && !wasGround {
		// AbstractCubeMob.tick's landing: a carried block lands with a bounce.
		name := "minecraft:entity.sulfur_cube.bounce"
		if m.size <= 1 {
			name = "minecraft:entity.small_sulfur_cube.squish"
		}
		h.cubeSound(players, m, name, sulfurVolume(m), ((h.rng.Float32()-h.rng.Float32())*0.2+1)/0.8)
	}
	// LookControl for a cube with a block: square to the nearest quarter.
	m.yaw -= float32(wrapDegrees90(float64(m.yaw)))
	if m.x != ox || m.y != oy || m.z != oz {
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, c.ground))
	}
}

// wrapDegrees90 is Mth.wrapDegrees90.
func wrapDegrees90(a float64) float64 {
	a = math.Mod(a, 90)
	if a >= 45 {
		a -= 90
	}
	if a < -45 {
		a += 90
	}
	return a
}

// blockBounce is Block.getBounceRestitution: a slime block returns all of
// it, a bed (and a shelf mushroom) three quarters.
func blockBounce(s uint32) float64 {
	switch {
	case isSlimeBlock(s):
		return 1
	case isBedBlock(s) || inRanges2(s, shelfMushroom):
		return 0.75
	}
	return 0
}

var shelfMushroom = blockRangeOK("shelf_mushroom")

// cubeTouch is the ball meeting players: playerTouch's push (the player's
// box, one wider and half higher, overlapping the cube; within 1.3
// sideways and overlapping in height) moves it by how fast they walk into
// it, and a hot block burns what it touches — players and mobs in its box.
func (h *hub) cubeTouch(players map[int32]*tracked, m *mob) {
	c := &m.cube
	a := m.cubeArch()
	if a == nil {
		a = &sulfurDefaultArchetype
	}
	b := m.box()
	top := m.y + b.h
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		// Player.touch: its box inflated by (1, 0.5, 1).
		if math.Abs(t.x-m.x) > 0.3+1+b.w/2 || math.Abs(t.z-m.z) > 0.3+1+b.w/2 ||
			t.y-0.5 > top || t.y+1.8+0.5 < m.y {
			continue
		}
		dx, dz := m.x-t.x, m.z-t.z
		if math.Hypot(dx, dz) >= sulfurPushDist || t.y > top || t.y+1.8 <= m.y {
			continue
		}
		kb := math.Max(0, 1-m.kbResist())
		n := math.Hypot(dx, dz)
		if n > 1e-9 {
			dx, dz = dx/n*kb, dz/n*kb
		}
		scale := 0.3
		if t.ridingEID != 0 {
			scale = 0.16
		}
		kx, ky, kz := h.knownMove(t)
		speed := math.Min(0.5, math.Sqrt(kx*kx+ky*ky+kz*kz)*2*scale)
		vy := 0.0
		if c.ground {
			vy = kb * f32(0.3)
		}
		px, py, pz := dx*speed, vy*speed, dz*speed
		th := f32(a.pushThreshold)
		if px*px+py*py+pz*pz > th*th && c.pushCD <= 0 {
			c.pushCD = int(a.pushCooldown * 20)
			h.cubeSound(players, m, a.pushSound(), 1, 1)
		}
		c.vx, c.vy, c.vz = c.vx+px, c.vy+py, c.vz+pz
		if a.hot {
			h.cubeBurn(players, m, t)
		}
	}
	if !a.hot {
		return
	}
	// LivingEntity.pushEntities → doPush → applyContactDamage.
	h.grid().nearby(m.dim, m.x, m.z, 2, func(o *mob) {
		if o == m || o.dying > 0 {
			return
		}
		ob := o.box()
		if math.Abs(o.x-m.x) > (ob.w+b.w)/2 || math.Abs(o.z-m.z) > (ob.w+b.w)/2 || o.y > top || o.y+ob.h < m.y {
			return
		}
		h.hurtMobOf(players, o, sulfurHotDamage, dtSulfurCubeHot)
	})
	for _, t := range players {
		if t.dim != m.dim || t.dead {
			continue
		}
		if math.Abs(t.x-m.x) <= 0.3+b.w/2 && math.Abs(t.z-m.z) <= 0.3+b.w/2 && t.y <= top && t.y+1.8 >= m.y {
			h.cubeBurn(players, m, t)
		}
	}
}

// cubeBurn is the hot archetype's contact damage on a player: 1 of
// sulfur_cube_hot, from the cube (a player is always told who burnt them).
func (h *hub) cubeBurn(players map[int32]*tracked, m *mob, t *tracked) {
	h.hurtFrom(players, t, sulfurHotDamage, dtSulfurCubeHot,
		deathCause{by: mobDisplayName(m.etype)}, fromMob(m.x, m.z))
}
