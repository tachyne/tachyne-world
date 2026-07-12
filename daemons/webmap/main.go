// webmap is a daemon plugin: a live web map of the running world, fed
// entirely by the bus — players tracked in real time from movement events,
// mobs refreshed by the mobs query. No engine changes, no compilation into
// the core; run it beside any tachyne server:
//
//	tachyne-plugin-manager run github.com/tachyne/tachyne-world/daemons/webmap
//
// or by hand: NATS_URL=nats://… webmap -addr :8100, then open the page.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/tachyne/tachyne-world/busplugin"
	"github.com/tachyne/tachyne-world/plugin"
)

type entity struct {
	EID  int32   `json:"eid"`
	Name string  `json:"name,omitempty"`
	Type string  `json:"type,omitempty"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Z    float64 `json:"z"`
	Dim  int     `json:"dim"`
}

type state struct {
	mu      sync.Mutex
	players map[int32]*entity
	mobs    map[int32]*entity
	world   map[string]any

	// downSince dedups poll-failure logging: the engine restarting mid-poll
	// is routine (rolling deploys), so log the outage once and the recovery
	// once instead of one line per 2-second poll.
	downSince time.Time
}

// pollFailed/pollOK bracket an outage in the log.
func (st *state) pollFailed(err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.downSince.IsZero() {
		st.downSince = time.Now()
		log.Printf("engine queries failing (%v) — serving last known state until it returns", err)
	}
}

func (st *state) pollOK() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.downSince.IsZero() {
		log.Printf("engine back after %s", time.Since(st.downSince).Round(time.Second))
		st.downSince = time.Time{}
	}
}

func main() {
	// Daemons keep timestamps: their logs are read next to the engine's when
	// correlating incidents (a bare "query failed" line is useless at 3am).
	log.SetFlags(log.LstdFlags)
	addr := flag.String("addr", ":8100", "HTTP listen address")
	flag.Parse()

	c, err := busplugin.ConnectEnv()
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	st := &state{players: map[int32]*entity{}, mobs: map[int32]*entity{}}

	// Players: primed by query, then live from the event stream.
	st.refreshPlayers(c)
	busplugin.On(c, "player_join", func(e plugin.PlayerJoinEvent) {
		st.mu.Lock()
		st.players[e.EID] = &entity{EID: e.EID, Name: e.Name, X: e.X, Y: e.Y, Z: e.Z, Dim: e.Dim}
		st.mu.Unlock()
	})
	busplugin.On(c, "player_quit", func(e plugin.PlayerQuitEvent) {
		st.mu.Lock()
		delete(st.players, e.EID)
		st.mu.Unlock()
	})
	busplugin.On(c, "player_move", func(e plugin.PlayerMoveEvent) {
		st.mu.Lock()
		if p := st.players[e.EID]; p != nil {
			p.X, p.Y, p.Z, p.Dim = e.ToX, e.ToY, e.ToZ, e.Dim
		}
		st.mu.Unlock()
	})

	// Mobs move constantly but only spawn/death ride the bus, so poll the
	// query — 2s is smooth enough for a map and costs one request-reply.
	go func() {
		for {
			st.refreshMobs(c)
			st.refreshWorld(c)
			time.Sleep(2 * time.Second)
		}
	}()

	http.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		players := make([]*entity, 0, len(st.players))
		for _, p := range st.players {
			players = append(players, p)
		}
		mobs := make([]*entity, 0, len(st.mobs))
		for _, m := range st.mobs {
			mobs = append(mobs, m)
		}
		world := st.world
		st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"players": players, "mobs": mobs, "world": world})
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	})

	log.Printf("webmap on %s", *addr)
	c.Announce("webmap", "Live web map is up — open http://<server-host>"+*addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func (st *state) refreshPlayers(c *busplugin.Conn) {
	var out struct {
		Players []struct {
			EID  int32   `json:"eid"`
			Name string  `json:"name"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
			Z    float64 `json:"z"`
			Dim  int     `json:"dim"`
		} `json:"players"`
	}
	if err := c.Request("players", nil, &out); err != nil {
		st.pollFailed(err)
		return
	}
	st.pollOK()
	st.mu.Lock()
	st.players = map[int32]*entity{}
	for _, p := range out.Players {
		st.players[p.EID] = &entity{EID: p.EID, Name: p.Name, X: p.X, Y: p.Y, Z: p.Z, Dim: p.Dim}
	}
	st.mu.Unlock()
}

