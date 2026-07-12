package server

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/plugin"
)

// The in-game plugin UI: a server-owned 9x6 chest window (winPlugin) where
// every entry is an item labelled with custom_name + lore components.
// Browse installed plugins and registry search results, click an entry for
// its card, click actions to install/upgrade/uninstall/rate. The window is
// READ-ONLY as an inventory: every click is intercepted before the generic
// move logic, answered with a full resync, and mapped to an action. All
// bus round trips run off the hub goroutine and refill the window through
// evPluginUIFill.

const menuGeneric9x6 = 5 // minecraft:generic_9x6 (static menu registry)

// uiPage
const (
	uiPageMain = iota
	uiPageDetail
)

// plugUIEntry is one row: a compiled-in plugin, a fleet daemon, or a
// registry search result (fields filled as known).
type plugUIEntry struct {
	name, module, desc string
	typ                string // "plugin" (compiled) | "daemon"
	manager            string // fleet manager running it ("" = not installed)
	current, latest    string
	status             string
	installs, ratings  int
	rating             float64
	restarts           int
	outdated           bool
	installed          bool
	compiled           bool
}

type plugUIAction struct {
	kind  string // "open" | "install" | "upgrade" | "uninstall" | "rate" | "refresh" | "back"
	entry plugUIEntry
	stars int
}

type plugUIState struct {
	page    int
	query   string
	seq     int // guards stale async fills
	entries []plugUIEntry
	actions map[int16]plugUIAction
	detail  plugUIEntry
}

type evOpenPluginUI struct {
	eid   int32
	query string
}

type evPluginUIFill struct {
	eid     int32
	winID   int32
	seq     int
	entries []plugUIEntry
	fail    string
}

func (evOpenPluginUI) isHubEvent() {}
func (evPluginUIFill) isHubEvent() {}

// UI item ids by NAME (never number); fallbacks keep the UI alive if a
// name ever leaves the registry.
func uiItemID(name string) int32 {
	if id, ok := itemByName[name]; ok {
		return id
	}
	return itemByName["book"]
}

var (
	uiItemDaemon    = uiItemID("book")
	uiItemCompiled  = uiItemID("knowledge_book")
	uiItemResult    = uiItemID("writable_book")
	uiItemRefresh   = uiItemID("clock")
	uiItemBack      = uiItemID("arrow")
	uiItemHint      = uiItemID("compass")
	uiItemInstall   = uiItemID("emerald")
	uiItemUpgrade   = uiItemID("blaze_powder")
	uiItemUninstall = uiItemID("redstone")
	uiItemStar      = uiItemID("gold_nugget")
)

// uiStack builds a labelled item: custom_name + lore components, composed
// in canonical-770 form like stackComponents (the chain renumbers per
// client version; both components are chain-whitelisted).
func uiStack(item int32, count int, name string, lore []string) attachproto.ItemStack {
	var b []byte
	comps := int32(1)
	if len(lore) > 0 {
		comps++
	}
	b = protocol.AppendVarInt(b, comps)
	b = protocol.AppendVarInt(b, 0)
	b = protocol.AppendVarInt(b, componentCustomName)
	b = append(b, chatNBT(name)...)
	if len(lore) > 0 {
		b = protocol.AppendVarInt(b, componentLore)
		b = protocol.AppendVarInt(b, int32(len(lore)))
		for _, line := range lore {
			b = append(b, chatNBT(line)...)
		}
	}
	if count < 1 || count > 64 {
		count = 1
	}
	return attachproto.ItemStack{ID: item, Count: int32(count), Components: b}
}

// openPluginUI opens the window and kicks off the async fill.
func (h *hub) openPluginUI(t *tracked, query string) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind = h.nextWin, winPlugin
	if t.plugUI == nil {
		t.plugUI = &plugUIState{}
	}
	t.plugUI.page, t.plugUI.query = uiPageMain, query
	t.plugUI.seq++

	title := "Plugins"
	if query != "" {
		title = "Plugins — " + query
	}
	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: menuGeneric9x6, Title: title})
	h.sendPluginLoading(t)
	h.fetchPluginUI(t.p.eid, int32(t.winID), t.plugUI.seq, query)
}

