package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"log"

	"tachyne/internal/worldgen"
)

// Furnaces: right-click opens the furnace menu; the hub owns per-position
// furnace state (input/fuel/output + burn/cook progress) and steps it every
// tick with vanilla rules — fuel ignites only when something can smelt, cooking
// takes the recipe's time (usually 200 ticks), progress decays without heat.
// The block's "lit" state flips with the flame so it glows for everyone.
// Contents + burn state persist via containerStore (containers.json).

const (
	menuFurnace = 14 // minecraft:furnace menu id (same through 26.2)

	furnaceInput  = 0
	furnaceFuel   = 1
	furnaceOutput = 2

	// Furnace container properties (progress bars).
	propBurnLeft = 0
	propBurnMax  = 1
	propCook     = 2
	propCookMax  = 3
)

var (
	furnaceStateMin = worldgen.BlockBase("furnace") // furnace block states: facing (4) x lit (2)
	furnaceStateMax = worldgen.BlockBase("furnace") + 7
)

type furnace struct {
	slots    [3]invStack // input, fuel, output
	burnLeft int         // ticks of current fuel remaining
	burnMax  int         // total ticks of the current fuel (for the flame bar)
	cook     int         // progress toward the current smelt
	cookMax  int         // the current recipe's cook time
	xpBank   float64     // smelting XP owed, paid out when the output is taken
	viewer   int32       // eid of the player with the window open (0 = none)
	resync   int         // ticks until the lit-state block update is re-broadcast
	lastBars [4]int      // last progress-bar values sent (only send on change)
}

type evOpenFurnace struct {
	eid     int32
	x, y, z int
}

func (evOpenFurnace) isHubEvent() {}

// openFurnace opens the furnace window at a block position for a player.
func (h *hub) openFurnace(t *tracked, x, y, z int) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	pos := blockPos{x, y, z}
	f := h.furnaces[pos]
	if f == nil {
		f = &furnace{cookMax: 200}
		h.furnaces[pos] = f
	}
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind = h.nextWin, pos, winFurnace
	f.viewer = t.p.eid
	f.lastBars = [4]int{-1, -1, -1, -1} // force a full bar sync to the new window

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuFurnace), Title: "Furnace"})
	h.sendFurnaceWindow(t, f)
}

// updateFurnaces steps every furnace one tick: vanilla smelt/fuel rules, block
// lit-state flips, and live progress-bar/slot sync to the viewing player.
func (h *hub) updateFurnaces(players map[int32]*tracked) {
	for pos, f := range h.furnaces {
		wasLit := f.burnLeft > 0
		changedSlots := false

		out, canSmelt := smeltResult[f.slots[furnaceInput].item]
		canCook := canSmelt && f.slots[furnaceInput].count > 0 &&
			(f.slots[furnaceOutput].item == 0 ||
				(f.slots[furnaceOutput].item == out.Out && f.slots[furnaceOutput].count < stackCap(out.Out)))

		if f.burnLeft > 0 {
			f.burnLeft--
		}
		// Ignite new fuel only when there's something to cook (vanilla).
		if f.burnLeft == 0 && canCook {
			if ticks := fuelTicks[f.slots[furnaceFuel].item]; ticks > 0 && f.slots[furnaceFuel].count > 0 {
				f.burnLeft, f.burnMax = ticks, ticks
				if f.slots[furnaceFuel].count--; f.slots[furnaceFuel].count == 0 {
					f.slots[furnaceFuel].item = 0
				}
				changedSlots = true
			}
		}
		switch {
		case f.burnLeft > 0 && canCook:
			f.cookMax = out.Cook
			if f.cook++; f.cook >= f.cookMax {
				f.cook = 0
				f.slots[furnaceOutput].item = out.Out
				f.slots[furnaceOutput].count++
				f.xpBank += smeltXP(out.Out) // banked until a player takes the output
				if f.slots[furnaceInput].count--; f.slots[furnaceInput].count == 0 {
					f.slots[furnaceInput].item = 0
				}
				changedSlots = true
			}
		case f.cook > 0: // no heat or nothing to cook: progress decays (vanilla -2)
			f.cook -= 2
			if f.cook < 0 {
				f.cook = 0
			}
		}

		if lit := f.burnLeft > 0; lit != wasLit {
			h.setFurnaceLit(players, pos, lit)
			f.resync = 20 // re-broadcast in 1s: the flip must not be lost to a full
			//               send queue or the client shows flames forever
		}
		if f.resync > 0 {
			if f.resync--; f.resync == 0 {
				h.broadcastBlock(players, pos.x, pos.y, pos.z, h.world.Block(pos.x, pos.y, pos.z))
			}
		}
		// Idle, empty, unlit, unwatched furnaces are dropped from the tick set.
		if f.burnLeft == 0 && f.cook == 0 && f.viewer == 0 &&
			f.slots[0].item == 0 && f.slots[1].item == 0 && f.slots[2].item == 0 {
			delete(h.furnaces, pos)
			continue
		}

		if f.viewer != 0 {
			t := players[f.viewer]
			if t == nil || t.winKind != winFurnace || t.winPos != pos {
				f.viewer = 0
				continue
			}
			if changedSlots {
				for i := 0; i < 3; i++ {
					h.sendWinSlot(t, int16(i), f.slots[i])
				}
			}
			h.sendFurnaceBars(t, f)
		}
	}
}

