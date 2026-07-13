package server

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/handover"
	"github.com/tachyne/tachyne-common/shard"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	"github.com/tachyne/tachyne-world/plugin"
)

// The hub is the central authority: one goroutine owns the player registry and
// the server clock, so multiplayer state needs no locks. Connection goroutines
// stay I/O-only — they translate inbound packets into events and drain an
// outbound queue — and never touch each other's state. This is the Go-idiomatic
// alternative to Java's lock-everywhere model and the backbone for entities and
// (later) NPCs.
//
// The world keeps its own RWMutex for now: chunk generation + lighting is heavy
// and runs on connection goroutines (per player), so routing it through the hub
// goroutine would serialise it behind one core. The hub owns the multiplayer-
// relevant mutations (who's connected, where they are, broadcasting edits);
// world block storage stays mutex-guarded. Fold it in if/when contention bites.

const (
	entitySyncInterval = 40 // ticks (2s) between absolute entity-position resyncs

	dayLengthTicks = 24000 // one Minecraft day = 24000 ticks = 20 min at 20 TPS
)

// playerEntityType is minecraft:entity_type "player" in the canonical ID
// space. Derived from the generated table: the hardcoded 148 (1.21.5) went
// stale in the 1.21.11 re-target — player is 155 there, and 148 = wolf, so
// every remote player rendered as a wolf until this was caught by the
// Bedrock gateway's probe.
var playerEntityType = entityID("player")

// hub events — produced by connection goroutines, consumed by the hub goroutine.
type hubEvent interface{ isHubEvent() }

type evJoin struct {
	p          *player
	x, y, z    float64
	yaw, pitch float32
	dim        int // 0 overworld, 1 nether
	gamemode   int
	resume     *handover.PlayerState // non-nil = resume a migrated player (seed from snapshot, no fresh survival)
}
type evMove struct {
	eid        int32
	x, y, z    float64
	yaw, pitch float32
	onGround   bool
	sprinting  bool
	teleport   bool // server-initiated (/tp): bypasses movement validation
}
type evLeave struct{ p *player }
type evPeerFrame struct { // a frame arrived from a neighbour over the peer mesh
	from    int32
	typ     byte
	payload []byte
}

func (evPeerFrame) isHubEvent() {}

type evBlock struct {
	x, y, z int
	dim     int // which dimension the edit happened in
	state   uint32
	by      int32  // editor entity ID (already saw its own prediction; skip echo)
	broken  uint32 // when a dig destroyed a block: its old state (0 = not a break) —
	//                drives world_event 2001 (break particles + sound) for OTHERS
}

// evChat broadcasts chat. from != nil marks real player chat (raw text, no
// name prefix yet — the hub formats it AFTER the plugin chat event so a
// mutated message is honored); from == nil is a system line sent verbatim.
type evChat struct {
	from *player
	text string
}
type evList struct{ p *player }             // send the online-player list to one player
type evSetTime struct{ t uint64 }           // explicit day-time set (command/bus) — fires the plugin event
type evAnnounce struct{ name, text string } // a plugin's note, relayed to online ops
type evSetGamemode struct {                 // apply a game-mode change to a named player
	name string
	mode int
	by   string // who initiated it ("" or self = no "operator changed" notice)
}
type evSetHud struct { // toggle a player's HUD
	eid int32
	on  bool
}
type evSetBlock struct { // a bus-driven block change (apply + broadcast + simulate)
	x, y, z int
	state   uint32
}
type evSetBehavior struct { // a bus-driven behavior change on an existing mob
	eid      int32
	behavior string
}
type evDrop struct { // a destroyed block's loot — roll its drop table and spawn items
	x, y, z int
	state   uint32
	held    uint16 // item id used to break it (0 = hand); gates tool-required drops
	by      int32  // breaker's entity id (0 = the world itself) — pays mining XP
}
type evRespawn struct{ eid int32 } // player clicked Respawn after dying
type evUseMap struct{ eid int32 }  // player right-clicked an empty map
type evEat struct {
	eid  int32
	slot int
}                                              // player right-clicked food in a hotbar slot
type evAttack struct{ attacker, target int32 } // player melee-hit an entity
type evInteractMob struct {                    // right-clicked a mob (feed/shear/mount screen)
	eid, target int32
	sneak       bool
}

func (evInteractMob) isHubEvent() {}

type evThrowPearl struct{ eid int32 } // right-clicked with an ender pearl

func (evThrowPearl) isHubEvent() {}

type evNPCDecision struct { // an LLM NPC's decided action (nil = none)
	eid    int32
	action *npcAction
}
type evConsume struct{ eid, slot int32 } // survival: consume one of a hotbar slot after placing
type evStopEat struct {                  // release_use_item / hotbar switch: end an eat-hold or bow draw
	eid  int32
	fire bool // true on an explicit release: a drawn bow LOOSES instead of lowering
}

func (evJoin) isHubEvent()        {}
func (evMove) isHubEvent()        {}
func (evSetTime) isHubEvent()     {}
func (evAnnounce) isHubEvent()    {}
func (evLeave) isHubEvent()       {}
func (evBlock) isHubEvent()       {}
func (evChat) isHubEvent()        {}
func (evList) isHubEvent()        {}
func (evSetGamemode) isHubEvent() {}
func (evSetHud) isHubEvent()      {}
func (evSetBlock) isHubEvent()    {}
func (evSetBehavior) isHubEvent() {}
func (evDrop) isHubEvent()        {}
func (evRespawn) isHubEvent()     {}
func (evEat) isHubEvent()         {}
func (evUseMap) isHubEvent()      {}
func (evAttack) isHubEvent()      {}
func (evNPCDecision) isHubEvent() {}
func (evConsume) isHubEvent()     {}
func (evSaveState) isHubEvent()   {}

// evSaveState asks the hub to snapshot + flush everything it persists
// (inventories, containers). done is closed when the write completed — the
// graceful-shutdown path posts this and waits so a SIGTERM can't race the hub.
type evSaveState struct{ done chan struct{} }

func (evStopEat) isHubEvent() {}

// winKind says what a player's open window views: their own inventory (window
// 0), a crafting table's 3x3, a furnace, or a chest. The latter two point at a
// world block via winPos.
type winKind int

const (
	winPlayer winKind = iota
	winCraft
	winFurnace
	winChest
	winEnchant
	winAnvil
	winGrind
	winCarto    // cartography table (shares the two-slot machinery)
	winBeacon   // beacon menu (payment slot in t.anvil[0] + three properties)
	winStonecut // stonecutter (input in t.anvil[0], indexed recipe buttons)
	winLoom     // loom (banner/dye in t.anvil, pattern item in t.extraSlot)
	winSmith    // smithing table (template in t.extraSlot, base/addition in t.anvil)
	winHorse    // mount inventory (slots live on the mob; see horseSlotPtr)
	winLectern  // lectern reader (one read-only slot + the page property)
	winBin      // dispenser/dropper/hopper (h.bins)
	winTrade    // villager merchant screen
	winPlugin   // the server-owned plugin browser (plugui.go)
)

// tracked is the hub's authoritative record for a connected player. Position is
// the hub's own copy, fed by move events, so it never races the connection's copy.
type tracked struct {
	p              *player
	adv            advState          // advancement grants (advID → criterion → millis)
	advVisible     map[string]bool   // nodes revealed to the client (vanilla frontier)
	stats          map[statKey]int32 // statistics counters (canonical 774 keys)
	rbKnown        map[int32]bool    // recipe book: unlocked display ids
	rbHighlight    map[int32]bool    // recipe book: "new" badges not yet viewed
	rbSettings     attachproto.RecipeSettings
	migrating      string // non-empty (migID) while a handover to a neighbour is in flight
	x, y, z        float64
	yaw, pitch     float32
	dim            int    // 0 overworld, 1 nether
	portalTicks    int    // consecutive dwell passes standing in a portal block
	portalLatch    bool   // just arrived by portal: no re-trigger until they step off
	rejectStreak   int    // rejections within the rolling window (yields at 40)
	lastRejectTick uint64 // window anchor for rejectStreak
	bossBarOn      bool   // dragon bossbar currently shown to this client
	graceUntil     uint64 // no environmental damage until this tick (portal arrival)
	onGround       bool
	sprinting      bool // last reported sprint state (crit/knockback modifiers)
	gamemode       int
	hudOn          bool

	lastAttack    uint64 // tick of the last melee swing (attack-cooldown scaling)
	drawingAt     uint64 // tick a bow draw began (0 = not drawing)
	blockingSince uint64 // tick a shield was raised (0 = not blocking)
	fireSecs      int    // seconds of afterburn left (lava/fire) — 1 dmg/s, water clears

	// Survival state — simulated only while gamemode == gmSurvival.
	health      float32
	absorption  float32 // extra damage buffer from the Absorption effect (soaked first)
	food        int
	saturation  float32
	exhaustion  float32
	dead        bool
	airborne    bool
	peakY       float64    // highest y since leaving the ground (for fall damage)
	air         int        // remaining breath in ticks (maxAir underwater→0 = drowning)
	inv         *inventory // survival inventory (picked-up drops)
	eatingSlot  int        // hotbar slot being eaten from (-1 = not eating)
	eatingAt    uint64     // tick the eat-hold started (applies after eatDuration)
	resyncInvAt uint64     // tick to re-push the inventory (self-heal a dropped one-shot)
	sleeping    bool       // in bed, waiting for everyone else (skips night when all sleep)
	sleepPos    blockPos   // the bed being slept in (drifting away wakes)
	sleepingAt  uint64     // tick they lay down (night turns after sleepSkipTicks)

	effects map[int32]*activeEffect // active status effects (hub-owned, 1 Hz tick)

	// Experience — persisted with the inventory; dying scatters and zeroes it.
	xpLevel  int
	xpPoints int // points into the current level (bar = points/xpToNext)

	// Movement authority (validateMove) — zero values are correct at join.
	moveBudget   float64 // banked movement allowance in blocks (accrues per tick)
	lastMoveTick uint64  // tick of the last vetted move event
	floatTicks   int     // consecutive ticks unsupported and not descending
	lastRubber   uint64  // tick of the last correction teleport (throttle)

	// Container state — window 0 unless a crafting table / furnace / chest is open.
	cursor  invStack    // the stack carried on the mouse cursor
	craft   [9]invStack // active crafting grid (first 4 cells for the 2x2)
	winID   int32       // open window id; 0 = player inventory
	winKind winKind     // what the open window views (winPlayer while winID == 0)
	winPos  blockPos    // the furnace/chest block this window views
	armor   [4]invStack // window-0 armor slots — worn, applied, persisted
	offhand invStack

	plugUI *plugUIState // plugin-browser window state (nil until first opened)

	// Enchanting table view (winEnchant): the two table slots + rolled offers.
	enchSlots [2]invStack // 0 = the item, 1 = lapis
	enchOpts  [3]enchOption

	// Anvil/grindstone view (winAnvil/winGrind): two inputs + the rename box.
	anvil     [2]invStack
	trade     [2]invStack // merchant input slots
	tradeSel  int         // selected offer row
	stoneSel  int         // stonecutter/loom: selected row (-1 = none)
	extraSlot invStack    // third menu input (loom pattern item / smithing template)
	horseEID  int32       // the mount whose window is open (winHorse)
	tradeWith int32       // villager eid the open trade screen belongs to
	renameTo  string
}

