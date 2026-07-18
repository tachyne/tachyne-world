// Package server accepts client connections and drives them through the
// Minecraft connection-state machine. Milestone 1 implements Handshake +
// Status (the server-list ping); Login and Play come later.
package server

import (
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
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
)

// idSpaceVersion marks which block-state id space the persisted edits use. Bump
// it (and migrateEditsIDSpace remaps) whenever the canonical version changes.
const idSpaceVersion = "1.21.11"

// migrateEditsIDSpace remaps every persisted block edit from the previous
// canonical id space (1.21.5 / proto 770) to the current one, exactly once. The
// marker file records the id space already applied, so subsequent restarts skip.
func (s *Server) migrateEditsIDSpace() error {
	marker := filepath.Join(filepath.Dir(s.WorldFile), ".idspace")
	if b, _ := os.ReadFile(marker); strings.TrimSpace(string(b)) == idSpaceVersion {
		return nil
	}
	// UnmapID(RegBlockState, 770, id) maps a 1.21.5 client id back to the 1.21.11
	// canonical id (reverseTables invert the 1.21.11→1.21.5 forward table).
	remap := func(state uint32) uint32 {
		return uint32(protocol.UnmapID(protocol.RegBlockState, 770, int32(state)))
	}
	total := 0
	for _, w := range []*world.World{s.world, s.nether, s.end} {
		if w == nil {
			continue
		}
		n, err := w.MigrateEdits(remap)
		if err != nil {
			return err
		}
		total += n
	}
	log.Printf("id-space migration: remapped %d block edits 1.21.5→%s", total, idSpaceVersion)
	return os.WriteFile(marker, []byte(idSpaceVersion+"\n"), 0o644)
}

