package server

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Item.useOn overriders the block-use path did not carry: shears trimming a
// growing plant, a water bottle turning dirt to mud, a spawn egg retargeting
// a spawner, an end crystal set on obsidian or bedrock, and a firework rocket
// lit against a block (the shovel's campfire dowsing already lived on the
// placement path).

var (
	rootedDirt     = worldgen.BlockBase("rooted_dirt")
	obsidianBase   = worldgen.BlockBase("obsidian")
	spawnerBlock   = worldgen.BlockBase("spawner")
	itemEndCrystal = itemByName["end_crystal"]
)

// entityRegistryName is the registry name of an entity type (the rules file
// keys spawner choices by name, which survives an id renumbering).
func entityRegistryName(et int) string { return entityNameByID[et] }

// faceDelta is the unit offset of a clicked face (vanilla direction ids:
// down, up, north, south, west, east).
func faceDelta(face int32) (int, int, int) {
	switch face {
	case 0:
		return 0, -1, 0
	case 1:
		return 0, 1, 0
	case 2:
		return 0, 0, -1
	case 3:
		return 0, 0, 1
	case 4:
		return -1, 0, 0
	}
	return 1, 0, 0
}

type evTrimPlant struct {
	eid     int32
	x, y, z int
	off     bool // used from the offhand (the packet's InteractionHand)
}
type evMudBottle struct {
	eid     int32
	x, y, z int
	off     bool // used from the offhand (the packet's InteractionHand)
}
type evEggSpawner struct {
	eid     int32
	x, y, z int
	off     bool // used from the offhand (the packet's InteractionHand)
}

// evSpawnEgg is SpawnEggItem.useOn on an ordinary block: the mob appears on
// the face that was clicked.
type evSpawnEgg struct {
	eid     int32
	x, y, z int
	face    int32
	off     bool // used from the offhand (the packet's InteractionHand)
}
type evPlaceCrystal struct {
	eid     int32
	x, y, z int
	off     bool // used from the offhand (the packet's InteractionHand)
}
type evPlaceRocket struct {
	eid        int32
	x, y, z    int
	face       int32
	cx, cy, cz float32
	off        bool // used from the offhand (the packet's InteractionHand)
}

func (evTrimPlant) isHubEvent()    {}
func (evMudBottle) isHubEvent()    {}
func (evEggSpawner) isHubEvent()   {}
func (evSpawnEgg) isHubEvent()     {}
func (evPlaceCrystal) isHubEvent() {}
func (evPlaceRocket) isHubEvent()  {}

// growingPlantOf finds the species a head state belongs to.
func growingPlantOf(state uint32) (growingPlant, bool) {
	for _, g := range growingPlants {
		if state >= g.headLo && state <= g.headHi {
			return g, true
		}
	}
	return growingPlant{}, false
}

// isGrowingPlantHead reports a kelp/vine head short of its full age.
func isGrowingPlantHead(state uint32) bool {
	g, ok := growingPlantOf(state)
	return ok && g.age(state) < growingPlantMaxAge
}

// trimPlant is ShearsItem.useOn: a head short of its maximum age is set to
// it (with its berries as they were), so it stops growing; the shears wear.
func (h *hub) trimPlant(players map[int32]*tracked, e evTrimPlant) {
	t := players[e.eid]
	if t == nil {
		return
	}
	w := h.worldFor(t.dim)
	state := w.At(e.x, e.y, e.z)
	g, ok := growingPlantOf(state)
	if !ok || g.age(state) >= growingPlantMaxAge {
		return
	}
	berries := g.berryStride == 2 && (state-g.headLo)%2 == 0
	h.setBlockLive(players, t.dim, e.x, e.y, e.z, g.headAt(growingPlantMaxAge, berries))
	h.playSoundDim(players, t.dim, "minecraft:block.growing_plant.crop", sndBlock, float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
	if isSurvival(t.gamemode) {
		h.applyToolWear(t, t.useSlot(), 1)
	}
}

// mudBottle is PotionItem.useOn: a water bottle on dirt makes mud and hands
// back the glass bottle.
func (h *hub) mudBottle(players map[int32]*tracked, e evMudBottle) {
	t := players[e.eid]
	if t == nil || t.inv == nil {
		return
	}
	held := usedStack(t)
	if held.item != itemPotion || held.potion != potWater || !convertableToMud(h.worldFor(t.dim).At(e.x, e.y, e.z)) {
		return
	}
	x, y, z := float64(e.x)+0.5, float64(e.y)+1, float64(e.z)+0.5
	h.playSoundDim(players, t.dim, "minecraft:entity.generic.splash", sndBlock, x, y, z, 1, 1)
	h.playSoundDim(players, t.dim, "minecraft:item.bottle.empty", sndBlock, x, y, z, 1, 1)
	h.spawnParticles(players, t.dim, particleSplash, x, y, z, 0.5, 0, 5)
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
		if changed, left := t.inv.addStack(invStack{item: itemGlassBottle, count: 1}); left == 0 {
			for _, sl := range changed {
				h.sendSlot(t, sl)
			}
		} else {
			h.spawnItemIn(players, t.dim, itemGlassBottle, 1, t.x, t.y+1, t.z)
		}
	}
	h.setBlockLive(players, t.dim, e.x, e.y, e.z, worldgen.Mud)
}

