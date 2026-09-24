package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// cubeFixture is a hub with a stone floor under y=180 around the origin, a
// survival player beside it, and a grown sulfur cube standing on the floor.
func cubeFixture(t *testing.T) (*hub, map[int32]*tracked, *tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.adv = advState{}
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	m := h.spawnSulfurCube(players, 0, 2.5, 180, 0.5, false)
	if m == nil {
		t.Fatal("no sulfur cube")
	}
	return h, players, pl, m
}

func holding(pl *tracked, item int32, n int) {
	pl.p.held = 0
	pl.inv.slots[0] = invStack{item: item, count: n}
}

// SulfurCube: size 2 grown (8 health, 0.4 speed), size 1 a baby (4
// health), never hostile, no attack, a MONSTER for the spawn census.
func TestSulfurCubeStats(t *testing.T) {
	h, players, _, m := cubeFixture(t)
	if m.size != 2 || m.maxHP() != 8 || m.health != 8 || m.baby {
		t.Errorf("grown cube: size %d, health %d/%d, baby %v; want 2, 8/8, false", m.size, m.health, m.maxHP(), m.baby)
	}
	if !closeTo(m.moveSpeed(), slimeSpeed(2)) {
		t.Errorf("speed %v, want 0.2+0.1×2 (%v per step)", m.moveSpeed(), slimeSpeed(2))
	}
	if m.hostile || m.attackDamage() != 0 || isEnemyType(m.etype) {
		t.Errorf("hostile %v, attack %v, enemy %v: a sulfur cube never attacks", m.hostile, m.attackDamage(), isEnemyType(m.etype))
	}
	if mobSpawnCategory(m) != catMonster {
		t.Error("a sulfur cube counts as a monster")
	}
	b := h.spawnSulfurCube(players, 0, 4.5, 180, 0.5, true)
	if b.size != 1 || b.maxHP() != 4 || !b.baby || b.growLeft != growUpTicks {
		t.Errorf("baby: size %d, health %d, baby %v, grow %d", b.size, b.maxHP(), b.baby, b.growLeft)
	}
	if got := m.box(); !closeTo(got.w, 0.98) || !closeTo(b.box().w, 0.49) {
		t.Errorf("boxes %v / %v, want 0.98 and 0.49", got.w, b.box().w)
	}
	if xp := sulfurXP(b, func(int) int { return 1 }); xp != 0 {
		t.Errorf("a baby pays %d experience, want none", xp)
	}
	if xp := sulfurXP(m, func(n int) int { return n - 1 }); xp != 2 {
		t.Errorf("a grown cube pays up to %d, want 1-2", xp)
	}
}

// Every swallowable block belongs to exactly one archetype, and the two
// with side effects are TNT and the magma block.
func TestSulfurArchetypeTags(t *testing.T) {
	if len(sulfurSwallowable) < 300 {
		t.Fatalf("only %d swallowable items", len(sulfurSwallowable))
	}
	for item := range sulfurSwallowable {
		n := 0
		for i := range sulfurArchetypes {
			if sulfurArchetypes[i].items[item] {
				n++
			}
		}
		if n != 1 {
			t.Errorf("item %d is in %d archetypes, want one", item, n)
		}
	}
	if a := sulfurArchetypeFor(itemTNTBlock); a == nil || !a.explosive {
		t.Error("TNT is not the explosive archetype")
	}
	if a := sulfurArchetypeFor(itemByName["magma_block"]); a == nil || !a.hot {
		t.Error("a magma block is not the hot archetype")
	}
	if !sulfurFood[itemSlimeball] || len(sulfurFood) != 1 {
		t.Error("#sulfur_cube_food is the slime ball alone")
	}
}