// reconcileFurnaceBlocks runs once at boot, after persisted furnace state is
// restored: a furnace block's lit bit must agree with its restored burn state.
// Lit blocks with no burning furnace behind them are extinguished (they'd glow
// forever); restored burning furnaces relight their block and keep smelting.
// (lit is the fast-varying state bit: even offset = lit.)
func (h *hub) reconcileFurnaceBlocks() {
	out, relit := 0, 0
	for _, c := range h.world.EditedChunks() {
		for _, e := range h.world.EditedBlocks(c[0], c[1]) {
			if e.State < furnaceStateMin || e.State > furnaceStateMax {
				continue
			}
			wx, wz := int(c[0])*16+e.LX, int(c[1])*16+e.LZ
			f := h.furnaces[blockPos{wx, e.Y, wz}]
			burning := f != nil && f.burnLeft > 0
			if lit := (e.State-furnaceStateMin)%2 == 0; lit != burning {
				if burning {
					h.world.SetBlock(wx, e.Y, wz, e.State-1) // same facing, lit=true
					relit++
				} else {
					h.world.SetBlock(wx, e.Y, wz, e.State+1) // same facing, lit=false
					out++
				}
			}
		}
	}
	if out > 0 || relit > 0 {
		log.Printf("furnace boot sweep: %d extinguished, %d relit from persisted burn state", out, relit)
	}
}

// setFurnaceLit flips the furnace block's lit property and broadcasts it.
func (h *hub) setFurnaceLit(players map[int32]*tracked, pos blockPos, lit bool) {
	state := h.world.Block(pos.x, pos.y, pos.z)
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("lit") {
		return
	}
	v := "false"
	if lit {
		v = "true"
	}
	h.setBlock(players, pos, worldgen.SetProperty(info, state, "lit", v))
}

// sendFurnaceBars pushes the progress-bar properties to the viewer — only the
// ones that changed since the last send, so an idle furnace is silent and a
// burning one doesn't flood the send queue (dropped packets there are what made
// stale lit-states possible in the first place).
func (h *hub) sendFurnaceBars(t *tracked, f *furnace) {
	send := func(prop, val int) {
		if f.lastBars[prop] == val {
			return
		}
		f.lastBars[prop] = val
		t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: int32(prop), Value: int32(val)})
	}
	send(propBurnLeft, f.burnLeft)
	send(propBurnMax, f.burnMax)
	send(propCook, f.cook)
	send(propCookMax, f.cookMax)
}

// sendFurnaceWindow refreshes the whole furnace window: 3 furnace slots + the
// player's main inventory + hotbar, then the progress bars.
func (h *hub) sendFurnaceWindow(t *tracked, f *furnace) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 39)
	for i := 0; i < 3; i++ {
		slots = append(slots, stackEv(f.slots[i]))
	}
	for i := 9; i <= 35; i++ { // main inventory: window 3-29
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ { // hotbar: window 30-38
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
	h.sendFurnaceBars(t, f)
}

// smeltXP is the experience one smelted item banks (vanilla varies 0.1-1.0
// per recipe; we approximate: food 0.35, everything else 0.7).
func smeltXP(output int32) float64 {
	if _, ok := foodPoints[output]; ok {
		return 0.35
	}
	return 0.7
}
