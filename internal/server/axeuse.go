package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Axe and honeycomb use on a block — the vanilla AxeItem.useOn and
// HoneycombItem.useOn item actions. An axe strips a log/wood/stem/hyphae
// (STRIPPABLES, keeping the axis), scrapes one oxidation stage off copper
// (WeatheringCopper.getPrevious) or takes the wax off waxed copper
// (WAX_OFF_BY_BLOCK), in that order of preference; a honeycomb waxes copper
// (WAXABLES). Both run AFTER the block's own use (a door still opens, a chest
// still opens — sneak to reach the copper under it) and BEFORE block
// placement, which is where vanilla's InteractionResult chain puts item.useOn.
//
// Vanilla level events carry the particle burst: 3003 wax on, 3004 wax off,
// 3005 scrape. The item_used_on_block trigger fires with the block as it was
// BEFORE the change — that is the state the advancement predicates name
// (wax_on lists the unwaxed blocks, wax_off the waxed ones).

const (
	worldEventWaxOn  = 3003
	worldEventWaxOff = 3004
	worldEventScrape = 3005
)

type evUseAxe struct {
	eid     int32
	x, y, z int
	slot    int32
}

func (evUseAxe) isHubEvent() {}

type evUseHoneycomb struct {
	eid     int32
	x, y, z int
	slot    int32
}

func (evUseHoneycomb) isHubEvent() {}

// strippables maps every state of an unstripped log/wood/stem/hyphae/bamboo
// block to its stripped block with the same axis (AxeItem.getStripped).
var strippables = buildStrippables()

func buildStrippables() map[uint32]uint32 {
	m := map[uint32]uint32{}
	var pairs [][2]string
	for _, sp := range []string{"oak", "spruce", "birch", "jungle", "acacia",
		"dark_oak", "pale_oak", "mangrove", "cherry"} {
		pairs = append(pairs, [2]string{sp + "_log", "stripped_" + sp + "_log"},
			[2]string{sp + "_wood", "stripped_" + sp + "_wood"})
	}
	for _, sp := range []string{"crimson", "warped"} {
		pairs = append(pairs, [2]string{sp + "_stem", "stripped_" + sp + "_stem"},
			[2]string{sp + "_hyphae", "stripped_" + sp + "_hyphae"})
	}
	pairs = append(pairs, [2]string{"bamboo_block", "stripped_bamboo_block"})
	for _, pr := range pairs {
		lo, hi, ok := worldgen.BlockRangeOK(pr[0])
		slo, _, sok := worldgen.BlockRangeOK(pr[1])
		if !ok || !sok {
			continue
		}
		sinfo, ok := worldgen.InfoForState(slo)
		if !ok {
			continue
		}
		for st := lo; st <= hi; st++ {
			info, ok := worldgen.InfoForState(st)
			if !ok {
				continue
			}
			m[st] = worldgen.SetProperty(sinfo, slo, "axis", worldgen.GetProperty(info, st, "axis"))
		}
	}
	return m
}

// axeKind is which of the three axe actions applies to a block.
type axeKind int

const (
	axeNone axeKind = iota
	axeStrip
	axeScrape
	axeWaxOff
)

// axeResult is AxeItem.evaluateNewBlockState: the block an axe turns this
// state into, and which action that is.
func axeResult(state uint32) (uint32, axeKind) {
	if next, ok := strippables[state]; ok {
		return next, axeStrip
	}
	if next, ok := scrapedCopper(state); ok {
		return next, axeScrape
	}
	if next, ok := unwaxedCopper(state); ok {
		return next, axeWaxOff
	}
	return 0, axeNone
}

// tryAxeUse (connection side): an axe on a strippable, scrapable or waxed
// block hands the click to the hub. The block is acknowledged unchanged here;
// the hub's setBlockLive delivers the new state.
func (s *Server) tryAxeUse(p *player, x, y, z int, seq int32) bool {
	if !axeItems[p.heldItem()] {
		return false
	}
	state := s.worldFor(p).Block(x, y, z)
	if _, kind := axeResult(state); kind == axeNone {
		return false
	}
	s.hub.post(evUseAxe{eid: p.eid, x: x, y: y, z: z, slot: int32(p.held)})
	s.sendBlockChange(p, x, y, z, state, seq)
	return true
}

