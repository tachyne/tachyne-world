package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// /summon through the dispatcher: a mob at the caller's feet by default with
// NBT (a name, tags, health), the non-mob entities (TNT, an armour stand, a
// boat, an item from its NBT), and the refusals.
func TestSummonDepth(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, `summon zombie ~ ~ ~ {CustomName:"Bob the Zombie",Tags:["boss"],Health:5,PersistenceRequired:1b}`)
	s.handleCommand(alice, "summon pig")
	s.handleCommand(alice, "summon tnt ~2 ~ ~ {fuse:200}")
	s.handleCommand(alice, "summon armor_stand ~ ~ ~3")
	s.handleCommand(alice, "summon oak_boat ~4 ~ ~")
	s.handleCommand(alice, `summon item ~ ~1 ~ {Item:{id:"minecraft:diamond",count:3}}`)
	s.handleCommand(alice, "summon item")
	s.handleCommand(alice, "summon nonsense")
	settle(t, h, logs, "U1")
	a := linesBetween(logs["alice"], "", "U1")
	for _, want := range []string{"Summoned new Bob the Zombie", "Summoned new Pig", "Summoned new Tnt",
		"Summoned new Armor Stand", "Summoned new Oak Boat", "Summoned new Diamond", "Unable to summon entity",
		"Can't find element 'minecraft:nonsense' of type 'minecraft:entity_type'"} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		var zombie, pig *mob
		for _, m := range h.mobs {
			switch m.etype {
			case entityZombie:
				zombie = m
			case entityPig:
				pig = m
			}
		}
		if zombie == nil || zombie.customName != "Bob the Zombie" || !zombie.tags["boss"] || zombie.health != 5 || !zombie.persistent {
			t.Errorf("zombie %+v", zombie)
		}
		if pig == nil || pig.x != alice.x || pig.z != alice.z {
			t.Errorf("the pig is not at the caller's feet: %+v", pig)
		}
		if len(h.tnt) != 1 || h.tnt[0].fuse < 150 {
			t.Errorf("tnt %d", len(h.tnt))
		}
		if len(h.armorStands) != 1 || len(h.vehicles) != 1 {
			t.Errorf("%d stands, %d vehicles", len(h.armorStands), len(h.vehicles))
		}
		n := 0
		for _, it := range h.items {
			if it.item == itemByName["diamond"] {
				n += it.count
			}
		}
		if n != 3 {
			t.Errorf("%d diamonds summoned, want 3", n)
		}
	})
}

