package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A held weapon adds its ATTACK_DAMAGE modifier to the mob's base: the
// item's damage less the player's own 1, so a vindicator's iron axe adds 8
// to its 5 for vanilla's 13.
func TestMobWeaponAddsItsModifierOnly(t *testing.T) {
	for _, tc := range []struct {
		held int32
		want float32
	}{{itemIronSword, 5}, {itemByName["iron_axe"], 8}, {itemByName["golden_sword"], 3}, {itemByName["stone_sword"], 4}} {
		m := &mob{held: tc.held}
		if got := mobHeldBonus(m); got != tc.want {
			t.Errorf("held %d adds %v, want %v", tc.held, got, tc.want)
		}
	}
	if d := meleeDamageFor(entityBlaze); d != 6 {
		t.Errorf("blaze ATTACK_DAMAGE %v, want 6", d)
	}
}

// A witch targets players only; the raiders with villager goals keep them,
// and a ravager leaves baby villagers alone.
func TestRaiderPrey(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	spawn := func(et int, x float64) *mob { return h.spawnMob(players, et, x, 180, 0.5) }
	villager := spawn(entityVillager, 0.5)
	baby := spawn(entityVillager, 2.5)
	baby.baby = true
	golem := spawn(entityIronGolem, 4.5)
	witch := spawn(entityWitch, 6.5)
	pillager := spawn(entityPillager, 8.5)
	ravager := spawn(entityRavager, 10.5)
	for _, o := range []*mob{villager, golem} {
		if h.preyOf(witch, o) {
			t.Errorf("a witch hunts %s", advEntityName[o.etype])
		}
		if !h.preyOf(pillager, o) {
			t.Errorf("a pillager ignores %s", advEntityName[o.etype])
		}
	}
	if !h.preyOf(ravager, villager) || h.preyOf(ravager, baby) {
		t.Error("a ravager takes adult villagers only")
	}
}

// A parched is a skeleton: in the skeleton family, not burning in daylight,
// and drawing its bow on its own slower interval (70 ticks, 50 on hard).
func TestParchedIsASlowerSkeleton(t *testing.T) {
	if !skeletonKind(entityParched) {
		t.Fatal("parched is not in the skeleton family")
	}
	if burnsInDaylight[entityParched] {
		t.Error("a parched burns in daylight; 26.3's #burn_in_daylight leaves it out")
	}
	h := newHub(world.New(83))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	h.world.ForceLoad(0, 0, 2)
	m := h.spawnMob(players, entityParched, 8.5, 180, 0.5)
	for i := 0; i < 200 && len(h.arrows) == 0; i++ {
		h.skeletonShoot(players, m)
	}
	if len(h.arrows) == 0 {
		t.Fatal("a parched in range never shot")
	}
	if m.attackCD != 34 {
		t.Errorf("parched cooldown %d mob-updates, want 34 (70 ticks)", m.attackCD)
	}
}

// A blow sets a mob running only if its species has a PanicGoal: a cow
// bolts, an ocelot or a snow golem does not.
func TestStruckMobPanicsOnlyIfItsSpeciesDoes(t *testing.T) {
	for _, tc := range []struct {
		etype int
		panic bool
	}{{entityCow, true}, {entityOcelot, false}, {entitySnowGolem, false}, {entityZombieHorse, false}} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		pl.x, pl.y, pl.z = 0.5, 180, 0.5
		h.world.ForceLoad(0, 0, 2)
		m := h.spawnMob(players, tc.etype, 1.5, 180, 0.5)
		if m == nil {
			t.Fatalf("%s did not spawn", advEntityName[tc.etype])
		}
		m.health = 1000
		h.attackMob(players, pl.p.eid, m.eid)
		if got := m.panic > 0; got != tc.panic {
			t.Errorf("struck %s panicking=%v, want %v", advEntityName[tc.etype], got, tc.panic)
		}
	}
}

// The target classes vanilla hangs on these species besides players.
func TestMoreTargetClasses(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	spawn := func(et int, x float64) *mob { return h.spawnMob(players, et, x, 180, 0.5) }
	golem := spawn(entityIronGolem, 0.5)
	skel := spawn(entitySkeleton, 2.5)
	wsk := spawn(entityWitherSkeleton, 4.5)
	piglin := spawn(entityPiglin, 6.5)
	guardian := spawn(entityGuardian, 8.5)
	axolotl := spawn(entityAxolotl, 10.5)
	if !h.preyOf(skel, golem) || !h.preyOf(wsk, golem) {
		t.Error("skeletons hunt iron golems")
	}
	if !h.preyOf(wsk, piglin) || h.preyOf(skel, piglin) {
		t.Error("a wither skeleton hunts piglins; a plain skeleton does not")
	}
	if !h.preyOf(guardian, axolotl) {
		t.Error("a guardian hunts axolotls")
	}
}