// tryHoneycombUse (connection side): honeycomb on unwaxed copper.
func (s *Server) tryHoneycombUse(p *player, x, y, z int, seq int32) bool {
	if p.heldItem() != itemHoneycomb {
		return false
	}
	state := s.worldFor(p).Block(x, y, z)
	if _, ok := waxedCopper(state); !ok {
		return false
	}
	s.hub.post(evUseHoneycomb{eid: p.eid, x: x, y: y, z: z, slot: int32(p.held)})
	s.sendBlockChange(p, x, y, z, state, seq)
	return true
}

// onUseAxe applies AxeItem.useOn on the hub: re-check the held tool and the
// block (both may have changed in flight), honour the shield-in-offhand
// intent rule, then swap the block, sound, particles, advancement and wear.
func (h *hub) onUseAxe(players map[int32]*tracked, e evUseAxe) {
	t := players[e.eid]
	if t == nil || t.dead {
		return
	}
	held := heldStack(t)
	if !axeItems[held.item] {
		return
	}
	// playerHasBlockingItemUseIntent: main hand with a shield in the offhand
	// and not sneaking means the player wants to block, not strip.
	if t.offhand.item == itemShield && !t.p.sneaking {
		return
	}
	state := h.worldFor(t.dim).At(e.x, e.y, e.z)
	next, kind := axeResult(state)
	if kind == axeNone {
		return
	}
	h.advance(players, t, "item_used_on_block", advMatch{blockState: state, item: held.item})
	x, y, z := float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5
	switch kind {
	case axeStrip:
		h.setBlockLive(players, t.dim, e.x, e.y, e.z, next)
		h.playSoundDim(players, t.dim, "minecraft:item.axe.strip", sndBlock, x, y, z, 1, 1)
	case axeScrape:
		h.copperFX(players, t, e.x, e.y, e.z, state, next, "minecraft:item.axe.scrape", worldEventScrape)
	case axeWaxOff:
		h.copperFX(players, t, e.x, e.y, e.z, state, next, "minecraft:item.axe.wax_off", worldEventWaxOff)
	}
	h.applyToolWear(t, int(e.slot), 1)
}

// onUseHoneycomb applies HoneycombItem.useOn on the hub.
func (h *hub) onUseHoneycomb(players map[int32]*tracked, e evUseHoneycomb) {
	t := players[e.eid]
	if t == nil || t.dead {
		return
	}
	held := heldStack(t)
	if held.item != itemHoneycomb {
		return
	}
	state := h.worldFor(t.dim).At(e.x, e.y, e.z)
	next, ok := waxedCopper(state)
	if !ok {
		return
	}
	h.advance(players, t, "item_used_on_block", advMatch{blockState: state, item: held.item})
	h.signConsume(t, e.slot) // survival: one honeycomb
	h.copperFX(players, t, e.x, e.y, e.z, state, next, "minecraft:item.honeycomb.wax_on", worldEventWaxOn)
}

// copperFX writes a copper change (and its double-chest partner's), then the
// sound at the clicked block and the particle level event at both halves —
// AxeItem.spawnSoundAndParticle / the tail of HoneycombItem.useOn.
func (h *hub) copperFX(players map[int32]*tracked, t *tracked, bx, by, bz int, old, next uint32, sound string, event int32) {
	partner, paired := h.setCopperState(players, t.dim, bx, by, bz, old, next)
	h.playSoundDim(players, t.dim, sound, sndBlock, float64(bx)+0.5, float64(by)+0.5, float64(bz)+0.5, 1, 1)
	h.toNearbyEv(players, t.dim, float64(bx), float64(bz), attachproto.WorldFX{Event: event, X: bx, Y: by, Z: bz})
	if paired {
		h.toNearbyEv(players, t.dim, float64(partner.x), float64(partner.z),
			attachproto.WorldFX{Event: event, X: partner.x, Y: partner.y, Z: partner.z})
	}
}