type hub struct {
	world      *world.World
	nether     *world.World // second dimension (nil in bare tests → worldFor falls back)
	end        *world.World // third dimension
	events     chan hubEvent
	eidCounter int64         // per-pod eid mint counter, fed through shard.MintEID when sharded
	tick       atomic.Uint64 // world age (ticks); atomic so connections can read it
	dayTime    atomic.Uint64 // time of day (ticks); advances with tick, settable by /time

	// owned reports whether this pod owns a chunk in a sharded world. nil means
	// unsharded — own the whole world (the default for a single-pod or test hub).
	// Set by Server from the region map; see shardown.go.
	shardOf      func(cx, cz int32) int32 // chunk → owning SID (nil = unsharded: own everything)
	topo         shard.Map                // region map (for shadow awareness; zero when unsharded)
	sid          int32                    // this pod's shard id (eid mint lane when sharded)
	debugBorders bool                     // dev: particle wall along region seams (-debug-borders)
	peers        peerSender               // warm world↔world links to neighbours (nil = unsharded)
	handoffs     map[string]*handoff      // player releases in flight, by migID
	migSeq       int64                    // monotonic handover id counter

	// Cross-seam shadows (shadow.go): read-only mirrors of near-border entities.
	// shadowOut = local eid → neighbour SIDs currently holding its shadow;
	// shadowIn  = inbound shadows we render to our players, by owner eid.
	shadowOut map[int32]map[int32]bool
	shadowIn  map[int32]*shadowEnt

	// World spawn (death respawn fallback): the configured -spawn, resolved to the
	// surface. Set only when this shard OWNS it; otherwise respawn falls back to a
	// point inside this shard's own region so a death never lands you off-shard.
	worldSpawnX, worldSpawnY, worldSpawnZ float64
	hasWorldSpawn                         bool

	// pendingResume holds migrated player state waiting for the gateway to
	// reconnect with Hello{Purpose:"resume", token}. Written on the hub goroutine
	// (applyMigration) and claimed on attach-session goroutines (ResumeRemote),
	// so it is mutex-guarded.
	pendingResume map[string]handover.PlayerState
	pendingMu     sync.Mutex

	// pending block updates bucketed by the tick they're due — the heart of
	// world simulation (falling blocks, fluid flow). Hub-goroutine-only.
	pending map[uint64][]blockPos

	hud []HudWidget // action-bar HUD widgets (nil = HUD off)
	bus bus         // out-of-process plugin bus (nopBus = disabled)

	// In-process plugin system (plughost.go). plugins is always non-nil so
	// emission sites never nil-check; playersRef is run()'s registry map,
	// exposed for facade methods (hub-goroutine-only reads); plugHost is nil
	// when no plugins are compiled in.
	plugins    *plugin.Dispatcher
	playersRef map[int32]*tracked
	psched     *pluginSched
	plugHost   *pluginHost
	spawnCause plugin.SpawnReason // in-force MobSpawnEvent reason (zero = SpawnNatural)
	opsRef     map[string]bool    // Server.Ops, read-only after Serve (announce targeting)

	invs       *invStore        // survival inventory persistence (nil = in-memory only)
	advs       *advStore        // advancement grant persistence (nil = in-memory only)
	statstore  *statsStore      // statistics persistence (nil = in-memory only)
	rbstore    *recipeBookStore // recipe-book persistence (nil = in-memory only)
	sb         *scoreboardState // the world scoreboard (objectives/scores/teams)
	sbstore    *sbStore         // its persistence (flushed when sbDirty)
	sbDirty    bool
	containers *containerStore // furnace/chest content persistence (nil = in-memory only)
	spawns     *spawnStore     // per-player bed respawn points (nil = world spawn only)

	signs       *signStore       // sign text (the store is the live owner — chunk builders read it)
	maps        *mapStore        // filled maps (colors + per-holder dirty tracking)
	signMayEdit map[string]int32 // transient edit locks (vanilla playerWhoMayEdit), keyed by signKey

	mobs   map[int32]*mob         // server-controlled entities (living world)
	items  map[int32]*itemEntity  // dropped-item entities (block drops)
	arrows map[int32]*arrowEntity // in-flight/stuck projectiles (skeleton shots)
	orbs   map[int32]*xpOrb       // experience orbs awaiting pickup
	rng    *rand.Rand             // hub-goroutine-only randomness (mob behaviour, drops)

	nextWin  int32                 // last container window id handed out (cycles 1..100)
	furnaces map[blockPos]*furnace // active furnace states (hub-goroutine-only)
	chests   map[blockPos]*chest   // chest storage (hub-goroutine-only)

	npcs map[int32]*npc // LLM-driven villagers (the differentiator)
	llm  *llmClient     // nil = NPCs disabled

	tnt []*primedTNT // lit TNT charges counting down

	rules     worldRules // difficulty + gamerules (persisted to rulesPath)
	rulesPath string
	// difficultyPub mirrors rules.Difficulty for connection-side reads (the
	// join sequence sends Change Difficulty outside the hub goroutine).
	difficultyPub atomic.Int32

	pressedAt map[blockPos]uint64 // button-press ticks (for the unpress timer)
	rsDue     map[blockPos]uint64 // repeater flip due-ticks
	obsPulse  map[blockPos]uint64 // observer pulse start ticks
	obsSeen   map[blockPos]uint32 // observer last-seen watched state
	compOut   map[blockPos]int    // comparator output levels (vanilla block entity)
	platesOn  map[blockPos]bool   // currently pressed pressure plates
	fireAge   map[blockPos]int    // fire-block age 0-15 (vanilla AGE property; side-mapped)
	bins      map[blockPos]*bin   // dispenser/dropper/hopper storage

	vehicles     map[int32]*vehicle        // minecarts + boats
	paintings    map[int32]*painting       // placed hanging paintings (persisted with containers)
	itemFrames   map[int32]*itemFrame      // placed item frames (persisted with containers)
	armorStands  map[int32]*armorStand     // placed armor stands (persisted with containers)
	jukeboxes    map[blockPos]*jukebox     // discs + playback clocks (persisted with containers)
	beacons      map[blockPos]*beacon      // placed beacons (chosen powers persisted with containers)
	campfires    map[blockPos]*campfire    // live cook state (item view in cfStore)
	cfStore      *campfireStore            // campfires.json + the chunk builders' read view
	banners      *bannerStore              // banners.json + the chunk builders' read view
	books        *bookStore                // books.json (contents by book id, the map model)
	lecterns     map[blockPos]*lectern     // held books + open pages (persisted with containers)
	bookshelves  map[blockPos]*[6]invStack // chiseled shelves (persisted with containers)
	detectorsOn  map[blockPos]bool         // detector rails currently pressed
	spawnerNext  map[blockPos]uint64       // dungeon spawner cooldowns
	patrolNextAt uint64                    // world tick the next pillager-patrol attempt is due
	raids        map[blockPos]*raid        // active village raids by centre
	brewProg     map[blockPos]int          // brewing stand progress (ticks)
	portalLinks  map[dimPos]dimPos         // sticky portal pairs (both directions)
	bossSeen     map[[2]int32]bool         // {playerEID, bossEID} pairs currently shown a boss bar
	openDoors    map[blockPos]uint64       // wooden doors a villager opened → tick opened (auto-close)

	dragon       *mob               // the ender dragon (nil = none / defeated)
	crystals     map[int32]*crystal // end crystals by eid
	dragonNextAt uint64             // next waypoint/swoop decision tick
	dragonSwoop  *tracked           // current dive target (nil = circling)
	villageDone  map[blockPos]bool  // villages populated this session
	outpostDone  map[blockPos]bool  // pillager outposts populated this session

	// Weather (hub-goroutine-only): the vanilla two-timer cycle + lightning.
	// raining/thundering are the level-derived gameplay booleans the rest of
	// the engine reads; the flags/timers/levels are the cycle's internals.
	raining      bool
	thundering   bool
	rainFlag     bool // vanilla WeatherData.raining (the timer's target)
	thunderFlag  bool // vanilla WeatherData.thundering
	clearTime    int  // /weather clear window (suppresses both spells)
	rainTime     int  // ticks left in the current rain spell or delay
	thunderTime  int  // ticks left in the current thunder spell or delay
	rainLevel    float32
	thunderLevel float32
	rods         map[blockPos]struct{} // lightning-rod POIs (overworld)
	bolts        []bolt

	// Each herd has its own goal the cows in it travel toward, so a herd moves as
	// one group. Goals roam slowly over land. A mob's herd index points in here.
	herds []*herd
}

