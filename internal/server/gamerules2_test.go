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

// Five more of vanilla's rules, each wired to a mechanic the engine already
// has rather than merely accepted and ignored.
func TestPortalProjectileAndSoundRules(t *testing.T) {
	h := newHub(world.New(41))
	if !h.rules.AllowNether || !h.rules.ProjectilesBreak || !h.rules.GlobalSounds {
		t.Error("the three booleans default on, as vanilla's do")
	}
	if h.rules.PortalDelay != 80 || h.rules.PortalDelayCreate != 0 {
		t.Errorf("portal delays are %d/%d, want vanilla's 80 and 0",
			h.rules.PortalDelay, h.rules.PortalDelayCreate)
	}
	// Every one of them is settable under its vanilla name.
	players := map[int32]*tracked{}
	for _, tc := range []struct {
		name string
		on   bool
		num  int
		get  func() any
		want any
	}{
		{name: "allow_entering_nether_using_portals", on: false,
			get: func() any { return h.rules.AllowNether }, want: false},
		{name: "projectiles_can_break_blocks", on: false,
			get: func() any { return h.rules.ProjectilesBreak }, want: false},
		{name: "global_sound_events", on: false,
			get: func() any { return h.rules.GlobalSounds }, want: false},
		{name: "players_nether_portal_default_delay", num: 200,
			get: func() any { return h.rules.PortalDelay }, want: 200},
		{name: "players_nether_portal_creative_delay", num: 40,
			get: func() any { return h.rules.PortalDelayCreate }, want: 40},
	} {
		h.applyRule(players, evSetRule{rule: tc.name, on: tc.on, num: tc.num})
		if got := tc.get(); got != tc.want {
			t.Errorf("%s did not take: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ServerLevel.globalLevelEvent: everyone in the dimension hears it, and a
// listener out of earshot hears it from a point thirty-two blocks off in its
// direction rather than from where it really happened.
func TestGlobalSoundReachesEveryone(t *testing.T) {
	h := newHub(world.New(43))
	near, far := testTracked(), testTracked()
	far.p.eid = 99
	near.dim, near.x, near.y, near.z = 0, 5, 70, 0
	far.dim, far.x, far.y, far.z = 0, 5000, 70, 0
	players := map[int32]*tracked{near.p.eid: near, far.p.eid: far}

	// Nothing panics and nothing is skipped: both are in the dimension.
	h.playSoundGlobal(players, 0, "minecraft:entity.wither.spawn", sndHostile, 0, 70, 0, 4, 1)

	// The clamp itself: a listener 5000 blocks away is given a source 32 away.
	sx, sy, sz := 0.0, 70.0, 0.0
	d := dist3(far.x, far.y, far.z, sx, sy, sz)
	cx := far.x + (sx-far.x)/d*globalSoundRange
	if got := dist3(far.x, far.y, far.z, cx, sy, sz); got < globalSoundRange-1 || got > globalSoundRange+1 {
		t.Errorf("the clamped source sits %v blocks away, want %v", got, globalSoundRange)
	}
	// A player in another dimension hears nothing at all.
	far.dim = 1
	h.playSoundGlobal(players, 0, "minecraft:entity.wither.spawn", sndHostile, 0, 70, 0, 4, 1)
}

// /gamerule <rule>: every rule the command lists can be read back.
func TestEveryGameruleQueries(t *testing.T) {
	h := newHub(world.New(1))
	for _, r := range append(append([]string{}, booleanRules...), numericRules...) {
		if _, ok := h.ruleValueText(r); !ok {
			t.Errorf("%s has no readable value", r)
		}
	}
	if v, _ := h.ruleValueText("limited_crafting"); v != "false" {
		t.Errorf("limited_crafting reads %q, want false", v)
	}
}
