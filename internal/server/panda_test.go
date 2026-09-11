package server

import (
	"math/rand"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Genes roll at vanilla's odds, resolve to a trait with recessives needing a
// match, a cub takes one gene from each parent, a weak panda has ten
// health, an aggressive one bites back, and the metadata carries both bytes.
func TestPandaGenes(t *testing.T) {
	if pandaTrait(packPandaGenes(pandaBrown, pandaLazy)) != pandaNormal {
		t.Error("a lone recessive brown gene shows normal")
	}
	if pandaTrait(packPandaGenes(pandaBrown, pandaBrown)) != pandaBrown {
		t.Error("two brown genes show brown")
	}
	if pandaTrait(packPandaGenes(pandaLazy, pandaBrown)) != pandaLazy {
		t.Error("a dominant main gene shows itself")
	}
	rng := rand.New(rand.NewSource(7))
	counts := map[int32]int{}
	for i := 0; i < 16000; i++ {
		counts[pandaGeneRandom(rng.Intn)]++
	}
	if counts[pandaWeak] < 4200 || counts[pandaWeak] > 5800 || counts[pandaNormal] < 4200 || counts[pandaNormal] > 5800 || counts[pandaBrown] < 1500 || counts[pandaAggressive] < 700 {
		t.Errorf("gene draw %v, want weak 5/16, normal 5/16, brown 2/16, aggressive 1/16", counts)
	}
	a, b := packPandaGenes(pandaLazy, pandaBrown), packPandaGenes(pandaWeak, pandaWeak)
	fromParents := 0
	for i := 0; i < 400; i++ {
		v := pandaCubGenes(a, b, rng.Intn)
		m, hd := pandaGenes(v)
		if (m == pandaLazy || m == pandaBrown || m == pandaWeak) && (hd == pandaLazy || hd == pandaBrown || hd == pandaWeak) {
			fromParents++
		}
	}
	if fromParents < 360 {
		t.Errorf("only %d of 400 cubs took both genes from their parents (mutation is 1/32 each)", fromParents)
	}

	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	weak := h.spawnMobIn(players, entityPanda, 0, 0, 70, 0)
	weak.variant, weak.variantSet = packPandaGenes(pandaWeak, pandaWeak), true
	h.applyPandaGenes(weak)
	if weak.maxHP() != pandaWeakHealth || weak.health > pandaWeakHealth {
		t.Errorf("weak panda health %d/%d", weak.health, weak.maxHP())
	}
	angry := h.spawnMobIn(players, entityPanda, 0, 0, 70, 0)
	angry.variant, angry.variantSet = packPandaGenes(pandaAggressive, pandaNormal), true
	h.applyPandaGenes(angry)
	if !angry.retaliates {
		t.Error("an aggressive panda bites back")
	}
	meta := variantMeta(angry)
	if meta == nil {
		t.Fatal("no variant metadata")
	}
	// eid varint, then [20, 0, main], [21, 0, hidden], 0xff
	tail := meta[len(meta)-7:]
	if tail[0] != metaIndexPandaMainGene || tail[2] != pandaAggressive || tail[3] != metaIndexPandaHiddenGene || tail[5] != pandaNormal || tail[6] != 0xff {
		t.Errorf("gene metadata tail %v", tail)
	}
	// Wild pandas roll genes at spawn.
	set := 0
	for i := 0; i < 20; i++ {
		p := h.spawnSpecies(players, entityPanda, 0, 0, 70, 0)
		if p.variantSet {
			set++
		}
	}
	if set != 20 {
		t.Errorf("%d of 20 spawned pandas carry genes", set)
	}
}