// A swallowed block's archetype becomes attribute modifiers, as vanilla
// names and computes them, and they come off with the block.
func TestSulfurArchetypeModifiers(t *testing.T) {
	h, players, _, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemByName["dirt"], count: 1}) // regular
	a := m.mobAttrs()
	if got := a.Value(attr.KnockbackResistance); !closeTo(got, -1) {
		t.Errorf("regular knockback resistance %v, want -1 (twice the shove)", got)
	}
	if got := a.Value(attr.ExplosionKnockbackResistance); got != 0 {
		t.Errorf("explosion knockback resistance %v, want clamped to 0", got)
	}
	if got := a.Value(attr.Bounciness); !closeTo(got, 0.5) {
		t.Errorf("bounciness %v, want 0.5", got)
	}
	if got := a.Value(attr.FrictionModifier); !closeTo(got, float64(float32(0.3))) {
		t.Errorf("friction modifier %v, want 0.3f", got)
	}
	if got := a.Value(attr.AirDragModifier); !closeTo(got, float64(float32(0.1))) {
		t.Errorf("air drag modifier %v, want 0.1f", got)
	}
	if !a.Get(attr.Bounciness).HasModifier("minecraft:regular_add_bounciness") ||
		!a.Get(attr.FrictionModifier).HasModifier("minecraft:regular_mul_friction_modifier") {
		t.Error("the modifiers are not named as vanilla names them")
	}
	h.setCubeBody(players, m, invStack{item: itemByName["mycelium"], count: 1}) // slow_sliding
	if got := a.Value(attr.KnockbackResistance); !closeTo(got, float64(float32(0.8))) {
		t.Errorf("slow sliding knockback resistance %v, want 0.8", got)
	}
	if a.Get(attr.Bounciness).HasModifier("minecraft:regular_add_bounciness") {
		t.Error("the regular modifiers stayed after the block changed")
	}
	h.setCubeBody(players, m, invStack{})
	if a.Value(attr.KnockbackResistance) != 0 || a.Value(attr.Bounciness) != 0 || a.Value(attr.FrictionModifier) != 1 {
		t.Error("an empty cube kept its archetype's modifiers")
	}
}

// Giving a grown cube TNT from the hand: it swallows it, the TNT is spent,
// it stops being a creature, and husbandry/uh_oh is granted.
func TestSulfurCubeTakesTNTFromHand(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	holding(pl, itemTNTBlock, 2)
	if !h.interactMob(players, pl, m, false) {
		t.Fatal("the cube did not take the TNT")
	}
	if m.cube.body.item != itemTNTBlock || m.cube.body.count != 1 {
		t.Fatalf("body %+v, want one TNT", m.cube.body)
	}
	if pl.inv.slots[0].count != 1 {
		t.Errorf("held count %d, want one TNT spent", pl.inv.slots[0].count)
	}
	if !pl.adv.done(advByID["minecraft:husbandry/uh_oh"]) {
		t.Error("husbandry/uh_oh not granted for giving TNT")
	}
	// The same block again is refused; a different one swaps and pops the old.
	holding(pl, itemTNTBlock, 1)
	if h.trySulfurCube(players, pl, m) {
		t.Error("a second TNT was taken")
	}
	holding(pl, itemByName["dirt"], 1)
	if !h.trySulfurCube(players, pl, m) || m.cube.body.item != itemByName["dirt"] {
		t.Fatal("dirt did not replace the TNT")
	}
	if droppedItem(h, itemTNTBlock) == nil {
		t.Error("the TNT was not spat out")
	}
	// A baby never swallows (and never grants uh_oh).
	b := h.spawnSulfurCube(players, 0, 4.5, 180, 0.5, true)
	holding(pl, itemTNTBlock, 1)
	if h.trySulfurCube(players, pl, b) || b.hasBody() {
		t.Error("a baby swallowed a block")
	}
}

// uh_oh's other criterion: a cube picking up TNT a player threw.
func TestSulfurCubePicksUpThrownTNT(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	it := h.spawnItemAt(players, 0, itemTNTBlock, 3, m.x+0.8, 180, m.z, 0, 0, 0)
	it.thrower, it.noPickupUntil = pl.p.eid, 0
	h.updateSulfurCubes(players)
	if m.cube.body.item != itemTNTBlock {
		t.Fatal("the cube did not swallow the TNT beside it")
	}
	if it.count != 2 {
		t.Errorf("stack left %d, want one taken", it.count)
	}
	if !pl.adv.done(advByID["minecraft:husbandry/uh_oh"]) {
		t.Error("husbandry/uh_oh not granted for thrown TNT")
	}
	// A cube carrying a block picks nothing more up.
	dirt := h.spawnItemAt(players, 0, itemByName["dirt"], 1, m.x, m.y, m.z, 0, 0, 0)
	dirt.noPickupUntil = 0
	h.updateSulfurCubes(players)
	if m.cube.body.item != itemTNTBlock || h.items[dirt.eid] == nil {
		t.Error("a full cube swallowed again")
	}
}