// fetchPluginUI gathers entries OFF the hub goroutine (bus round trips) and
// posts the fill event. The compiled-in set is snapshotted here (immutable
// after boot, so reading it from the goroutine is safe).
func (h *hub) fetchPluginUI(eid, winID int32, seq int, query string) {
	var compiled []plugUIEntry
	if h.plugHost != nil {
		enabled := map[string]bool{}
		for _, ep := range h.plugHost.enabled {
			enabled[ep.p.Name()] = true
		}
		for _, pl := range plugin.Registered() {
			status := "enabled"
			if !enabled[pl.Name()] {
				status = "disabled by config"
			}
			compiled = append(compiled, plugUIEntry{name: pl.Name(), typ: "plugin",
				status: status, installed: true, compiled: true})
		}
	}
	go func() {
		entries := compiled
		fail := ""

		// Fleet daemons.
		if raws, err := h.bus.requestMany("mc.plugin.list", map[string]any{}, fleetWindow); err == nil {
			for _, r := range parseReplies(raws) {
				for _, d := range r.Daemons {
					cur := d.Version
					if cur == "" {
						cur = d.Built
					}
					entries = append(entries, plugUIEntry{name: d.Name, module: d.Module,
						typ: "daemon", manager: r.Manager, current: cur, latest: d.Latest,
						status: d.Status, restarts: d.Restarts, outdated: d.Outdated, installed: true})
				}
			}
		} else {
			fail = "daemon managers unreachable: " + err.Error()
		}

		// Registry catalog: ALWAYS shown — a bare open lists everything
		// available (query "" matches all), so an uninstalled plugin stays
		// one click from reinstall; a query narrows the results.
		if raw, err := h.bus.request("mc.plugin.search", map[string]any{"q": query}); err == nil {
			var r managerReply
			if jsonUnmarshal(raw, &r) && r.OK {
				installed := map[string]bool{}
				for _, e := range entries {
					if e.module != "" {
						installed[e.module] = true
					}
				}
				for _, pl := range r.Plugins {
					if installed[pl.Module] {
						continue // already shown as an installed row
					}
					entries = append(entries, plugUIEntry{name: pl.Name, module: pl.Module,
						typ: pl.Type, desc: pl.Description, latest: pl.Latest,
						installs: pl.Installs, rating: pl.Rating, ratings: pl.Ratings})
				}
			} else if query != "" {
				fail = "search failed: " + r.Error
			}
		} else if query != "" {
			fail = "search failed: " + err.Error()
		}

		h.post(evPluginUIFill{eid: eid, winID: winID, seq: seq, entries: entries, fail: fail})
	}()
}

// applyPluginUIFill lands the async fetch (hub goroutine).
func (h *hub) applyPluginUIFill(players map[int32]*tracked, e evPluginUIFill) {
	t := players[e.eid]
	if t == nil || t.plugUI == nil || t.winKind != winPlugin ||
		int32(t.winID) != e.winID || t.plugUI.seq != e.seq {
		return // window closed / superseded while we fetched
	}
	if e.fail != "" {
		t.p.trySendEv(chatEv(e.fail))
	}
	t.plugUI.entries = e.entries
	if t.plugUI.page == uiPageDetail {
		// Refresh the detail entry from the new rows if still present.
		for _, en := range e.entries {
			if en.name == t.plugUI.detail.name && en.module == t.plugUI.detail.module {
				t.plugUI.detail = en
			}
		}
		h.sendPluginDetail(t)
		return
	}
	h.sendPluginMain(t)
}

// sendPluginLoading shows a placeholder while the fetch runs.
func (h *hub) sendPluginLoading(t *tracked) {
	slots := emptyUISlots(t)
	slots[22] = uiStack(uiItemHint, 1, "Loading…", []string{"gathering plugins from the fleet"})
	h.sendPluginWindow(t, slots)
	t.plugUI.actions = map[int16]plugUIAction{}
}