// spawnerKey names a spawner position for the rules file.
func spawnerKey(dim, x, y, z int) string { return fmt.Sprintf("%d,%d,%d,%d", dim, x, y, z) }

// eggSpawner is SpawnEggItem.useOn on a spawner: the spawner's mob becomes
// the egg's, and the egg is spent.
func (h *hub) eggSpawner(players map[int32]*tracked, e evEggSpawner) {
	t := players[e.eid]
	if t == nil {
		return
	}
	et, ok := spawnEggEntity[usedStack(t).item]
	if !ok || h.worldFor(t.dim).At(e.x, e.y, e.z) != spawnerBlock {
		return
	}
	h.setSpawnerEntity(simPos{dim: t.dim, blockPos: blockPos{e.x, e.y, e.z}}, entityRegistryName(et))
	// The cage shows what it will spawn from now on, without waiting for the
	// spawner's own cadence to come round.
	h.showSpawner(players, t.dim, blockPos{e.x, e.y, e.z}, et)
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
	h.playSoundDim(players, t.dim, "minecraft:block.metal.place", sndBlock, float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
}

// useSpawnEgg is SpawnEggItem.useOn away from a spawner: the egg's mob
// appears on the clicked face — inside the block itself when it has no
// collision (tall grass, a flower), otherwise in the cell beyond it. Only a
// survival player pays for it.
func (h *hub) useSpawnEgg(players map[int32]*tracked, e evSpawnEgg) {
	t := players[e.eid]
	if t == nil {
		return
	}
	et, ok := spawnEggEntity[usedStack(t).item]
	if !ok {
		return
	}
	w := h.worldFor(t.dim)
	if w == nil {
		return
	}
	x, y, z := e.x, e.y, e.z
	// Vanilla asks whether the clicked block's collision shape is empty; the
	// engine's nearest predicate is #replaceable, which is the same set of
	// things you can stand inside — grass, a flower, water.
	if !worldgen.IsReplaceable(w.At(x, y, z)) {
		dx, dy, dz := faceDelta(e.face)
		x, y, z = x+dx, y+dy, z+dz
	}
	m := h.spawnConfigured(players, et, t.dim, float64(x)+0.5, float64(y), float64(z)+0.5)
	if m == nil {
		return // a plugin refused it, or the species has no spawn path
	}
	// Vanilla marks an egg-spawned mob persistent: it is a placed thing, not
	// part of the natural population, and must not despawn.
	m.persistent = true
	// SpawnEggItem.spawnMob: ENTITY_PLACE where it went, and no sound of its
	// own (the egg-throw sound is a thrown egg's).
	h.vib(t.dim, freqEntityPlace, x, y, z, t.p.eid)
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
}

// spawnerMobFor is the mob a spawner spawns: a spawn egg's choice if one
// was used on it, else the dungeon's.
func (h *hub) spawnerMobFor(dim, x, y, z int, dflt int) int {
	if name, ok := h.rules.SpawnerMobs[spawnerKey(dim, x, y, z)]; ok {
		if et, ok := entityByName[name]; ok {
			return et
		}
	}
	return dflt
}

// placeCrystal is EndCrystalItem.useOn: on obsidian or bedrock, in any
// dimension, with the cell above empty and no entity inside the 1×2×1 box
// over it, a crystal appears; in the End, four of them round the exit portal
// bring the dragon back.
func (h *hub) placeCrystal(players map[int32]*tracked, e evPlaceCrystal) {
	t := players[e.eid]
	if t == nil || usedStack(t).item != itemEndCrystal {
		return
	}
	w := h.worldFor(t.dim)
	if s := w.At(e.x, e.y, e.z); s != obsidianBase && s != worldgen.Bedrock {
		return
	}
	if w.At(e.x, e.y+1, e.z) != worldgen.Air { // level.isEmptyBlock(above)
		return
	}
	// level.getEntities(null, AABB(above, above + (1, 2, 1))): any entity at all.
	bx, by, bz := float64(e.x), float64(e.y+1), float64(e.z)
	hits := func(dim int, x, y, z, hw, ht float64) bool {
		return dim == t.dim && x+hw > bx && x-hw < bx+1 && z+hw > bz && z-hw < bz+1 && y+ht > by && y < by+2
	}
	for _, o := range players {
		if !o.dead && hits(o.dim, o.x, o.y, o.z, 0.3, 1.8) {
			return
		}
	}
	for _, m := range h.mobs {
		if b := m.box(); m.dying == 0 && hits(m.dim, m.x, m.y, m.z, b.w/2, b.h) {
			return
		}
	}
	for _, it := range h.items {
		if hits(it.dim, it.x, it.y, it.z, 0.125, 0.25) {
			return
		}
	}
	for _, c := range h.crystals {
		if hits(c.dim, c.x, c.y, c.z, 1, 2) {
			return
		}
	}
	cx, cy, cz := bx+0.5, by, bz+0.5
	c := &crystal{eid: h.allocEID(), dim: t.dim, x: cx, y: cy, z: cz}
	binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
	h.crystals[c.eid] = c
	h.toDimEv(players, t.dim, entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
	h.vib(t.dim, freqEntityPlace, e.x, e.y+1, e.z, t.p.eid) // ENTITY_PLACE
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
	if t.dim == dimEnd { // serverLevel.getDragonFight(): the End's alone
		h.tryRespawnDragon(players)
	}
}

// sendCrystalsTo spawns the end crystals of the joiner's dimension.
func (h *hub) sendCrystalsTo(t *tracked) {
	for _, c := range h.crystals {
		if c.dim == t.dim {
			t.p.trySendEv(entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
		}
	}
}

// tryRespawnDragon is EndDragonFight.tryRespawn: with the dragon beaten and
// a crystal two blocks out on each side of the exit portal, the four are
// spent and the fight is staged again.
func (h *hub) tryRespawnDragon(players map[int32]*tracked) {
	if !h.rules.DragonDefeated || h.dragon != nil {
		return
	}
	cy := worldgen.EndSurfaceY
	for h.end.At(0, cy, 0) != worldgen.Bedrock && h.end.At(0, cy, 0) != worldgen.Air && cy < worldgen.EndSurfaceY+8 {
		cy++
	}
	var found []*crystal
	for _, d := range [4][2]int{{2, 0}, {-2, 0}, {0, 2}, {0, -2}} {
		var hit *crystal
		for _, c := range h.crystals {
			if c.dim == dimEnd && int(math.Floor(c.x)) == d[0] && int(math.Floor(c.z)) == d[1] && c.y >= float64(cy-1) && c.y <= float64(cy+2) {
				hit = c
			}
		}
		if hit == nil {
			return
		}
		found = append(found, hit)
	}
	if h.dragonRespawn != nil {
		return
	}
	h.startDragonRespawn(players, found, cy) // the ceremony brings the dragon back
}

// placeRocket is FireworkRocketItem.useOn: a rocket lit against a block
// launches from the click point, a little way off the face.
func (h *hub) placeRocket(players map[int32]*tracked, e evPlaceRocket) {
	t := players[e.eid]
	if t == nil || usedStack(t).item != itemFireworkRocket {
		return
	}
	dx, dy, dz := faceDelta(e.face)
	x := float64(e.x) + float64(e.cx) + float64(dx)*0.15
	y := float64(e.y) + float64(e.cy) + float64(dy)*0.15
	z := float64(e.z) + float64(e.cz) + float64(dz)*0.15
	st := usedStack(t)
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
	h.spawnRocket(players, t.dim, x, y, z, 0, st)
}
