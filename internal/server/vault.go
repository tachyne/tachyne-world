package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The vault: the other half of a trial chamber. A spawner's fight pays out a
// trial key; a vault turns that key into loot — ONCE per player, forever. That
// last part is the whole design: a vault is not a chest to be emptied, it is a
// reward each player may claim exactly one of, which is why several people can
// run the same chamber and all be paid.
//
// Ported from VaultBlockEntity + VaultConfig.

// vaultState is VaultState.
type vaultState uint8

const (
	vaultInactive  vaultState = iota // nobody near
	vaultActive                      // someone in range, waiting for a key
	vaultUnlocking                   // key accepted, opening
	vaultEjecting                    // paying out
)

const (
	vaultActivateRange   = 4.0 // VaultConfig activationRange
	vaultDeactivateRange = 4.5 // …and deactivationRange, so it does not flicker
	vaultUnlockTicks     = 14  // UNLOCKING_DELAY_TICKS
	vaultEjectTicks      = 20  // how long the ejecting pose is held
)

var vaultMin, vaultMax = worldgen.BlockRange("vault")

// isVault reports whether a state is any vault.
func isVault(s uint32) bool { return s >= vaultMin && s <= vaultMax }

// vaultStateOrder is the block's vault_state values in registry order. The 32
// states are facing(4) x ominous(true,false) x these four, with vault_state
// varying fastest — so the offset within a facing is ominousIndex*4 + state.
//
// Computed rather than looked up: the same reason as the trial spawner, and
// here it also means the vault keeps whichever way it was facing.
var vaultStateOrder = []vaultState{vaultInactive, vaultActive, vaultUnlocking, vaultEjecting}

// vaultBlock returns the state id for a vault, keeping its facing.
func vaultBlock(cur uint32, ominous bool, st vaultState) uint32 {
	if !isVault(cur) {
		return cur
	}
	const perFacing = 8 // ominous(2) x vault_state(4)
	facing := (cur - vaultMin) / perFacing
	off := uint32(0) // ominous=true comes FIRST
	if !ominous {
		off = uint32(len(vaultStateOrder))
	}
	return vaultMin + facing*perFacing + off + uint32(st)
}

// vaultOminousAt reports whether the vault standing at a state is the ominous
// variant — the chamber places both kinds, and they take different keys.
func vaultOminousAt(cur uint32) bool {
	if !isVault(cur) {
		return false
	}
	const perFacing = 8
	return (cur-vaultMin)%perFacing < uint32(len(vaultStateOrder))
}

// vaultMaxRewarded is VaultServerData.MAX_REWARD_PLAYERS: past this many
// claimants the oldest is forgotten, so one vault's record cannot grow without
// bound on a long-lived world.
const vaultMaxRewarded = 128

// vaultRecord is one vault's live state plus who has already claimed it.
type vaultRecord struct {
	pos     blockPos
	ominous bool
	state   vaultState
	until   uint64 // tick the unlocking/ejecting pose ends
	// Player UUIDs, not eids: this outlives a session AND a restart (it is
	// written to containers.json). Ordered oldest-first like vanilla's linked
	// set, because the cap evicts by insertion order.
	rewarded [][16]byte
}

// hasRewarded reports whether this player has already had their share.
func (v *vaultRecord) hasRewarded(u [16]byte) bool {
	for _, r := range v.rewarded {
		if r == u {
			return true
		}
	}
	return false
}

// addRewarded records a claim, forgetting the oldest past the cap.
func (v *vaultRecord) addRewarded(u [16]byte) {
	if v.hasRewarded(u) {
		return
	}
	v.rewarded = append(v.rewarded, u)
	if n := len(v.rewarded) - vaultMaxRewarded; n > 0 {
		v.rewarded = append(v.rewarded[:0], v.rewarded[n:]...)
	}
}

// vaultKeyFor is the item a vault of this kind opens with.
func vaultKeyFor(ominous bool) int32 {
	if ominous {
		return int32(itemByName["ominous_trial_key"])
	}
	return int32(itemByName["trial_key"])
}

// vaultLootFor is the table it pays from.
func vaultLootFor(ominous bool) string {
	if ominous {
		return "chests/trial_chambers/reward_ominous"
	}
	return "chests/trial_chambers/reward"
}

// vaultAt returns a vault's record, created on first sight.
func (h *hub) vaultAt(pos blockPos, ominous bool) *vaultRecord {
	if h.vaults == nil {
		h.vaults = map[blockPos]*vaultRecord{}
	}
	v := h.vaults[pos]
	if v == nil {
		v = &vaultRecord{pos: pos}
		h.vaults[pos] = v
	}
	v.ominous = ominous // the block is authoritative; a restored record holds only claims
	return v
}