// herd is a roaming goal a group of mobs steers toward (cohesion target).
type herd struct {
	x, z   float64
	vx, vz float64
}

// snapshotItems converts live dropped-item entities for persistence.
func (h *hub) snapshotItems() []savedItem {
	out := make([]savedItem, 0, len(h.items))
	for _, it := range h.items {
		si := savedItem{Dim: it.dim, X: it.x, Y: it.y, Z: it.z,
			Item: it.item, Count: it.count, Dmg: it.dmg, Ench: packEnch(it.ench),
			MapID: it.mapID, Trim: int32(it.trimMat)<<8 | int32(it.trimPat), Book: it.bookID}
		for i, l := range it.pats {
			si.Pats[i] = int32(l.patPlus1)<<8 | int32(l.color)
		}
		out = append(out, si)
	}
	return out
}

// restoreItems respawns persisted drops at boot (before any player joins —
// the join pass shows them like any other item).
func (h *hub) restoreItems(saved []savedItem) {
	none := map[int32]*tracked{}
	for _, si := range saved {
		if it := h.spawnItemIn(none, si.Dim, si.Item, si.Count, si.X, si.Y, si.Z); it != nil {
			it.dmg, it.ench, it.mapID = si.Dmg, unpackEnch(si.Ench), si.MapID
			for i, p := range si.Pats {
				it.pats[i] = bannerLayer{patPlus1: int16(p >> 8), color: int8(p & 0xff)}
			}
			it.trimMat, it.trimPat = int8(si.Trim>>8), int8(si.Trim&0xff)
			it.bookID = si.Book
		}
	}
}

// worldFor picks the world a dimension index lives in.
func (h *hub) worldFor(dim int) *world.World {
	switch {
	case dim == 1 && h.nether != nil:
		return h.nether
	case dim == 2 && h.end != nil:
		return h.end
	}
	return h.world
}

func newHub(w *world.World) *hub {
	sb, sbst := newScoreboard("") // in-memory board; server.Run swaps in the persisted one
	h := &hub{
		world:         w,
		sb:            sb,
		sbstore:       sbst,
		signs:         newSignStore(""), // in-memory; server.Run swaps in the persisted one
		books:         newBookStore(""), // in-memory; server.Run swaps in the persisted one
		signMayEdit:   map[string]int32{},
		events:        make(chan hubEvent, 256),
		pending:       map[uint64][]blockPos{},
		handoffs:      map[string]*handoff{},
		pendingResume: map[string]handover.PlayerState{},
		shadowOut:     map[int32]map[int32]bool{},
		shadowIn:      map[int32]*shadowEnt{},
		hud:           defaultHud(),
		bus:           nopBus{}, // optional; enabled via -bus / -nats
		mobs:          map[int32]*mob{},
		items:         map[int32]*itemEntity{},
		arrows:        map[int32]*arrowEntity{},
		orbs:          map[int32]*xpOrb{},
		npcs:          map[int32]*npc{},
		furnaces:      map[blockPos]*furnace{},
		chests:        map[blockPos]*chest{},
		rng:           rand.New(rand.NewSource(1)),
		rules:         defaultRules(),
		pressedAt:     map[blockPos]uint64{},
		rsDue:         map[blockPos]uint64{},
		obsPulse:      map[blockPos]uint64{},
		obsSeen:       map[blockPos]uint32{},
		compOut:       map[blockPos]int{},
		platesOn:      map[blockPos]bool{},
		fireAge:       map[blockPos]int{},
		bins:          map[blockPos]*bin{},
		vehicles:      map[int32]*vehicle{},
		paintings:     map[int32]*painting{},
		itemFrames:    map[int32]*itemFrame{},
		armorStands:   map[int32]*armorStand{},
		lecterns:      map[blockPos]*lectern{},
		bookshelves:   map[blockPos]*[6]invStack{},
		jukeboxes:     map[blockPos]*jukebox{},
		beacons:       map[blockPos]*beacon{},
		campfires:     map[blockPos]*campfire{},
		cfStore:       newCampfireStore(""), // replaced by Run when CampfireFile is set
		banners:       newBannerStore(""),

		detectorsOn: map[blockPos]bool{},
		spawnerNext: map[blockPos]uint64{},
		raids:       map[blockPos]*raid{},
		brewProg:    map[blockPos]int{},
		portalLinks: map[dimPos]dimPos{},
		bossSeen:    map[[2]int32]bool{},
		openDoors:   map[blockPos]uint64{},
		crystals:    map[int32]*crystal{},
		villageDone: map[blockPos]bool{},
		outpostDone: map[blockPos]bool{},
		rods:        map[blockPos]struct{}{},
		// Weather timers start at zero: the first tick rolls fresh vanilla
		// delays (rain 12000–180000, thunder likewise), like a new world.
	}
	h.difficultyPub.Store(int32(h.rules.Difficulty))
	h.plugins = plugin.NewDispatcher()
	h.psched = newPluginSched(h)
	globalBooks.Store(h.books) // free-function component composition (see book.go)
	return h
}

// hudRefresh is how often (in ticks) the action-bar HUD is repushed. The action
// bar fades after a few seconds, so we refresh briskly to keep it solid and the
// coordinates responsive — it's a tiny text packet.
const hudRefresh = 4 // 5×/second

// timeEv builds the world-clock event (rendered as Update Time).
func timeEv(age, dayTime uint64) attachproto.Time {
	return attachproto.Time{Age: int64(age), Time: int64(dayTime % dayLengthTicks)}
}

func chatEv(text string) attachproto.Chat { return attachproto.Chat{Text: text} }

// actionBarEv is a chat event rendered as the above-hotbar overlay.
func actionBarEv(text string) attachproto.Chat {
	return attachproto.Chat{Text: text, ActionBar: true}
}

// allocEID hands out a unique entity ID. Safe to call from any goroutine.
// allocEID and mintPlayerEID live in shardown.go (they route through the shard
// eid lanes when this pod is sharded).

// post sends an event to the hub. Blocking is acceptable: the events buffer is
// large and the hub goroutine never blocks on a single client (it uses trySend).
func (h *hub) post(ev hubEvent) { h.events <- ev }

