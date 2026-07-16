// Package servercmd is the tachyne world-engine entrypoint as a library:
// a custom server binary (e.g. one assembled by cmd/tachyne-build with
// third-party plugins compiled in) blank-imports its plugin packages and
// calls Main(). The stock cmd/server binary is exactly that plus the
// in-repo example plugin.
package servercmd

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/tachyne/tachyne-world/internal/server"
)

// Main parses flags and runs the engine; it never returns.
func Main() {
	addr := flag.String("addr", "", "DEPRECATED no-op: the engine has no Minecraft socket (protocol lives in gateways)")
	seed := flag.Int64("seed", 1, "world seed")
	spawn := flag.String("spawn", "", "spawn override as x,z (Y resolved to the surface) or x,y,z (literal Y); default: surface at 0,0")
	worldFile := flag.String("world", "world.gob", "file to persist block edits to (empty = in-memory only)")
	gamemode := flag.String("gamemode", "survival", "default game mode for new players (survival|creative|adventure|spectator)")
	ops := flag.String("ops", "", "comma-separated player names allowed to change game modes")
	hud := flag.Bool("hud", true, "show the action-bar HUD (time/coords/facing/online)")
	natsURL := flag.String("nats", "", "OPTIONAL standalone NATS server URL for the plugin bus, e.g. nats://localhost:4222 (empty = off)")
	chunkDir := flag.String("chunkdir", "chunks", "directory for the generated-chunk cache (empty = off)")
	valkey := flag.String("valkey", "", "OPTIONAL Valkey/Redis address for the chunk cache, e.g. localhost:6379 (falls back to -chunkdir)")
	llmURL := flag.String("llm", "", "OPTIONAL OpenAI-compatible endpoint for LLM NPCs, e.g. http://localhost:1234/v1 (empty = NPCs off)")
	llmModel := flag.String("llm-model", "", "model name for the LLM NPC endpoint (e.g. gpt-oss-20b)")
	attachAddr := flag.String("attach", ":25500", "tachyne gateway attach listener (token from ATTACH_TOKEN env)")
	sid := flag.Int("sid", -1, "shard id for a sharded world (-1 = derive from POD_NAME ordinal)")
	topology := flag.String("topology", "", "path to shard topology JSON (empty = unsharded single pod)")
	debugBorders := flag.Bool("debug-borders", false, "dev: draw a particle wall along shard region seams")
	peerAddr := flag.String("peer-addr", "", "world↔world peer-link listener for a sharded world, e.g. :25501")
	peerPattern := flag.String("peer-pattern", "tachyne-world-%d.tachyne-world.tachyne.svc:25501", "dial address for neighbour shards (%d = sid)")
	earth := flag.String("earth", "", "EARTH MODE: overworld terrain from an embedded real elevation model, e.g. capetown (empty = procedural noise)")
	earthVScale := flag.Float64("earth-vscale", 4.5, "earth mode: metres of real elevation per block above sea level")
	ceiling := flag.Int("ceiling", 0, "TALL WORLD: overworld top build limit (0 = vanilla 320; Java max 2032). Pair with -earth-vscale so the region's summits fit, e.g. -ceiling 1664 -earth-vscale 1")
	pluginDir := flag.String("plugindir", "plugins", "directory for per-plugin config + data folders")
	spawner := flag.String("spawner", "tachyne", "natural-spawn model: tachyne (cheaper 1/8 sampler + herd top-up) or vanilla (exact NaturalSpawner: per-chunk rate + chunk-generation herds)")
	flag.Parse()

	if *addr != "" {
		log.Printf("WARNING: -addr is a no-op — the engine has no Minecraft socket; connect through a gateway")
	}
	srv := server.New()
	srv.Seed = *seed
	srv.EarthName = *earth
	srv.EarthVScale = *earthVScale
	srv.Ceiling = *ceiling
	srv.AttachAddr = *attachAddr
	srv.AttachToken = os.Getenv("ATTACH_TOKEN")
	srv.WorldFile = *worldFile
	srv.DisableHUD = !*hud
	switch *spawner {
	case "tachyne":
		srv.VanillaSpawner = false
	case "vanilla":
		srv.VanillaSpawner = true
	default:
		log.Fatalf("invalid -spawner %q (want tachyne or vanilla)", *spawner)
	}
	srv.NatsAddr = *natsURL
	srv.ChunkCacheDir = *chunkDir
	srv.ValkeyAddr = *valkey
	srv.LLMAddr = *llmURL
	srv.LLMModel = *llmModel
	srv.PlayerDataFile = "players.json"
	srv.InventoryFile = "inventories.json"
	srv.AdvancementFile = "advancements.json"
	srv.StatsFile = "stats.json"
	srv.RecipeBookFile = "recipebook.json"
	srv.ScoreboardFile = "scoreboard.json"
	srv.SignFile = "signs.json"
	srv.CampfireFile = "campfires.json"
	srv.BannerFile = "banners.json"
	srv.BookFile = "books.json"
	srv.MapFile = "maps.json"
	srv.ContainerFile = "containers.json"
	srv.MobFile = "mobs.json"
	srv.SpawnPointFile = "spawns.json"
	srv.PluginDataDir = *pluginDir
	if m, ok := server.ParseGamemode(*gamemode); ok {
		srv.DefaultGamemode = m
	} else {
		log.Fatalf("invalid -gamemode %q", *gamemode)
	}
	srv.Ops = map[string]bool{}
	for _, name := range strings.Split(*ops, ",") {
		if name = strings.TrimSpace(name); name != "" {
			srv.Ops[name] = true
		}
	}
	if *spawn != "" {
		if n, _ := fmt.Sscanf(*spawn, "%f,%f,%f", &srv.SpawnX, &srv.SpawnY, &srv.SpawnZ); n == 3 {
			srv.SpawnSet = true
		} else if n, _ := fmt.Sscanf(*spawn, "%f,%f", &srv.SpawnX, &srv.SpawnZ); n == 2 {
			srv.SpawnSet, srv.SpawnAuto = true, true // resolve Y to the surface of (x,z) — never spawn in mid-air
		} else {
			log.Fatalf("invalid -spawn %q (want x,z or x,y,z)", *spawn)
		}
	}
	if *topology != "" {
		m, err := server.LoadTopology(*topology)
		if err != nil {
			log.Fatalf("topology: %v", err)
		}
		srv.Sharded = true
		srv.Topo = m
		srv.SID = resolveSID(*sid)
		srv.DebugBorders = *debugBorders
		srv.PeerAddr = *peerAddr
		srv.PeerPattern = *peerPattern
		log.Printf("sharded world: sid=%d regions=%d topo=%s", srv.SID, len(m.Regions), m.TopoHash()[:12])
	}

	// On SIGINT/SIGTERM, flush the world one last time before exiting so a clean
	// stop never loses recent building. (SIGKILL can't be caught — the periodic
	// autosave bounds the loss there.)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Printf("received %s, saving world…", sig)
		if err := srv.Save(); err != nil {
			log.Printf("final save failed: %v", err)
		}
		os.Exit(0)
	}()

	log.Fatal(srv.Run())
}

// resolveSID returns the shard id from -sid, or parses the trailing ordinal of
// POD_NAME (a StatefulSet pod like "shardtest-world-2" → 2). A wrong SID
// corrupts the world, so a sharded pod with neither is a fatal misconfiguration.
func resolveSID(flagSID int) int32 {
	if flagSID >= 0 {
		return int32(flagSID)
	}
	pod := os.Getenv("POD_NAME")
	if i := strings.LastIndex(pod, "-"); i >= 0 {
		if n, err := strconv.Atoi(pod[i+1:]); err == nil && n >= 0 {
			return int32(n)
		}
	}
	log.Fatalf("sharded world needs -sid or a POD_NAME with a trailing ordinal (POD_NAME=%q)", pod)
	return 0
}
