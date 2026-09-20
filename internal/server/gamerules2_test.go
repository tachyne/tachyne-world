package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The rules added on 2026-09-18 resolve, default as vanilla's do, and each
// reaches its field.
func TestNewGamerulesResolveAndApply(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	r := h.rules
	if !r.FreezeDamage || !r.SpreadVines || !r.SpawnMonsters || !r.SpawnerBlocks || !r.ForgiveDead ||
		!r.PearlsVanish || !r.EntityDrops || !r.BlockDropDecay || !r.MobDropDecay || r.TNTDropDecay ||
		r.MaxCramming != 24 || r.RespawnRadius != 10 {
		t.Fatalf("defaults: %+v", r)
	}
	for _, name := range []string{"freeze_damage", "spread_vines", "spawn_monsters", "spawner_blocks_work",
		"forgive_dead_players", "ender_pearls_vanish_on_death", "entity_drops", "block_explosion_drop_decay",
		"mob_explosion_drop_decay", "spawn_wandering_traders"} {
		h.applyRule(players, evSetRule{rule: name, on: false})
	}
	h.applyRule(players, evSetRule{rule: "tnt_explosion_drop_decay", on: true})
	h.applyRule(players, evSetRule{rule: "max_entity_cramming", num: 6})
	h.applyRule(players, evSetRule{rule: "spawnRadius", num: 0})
	r = h.rules
	if r.FreezeDamage || r.SpreadVines || r.SpawnMonsters || r.SpawnerBlocks || r.ForgiveDead ||
		r.PearlsVanish || r.EntityDrops || r.BlockDropDecay || r.MobDropDecay || !r.TNTDropDecay ||
		r.DoTraderSpawning || r.MaxCramming != 6 || r.RespawnRadius != 0 {
		t.Fatalf("after setting: %+v", r)
	}
	if h.dropDecay(blastTNT) != true || h.dropDecay(blastMob) != false || h.dropDecay(blastBlock) != false {
		t.Fatal("drop decay by kind does not follow the rules")
	}
}

// A TNT blast drops every block it breaks by default; a creeper's drops one
// in three; turning tnt_explosion_drop_decay on thins TNT's to one in four.
func TestExplosionDropDecay(t *testing.T) {
	count := func(h *hub, kind blastKind, radius int) int {
		w := h.world
		for x := -8; x <= 8; x++ {
			for y := 176; y <= 184; y++ {
				for z := -8; z <= 8; z++ {
					w.SetBlock(x, y, z, worldgen.Air)
				}
			}
		}
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				w.SetBlock(x, 179, z, worldgen.BlockBase("dirt"))
			}
		}
		players := map[int32]*tracked{}
		before := len(h.items)
		h.explodeIn(players, 0, 0.5, 180.5, 0.5, radius, float64(radius), kind)
		n := 0
		for _, it := range h.items {
			n += it.count
		}
		return n - before
	}
	h := newHub(world.New(1))
	tnt := count(h, blastTNT, 4)
	broken := 0
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			if h.world.At(x, 179, z) == worldgen.Air {
				broken++
			}
		}
	}
	if broken < 20 || tnt != broken {
		t.Fatalf("TNT should drop every block it breaks: %d drops for %d broken", tnt, broken)
	}
	h2 := newHub(world.New(1))
	if mob := count(h2, blastMob, 4); mob >= tnt/2 {
		t.Fatalf("a mob's blast should thin the drops to a quarter: %d of %d", mob, tnt)
	}
}

// Death forgives: a wolf angry at a player calms when the player dies, and
// the player's pearl in flight vanishes.
func TestDeathForgivesAndVanishesPearls(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	wolf := h.spawnMob(players, entityWolf, 3.5, 80, 0.5)
	h.provoke(wolf, pl)
	if !wolf.hostile || wolf.targetEID != pl.p.eid {
		t.Fatal("the wolf should be angry at the player")
	}
	pl.inv.slots[0] = invStack{item: itemEnderPearl, count: 1}
	h.throwPearl(players, pl)
	if len(h.arrows) != 1 {
		t.Fatal("no pearl in flight")
	}
	pl.health = 1
	h.hurtFrom(players, pl, 5, dtGeneric, deathCause{}, from(0, 0))
	if !pl.dead {
		t.Fatal("the player should be dead")
	}
	if wolf.hostile || wolf.targetEID != 0 {
		t.Error("the wolf did not forgive the dead player")
	}
	if len(h.arrows) != 0 {
		t.Error("the dead player's pearl should vanish")
	}
}