// run is the tick loop. It owns the registry and advances the clock at 20 TPS.
func (h *hub) run() {
	if h.containers != nil { // restore furnace/chest contents from the last run
		h.furnaces = h.containers.loadFurnaces()
		h.chests = h.containers.loadChests()
		h.bins = h.containers.loadBins()
		h.restoreItems(h.containers.loadItems())
		h.paintings = h.containers.loadPaintings(h.allocEID)
		h.itemFrames = h.containers.loadFrames(h.allocEID)
		h.armorStands = h.containers.loadStands(h.allocEID)
		h.jukeboxes = h.containers.loadJukeboxes()
		h.containers.loadBeacons(h.beacons) // re-attach chosen powers to rebuilt beacons
		h.lecterns = h.containers.loadLecterns()
		h.bookshelves = h.containers.loadShelves()
		h.loadCampfires()
		for pos := range h.bins { // restart hoppers' self-scheduling chains
			if isHopper(h.world.At(pos.x, pos.y, pos.z)) {
				h.schedule(pos, hopperCadence)
			}
		}
	}
	h.reconcileFurnaceBlocks()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	players := map[int32]*tracked{}
	h.playersRef = players // created once, never reassigned — facades read it on this goroutine

	// Seed a few clustered herds on dry land near world spawn (so there's life to
	// see on join). Each herd gets its own roaming goal at its centre, and its
	// cows are tagged with that herd's index so they group up separately.
	occupied := map[[2]int]bool{} // one cow per column — no stacking at spawn
	for hi := 0; hi < 3; hi++ {
		// Spread herds out so they don't start on top of each other.
		ang := h.rng.Float64() * 2 * math.Pi
		ox, oz := int(math.Cos(ang)*60), int(math.Sin(ang)*60)
		hx, hz := h.findLand(ox, oz)
		h.herds = append(h.herds, &herd{x: float64(hx), z: float64(hz)})

		herdSize := 3 + h.rng.Intn(3) // 3..5 cows — a family, not a stampede
		for i := 0; i < herdSize; i++ {
			x, z := h.spreadSpawn(hx, hz, occupied)
			if m := h.spawnMob(players, entityCow, float64(x), float64(h.world.SurfaceFeet(x, z)), float64(z)); m != nil {
				m.behavior, m.herd = herdBehavior{}, hi
			}
		}
	}

	// Spawn a small village of LLM-driven villagers when a model is configured —
	// each with its own persona (and its own memory file + conversation history).
	if h.llm != nil {
		village := []struct {
			name, persona string
			ox, oz        int
		}{
			{"Bram", "a curious, warm-hearted village farmer who loves chatting with travellers", 6, 6},
			{"Greta", "a gruff, no-nonsense blacksmith with a hidden soft spot for newcomers", -9, 5},
			{"Pip", "a cheerful, gossipy baker who knows everyone's business and loves a good story", 7, -9},
			{"Wren", "a wise, cryptic village elder who answers in riddles and old proverbs", -7, -11},
		}
		for _, v := range village {
			nx, nz := h.findLand(v.ox, v.oz)
			h.spawnNPC(players, v.name, v.persona, float64(nx), float64(nz))
			log.Printf("spawned LLM NPC %q at (%d,%d)", v.name, nx, nz)
		}
	}

	for {
		select {
		case <-ticker.C:
			age := h.tick.Add(1) // world age; drives day/night
			dt := h.dayTime.Load()
			if h.rules.DoDaylight {
				dt = h.dayTime.Add(1)
			}
			if age%20 == 0 { // broadcast the time once a second; client interpolates
				body := timeEv(age, dt)
				for _, t := range players {
					t.p.trySendEv(body)
				}
			}
			if age%20 == 0 {
				h.jukeboxTick(players) // end songs whose length elapsed
			}
			if age%80 == 0 {
				h.beaconTick(players) // pyramid re-scan + effect refresh (vanilla cadence)
			}
			h.campfireTick(players)    // per-tick cook progress (vanilla cookTick)
			h.psched.run(age)          // plugin-scheduled tasks see the previous tick's world
			h.runUpdates(players, age) // falling blocks, fluid flow
			h.updateFurnaces(players)  // smelting progress + lit state + viewer sync
			h.runRandomTicks(players)  // growth: crops, cane, cactus, saplings, grass, leaves
			if age%mobMoveInterval == 0 {
				h.updateMobs(players)      // living world: mob behaviour + movement
				h.updateOpenDoors(players) // shut wooden doors villagers left open
				h.updateShadows(players)   // cross-seam: push near-border entities to neighbours
			}
			h.updateArrows(players) // every tick: arrows are fast enough to tunnel otherwise
			h.mapsTick(players)     // held filled maps: color scan + holder updates
			if age%10 == 0 {
				h.mapFramesTick(players) // framed maps: viewers get patches + markers
			}
			if h.debugBorders && age%10 == 0 {
				h.emitDebugBorders(players) // dev: crit-particle wall along region seams
			}
			if age%entitySyncInterval == 0 {
				h.broadcastSync(players) // absolute resync — self-heal dropped relative moves
				h.syncShadows(players)   // …and inbound cross-seam shadows
			}
			if age%20 == 0 {
				h.updateItems(players) // despawn dropped items past their lifetime
			}
			h.pickupItems(players)  // collect dropped items into survival inventories
			h.updateOrbs(players)   // collect experience orbs / expire old ones
			h.updateEating(players) // apply finished eat-holds (32-tick chew)
			h.updateSleep(players)  // turn the night once everyone's slept ~5s
			for _, t := range players {
				if t.resyncInvAt != 0 && age >= t.resyncInvAt {
					t.resyncInvAt = 0
					h.sendInventory(t) // self-heal a dropped mode-switch inventory push
				}
			}
			if age%10 == 0 {
				h.fastRegen(players) // saturation regen at vanilla's 10-tick cadence
			}
			if age%survivalTickN == 0 {
				h.survivalTick(players)   // health regen, hunger, starvation, void
				h.updateHostiles(players) // night mob spawning + daylight burn
				h.updatePatrols(players)  // roaming pillager patrols (day 5+, throttled)
				h.updateRaids(players)    // active village raids: waves, bar, win/timeout
				for _, t := range players {
					h.checkRaidTrigger(players, t) // Bad Omen + village → start a raid
				}
				h.mobEnvironment(players) // mob lava/fire/drowning/afterburn (after daylight ignites)
				h.updateSpawners(players) // dungeon spawner rooms
				h.updateVillages(players) // populate villages on approach
				h.updateOutposts(players) // populate pillager outposts on approach
				h.updatePortalDwell(players)
				h.updateEndPortalContact(players)
				h.updateDragon(players)
				if age%20 == 0 {
					h.updateDragonBar(players)
				}
				if age%4 == 0 {
					h.updateWithers(players) // spawn charge + boss bars
				}
				h.updateBrewing(players)
				h.mobAmbience(players)        // idle groans/moos near players
				h.updateBreeding(players)     // courting, babies, eggs, wool regrowth
				h.updateCopperGolems(players) // oxidation → statue
			}
			if age%passiveSpawnEvery == 0 {
				h.herdTopUp(players) // the chunk-gen herd analog (spawn.go)
			}
			h.naturalSpawn(players)  // vanilla NaturalSpawner port: all categories, all heights
			h.updateWeather(players) // vanilla per-tick cycle: timers, level ramps, lightning
			h.updateBolts(players)   // despawn finished lightning flashes
			h.updateTNT(players)     // primed charges burn their fuses
			h.updatePlates(players)
			h.updateVehicles(players)
			if age%survivalTickN == 0 {
				h.runNPCs(players) // LLM NPCs: throttled perceive → decide → act
				h.advTick(players) // polled advancement criteria (inventory, biome)
				for _, t := range players {
					h.incCustom(t, "play_time", survivalTickN)
				}
			}
			if age%600 == 0 { // persist inventories + containers every 30s (crash window)
				if h.invs != nil {
					for _, t := range players {
						h.invs.record(t.p.name, t)
					}
					h.invs.flush()
				}
				if h.advs != nil {
					for _, t := range players {
						h.advs.record(t.p.name, t.adv)
					}
					h.advs.flush()
				}
				if h.statstore != nil {
					for _, t := range players {
						h.statstore.record(t.p.name, t.stats)
					}
					h.statstore.flush()
				}
				if h.rbstore != nil {
					for _, t := range players {
						h.rbstore.record(t.p.name, t)
					}
					h.rbstore.flush()
				}
				if h.sbDirty {
					h.sbDirty = false
					h.sbstore.flush(h.sb)
				}
				h.signs.flushIfDirty()
				h.cfStore.flushIfDirty()
				h.banners.flushIfDirty()
				h.books.flushIfDirty()
				if h.maps != nil {
					h.maps.flushIfDirty()
				}
				if h.containers != nil {
					h.containers.recordFurnaces(h.furnaces)
					h.containers.recordChests(h.chests)
					h.containers.recordBins(h.bins)
					h.containers.recordItems(h.snapshotItems())
					h.containers.recordPaintings(h.paintings)
					h.containers.recordFrames(h.itemFrames)
					h.containers.recordJukeboxes(h.jukeboxes)
					h.containers.recordBeacons(h.beacons)
					h.containers.recordStands(h.armorStands)
					h.containers.recordLecterns(h.lecterns)
					h.containers.recordShelves(h.bookshelves)
					h.containers.flush()
				}
				h.saveRules() // weather timers ride settings.json (tiny file)
				if h.plugHost != nil {
					h.plugHost.flushStores()
				}
			}

			if len(h.hud) > 0 && age%hudRefresh == 0 {
				for _, t := range players {
					if !t.hudOn {
						continue
					}
					shardHud := -1
					if h.shardOf != nil {
						shardHud = int(h.sid)
					}
					v := HudView{
						Name: t.p.name, X: t.x, Y: t.y, Z: t.z, Yaw: t.yaw,
						DayTime: dt, Online: len(players), Gamemode: t.gamemode,
						Biome: h.worldFor(t.dim).BiomeAt(int(t.x), int(t.z)),
						Shard: shardHud,
					}
					t.p.trySendEv(actionBarEv(renderHud(h.hud, v)))
				}
			}

		case ev := <-h.events:
			switch e := ev.(type) {
			case evJoin:
				h.onJoin(players, e)
			case evMove:
				if t := players[e.eid]; t != nil {
					h.onMove(players, t, e)
					h.checkSeamCrossing(players, t) // hand off if they crossed into a neighbour
				}
			case evPeerFrame:
				h.handlePeerFrame(players, e.from, e.typ, e.payload)
			case evLeave:
				h.onLeave(players, e.p)
			case evBlock:
				h.onBlock(players, e)
				h.checkWitherBuild(players, e.dim, e.x, e.y, e.z, e.state)
				h.checkCopperGolemBuild(players, e.dim, e.x, e.y, e.z, e.state)
				if t := players[e.by]; t != nil {
					if e.state != 0 && e.broken == 0 {
						h.advance(players, t, "placed_block", advMatch{blockState: e.state})
					}
					if e.broken != 0 {
						if reg, ok := statBlockReg(e.broken); ok {
							h.incStat(t, attachproto.StatMined, reg, 1)
						}
					}
				}
			case evPortalLinked:
				h.portalLinks[e.from] = e.to
				h.portalLinks[e.to] = e.from
				log.Printf("portal: linked %v <-> %v", e.from, e.to)
			case evDim:
				if t := players[e.eid]; t != nil {
					h.onDimSwitch(players, t, e)
					h.advance(players, t, "changed_dimension", advMatch{dim: int32(e.dim)})
				}
			case evChat:
				if e.from == nil {
					h.roomChat(players, e.text)
					break
				}
				msg := e.text
				if plugin.Has[*plugin.PlayerChatEvent](h.plugins) {
					cev := &plugin.PlayerChatEvent{EID: e.from.eid, Name: e.from.name, Message: msg}
					if !h.plugins.Fire(cev) {
						break // cancelled: no broadcast, no NPCs, no bus
					}
					msg = cev.Message
				}
				h.roomChat(players, fmt.Sprintf("<%s> %s", e.from.name, msg))
			case evSetTime:
				h.setDayTime(e.t)
			case evAnnounce:
				line := chatEv("[" + e.name + "] " + e.text)
				for _, t := range players {
					if h.opsRef[t.p.name] {
						t.p.trySendEv(line)
					}
				}
				log.Printf("plugin announce [%s] %s", e.name, e.text)
			case evCommand:
				e.reply <- h.runPluginCommand(e.p, e.line)
			case evOpenPluginUI:
				if t := players[e.eid]; t != nil {
					h.openPluginUI(t, e.query)
				}
			case evPluginUIFill:
				h.applyPluginUIFill(players, e)
			case evPluginSync:
				e.reply <- h.plugins.Fire(e.ev)
			case evDisablePlugins:
				if h.plugHost != nil {
					h.plugHost.disableAll()
				}
				close(e.done)
			case evList:
				names := make([]string, 0, len(players))
				for _, t := range players {
					names = append(names, t.p.name)
				}
				e.p.trySendEv(chatEv(
					fmt.Sprintf("Players online (%d): %s", len(names), strings.Join(names, ", "))))
			case evSetGamemode:
				for _, t := range players {
					if t.p.name != e.name {
						continue
					}
					t.gamemode = e.mode // the hub's authoritative copy (pickup/survival sim read this)
					t.p.trySendEv(attachproto.GameEvent{Event: gameEventChangeGameMode, Value: float32(e.mode)})
					t.p.trySendEv(abilitiesFor(e.mode))
					// Modes share ONE inventory (vanilla): push it on EVERY switch so
					// the client's view matches the server in both directions, and
					// re-push a second later — a mode switch is one-shot un-resent
					// state on a lossy send (the stuck-furnace packet-drop class).
					if e.mode == gmSurvival {
						h.sendHealth(t) // (state already exists from join — don't reset it)
					}
					h.sendInventory(t)
					t.resyncInvAt = h.tick.Load() + 20
					if e.by != "" && e.by != e.name {
						t.p.trySendEv(chatEv("An operator changed your game mode."))
					}
				}
			case evPrimeTNT:
				h.primeTNT(players, e.x, e.y, e.z, tntFuseTicks)
			case evEffect:
				for _, t := range players {
					if t.p.name != e.target {
						continue
					}
					if e.clear {
						h.clearEffects(t)
					} else {
						h.applyEffect(players, t, e.id, e.amp, e.secs)
					}
				}
			case evGive:
				for _, t := range players {
					if t.p.name == e.target {
						h.giveTo(players, t, e.item, e.count)
					}
				}
			case evKill:
				for _, t := range players {
					if t.p.name == e.target {
						h.damage(players, t, 100000)
					}
				}
			case evXPLevels:
				for _, t := range players {
					if t.p.name == e.target {
						if t.xpLevel += e.levels; t.xpLevel < 0 {
							t.xpLevel = 0
						}
						h.sendExperience(t)
					}
				}
			case evEndRefresh:
				h.onEndRefresh(players, e.eid)
			case evSummon:
				h.withSpawnCause(plugin.SpawnCommand, func() {
					switch {
					case e.dim != 0: // summon into the operator's own dimension
						m := h.spawnMobIn(players, e.etype, e.dim, float64(e.x)+0.5, e.y, float64(e.z)+0.5)
						if m == nil {
							break // plugin-cancelled
						}
						if d := speciesOf(e.etype); d != nil {
							h.applySpecies(players, m) // roster species: proper stance
						} else {
							m.hostile, m.behavior = true, idleBehavior{}
						}
					case e.etype == entityCow || e.etype == entityChicken || e.etype == entityPig || e.etype == entitySheep:
						h.spawnAnimal(players, e.etype, e.x, e.z)
					case isRosterPassive(e.etype):
						h.spawnAnimal(players, e.etype, e.x, e.z) // wolves/horses/fish/… spawn peaceful
					default:
						h.spawnHostile(players, e.etype, e.x, e.z)
					}
				})
			case evUseRedstone:
				pos := blockPos{e.x, e.y, e.z}
				st := h.world.At(e.x, e.y, e.z)
				switch {
				case isButton(st):
					h.pressButton(players, pos, st)
				case isLever(st):
					h.toggleLever(players, pos, st)
				default:
					h.useRedstone1b(players, pos, st)
				}
			case evSetRule:
				h.applyRule(players, e)
			case evSetWeather:
				h.applyWeatherCommand(e)
			case evSetHud:
				if t := players[e.eid]; t != nil {
					t.hudOn = e.on
				}
			case evSetBlock:
				h.setBlockLive(players, 0, e.x, e.y, e.z, e.state)
			case evSetBehavior:
				if m := h.mobs[e.eid]; m != nil {
					h.applyBehavior(m, e.behavior)
				}
			case evDrop:
				if !h.rules.DoTileDrops {
					break // gamerule doTileDrops=false: blocks break dry
				}
				if !worldgen.HarvestableBy(e.state, e.held) {
					break // wrong tool (e.g. stone by hand) — no drops, vanilla parity
				}
				var silk, fortune int
				if t := players[e.by]; t != nil {
					held := heldStack(t)
					silk = held.enchLvl(enchSilkTouch)
					fortune = held.enchLvl(enchFortune)
				}
				if item, ok := silkTouchDrop[e.state]; ok && silk > 0 {
					// Silk Touch: the block itself, no ore XP (vanilla).
					h.spawnBlockDrop(players, item, 1, e.x, e.y, e.z)
				} else {
					for _, d := range h.rollDrops(e.state) {
						if fortune > 0 && isOreState(e.state) {
							d.count *= 1 + h.rng.Intn(fortune+1) // Fortune multiplies ore yield
						}
						h.spawnBlockDrop(players, d.item, d.count, e.x, e.y, e.z)
					}
				}
				// Ore XP: only for an actual survival miner (never creative/world).
				if t := players[e.by]; t != nil && t.gamemode == gmSurvival {
					t.exhaustion += 0.005 // vanilla: mining a block
					if xp := xpForBlock(e.state, h.rng.Intn); xp > 0 && silk == 0 {
						h.spawnXPOrb(players, xp, float64(e.x)+0.5, float64(e.y), float64(e.z)+0.5)
					}
				}
			case evScoreboardCmd:
				h.cmdScoreboard(players, e)
			case evTeamCmd:
				h.cmdTeam(players, e)
			case evSignPlaced:
				h.onSignPlaced(players, e)
			case evUseSign:
				h.onUseSign(players, e)
			case evSignUpdate:
				h.onSignUpdate(players, e)
			case evRecipeSettings:
				if t := players[e.eid]; t != nil && e.book >= 0 && e.book < 4 {
					t.rbSettings.Open[e.book] = e.open
					t.rbSettings.Filter[e.book] = e.filter
				}
			case evRecipeSeen:
				if t := players[e.eid]; t != nil {
					delete(t.rbHighlight, e.id)
				}
			case evStatsReq:
				if t := players[e.eid]; t != nil {
					t.p.trySendEv(statsSnapshot(t))
				}
			case evRespawn:
				if t := players[e.eid]; t != nil {
					h.respawn(t)
				}
			case evInsertEye:
				if t := players[e.eid]; t != nil {
					pos := blockPos{e.x, e.y, e.z}
					if st := h.world.At(e.x, e.y, e.z); isEndFrame(st) &&
						dist3(t.x, t.y, t.z, float64(e.x), float64(e.y), float64(e.z)) < maxMeleeReach+1 {
						if t.inv != nil {
							sl := &t.inv.slots[t.p.heldSlot()]
							if sl.item == itemEnderEye && sl.count > 0 {
								if t.gamemode != gmCreative {
									sl.count--
									if sl.count == 0 {
										*sl = invStack{}
									}
									h.sendSlot(t, t.p.heldSlot())
								}
								h.insertEye(players, t, pos, st)
							}
						}
					}
				}
			case evThrowEye:
				if t := players[e.eid]; t != nil {
					h.throwEye(players, t)
				}
			case evFillBottle:
				if t := players[e.eid]; t != nil {
					h.fillBottle(t, e.slot)
				}
			case evEat:
				if t := players[e.eid]; t != nil {
					h.startEating(t, e.slot)
				}
			case evUseMap:
				if t := players[e.eid]; t != nil {
					h.mapCreateFilled(players, t)
				}
			case evStopEat:
				if t := players[e.eid]; t != nil {
					h.stopEating(players, t)
					h.lowerShield(t) // release / hotbar switch also drops a shield
					if e.fire {      // release_use_item looses a drawn bow…
						h.releaseDraw(players, t)
					} else { // …a hotbar switch just lowers it
						t.drawingAt = 0
					}
				}
			case evBowStart:
				if t := players[e.eid]; t != nil {
					h.startDraw(t)
				}
			case evBlockStart:
				if t := players[e.eid]; t != nil {
					h.raiseShield(t)
				}
			case evThrow:
				if t := players[e.eid]; t != nil {
					h.throwProjectile(players, t, e.item)
				}
			case evAttack:
				if h.hitCrystal(players, e.target) {
					break
				}
				if pt := h.paintings[e.target]; pt != nil {
					h.breakPainting(players, pt, players[e.attacker])
					break
				}
				if st := h.armorStands[e.target]; st != nil {
					h.hitStand(players, players[e.attacker], st)
					break
				}
				if f := h.itemFrames[e.target]; f != nil {
					h.hitFrame(players, players[e.attacker], f)
					break
				}
				if v := h.vehicles[e.target]; v != nil {
					h.breakVehicle(players, v)
				} else {
					h.attackMob(players, e.attacker, e.target)
				}
			case evPlaceVehicle:
				if t := players[e.eid]; t != nil {
					h.placeVehicle(players, t, e)
				}
			case evPlacePainting:
				h.onPlacePainting(players, e)
			case evPlaceFrame:
				h.onPlaceFrame(players, e)
			case evNoteBlock:
				h.onNoteBlock(players, e)
			case evUseJukebox:
				h.onUseJukebox(players, e)
			case evVehicleMove:
				if t := players[e.eid]; t != nil {
					if !h.applyGhastMove(players, t, e) && // piloted happy ghast first…
						!h.applyMountMove(players, t, e) { // …then a ridden animal…
						h.applyVehicleMove(players, t, e) // …else a boat/minecart
					}
				}
			case evDismount:
				if t := players[e.eid]; t != nil {
					if !h.leaveGhast(players, t) && !h.dismountMob(players, t) {
						h.dismount(players, t)
					}
				}
			case evInteractMob:
				if t := players[e.eid]; t != nil {
					if st := h.armorStands[e.target]; st != nil {
						h.interactStand(players, t, st)
						break
					}
					if f := h.itemFrames[e.target]; f != nil {
						h.interactFrame(players, t, f)
						break
					}
					if v := h.vehicles[e.target]; v != nil {
						h.mountVehicle(players, t, v)
						break
					}
					if m := h.mobs[e.target]; m != nil && m.etype == entityVillager && m.dying == 0 &&
						dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= maxMeleeReach {
						h.openTrades(t, m)
						break
					}
					if m := h.mobs[e.target]; m != nil && m.dying == 0 &&
						dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= maxMeleeReach {
						_ = h.tryHorseScreen(players, t, m, e.sneak) || h.tryHappyGhast(players, t, m) ||
							h.tryCopperGolem(players, t, m) || h.tryMount(players, t, m) ||
							h.tryTame(players, t, m) || h.shearSheep(players, t, m) || h.feedAnimal(players, t, m)
					}
				}
			case evClick:
				h.handleClick(players, e)
			case evCloseWin:
				if t := players[e.eid]; t != nil && t.inv != nil {
					h.closeWindow(players, t)
				}
			case evTossHeld:
				if t := players[e.eid]; t != nil && t.gamemode == gmSurvival {
					h.tossHeld(players, t, e.slot, e.all)
					h.incCustom(t, "drop", 1)
				}
			case evCreativeSlot:
				// Modes share ONE inventory (vanilla): creative slot sets write
				// through to the hub's copy, so server inventory pushes don't
				// revert the hotbar and a switch back to survival keeps the items.
				// Window-0 numbering only applies while no container is open.
				// AUTHORITY: only an actually-creative player may conjure items —
				// a hacked survival client sending set_creative_slot is ignored.
				if t := players[e.eid]; t != nil && t.gamemode == gmCreative && t.inv != nil && t.winID == 0 {
					if ptr, hot := h.winSlotPtr(t, e.slot); ptr != nil {
						*ptr = e.st
						if hot >= 0 {
							t.p.setHotbarSlot(hot, e.st.item)
						}
						h.broadcastEquipment(players, t)
					}
				}
			case evOpenCraft:
				if t := players[e.eid]; t != nil {
					h.openCraftingTable(t)
				}
			case evOpenFurnace:
				if t := players[e.eid]; t != nil {
					h.openFurnace(t, e.x, e.y, e.z)
					switch kind, _ := furnaceKindOf(h.worldFor(t.dim).At(e.x, e.y, e.z)); kind {
					case cookBlast:
						h.incCustom(t, "interact_with_blast_furnace", 1)
					case cookSmoker:
						h.incCustom(t, "interact_with_smoker", 1)
					default:
						h.incCustom(t, "interact_with_furnace", 1)
					}
				}
			case evOpenChest:
				if t := players[e.eid]; t != nil {
					h.openChest(t, e.x, e.y, e.z)
					h.incCustom(t, "open_chest", 1)
				}
			case evOpenBin:
				if t := players[e.eid]; t != nil {
					h.openBin(t, e.x, e.y, e.z)
				}
			case evOpenAnvil:
				if t := players[e.eid]; t != nil {
					h.openAnvil(t)
				}
			case evOpenGrind:
				if t := players[e.eid]; t != nil {
					h.openGrindstone(t)
				}
			case evOpenCarto:
				if t := players[e.eid]; t != nil {
					h.openCartography(t)
					h.incCustom(t, "interact_with_cartography_table", 1)
				}
			case evOpenStonecut:
				if t := players[e.eid]; t != nil {
					h.openStonecutter(t)
					h.incCustom(t, "interact_with_stonecutter", 1)
				}
			case evOpenLoom:
				if t := players[e.eid]; t != nil {
					h.openLoom(t)
					h.incCustom(t, "interact_with_loom", 1)
				}
			case evEditBook:
				if t := players[e.eid]; t != nil {
					h.onEditBook(t, e)
				}
			case evWhisper:
				h.onWhisper(players, e)
			case evKick:
				h.onKick(players, e)
			case evClearInv:
				h.onClearInv(players, e)
			case evSetSpawnpoint:
				h.onSetSpawnpoint(players, e)
			case evPlaysound:
				h.onPlaysound(players, e)
			case evParticleCmd:
				h.toNearbyEv(players, 0, e.x, e.z, attachproto.Particles{
					PID: e.pid, X: e.x, Y: e.y, Z: e.z, Spread: 0.5, Count: e.count})
			case evUseLectern:
				h.onUseLectern(players, e)
			case evUseShelf:
				h.onUseShelf(players, e)
			case evPlaceStand:
				h.onPlaceStand(players, e)
			case evOpenSmith:
				if t := players[e.eid]; t != nil {
					h.openSmithing(t, e.x, e.y, e.z)
					h.incCustom(t, "interact_with_smithing_table", 1)
				}
			case evOpenBeacon:
				if t := players[e.eid]; t != nil {
					h.openBeacon(t, e.x, e.y, e.z)
					h.incCustom(t, "interact_with_beacon", 1)
				}
			case evSetBeacon:
				if t := players[e.eid]; t != nil {
					h.onSetBeacon(players, t, e.primary, e.secondary)
				}
			case evCampfireAdd:
				h.onCampfireAdd(players, e)
			case evRename:
				if t := players[e.eid]; t != nil && t.winKind == winAnvil {
					t.renameTo = e.name
					h.sendTwoSlotWindow(t)
				}
			case evOpenEnchant:
				if t := players[e.eid]; t != nil {
					h.openEnchantTable(t, e.x, e.y, e.z)
				}
			case evSelTrade:
				if t := players[e.eid]; t != nil && t.winKind == winTrade {
					t.tradeSel = int(e.slot)
					h.sendTradeWindow(t)
				}
			case evEnchant: // container_button_click: enchant option or stonecutter row
				if t := players[e.eid]; t != nil {
					switch t.winKind {
					case winStonecut:
						h.stonecutSelect(t, e.button)
					case winLoom:
						h.loomSelect(t, e.button)
					case winLectern:
						h.lecternButton(players, t, e.button)
					default:
						h.handleEnchant(players, t, e.button)
					}
				}
			case evUseBed:
				if t := players[e.eid]; t != nil {
					h.handleUseBed(players, t, blockPos{e.x, e.y, e.z})
				}
			case evToolWear:
				if t := players[e.eid]; t != nil {
					h.applyToolWear(t, e.slot, 1)
				}
			case evSneak:
				if t := players[e.eid]; t != nil {
					pose := int32(poseStanding)
					if e.sneaking {
						pose = poseSneaking
					}
					h.toNearbyEv(players, t.dim, t.x, t.z, metaEv(poseMeta(t.p.eid, pose)))
				}
			case evStopSleep:
				if t := players[e.eid]; t != nil {
					h.wakePlayer(players, t)
				}
			case evHeldChange:
				if t := players[e.eid]; t != nil {
					h.broadcastEquipment(players, t) // new item in hand
				}
			case evCraftRequest:
				if t := players[e.eid]; t != nil {
					h.placeRecipe(players, t, e)
				}
			case evNPCDecision:
				if n := h.npcs[e.eid]; n != nil {
					n.inFlight = false
					if e.action != nil {
						h.npcAct(players, n, *e.action)
					}
				}
			case evConsume:
				if t := players[e.eid]; t != nil && t.gamemode == gmSurvival && t.inv != nil && e.slot >= 0 && e.slot < 9 {
					if sl := &t.inv.slots[e.slot]; sl.count > 0 {
						sl.count--
						if sl.count == 0 {
							sl.item = 0
						}
						h.sendSlot(t, int(e.slot)) // updates client + mirrors hotbar
					}
				}
			case evSaveState:
				if h.invs != nil {
					for _, t := range players {
						h.invs.record(t.p.name, t)
					}
					h.invs.flush()
				}
				if h.containers != nil {
					h.containers.recordFurnaces(h.furnaces)
					h.containers.recordChests(h.chests)
					h.containers.recordBins(h.bins)
					h.containers.recordItems(h.snapshotItems())
					h.containers.recordPaintings(h.paintings)
					h.containers.recordFrames(h.itemFrames)
					h.containers.recordJukeboxes(h.jukeboxes)
					h.containers.recordBeacons(h.beacons)
					h.containers.recordStands(h.armorStands)
					h.containers.recordLecterns(h.lecterns)
					h.containers.recordShelves(h.bookshelves)
					h.containers.flush()
				}
				h.signs.flushIfDirty()
				h.cfStore.flushIfDirty()
				h.banners.flushIfDirty()
				h.books.flushIfDirty()
				if h.maps != nil {
					h.maps.flushIfDirty()
				}
				if h.plugHost != nil {
					h.plugHost.flushStores()
				}
				close(e.done)
			}
		}
	}
}

