// Package server accepts client connections and drives them through the
// Minecraft connection-state machine. Milestone 1 implements Handshake +
// Status (the server-list ping); Login and Play come later.
package server

import (
	"github.com/tachyne/tachyne-common/access"
	"github.com/tachyne/tachyne-world/internal/attach"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-common/shard"

	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
)

// idSpaceVersion is the canonical content version: the id space every
// persisted block state, item, entity type and statistic is written in. Bump
// it with the canonical version and the migrations below carry saves across.
const idSpaceVersion = "26.3"

// savedIDSpace reads the id space a marker file records. No marker means a
// save from before markers existed: the 1.21.5 canonical.
func savedIDSpace(marker string) string {
	b, err := os.ReadFile(marker)
	if err != nil {
		return "1.21.5"
	}
	return strings.TrimSpace(string(b))
}

// migrateEditsIDSpace remaps every persisted block edit from the id space its
// marker records to the canonical one, exactly once. Every state is checked
// before any is changed, so a state with no canonical counterpart leaves the
// world as it was rather than half migrated.
func (s *Server) migrateEditsIDSpace() error {
	marker := filepath.Join(filepath.Dir(s.WorldFile), ".idspace")
	from := savedIDSpace(marker)
	if from == idSpaceVersion {
		return nil
	}
	worlds := []*world.World{s.world, s.nether, s.end}
	for _, pass := range []string{"check", "apply"} {
		if pass == "apply" {
			// The edits are rewritten in place: keep each dimension's file as
			// it was, as the JSON stores keep theirs.
			dir := filepath.Dir(s.WorldFile)
			for _, f := range []string{s.WorldFile, filepath.Join(dir, "nether.gob"), filepath.Join(dir, "end.gob")} {
				if _, err := os.Stat(f); err != nil {
					continue
				}
				if err := copyFile(f, f+".pre-"+idSpaceVersion); err != nil {
					return fmt.Errorf("id-space migration: backing up %s: %w", f, err)
				}
			}
		}
		m, err := newContentMigrator(from)
		if err != nil {
			return err
		}
		remap := func(state uint32) uint32 {
			if state == 0 {
				return 0
			}
			n := uint32(m.remap(protocol.RegBlockState, int64(state)))
			if pass == "check" {
				return state
			}
			return n
		}
		total := 0
		for _, w := range worlds {
			if w == nil {
				continue
			}
			n, err := w.MigrateEdits(remap)
			if err != nil {
				return err
			}
			total += n
		}
		if m.err != nil {
			return m.err
		}
		if pass == "apply" {
			log.Printf("id-space migration: remapped %d block edits %s→%s", total, from, idSpaceVersion)
		}
	}
	return writeAtomic(marker, []byte(idSpaceVersion+"\n"))
}

// migrateContentIDSpace carries every JSON store holding built-in registry
// ids — inventories, containers, mobs, statistics — from the id space its
// marker records to the canonical one, exactly once, before any store loads
// (see contentmigrate.go).
func (s *Server) migrateContentIDSpace() error {
	marker := filepath.Join(filepath.Dir(s.WorldFile), ".itemspace")
	from := savedIDSpace(marker)
	if from == idSpaceVersion {
		return nil
	}
	n, err := migrateContent(contentFiles{
		Inventories: s.InventoryFile, Containers: s.ContainerFile,
		Mobs: s.MobFile, Stats: s.StatsFile,
	}, from, idSpaceVersion)
	if err != nil {
		return err
	}
	logContentMigration(n, from, idSpaceVersion)
	return writeAtomic(marker, []byte(idSpaceVersion+"\n"))
}

// autosaveInterval is how often the world's edits are flushed to disk while
// running (a no-op when nothing changed). Bounds how much building a hard kill
// can lose; graceful shutdown also saves once more.
const autosaveInterval = 30 * time.Second

