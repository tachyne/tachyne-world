package server

import (
	"bytes"
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// TestUIStackComponents pins the labelled-item wire shape: custom_name +
// lore composed as canonical components.
func TestUIStackComponents(t *testing.T) {
	st := uiStack(uiItemDaemon, 3, "webmap", []string{"line one", "line two"})
	if st.ID != uiItemDaemon || st.Count != 3 {
		t.Fatalf("stack %+v", st)
	}
	r := bytes.NewReader(st.Components)
	addC, _ := protocol.ReadVarInt(r)
	remC, _ := protocol.ReadVarInt(r)
	if addC != 2 || remC != 0 {
		t.Fatalf("addC=%d remC=%d", addC, remC)
	}
	cid, _ := protocol.ReadVarInt(r)
	if cid != componentCustomName {
		t.Fatalf("first component %d", cid)
	}
	readTagString := func() string {
		tag, _ := r.ReadByte()
		if tag != 0x08 {
			t.Fatalf("tag %#x", tag)
		}
		hi, _ := r.ReadByte()
		lo, _ := r.ReadByte()
		buf := make([]byte, int(hi)<<8|int(lo))
		r.Read(buf)
		return string(buf)
	}
	if got := readTagString(); got != "webmap" {
		t.Fatalf("name %q", got)
	}
	cid, _ = protocol.ReadVarInt(r)
	if cid != componentLore {
		t.Fatalf("second component %d", cid)
	}
	n, _ := protocol.ReadVarInt(r)
	if n != 2 {
		t.Fatalf("lore count %d", n)
	}
	if got := readTagString(); got != "line one" {
		t.Fatalf("lore[0] %q", got)
	}
	if got := readTagString(); got != "line two" {
		t.Fatalf("lore[1] %q", got)
	}
	if r.Len() != 0 {
		t.Fatalf("%d trailing bytes", r.Len())
	}
}

// drainWindow waits for the next WindowOpen or WindowItems on the queue.
func drainWindow(t *testing.T, p *player) (opens []attachproto.WindowOpen, items []attachproto.WindowItems) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case pkt := <-p.out:
			switch ev := pkt.ev.(type) {
			case attachproto.WindowOpen:
				opens = append(opens, ev)
			case attachproto.WindowItems:
				items = append(items, ev)
				if len(items) >= 2 { // loading fill + data fill
					return
				}
			}
		case <-deadline:
			t.Fatalf("windows so far: %d opens, %d fills", len(opens), len(items))
		}
	}
}

func TestPluginUIBrowseAndAct(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	stub := &stubDaemonBus{replies: []string{
		`{"ok":true,"manager":"shard-0","daemons":[{"manager":"shard-0","name":"webmap","module":"github.com/x/mapd","built":"v1.0.0","latest":"v1.1.0","outdated":true,"status":"running","restarts":0}]}`,
	}}
	h.bus = stub

	s.Ops[p.name] = true
	s.handleCommand(p, "plugin") // bare /plugin opens the browser

	opens, fills := drainWindow(t, p)
	if len(opens) != 1 || opens[0].Menu != menuGeneric9x6 || opens[0].Title != "Plugins" {
		t.Fatalf("opens %+v", opens)
	}
	if stub.subject() != "mc.plugin.search" { // list then catalog — search is last
		t.Fatalf("fetch subject %q", stub.subject())
	}
	final := fills[len(fills)-1]
	if final.Slots[0].ID != uiItemDaemon {
		t.Fatalf("entry slot item %d, want daemon book %d", final.Slots[0].ID, uiItemDaemon)
	}
	if len(final.Slots) != 90 {
		t.Fatalf("window slots %d, want 90 (54 + inventory)", len(final.Slots))
	}
	// The entry's components carry the OUTDATED lore.
	if !bytes.Contains(final.Slots[0].Components, []byte("OUTDATED")) {
		t.Fatal("outdated lore missing from the entry item")
	}

	// Click the entry → detail page with install absent (installed), upgrade
	// + uninstall + rating present.
	var ui *plugUIState
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		h.pluginUIClick(h.playersRef, tr, evClick{eid: p.eid, windowID: tr.winID, slot: 0})
		ui = tr.plugUI
	})
	if ui.page != uiPageDetail {
		t.Fatalf("page %d after entry click", ui.page)
	}
	if _, hasInstall := ui.actions[29]; hasInstall {
		t.Fatal("installed plugin offers install")
	}
	if ui.actions[31].kind != "upgrade" || ui.actions[33].kind != "uninstall" {
		t.Fatalf("detail actions %+v", ui.actions)
	}
	if ui.actions[39].kind != "rate" || ui.actions[39].stars != 3 {
		t.Fatalf("rating action %+v", ui.actions[39])
	}

	// Click uninstall → the fleet op goes out on the bus.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		h.pluginUIClick(h.playersRef, tr, evClick{eid: p.eid, windowID: tr.winID, slot: 33})
	})
	deadline := time.Now().Add(10 * time.Second)
	for stub.subject() != "mc.plugin.uninstall" {
		if time.Now().After(deadline) {
			t.Fatalf("uninstall never hit the bus (last subject %q)", stub.subject())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Back action returns to main.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.plugUI.page = uiPageDetail
		tr.plugUI.actions = map[int16]plugUIAction{45: {kind: "back"}}
		h.pluginUIClick(h.playersRef, tr, evClick{eid: p.eid, windowID: tr.winID, slot: 45})
		if tr.plugUI.page != uiPageMain {
			t.Error("back did not return to main")
		}
	})
}