func (st *state) refreshMobs(c *busplugin.Conn) {
	var out struct {
		Mobs []struct {
			EID  int32   `json:"eid"`
			Type string  `json:"type"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
			Z    float64 `json:"z"`
			Dim  int     `json:"dim"`
		} `json:"mobs"`
	}
	if err := c.Request("mobs", nil, &out); err != nil {
		st.pollFailed(err)
		return
	}
	st.pollOK()
	st.mu.Lock()
	st.mobs = map[int32]*entity{}
	for _, m := range out.Mobs {
		st.mobs[m.EID] = &entity{EID: m.EID, Type: m.Type, X: m.X, Y: m.Y, Z: m.Z, Dim: m.Dim}
	}
	st.mu.Unlock()
}

func (st *state) refreshWorld(c *busplugin.Conn) {
	var out map[string]any
	if err := c.Request("world", nil, &out); err != nil {
		return
	}
	st.mu.Lock()
	st.world = out
	st.mu.Unlock()
}

// page is the whole client: a canvas, dots, one fetch per second.
const page = `<!doctype html>
<meta charset="utf-8">
<title>tachyne webmap</title>
<style>
  html,body{margin:0;height:100%;background:#10141a;color:#cfd8e3;
    font:13px/1.4 system-ui,sans-serif}
  #hud{position:fixed;top:10px;left:12px;background:rgba(16,20,26,.85);
    padding:8px 12px;border-radius:8px;border:1px solid #2a3442}
  canvas{display:block;width:100vw;height:100vh}
  .k{color:#7f8ea3}
</style>
<div id="hud">loading…</div>
<canvas id="map"></canvas>
<script>
const cv = document.getElementById('map'), hud = document.getElementById('hud');
const ctx = cv.getContext('2d');
const HOSTILE = new Set(['zombie','skeleton','spider','creeper','enderman','witch','slime',
  'husk','stray','drowned','phantom','blaze','ghast','magma_cube','zombified_piglin',
  'wither_skeleton','pillager','ravager','vindicator','evoker','warden','wither','ender_dragon',
  'piglin','piglin_brute','hoglin','zoglin','guardian','elder_guardian','vex','silverfish',
  'cave_spider','endermite','shulker','bogged','breeze','creaking']);
let snap = {players:[],mobs:[],world:{}};

async function poll(){
  try {
    snap = await (await fetch('/state')).json();
    const w = snap.world||{};
    hud.innerHTML = '<b>tachyne webmap</b> &nbsp; '
      + '<span class=k>players</span> ' + (snap.players||[]).length
      + ' &nbsp;<span class=k>mobs</span> ' + (snap.mobs||[]).length
      + ' &nbsp;<span class=k>time</span> ' + (w.day_time??'?')
      + (w.raining ? ' &nbsp;🌧' : '') + (w.thundering ? '⛈' : '');
  } catch(e) { hud.textContent = 'server unreachable'; }
}
setInterval(poll, 1000); poll();

function draw(){
  const W = cv.width = innerWidth * devicePixelRatio;
  const H = cv.height = innerHeight * devicePixelRatio;
  ctx.clearRect(0,0,W,H);
  const ents = [...(snap.players||[]), ...(snap.mobs||[])].filter(e => e.dim === 0);
  // Fit view to entities (min span 128 blocks) centered on their midpoint.
  let minX=-64,maxX=64,minZ=-64,maxZ=64;
  if (ents.length) {
    minX=Math.min(...ents.map(e=>e.x)); maxX=Math.max(...ents.map(e=>e.x));
    minZ=Math.min(...ents.map(e=>e.z)); maxZ=Math.max(...ents.map(e=>e.z));
  }
  const cx=(minX+maxX)/2, cz=(minZ+maxZ)/2;
  const span=Math.max(maxX-minX, maxZ-minZ, 128)*1.2;
  const s=Math.min(W,H)/span;
  const px=x=>W/2+(x-cx)*s, pz=z=>H/2+(z-cz)*s;

  // Block grid every 16 (chunk lines), faint.
  ctx.strokeStyle='#1c2430'; ctx.lineWidth=1;
  const step=16*s, x0=px(Math.floor((cx-span/2)/16)*16), z0=pz(Math.floor((cz-span/2)/16)*16);
  for(let x=x0; x<W; x+=step){ ctx.beginPath(); ctx.moveTo(x,0); ctx.lineTo(x,H); ctx.stroke(); }
  for(let z=z0; z<H; z+=step){ ctx.beginPath(); ctx.moveTo(0,z); ctx.lineTo(W,z); ctx.stroke(); }
  // Origin cross.
  ctx.strokeStyle='#2a3442';
  ctx.beginPath(); ctx.moveTo(px(0),0); ctx.lineTo(px(0),H); ctx.stroke();
  ctx.beginPath(); ctx.moveTo(0,pz(0)); ctx.lineTo(W,pz(0)); ctx.stroke();

  ctx.font = (11*devicePixelRatio)+'px system-ui';
  for (const m of (snap.mobs||[])) {
    if (m.dim !== 0) continue;
    ctx.fillStyle = HOSTILE.has(m.type) ? '#e15d5d' : '#69b56b';
    ctx.beginPath(); ctx.arc(px(m.x), pz(m.z), 3*devicePixelRatio, 0, 7); ctx.fill();
  }
  for (const p of (snap.players||[])) {
    if (p.dim !== 0) continue;
    ctx.fillStyle = '#ffd76e';
    ctx.beginPath(); ctx.arc(px(p.x), pz(p.z), 5*devicePixelRatio, 0, 7); ctx.fill();
    ctx.fillText(p.name, px(p.x)+8*devicePixelRatio, pz(p.z)+4*devicePixelRatio);
  }
  requestAnimationFrame(draw);
}
requestAnimationFrame(draw);
</script>`
