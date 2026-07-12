package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"tachyne/plugin"
)

// The plugin host implements the tachyne/plugin facade interfaces over the
// hub. Every facade method except the scheduler is hub-goroutine-only (the
// plugin package documents this contract); they mutate through the same
// helpers the evXxx cases use, called DIRECTLY with h.playersRef — never
// h.post, which could deadlock the hub against its own events buffer.

// entityNameByID is the reverse of entityByName (TypeName lookups).
var entityNameByID = func() map[int]string {
	m := make(map[int]string, len(entityByName))
	for name, id := range entityByName {
		m[id] = name
	}
	return m
}()

// pluginHost owns the enabled plugin set and their contexts.
type pluginHost struct {
	h       *hub
	s       *Server
	enabled []*enabledPlugin
	cmds    map[string]*plugin.Command // name + aliases → command (step 6 dispatch)
}

type enabledPlugin struct {
	p   plugin.Plugin
	ctx *pluginCtx
}

// enablePlugins runs every registered plugin's Enable before the tick loop
// starts. A plugin whose config.json says {"enabled": false} is skipped.
func (s *Server) enablePlugins() error {
	ps := plugin.Registered()
	if len(ps) == 0 {
		return nil
	}
	host := &pluginHost{h: s.hub, s: s, cmds: map[string]*plugin.Command{}}
	s.hub.plugHost = host
	for _, p := range ps {
		ctx := &pluginCtx{host: host, name: p.Name(),
			dir: filepath.Join(s.pluginDataDir(), p.Name())}
		if !ctx.confEnabled() {
			log.Printf("plugin %s: disabled by config", p.Name())
			continue
		}
		if err := os.MkdirAll(ctx.dir, 0o755); err != nil {
			return fmt.Errorf("plugin %s: %w", p.Name(), err)
		}
		ctx.logger = log.New(log.Writer(), "[plugin/"+p.Name()+"] ", log.LstdFlags)
		if err := p.Enable(ctx); err != nil {
			return fmt.Errorf("plugin %s: enable: %w", p.Name(), err)
		}
		host.enabled = append(host.enabled, &enabledPlugin{p: p, ctx: ctx})
		log.Printf("plugin %s enabled", p.Name())
	}
	if len(host.cmds) > 0 { // rebuild the tab-completion tree with plugin commands
		s.commandTree = buildCommandTree(host.allCommandNames()...)
	}
	return nil
}

// pluginDataDir is where per-plugin config + data folders live (cwd-relative
// like settings.json; the world PVC picks it up automatically).
func (s *Server) pluginDataDir() string {
	if s.PluginDataDir != "" {
		return s.PluginDataDir
	}
	return "plugins"
}

// disableAll runs Disable in reverse enable order and flushes stores. Hub
// goroutine only (via evDisablePlugins).
func (ph *pluginHost) disableAll() {
	for i := len(ph.enabled) - 1; i >= 0; i-- {
		ph.enabled[i].p.Disable()
	}
	ph.flushStores()
}

func (ph *pluginHost) flushStores() {
	for _, ep := range ph.enabled {
		if ep.ctx.store != nil {
			ep.ctx.store.flushIfDirty()
		}
	}
}

// evDisablePlugins asks the hub to run plugin Disable hooks (graceful
// shutdown); done is closed when they finished.
type evDisablePlugins struct{ done chan struct{} }

func (evDisablePlugins) isHubEvent() {}

// fireSync fires an event from a session goroutine and waits for the hub to
// run the handler ladder, returning Fire's proceed verdict. The HasType guard
// keeps the no-listeners case free of the round trip.
func (h *hub) fireSync(ev plugin.Event) bool {
	if !h.plugins.HasType(ev) {
		return true
	}
	r := make(chan bool, 1)
	h.post(evPluginSync{ev: ev, reply: r})
	return <-r
}

type evPluginSync struct {
	ev    plugin.Event
	reply chan bool // buffered, cap 1
}