// The NBT /summon reads onto a mob — NoAI, Silent, Invulnerable, a name
// that shows, a rotation, a variant, an age, a fleece — and the refusals:
// a key the engine does not model, a type it cannot summon, Peaceful, and a
// position outside the world.
func TestSummonNBT(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, cmd := range []string{
		`summon zombie ~ ~ ~ {NoAI:1b,Silent:1b,Invulnerable:1b,CustomName:"Z",CustomNameVisible:1b,Rotation:[90f,0f]}`,
		`summon zombie ~ ~ ~ {Fuse:3,Foo:1b}`,
		`summon sheep ~ ~ ~ {Color:14b}`,
		`summon wolf ~ ~ ~ {variant:"minecraft:ashen"}`,
		`summon cow ~ ~ ~ {Age:-24000}`,
		`summon rabbit ~ ~ ~ {RabbitType:99}`,
		`summon tnt ~ ~ ~ {fuse:200,Motion:[0.0,0.5,0.0]}`,
		`summon falling_block ~ ~100 ~ {BlockState:{Name:"minecraft:gravel"}}`,
		`summon evoker_fangs ~ ~ ~ {Warmup:200}`,
		`summon ender_pearl ~ ~100 ~`,
		`summon iron_golem`,
		`summon villager`,
		`summon player`,
		`summon marker`,
		`summon pig 0 20000001 0`,
		`summon item ~ ~ ~ {Item:{id:"minecraft:stone",count:1,components:{}}}`,
	} {
		s.handleCommand(alice, cmd)
	}
	settle(t, h, logs, "S1")
	a := linesBetween(logs["alice"], "", "S1")
	for _, want := range []string{
		"Summoned new Z",
		"Unsupported NBT for minecraft:zombie: Foo, Fuse (this server does not model it)",
		"Summoned new Sheep", "Summoned new Wolf", "Summoned new Cow", "Summoned new Rabbit",
		"Summoned new Tnt", "Summoned new Falling Block", "Summoned new Evoker Fangs",
		"Summoned new Ender Pearl", "Summoned new Iron Golem", "Summoned new Villager",
		"Can't summon entity of type minecraft:player",
		"Unable to summon entity",
		"Invalid position for summon",
		"Unsupported NBT for minecraft:item: Item.components (this server does not model it)",
	} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		byType := map[int]*mob{}
		for _, m := range h.mobs {
			byType[m.etype] = m
		}
		z := byType[entityZombie]
		if z == nil || !z.noAI || !z.silent || !z.invulnerable || z.nameHidden || z.yaw != 90 || z.mobFlags()&mobFlagNoAI == 0 {
			t.Fatalf("zombie %+v", z)
		}
		if hurt, death, amb := h.mobSoundsFor(z); hurt+death+amb != "" {
			t.Error("a silent zombie still has a voice")
		}
		hp := z.health
		z.hurt(5)
		if z.health != hp {
			t.Error("an invulnerable zombie took a blow")
		}
		z.hurtKind(5, dtGenericKill)
		if z.health >= hp {
			t.Error("#bypasses_invulnerability should still hurt it")
		}
		if sh := byType[entitySheep]; sh == nil || sh.color != 14 {
			t.Errorf("sheep %+v", sh)
		}
		if w := byType[entityWolf]; w == nil || !w.variantSet || w.variant != wolfVariantID[wolfAshen] {
			t.Errorf("wolf %+v", w)
		}
		if c := byType[entityCow]; c == nil || !c.baby || c.growLeft != 24000 {
			t.Errorf("cow %+v", c)
		}
		if r := byType[entityRabbit]; r == nil || r.variant != rabbitEvil {
			t.Errorf("rabbit %+v", r)
		}
		if byType[entityIronGolem] == nil || byType[entityVillager] == nil {
			t.Error("an iron golem and a villager are summonable")
		}
		if len(h.tnt) != 1 || h.tnt[0].fuse < 150 {
			t.Errorf("tnt %+v", h.tnt)
		}
		gravel, _ := parseBlockState("gravel")
		if len(h.fallingBlocks) != 1 || h.fallingBlocks[0].state != gravel {
			t.Errorf("falling blocks %+v", h.fallingBlocks)
		}
		if len(h.fangs) != 1 {
			t.Errorf("%d fangs", len(h.fangs))
		}
		pearls := 0
		for _, ar := range h.arrows {
			if ar.etype == entityPearlProj && ar.pearl {
				pearls++
			}
		}
		if pearls != 1 {
			t.Errorf("%d pearls", pearls)
		}
	})

	// Peaceful turns the monsters away, not the animals.
	onHub(t, h, func() { h.rules.Difficulty = diffPeaceful })
	s.handleCommand(alice, "summon creeper")
	s.handleCommand(alice, "summon chicken")
	settle(t, h, logs, "S2")
	a = linesBetween(logs["alice"], "S1", "S2")
	if !hasLine(a, "Monsters cannot be summoned in Peaceful difficulty") || !hasLine(a, "Summoned new Chicken") {
		t.Errorf("peaceful: %q", a)
	}
}

// Every mob in the species table and every non-living type /summon lists is
// summonable, and the suggestions offer exactly those.
func TestSummonCoversTheModelledTypes(t *testing.T) {
	for et, d := range speciesTable {
		if _, ok := summonableType(d.name); !ok {
			t.Errorf("%s (%d) is modelled but not summonable", d.name, et)
		}
	}
	for _, n := range []string{"villager", "iron_golem", "blaze", "armor_stand", "item", "experience_orb",
		"falling_block", "evoker_fangs", "splash_potion", "lingering_potion", "experience_bottle", "ender_pearl",
		"tnt_minecart", "oak_chest_boat", "bamboo_raft", "wind_charge", "lightning_bolt"} {
		if _, ok := summonableType(n); !ok {
			t.Errorf("%s is not summonable", n)
		}
	}
	h := newTestHub(world.New(1))
	sugg := h.argCandidates(nil, "summon", nil)
	for _, c := range sugg {
		if c == "minecraft:player" || c == "minecraft:marker" {
			t.Errorf("%s offered for /summon", c)
		}
	}
}