// onJoin registers the newcomer and exchanges spawn packets with everyone else:
// the newcomer learns of every existing player and vice-versa.
func (h *hub) onJoin(players map[int32]*tracked, e evJoin) {
	nt := &tracked{p: e.p, x: e.x, y: e.y, z: e.z, yaw: e.yaw, pitch: e.pitch, gamemode: e.gamemode, hudOn: true}
	if e.resume != nil {
		// A migrated player: the handover snapshot is the source of truth (health,
		// food, effects, inventory, xp) — not a fresh spawn or the on-disk store.
		nt.applyPlayerState(*e.resume)
		h.sendExperience(nt)
	} else {
		initSurvival(nt)
		if h.invs != nil { // restore a persisted inventory
			h.invs.loadInto(nt, e.p.name)
			h.sendExperience(nt) // restore the XP bar with the loadout
		}
	}
	if nt.gamemode == gmSurvival { // sync the survival HUD (hearts/hunger + inventory)
		h.sendHealth(nt)
		h.sendInventory(nt)
	}
	if h.advs != nil { // advancement state + the tree (a resume reloads this pod's store)
		nt.adv = h.advs.load(e.p.name)
	} else {
		nt.adv = advState{}
	}
	h.advSendAll(nt)
	if h.statstore != nil {
		nt.stats = h.statstore.load(e.p.name)
	}
	if h.rbstore != nil {
		h.rbstore.loadInto(nt, e.p.name)
	} else {
		nt.rbKnown, nt.rbHighlight = map[int32]bool{}, map[int32]bool{}
	}
	h.recipeSendInitial(nt)
	h.sbSendAll(nt)

	// (The newcomer's initial world clock is sent reliably in handlePlay, as part
	// of the join stream, so it isn't dropped in the join packet flood.)
	for _, t := range players {
		// Tab-list entries are global; entity visibility is per-dimension
		// (cross-dim views swap on dimension switch, not at join).
		t.p.trySendEv(infoAdd(e.p))
		e.p.trySendEv(infoAdd(t.p))
		if t.dim != nt.dim {
			continue
		}
		t.p.trySendEv(entAdd(e.p.eid, playerEntityType, e.p.uuid, nt.x, nt.y, nt.z, nt.yaw, nt.pitch))
		// Gear rides with the spawn in BOTH directions (vanilla sends
		// set_equipment right after add_entity; without this the newcomer's
		// armor is invisible to others until the 2 s resync).
		t.p.trySendEv(equipEv(e.p.eid, heldStack(nt), nt.offhand, nt.armor))
		e.p.trySendEv(entAdd(t.p.eid, playerEntityType, t.p.uuid, t.x, t.y, t.z, t.yaw, t.pitch))
		e.p.trySendEv(equipEv(t.p.eid, heldStack(t), t.offhand, t.armor))
		if t.sleeping { // …lying down, if they're mid-sleep
			e.p.trySendEv(metaEv(sleepMetadata(t.p.eid, t.sleepPos)))
		}
	}
	players[e.p.eid] = nt
	if e.resume != nil {
		// A crossing completed: this player was rendered here as an inbound shadow
		// while it approached. The real entity (same eid) now supersedes it — drop
		// the shadow bookkeeping so syncShadows won't fight onMove over the eid.
		h.dropShadowSuperseded(e.p.eid)
	}

	h.sendVehiclesTo(nt)
	h.sendPaintingsTo(nt)
	h.sendFramesTo(nt)
	h.sendStandsTo(nt)
	h.waypointOnJoin(players, nt)
	// Show the newcomer every mob already in their dimension.
	for _, m := range h.mobs {
		if m.dim != nt.dim {
			continue
		}
		e.p.trySendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, m.yaw, 0))
		if m.burning {
			e.p.trySendEv(metaEv(fireMetadata(m.eid, true)))
		}
		if m.etype == entitySkeleton {
			e.p.trySendEv(skeletonEquip(m.eid))
		}
		if m.baby {
			e.p.trySendEv(metaEv(babyMeta(m.eid, true)))
		}
		if m.sheared {
			e.p.trySendEv(metaEv(sheepMeta(m.eid, true)))
		}
	}
	h.showShadowsTo(nt) // …and every cross-seam shadow (neighbour entities near the border).
	// …and every dropped item and waiting XP orb in their dimension.
	for _, it := range h.items {
		if it.dim != nt.dim {
			continue
		}
		e.p.trySendEv(entAdd(it.eid, entityItem, it.uuid, it.x, it.y, it.z, 0, 0))
		e.p.trySendEv(metaEv(itemMetadata(it.eid, it.item, it.count)))
	}
	for _, o := range h.orbs {
		if o.dim != nt.dim {
			continue
		}
		e.p.trySendEv(entAdd(o.eid, entityXPOrb, o.uuid, o.x, o.y, o.z, 0, 0))
	}

	if h.rainLevel > 0 { // late joiners start under the same sky
		h.sendWeather(nt)
	}
	h.plugins.Fire(&plugin.PlayerJoinEvent{EID: e.p.eid, Name: e.p.name, X: e.x, Y: e.y, Z: e.z, Dim: nt.dim})
}

