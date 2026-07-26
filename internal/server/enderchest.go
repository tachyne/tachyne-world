package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The ender chest. The BLOCK is only a door: the 27 slots belong to the player
// (vanilla's PlayerEnderChestContainer), so every ender chest in every
// dimension shows the same contents, and what you leave in one is there when
// you open another a thousand blocks away.
//
// That is why this needs no per-position storage at all — it hangs the ordinary
// chest window off the player's own chest instead of a block's.

var enderChestMin, enderChestMax = worldgen.BlockRange("ender_chest")

// isEnderChest reports whether a state is an ender chest.
func isEnderChest(s uint32) bool { return s >= enderChestMin && s <= enderChestMax }

// enderChest is the player's own 27 slots, created on first use.
func (t *tracked) enderChest() *chest {
	if t.ender == nil {
		t.ender = &chest{}
	}
	return t.ender
}

// openEnderChest shows the player their own ender inventory.
func (h *hub) openEnderChest(players map[int32]*tracked, t *tracked, x, y, z int) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(players, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind = h.nextWin, blockPos{x, y, z}, winChest
	t.viewChest = t.enderChest()

	h.playSound(players, "minecraft:block.ender_chest.open", sndBlock,
		float64(x)+0.5, float64(y), float64(z)+0.5, 0.5, 1)
	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuGeneric9x3),
		Title: "Ender Chest"})
	h.sendChestWindow(t, t.viewChest)
}

// evOpenEnder asks the hub to show a player their ender inventory.
type evOpenEnder struct {
	eid     int32
	x, y, z int
}

func (evOpenEnder) isHubEvent() {}
