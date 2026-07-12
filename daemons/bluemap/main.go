// bluemap is a daemon plugin: a full 3D web map of the world, rendered by
// BlueMap (bluemap.bluecolored.de). The daemon exports the world to the
// vanilla Anvil format on a timer (incrementally — only changed chunks get
// new timestamps), provisions and supervises the BlueMap CLI (downloading
// the jar, and a Java runtime if none is present), and feeds live player
// positions from the bus so players show on the map in real time.
//
// It reads the engine's world files directly, so it must run somewhere with
// access to them (the same host/volume as the server):
//
//	tachyne-plugin-manager run github.com/tachyne/tachyne-world/daemons/bluemap \
//	    -- -accept-download -world /var/world/world.gob -seed <the server's seed>
//
// Every flag falls back to an env var (BLUEMAP_WORLD, TACHYNE_SEED, …), so a
// fleet manager can configure it pod-wide. BlueMap downloads assets from
// Mojang to render with real textures; passing -accept-download (or
// BLUEMAP_ACCEPT_DOWNLOAD=true) asserts the operator accepts Mojang's EULA.
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tachyne/tachyne-world/busplugin"
	"github.com/tachyne/tachyne-world/internal/anvil"
	"github.com/tachyne/tachyne-world/internal/world"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

type dimWorld struct {
	id   string // map id: overworld/nether/end
	file string
	open func(int64, world.Store) (*world.World, error)
}

func main() {
	log.SetFlags(log.LstdFlags)
	worldFile := flag.String("world", envOr("BLUEMAP_WORLD", "world.gob"),
		"overworld edit file; nether.gob/end.gob are read from the same directory")
	seed := flag.Int64("seed", int64(envInt("TACHYNE_SEED", 0)), "world seed (must match the server's)")
	dataDir := flag.String("data", envOr("BLUEMAP_DATA", "bluemap-data"), "working directory (jar, configs, tiles, JRE)")
	addr := flag.String("addr", envOr("BLUEMAP_ADDR", ":8123"), "map webserver address")
	radius := flag.Int("radius", envInt("BLUEMAP_RADIUS", 24), "chunk radius exported around -center (plus all edited chunks)")
	center := flag.String("center", envOr("BLUEMAP_CENTER", "0,0"), "block x,z at the centre of the export window")
	dims := flag.String("dims", envOr("BLUEMAP_DIMS", "overworld,nether,end"), "dimensions to render")
	interval := flag.Duration("interval", mustDuration(envOr("BLUEMAP_INTERVAL", "5m")), "re-export period")
	javaFlag := flag.String("java", os.Getenv("BLUEMAP_JAVA"), "java binary (default: find or provision one)")
	xmx := flag.String("xmx", envOr("BLUEMAP_XMX", "1g"), "BlueMap JVM heap")
	accept := flag.Bool("accept-download", os.Getenv("BLUEMAP_ACCEPT_DOWNLOAD") == "true",
		"accept Mojang's EULA so BlueMap may download the client assets it renders with")
	flag.Parse()

	if !*accept {
		log.Fatal("BlueMap renders with Mojang's own textures and must download them once; " +
			"run with -accept-download (or BLUEMAP_ACCEPT_DOWNLOAD=true) to accept Mojang's EULA " +
			"(https://account.mojang.com/documents/minecraft_eula) and enable this")
	}
	cx, cz, err := parseCenter(*center)
	if err != nil {
		log.Fatalf("-center: %v", err)
	}
	port, err := parsePort(*addr)
	if err != nil {
		log.Fatalf("-addr: %v", err)
	}
	dimList := splitTrim(*dims)
	dir := filepath.Dir(*worldFile)
	byID := map[string]dimWorld{
		"overworld": {"overworld", *worldFile, world.NewWithStore},
		"nether":    {"nether", filepath.Join(dir, "nether.gob"), world.NewNether},
		"end":       {"end", filepath.Join(dir, "end.gob"), world.NewEnd},
	}
	for _, d := range dimList {
		if _, ok := byID[d]; !ok {
			log.Fatalf("unknown dimension %q (want overworld, nether, end)", d)
		}
	}

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	savePath := filepath.Join(abs, "save")

	// Provision: BlueMap jar + a Java it can run on + configs.
	jar, err := ensureBlueMap(abs, envOr("BLUEMAP_VERSION", bluemapVersion))
	if err != nil {
		log.Fatalf("bluemap jar: %v", err)
	}
	java, err := ensureJava(abs, *javaFlag, 25)
	if err != nil {
		log.Fatalf("java: %v", err)
	}
	if err := writeConfigs(abs, savePath, port, dimList); err != nil {
		log.Fatalf("configs: %v", err)
	}

	// First export, then keep it fresh on the interval.
	statePath := filepath.Join(abs, "export-state.json")
	st, err := anvil.LoadExportState(statePath)
	if err != nil {
		log.Fatalf("export state: %v", err)
	}
	export := func() {
		start := time.Now()
		total := 0
		for _, id := range dimList {
			d := byID[id]
			w, err := d.open(*seed, world.NewFileStore(d.file))
			if err != nil {
				log.Printf("%s: %v", id, err)
				continue
			}
			sub := map[string]string{"overworld": "", "nether": "DIM-1", "end": "DIM1"}[id]
			n, err := anvil.Export(w, anvil.Options{
				Dir: savePath, SubDir: sub,
				CenterX: int32(cx >> 4), CenterZ: int32(cz >> 4),
				Radius:    int32(*radius),
				Timestamp: uint32(time.Now().Unix()),
				State:     st,
			})
			if err != nil {
				log.Printf("%s: export: %v", id, err)
				continue
			}
			total += n
			if id == "overworld" {
				y := w.GroundY(cx, cz) + 1
				if err := anvil.WriteLevelDat(filepath.Join(savePath, "level.dat"),
					"tachyne", int32(cx), int32(y), int32(cz)); err != nil {
					log.Printf("level.dat: %v", err)
				}
			}
		}
		if err := st.Save(statePath); err != nil {
			log.Printf("export state: %v", err)
		}
		log.Printf("export: %d chunks changed in %s", total, time.Since(start).Round(time.Millisecond))
	}
	export()

	// The bus is optional: without it the map still renders, just without
	// live players and the op announcement.
	if c, err := busplugin.ConnectEnv(); err != nil {
		log.Printf("bus unavailable (%v) — live players and announce disabled", err)
	} else {
		go livePlayers(c, abs, dimList)
		c.Announce("bluemap", "3D world map is up — open http://<server-host>"+*addr)
	}

	// Supervise BlueMap: render + watch + serve. It re-renders whatever the
	// exports change and its webserver serves the tiles.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	procDone := make(chan error, 1)
	var proc *exec.Cmd
	startBlueMap := func() {
		proc = exec.Command(java, "-Xmx"+*xmx, "-jar", jar, "-r", "-u", "-w")
		proc.Dir = abs
		out, _ := proc.StdoutPipe()
		proc.Stderr = proc.Stdout
		if err := proc.Start(); err != nil {
			procDone <- err
			return
		}
		go func() {
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				log.Printf("[bluemap] %s", sc.Text())
			}
		}()
		go func() { procDone <- proc.Wait() }()
	}
	startBlueMap()

	tick := time.NewTicker(*interval)
	defer tick.Stop()
	backoff := time.Second
	for {
		select {
		case <-tick.C:
			export()
		case err := <-procDone:
			log.Printf("bluemap exited (%v) — restarting in %s", err, backoff)
			select {
			case <-time.After(backoff):
			case <-stop:
				return
			}
			if backoff < time.Minute {
				backoff *= 2
			}
			startBlueMap()
		case <-stop:
			if proc != nil && proc.Process != nil {
				proc.Process.Signal(syscall.SIGTERM)
			}
			return
		}
	}
}

