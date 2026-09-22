package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestMobCombatKillDropsBeef(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityCow, 5, 70, 5)
	if m.health != cowHealth {
		t.Fatalf("cow should start at %d hp, got %d", cowHealth, m.health)
	}

	h.attackMob(players, 999, m.eid) // one hit (no attacker in map → no knockback)
	if m.health != cowHealth-fistDamage {
		t.Errorf("after one hit hp=%d want %d", m.health, cowHealth-fistDamage)
	}
	if h.mobs[m.eid] == nil {
		t.Fatal("cow should still be alive after one hit")
	}

	for m.dying == 0 { // beat it to death (starts the death animation)
		h.tick.Add(20) // each blow lands past the last one's damage cooldown
		m.invulnTicks = 0
		h.attackMob(players, 999, m.eid)
	}
	for h.mobs[m.eid] != nil { // let the death animation play out → despawn + drops
		h.updateMobs(players)
	}
	beef := 0
	for _, it := range h.items {
		if it.item == itemBeef {
			beef += it.count
		}
	}
	if beef < 1 || beef > 3 {
		t.Errorf("dead cow should drop 1-3 beef, got %d", beef)
	}
}

func TestHitCowPanicsAndFlees(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(0, 0)
	atk := &tracked{p: newPlayer(1, "a", [16]byte{}), x: float64(lx) + 3, y: 70, z: float64(lz)}
	players := map[int32]*tracked{1: atk}
	m := h.spawnMob(players, entityCow, float64(lx), float64(h.world.GroundY(lx, lz)), float64(lz))
	atk.y = m.y // stand level with the cow (reach is validated server-side now)

	h.attackMob(players, 1, m.eid)
	if m.panic != panicTicks || m.fleeX != atk.x {
		t.Fatalf("hit cow should panic away from attacker: panic=%d fleeX=%v", m.panic, m.fleeX)
	}
	x0 := m.x
	for i := 0; i < 20; i++ {
		h.updateMobs(players)
	}
	if m.x > x0+0.01 { // attacker is to the +x side, so the cow must flee toward -x
		t.Errorf("fleeing cow drifted toward the attacker: x0=%v x=%v", x0, m.x)
	}
}

func TestAttackNonMobIsNoop(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.attackMob(players, 1, 12345) // unknown target — must not panic
}

// TestSlimeAndMagmaCubeDrops pins the two cooker-sized rolls the entity
// tables make: only the SMALLEST slime leaves a ball (uniform 0-2), a small
// magma cube leaves nothing at all, and a big one's cream comes off roughly
// one kill in four because set_count starts at uniform -2..1.
func TestSlimeAndMagmaCubeDrops(t *testing.T) {
	h := newHub(world.New(1))

	count := func(etype int, size, n int) map[int32]int {
		got := map[int32]int{}
		for i := 0; i < n; i++ {
			m := &mob{etype: etype, size: size}
			for _, d := range h.mobLoot(m) {
				if d.count > 0 {
					got[d.item] += d.count
				}
			}
		}
		return got
	}

	if got := count(entitySlime, 2, 200); len(got) != 0 {
		t.Fatalf("a big slime dropped %v, want nothing", got)
	}
	small := count(entitySlime, 1, 400)
	if small[itemSlimeball] == 0 {
		t.Fatal("a small slime never dropped a slimeball")
	}
	if len(small) != 1 {
		t.Fatalf("a small slime dropped %v, want slimeballs only", small)
	}

	if got := count(entityMagmaCube, 1, 200); len(got) != 0 {
		t.Fatalf("a small magma cube dropped %v, want nothing", got)
	}
	// 400 kills at p=1/4 for one cream: the total should sit near 100 and
	// nowhere near the 200 an even coin flip would give.
	cream := count(entityMagmaCube, 2, 400)[itemMagmaCream]
	if cream < 50 || cream > 150 {
		t.Fatalf("400 big magma cubes yielded %d cream, want about 100 (uniform -2..1)", cream)
	}
}

// TestGuardianRareFish: both guardian tables end on a killed_by_player pool
// that rolls a fish out of the fishing table at 2.5%, and Looting must not
// multiply it (that pool has no enchanted_count_increase) — nor the elder's
// sponge or tide template.
func TestGuardianRareFish(t *testing.T) {
	h := newHub(world.New(1))
	fishes := map[int32]bool{
		itemByName["cod"]: true, itemByName["salmon"]: true,
		itemByName["pufferfish"]: true, itemByName["tropical_fish"]: true,
	}

	// A mob death that no player caused rolls no fish pool at all.
	for i := 0; i < 300; i++ {
		for _, d := range h.guardianLoot(&mob{etype: entityGuardian}) {
			if d.item == itemPrismarineCrystals || d.item == itemPrismarineShard {
				continue
			}
			if d.fixed && fishes[d.item] {
				t.Fatal("a guardian that no player killed dropped the rare fish")
			}
		}
	}

	fixedSeen := 0
	for i := 0; i < 4000; i++ {
		for _, d := range h.guardianLoot(&mob{etype: entityGuardian, hitByPlayer: true}) {
			if d.fixed {
				fixedSeen++
				if !fishes[d.item] {
					t.Fatalf("unexpected fixed drop %d", d.item)
				}
				if d.count != 1 {
					t.Fatalf("rare fish count = %d, want 1", d.count)
				}
			}
		}
	}
	if fixedSeen < 40 || fixedSeen > 160 { // 2.5% of 4000 is 100
		t.Fatalf("%d rare fish in 4000 kills, want about 100", fixedSeen)
	}

	sponge, template := 0, 0
	for i := 0; i < 400; i++ {
		for _, d := range h.guardianLoot(&mob{etype: entityElderGuardian, hitByPlayer: true}) {
			switch d.item {
			case itemWetSponge:
				sponge++
				if !d.fixed {
					t.Fatal("Looting must not multiply a wet sponge")
				}
			case itemTideTemplate:
				template++
				if !d.fixed {
					t.Fatal("Looting must not multiply a tide template")
				}
			}
		}
	}
	if sponge != 400 {
		t.Fatalf("elder guardians killed by a player dropped %d sponges in 400, want 400", sponge)
	}
	if template < 40 || template > 120 { // 1 in 5
		t.Fatalf("%d tide templates in 400, want about 80", template)
	}
}