// Server holds configuration and serves attach sessions (gateways are the
// only way in — the engine has no Minecraft socket; see docs/DOMAIN-EVENTS.md).
type Server struct {
	MOTD       string
	MaxPlayers int
	Seed       int64

	// Stop is /stop: the engine's graceful shutdown (save, then exit). Nil
	// where there is no process to stop (tests, embedding).
	Stop func()

	// Spawn overrides the join position when SpawnSet is true; otherwise players
	// spawn on the surface at (0,0). A dev affordance for dropping into a chosen
	// spot (e.g. a cave) until commands/teleport exist. SpawnAuto resolves Y to
	// the surface of (SpawnX,SpawnZ) so a chosen column never drops the player in
	// from mid-air (the -spawn x,z form).
	SpawnSet               bool
	SpawnAuto              bool
	SpawnX, SpawnY, SpawnZ float64

	// Earth mode: overworld terrain from an embedded real elevation model
	// (worldgen/earth.go). EarthName selects the grid ("capetown");
	// EarthVScale is metres of real elevation per block above sea level.
	EarthName   string
	EarthVScale float64

	// Ceiling raises the OVERWORLD's top build limit (0 = vanilla 320). Tall
	// worlds exist for earth mode at true vertical scale: pick Ceiling and
	// EarthVScale together so the region's summits fit (e.g. Cape Town's
	// 1,587 m Hottentots-Holland at vscale 1 wants -ceiling 1664). Nether and
	// End stay vanilla height; Bedrock clients see the world clamped at 320
	// (platform limit).
	Ceiling int

	// MobFile, if set, persists live mobs (entities) to that JSON file so herds,
	// farm animals and tamed pets survive a restart ("" = in-memory only).
	MobFile string

	// CullAnimals, if > 0, runs a ONE-TIME maintenance pass at boot that caps
	// each species to this many per chunk in the persisted mob store and thins
	// overgrown cow coverage, preserving tamed mobs and villagers. Used once to
	// undo the pre-fix generation-herd doubling; leave 0 in normal operation.
	CullAnimals int

	// CleanupVillage, if non-empty ("x,z"), runs a ONE-TIME pass at boot that
	// removes a suppressed village's stranded mobs + crop/door debris edits near
	// that point (protecting a nearby castle). Set once, then clear.
	CleanupVillage string

	// WipeWild, if set, runs a ONE-TIME pass at boot that removes every
	// naturally-spawned mob (wild passives + hostiles) from the persisted store,
	// keeping only village-tied and tamed mobs, and marks every populated chunk
	// permanently seeded so the vanilla chunk-generation herds never re-lay.
	// Used once to undo runaway accumulation; leave false in normal operation.
	WipeWild bool

	// WorldFile, if set, persists block edits to that file so they survive
	// restarts (empty = in-memory only). Swap the store for a DB later.
	WorldFile string

	// Mixed survival/creative: DefaultGamemode is what new players get;
	// PlayerDataFile persists per-player modes; Ops may change game modes.
	DefaultGamemode int
	PlayerDataFile  string
	InventoryFile   string         // persists survival inventories (empty = in-memory only)
	AdvancementFile string         // persists advancement grants (empty = in-memory only)
	StatsFile       string         // persists statistics counters (empty = in-memory only)
	RecipeBookFile  string         // persists recipe-book unlocks/settings (empty = in-memory only)
	ScoreboardFile  string         // persists the scoreboard (empty = in-memory only)
	SignFile        string         // persists sign text (empty = in-memory only)
	BugFile         string         // persists in-game /bug reports (empty = in-memory only)
	CampfireFile    string         // persists campfire contents (empty = in-memory only)
	BannerFile      string         // persists placed-banner patterns (empty = in-memory only)
	BookFile        string         // persists book contents (empty = in-memory only)
	MapFile         string         // persists filled maps (empty = in-memory only)
	ContainerFile   string         // persists furnace/chest contents (empty = in-memory only)
	SpawnPointFile  string         // persists bed respawn points (empty = in-memory only)
	Access          *access.Client // tachyne-access admin API (nil = none): /op, /ban, /ban-ip, /banlist
	Ops             map[string]bool
	roleOps         sync.Map // names of players online now whose access roles include op

	// PluginDataDir is where compiled-in plugins keep per-plugin config +
	// data folders (default "plugins", cwd-relative like settings.json).
	PluginDataDir string

	// LLM-driven NPCs: OpenAI-compatible endpoint (e.g. LM Studio) + model.
	// Empty LLMAddr disables NPCs.
	LLMAddr  string
	LLMModel string

	// DisableHUD turns off the action-bar HUD (time/coords/etc.) for all players.
	DisableHUD bool

	// CullSpawnCows, if set, runs a ONE-TIME pass at boot that removes the
	// wild cows within 160 blocks of the origin from the persisted store — the
	// boot-seeded "herds" every restart used to add near spawn (removed
	// 2026-09-19). Tamed and named cows stay. Set once, then clear.
	CullSpawnCows bool
	// CullSpecies names species whose WILD members are removed from the saved
	// mobs at boot — one-time maintenance, set from -cull-species and taken
	// back out of the manifest once it has run.
	CullSpecies []string

	// Waves enables the NON-VANILLA cosmetic beach-wave overlay (a client-only
	// water sheet washing up the shore and rolling back). Off by default — it
	// deliberately departs from vanilla water behaviour, hence the opt-in.
	Waves bool

	// AttachAddr, if set, serves the tachyne domain attach protocol there
	// (gateway sessions); AttachToken is the shared secret gateways present.
	AttachAddr  string
	AttachToken string

	// HealthAddr, if set, serves /healthz + /debug/vars + /debug/pprof there
	// (health.go). Bind it to the pod, never to the ingress.
	HealthAddr string

	// Degraded-state flags surfaced on /debug/vars so "the cache never came
	// up" is a number on a dashboard, not a boot log line nobody re-reads.
	cacheBackend atomic.Value // string: valkey | dir | none
	busConnected atomic.Bool
	natsURL      string // set from NatsAddr once the hub exists; connectBusWithRetry consumes it

	// NatsAddr, if set, connects the plugin bus to a standalone NATS server
	// (e.g. "nats://localhost:4222"). OPTIONAL — the server runs fine without it.
	NatsAddr string

	// Generated-chunk cache: terrain is deterministic by (seed, GenVersion), so
	// generated chunks are cached persistently instead of re-derived from noise
	// on every restart/eviction. ValkeyAddr selects a Valkey/Redis backend
	// (host:port); otherwise ChunkCacheDir is a local directory ("" = off).
	// Both are pure caches — misses and backend failures just regenerate.
	ChunkCacheDir string
	ValkeyAddr    string

	// Sharding: when Sharded, this pod owns only Topo's region for SID; chunks
	// outside it are not served (finite world) and mutations there are rejected.
	// Unsharded (the default) owns the whole world — unchanged single-pod behavior.
	Sharded bool
	SID     int32
	Topo    shard.Map
	// DebugBorders draws a particle wall along region seams (dev cue). No-op
	// unless Sharded.
	DebugBorders bool
	// PeerAddr is the world↔world peer-link listener (e.g. ":25501"); PeerPattern
	// is the dial address for neighbour shards with a %d for the sid. Sharded only.
	PeerAddr    string
	PeerPattern string

	world  *world.World
	nether *world.World // the second dimension (same seed, nether generator)

	gate  *gatekeeper  // whitelist + bans (nil = wide open)
	end   *world.World // the third dimension (End island)
	hub   *hub
	modes *modeStore

	// commandTree is the tab-completion tree including plugin commands,
	// built once after the plugin enable phase (nil = static built-ins only).
	commandTree []byte
}

