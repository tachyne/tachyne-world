package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// armSword puts a stone sword in the player's hand.
func armSword(pl *tracked) {
	sword := int32(itemByName["stone_sword"])
	pl.p.setHotbarSlot(0, sword)
	pl.inv.slots[0] = invStack{item: sword, count: 1}
}

// punchThenFall has the player punch a zombie, lets `wait` ticks pass, and
// then kills it with a fall.
func punchThenFall(t *testing.T, wait uint64) (*hub, *tracked, *mob) {
	t.Helper()
	h, pl, players := killRig(t)
	h.allocEID() // the rig's player is eid 1: keep the zombie off it
	z := h.spawnMob(players, entityZombie, 0.5, 180, 1.5)
	armSword(pl)
	h.tick.Add(40)
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: z.eid})
	if z.health >= z.maxHP() {
		t.Fatalf("the punch did not land: health %d of %d", z.health, z.maxHP())
	}
	h.tick.Add(wait)
	h.hurtMobOf(players, z, 100, dtFall)
	if z.dying == 0 {
		t.Fatal("the fall did not kill the zombie")
	}
	return h, pl, z
}

// LivingEntity.lastHurtByPlayer: a mob that dies within 100 ticks of a
// player's hurt is that player's kill, however it died — the trigger, the
// kill counters, the experience and the player-kill loot. The killed
// statistic stays with the killing blow's own attacker (the fall has none).
func TestKillCreditRemembersThePlayer(t *testing.T) {
	_, pl, z := punchThenFall(t, 60)
	if !z.hitByPlayer {
		t.Error("a zombie that fell to its death 60 ticks after a punch does not drop as a player kill")
	}
	if !hasCrit(pl, "minecraft:adventure/kill_a_mob", "minecraft:zombie") || customStat(pl, "mob_kills") != 1 {
		t.Error("the fall death 60 ticks after the punch was not credited to the player")
	}
	if pl.stats[statKey{attachproto.StatKilled, int32(entityZombie)}] != 0 {
		t.Error("the killed statistic went to a player who did not strike the killing blow")
	}
}

// …and after 100 ticks the memory is gone.
func TestKillCreditMemoryExpires(t *testing.T) {
	_, pl, z := punchThenFall(t, 101)
	if z.hitByPlayer {
		t.Error("a zombie that fell 101 ticks after a punch still drops as a player kill")
	}
	if customStat(pl, "mob_kills") != 0 {
		t.Error("a death 101 ticks after the punch was credited")
	}
}

// A player's direct kill keeps the killed statistic.
func TestKillCreditDirectKillStat(t *testing.T) {
	h, pl, players := killRig(t)
	h.allocEID() // the rig's player is eid 1: keep the zombie off it
	z := h.spawnMob(players, entityZombie, 0.5, 180, 1.5)
	z.health = 1
	armSword(pl)
	h.tick.Add(40)
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: z.eid})
	if z.dying == 0 {
		t.Fatal("the punch did not kill")
	}
	if !creditedKill(pl, entityZombie) || customStat(pl, "mob_kills") != 1 {
		t.Error("a punched-to-death zombie is not credited exactly once")
	}
}

// A dispensed arrow has no owner: its hit is nobody's.
func TestDispensedArrowIsNoPlayerKill(t *testing.T) {
	h, pl, players := killRig(t)
	pl.x, pl.z = 6, -6
	h.arrows = map[int32]*arrowEntity{}
	z := h.spawnMob(players, entityZombie, 0.5, 180, -1.5)
	z.health = 1
	dispenseAt(t, h, players, itemArrowAmmo)
	if z.dying == 0 {
		t.Fatal("the dispensed arrow did not kill the zombie")
	}
	if z.hitByPlayer {
		t.Error("a zombie killed by a dispenser's arrow drops as a player kill")
	}
}

// A player killed by another player's Thorns is the wearer's kill.
func TestPvPThornsKillIsCredited(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.PvP = true
	a, b, players := pvpPair(h)
	for i := range b.armor {
		b.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1, ench: enchList{{id: enchThorns, lvl: 3}}}
	}
	for i := 0; i < 60 && !a.dead; i++ {
		a.health, b.health = 1, 20
		a.lastAttack = 0
		h.tick.Add(20)
		h.attackPlayer(players, a.p.eid, b.p.eid)
	}
	if !a.dead {
		t.Fatal("Thorns never killed the attacker")
	}
	if customStat(b, "player_kills") != 1 {
		t.Errorf("the Thorns wearer has %d player kills, want 1", customStat(b, "player_kills"))
	}
	if a.stats[statKey{attachproto.StatKilledBy, int32(entityPlayerType)}] != 1 {
		t.Error("the attacker's killed_by:player statistic did not count")
	}
}

// A player blown up by TNT another player lit is that player's kill.
func TestPvPExplosionKillIsCredited(t *testing.T) {
	h, pl, players := killRig(t)
	h.rules.PvP = true
	pl.z = -12 // well out of the blast
	v := &tracked{p: newPlayer(2, "victim", [16]byte{2}), gamemode: gmSurvival}
	initSurvival(v)
	v.x, v.y, v.z, v.health = 0.5, 180, 4.5, 1
	players[v.p.eid] = v
	h.primeTNTBy(players, 0, 0, 180, 3, 1, pl.p.eid)
	for i := 0; i < 3 && len(h.tnt) > 0; i++ {
		h.updateTNT(players)
	}
	if !v.dead {
		t.Fatal("the blast did not kill the victim")
	}
	if customStat(pl, "player_kills") != 1 {
		t.Errorf("the TNT's owner has %d player kills, want 1", customStat(pl, "player_kills"))
	}
}
