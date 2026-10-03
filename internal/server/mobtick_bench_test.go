package server

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BenchmarkMobTick is the mob loop's cost per game tick for a live-sized
// population: a few players and some hundreds of mobs of the common kinds
// spread over the chunks about them, at night so the hostiles hunt. One op
// is one game tick of the hub loop's mob phase (updateMobs, which runs
// pushMobs), so ns/op reads directly against the 50 ms tick budget.
//
//	go test -run '^$' -bench MobTick -benchtime 400x ./internal/server/
func BenchmarkMobTick(b *testing.B) {
	for _, n := range []int{300, 600} {
		b.Run(fmt.Sprintf("mobs=%d", n), func(b *testing.B) {
			h, players := benchMobWorld(b, n)
			for i := 0; i < 200; i++ { // let paths, targets and the crowd settle
				benchGameTick(h, players)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchGameTick(h, players)
			}
			b.StopTimer()
			b.ReportMetric(float64(len(h.mobs)), "mobs")
		})
	}
}

// benchGameTick is one tick of the hub loop's mob phase, with the
// gateways' side of it: whatever the tick sent the players is drained.
func benchGameTick(h *hub, players map[int32]*tracked) {
	h.tick.Add(1)
	h.updateMobs(players)
	for _, t := range players {
		t.health, t.dead = 20, false // the players are there to be hunted, not to die
		for {
			select {
			case <-t.p.out:
				continue
			default:
			}
			break
		}
	}
}

// benchMix is the population's make-up: mostly the overworld's common
// animals and monsters, some villagers, and a few swimmers and bats.
var benchMix = []struct {
	name   string
	weight int
}{
	{"cow", 8}, {"sheep", 8}, {"pig", 6}, {"chicken", 6}, {"rabbit", 3}, {"horse", 2}, {"wolf", 2},
	{"zombie", 10}, {"skeleton", 8}, {"creeper", 6}, {"spider", 6}, {"enderman", 3}, {"witch", 1},
	{"villager", 8}, {"iron_golem", 1}, {"squid", 3}, {"cod", 3}, {"bat", 3},
}

// benchMobWorld builds the fixture: terrain loaded 8 chunks about the
// origin, three survival players, and n mobs on the surface within 64
// blocks of them.
func benchMobWorld(b *testing.B, n int) (*hub, map[int32]*tracked) {
	b.Helper()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(os.Stderr) })
	w := world.New(42)
	h := newTestHub(w)
	w.ForceLoad(0, 0, 8)
	h.tick.Store(18000) // midnight: the monsters are out
	players := map[int32]*tracked{}
	for i, p := range [][2]float64{{0.5, 0.5}, {40.5, 30.5}, {-35.5, 20.5}} {
		t := &tracked{p: newPlayer(int32(9000+i), fmt.Sprintf("bench%d", i), [16]byte{byte(i + 1)}), gamemode: gmSurvival}
		initSurvival(t)
		x, z := int(p[0]), int(p[1])
		t.x, t.y, t.z = p[0], float64(w.SurfaceFeet(x, z)), p[1]
		players[t.p.eid] = t
	}
	h.playersRef = players
	total := 0
	for _, m := range benchMix {
		total += m.weight
	}
	r := rand.New(rand.NewSource(7))
	for tries := 0; len(h.mobs) < n && tries < n*20; tries++ {
		pick := r.Intn(total)
		name := ""
		for _, m := range benchMix {
			if pick < m.weight {
				name = m.name
				break
			}
			pick -= m.weight
		}
		x, z := r.Intn(128)-64, r.Intn(128)-64
		y := w.MobFeet(x, z)
		et := entityID(name)
		wet := worldgen.HoldsWater(w.At(x, y, z))
		switch name {
		case "squid", "cod":
			if !wet {
				continue
			}
		case "bat":
			y += 3
		default:
			if wet {
				continue
			}
		}
		fx, fy, fz := float64(x)+0.5, float64(y), float64(z)+0.5
		switch name {
		case "zombie", "skeleton", "creeper", "spider", "enderman", "witch":
			h.spawnHostileY(players, et, fx, fy, fz)
		default:
			if m := h.spawnMob(players, et, fx, fy, fz); m != nil {
				h.applySpecies(players, m)
			}
		}
	}
	if len(h.mobs) < n*9/10 {
		b.Fatalf("placed %d mobs of %d", len(h.mobs), n)
	}
	return h, players
}