// commandTreeBytes picks the completion tree sessions send at join.
func (s *Server) commandTreeBytes() []byte {
	if s.commandTree != nil {
		return s.commandTree
	}
	return commandTreeBody
}

// isOp reports whether a player name may run privileged commands.
func (s *Server) isOp(name string) bool {
	if s.Ops[name] {
		return true
	}
	_, ok := s.roleOps.Load(name)
	return ok
}

// roleOp is the tachyne-access role that makes a player an operator.
const roleOp = "op"

// New returns a Server with sensible defaults.
func New() *Server {
	return &Server{
		MOTD:       "A Minecraft server written in Go",
		MaxPlayers: 100,
		Seed:       1,
	}
}

// Run starts the engine: worlds, hub, attach listener. There is no Minecraft
// socket — the protocol lives only in gateways (docs/DOMAIN-EVENTS.md).
func (s *Server) Run() error {
	log.Printf("attach-only engine: no Minecraft listener (gateways are the only way in)")
	return s.Serve()
}

// worldFor picks the world a player's dimension lives in.
func (s *Server) worldFor(p *player) *world.World {
	switch {
	case p.dim == 1 && s.nether != nil:
		return s.nether
	case p.dim == 2 && s.end != nil:
		return s.end
	}
	return s.world
}

func (s *Server) Serve() error {
	if s.world == nil {
		if s.WorldFile != "" {
			w, err := world.NewWithStore(s.Seed, world.NewFileStore(s.WorldFile))
			if err != nil {
				return err
			}
			s.world = w
			log.Printf("persisting world edits to %s (loaded %d block edits)", s.WorldFile, w.EditCount())
			go s.autosave()
		} else {
			s.world = world.New(s.Seed)
		}
		// Tall world: raise the overworld ceiling before anything generates
		// (and before the chunk cache attaches — the height-tagged cache keys
		// must be in force from the first chunk).
		if s.Ceiling > 0 {
			s.world.SetCeiling(s.Ceiling)
			log.Printf("tall world: overworld ceiling y=%d (%d sections)", s.world.Ceiling(), s.world.Sections())
		}
		// Earth mode: real-DEM terrain for the OVERWORLD (nether/end stay
		// procedural). Set before the chunk cache so the earth-tagged cache
		// keys are in force from the first generated chunk.
		if s.EarthName != "" {
			dem, err := s.world.SetEarth(s.EarthName, s.EarthVScale)
			if err != nil {
				return err
			}
			lat, lon := dem.BlockToLatLon(0, 0)
			log.Printf("earth mode: %s (block 0,0 = %.4f,%.4f; 1 block = 1 m, vertical 1:%g)",
				s.EarthName, lat, lon, s.EarthVScale)
		}
		if cache := s.openChunkCache(); cache != nil {
			s.world.SetChunkCache(cache)
		}
	}
	if s.nether == nil {
		var store world.Store
		if s.WorldFile != "" {
			dir := filepath.Dir(s.WorldFile)
			store = world.NewFileStore(filepath.Join(dir, "nether.gob"))
		}
		n, err := world.NewNether(s.Seed, store)
		if err != nil {
			return err
		}
		s.nether = n
		if cache := s.openChunkCache(); cache != nil {
			s.nether.SetChunkCache(cache)
		}
	}
	if s.end == nil {
		var store world.Store
		if s.WorldFile != "" {
			store = world.NewFileStore(filepath.Join(filepath.Dir(s.WorldFile), "end.gob"))
		}
		e, err := world.NewEnd(s.Seed, store)
		if err != nil {
			return err
		}
		s.end = e
		if cache := s.openChunkCache(); cache != nil {
			s.end.SetChunkCache(cache)
		}
	}
	// One-time block-state id-space migration. Edits saved before the 1.21.11
	// canonical bump hold 1.21.5 (proto 770) block-state ids; the engine now
	// speaks 1.21.11, so remap every saved edit once (guarded by a marker file).
	if s.WorldFile != "" {
		if err := s.migrateEditsIDSpace(); err != nil {
			log.Printf("WARNING: world id-space migration failed: %v", err)
		}
		if err := s.migrateContentIDSpace(); err != nil {
			log.Fatalf("content id-space migration failed, nothing written: %v", err)
		}
		s.repairPlacedLeaves() // hedges saved before placed leaves were persistent
		// Generation's build guard reads the builds as they stood when this
		// GenVersion first booted, not as they are now (world/guardsnap.go).
		dir := filepath.Dir(s.WorldFile)
		for name, w := range map[string]*world.World{"world": s.world, "nether": s.nether, "end": s.end} {
			if err := w.FreezeGuard(dir, name); err != nil {
				log.Fatalf("build guard snapshot for %s: %v", name, err)
			}
		}
	}
	s.resolveSpawn() // fix an auto (x,z) spawn Y before anything reads it
	if s.gate == nil {
		s.gate = newGatekeeper("gatekeeper.json")
	}
	if s.PlayerDataFile != "" {
		// Before any player store loads: they key players by UUID, and
		// convert name-keyed files through this (playerkeys.go).
		ids = loadPlayerIDs(filepath.Dir(s.PlayerDataFile))
	}
	if s.modes == nil {
		s.modes = newModeStore(s.PlayerDataFile, s.DefaultGamemode)
	}
	if s.hub == nil {
		s.hub = newHub(s.world)
		s.hub.nether = s.nether
		s.hub.end = s.end
		if s.Sharded {
			sid, topo := s.SID, s.Topo
			s.hub.sid = sid
			s.hub.topo = topo
			s.hub.shardOf = func(cx, cz int32) int32 { return topo.ShardOf(0, cx, cz) }
			s.hub.debugBorders = s.DebugBorders
		}
		// World spawn for death respawns: use the configured spawn only when this
		// shard owns that column (so an east-shard death doesn't respawn you in the
		// west); otherwise worldSpawn() falls back to this shard's own region.
		if s.SpawnSet && s.hub.ownedAt(s.SpawnX, s.SpawnZ) {
			s.hub.worldSpawnX, s.hub.worldSpawnY, s.hub.worldSpawnZ = s.SpawnX, s.SpawnY, s.SpawnZ
			s.hub.hasWorldSpawn = true
		}
		if s.DisableHUD {
			s.hub.hud = nil
		}
		s.hub.waves = s.Waves
		s.hub.invs = newInvStore(s.InventoryFile)
		s.hub.advs = newAdvStore(s.AdvancementFile)
		s.hub.statstore = newStatsStore(s.StatsFile)
		s.hub.rbstore = newRecipeBookStore(s.RecipeBookFile)
		s.hub.sb, s.hub.sbstore = newScoreboard(s.ScoreboardFile)
		s.hub.signs = newSignStore(s.SignFile)
	}
	if s.BugFile != "" {
		s.hub.bugs = newBugStore(s.BugFile)
		s.hub.cfStore = newCampfireStore(s.CampfireFile)
		s.hub.banners = newBannerStore(s.BannerFile)
		s.hub.books = newBookStore(s.BookFile)
		globalBooks.Store(s.hub.books)
		s.hub.maps = newMapStore(s.MapFile)
		s.hub.containers = newContainerStore(s.ContainerFile)
		s.hub.mobstore = newMobStore(s.MobFile)
		if s.WipeWild {
			before, after := s.hub.mobstore.wipeWild()
			s.hub.mobstore.flush()
			log.Printf("wipe-wild: removed all wild mobs (kept village-tied + tamed), persisted mobs %d -> %d",
				before, after)
		}
		if len(s.CullSpecies) > 0 {
			set := map[int]bool{}
			var names []string
			for _, n := range s.CullSpecies {
				if id, ok := entityByName[n]; ok {
					set[id], names = true, append(names, n)
				} else {
					log.Printf("cull-species: no such entity %q — skipped", n)
				}
			}
			if len(set) > 0 {
				before, after, removed := s.hub.mobstore.cullSpecies(set)
				s.hub.mobstore.flush()
				log.Printf("cull-species %v: removed %d wild (kept tamed/named/persistent), persisted mobs %d -> %d",
					names, removed, before, after)
			}
		}
		if s.CullSpawnCows {
			before, after := s.hub.mobstore.cullSpawnCows(entityCow, 160)
			s.hub.mobstore.flush()
			log.Printf("cull-spawn-cows: removed the wild cows within 160 of the origin, persisted mobs %d -> %d", before, after)
		}
		if s.CullAnimals > 0 {
			before, after := s.hub.mobstore.cullAnimals(s.CullAnimals, 5)
			s.hub.mobstore.flush()
			log.Printf("cull-animals: capped to %d/chunk + cow-thinned, persisted mobs %d -> %d",
				s.CullAnimals, before, after)
		}
		s.cullEndermenOnce() // the copies bug #45 left in the store
		// Load the persisted seeded-chunk set so the vanilla chunk-generation herds
		// fire once per chunk EVER (not once per restart) — the accumulation fix.
		s.hub.seededChunks = s.hub.mobstore.seededSet()
		if s.CleanupVillage != "" {
			var cvx, cvz int
			if _, err := fmt.Sscanf(s.CleanupVillage, "%d,%d", &cvx, &cvz); err == nil {
				s.cleanupSpawnVillage(cvx, cvz)
			} else {
				log.Printf("cleanup-village: bad coords %q (want x,z)", s.CleanupVillage)
			}
		}
		for _, w := range s.hub.mobstore.villages() {
			s.hub.villageDone[unpackPos(w)] = true // populated villages stay populated
		}
		if s.WorldFile != "" {
			s.clearLoneDoors() // villagers' door swings a new village layout left standing (#48)
		}
		for w, keys := range s.hub.mobstore.villagePlaced() {
			s.hub.villagePlaced[w] = keys // …and each template entity is placed once
		}
		for _, mn := range s.hub.mobstore.mansions() {
			s.hub.mansionDone[[2]int32{int32(mn[0]), int32(mn[1])}] = true // cleared mansions stay cleared
		}
		for _, b := range s.hub.mobstore.bastions() {
			s.hub.bastionDone[[2]int32{int32(b[0]), int32(b[1])}] = true // cleared bastions stay cleared
		}
		for _, hh := range s.hub.mobstore.huts() {
			s.hub.hutDone[[2]int32{int32(hh[0]), int32(hh[1])}] = true // a cleared hut stays cleared
		}
		for _, c := range s.hub.mobstore.endCities() {
			s.hub.endCityDone[[2]int32{int32(c[0]), int32(c[1])}] = true // looted cities stay looted
		}
		for _, r := range s.hub.mobstore.oceanRuins() {
			s.hub.oceanRuinDone[[2]int32{int32(r[0]), int32(r[1])}] = true // cleared ruins stay cleared
		}
		s.hub.restoreRaids(s.hub.mobstore.raids()) // raids in progress pick up mid-wave
		// One-time ITEM id-space migration for persisted inventories + containers
		// (before the hub loads them in run()), mirroring the block-edit migration.
		s.hub.spawns = newSpawnStore(s.SpawnPointFile)
		s.hub.hivestore = newHiveStore(hivesPathFor(s.SpawnPointFile))
		s.hub.postFX = newPostEffectStore(postEffectsPathFor(s.SpawnPointFile))
		s.hub.hivesLoad()
		s.hub.rulesPath = "settings.json"
		s.hub.isOp = s.isOp // announce targeting: the -ops list and the op role
		s.hub.loadRules()
		if s.restoreWorldSpawn() { // a /setworldspawn outranks -spawn
			log.Printf("world spawn from settings: (%.1f, %.0f, %.1f)", s.SpawnX, s.SpawnY, s.SpawnZ)
		}
		if gm := s.hub.rules.DefaultGamemode; gm != nil { // a /defaultgamemode outranks -gamemode
			s.DefaultGamemode = *gm
			s.modes.setDefault(*gm)
		}
		s.hub.restoreForced()
		// Rebuild the lightning-rod POI set from the persisted edits, so rods
		// placed before a restart keep attracting storms.
		s.hub.world.ForEachEdit(func(x, y, z int, state uint32) {
			if isLightningRodState(state) {
				s.hub.rods[blockPos{x, y, z}] = struct{}{}
			}
			if state == beaconState { // beacons rebuild from edits; powers re-attach from containers
				s.hub.beacons[simPos{blockPos: blockPos{x, y, z}}] = &beacon{}
			}
			s.hub.sculkIndexOnBlockChange(dimOverworld, x, y, z, state) // sculk listener/catalyst POI sets
			s.hub.heartIndexOnBlockChange(dimOverworld, x, y, z, state) // built creaking hearts
		})
		// Sculk and hearts built in the Nether and the End register from those
		// worlds' own edits, so they keep working across a restart.
		for _, dim := range []int{dimNether, dimEnd} {
			if w := s.hub.worldFor(dim); w != s.hub.world {
				w.ForEachEdit(func(x, y, z int, state uint32) {
					s.hub.sculkIndexOnBlockChange(dim, x, y, z, state)
					s.hub.heartIndexOnBlockChange(dim, x, y, z, state)
				})
			}
		}
		if s.LLMAddr != "" {
			s.hub.llm = newLLMClient(s.LLMAddr, s.LLMModel)
			log.Printf("LLM NPCs enabled: %s (model %q)", s.LLMAddr, s.LLMModel)
		}
		// Optional NATS plugin bus (set before run() reads h.bus). A misconfigured
		// or down broker must NOT stop the game — we log and run without it.
		if s.NatsAddr != "" {
			// A broker that is down at boot must not stop the game — and must
			// not stay disconnected forever either (connectBusWithRetry).
			// v2: registerBusBridge mirrors the plugin event catalog onto
			// mc.event.v2.* only once a real bus exists, so hot sites like
			// PlayerMove stay cold otherwise.
			s.natsURL = s.NatsAddr
		}
		// World↔world peer mesh: warm links to neighbour shards for handover +
		// shadow (sharded only). Established at boot so a seam crossing is instant.
		if s.Sharded && s.PeerAddr != "" {
			pln, err := net.Listen("tcp", s.PeerAddr)
			if err != nil {
				return fmt.Errorf("peer listener: %w", err)
			}
			defer pln.Close()
			pattern := s.PeerPattern
			mesh := newPeerMesh(s.SID, s.Topo.TopoHash(), s.AttachToken,
				func(sid int32) string { return fmt.Sprintf(pattern, sid) }, s.hub.onPeerFrame)
			s.hub.peers = mesh
			nb := s.Topo.NeighboursWithin(s.SID, awarenessRadiusChunks)
			go mesh.serve(pln)
			mesh.dial(nb)
			log.Printf("peer mesh: sid=%d listening %s, neighbours=%v", s.SID, s.PeerAddr, nb)
		}
		// Enable compiled-in plugins (registered via blank imports in
		// cmd/server) before the tick loop starts, so Enable-time listener
		// and command registration never races a live hub.
		if err := s.enablePlugins(); err != nil {
			return err
		}
		go s.hub.run()
		if s.natsURL != "" {
			s.connectBusWithRetry(s.natsURL)
		}
		if s.HealthAddr != "" {
			go s.serveHealth(s.HealthAddr)
		}
	}
	// Domain attach listener for tachyne gateways ("worlds are versionless":
	// gateways speak Minecraft, this side speaks raw world state).
	if s.AttachAddr != "" {
		aln, err := net.Listen("tcp", s.AttachAddr)
		if err != nil {
			return fmt.Errorf("attach listener: %w", err)
		}
		defer aln.Close()
		spawn := attach.Config{
			World: s.world,
			Time:  func() int64 { return int64(s.hub.dayTime.Load()) },
			LoginFlags: func() (bool, bool, bool) {
				f := s.hub.loginFlags.Load()
				return f&loginNoRespawnScreen != 0, f&loginLimitedCrafting != 0, f&loginReducedDebug != 0
			},
			Token:  s.AttachToken,
			Spawn:  attachproto.Pos{X: 0.5, Y: s.world.SurfaceY(0, 0), Z: 0.5},
			Join:   s.JoinRemote,
			Resume: s.ResumeRemote,
			Status: s.statusRoster,
			Owned:  func(dim, cx, cz int32) bool { return s.hub.serveChunk(cx, cz) }, // stream neighbour border chunks too (seamless overlap)
			ChunkGate: func(r attach.Remote) func(dim, cx, cz int32) bool {
				if rp, ok := r.(*remotePlayer); ok && rp.p.loadedOnly.Load() {
					return s.hub.heldLoaded
				}
				return nil
			},
			BlockEntities: func(w *world.World, cx, cz int32) []byte {
				dim := 0
				switch w {
				case s.nether:
					dim = 1
				case s.end:
					dim = 2
				}
				return appendBlockEntities(nil, w, cx, cz, dim, s.hub.signs, s.hub.cfStore, s.hub.banners, s.hub.shelfView, s.hub.potSherds)
			},
			Worlds: func(dim int32) *world.World {
				switch dim {
				case 1:
					return s.nether
				case 2:
					return s.end
				}
				return s.world
			},
		}
		if s.SpawnSet { // SpawnY already resolved (surface) for the auto x,z form
			spawn.Spawn = attachproto.Pos{X: s.SpawnX, Y: s.SpawnY, Z: s.SpawnZ}
		}
		if s.AttachToken == "" {
			log.Print("WARNING: -attach set but ATTACH_TOKEN empty — all attach sessions will be refused")
		}
		log.Printf("attach listener on %s", s.AttachAddr)
		go attach.Serve(aln, spawn)
	}
	select {} // the attach listener and hub carry the process
}

