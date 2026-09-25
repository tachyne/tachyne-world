package server

import (
	"path/filepath"
	"testing"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// A player's /attribute changes outlive a relog: the base value and the
// command's modifier are saved with the player's data and come back on a
// fresh load from disk, and gear-style modifiers are not saved. A death
// keeps the base and drops the modifier (ServerPlayer.restoreFrom).
func TestAttributeCommandPersists(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, "attribute bob minecraft:max_health base set 30")
	s.handleCommand(alice, "attribute bob minecraft:scale modifier add minecraft:big 1 add_value")
	settle(t, h, logs, "A1")
	path := filepath.Join(t.TempDir(), "inventories.json")
	onHub(t, h, func() {
		var bob *tracked
		for _, tr := range h.playersRef {
			if tr.p.name == "bob" {
				bob = tr
			}
		}
		bob.playerAttrs().Get(attr.MovementSpeed).AddModifier(attr.Modifier{Source: "gear:boots", Amount: 0.5, Op: attr.AddValue})
		st := newInvStore(path)
		st.save("bob", bob)
		fresh := &tracked{living: living{attrs: newPlayerAttributes()}, p: bob.p, gamemode: gmSurvival}
		initSurvival(fresh)
		newInvStore(path).loadInto(fresh, "bob")
		a := fresh.playerAttrs()
		if v := a.Get(attr.MaxHealth).Base(); v != 30 {
			t.Errorf("max_health base after relog: %v, want 30", v)
		}
		if v := a.Value(attr.Scale); v != 2 {
			t.Errorf("scale after relog: %v, want 2 (the command's +1 kept)", v)
		}
		if a.Get(attr.MovementSpeed).HasModifier("gear:boots") {
			t.Error("a transient modifier was saved")
		}
		fresh.dead = true
		h.respawn(fresh)
		if v := fresh.playerAttrs().Get(attr.MaxHealth).Base(); v != 30 {
			t.Errorf("max_health base after death: %v, want 30", v)
		}
		if v := fresh.playerAttrs().Value(attr.Scale); v != 1 {
			t.Errorf("scale after death: %v, want 1 (the modifier dropped)", v)
		}
	})
}
