package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// LivingEntity.hurtServer: a #no_impact type skips markHurt, the only sync
// that carries a player's knockback to its own client. Only drowning is in
// the tag (and it is #no_knockback too), so the test lends the tag to
// mob_attack (generic is #no_knockback): /damage with a source position
// shoves a player, unless the type is #no_impact.
func TestNoImpactDamageGivesPlayerNoShove(t *testing.T) {
	shoved := func(noImpact bool) bool {
		saved := dmgTypeTags[dtMobAttack]
		defer func() { dmgTypeTags[dtMobAttack] = saved }()
		if noImpact {
			dmgTypeTags[dtMobAttack] |= tagNoImpact
		}
		h := newTestHub(world.New(1))
		pl := testTracked()
		pl.x, pl.y, pl.z = 10.5, 180, 10.5
		players := map[int32]*tracked{1: pl}
		drainEvents(pl)
		src := cmdDamageSource{pos: true, x: 12.5, z: 10.5}
		if !h.commandHurt(players, cmdEntity{t: pl}, 1, dtMobAttack, src) {
			t.Fatal("the blow did not land")
		}
		for _, ev := range takeEvents(pl) {
			if v, ok := ev.(attachproto.Velocity); ok && v.EID == pl.p.eid {
				return true
			}
		}
		return false
	}
	if !shoved(false) {
		t.Fatal("a generic blow with a source position sent the player no shove")
	}
	if shoved(true) {
		t.Error("a #no_impact blow shoved the player")
	}
}