// resolveSpawn turns an auto (x,z) spawn into a concrete standing Y at the column
// surface, once, before anything reads it. BOTH the attach Welcome and
// JoinRemote's per-join position read s.SpawnY, so resolving here keeps them in
// lockstep — an unresolved auto Y is the leftover from the failed 3-field parse
// (e.g. -103,-31 → Y=-31, underground: "spawn into rock").
func (s *Server) resolveSpawn() {
	if !s.SpawnSet || !s.SpawnAuto || s.world == nil {
		return
	}
	s.SpawnY = s.world.SurfaceY(int(s.SpawnX), int(s.SpawnZ))
	s.SpawnAuto = false
	log.Printf("spawn resolved to surface: (%.0f, %.0f, %.0f)", s.SpawnX, s.SpawnY, s.SpawnZ)
}

// openChunkCache picks the generated-chunk cache backend: Valkey when
// configured (falling back to the directory if unreachable), else the local
// directory, else none.
func (s *Server) openChunkCache() world.ChunkCache {
	var dir world.ChunkCache
	if s.ChunkCacheDir != "" {
		if dir = world.NewDirCache(s.ChunkCacheDir); dir != nil {
			log.Printf("chunk cache: directory %s", s.ChunkCacheDir)
			s.cacheBackend.Store("dir")
		}
	}
	if s.ValkeyAddr == "" {
		if dir == nil {
			s.cacheBackend.Store("none")
		}
		return dir
	}
	// Valkey in front of the directory. A dial that fails at boot (the
	// cluster's DNS is not always answering in a pod's first seconds) is
	// retried in the background rather than abandoned for the life of the
	// process — the previous behaviour, which once left the pod on the
	// directory fallback for a day with nothing but a boot log line to say so.
	addr := s.ValkeyAddr
	return world.NewReconnecting("valkey "+addr,
		func() (world.ChunkCache, error) { return world.NewValkeyCache(addr) },
		dir, chunkCacheRetry,
		func() { s.cacheBackend.Store("valkey") })
}