// updateVaults lights the vaults near players and lets their poses lapse.
// Found from worldgen like the spawners, so no block scan.
func (h *hub) updateVaults(players map[int32]*tracked) {
	gen := h.world.Gen()
	now := h.tick.Load()
	seen := map[blockPos]bool{}
	for _, t := range players {
		if t.dim != 0 || t.dead {
			continue
		}
		tc := gen.TrialChamberIn(int(t.x), int(t.z))
		if !tc.Exists {
			continue
		}
		for _, f := range gen.TrialChamberVaults(tc) {
			pos := blockPos{f.X, f.Y, f.Z}
			if seen[pos] || !h.ownedBlock(f.X, f.Z) {
				continue
			}
			seen[pos] = true
			cur := h.world.At(f.X, f.Y, f.Z)
			if !isVault(cur) {
				delete(h.vaults, pos) // mined out
				continue
			}
			h.tickVault(players, h.vaultAt(pos, vaultOminousAt(cur)), cur, now)
		}
	}
}

// tickVault runs one vault's state.
func (h *hub) tickVault(players map[int32]*tracked, v *vaultRecord, cur uint32, now uint64) {
	switch v.state {
	case vaultUnlocking:
		if now >= v.until {
			v.state, v.until = vaultEjecting, now+vaultEjectTicks
			h.ejectVaultReward(players, v)
		}
	case vaultEjecting:
		if now >= v.until {
			v.state = vaultInactive
		}
	default:
		// Active while someone is close, with a wider range to switch off so it
		// does not flicker on the threshold.
		near := h.playerNearVault(players, v, vaultActivateRange)
		if v.state == vaultActive {
			near = h.playerNearVault(players, v, vaultDeactivateRange)
		}
		if near {
			v.state = vaultActive
		} else {
			v.state = vaultInactive
		}
	}
	if next := vaultBlock(cur, v.ominous, v.state); next != cur {
		h.setBlockAt(players, 0, v.pos, next)
	}
}

func (h *hub) playerNearVault(players map[int32]*tracked, v *vaultRecord, r float64) bool {
	for _, t := range players {
		if t.dim == 0 && !t.dead && t.gamemode != gmSpectator &&
			dist3(t.x, t.y, t.z, float64(v.pos.x)+0.5, float64(v.pos.y), float64(v.pos.z)+0.5) <= r {
			return true
		}
	}
	return false
}

// useVault is the right-click: hand over a key and take the reward, once.
func (h *hub) useVault(players map[int32]*tracked, t *tracked, pos blockPos) {
	cur := h.world.At(pos.x, pos.y, pos.z)
	if !isVault(cur) || t.inv == nil {
		return
	}
	v := h.vaultAt(pos, vaultOminousAt(cur))
	if v.state != vaultActive {
		return // still opening, or nobody is close enough for it to be awake
	}
	if v.hasRewarded(t.p.uuid) {
		// VAULT_REJECT_REWARDED_PLAYER: you have had your share of this one.
		h.playSound(players, "minecraft:block.vault.reject_rewarded_player", sndBlock,
			float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, 1, 1)
		return
	}
	held := heldStack(t)
	if held.item != vaultKeyFor(v.ominous) || held.count <= 0 {
		h.playSound(players, "minecraft:block.vault.insert_item_fail", sndBlock,
			float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, 1, 1)
		return
	}
	// The key is spent and the player is recorded — permanently, by UUID, so it
	// survives a relog and a restart.
	slot := t.p.heldSlot()
	held.count--
	if held.count <= 0 {
		held = invStack{}
	}
	t.inv.slots[slot] = held
	h.sendSlot(t, slot)

	v.addRewarded(t.p.uuid)
	v.state, v.until = vaultUnlocking, h.tick.Load()+vaultUnlockTicks
	h.playSound(players, "minecraft:block.vault.insert_item", sndBlock,
		float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, 1, 1)
	if next := vaultBlock(cur, v.ominous, v.state); next != cur {
		h.setBlockAt(players, 0, pos, next)
	}
}

// ejectVaultReward rolls the vault's table and drops it in front of the block.
func (h *hub) ejectVaultReward(players map[int32]*tracked, v *vaultRecord) {
	tbl, ok := lootForChest(vaultLootFor(v.ominous))
	if !ok {
		return
	}
	ctx := &lootCtx{rng: h.rng.Intn, randf: h.rng.Float64}
	for _, st := range h.evalChestStacks(tbl, ctx, 0) {
		if st.item == 0 || st.count <= 0 {
			continue
		}
		it := h.spawnItem(players, st.item, st.count,
			float64(v.pos.x)+0.5, float64(v.pos.y)+1, float64(v.pos.z)+0.5)
		if it != nil {
			it.dmg, it.ench = st.dmg, st.ench
		}
	}
	h.playSound(players, "minecraft:block.vault.eject_item", sndBlock,
		float64(v.pos.x)+0.5, float64(v.pos.y), float64(v.pos.z)+0.5, 1, 1)
}

// evUseVault asks the hub to run a right-click on a vault.
type evUseVault struct {
	eid     int32
	x, y, z int
}

func (evUseVault) isHubEvent() {}