func (evPluginSync) isHubEvent() {}

// ---- Context ----

type pluginCtx struct {
	host   *pluginHost
	name   string
	dir    string
	logger *log.Logger
	store  *plugStore
}

// confEnabled peeks at config.json's reserved "enabled" key (default true).
func (c *pluginCtx) confEnabled() bool {
	var probe struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.Config(&probe); err != nil {
		return true
	}
	return probe.Enabled == nil || *probe.Enabled
}

func (c *pluginCtx) Server() plugin.Server       { return srvFacade{c.host} }
func (c *pluginCtx) Events() *plugin.Dispatcher  { return c.host.h.plugins }
func (c *pluginCtx) Scheduler() plugin.Scheduler { return schedFacade{c.host.h.psched} }
func (c *pluginCtx) DataDir() string             { return c.dir }
func (c *pluginCtx) Logger() *log.Logger         { return c.logger }

func (c *pluginCtx) Config(v any) error {
	data, err := os.ReadFile(filepath.Join(c.dir, "config.json"))
	if os.IsNotExist(err) {
		return nil // operators author configs; absence = defaults
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (c *pluginCtx) Store() plugin.KV {
	if c.store == nil {
		c.store = newPlugStore(filepath.Join(c.dir, "data.json"))
	}
	return c.store
}

func (c *pluginCtx) RegisterCommand(cmd plugin.Command) error {
	if cmd.Name == "" || cmd.Run == nil {
		return fmt.Errorf("plugin %s: command needs a name and a Run", c.name)
	}
	names := append([]string{cmd.Name}, cmd.Aliases...)
	for _, n := range names {
		for _, builtin := range commandNames {
			if n == builtin {
				return fmt.Errorf("plugin %s: command %q collides with a built-in", c.name, n)
			}
		}
		if _, taken := c.host.cmds[n]; taken {
			return fmt.Errorf("plugin %s: command %q already registered", c.name, n)
		}
	}
	reg := cmd // one shared copy behind all names
	for _, n := range names {
		c.host.cmds[n] = &reg
	}
	return nil
}

// ---- Server facade ----

type srvFacade struct{ ph *pluginHost }

func (f srvFacade) BroadcastMessage(text string) {
	f.ph.h.roomChat(f.ph.h.playersRef, text)
}

func (f srvFacade) Player(name string) (plugin.Player, bool) {
	for eid, t := range f.ph.h.playersRef {
		if t.p.name == name {
			return playerHandle{f.ph, eid}, true
		}
	}
	return playerHandle{f.ph, 0}, false
}

func (f srvFacade) Players() []plugin.Player {
	out := make([]plugin.Player, 0, len(f.ph.h.playersRef))
	for eid := range f.ph.h.playersRef {
		out = append(out, playerHandle{f.ph, eid})
	}
	return out
}

func (f srvFacade) Mob(eid int32) (plugin.Mob, bool) {
	_, ok := f.ph.h.mobs[eid]
	return mobHandle{f.ph, eid}, ok
}

func (f srvFacade) Mobs() []plugin.Mob {
	out := make([]plugin.Mob, 0, len(f.ph.h.mobs))
	for eid := range f.ph.h.mobs {
		out = append(out, mobHandle{f.ph, eid})
	}
	return out
}

func (f srvFacade) World(dim int) plugin.World { return worldFacade{f.ph, dim} }

func (f srvFacade) SpawnMob(dim, etype int, x, y, z float64, opts *plugin.SpawnOpts) (plugin.Mob, bool) {
	h := f.ph.h
	if _, known := entityNameByID[etype]; !known {
		return mobHandle{f.ph, 0}, false
	}
	m := h.spawnMobCause(h.playersRef, etype, dim, x, y, z, plugin.SpawnPlugin)
	if m == nil {
		return mobHandle{f.ph, 0}, false
	}
	if d := speciesOf(etype); d != nil {
		h.applySpecies(h.playersRef, m)
	}
	if opts != nil {
		if opts.Behavior != "" {
			h.applyBehavior(m, opts.Behavior)
		}
		if opts.MaxHealth > 0 {
			m.maxHealth = opts.MaxHealth
			m.health = opts.MaxHealth
		}
		if opts.Speed > 0 {
			m.ovrSpeed, m.speed = opts.Speed, opts.Speed
		}
		if opts.Damage > 0 {
			m.ovrDamage = opts.Damage
		}
	}
	return mobHandle{f.ph, m.eid}, true
}

func (f srvFacade) SetWeather(kind string, durationTicks int) {
	f.ph.h.applyWeatherCommand(evSetWeather{kind: kind, duration: durationTicks})
}

func (f srvFacade) Weather() (bool, bool) { return f.ph.h.raining, f.ph.h.thundering }

func (f srvFacade) SetTime(ticks uint64) { f.ph.h.setDayTime(ticks) }
func (f srvFacade) Time() uint64         { return f.ph.h.dayTime.Load() }
func (f srvFacade) Tick() uint64         { return f.ph.h.tick.Load() }

func (f srvFacade) SetGamerule(rule string, on bool) error {
	switch rule {
	case "keepInventory", "doDaylightCycle", "doMobSpawning", "mobGriefing", "doWeatherCycle":
		f.ph.h.applyRule(f.ph.h.playersRef, evSetRule{rule: rule, on: on})
		return nil
	}
	return fmt.Errorf("unknown gamerule %q", rule)
}

func (f srvFacade) SetDifficulty(level int) {
	f.ph.h.applyRule(f.ph.h.playersRef, evSetRule{rule: "difficulty", num: level})
}

func (f srvFacade) IsOp(name string) bool { return f.ph.s.isOp(name) }

func (f srvFacade) EntityTypeByName(name string) (int, bool) {
	id, ok := entityByName[name]
	return id, ok
}

func (f srvFacade) ItemByName(name string) (int32, bool) {
	id, ok := itemByName[name]
	return id, ok
}

// ---- World facade ----

type worldFacade struct {
	ph  *pluginHost
	dim int
}

func (f worldFacade) Block(x, y, z int) uint32 {
	return f.ph.h.worldFor(f.dim).At(x, y, z)
}

func (f worldFacade) SetBlock(x, y, z int, state uint32) {
	f.ph.h.setBlockLive(f.ph.h.playersRef, f.dim, x, y, z, state)
}

func (f worldFacade) SurfaceY(x, z int) float64 {
	return f.ph.h.worldFor(f.dim).SurfaceY(x, z)
}

func (f worldFacade) BiomeAt(x, z int) string {
	return f.ph.h.worldFor(f.dim).BiomeAt(x, z)
}

// ---- Player handle ----

type playerHandle struct {
	ph  *pluginHost
	eid int32
}

func (p playerHandle) t() *tracked { return p.ph.h.playersRef[p.eid] }

func (p playerHandle) Valid() bool { return p.t() != nil }
func (p playerHandle) EID() int32  { return p.eid }

func (p playerHandle) Name() string {
	if t := p.t(); t != nil {
		return t.p.name
	}
	return ""
}

func (p playerHandle) Pos() (float64, float64, float64, int) {
	if t := p.t(); t != nil {
		return t.x, t.y, t.z, t.dim
	}
	return 0, 0, 0, 0
}

func (p playerHandle) SendMessage(text string) {
	if t := p.t(); t != nil {
		t.p.trySendEv(chatEv(text))
	}
}

func (p playerHandle) Gamemode() int {
	if t := p.t(); t != nil {
		return t.gamemode
	}
	return 0
}

func (p playerHandle) Health() float32 {
	if t := p.t(); t != nil {
		return t.health
	}
	return 0
}

func (p playerHandle) SetHealth(v float32) {
	t := p.t()
	if t == nil {
		return
	}
	if v <= 0 {
		p.ph.h.damage(p.ph.h.playersRef, t, 100000) // the evKill idiom
		return
	}
	if v > 20 {
		v = 20
	}
	t.health = v
	p.ph.h.sendHealth(t)
}

func (p playerHandle) Give(item int32, count int) {
	if t := p.t(); t != nil {
		p.ph.h.giveTo(p.ph.h.playersRef, t, item, count)
	}
}

func (p playerHandle) Teleport(x, y, z float64) {
	t := p.t()
	if t == nil {
		return
	}
	h := p.ph.h
	t.x, t.y, t.z = x, y, z
	t.p.trySendEv(teleportEv(x, y, z, t.yaw, t.pitch))
	move := entMove(p.eid, x, y, z, t.yaw, t.pitch, true)
	for eid, other := range h.playersRef {
		if eid == p.eid || other.dim != t.dim {
			continue
		}
		other.p.trySendEv(move)
	}
}

func (p playerHandle) IsOp() bool { return p.ph.s.isOp(p.Name()) }

// ---- Mob handle ----

type mobHandle struct {
	ph  *pluginHost
	eid int32
}

func (mh mobHandle) m() *mob { return mh.ph.h.mobs[mh.eid] }

func (mh mobHandle) Valid() bool { return mh.m() != nil }
func (mh mobHandle) EID() int32  { return mh.eid }

func (mh mobHandle) Type() int {
	if m := mh.m(); m != nil {
		return m.etype
	}
	return 0
}

func (mh mobHandle) TypeName() string {
	if m := mh.m(); m != nil {
		return entityNameByID[m.etype]
	}
	return ""
}

func (mh mobHandle) Pos() (float64, float64, float64, int) {
	if m := mh.m(); m != nil {
		return m.x, m.y, m.z, m.dim
	}
	return 0, 0, 0, 0
}

func (mh mobHandle) Health() int {
	if m := mh.m(); m != nil {
		return m.health
	}
	return 0
}

func (mh mobHandle) SetHealth(v int) {
	m := mh.m()
	if m == nil {
		return
	}
	if v > m.maxHealth {
		v = m.maxHealth
	}
	if v <= 0 {
		mh.ph.h.killMob(mh.ph.h.playersRef, m)
		return
	}
	m.health = v
}

func (mh mobHandle) MaxHealth() int {
	if m := mh.m(); m != nil {
		return m.maxHealth
	}
	return 0
}

func (mh mobHandle) SetMaxHealth(v int, heal bool) {
	m := mh.m()
	if m == nil || v <= 0 {
		return
	}
	m.maxHealth = v
	if heal || m.health > v {
		m.health = v
	}
}

func (mh mobHandle) Speed() float64 {
	if m := mh.m(); m != nil {
		return m.speed
	}
	return 0
}

func (mh mobHandle) SetSpeed(v float64) {
	if m := mh.m(); m != nil && v > 0 {
		m.ovrSpeed, m.speed = v, v
	}
}

func (mh mobHandle) MeleeDamage() float64 {
	if m := mh.m(); m != nil {
		return float64(hostileMelee(m))
	}
	return 0
}

func (mh mobHandle) SetMeleeDamage(v float64) {
	if m := mh.m(); m != nil && v > 0 {
		m.ovrDamage = v
	}
}

func (mh mobHandle) SetBehavior(name string) bool {
	m := mh.m()
	if m == nil {
		return false
	}
	return mh.ph.h.applyBehavior(m, name)
}

func (mh mobHandle) Remove() {
	if m := mh.m(); m != nil {
		mh.ph.h.removeMob(mh.ph.h.playersRef, m)
	}
}

func (mh mobHandle) Kill() {
	if m := mh.m(); m != nil {
		mh.ph.h.killMob(mh.ph.h.playersRef, m)
	}
}
