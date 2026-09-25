package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The damage-type tags a hurt now reads: the damage gamerules by tag (a
// sweet berry bush is no is_fire, but an ender pearl's landing is is_fall),
// Frost Walker against #burn_from_stepping, and the dragon, which only
// #always_hurts_ender_dragons (or a player) can harm.
func TestDamageTagRules(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.rules.FallDamage = false
	if h.hurtBy(players, pl, 4, dtEnderPearl, deathCause{}) || pl.health != 20 {
		t.Errorf("an ender pearl landing hurt with fall damage off (health %v)", pl.health)
	}
	h.rules.FallDamage = true
	pl.armor[3] = invStack{item: itemByName["iron_boots"], count: 1, ench: enchList{{id: enchFrostWalker, lvl: 1}}}
	if h.hurtBy(players, pl, 1, dtCampfire, deathCause{}) {
		t.Error("a campfire burned Frost Walker boots")
	}
	d := &mob{etype: entityEnderDragon, health: 200}
	h.dragon = d
	h.hurtMobOf(players, d, 10, dtGeneric)
	if d.health != 200 {
		t.Errorf("generic damage hurt the dragon: %d", d.health)
	}
	h.hurtMobOf(players, d, 10, dtExplosion)
	if d.health >= 200 {
		t.Error("an explosion did not hurt the dragon")
	}
}
