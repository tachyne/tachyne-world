package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The abandoned camp's maps carry their set_name (item_name) text: a
// camp's own map is a "… Camp Map", not an unnamed map.
func TestCampMapsAreNamed(t *testing.T) {
	tbl, ok := lootForChest("chests/abandoned_camp_common_chest")
	if !ok {
		t.Skip("no abandoned camp table")
	}
	h := newHub(world.New(1))
	named := 0
	for _, p := range tbl.Pools {
		for _, e := range p.Entries {
			for _, f := range e.Functions {
				if f.F != "set_name" {
					continue
				}
				st := h.applyChestExtraFn(&lootCtx{rng: h.rng.Intn, randf: h.rng.Float64}, &f, invStack{item: e.ID, count: 1})
				if !strings.HasSuffix(st.name, "Camp Map") {
					t.Errorf("a camp map named %q", st.name)
				}
				named++
			}
		}
	}
	if named != 8 {
		t.Fatalf("%d named camp maps, want the 8 biome camps", named)
	}
}