// chunkCacheRetry is how often an unreachable shared chunk cache is re-dialed.
const chunkCacheRetry = 30 * time.Second

// connectBusWithRetry attaches the NATS plugin bus, retrying in the background
// when the broker is unreachable at boot. The swap happens ON the hub goroutine
// (an evRunOnHub barrier) because h.bus is hub-owned state and registering the
// event bridge touches hub dispatch tables.
func (s *Server) connectBusWithRetry(url string) {
	attach := func() bool {
		nb, err := newNatsBus(s.hub, url)
		if err != nil {
			return false
		}
		s.hub.runOnHub(func() {
			s.hub.bus = nb
			s.hub.registerBusBridge()
		})
		s.busConnected.Store(true)
		return true
	}
	if attach() {
		return
	}
	log.Printf("NATS bus unavailable at %s — server continues without it and retries every %s", url, busRetry)
	go func() {
		failures := 1
		for {
			time.Sleep(busRetry)
			if attach() {
				log.Printf("NATS bus connected after %d failed attempt(s)", failures)
				return
			}
			failures++
			if failures%20 == 0 {
				log.Printf("NATS bus still unavailable after %d attempts", failures)
			}
		}
	}()
}

const busRetry = 30 * time.Second

// autosave periodically flushes world edits to disk (a no-op when unchanged).
func (s *Server) autosave() {
	t := time.NewTicker(autosaveInterval)
	defer t.Stop()
	last := s.world.EditCount()
	lastNether := 0
	for range t.C {
		if s.hub != nil && s.hub.saveOff.Load() {
			continue // /save-off
		}
		if err := s.world.Save(); err != nil {
			log.Printf("world autosave failed: %v", err)
			continue
		}
		if s.nether != nil {
			if err := s.nether.Save(); err != nil {
				log.Printf("nether autosave failed: %v", err)
			} else if n := s.nether.EditCount(); n != lastNether {
				log.Printf("nether autosaved (%d block edits)", n)
				lastNether = n
			}
		}
		if s.end != nil {
			if err := s.end.Save(); err != nil {
				log.Printf("end autosave failed: %v", err)
			}
		}
		if n := s.world.EditCount(); n != last { // only chatter when something changed
			log.Printf("world autosaved (%d block edits)", n)
			last = n
		}
	}
}

// Save flushes world edits + hub-owned state (inventories, containers) to disk
// now (used on graceful shutdown). The hub snapshot goes through an event so
// the write can't race the tick loop; a short timeout keeps shutdown prompt if
// the hub is wedged.
func (s *Server) Save() error {
	if s.hub != nil && s.hub.plugHost != nil {
		// Plugin Disable hooks run first (on the hub goroutine), so anything
		// they write in their stores rides the same shutdown flush.
		done := make(chan struct{})
		s.hub.post(evDisablePlugins{done: done})
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			log.Printf("plugin disable timed out; continuing shutdown")
		}
	}
	if s.hub != nil {
		done := make(chan struct{})
		s.hub.post(evSaveState{done: done})
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			log.Printf("hub state save timed out; continuing shutdown")
		}
	}
	if s.world == nil {
		return nil
	}
	if s.nether != nil {
		s.nether.Save()
	}
	if s.end != nil {
		s.end.Save()
	}
	return s.world.Save()
}