// onMove relays motion to everyone else as an EntityMove event (absolute) plus
// a head rotation. Each viewer's renderer turns the stream into wire format —
// relative entity_move_look deltas against what THAT viewer last saw, with
// absolute resyncs on first sight / big jumps / every 40th move (render770).
// We deliberately avoid Teleport Entity on the wire: its 1.21.5 layout was
// reworked (velocity + f32 angles) in a way minecraft-data still mis-lists,
// and a wrong byte count there desyncs and disconnects the client.
func (h *hub) onMove(players map[int32]*tracked, t *tracked, e evMove) {
	if !h.validateMove(t, e) {
		return // impossible move — not applied, client rubber-banded back
	}
	fromX, fromY, fromZ := t.x, t.y, t.z                   // pre-move position (plugin move event)
	h.onFallAndExhaust(players, t, e)                      // fall damage + walking hunger (reads pre-move position)
	if d := math.Hypot(e.x-t.x, e.z-t.z); d > 0 && d < 8 { // cm, teleports excluded
		name := "walk_one_cm"
		if e.sprinting {
			name = "sprint_one_cm"
		}
		h.incCustom(t, name, int32(d*100))
	}
	wpMoved := int32(t.x) != int32(e.x) || int32(t.y) != int32(e.y) || int32(t.z) != int32(e.z)
	t.x, t.y, t.z = e.x, e.y, e.z
	t.yaw, t.pitch, t.onGround, t.sprinting = e.yaw, e.pitch, e.onGround, e.sprinting
	h.wakeIfAway(players, t) // walking off ends a bed sleep

	// Interest management: relay this move only to players whose loaded-chunk
	// window overlaps ours. A player N chunks away can't see us, so forwarding
	// them 20 moves/sec is pure waste — and unfiltered it's O(players²) packets
	// per tick, the first thing that buckles under load (mobs already filter via
	// toNearby; players used to broadcast to everyone). Entities out of range
	// simply hold their last position; the 2 s absolute resync (broadcastSync,
	// itself range-filtered) re-baselines anyone who comes back into view.
	move := entMove(e.eid, t.x, t.y, t.z, e.yaw, e.pitch, e.onGround)
	head := entHead(e.eid, e.yaw)
	cx, cz := chunkFloor(t.x), chunkFloor(t.z)
	for eid, other := range players {
		if other.dim != t.dim {
			continue // another dimension — invisible to each other
		}
		if eid == e.eid {
			continue
		}
		if abs(chunkFloor(other.x)-cx) > viewRadius || abs(chunkFloor(other.z)-cz) > viewRadius {
			continue
		}
		other.p.trySendEv(move)
		other.p.trySendEv(head)
	}
	h.waypointOnMove(players, t, wpMoved)
	if plugin.Has[*plugin.PlayerMoveEvent](h.plugins) { // hot path: never build the event unheard
		h.plugins.Fire(&plugin.PlayerMoveEvent{EID: e.eid, Name: t.p.name,
			FromX: fromX, FromY: fromY, FromZ: fromZ, ToX: t.x, ToY: t.y, ToZ: t.z, Dim: t.dim})
	}
}

