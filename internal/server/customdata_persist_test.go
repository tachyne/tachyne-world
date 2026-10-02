package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// An entity's custom data compound (Entity.customData, /data … data and
// /summon {data:…}) is saved with the mob in mobs.json and with the player,
// tag types and all, and comes back on reload.
func TestCustomDataPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobs.json")
	players := map[int32]*tracked{}
	h := newTestHub(world.New(1))
	h.mobstore = newMobStore(path)
	cow := h.spawnMob(players, entityCow, 10.5, 70, 10.5)
	h.applySpecies(players, cow)
	old := h.entityNBT(cmdEntity{m: cow})
	tag := tagCopy(old).(map[string]any)
	tag["data"] = mustTyped(t, `{n: 3b, list: [1L, 2L], sub: {f: 1.5f}}`)
	h.setMobNBT(players, cow, old, tag) // what /data merge entity writes
	if len(cow.custom) == 0 {
		t.Fatal("the data compound was not taken")
	}
	active := map[[3]int32]bool{{0, 0, 0}: true}
	h.activeChunks = active
	h.mobstore.bucketLive(h.mobs, h.persistMob, active)
	h.mobstore.flush()

	h2 := newTestHub(world.New(1))
	h2.mobstore = newMobStore(path)
	h2.reconcileMobChunks(players, map[[3]int32]bool{{0, 0, 0}: true})
	var got *mob
	for _, m := range h2.mobs {
		if m.etype == entityCow {
			got = m
		}
	}
	if got == nil {
		t.Fatal("the cow did not reload")
	}
	if want := tag["data"]; !tagEqual(got.custom, want) {
		t.Errorf("reloaded data %s, want %s", tagString(got.custom), tagString(want))
	}
	if d := h2.entityNBT(cmdEntity{m: got})["data"]; !tagEqual(d, tag["data"]) {
		t.Errorf("/data get after reload shows %s", tagString(d))
	}

	ipath := filepath.Join(t.TempDir(), "inv.json")
	st := newInvStore(ipath)
	pl := testTracked()
	pl.custom = mustTyped(t, `{k: "v", i: 7}`).(map[string]any)
	st.save("Steve", pl)
	back := testTracked()
	newInvStore(ipath).loadInto(back, "Steve")
	if !tagEqual(back.custom, pl.custom) {
		t.Errorf("a player's data after reload: %s", tagString(back.custom))
	}
	if customDataSave(nil) != "" || customDataLoad("") != nil || customDataLoad("not snbt {") != nil {
		t.Error("empty or unreadable custom data")
	}
}

// /summon takes a data compound and keeps its tag types.
func TestSummonCustomData(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	s.handleCommand(ps["alice"], `summon pig 3 100 3 {data:{a:1b,b:"x"},Tags:["marked"]}`)
	settle(t, h, logs, "SD1")
	onHub(t, h, func() {
		found := false
		for _, m := range h.mobs {
			if m.etype == entityPig && m.tags["marked"] {
				found = true
				if want := mustTypedNoT(`{a:1b,b:"x"}`); !tagEqual(m.custom, want) {
					t.Errorf("summoned pig's data %s", tagString(m.custom))
				}
			}
		}
		if !found {
			t.Error("no pig was summoned")
		}
	})
}

func mustTypedNoT(s string) any {
	v, _ := parseSNBTTyped(s)
	return v
}
