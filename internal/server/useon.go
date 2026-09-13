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

// entityNameOf is the registry name of an entity type (the rules file keys
// spawner choices by name, which survives an id renumbering).
func entityRegistryName(et int) string {
	for name, id := range entityByName {
		if id == et {
			return name
		}
	}
	return ""
}

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
}
type evMudBottle struct {
	eid     int32
	x, y, z int
}
type evEggSpawner struct {
	eid     int32
	x, y, z int
}
type evPlaceCrystal struct {
	eid     int32
	x, y, z int
}
type evPlaceRocket struct {
	eid        int32
	x, y, z    int
	face       int32
	cx, cy, cz float32
}

func (evTrimPlant) isHubEvent()    {}
func (evMudBottle) isHubEvent()    {}
func (evEggSpawner) isHubEvent()   {}
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
	if t.gamemode == gmSurvival {
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
}

// mudBottle is PotionItem.useOn: a water bottle on dirt makes mud and hands
// back the glass bottle.
func (h *hub) mudBottle(players map[int32]*tracked, e evMudBottle) {
	t := players[e.eid]
	if t == nil || t.inv == nil {
		return
	}
	held := heldStack(t)
	if held.item != itemPotion || held.potion != potWater || !convertableToMud(h.worldFor(t.dim).At(e.x, e.y, e.z)) {
		return
	}
	x, y, z := float64(e.x)+0.5, float64(e.y)+1, float64(e.z)+0.5
	h.playSoundDim(players, t.dim, "minecraft:entity.generic.splash", sndBlock, x, y, z, 1, 1)
	h.playSoundDim(players, t.dim, "minecraft:item.bottle.empty", sndBlock, x, y, z, 1, 1)
	h.spawnParticles(players, particleSplash, x, y, z, 0.5, 0, 5)
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
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
	et, ok := spawnEggEntity[heldStack(t).item]
	if !ok || h.worldFor(t.dim).At(e.x, e.y, e.z) != spawnerBlock {
		return
	}
	if h.rules.SpawnerMobs == nil {
		h.rules.SpawnerMobs = map[string]string{}
	}
	h.rules.SpawnerMobs[spawnerKey(t.dim, e.x, e.y, e.z)] = entityRegistryName(et)
	h.saveRules()
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.playSoundDim(players, t.dim, "minecraft:block.metal.place", sndBlock, float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
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

// placeCrystal is EndCrystalItem.useOn: on obsidian or bedrock with two
// clear cells above and nothing standing there, a crystal appears; in the
// End, four of them round the exit portal bring the dragon back.
func (h *hub) placeCrystal(players map[int32]*tracked, e evPlaceCrystal) {
	t := players[e.eid]
	if t == nil || t.dim != 2 || heldStack(t).item != itemEndCrystal {
		return // crystals live in the End's fight table; elsewhere they have no home yet
	}
	w := h.worldFor(t.dim)
	if s := w.At(e.x, e.y, e.z); s != obsidianBase && s != worldgen.Bedrock {
		return
	}
	if w.At(e.x, e.y+1, e.z) != worldgen.Air || w.At(e.x, e.y+2, e.z) != worldgen.Air {
		return
	}
	cx, cy, cz := float64(e.x)+0.5, float64(e.y+1), float64(e.z)+0.5
	for _, o := range players {
		if o.dim == t.dim && math.Abs(o.x-cx) < 1 && math.Abs(o.z-cz) < 1 && o.y > cy-2 && o.y < cy+2 {
			return
		}
	}
	for _, c := range h.crystals {
		if math.Abs(c.x-cx) < 1 && math.Abs(c.z-cz) < 1 && math.Abs(c.y-cy) < 2 {
			return
		}
	}
	c := &crystal{eid: h.allocEID(), x: cx, y: cy, z: cz}
	binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
	h.crystals[c.eid] = c
	h.toDimEv(players, 2, entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.tryRespawnDragon(players)
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
			if int(math.Floor(c.x)) == d[0] && int(math.Floor(c.z)) == d[1] && c.y >= float64(cy-1) && c.y <= float64(cy+2) {
				hit = c
			}
		}
		if hit == nil {
			return
		}
		found = append(found, hit)
	}
	for _, c := range found {
		delete(h.crystals, c.eid)
		h.toDimEv(players, 2, entGone(c.eid))
	}
	h.rules.DragonDefeated = false
	h.rules.DragonHealth = 0
	h.saveRules()
	h.enterEnd(players, nil)
	for _, t := range players {
		if t.dim == 2 {
			t.p.trySendEv(chatEv("The Ender Dragon stirs again."))
		}
	}
}

// placeRocket is FireworkRocketItem.useOn: a rocket lit against a block
// launches from the click point, a little way off the face.
func (h *hub) placeRocket(players map[int32]*tracked, e evPlaceRocket) {
	t := players[e.eid]
	if t == nil || heldStack(t).item != itemFireworkRocket {
		return
	}
	dx, dy, dz := faceDelta(e.face)
	x := float64(e.x) + float64(e.cx) + float64(dx)*0.15
	y := float64(e.y) + float64(e.cy) + float64(dy)*0.15
	z := float64(e.z) + float64(e.cz) + float64(dz)*0.15
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.spawnRocket(players, t.dim, x, y, z, 0)
}