// onLeave removes the player and tells everyone else to despawn them.
func (h *hub) onLeave(players map[int32]*tracked, p *player) {
	t, ok := players[p.eid]
	if !ok {
		return
	}
	if t.inv != nil { // fold the crafting grid + cursor back so nothing is lost
		h.reclaimCraft(players, t) // (armor + offhand stay worn — they persist)
		h.reclaimEnchant(players, t)
		h.reclaimAnvil(players, t)
	}
	if h.invs != nil { // persist the survival loadout on disconnect
		h.invs.save(p.name, t)
	}
	if h.advs != nil {
		h.advs.save(p.name, t.adv)
	}
	h.incCustom(t, "leave_game", 1)
	for k, eid := range h.signMayEdit { // release any sign edit lock they held
		if eid == p.eid {
			delete(h.signMayEdit, k)
		}
	}
	if h.statstore != nil {
		h.statstore.save(p.name, t.stats)
	}
	if h.rbstore != nil {
		h.rbstore.save(p.name, t)
	}
	for _, v := range h.vehicles { // a leaver stands up first
		if v.rider == p.eid {
			v.rider = 0
			h.toNearbyEv(players, v.dim, v.x, v.z, passengersBody(v.eid))
		}
	}
	for _, m := range h.mobs { // a leaver aboard a happy ghast steps off
		for i, r := range m.riders {
			if r == p.eid {
				m.riders = append(m.riders[:i], m.riders[i+1:]...)
				h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, m.riders...))
				break
			}
		}
	}
	delete(players, p.eid) // (if the leaver was the last one awake, the tick
	//                        loop's updateSleep turns the night on its own)
	rm := infoGone(p.uuid)
	h.waypointOnLeave(players, p)
	for _, t := range players {
		t.p.trySendEv(rm)
		t.p.trySendEv(entGone(p.eid))
	}
	h.shadowGoneAll(p.eid) // retract any cross-seam shadow of the leaver
	h.plugins.Fire(&plugin.PlayerQuitEvent{EID: p.eid, Name: p.name})
}