// migrateItemIDSpace remaps persisted ITEM ids (survival inventories + world
// containers) from 1.21.5 to the current canonical version, exactly once. Guarded
// by its own marker, independent of the block-edit migration above.
func (s *Server) migrateItemIDSpace() error {
	marker := filepath.Join(filepath.Dir(s.WorldFile), ".itemspace")
	if b, _ := os.ReadFile(marker); strings.TrimSpace(string(b)) == idSpaceVersion {
		return nil
	}
	remap := func(id int32) int32 { return protocol.UnmapID(protocol.RegItem, 770, id) }
	n := 0
	if s.hub.invs != nil {
		n += s.hub.invs.migrateItemIDs(remap)
	}
	if s.hub.containers != nil {
		n += s.hub.containers.migrateItemIDs(remap)
	}
	if n > 0 {
		if s.hub.invs != nil {
			s.hub.invs.flush()
		}
		if s.hub.containers != nil {
			s.hub.containers.flush()
		}
	}
	log.Printf("item id-space migration: remapped %d item ids 1.21.5→%s", n, idSpaceVersion)
	return os.WriteFile(marker, []byte(idSpaceVersion+"\n"), 0o644)
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

	// WorldFile, if set, persists block edits to that file so they survive
	// restarts (empty = in-memory only). Swap the store for a DB later.
	WorldFile string

	// Mixed survival/creative: DefaultGamemode is what new players get;
	// PlayerDataFile persists per-player modes; Ops may change game modes.
	DefaultGamemode int
	PlayerDataFile  string
	InventoryFile   string // persists survival inventories (empty = in-memory only)
	AdvancementFile string // persists advancement grants (empty = in-memory only)
	StatsFile       string // persists statistics counters (empty = in-memory only)
	RecipeBookFile  string // persists recipe-book unlocks/settings (empty = in-memory only)
	ScoreboardFile  string // persists the scoreboard (empty = in-memory only)
	SignFile        string // persists sign text (empty = in-memory only)
	CampfireFile    string // persists campfire contents (empty = in-memory only)
	BannerFile      string // persists placed-banner patterns (empty = in-memory only)
	BookFile        string // persists book contents (empty = in-memory only)
	MapFile         string // persists filled maps (empty = in-memory only)
	ContainerFile   string // persists furnace/chest contents (empty = in-memory only)
	SpawnPointFile  string // persists bed respawn points (empty = in-memory only)
	Ops             map[string]bool

	// PluginDataDir is where compiled-in plugins keep per-plugin config +
	// data folders (default "plugins", cwd-relative like settings.json).
	PluginDataDir string

	// LLM-driven NPCs: OpenAI-compatible endpoint (e.g. LM Studio) + model.
	// Empty LLMAddr disables NPCs.
	LLMAddr  string
	LLMModel string

	// DisableHUD turns off the action-bar HUD (time/coords/etc.) for all players.
	DisableHUD bool

	// VanillaSpawner selects the exact-vanilla NaturalSpawner (per-chunk rate +
	// chunk-generation herds) instead of the default tachyne sampler.
	VanillaSpawner bool

	// AttachAddr, if set, serves the tachyne domain attach protocol there
	// (gateway sessions); AttachToken is the shared secret gateways present.
	AttachAddr  string
	AttachToken string

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
func (s *Server) isOp(name string) bool { return s.Ops[name] }

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
	}
	s.resolveSpawn() // fix an auto (x,z) spawn Y before anything reads it
	if s.gate == nil {
		s.gate = newGatekeeper("gatekeeper.json")
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
		s.hub.vanillaSpawner = s.VanillaSpawner
		s.hub.invs = newInvStore(s.InventoryFile)
		s.hub.advs = newAdvStore(s.AdvancementFile)
		s.hub.statstore = newStatsStore(s.StatsFile)
		s.hub.rbstore = newRecipeBookStore(s.RecipeBookFile)
		s.hub.sb, s.hub.sbstore = newScoreboard(s.ScoreboardFile)
		s.hub.signs = newSignStore(s.SignFile)
		s.hub.cfStore = newCampfireStore(s.CampfireFile)
		s.hub.banners = newBannerStore(s.BannerFile)
		s.hub.books = newBookStore(s.BookFile)
		globalBooks.Store(s.hub.books)
		s.hub.maps = newMapStore(s.MapFile)
		s.hub.containers = newContainerStore(s.ContainerFile)
		s.hub.mobstore = newMobStore(s.MobFile)
		if s.CullAnimals > 0 {
			before, after := s.hub.mobstore.cullAnimals(s.CullAnimals, 5)
			s.hub.mobstore.flush()
			log.Printf("cull-animals: capped to %d/chunk + cow-thinned, persisted mobs %d -> %d",
				s.CullAnimals, before, after)
		}
		for _, w := range s.hub.mobstore.villages() {
			s.hub.villageDone[unpackPos(w)] = true // populated villages stay populated
		}
		// One-time ITEM id-space migration for persisted inventories + containers
		// (before the hub loads them in run()), mirroring the block-edit migration.
		if s.WorldFile != "" {
			if err := s.migrateItemIDSpace(); err != nil {
				log.Printf("WARNING: item id-space migration failed: %v", err)
			}
		}
		s.hub.spawns = newSpawnStore(s.SpawnPointFile)
		s.hub.rulesPath = "settings.json"
		s.hub.opsRef = s.Ops // read-only after this point (announce targeting)
		s.hub.loadRules()
		// Rebuild the lightning-rod POI set from the persisted edits, so rods
		// placed before a restart keep attracting storms.
		s.hub.world.ForEachEdit(func(x, y, z int, state uint32) {
			if isLightningRodState(state) {
				s.hub.rods[blockPos{x, y, z}] = struct{}{}
			}
			if state == beaconState { // beacons rebuild from edits; powers re-attach from containers
				s.hub.beacons[blockPos{x, y, z}] = &beacon{}
			}
			s.hub.sculkIndexOnBlockChange(x, y, z, state) // sculk listener/catalyst POI sets
		})
		if s.LLMAddr != "" {
			s.hub.llm = newLLMClient(s.LLMAddr, s.LLMModel)
			log.Printf("LLM NPCs enabled: %s (model %q)", s.LLMAddr, s.LLMModel)
		}
		// Optional NATS plugin bus (set before run() reads h.bus). A misconfigured
		// or down broker must NOT stop the game — we log and run without it.
		if s.NatsAddr != "" {
			if nb, err := newNatsBus(s.hub, s.NatsAddr); err != nil {
				log.Printf("NATS bus disabled: %v (server continues without it)", err)
			} else {
				s.hub.bus = nb
				// v2: mirror the plugin event catalog onto mc.event.v2.*
				// (busv2.go). Registered only when a real bus exists, so
				// hot sites like PlayerMove stay cold otherwise.
				s.hub.registerBusBridge()
			}
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
			World:  s.world,
			Time:   func() int64 { return int64(s.hub.dayTime.Load()) },
			Token:  s.AttachToken,
			Spawn:  attachproto.Pos{X: 0.5, Y: s.world.SurfaceY(0, 0), Z: 0.5},
			Join:   s.JoinRemote,
			Resume: s.ResumeRemote,
			Owned:  func(dim, cx, cz int32) bool { return s.hub.serveChunk(cx, cz) }, // stream neighbour border chunks too (seamless overlap)
			BlockEntities: func(w *world.World, cx, cz int32) []byte {
				dim := 0
				switch w {
				case s.nether:
					dim = 1
				case s.end:
					dim = 2
				}
				return appendBlockEntities(nil, w, cx, cz, dim, s.hub.signs, s.hub.cfStore, s.hub.banners)
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
	if s.ValkeyAddr != "" {
		if c, err := world.NewValkeyCache(s.ValkeyAddr); err == nil {
			log.Printf("chunk cache: valkey at %s", s.ValkeyAddr)
			return c
		} else {
			log.Printf("chunk cache: valkey at %s unavailable (%v) — falling back", s.ValkeyAddr, err)
		}
	}
	if s.ChunkCacheDir != "" {
		if c := world.NewDirCache(s.ChunkCacheDir); c != nil {
			log.Printf("chunk cache: directory %s", s.ChunkCacheDir)
			return c
		}
	}
	return nil
}

// autosave periodically flushes world edits to disk (a no-op when unchanged).
func (s *Server) autosave() {
	t := time.NewTicker(autosaveInterval)
	defer t.Stop()
	last := s.world.EditCount()
	lastNether := 0
	for range t.C {
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
