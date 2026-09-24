package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ServerLevel.explode: a ghast's fireball and a wither skull explode with
// the MOB interaction, which keeps every block when mobGriefing is off.
func TestMobProjectileBlastsObeyMobGriefing(t *testing.T) {
	dirt, _ := worldgen.BlockRange("dirt")
	for _, c := range []struct {
		name string
		fire func(h *hub, players map[int32]*tracked, w *mob)
	}{
		{"ghast fireball", func(h *hub, players map[int32]*tracked, w *mob) {
			// launched as the ghast's charge launches it
			a := h.launchProjectileIn(players, entityLargeFireball, 0, 0.5, 72.5, 0.5, hurtingSpeed, 0, 0)
			a.shooter, a.dmg, a.explode, a.fire = w.eid, 6, 1, true
		}},
		{"wither skull", func(h *hub, players map[int32]*tracked, w *mob) {
			h.witherSkullAt(players, w, 6.5, 72.5, 0.5, false)
		}},
	} {
		for _, grief := range []bool{false, true} {
			t.Run(c.name, func(t *testing.T) {
				h, players, w := skullHub(t)
				h.rules.MobGriefing = grief
				for y := 70; y <= 75; y++ {
					for z := -2; z <= 2; z++ {
						h.world.SetBlock(6, y, z, dirt)
					}
				}
				c.fire(h, players, w)
				for i := 0; i < 100 && len(h.arrows) > 0; i++ {
					h.tick.Add(1)
					h.updateArrows(players)
				}
				if len(h.arrows) != 0 {
					t.Fatal("the projectile never struck the wall")
				}
				left := 0
				for y := 70; y <= 75; y++ {
					for z := -2; z <= 2; z++ {
						if h.world.At(6, y, z) == dirt {
							left++
						}
					}
				}
				if !grief && left != 30 {
					t.Fatalf("mobGriefing off, yet the blast broke %d blocks", 30-left)
				}
				if grief && left == 30 {
					t.Fatal("mobGriefing on, and the blast broke nothing")
				}
			})
		}
	}
}
