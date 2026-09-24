package server

import (
	"math"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// How fast a player breaks a block, as vanilla works it out every tick of a
// dig (BlockBehaviour.getDestroyProgress over Player.getDestroySpeed). The
// held item's tool component names the blocks it is fast on: a pickaxe its
// material's speed on #mineable/pickaxe and nothing anywhere else, a sword
// 15 on cobweb and 1.5 on #sword_efficient, shears 15, 5 or 2 on their three
// tags. Everything else, and the hand, is speed 1. It drives the crack stages
// other players see (digcracks.go).

// toolMaterialSpeed is ToolMaterial's speed, by the item name's prefix.
var toolMaterialSpeed = map[string]float64{
	"wooden": 2, "stone": 4, "copper": 5, "iron": 6, "diamond": 8, "netherite": 9, "golden": 12,
}

// toolRule is one Tool.Rule with a speed: the blocks it covers and the speed there.
type toolRule struct {
	blocks map[uint32]bool
	speed  float64
}

var tagStateSets = map[string]map[uint32]bool{}

// tagStates is a block tag as a state set, built once.
func tagStates(tag string) map[uint32]bool {
	if m, ok := tagStateSets[tag]; ok {
		return m
	}
	m := map[uint32]bool{}
	for _, r := range worldgen.BlockTag(tag) {
		for s := r[0]; s <= r[1]; s++ {
			m[s] = true
		}
	}
	tagStateSets[tag] = m
	return m
}

// toolRules are an item's speed rules in order (the first match wins), or
// nil for an item with no tool component.
var toolRules = func() map[int32][]toolRule {
	out := map[int32][]toolRule{}
	for name, id := range itemByName {
		i := strings.LastIndexByte(name, '_')
		if i < 0 {
			if name == "shears" {
				out[id] = []toolRule{{map[uint32]bool{cobwebState: true}, 15},
					{tagStates("shears_extreme_breaking_speed"), 15},
					{tagStates("shears_major_breaking_speed"), 5},
					{tagStates("shears_minor_breaking_speed"), 2}}
			}
			continue
		}
		speed, ok := toolMaterialSpeed[name[:i]]
		if !ok {
			continue
		}
		switch kind := name[i+1:]; kind {
		case "pickaxe", "axe", "shovel", "hoe":
			out[id] = []toolRule{{tagStates("mineable/" + kind), speed}}
		case "sword":
			out[id] = []toolRule{{map[uint32]bool{cobwebState: true}, 15},
				{tagStates("sword_instantly_mines"), math.MaxFloat32},
				{tagStates("sword_efficient"), 1.5}}
		}
	}
	return out
}()

// itemMiningSpeed is ItemStack.getDestroySpeed: the first matching rule's
// speed, or the tool's default of 1.
func itemMiningSpeed(held int32, state uint32) float64 {
	for _, r := range toolRules[held] {
		if r.blocks[state] {
			return r.speed
		}
	}
	return 1
}

// destroyProgress is the fraction of a block one tick of digging takes off
// (getDestroyProgress): 0 for an unbreakable block.
func (h *hub) destroyProgress(t *tracked, state uint32) float64 {
	hardness := float64(worldgen.Hardness(state))
	if hardness < 0 || !worldgen.Diggable(state) {
		return 0
	}
	if hardness == 0 {
		return 1
	}
	held := heldStack(t)
	speed := itemMiningSpeed(held.item, state)
	if speed > 1 {
		speed += float64(efficiencyBonus(held)) // MINING_EFFICIENCY
	}
	// MobEffectUtil.hasDigSpeed: Haste or Conduit Power, the stronger.
	if dig := max(t.hasEffect(effHaste), t.hasEffect(effConduitPower)); dig > 0 {
		speed *= 1 + float64(dig)*0.2
	}
	if f := t.hasEffect(effMiningFatigue); f > 0 {
		speed *= math.Pow(0.3, float64(f))
	}
	// BLOCK_BREAK_SPEED multiplies whatever the tool and effects made of it
	// (1 unless a command, an item or a plugin has changed it).
	speed *= t.playerAttrs().Value(attr.BlockBreakSpeed)
	// SUBMERGED_MINING_SPEED is 0.2 unless the helmet has Aqua Affinity.
	if h.inWater(t.dim, t.x, t.y+1.62, t.z) && t.armor[0].enchLvl(enchAquaAffinity) == 0 {
		speed *= 0.2
	}
	if !t.onGround {
		speed /= 5
	}
	mod := 30.0
	if !worldgen.HarvestableBy(state, uint16(held.item)) {
		mod = 100 // hasCorrectToolForDrops
	}
	return speed / hardness / mod
}