// sendPluginMain renders the entry grid + control row.
func (h *hub) sendPluginMain(t *tracked) {
	ui := t.plugUI
	slots := emptyUISlots(t)
	actions := map[int16]plugUIAction{}

	entries := append([]plugUIEntry{}, ui.entries...)
	sort.SliceStable(entries, func(i, j int) bool { // compiled, then daemons, then results
		rank := func(e plugUIEntry) int {
			switch {
			case e.compiled:
				return 0
			case e.installed:
				return 1
			}
			return 2
		}
		return rank(entries[i]) < rank(entries[j])
	})
	for i, en := range entries {
		if i >= 45 {
			break
		}
		item, name, lore := uiEntryItem(en)
		slots[i] = uiStack(item, 1, name, lore)
		actions[int16(i)] = plugUIAction{kind: "open", entry: en}
	}

	slots[45] = uiStack(uiItemRefresh, 1, "Refresh", []string{"re-gather the fleet + registry"})
	actions[45] = plugUIAction{kind: "refresh"}
	hint := "Showing installed + all available plugins"
	if ui.query != "" {
		hint = "Filtering: " + ui.query
	}
	slots[53] = uiStack(uiItemHint, 1, hint, []string{"narrow with /plugin ui <query>"})

	h.sendPluginWindow(t, slots)
	ui.actions = actions
}

// sendPluginDetail renders one plugin's card + action row.
func (h *hub) sendPluginDetail(t *tracked) {
	ui := t.plugUI
	en := ui.detail
	slots := emptyUISlots(t)
	actions := map[int16]plugUIAction{}

	item, name, lore := uiEntryItem(en)
	if en.desc != "" {
		lore = append(lore, "", en.desc)
	}
	slots[13] = uiStack(item, 1, name, lore)

	if !en.compiled {
		if !en.installed && en.module != "" {
			slots[29] = uiStack(uiItemInstall, 1, "Install", []string{"install on every shard"})
			actions[29] = plugUIAction{kind: "install", entry: en}
		}
		if en.installed {
			label, sub := "Upgrade (progressive)", "one shard at a time, health-checked"
			if !en.outdated {
				label, sub = "Rebuild / restart", "rebuilds at latest and reboots"
			}
			slots[31] = uiStack(uiItemUpgrade, 1, label, []string{sub})
			actions[31] = plugUIAction{kind: "upgrade", entry: en}
			slots[33] = uiStack(uiItemUninstall, 1, "Uninstall", []string{"remove from every shard"})
			actions[33] = plugUIAction{kind: "uninstall", entry: en}
		}
		if en.module != "" { // rating row (registry-listed plugins only)
			for s := 1; s <= 5; s++ {
				slot := int16(36 + s) // 37..41
				slots[slot] = uiStack(uiItemStar, s, fmt.Sprintf("Rate %d★", s),
					[]string{fmt.Sprintf("current: %.1f★ (%d ratings)", en.rating, en.ratings)})
				actions[slot] = plugUIAction{kind: "rate", entry: en, stars: s}
			}
		}
	} else {
		slots[31] = uiStack(uiItemHint, 1, "Compiled-in plugin", []string{
			"part of the server binary", "configure: plugins/" + en.name + "/config.json"})
	}

	slots[45] = uiStack(uiItemBack, 1, "Back", nil)
	actions[45] = plugUIAction{kind: "back"}

	h.sendPluginWindow(t, slots)
	ui.actions = actions
}

// uiEntryItem picks the item + label + lore for an entry row.
func uiEntryItem(en plugUIEntry) (int32, string, []string) {
	var lore []string
	item := uiItemResult
	switch {
	case en.compiled:
		item = uiItemCompiled
		lore = append(lore, "compiled-in ("+en.status+")")
	case en.installed:
		item = uiItemDaemon
		lore = append(lore, fmt.Sprintf("[%s] %s, %d restarts", en.manager, en.status, en.restarts))
		if en.current != "" {
			lore = append(lore, "running "+shortVer(en.current))
		}
		if en.outdated {
			lore = append(lore, "*** OUTDATED — latest "+shortVer(en.latest))
		}
	default:
		lore = append(lore, "available ("+en.typ+")")
		if en.latest != "" {
			lore = append(lore, "latest "+shortVer(en.latest))
		}
		if en.ratings > 0 {
			lore = append(lore, fmt.Sprintf("%.1f★ (%d) · %d installs", en.rating, en.ratings, en.installs))
		} else if en.installs > 0 {
			lore = append(lore, fmt.Sprintf("%d installs", en.installs))
		}
	}
	if en.module != "" {
		lore = append(lore, en.module)
	}
	name := en.name
	if en.outdated {
		name += " (outdated)"
	}
	return item, name, lore
}

func shortVer(v string) string {
	if len(v) > 20 {
		return v[:20] + "…"
	}
	return v
}

