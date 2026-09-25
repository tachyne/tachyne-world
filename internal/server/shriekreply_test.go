package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// SculkShriekerBlockEntity.playWardenReplySound: the warden's answer is
// SOUND_BY_LEVEL — close, closer, closest, then listening angry — from a
// random point within 10 blocks of the shrieker.
func TestShriekerReplySoundByLevel(t *testing.T) {
	for lvl, want := range map[int]string{
		1: "minecraft:entity.warden.nearby_close",
		2: "minecraft:entity.warden.nearby_closer",
		3: "minecraft:entity.warden.nearby_closest",
	} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		pl.x, pl.y, pl.z = 0.5, 180, 0.5
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		pos := simPos{blockPos: blockPos{0, 180, 0}}
		h.sculkWarn[pos] = lvl
		drainOut(pl.p)
		h.shriekerRespond(players, pos, shriekerBase) // can_summon, shrieking
		got := ""
		var sx, sy, sz float64
		for len(pl.p.out) > 0 {
			if ev, ok := (<-pl.p.out).ev.(attachproto.Sound); ok && got == "" {
				got, sx, sy, sz = ev.Name, ev.X, ev.Y, ev.Z
			}
		}
		if got != want {
			t.Errorf("warning level %d answered with %q, want %q", lvl, got, want)
		}
		if sx < -10 || sx > 11 || sy < 170 || sy > 191 || sz < -10 || sz > 11 {
			t.Errorf("the reply came from (%.0f,%.0f,%.0f), past 10 blocks", sx, sy, sz)
		}
	}
}