// Shears pop the block out, and for five seconds it will not swallow.
func TestSulfurCubeShears(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemByName["oak_planks"], count: 1})
	holding(pl, itemShears, 1)
	if !h.interactMob(players, pl, m, false) {
		t.Fatal("the shears did nothing")
	}
	if m.hasBody() || droppedItem(h, itemByName["oak_planks"]) == nil {
		t.Fatal("the block was not ejected")
	}
	if m.cube.pickup != sulfurPickupTicks {
		t.Errorf("pickup timer %d, want %d", m.cube.pickup, sulfurPickupTicks)
	}
	it := droppedItem(h, itemByName["oak_planks"])
	it.x, it.y, it.z, it.noPickupUntil = m.x, m.y, m.z, 0
	h.updateSulfurCubes(players)
	if m.hasBody() {
		t.Error("it swallowed again inside the pickup timer")
	}
	m.cube.pickup = 0
	h.updateSulfurCubes(players)
	if !m.hasBody() {
		t.Error("it did not swallow once the timer ran out")
	}
}

// The empty cube's goals: a player holding a swallowable block within
// eight tempts a grown cube (a baby only by a slime ball), else a block on
// the ground within eight draws it, and it hops toward either.
func TestSulfurCubeGoals(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	holding(pl, itemByName["dirt"], 1)
	if gx, _, ok := h.sulfurGoal(players, m); !ok || gx != pl.x {
		t.Errorf("tempt: goal %v ok %v, want the player at %v", gx, ok, pl.x)
	}
	b := h.spawnSulfurCube(players, 0, 4.5, 180, 0.5, true)
	if _, _, ok := h.sulfurGoal(players, b); ok {
		t.Error("a baby was tempted by dirt")
	}
	holding(pl, itemSlimeball, 1)
	if _, _, ok := h.sulfurGoal(players, b); !ok {
		t.Error("a baby was not tempted by a slime ball")
	}
	holding(pl, 0, 0)
	it := h.spawnItemAt(players, 0, itemByName["dirt"], 1, m.x+6, 180, m.z, 0, 0, 0)
	it.noPickupUntil = 0
	if gx, _, ok := h.sulfurGoal(players, m); !ok || gx != it.x {
		t.Errorf("search: goal %v ok %v, want the block at %v", gx, ok, it.x)
	}
	m.hopDelay, m.hopTicks = 1, 0
	h.sulfurHop(players, m)
	if m.vx <= 0 || m.hopTicks == 0 {
		t.Errorf("hop vx %v ticks %d: it should bound toward the block", m.vx, m.hopTicks)
	}
}

// A slime ball ages a baby by a tenth of what is left.
func TestSulfurCubeBabyFood(t *testing.T) {
	h, players, pl, _ := cubeFixture(t)
	b := h.spawnSulfurCube(players, 0, 4.5, 180, 0.5, true)
	holding(pl, itemSlimeball, 1)
	if !h.interactMob(players, pl, b, false) {
		t.Fatal("the baby did not eat the slime ball")
	}
	if b.growLeft != growUpTicks-2400 {
		t.Errorf("grow left %d, want %d", b.growLeft, growUpTicks-2400)
	}
	b.growLeft = 1
	h.updateBreeding(players)
	if b.baby || b.size != 2 || b.maxHP() != 8 {
		t.Errorf("grown up: baby %v size %d health %d, want false 2 8", b.baby, b.size, b.maxHP())
	}
}

// Flint and steel lights an explosive cube: invulnerable, the MAX_FUSE
// synced, and 120 ticks later it goes off — gone, unsplit, no drops.
func TestSulfurCubeFuse(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemTNTBlock, count: 1})
	holding(pl, itemFlintAndSteel, 1)
	if !h.interactMob(players, pl, m, false) || !m.cube.lit || m.cube.fuse != sulfurFuse || m.cube.maxFuse != sulfurFuse {
		t.Fatalf("lit %v fuse %d max %d, want lit at 120", m.cube.lit, m.cube.fuse, m.cube.maxFuse)
	}
	hp := m.health
	m.hurtOf(100, 0, dtLava)
	if m.health != hp {
		t.Error("a lit cube took damage")
	}
	for i := 0; i < sulfurFuse-1; i++ {
		h.updateSulfurCubes(players)
	}
	if h.mobs[m.eid] == nil {
		t.Fatal("it went off early")
	}
	h.updateSulfurCubes(players)
	if h.mobs[m.eid] != nil {
		t.Fatal("it did not go off at 120")
	}
	for _, o := range h.mobs {
		if o.etype == entitySulfurCube {
			t.Error("an exploded cube split")
		}
	}
	if droppedItem(h, itemTNTBlock) != nil {
		t.Error("the TNT dropped from an exploded cube")
	}
}