// livePlayers feeds BlueMap's live-data endpoint: the webapp polls
// maps/<id>/live/players.json, which the CLI's webserver serves statically —
// so writing it from bus queries gives real-time player markers even though
// no BlueMap server-plugin is running.
func livePlayers(c *busplugin.Conn, dataDir string, dims []string) {
	type prow struct {
		Name string  `json:"name"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
		Z    float64 `json:"z"`
		Dim  int     `json:"dim"`
	}
	dimOf := map[string]int{"overworld": 0, "nether": 1, "end": 2}
	for range time.Tick(2 * time.Second) {
		var reply struct {
			Players []prow `json:"players"`
		}
		if err := c.Request("players", nil, &reply); err != nil {
			continue // engine restarting; markers just go stale briefly
		}
		for _, id := range dims {
			out := struct {
				Players []map[string]any `json:"players"`
			}{Players: []map[string]any{}}
			for _, p := range reply.Players {
				out.Players = append(out.Players, map[string]any{
					"uuid":    offlineUUID(p.Name),
					"name":    p.Name,
					"foreign": p.Dim != dimOf[id],
					"position": map[string]float64{
						"x": p.X, "y": p.Y, "z": p.Z,
					},
					"rotation": map[string]float64{
						"pitch": 0, "yaw": 0, "roll": 0,
					},
				})
			}
			raw, _ := json.Marshal(out)
			live := filepath.Join(dataDir, "web", "maps", id, "live")
			if err := os.MkdirAll(live, 0o755); err != nil {
				continue
			}
			path := filepath.Join(live, "players.json")
			tmp := path + ".tmp"
			if os.WriteFile(tmp, raw, 0o644) == nil {
				os.Rename(tmp, path)
			}
		}
	}
}

// offlineUUID derives the vanilla offline-mode UUID (v3 of
// "OfflinePlayer:"+name) so markers are stable per player.
func offlineUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = (sum[6] & 0x0f) | 0x30 // version 3
	sum[8] = (sum[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func parseCenter(s string) (int, int, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("want x,z")
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	z, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, err
	}
	return x, z, nil
}

func parsePort(addr string) (int, error) {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0, fmt.Errorf("want [host]:port")
	}
	return strconv.Atoi(addr[i+1:])
}

func splitTrim(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func mustDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}
