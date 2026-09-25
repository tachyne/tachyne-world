package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func usedStat(t *tracked, item int32) int32 {
	return t.stats[statKey{attachproto.StatUsed, item}]
}

// minecraft:used through the real entry paths: placing a block (in creative
// too), mining with a pickaxe and landing a sword blow each count one use of
// the item, as ItemStack.useOn, mineBlock and hurtEnemy award it.
func TestItemUsedStatistic(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	stone := itemByName["stone"]
	pick := itemByName["diamond_pickaxe"]
	sword := itemByName["diamond_sword"]
	var tr *tracked
	onHub(t, h, func() {
		tr = h.playersRef[p.eid]
		tr.inv.slots[0] = invStack{item: stone, count: 64}
		h.sendHandSlot(tr, 0)
	})
	p.setHotbarSlot(0, stone)
	y := int(p.y) + 1
	s.handlePlace(p, placeBody(3, y-1, 0, 1)) // on top of the ground at 3,y-1
	onHub(t, h, func() {
		if got := usedStat(tr, stone); got != 1 {
			t.Errorf("placing stone counted %d uses, want 1", got)
		}
	})

	s.modes.set(p.key(), gmSurvival)
	onHub(t, h, func() {
		tr.gamemode, tr.onGround = gmSurvival, true
		tr.inv.slots[0] = invStack{item: pick, count: 1}
		h.world.SetBlock(5, y, 0, worldgen.Stone)
		z := h.spawnMob(h.playersRef, entityZombie, 1.5, float64(y), 0.5)
		z.health = 100
		tr.inv.slots[1] = invStack{item: sword, count: 1}
		tr.p.setHotbarSlot(1, sword)
		tr.p.setHeldSlot(1)
		h.onAttack(h.playersRef, evAttack{attacker: p.eid, target: z.eid})
		if got := usedStat(tr, sword); got != 1 {
			t.Errorf("a landed sword blow counted %d uses, want 1", got)
		}
		tr.p.setHeldSlot(0)
	})
	p.setHotbarSlot(0, pick)
	s.handleDig(p, digBody(digStartBreak, 5, y, 0))
	p.digStartAt -= 100
	s.handleDig(p, digBody(digFinishBreak, 5, y, 0))
	onHub(t, h, func() {})
	onHub(t, h, func() {
		if got := usedStat(tr, pick); got != 1 {
			t.Errorf("mining stone counted %d pickaxe uses, want 1", got)
		}
	})
}
