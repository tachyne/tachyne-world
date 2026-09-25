package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Every species' voice must be a sound the vanilla client knows: the mobs
// whose ids differ from their names borrow the right voice, and the ones
// whose vanilla sound depends on state pick it from the mob.
func TestMobVoicesMatchVanilla(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	voice := func(etype int) (string, string, string) {
		m := h.spawnMob(players, etype, 0.5, 200, 0.5)
		return h.mobSoundsFor(m)
	}
	if hurt, death, ambient := voice(entityCaveSpider); hurt != "minecraft:entity.spider.hurt" || death != "minecraft:entity.spider.death" || ambient != "minecraft:entity.spider.ambient" {
		t.Fatalf("a cave spider speaks with the spider's voice: %s %s %s", hurt, death, ambient)
	}
	if hurt, _, ambient := voice(entityPufferfish); hurt != "minecraft:entity.puffer_fish.hurt" || ambient != "" {
		t.Fatalf("pufferfish: %s / %q", hurt, ambient)
	}
	if hurt, _, ambient := voice(entityCreaking); hurt != "minecraft:entity.creaking.sway" || ambient != "minecraft:entity.creaking.ambient" {
		t.Fatalf("creaking: %s / %s", hurt, ambient)
	}
	if _, _, ambient := voice(entitySniffer); ambient != "minecraft:entity.sniffer.idle" {
		t.Fatalf("sniffer: %s", ambient)
	}
	if _, _, ambient := voice(entityAxolotl); ambient != "minecraft:entity.axolotl.idle_air" {
		t.Fatalf("an axolotl on land: %s", ambient)
	}
	turtle := h.spawnMob(players, entityTurtle, 0.5, 200, 0.5)
	if _, _, ambient := h.mobSoundsFor(turtle); ambient != "minecraft:entity.turtle.ambient_land" {
		t.Fatalf("a turtle on land: %s", ambient)
	}
	turtle.baby = true
	if hurt, death, _ := h.mobSoundsFor(turtle); hurt != "minecraft:entity.turtle.hurt_baby" || death != "minecraft:entity.turtle.death_baby" {
		t.Fatalf("a baby turtle: %s %s", hurt, death)
	}
	allay := h.spawnMob(players, entityAllay, 0.5, 200, 0.5)
	if _, _, ambient := h.mobSoundsFor(allay); ambient != "minecraft:entity.allay.ambient_without_item" {
		t.Fatalf("an empty-handed allay: %s", ambient)
	}
	allay.held = int32(itemByName["cobblestone"])
	if _, _, ambient := h.mobSoundsFor(allay); ambient != "minecraft:entity.allay.ambient_with_item" {
		t.Fatalf("an allay with an item: %s", ambient)
	}
}

// A copper golem's hurt, death and step sounds follow its oxidation
// (CopperGolemOxidationLevels): exposed keeps the plain voice, weathered and
// oxidized have their own.
func TestCopperGolemVoiceByOxidation(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	g := h.spawnMob(players, entityCopperGolem, 0.5, 200, 0.5)
	for ox, want := range []string{"copper_golem", "copper_golem", "copper_golem_weathered", "copper_golem_oxidized"} {
		g.oxidation = ox
		hurt, death, _ := h.mobSoundsFor(g)
		steps := footstepsFor(h.world, g, 0, 199, 0)
		if hurt != "minecraft:entity."+want+".hurt" || death != "minecraft:entity."+want+".death" ||
			len(steps) != 1 || steps[0].name != "minecraft:entity."+want+".step" {
			t.Fatalf("oxidation %d: %s %s %v", ox, hurt, death, steps)
		}
	}
}