// Fire lights it at the full fuse, a blast lights it short
// (fuse/8 + rand(fuse/4): 15-44), and a redstone signal lights it too.
func TestSulfurCubeLitByFireBlastAndRedstone(t *testing.T) {
	h, players, _, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemTNTBlock, count: 1})
	m.hurtKind(1, dtInFire)
	h.updateSulfurCubes(players)
	if !m.cube.lit || m.cube.maxFuse != sulfurFuse {
		t.Errorf("fire: lit %v max %d, want 120", m.cube.lit, m.cube.maxFuse)
	}

	h2, players2, _, m2 := cubeFixture(t)
	h2.setCubeBody(players2, m2, invStack{item: itemTNTBlock, count: 1})
	hp := m2.health
	h2.explodeHurt(players2, 0, m2.x+1, m2.y, m2.z, 4, dtExplosion, deathCause{})
	if m2.health != hp {
		t.Error("the blast hurt a cube carrying a block")
	}
	h2.updateSulfurCubes(players2)
	if !m2.cube.lit || m2.cube.maxFuse < 15 || m2.cube.maxFuse > 44 {
		t.Errorf("blast: lit %v max %d, want a short fuse 15-44", m2.cube.lit, m2.cube.maxFuse)
	}

	h3, players3, _, m3 := cubeFixture(t)
	h3.setCubeBody(players3, m3, invStack{item: itemTNTBlock, count: 1})
	h3.world.SetBlock(floorInt(m3.x)+1, floorInt(m3.y), floorInt(m3.z), worldgen.BlockBase("redstone_block"))
	h3.updateSulfurCubes(players3)
	if !m3.cube.lit {
		t.Error("a redstone block beside it did not light it")
	}
}

// A punch sends a cube carrying a block away from the player, harder for a
// regular block (knockback resistance -1) than for a slow one.
func TestSulfurCubeKnockback(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemByName["dirt"], count: 1})
	pl.yaw, pl.pitch = -90, 0 // looking along +x, at the cube
	hp := m.health
	h.cubeStruckByPlayer(players, m, pl, 1, dtPlayerAttack)
	if m.health != hp {
		t.Error("a punch hurt a cube carrying a block")
	}
	if m.cube.vx <= 0 {
		t.Fatalf("vx %v: the cube should fly away from the player (+x)", m.cube.vx)
	}
	regular := m.cube.vx

	h2, players2, pl2, m2 := cubeFixture(t)
	h2.setCubeBody(players2, m2, invStack{item: itemByName["soul_sand"], count: 1}) // high resistance
	pl2.yaw = -90
	h2.cubeStruckByPlayer(players2, m2, pl2, 1, dtPlayerAttack)
	if m2.cube.vx <= 0 || m2.cube.vx >= regular {
		t.Errorf("high resistance vx %v, want less than regular's %v", m2.cube.vx, regular)
	}
	// A level look passes over a cube at the player's feet, so the vertical
	// hit angle moves half the power to the horizontal (×1.5 / ×0.5), and the
	// ratio cap scales both back by 1.5: 0.4125 × √1 × (1-(-1)) × 0.4 = 0.33
	// sideways, and a lift of 0.09 × 0.5/1.5 × 2 × 1.2.
	if want := float64(float32(0.4125)) * 2 * 0.4; math.Abs(regular-want) > 1e-6 {
		t.Errorf("regular shove %v, want %v", regular, want)
	}
	if want := float64(float32(0.09)) * 0.5 / 1.5 * 2 * 1.2; math.Abs(m.cube.vy-want) > 1e-6 {
		t.Errorf("lift %v, want %v", m.cube.vy, want)
	}
}

