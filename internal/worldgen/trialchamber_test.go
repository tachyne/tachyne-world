package worldgen

import (
	"strings"
	"testing"
)

// Trial chambers must actually contain trial spawners. They did not for a long
// time: the spawner connectors name a pool that deliberately does not exist
// (trial_chambers/spawner/contents/melee and friends), and the structure binds
// those aliases to real pools once per chamber. With no alias support the
// connectors resolved to nothing, so chambers grew corridors and vaults and
// never a single spawner.
func TestTrialChambersGenerateSpawners(t *testing.T) {
	g := NewGenerator(12345)
	chambers, spawners, vaults := 0, 0, 0
	for r := 0; r < 12 && chambers < 4; r++ {
		tc := g.TrialChamberIn(r*1024, r*1024)
		if !tc.Exists {
			continue
		}
		chambers++
		spawners += len(g.TrialChamberSpawners(tc))
		vaults += len(g.TrialChamberVaults(tc))
	}
	if chambers == 0 {
		t.Fatal("no trial chamber generated in the sampled range")
	}
	if spawners == 0 {
		t.Errorf("%d chambers generated %d spawners — the pool aliases are not resolving", chambers, spawners)
	}
	if vaults == 0 {
		t.Errorf("%d chambers generated no vaults", chambers)
	}
}

// The alias table has to load and bind every alias the connectors reference,
// or one family of spawner silently never appears.
func TestPoolAliasesCoverEverySpawnerFamily(t *testing.T) {
	g := NewGenerator(7)
	got := resolveAliases("trial_chambers", newJigsawRNG(g.seed, 0, 0))
	for _, alias := range []string{
		"trial_chambers/spawner/contents/melee",
		"trial_chambers/spawner/contents/ranged",
		"trial_chambers/spawner/contents/slow_ranged",
		"trial_chambers/spawner/contents/small_melee",
	} {
		target, ok := got[alias]
		if !ok {
			t.Errorf("%s is unbound — its connectors will place nothing", alias)
			continue
		}
		if pools[target] == nil {
			t.Errorf("%s binds to %q, which is not a pool", alias, target)
		}
	}
	// The ranged pair is bound as a GROUP so a chamber's archers match.
	rg, sr := got["trial_chambers/spawner/contents/ranged"], got["trial_chambers/spawner/contents/slow_ranged"]
	rk := strings.TrimPrefix(rg, "trial_chambers/spawner/ranged/")
	sk := strings.TrimPrefix(sr, "trial_chambers/spawner/slow_ranged/")
	if rk != sk {
		t.Errorf("ranged %q and slow_ranged %q disagree — they are bound as one group", rk, sk)
	}
}

// Every property a template records must survive being stamped. resolveState
// only applies properties for blocks InfoForState knows, and that table used to
// hold only ORIENTABLE blocks — so a rail's shape, farmland's moisture, snow
// layers and a trial spawner's state were all silently dropped, and 43 blocks
// stamped at their base state instead.
func TestTemplatePropertiesAreNotDropped(t *testing.T) {
	lost := map[string]bool{}
	for _, tm := range templates {
		for _, p := range tm.Palette {
			if len(p.Props) == 0 {
				continue
			}
			base := safeBase(trimNS(p.Name))
			if base == tmplSkip {
				continue
			}
			if _, ok := InfoForState(base); !ok {
				lost[trimNS(p.Name)] = true
			}
		}
	}
	for name := range lost {
		t.Errorf("%s carries template properties that resolveState will drop", name)
	}
}
