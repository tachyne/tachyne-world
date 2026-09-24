package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Lids. Vanilla's chest, trapped chest, ender chest and shulker box block
// entities count their openers and send a block event (action 1, the
// count) on every change; the client animates the lid — or the shulker's
// shell — from it. Barrels use their block state instead (closeBarrel).

// lidEvent sends the container's opener count as its block event, to
// everyone near it.
func (h *hub) lidEvent(players map[int32]*tracked, pos simPos) {
	st := h.worldFor(pos.dim).At(pos.x, pos.y, pos.z)
	if !isChestBlock(st) && !isTrappedChest(st) && !isEnderChest(st) && !isShulkerBox(st) {
		return
	}
	name, _ := worldgen.StateName(st)
	id, ok := worldgen.BlockRegistryID(name)
	if !ok {
		return
	}
	n := 0
	for _, t := range players {
		if t.winKind == winChest && t.winPos == pos || t.winKind == winDoubleChest && (t.winPos == pos || t.winPos2 == pos) {
			n++
		}
	}
	if isShulkerBox(st) {
		h.shulkerLidEvent(pos, n) // the server runs the lid too (shulkerlid.go)
	}
	h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), attachproto.BlockEvent{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Action: 1, Param: uint8(n), Block: int32(id)})
}

// containerSound is the open (or close) sound of a container block.
func containerSound(st uint32, open bool) string {
	oc := "close"
	if open {
		oc = "open"
	}
	switch {
	case isShulkerBox(st):
		return "minecraft:block.shulker_box." + oc
	case isEnderChest(st):
		return "minecraft:block.ender_chest." + oc
	case isBarrel(st):
		return "minecraft:block.barrel." + oc
	}
	return "minecraft:block.chest." + oc
}

// containerSoundAt plays a container's open/close sound to everyone near
// it, at vanilla's 0.5 volume and 0.9–1.0 pitch.
func (h *hub) containerSoundAt(players map[int32]*tracked, pos simPos, open bool) {
	st := h.worldFor(pos.dim).At(pos.x, pos.y, pos.z)
	h.playSoundDim(players, pos.dim, containerSound(st, open), sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 0.9+h.rng.Float32()*0.1)
}
