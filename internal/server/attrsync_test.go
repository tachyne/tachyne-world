package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/attribute"
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

// CAMERA_DISTANCE is syncable: a ridden happy ghast pulls the camera back
// to 8 and a giant's is 16; a mob that never set it sends nothing.
func TestCameraDistanceSyncs(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	cam := func(m *mob) (float64, bool) {
		for _, a := range mobAttrFrame(m).Attrs {
			if a.Name == "minecraft:camera_distance" {
				return a.Base, true
			}
		}
		return 0, false
	}
	if v, ok := cam(h.spawnSpecies(players, entityHappyGhast, 0, 0.5, 180, 0.5)); !ok || v != 8 {
		t.Fatalf("happy ghast camera_distance = %v (%v), want 8", v, ok)
	}
	if v, ok := cam(h.spawnSpecies(players, entityGiant, 0, 4.5, 180, 0.5)); !ok || v != 16 {
		t.Fatalf("giant camera_distance = %v (%v), want 16", v, ok)
	}
	if _, ok := cam(h.spawnSpecies(players, entityCow, 0, 8.5, 180, 0.5)); ok {
		t.Fatal("a cow never sets camera_distance")
	}
}

// 26.3's syncable newcomers (Attributes: bounciness, friction_modifier,
// air_drag_modifier, below_name_distance, name_tag_distance) reach the
// client once an entity carries them.
func TestNewSyncableAttributesSync(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnSpecies(players, entityCow, 0, 0.5, 180, 0.5)
	if m.attrs == nil {
		m.attrs = attribute.NewMap()
	}
	ids := []api.ID{api.Bounciness, api.FrictionModifier, api.AirDragModifier, api.BelowNameDistance, api.NameTagDistance}
	for _, id := range ids {
		m.attrs.SetBase(id, 0.5)
	}
	got := map[string]bool{}
	for _, a := range mobAttrFrame(m).Attrs {
		got[a.Name] = true
	}
	for _, id := range ids {
		if !got[string(id)] {
			t.Errorf("%s is syncable but was left out of the frame", id)
		}
	}
}

// X8: the species whose step is tuned by hand still sync vanilla's
// MOVEMENT_SPEED — a ridden nautilus steers by 1.0, a happy ghast by 0.05,
// a villager reads 0.5 — while they move at the tuned step.
func TestTunedSpeciesSyncVanillaSpeed(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	speedOf := func(m *mob) float64 {
		for _, a := range mobAttrFrame(m).Attrs {
			if a.Name == string(api.MovementSpeed) {
				return a.Base
			}
		}
		return -1
	}
	for _, c := range []struct {
		etype int
		want  float64
		step  float64
	}{
		{entityNautilus, 1.0, speciesOf(entityNautilus).step},
		{entityHappyGhast, 0.05, speciesOf(entityHappyGhast).step},
		{entityTurtle, 0.25, speciesOf(entityTurtle).step},
		{entityWither, 0.6, speciesOf(entityWither).step},
		{entityVillager, 0.5, villagerStep},
	} {
		m := h.spawnMob(players, c.etype, 0.5, 180, 0.5)
		if got := speedOf(m); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s syncs MOVEMENT_SPEED %v, want vanilla's %v", entityNameByID[c.etype], got, c.want)
		}
		if got := m.moveSpeed(); math.Abs(got-c.step) > 1e-9 {
			t.Errorf("%s moves at %v, want its tuned step %v", entityNameByID[c.etype], got, c.step)
		}
	}
}
