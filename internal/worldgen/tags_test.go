package worldgen

import "testing"

// Every vanilla block tag is generated, nested tags resolved, and each member
// is a block the engine knows.
func TestBlockTagsAreComplete(t *testing.T) {
	if len(blockTags) < 300 {
		t.Errorf("%d block tags generated, want every 26.3 tag (300)", len(blockTags))
	}
	for name, members := range blockTags { // some are empty in vanilla (incorrect_for_netherite_tool)
		for _, m := range members {
			if _, _, ok := BlockRangeOK(m); !ok {
				t.Errorf("block tag %s: member %s is not a block", name, m)
			}
		}
	}
	// #logs holds #oak_logs … #crimson_stems, resolved to their blocks.
	has := func(tag, block string) bool {
		for _, n := range BlockTagNames(tag) {
			if n == block {
				return true
			}
		}
		return false
	}
	for _, c := range [][2]string{{"logs", "oak_log"}, {"logs", "stripped_warped_hyphae"}, {"logs", "poplar_wood"},
		{"mineable/axe", "oak_planks"}, {"wool", "white_wool"}, {"base_stone_overworld", "deepslate"}} {
		if !has(c[0], c[1]) {
			t.Errorf("#%s lacks %s", c[0], c[1])
		}
	}
}
