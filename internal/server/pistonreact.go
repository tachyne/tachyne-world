package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Piston push reactions and block-entity ownership. PistonBaseBlock.isPushable
// consults three things about a block: its PushReaction (PUSH_PULL by
// default; IMMOVEABLE never moves, POPPED breaks when pushed and cannot be
// pulled, PUSH moves only away from the piston), whether it can be broken at
// all, and whether it carries a block entity (chests, signs, banners… never
// move). The ranges are the game's own answers (pushreaction_gen.go).

type pushReaction uint8

const (
	pushNormal pushReaction = iota
	pushBlock
	pushDestroy
	pushOnly
)

// rangesOf collects the state spans of every named block that exists in the
// engine's tables (names from a newer or older version are skipped).
func rangesOf(names []string) []stateRange {
	var out []stateRange
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			out = append(out, stateRange{lo, hi})
		}
	}
	return out
}

func pushReactionOf(s uint32) pushReaction {
	switch {
	case inRanges(pushImmovableRanges, s):
		return pushBlock
	case inRanges(pushPoppedRanges, s):
		return pushDestroy
	case inRanges(pushOnlyRanges, s):
		return pushOnly
	}
	return pushNormal
}

func hasBlockEntity(s uint32) bool { return inRanges(blockEntityRanges, s) }