// onBlock relays an applied edit to every other player tracking that chunk, so
// builds appear for everyone (the editor already saw its own prediction).
func (h *hub) onBlock(players map[int32]*tracked, e evBlock) {
	bcx, bcz := chunkFloor(float64(e.x)), chunkFloor(float64(e.z))
	body := blockSetEv(e.x, e.y, e.z, e.state)
	for eid, t := range players {
		if eid == e.by || t.dim != e.dim {
			continue
		}
		if abs(chunkFloor(t.x)-bcx) > viewRadius || abs(chunkFloor(t.z)-bcz) > viewRadius {
			continue
		}
		t.p.trySendEv(body)
		if e.broken != 0 { // break particles + sound, rendered from the old state
			t.p.trySendEv(blockBreakEvent(e.x, e.y, e.z, e.broken))
		}
	}
	h.cascadeOrphanPortals(players, e.dim, blockPos{e.x, e.y, e.z}) // works in every dim
	if _, isSign := signKind(e.state); !isSign {                    // a sign was broken or overwritten
		h.signs.remove(e.dim, e.x, e.y, e.z)
		delete(h.signMayEdit, signKey(e.dim, e.x, e.y, e.z))
	}
	h.paintingsOnBlockChange(players, e.dim, e.x, e.y, e.z)
	h.framesOnBlockChange(players, e.dim, e.x, e.y, e.z)
	if e.dim != 0 {
		return // v1: block simulation (falling/fluids/redstone) runs overworld-only
	}
	h.rodIndexOnBlockChange(e.x, e.y, e.z, e.state) // lightning-rod POI set
	h.beaconsOnBlockChange(players, e.x, e.y, e.z, e.state)
	h.bannersOnBlockChange(players, e.x, e.y, e.z, e.state, e.by)
	// A player edit can trigger simulation: the block itself (a placed falling
	// block or fluid) and its neighbours (sand above loses support, fluid flows
	// into the new gap) all re-evaluate next tick.
	h.scheduleAround(blockPos{e.x, e.y, e.z}, 1)
	h.spillContainer(players, e.x, e.y, e.z, e.state) // broken chest/furnace scatters its contents
	h.bus.publish("block_change", map[string]any{"x": e.x, "y": e.y, "z": e.z, "state": e.state, "by": e.by})
}

// chunkFloor maps a world coordinate to its chunk index (floors toward -inf).
func chunkFloor(v float64) int { return int(math.Floor(v / 16)) }

// --- shared mutation helpers -----------------------------------------------
//
// One implementation each for a mutation reachable two ways: an evXxx case
// (posted by sessions/bus) and a plugin facade method (already on the hub
// goroutine, calling directly).

// roomChat sends a chat line to everyone, and lets NPCs + the bus hear it —
// the full chat sink, unlike raid.go's quiet broadcastChat announcement.
func (h *hub) roomChat(players map[int32]*tracked, text string) {
	body := chatEv(text)
	for _, t := range players {
		t.p.trySendEv(body)
	}
	log.Printf("chat: %s", text)
	h.npcsHear(text) // so NPCs can hear and remember the room
	h.bus.publish("chat", map[string]any{"text": text})
}

// giveTo adds items to a player's inventory, spilling the remainder at their
// feet (the /give behavior).
func (h *hub) giveTo(players map[int32]*tracked, t *tracked, item int32, count int) {
	if t.inv == nil {
		return
	}
	st := invStack{item: item, count: count}
	changed, left := t.inv.addStack(st)
	for _, sl := range changed {
		h.sendSlot(t, sl)
	}
	if left > 0 {
		h.spawnItem(players, item, left, t.x, t.y, t.z)
	}
}

// setBlockLive applies a world-driven block change (bus/plugin): set,
// broadcast to the dimension's viewers, and schedule simulation (overworld
// only — block sim is v1 overworld-only, like onBlock).
func (h *hub) setBlockLive(players map[int32]*tracked, dim, x, y, z int, state uint32) {
	h.worldFor(dim).SetBlock(x, y, z, state)
	bcx, bcz := chunkFloor(float64(x)), chunkFloor(float64(z))
	body := blockSetEv(x, y, z, state)
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		if abs(chunkFloor(t.x)-bcx) <= viewRadius && abs(chunkFloor(t.z)-bcz) <= viewRadius {
			t.p.trySendEv(body)
		}
	}
	if dim == 0 {
		h.rodIndexOnBlockChange(x, y, z, state)
		h.beaconsOnBlockChange(players, x, y, z, state)
		h.scheduleAround(blockPos{x, y, z}, 1)
	}
	h.bus.publish("block_change", map[string]any{"x": x, "y": y, "z": z, "state": state, "by": "world"})
}

// teleportPlayer moves a player server-side (plugin/bus): position sync to
// the mover, absolute entity move to everyone else in the dimension.
func (h *hub) teleportPlayer(players map[int32]*tracked, t *tracked, x, y, z float64) {
	t.x, t.y, t.z = x, y, z
	t.p.trySendEv(teleportEv(x, y, z, t.yaw, t.pitch))
	move := entMove(t.p.eid, x, y, z, t.yaw, t.pitch, true)
	for eid, other := range players {
		if eid == t.p.eid || other.dim != t.dim {
			continue
		}
		other.p.trySendEv(move)
	}
}

// setDayTime sets the day clock explicitly (command, bus, plugin) and fires
// the plugin TimeSetEvent — the natural per-tick advance never comes here.
// Hub goroutine only (handlers run inline).
func (h *hub) setDayTime(v uint64) {
	old := h.dayTime.Load()
	h.dayTime.Store(v)
	if old != v {
		h.plugins.Fire(&plugin.TimeSetEvent{Old: old, New: v})
	}
}

// --- entity domain events -------------------------------------------------
//
// The hub emits TYPED EVENTS (the attach frame types) for the entity family
// instead of prebuilt packets; the consumer renders them — render770 for TCP
// connections (play.go writeLoop), attach frames for gateway sessions
// (remote.go). Positions are ABSOLUTE: relative-move deltas are a wire
// concern, computed per viewer by the renderer against what that viewer
// actually saw, which makes dropped events self-healing by construction.

// angleByte encodes degrees as Minecraft's 1/256-turn signed byte.
func angleByte(deg float32) byte { return byte(int32(deg * 256.0 / 360.0)) }

func entAdd(eid int32, etype int, uuid [16]byte, x, y, z float64, yaw, pitch float32) attachproto.EntityAdd {
	return attachproto.EntityAdd{EID: eid, UUID: uuid, Type: int32(etype), X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch}
}

func entMove(eid int32, x, y, z float64, yaw, pitch float32, onGround bool) attachproto.EntityMove {
	return attachproto.EntityMove{EID: eid, X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch, OnGround: onGround}
}

func entHead(eid int32, yaw float32) attachproto.EntityHead {
	return attachproto.EntityHead{EID: eid, Yaw: yaw}
}

func entGone(eids ...int32) attachproto.EntityRemove {
	return attachproto.EntityRemove{EIDs: eids}
}

// infoAdd announces a player to the tab list / entity renderer, with the
// game-profile textures blob (skins) when online mode supplied one.
func infoAdd(p *player) attachproto.PlayerInfo {
	pi := attachproto.PlayerInfo{UUID: p.uuid, Name: p.name}
	for _, pr := range p.props {
		pi.Props = append(pi.Props, attachproto.Property{Name: pr.Name, Value: pr.Value, Signature: pr.Signature})
	}
	return pi
}

func infoGone(uuid [16]byte) attachproto.PlayerGone { return attachproto.PlayerGone{UUID: uuid} }

// teleportEv is the server-authoritative position set (rubber-band, /tp,
// portal arrival, respawn) — the session renders it as a position sync and
// re-centers its chunk view.
func teleportEv(x, y, z float64, yaw, pitch float32) attachproto.Teleport {
	return attachproto.Teleport{Pos: attachproto.Pos{X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch}}
}

// blockSetEv is the domain form of a single block change.
func blockSetEv(x, y, z int, state uint32) attachproto.BlockSet {
	return attachproto.BlockSet{X: x, Y: y, Z: z, State: state}
}

// chatNBT encodes plain text as a network-NBT text component: a nameless root
// TAG_String (type 8), which the client reads as {"text": s}. ASCII-safe.
func chatNBT(s string) []byte {
	s = sanitizeNBT(s)
	b := []byte{0x08, byte(len(s) >> 8), byte(len(s))}
	return append(b, s...)
}

// sanitizeNBT makes text safe for a network-NBT TAG_String: it drops the null
// char and supplementary (>U+FFFF) code points, which aren't valid "modified
// UTF-8" and make the client fail to decode the chat packet (a single emoji from
// the LLM or a player would disconnect them). BMP, non-null runes encode the same
// in modified UTF-8 as in Go's UTF-8, so the byte-length prefix stays correct.
func sanitizeNBT(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= 256 { // cap length (well under the 2-byte length field)
			break
		}
		if r == 0 || r > 0xFFFF {
			r = '?'
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
