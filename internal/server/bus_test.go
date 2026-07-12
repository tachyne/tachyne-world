package server

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/plugin"
)

// waitDayTime polls for an async day-time set to land (settime goes through
// the hub event queue since the plugin TimeSetEvent).
func waitDayTime(t *testing.T, h *hub, want uint64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for h.dayTime.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("dayTime=%d, want %d", h.dayTime.Load(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestExecuteCommand checks the shared bus-command router (backend-agnostic).
func TestExecuteCommand(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false
	h.rules.DoDaylight = false // hold the clock still so the settime poll target is exact
	go h.run()

	if _, e := executeCommand(h, "settime", json.RawMessage(`{"time":13000}`)); e != "" {
		t.Fatalf("settime: %s", e)
	}
	// settime routes through the hub now (the plugin TimeSetEvent fires on
	// the tick goroutine), so the write is asynchronous.
	waitDayTime(t, h, 13000)
	if _, e := executeCommand(h, "say", json.RawMessage(`{"text":"hi"}`)); e != "" {
		t.Errorf("say: %s", e)
	}
	if _, e := executeCommand(h, "say", json.RawMessage(`{}`)); e == "" {
		t.Error("say without text should error")
	}
	if _, e := executeCommand(h, "nope", nil); e == "" {
		t.Error("unknown command should error")
	}
}

// TestBusCommands exercises the facade-parity command set.
func TestBusCommands(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false
	go h.run()

	// weather + gamerule are async posts; poll their effects.
	if _, e := executeCommand(h, "weather", json.RawMessage(`{"kind":"thunder","duration":200}`)); e != "" {
		t.Fatalf("weather: %s", e)
	}
	if _, e := executeCommand(h, "gamerule", json.RawMessage(`{"rule":"keepInventory","on":true}`)); e != "" {
		t.Fatalf("gamerule: %s", e)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var flag, keep bool
		h.runOnHub(func() { flag, keep = h.thunderFlag, h.rules.KeepInventory })
		if flag && keep {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("weather/gamerule never applied (thunder=%v keepInv=%v)", flag, keep)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e := executeCommand(h, "gamerule", json.RawMessage(`{"rule":"noSuch","on":true}`)); e == "" {
		t.Error("unknown gamerule should error")
	}
	if _, e := executeCommand(h, "give", json.RawMessage(`{"player":"x","item":"not_an_item"}`)); e == "" {
		t.Error("unknown item should error")
	}

	// spawn: named entity with stat overrides, replies with the eid.
	data, e := executeCommand(h, "spawn", json.RawMessage(`{"type":"zombie","x":0.5,"y":80,"z":0.5,"damage":21,"max_health":60}`))
	if e != "" {
		t.Fatalf("spawn: %s", e)
	}
	eid := data.(map[string]any)["eid"].(int32)
	var dmg float32
	var maxHP int
	h.runOnHub(func() {
		if m := h.mobs[eid]; m != nil {
			dmg, maxHP = hostileMelee(m), m.maxHealth
		}
	})
	if dmg != 21 || maxHP != 60 {
		t.Fatalf("spawn overrides: dmg=%v maxHP=%d", dmg, maxHP)
	}
	if _, e := executeCommand(h, "spawn", json.RawMessage(`{"type":"gremlin","x":0,"z":0}`)); e == "" {
		t.Error("unknown entity should error")
	}

	// mobset mutates the same mob.
	if _, e := executeCommand(h, "mobset", json.RawMessage(
		json.RawMessage(`{"eid":`+jsonInt(eid)+`,"damage":30,"speed":0.4}`))); e != "" {
		t.Fatalf("mobset: %s", e)
	}
	h.runOnHub(func() {
		m := h.mobs[eid]
		if m == nil || m.ovrDamage != 30 || m.speed != 0.4 {
			t.Errorf("mobset not applied: %+v", m)
		}
	})

	// Queries.
	if data, e := executeCommand(h, "mobs", nil); e != "" {
		t.Fatalf("mobs query: %s", e)
	} else if rows := data.(map[string]any)["mobs"]; rows == nil {
		t.Fatal("mobs query returned no rows key")
	}
	if data, e := executeCommand(h, "world", nil); e != "" {
		t.Fatalf("world query: %s", e)
	} else if w := data.(map[string]any); w["difficulty"] == nil {
		t.Fatalf("world query shape: %v", w)
	}
	if data, e := executeCommand(h, "block", json.RawMessage(`{"x":0,"y":-60,"z":0}`)); e != "" {
		t.Fatalf("block query: %s", e)
	} else if _, ok := data.(map[string]any)["state"]; !ok {
		t.Fatal("block query missing state")
	}
	if _, e := executeCommand(h, "teleport", json.RawMessage(`{"player":"ghost","x":0,"y":80,"z":0}`)); e == "" {
		t.Error("teleporting an offline player should error")
	}
}

// TestBusEventBridge: with the bridge registered, plugin events publish as
// JSON on mc.event.<name>; cancelled events don't.
func TestBusEventBridge(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false
	rec := &recordingBus{}
	h.bus = rec
	h.registerBusBridge()
	go h.run()

	// A join publishes v2.player_join with the struct fields.
	p1 := newPlayer(h.allocEID(), "alice", [16]byte{1})
	h.post(evJoin{p: p1, x: 0.5, y: 80, z: 0.5})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if raw, ok := rec.get("player_join"); ok {
			var ev struct {
				EID  int32  `json:"eid"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil || ev.Name != "alice" || ev.EID != p1.eid {
				t.Fatalf("player_join payload %s (err %v)", raw, err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("player_join never published")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A cancelled chat is not published on v2 (the action didn't happen).
	pcancel := func(e *plugin.PlayerChatEvent) { e.SetCancelled(true) }
	off := plugin.On(h.plugins, plugin.Normal, false, pcancel)
	h.post(evChat{from: p1, text: "silenced"})
	h.runOnHub(func() {}) // barrier: the chat event was processed
	if _, ok := rec.get("player_chat"); ok {
		t.Fatal("cancelled chat leaked onto the bus")
	}
	off()
	h.post(evChat{from: p1, text: "audible"})
	deadline = time.Now().Add(10 * time.Second)
	for {
		if raw, ok := rec.get("player_chat"); ok {
			var ev struct {
				Message string `json:"message"`
			}
			json.Unmarshal(raw, &ev)
			if ev.Message != "audible" {
				t.Fatalf("player_chat payload %s", raw)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("player_chat never published")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// recordingBus captures publishes for assertions (publishes happen on the
// hub goroutine, reads on the test goroutine).
type recordingBus struct {
	mu     sync.Mutex
	topics map[string][]byte
}

func (b *recordingBus) publish(topic string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	b.mu.Lock()
	if b.topics == nil {
		b.topics = map[string][]byte{}
	}
	b.topics[topic] = raw
	b.mu.Unlock()
}

func (b *recordingBus) get(topic string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, ok := b.topics[topic]
	return raw, ok
}

func jsonInt(v int32) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