// The ball falls, bounces by its archetype, and comes to rest on the floor.
func TestSulfurCubeBallPhysics(t *testing.T) {
	h, players, _, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemByName["oak_planks"], count: 1}) // bouncy: 0.9
	m.y = 183
	m.cube.ground = false
	bounced := false
	for i := 0; i < 400; i++ {
		was := m.cube.vy
		h.updateSulfurCubes(players)
		if was < 0 && m.cube.vy > 0 {
			bounced = true
		}
		if m.y < 180-1e-9 {
			t.Fatalf("fell through the floor to %v", m.y)
		}
	}
	if !bounced {
		t.Error("a bouncy cube dropped three blocks did not bounce")
	}
	if !m.cube.ground || math.Abs(m.y-180) > 1e-6 {
		t.Errorf("at rest: ground %v y %v, want on the floor at 180", m.cube.ground, m.y)
	}

	// Sliding: the same shove carries a fast-sliding cube further than a sticky one.
	slide := func(item string) float64 {
		h, players, _, m := cubeFixture(t)
		h.setCubeBody(players, m, invStack{item: itemByName[item], count: 1})
		x0 := m.x
		m.cube.vx = 0.5
		for i := 0; i < 60; i++ {
			h.updateSulfurCubes(players)
		}
		return m.x - x0
	}
	if ice, sticky := slide("packed_ice"), slide("honeycomb_block"); ice <= sticky {
		t.Errorf("fast sliding went %v, sticky %v", ice, sticky)
	}
}

// A walking player pushes a cube carrying a block; a hot one burns them.
func TestSulfurCubePlayerPushAndHot(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemByName["magma_block"], count: 1})
	pl.x = m.x - 0.9
	h.tick.Store(100) // a known movement needs a tick to have been reported on
	h.noteKnownMove(pl, 0.2, 0, 0)
	hp := pl.health
	h.cubeTouch(players, m)
	if m.cube.vx <= 0 {
		t.Errorf("vx %v: a player walking in should push it along +x", m.cube.vx)
	}
	if pl.health >= hp {
		t.Error("a hot cube did not burn the player touching it")
	}
	if pl.lastCause.dt != dtSulfurCubeHot {
		t.Errorf("death cause %v, want sulfur_cube_hot", dmgTypeNames[pl.lastCause.dt])
	}
	z := h.spawnMob(players, entityZombie, m.x, m.y, m.z)
	zhp := z.health
	h.cubeTouch(players, m)
	if z.health >= zhp {
		t.Error("a hot cube did not burn the zombie inside it")
	}
}

// An empty bucket scoops the cube with its block; pouring it out gives the
// cube back, block and all, flagged from a bucket.
func TestSulfurCubeBucket(t *testing.T) {
	h, players, pl, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemByName["dirt"], count: 1})
	holding(pl, itemBucket, 1)
	if !h.interactMob(players, pl, m, false) {
		t.Fatal("the bucket did not scoop the cube")
	}
	st := pl.inv.slots[0]
	if st.item != itemSulfurCubeBucket || st.cube.item != itemByName["dirt"] {
		t.Fatalf("held %+v, want a sulfur cube bucket with dirt", st)
	}
	if h.mobs[m.eid] != nil {
		t.Error("the scooped cube is still in the world")
	}
	if row := packStack(st); unpackStack(row).cube != st.cube {
		t.Error("the bucket's cube does not survive a save")
	}
	h.bucketEmpty(players, pl, 0, 3, 180, 3, 3, 179, 3)
	var out *mob
	for _, o := range h.mobs {
		if o.etype == entitySulfurCube {
			out = o
		}
	}
	if out == nil || !out.fromBucket || out.cube.body.item != itemByName["dirt"] {
		t.Fatalf("released %+v", out)
	}
	if pl.inv.slots[0].item != itemBucket {
		t.Error("the bucket was not left empty")
	}
	if h.world.At(3, 180, 3) != worldgen.Air {
		t.Error("the sulfur cube bucket poured something")
	}
	if !h.requiresCustomPersistence(out) {
		t.Error("a cube carrying a block must never despawn")
	}
}

// A grown cube dies into two babies; its block drops; a baby just dies.
func TestSulfurCubeSplitAndDrops(t *testing.T) {
	h, players, _, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemByName["dirt"], count: 1})
	m.hurtKind(100, dtGenericKill)
	h.killMob(players, m)
	h.despawnMob(players, m)
	babies := 0
	for _, o := range h.mobs {
		if o.etype == entitySulfurCube {
			if !o.baby || o.size != 1 || o.maxHP() != 4 {
				t.Errorf("split: baby %v size %d health %d", o.baby, o.size, o.maxHP())
			}
			babies++
		}
	}
	if babies != 2 {
		t.Errorf("%d babies, want 2", babies)
	}
	if droppedItem(h, itemByName["dirt"]) == nil {
		t.Error("the swallowed block did not drop")
	}
	if ds, ok := h.evalEntityLoot(int32(entitySulfurCube), lootCtx{rng: h.rng.Intn, randf: h.rng.Float64}); !ok || len(ds) != 0 {
		t.Errorf("loot %v ok %v: the sulfur cube's table is empty", ds, ok)
	}
}

