package server

import "testing"

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
		"Summoned new Armor Stand", "Summoned new Oak Boat", "Summoned new Item", "Unable to summon entity",
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
