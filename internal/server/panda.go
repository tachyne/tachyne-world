package server

import "github.com/tachyne/tachyne-common/protocol"

// Panda genes (Panda.Gene). Every panda carries a main and a hidden gene;
// the look and temper come from the main gene unless it is recessive (brown,
// weak) and the hidden gene differs, in which case the panda is normal. A
// wild panda rolls both from Gene.getRandom (of sixteen: lazy 1, worried 1,
// playful 1, aggressive 1, weak 5 — vanilla's n == 3 falls through to weak —
// brown 2, normal 5); a cub takes one gene
// from each parent (either of theirs, at random), each with a one-in-32
// mutation. A weak panda has ten health; an aggressive one bites back.
// Both genes ride the variant field (main | hidden<<8) and the two gene
// bytes of the metadata.

const (
	pandaNormal     = 0
	pandaLazy       = 1
	pandaWorried    = 2
	pandaPlayful    = 3
	pandaBrown      = 4
	pandaWeak       = 5
	pandaAggressive = 6

	metaIndexPandaMainGene   = 20 // Panda MAIN_GENE_ID (byte); 26.2 shifts it with the ageable fields
	metaIndexPandaHiddenGene = 21
	pandaWeakHealth          = 10
)

func pandaRecessive(g int32) bool { return g == pandaBrown || g == pandaWeak }

// pandaGeneRandom is Gene.getRandom.
func pandaGeneRandom(rng func(int) int) int32 {
	switch n := rng(16); {
	case n == 0:
		return pandaLazy
	case n == 1:
		return pandaWorried
	case n == 2:
		return pandaPlayful
	case n == 4:
		return pandaAggressive
	case n < 9:
		return pandaWeak
	case n < 11:
		return pandaBrown
	}
	return pandaNormal
}

func pandaGenes(v int32) (main, hidden int32) { return v & 0xff, (v >> 8) & 0xff }
func packPandaGenes(main, hidden int32) int32 { return main | hidden<<8 }

// pandaTrait is Gene.getVariantFromGenes: what the panda shows and does.
func pandaTrait(v int32) int32 {
	main, hidden := pandaGenes(v)
	if pandaRecessive(main) && main != hidden {
		return pandaNormal
	}
	return main
}

// pandaOneOfGenes is getOneOfGenesRandomly: the main or the hidden gene.
func pandaOneOfGenes(v int32, rng func(int) int) int32 {
	main, hidden := pandaGenes(v)
	if rng(2) == 0 {
		return main
	}
	return hidden
}

// pandaCubGenes is setGeneFromParents.
func pandaCubGenes(a, b int32, rng func(int) int) int32 {
	var main, hidden int32
	if rng(2) == 0 {
		main, hidden = pandaOneOfGenes(a, rng), pandaOneOfGenes(b, rng)
	} else {
		main, hidden = pandaOneOfGenes(b, rng), pandaOneOfGenes(a, rng)
	}
	if rng(32) == 0 {
		main = pandaGeneRandom(rng)
	}
	if rng(32) == 0 {
		hidden = pandaGeneRandom(rng)
	}
	return packPandaGenes(main, hidden)
}

// applyPandaGenes is Panda.setAttributes plus the temper: the weak trait's
// ten health and the aggressive trait's retaliation.
func (h *hub) applyPandaGenes(m *mob) {
	switch pandaTrait(m.variant) {
	case pandaWeak:
		m.setMaxHP(pandaWeakHealth)
		if m.health > pandaWeakHealth {
			m.health = pandaWeakHealth
		}
	case pandaAggressive:
		m.retaliates = true
	}
}

// pandaGeneMeta is the two gene bytes.
func pandaGeneMeta(b []byte, m *mob) []byte {
	main, hidden := pandaGenes(m.variant)
	b = protocol.AppendU8(b, metaIndexPandaMainGene)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, byte(main))
	b = protocol.AppendU8(b, metaIndexPandaHiddenGene)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, byte(hidden))
	return b
}