// emptyUISlots is the 9x6 container plus the viewer's own inventory
// (windows always show main inv + hotbar below the container).
func emptyUISlots(t *tracked) []attachproto.ItemStack {
	slots := make([]attachproto.ItemStack, 54, 90)
	for i := 9; i <= 35; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	return slots
}

func (h *hub) sendPluginWindow(t *tracked, slots []attachproto.ItemStack) {
	t.inv.stateId++
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: attachproto.ItemStack{}})
}

// pluginUIClick handles every click while the plugin window is open: the
// window is read-only, so first undo whatever the client predicted (full
// resync), then run the mapped action.
func (h *hub) pluginUIClick(players map[int32]*tracked, t *tracked, e evClick) {
	ui := t.plugUI
	if ui == nil {
		h.resyncWindow(t)
		return
	}
	act, ok := ui.actions[e.slot]
	// Resync AFTER looking up the action: the actions map matches what the
	// client currently sees.
	h.resyncPluginPage(t)
	if !ok {
		return
	}
	switch act.kind {
	case "open":
		ui.page, ui.detail = uiPageDetail, act.entry
		h.sendPluginDetail(t)
	case "back":
		ui.page = uiPageMain
		h.sendPluginMain(t)
	case "refresh":
		ui.seq++
		h.sendPluginLoading(t)
		h.fetchPluginUI(t.p.eid, int32(t.winID), ui.seq, ui.query)
	case "install":
		target := act.entry.module
		if target == "" {
			target = act.entry.name
		}
		h.pluginUIOp(t, "install", installPayload(target), act.entry.name)
	case "uninstall":
		h.pluginUIOp(t, "uninstall", map[string]any{"name": act.entry.name}, act.entry.name)
	case "upgrade":
		t.p.trySendEv(chatEv("Rolling " + act.entry.name + " across the fleet…"))
		go func(p *player, name string) {
			h.daemonProgressiveUpgrade(p, name)
			h.post(evOpenPluginUI{eid: p.eid, query: ""}) // fresh view when done
		}(t.p, act.entry.name)
	case "rate":
		go func(p *player, name string, stars int) {
			if raw, err := h.bus.request("mc.plugin.rate", map[string]any{"name": name, "stars": stars}); err != nil {
				p.trySendEv(chatEv("Rating failed: " + err.Error()))
			} else {
				var r managerReply
				if jsonUnmarshal(raw, &r) && r.OK {
					p.trySendEv(chatEv(fmt.Sprintf("Rated %s %d★.", name, stars)))
				} else {
					p.trySendEv(chatEv("Rating failed: " + r.Error))
				}
			}
		}(t.p, act.entry.name, act.stars)
	}
}

// pluginUIOp runs a fleet mutation off the hub and refreshes the window.
func (h *hub) pluginUIOp(t *tracked, op string, payload map[string]any, label string) {
	t.p.trySendEv(chatEv("Sending " + op + " of " + label + " to the fleet…"))
	eid, winID := t.p.eid, int32(t.winID)
	go func(p *player) {
		raws, err := h.bus.requestMany("mc.plugin."+op, payload, fleetWindow)
		if err != nil {
			p.trySendEv(chatEv(op + " failed: " + err.Error()))
			return
		}
		for _, r := range parseReplies(raws) {
			if r.OK {
				p.trySendEv(chatEv(fmt.Sprintf("[%s] %s: done", r.Manager, op)))
			} else {
				p.trySendEv(chatEv(fmt.Sprintf("[%s] %s: %s", r.Manager, op, r.Error)))
			}
		}
		h.post(evOpenPluginUI{eid: eid, query: ""})
		_ = winID
	}(t.p)
}

// resyncPluginPage re-sends the current page (undoes client-side pickup
// predictions — the window never yields items).
func (h *hub) resyncPluginPage(t *tracked) {
	if t.plugUI == nil {
		return
	}
	if t.plugUI.page == uiPageDetail {
		h.sendPluginDetail(t)
	} else {
		h.sendPluginMain(t)
	}
}

func installPayload(target string) map[string]any {
	if strings.Contains(target, "/") {
		return map[string]any{"module": target}
	}
	return map[string]any{"name": target}
}

// jsonUnmarshal is a tiny bool-returning wrapper (keeps call sites tidy).
func jsonUnmarshal(raw []byte, v any) bool {
	return json.Unmarshal(raw, v) == nil
}
