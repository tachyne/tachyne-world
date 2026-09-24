package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// What each projectile shows when it breaks on a block, as vanilla does:
// a snowball's entity event 3 (its item in bits), a shulker bullet's small
// explosion burst, and nothing generic at all for a small fireball or a
// wither skull (which end in fire or their explosion).
func TestProjectileBreakEffects(t *testing.T) {
	type seen struct {
		poof, explosion int
		status3         bool
	}
	fly := func(t *testing.T, etype int, setup func(a *arrowEntity)) seen {
		t.Helper()
		h := newHub(world.New(1))
		h.world.ForceLoad(0, 0, 2)
		h.rules.MobGriefing = false
		h.arrows = map[int32]*arrowEntity{}
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
				for y := 180; y < 186; y++ {
					h.world.SetBlock(x, y, z, worldgen.Air)
				}
			}
		}
		watch := testTracked()
		watch.p.eid, watch.gamemode = 900, gmCreative
		watch.x, watch.y, watch.z = 3.5, 180, 3.5
		watch.p.out = make(chan outPkt, 4096)
		players := map[int32]*tracked{watch.p.eid: watch}
		a := h.launchProjectileIn(players, etype, 0, 0.5, 181.5, 0.5, 0, -0.5, 0)
		a.breaks = true
		setup(a)
		for i := 0; i < 20 && len(h.arrows) > 0; i++ {
			h.tick.Add(1)
			h.updateArrows(players)
		}
		if len(h.arrows) != 0 {
			t.Fatal("the projectile never broke on the floor")
		}
		close(watch.p.out)
		var s seen
		for pk := range watch.p.out {
			switch ev := pk.ev.(type) {
			case attachproto.Particles:
				if ev.PID == particlePoof {
					s.poof++
				}
				if ev.PID == particleExplosion {
					s.explosion++
				}
			case attachproto.EntityStatus:
				if ev.EID == a.eid && ev.Status == entityStatusProjectileBreak {
					s.status3 = true
				}
			}
		}
		return s
	}

	if s := fly(t, entitySnowball, func(a *arrowEntity) {}); !s.status3 || s.poof != 0 {
		t.Errorf("snowball: entity event 3 %v, poofs %d; want the event and no poof", s.status3, s.poof)
	}
	if s := fly(t, entitySmallFireball, func(a *arrowEntity) { a.dmg, a.fire = 5, true }); s.poof != 0 || s.status3 {
		t.Errorf("small fireball: %d poofs, event 3 %v; vanilla shows neither", s.poof, s.status3)
	}
	if s := fly(t, entityWitherSkull, func(a *arrowEntity) { a.dmg, a.explode = 8, 1 }); s.poof != 0 {
		t.Errorf("wither skull: %d poofs; it ends in its explosion only", s.poof)
	}
	if s := fly(t, entityShulkerBullet, func(a *arrowEntity) { a.dmg = 4 }); s.explosion == 0 || s.poof != 0 {
		t.Errorf("shulker bullet: %d explosion bursts, %d poofs; want the burst and no poof", s.explosion, s.poof)
	}
}