// The sulfur caves pool names it (weight 100, packs of 2-4), and its spawn
// rule takes any light; a natural spawn is an unhostile grown cube.
func TestSulfurCubeNaturalSpawn(t *testing.T) {
	pool, ok := biomeSpawnPool("minecraft:sulfur_caves", catMonster)
	if !ok {
		t.Fatal("no sulfur caves pool")
	}
	found := false
	for _, e := range pool {
		if e.etype == entitySulfurCube && e.weight == 100 && e.min == 2 && e.max == 4 {
			found = true
		}
	}
	if !found {
		t.Fatalf("sulfur caves monsters %+v lack the sulfur cube", pool)
	}
	h, players, _, _ := cubeFixture(t)
	if !h.spawnRulesOK(0, catMonster, entitySulfurCube, 0, 180, 0, 15, 15) {
		t.Error("checkSulfurCubeSpawnRules is always true, full daylight included")
	}
	h.spawnNatural(players, 0, catMonster, entitySulfurCube, 6, 180, 6)
	var n *mob
	for _, o := range h.mobs {
		if o.etype == entitySulfurCube && floorInt(o.x) == 6 {
			n = o
		}
	}
	if n == nil || n.hostile || n.size != 2 {
		t.Fatalf("natural spawn %+v", n)
	}
}

// The wire: a 26.2 client knows the sulfur cube (26.2 registered it as
// entity 130), so it is shifted, not substituted; and its synced fields ride
// at their 26.x indices (18 size, 19 MAX_FUSE, 20 FROM_BUCKET), which the
// gateway's cube shift (a VarInt at 16 only) leaves alone.
func TestSulfurCubeOnTheWire(t *testing.T) {
	canon := int32(entitySulfurCube)
	if protocol.IsSubstituted(776, canon) || protocol.IsSubstituted(777, canon) {
		t.Error("the sulfur cube is substituted for a client that has it")
	}
	if got := protocol.RemapID(protocol.RegEntity, 776, canon); got != 130 {
		t.Errorf("26.2 entity id %d, want 130", got)
	}
	if got := protocol.RemapID(protocol.RegEntity, 777, canon); got != canon {
		t.Errorf("26.3 entity id %d, want %d", got, canon)
	}
	m := &mob{eid: 7, etype: entitySulfurCube, size: 2, fromBucket: true}
	m.cube.maxFuse = 120
	body := sulfurMeta(m)
	for _, v := range []int32{776, 777} {
		if got := protocol.ShiftCubeMobMeta(v, body); string(got) != string(body) {
			t.Errorf("proto %d: the cube shift rewrote the sulfur cube's data", v)
		}
	}
	want := []byte{7, 18, metaTypeInt, 2, 19, metaTypeInt, 120, 20, metaTypeBool, 1, itemMetaEnd}
	if string(body) != string(want) {
		t.Errorf("metadata % x, want % x", body, want)
	}
}

// A cube's block, its lit fuse and its pickup timer survive a restart.
func TestSulfurCubePersists(t *testing.T) {
	h, players, _, m := cubeFixture(t)
	h.setCubeBody(players, m, invStack{item: itemTNTBlock, count: 1})
	h.primeSulfurCube(players, m, false)
	m.cube.fuse = 33
	sm := toSavedMob(m)
	h2, players2, _, _ := cubeFixture(t)
	h2.reloading = true
	r := h2.reloadMob(players2, &sm)
	h2.reloading = false
	if r == nil || r.cube.body.item != itemTNTBlock || !r.cube.lit || r.cube.fuse != 33 || r.cube.maxFuse != sulfurFuse {
		t.Fatalf("reloaded %+v", r.cube)
	}
	if r.size != 2 || r.maxHP() != 8 {
		t.Errorf("reloaded size %d health %d", r.size, r.maxHP())
	}
	if !r.mobAttrs().Get(attr.Bounciness).HasModifier("minecraft:explosive_add_bounciness") {
		t.Error("the archetype's modifiers did not come back")
	}
}
