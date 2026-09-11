package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	api "github.com/tachyne/tachyne-world/plugin/attribute"
)

// TestAttributeFrames: a horse's rolled speed and jump reach its frame, a
// player's max health carries its effect modifier, unsyncable attributes
// stay out, and the fingerprint only changes when something does.
func TestAttributeFrames(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	horse := h.spawnAnimal(players, entityHorse, 3, 3)
	h.rollHorseAttributes(horse) // the natural-spawn roll (spawnAnimal is the bare species)
	fr := mobAttrFrame(horse)
	has := func(fr attachproto.EntityAttributes, name string) *attachproto.AttributeSnapshot {
		for i := range fr.Attrs {
			if fr.Attrs[i].Name == name {
				return &fr.Attrs[i]
			}
		}
		return nil
	}
	if has(fr, "minecraft:movement_speed") == nil || has(fr, "minecraft:jump_strength") == nil || has(fr, "minecraft:max_health") == nil {
		t.Fatalf("a horse's frame should carry speed, jump and health: %+v", fr.Attrs)
	}
	if sp := has(fr, "minecraft:movement_speed").Base; sp < 0.1125 || sp > 0.3375 {
		t.Fatalf("the frame's speed is vanilla's figure, not the per-step one: %v", sp)
	}
	if j := has(fr, "minecraft:jump_strength").Base; j < 0.4 || j > 1 {
		t.Fatalf("jump strength %v", j)
	}
	// The roll survives a save and reload.
	sm := toSavedMob(horse)
	h2 := newHub(world.New(1))
	h2.reloading = true
	back := h2.reloadMob(players, &sm)
	if back.moveSpeed() != horse.moveSpeed() || back.jumpStrength() != horse.jumpStrength() {
		t.Fatalf("reload: speed %v/%v jump %v/%v", back.moveSpeed(), horse.moveSpeed(), back.jumpStrength(), horse.jumpStrength())
	}
	if has(fr, "minecraft:attack_damage") != nil || has(fr, "minecraft:follow_range") != nil {
		t.Fatalf("attack_damage and follow_range are not syncable: %+v", fr.Attrs)
	}
	fp := attrFingerprint(fr)
	if attrFingerprint(mobAttrFrame(horse)) != fp {
		t.Fatal("the fingerprint must be stable")
	}
	horse.setMaxHP(28)
	if attrFingerprint(mobAttrFrame(horse)) == fp {
		t.Fatal("a changed base must change the fingerprint")
	}
	// A player: health boost shows as a modifier on max_health.
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pl.playerAttrs().Get(api.MaxHealth).AddModifier(api.Modifier{Source: "effect:health_boost", Amount: 4, Op: api.AddValue})
	pf := playerAttrFrame(pl)
	mh := has(pf, "minecraft:max_health")
	if mh == nil || mh.Base != 20 || len(mh.Modifiers) != 1 || mh.Modifiers[0].Amount != 4 || mh.Modifiers[0].Op != 0 {
		t.Fatalf("player max_health snapshot: %+v", mh)
	}
	// The sweep sends once, then stays quiet until something changes.
	h.syncAttributes(players)
	sent := pl.attrSent
	if sent == 0 {
		t.Fatal("the sweep should record what it sent")
	}
	h.syncAttributes(players)
	if pl.attrSent != sent {
		t.Fatal("nothing changed: the print stays")
	}
}
