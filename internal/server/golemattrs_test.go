package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Attribute values from createAttributes: the iron golem's ATTACK_DAMAGE is
// 15 (its punch rolls half that plus nextInt(15)), the evoker's the default
// 2, the giant's MOVEMENT_SPEED 0.5. A blow that moves an iron golem to a
// new crack stage plays IRON_GOLEM_DAMAGE; one within the stage does not.
func TestGolemAttributesAndCrackSound(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl := survPlayer(h)
	pl.p.eid = 500
	pl.x, pl.y, pl.z = 30, 180, 30
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	g := h.spawnMob(players, entityIronGolem, 0.5, 180, 0.5)
	if g.attackDamage() != 15 {
		t.Fatalf("iron golem ATTACK_DAMAGE = %v, want 15", g.attackDamage())
	}
	for i := 0; i < 200; i++ {
		if d := h.golemPunchDamage(g); d < 7.5 || d > 21.5 {
			t.Fatalf("golem punch %v outside 7.5–21.5", d)
		}
	}
	if e := h.spawnMob(players, entityEvoker, 2.5, 180, 0.5); e.attackDamage() != 2 {
		t.Fatalf("evoker ATTACK_DAMAGE = %v, want 2", e.attackDamage())
	}
	if d := speciesOf(entityGiant); d.speed != 0.5 {
		t.Fatalf("giant MOVEMENT_SPEED = %v, want 0.5", d.speed)
	}

	pl.tracked = map[int32]bool{g.eid: true}
	clanks := func() int {
		n := 0
		for _, ev := range drainEvs(pl.p) {
			if s, ok := ev.(attachproto.Sound); ok && s.Name == "minecraft:entity.iron_golem.damage" {
				n++
			}
		}
		return n
	}
	h.updateMobs(players)
	clanks()
	g.health = 80 // still under a scratch: stage none
	h.updateMobs(players)
	if n := clanks(); n != 0 {
		t.Fatalf("a blow within the stage should not clank, got %d", n)
	}
	g.health = 60 // under three quarters: low cracks
	h.updateMobs(players)
	if n := clanks(); n != 1 {
		t.Fatalf("a blow that cracks the golem should clank once, got %d", n)
	}
	g.health = 90 // mended: silent
	h.updateMobs(players)
	if n := clanks(); n != 0 {
		t.Fatalf("mending should not clank, got %d", n)
	}
}
