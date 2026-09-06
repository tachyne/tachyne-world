package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A captain's death drops an ominous bottle (I–V); drinking it gives Bad
// Omen at the bottle's level; a trial spawner that sees the omen turns it
// into Trial Omen for fifteen minutes a level and goes ominous, restarting
// its round on the ominous rewards.
func TestOminousBottleAndTrialOmen(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	captain := h.spawnMob(players, entityPillager, 10.5, 200, 10.5)
	captain.patrolCaptain = true
	h.dropOminousBottle(players, captain)
	var bottle *itemEntity
	for _, it := range h.items {
		if it.item == itemOminousBottle {
			bottle = it
		}
	}
	if bottle == nil || bottle.potion < 1 || bottle.potion > 5 {
		t.Fatalf("the captain drops a bottle of level I–V, got %+v", bottle)
	}
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.gamemode = gmSurvival
	pl.inv.slots[0] = invStack{item: itemOminousBottle, count: 1, potion: 3}
	h.eat(players, pl, 0)
	if lvl := pl.hasEffect(effBadOmen); lvl != 3 {
		t.Fatalf("drinking a level-III bottle gives Bad Omen III (hasEffect=amp+1), got %d", lvl)
	}
	if pl.inv.slots[0].count != 0 {
		t.Error("the bottle is spent")
	}
	// The spawner sees the omen.
	ts := &trialSpawner{pos: blockPos{12, 200, 12}, kind: "zombie", state: trialWaitingPlayers,
		detected: map[[16]byte]bool{}, current: map[int32]bool{}}
	h.trials = map[blockPos]*trialSpawner{ts.pos: ts}
	pl.x, pl.y, pl.z = 12.5, 200, 12.5
	h.world.SetBlock(12, 200, 12, trialSpawnerBlock(false, trialWaitingPlayers))
	h.trialOmenCheck(players, ts)
	if !ts.ominous {
		t.Fatal("a Bad Omen player in range turns the spawner ominous")
	}
	if pl.hasEffect(effBadOmen) != 0 || pl.hasEffect(effTrialOmen) == 0 {
		t.Error("Bad Omen becomes Trial Omen")
	}
	if left := pl.effects[effTrialOmen].left; left < trialOmenSecsPerLevel*3*20-40 || left > trialOmenSecsPerLevel*3*20 {
		t.Errorf("Trial Omen lasts 15 min a level: %d ticks left for level III", left)
	}
	if !isTrialSpawner(h.world.At(12, 200, 12)) || h.world.At(12, 200, 12) != trialSpawnerBlock(true, ts.state) {
		t.Error("the block shows the ominous state")
	}
	keys, cons := 0, 0
	for i := 0; i < 200; i++ {
		switch h.trialRewardTable(ts) {
		case "spawners/ominous/trial_chamber/key":
			keys++
		case "spawners/ominous/trial_chamber/consumables":
			cons++
		default:
			t.Fatal("an ominous spawner pays from the ominous tables only")
		}
	}
	if keys == 0 || cons <= keys {
		t.Errorf("ominous rewards are 3:7 key:consumables, got %d:%d", keys, cons)
	}
	// The omen wears off with the cooldown.
	ts.state, ts.cooldown = trialCooldown, 0
	h.tickTrialSpawner(players, ts)
	if ts.ominous {
		t.Error("the cooldown's end removes the ominous state")
	}
}

// An ominous spawner's zombies come armed from the trial-chamber melee
// table, and that gear never drops.
func TestOminousTrialMobsAreEquipped(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	armed := 0
	for i := 0; i < 20; i++ {
		m := h.spawnMob(players, entityZombie, 10.5, 200, 10.5)
		h.equipTrialMob(players, m, "zombie")
		if m.held != 0 || m.gear[0].item != 0 || m.gear[1].item != 0 {
			armed++
			if !m.spawnGear {
				t.Fatal("spawn gear is marked so it never drops")
			}
		}
	}
	if armed == 0 {
		t.Error("the melee table always issues a sword")
	}
	if trialEquipmentTable("slime") != "" || trialEquipmentTable("stray") != "equipment/trial_chamber_ranged" {
		t.Error("equipment tables by kind")
	}
}
