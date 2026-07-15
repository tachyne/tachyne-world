package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/plugin"
)

// pluginTestHub is the shared setup: a quiet hub (no natural spawning) with
// a live tick loop and a facade host, following hub_test.go's pattern.
func pluginTestHub(t *testing.T) (*hub, srvFacade) {
	t.Helper()
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false // keep mob noise out (see TestHubMultiplayer)
	host := &pluginHost{h: h, s: New(), cmds: map[string]*plugin.Command{}}
	h.plugHost = host
	startHub(t, h)
	return h, srvFacade{host}
}

// waitJoined polls until the hub has registered the named player.
func waitJoined(t *testing.T, h *hub, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var found bool
		onHub(t, h, func() {
			for _, tr := range h.playersRef {
				if tr.p.name == name {
					found = true
				}
			}
		})
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never joined", name)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// onHub runs fn on the hub goroutine via the plugin scheduler and waits for
// it — the sanctioned way for tests to touch hub-owned state. fn must not
// call t.Fatal/t.Skip (Goexit would kill the hub goroutine); t.Error only.
func onHub(t *testing.T, h *hub, fn func()) {
	t.Helper()
	done := make(chan struct{})
	schedFacade{h.psched}.NextTick(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("hub tick task never ran")
	}
}

func TestPluginScheduler(t *testing.T) {
	h, _ := pluginTestHub(t)
	sched := schedFacade{h.psched}

	// After: fires once, roughly delayTicks later.
	fired := make(chan uint64, 1)
	var start uint64
	onHub(t, h, func() { start = h.tick.Load(); sched.After(3, func() { fired <- h.tick.Load() }) })
	select {
	case at := <-fired:
		if at < start+3 || at > start+6 {
			t.Fatalf("After(3) fired at tick %d (scheduled at %d)", at, start)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("After(3) never fired")
	}

	// Every: fires repeatedly until cancelled, then never again.
	ticks := make(chan uint64, 64)
	var task plugin.Task
	onHub(t, h, func() { task = sched.Every(2, func() { ticks <- h.tick.Load() }) })
	for i := 0; i < 3; i++ {
		select {
		case <-ticks:
		case <-time.After(10 * time.Second):
			t.Fatalf("Every(2) fire %d never came", i+1)
		}
	}
	task.Cancel()
	// Drain anything in flight, then insist on silence.
	deadline := time.After(400 * time.Millisecond)
drain:
	for {
		select {
		case <-ticks:
		case <-deadline:
			break drain
		}
	}
	select {
	case <-ticks:
		t.Fatal("Every task fired after Cancel")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPluginFacadeMutations(t *testing.T) {
	h, srv := pluginTestHub(t)

	type probe struct {
		rainFlag, thunderFlag bool
		dayTime               uint64
		keepInv, doDaylight   bool
		difficulty            int
		gruleErr              error
	}
	var got probe
	onHub(t, h, func() {
		srv.SetWeather("thunder", 100)
		srv.SetTime(6000)
		if err := srv.SetGamerule("keepInventory", true); err != nil {
			t.Errorf("SetGamerule(keepInventory): %v", err)
		}
		if err := srv.SetGamerule("doDaylightCycle", false); err != nil {
			t.Errorf("SetGamerule(doDaylightCycle): %v", err)
		}
		got.gruleErr = srv.SetGamerule("noSuchRule", true)
		srv.SetDifficulty(3)
		got.rainFlag, got.thunderFlag = h.rainFlag, h.thunderFlag
		got.dayTime = srv.Time()
		got.keepInv, got.doDaylight = h.rules.KeepInventory, h.rules.DoDaylight
		got.difficulty = h.rules.Difficulty
	})
	if !got.rainFlag || !got.thunderFlag {
		t.Errorf("SetWeather(thunder) → rainFlag=%v thunderFlag=%v, want true/true", got.rainFlag, got.thunderFlag)
	}
	if got.dayTime != 6000 {
		t.Errorf("SetTime(6000) → Time()=%d", got.dayTime)
	}
	if !got.keepInv || got.doDaylight {
		t.Errorf("gamerules: keepInv=%v doDaylight=%v, want true/false", got.keepInv, got.doDaylight)
	}
	if got.gruleErr == nil {
		t.Error("SetGamerule(noSuchRule) should error")
	}
	if got.difficulty != 3 {
		t.Errorf("SetDifficulty(3) → %d", got.difficulty)
	}
}

func TestPluginSpawnOverlayAndMobHandle(t *testing.T) {
	h, srv := pluginTestHub(t)

	etype, ok := srv.EntityTypeByName("zombie")
	if !ok {
		t.Fatal("EntityTypeByName(zombie) unknown")
	}
	onHub(t, h, func() {
		mh, ok := srv.SpawnMob(0, etype, 0.5, 80, 0.5,
			&plugin.SpawnOpts{MaxHealth: 100, Speed: 0.5, Damage: 25})
		if !ok {
			t.Error("SpawnMob failed")
			return
		}
		m := h.mobs[mh.EID()]
		if m == nil {
			t.Error("spawned mob not registered")
			return
		}
		if m.maxHealth != 100 || m.health != 100 {
			t.Errorf("overlay health: max=%d cur=%d, want 100/100", m.maxHealth, m.health)
		}
		if got := hostileMelee(m); got != 25 {
			t.Errorf("hostileMelee with override = %v, want 25", got)
		}
		// The speed override must survive a behavior swap (which resets
		// speed from the species table for hostiles).
		h.applyBehavior(m, "hostile")
		if m.speed != 0.5 {
			t.Errorf("speed after behavior swap = %v, want the 0.5 override", m.speed)
		}
		// Handle semantics: SetMaxHealth clamps, Kill invalidates.
		mh.SetMaxHealth(40, false)
		if m.health != 40 { // was 100, above the new cap
			t.Errorf("health after cap shrink = %d, want 40", m.health)
		}
		if mh.TypeName() != "zombie" {
			t.Errorf("TypeName = %q", mh.TypeName())
		}
		mh.Kill()
		if m.dying == 0 && h.mobs[mh.EID()] != nil {
			t.Error("Kill left the mob alive and undying")
		}
	})
}

func TestPluginPlayerHandle(t *testing.T) {
	h, srv := pluginTestHub(t)
	w := h.world

	p1 := newPlayer(h.allocEID(), "alice", [16]byte{1})
	sy := w.SurfaceY(0, 0)
	h.post(evJoin{p: p1, x: 0.5, y: sy, z: 0.5})

	waitJoined(t, h, "alice")

	bow, ok := srv.ItemByName("bow")
	if !ok {
		t.Fatal("ItemByName(bow) unknown")
	}
	onHub(t, h, func() {
		ph, _ := srv.Player("alice")
		ph.Give(bow, 1)
		ph.SendMessage("hello alice")
		ph.Teleport(20.5, sy+2, 20.5)

		tr := h.playersRef[ph.EID()]
		if tr == nil {
			t.Error("tracked player missing")
			return
		}
		var has bool
		for _, sl := range tr.inv.slots {
			if sl.item == bow && sl.count == 1 {
				has = true
			}
		}
		if !has {
			t.Error("Give(bow) not in inventory")
		}
		if tr.x != 20.5 || tr.z != 20.5 {
			t.Errorf("Teleport → (%v,%v)", tr.x, tr.z)
		}
		if ghost, found := srv.Player("nobody"); found || ghost.Valid() {
			t.Error("Player(nobody) should not resolve")
		}
	})

	// The private message reached her queue.
	chatDeadline := time.After(10 * time.Second)
	for {
		select {
		case pkt := <-p1.out:
			if c, ok := pkt.ev.(attachproto.Chat); ok && c.Text == "hello alice" {
				return
			}
		case <-chatDeadline:
			t.Fatal("SendMessage never reached the player queue")
		}
	}
}

func TestPluginMobSpawnCancelAndReason(t *testing.T) {
	h, srv := pluginTestHub(t)

	spawns := make(chan *plugin.MobSpawnEvent, 64)
	plugin.On(h.plugins, plugin.Normal, false, func(e *plugin.MobSpawnEvent) {
		if e.Y == 80 { // only this test's spawns — run() seeds herd cows at the surface
			spawns <- e
		}
		if e.TypeName == "creeper" {
			e.SetCancelled(true) // a no-creepers plugin
		}
	})

	creeper, _ := srv.EntityTypeByName("creeper")
	cow, _ := srv.EntityTypeByName("cow")
	onHub(t, h, func() {
		before := len(h.mobs)
		if m := h.spawnMobCause(h.playersRef, creeper, 0, 0.5, 80, 0.5, plugin.SpawnCommand); m != nil {
			t.Error("cancelled creeper spawn returned a live mob")
		}
		if len(h.mobs) != before {
			t.Error("cancelled spawn left the mob registered")
		}
		if m := h.spawnMob(h.playersRef, cow, 0.5, 80, 0.5); m == nil {
			t.Error("uncancelled cow spawn failed")
		}
	})
	e1, e2 := <-spawns, <-spawns
	if e1.TypeName != "creeper" || e1.Reason != plugin.SpawnCommand {
		t.Fatalf("creeper event %+v (want reason %v)", e1, plugin.SpawnCommand)
	}
	if e2.TypeName != "cow" || e2.Reason != plugin.SpawnNatural {
		t.Fatalf("cow event %+v", e2)
	}
}

func TestPluginMobDeathDropMutation(t *testing.T) {
	h, srv := pluginTestHub(t)

	diamond, _ := srv.ItemByName("diamond")
	plugin.On(h.plugins, plugin.Normal, false, func(e *plugin.MobDeathEvent) {
		e.Drops = []plugin.ItemStack{{Item: diamond, Count: 3}} // everything drops diamonds
		e.XP = 0
	})

	cow, _ := srv.EntityTypeByName("cow")
	onHub(t, h, func() {
		m := h.spawnMob(h.playersRef, cow, 0.5, 80, 0.5)
		if m == nil {
			t.Error("spawn failed")
			return
		}
		before := len(h.items)
		h.despawnMob(h.playersRef, m)
		var found *itemEntity
		for _, it := range h.items {
			found = it
		}
		if len(h.items) != before+1 || found == nil || found.item != diamond || found.count != 3 {
			t.Fatalf("death drops not replaced: %d new items", len(h.items)-before)
		}
	})
}

func TestPluginDamageMutation(t *testing.T) {
	h, srv := pluginTestHub(t)

	// Halve all player→mob damage; cancel any hit on cows entirely.
	cow, _ := srv.EntityTypeByName("cow")
	plugin.On(h.plugins, plugin.Normal, false, func(e *plugin.EntityDamageByEntityEvent) {
		if m := h.mobs[e.VictimEID]; m != nil && m.etype == cow {
			e.SetCancelled(true)
			return
		}
		e.Damage = e.Damage / 2
	})

	p1 := newPlayer(h.allocEID(), "alice", [16]byte{1})
	sy := h.world.SurfaceY(0, 0)
	h.post(evJoin{p: p1, x: 0.5, y: sy, z: 0.5})
	waitJoined(t, h, "alice")

	zombie, _ := srv.EntityTypeByName("zombie")
	// NOTE: no t.Fatal/t.Skip inside onHub closures — they Goexit the HUB
	// goroutine, not the test. t.Error + return only.
	onHub(t, h, func() {
		c := h.spawnMob(h.playersRef, cow, 1.5, sy, 1.5)
		z := h.spawnMob(h.playersRef, zombie, 1.5, sy, 0.5)
		if c == nil || z == nil {
			t.Error("spawns failed")
			return
		}
		ch0, zh0 := c.health, z.health
		h.attackMob(h.playersRef, p1.eid, c.eid)
		h.attackMob(h.playersRef, p1.eid, z.eid)
		if c.health != ch0 {
			t.Errorf("cancelled hit still damaged the cow (%d→%d)", ch0, c.health)
		}
		if z.health == zh0 {
			t.Error("halved hit did no damage at all")
		}
		if z.lastAttacker != p1.eid {
			t.Errorf("lastAttacker = %d, want %d", z.lastAttacker, p1.eid)
		}
	})
}

func TestPluginJoinQuitChatEvents(t *testing.T) {
	h, _ := pluginTestHub(t)

	joins := make(chan *plugin.PlayerJoinEvent, 4)
	quits := make(chan *plugin.PlayerQuitEvent, 4)
	plugin.On(h.plugins, plugin.Monitor, false, func(e *plugin.PlayerJoinEvent) { joins <- e })
	plugin.On(h.plugins, plugin.Monitor, false, func(e *plugin.PlayerQuitEvent) { quits <- e })
	// Chat: LOW rewrites, HIGH sees the rewrite; "muted" gets cancelled.
	plugin.On(h.plugins, plugin.Low, false, func(e *plugin.PlayerChatEvent) {
		if e.Message == "muted" {
			e.SetCancelled(true)
			return
		}
		e.Message = e.Message + "!"
	})

	p1 := newPlayer(h.allocEID(), "alice", [16]byte{1})
	sy := h.world.SurfaceY(0, 0)
	h.post(evJoin{p: p1, x: 0.5, y: sy, z: 0.5})
	select {
	case e := <-joins:
		if e.Name != "alice" || e.EID != p1.eid {
			t.Fatalf("join event %+v", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("PlayerJoinEvent never fired")
	}

	h.post(evChat{from: p1, text: "muted"})  // cancelled — must not broadcast
	h.post(evChat{from: p1, text: "hello"})  // rewritten to "hello!"
	h.post(evChat{text: "[system] weather"}) // system line: no chat event, verbatim

	var lines []string
	deadline := time.After(10 * time.Second)
	for len(lines) < 2 {
		select {
		case pkt := <-p1.out:
			if c, ok := pkt.ev.(attachproto.Chat); ok && !c.ActionBar {
				lines = append(lines, c.Text)
			}
		case <-deadline:
			t.Fatalf("waiting for chat lines, got %q", lines)
		}
	}
	if lines[0] != "<alice> hello!" || lines[1] != "[system] weather" {
		t.Fatalf("chat lines = %q (cancel/mutate/ordering broken)", lines)
	}

	h.post(evLeave{p: p1})
	select {
	case e := <-quits:
		if e.Name != "alice" {
			t.Fatalf("quit event %+v", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("PlayerQuitEvent never fired")
	}
}

func TestPluginWeatherTimeGameruleEvents(t *testing.T) {
	h, srv := pluginTestHub(t)

	// A canceller vetoes any flip toward rain.
	plugin.On(h.plugins, plugin.Normal, false, func(e *plugin.WeatherChangeEvent) {
		if e.Raining {
			e.SetCancelled(true)
		}
	})
	times := make(chan *plugin.TimeSetEvent, 4)
	rules := make(chan *plugin.GameruleChangeEvent, 4)
	plugin.On(h.plugins, plugin.Monitor, false, func(e *plugin.TimeSetEvent) { times <- e })
	plugin.On(h.plugins, plugin.Monitor, false, func(e *plugin.GameruleChangeEvent) { rules <- e })

	onHub(t, h, func() {
		srv.SetWeather("rain", 100)
		if h.rainFlag {
			t.Error("WeatherChangeEvent cancel did not block the rain flip")
		}
		srv.SetTime(9000)
		srv.SetGamerule("mobGriefing", false)
	})
	select {
	case e := <-times:
		if e.New != 9000 {
			t.Fatalf("TimeSetEvent %+v", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("TimeSetEvent never fired")
	}
	select {
	case e := <-rules:
		if e.Rule != "mobGriefing" || e.On {
			t.Fatalf("GameruleChangeEvent %+v", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GameruleChangeEvent never fired")
	}
}

func TestPlugStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	st := newPlugStore(path)
	st.Set("counter", 41)
	var n int
	if !st.Get("counter", &n) || n != 41 {
		t.Fatalf("Get after Set = %d,%v", n, st.Get("counter", &n))
	}
	st.flushIfDirty()

	st2 := newPlugStore(path) // fresh load from disk
	if !st2.Get("counter", &n) || n != 41 {
		t.Fatal("value did not survive a flush + reload")
	}
	st2.Delete("counter")
	st2.flushIfDirty()
	st3 := newPlugStore(path)
	if st3.Get("counter", &n) {
		t.Fatal("deleted key survived a flush + reload")
	}
}

func TestPluginConfigAndEnabled(t *testing.T) {
	dir := t.TempDir()
	ctx := &pluginCtx{name: "conftest", dir: dir}

	type conf struct {
		Greeting string `json:"greeting"`
		Radius   int    `json:"radius"`
	}
	// Missing file: defaults untouched, nil error.
	c := conf{Greeting: "hi", Radius: 8}
	if err := ctx.Config(&c); err != nil || c.Greeting != "hi" || c.Radius != 8 {
		t.Fatalf("missing config: %+v err=%v", c, err)
	}
	if !ctx.confEnabled() {
		t.Fatal("missing config must mean enabled")
	}

	os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"enabled": false, "greeting": "yo"}`), 0o644)
	if err := ctx.Config(&c); err != nil || c.Greeting != "yo" || c.Radius != 8 {
		t.Fatalf("config load: %+v err=%v", c, err)
	}
	if ctx.confEnabled() {
		t.Fatal(`{"enabled": false} must disable the plugin`)
	}
}
