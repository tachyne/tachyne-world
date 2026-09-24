package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A small fireball that strikes a block lights fire in the empty cell on the
// face it struck (SmallFireball.onHitBlock) — a blaze's only when mobGriefing
// is on, an ownerless one (a dispenser's) always.
func TestSmallFireballLightsTheStruckFace(t *testing.T) {
	stone := worldgen.BlockBase("stone")
	rig := func(griefing bool) (*hub, map[int32]*tracked, *mob) {
		h := newHub(world.New(1))
		h.world.ForceLoad(0, 0, 2)
		h.rules.MobGriefing = griefing
		h.arrows = map[int32]*arrowEntity{}
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 8; z++ {
				h.world.SetBlock(x, 179, z, stone)
				for y := 180; y < 186; y++ {
					h.world.SetBlock(x, y, z, worldgen.Air)
				}
			}
		}
		h.world.SetBlock(0, 181, 6, stone) // a wall block to strike side-on
		players := map[int32]*tracked{}
		blaze := h.spawnMob(players, entityBlaze, 0.5, 184, 0.5)
		return h, players, blaze
	}
	fly := func(h *hub, players map[int32]*tracked) {
		for i := 0; i < 60 && len(h.arrows) > 0; i++ {
			h.tick.Add(1)
			h.updateArrows(players)
		}
		if len(h.arrows) != 0 {
			t.Fatal("the fireball never struck anything")
		}
	}
	fire := func(h *hub, x, y, z int) bool { return isFire(h.world.At(x, y, z)) }

	// Straight down onto the floor: fire on top of it.
	h, players, blaze := rig(true)
	a := h.launchProjectileIn(players, entitySmallFireball, 0, 0.5, 182.5, 0.5, 0, -hurtingSpeed, 0)
	a.shooter, a.dmg, a.fire = blaze.eid, blazeFireballDmg, true
	fly(h, players)
	if !fire(h, 0, 180, 0) {
		t.Errorf("a blaze fireball on the floor left %v above it, want fire", h.world.At(0, 180, 0))
	}

	// Side-on into a wall: fire in the cell in front of the face it struck.
	h, players, blaze = rig(true)
	a = h.launchProjectileIn(players, entitySmallFireball, 0, 0.5, 181.5, 3.5, 0, 0, hurtingSpeed)
	a.shooter, a.dmg, a.fire = blaze.eid, blazeFireballDmg, true
	fly(h, players)
	if !fire(h, 0, 181, 5) {
		t.Errorf("a fireball into a wall's north face left %v in front of it, want fire", h.world.At(0, 181, 5))
	}

	// mobGriefing off: a blaze's fireball lights nothing.
	h, players, blaze = rig(false)
	a = h.launchProjectileIn(players, entitySmallFireball, 0, 0.5, 182.5, 0.5, 0, -hurtingSpeed, 0)
	a.shooter, a.dmg, a.fire = blaze.eid, blazeFireballDmg, true
	fly(h, players)
	if fire(h, 0, 180, 0) {
		t.Error("with mobGriefing off a blaze's fireball still lit a fire")
	}

	// …but an ownerless one still does.
	h, players, _ = rig(false)
	a = h.launchProjectileIn(players, entitySmallFireball, 0, 0.5, 182.5, 0.5, 0, -hurtingSpeed, 0)
	a.dmg, a.fire = blazeFireballDmg, true
	fly(h, players)
	if !fire(h, 0, 180, 0) {
		t.Error("an ownerless fireball did not light the floor it struck")
	}
}
