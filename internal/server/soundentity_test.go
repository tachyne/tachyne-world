package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Shearing a sheep plays the snip attached to the sheep (Sheep.shear →
// Level.playSound(null, this, …)), so it follows the animal.
func TestShearSoundFollowsTheSheep(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	m := h.spawnMobIn(players, entitySheep, 0, pl.x+1, pl.y, pl.z)
	drainOut(pl.p)
	h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.sheep.shear", sndNeutral, m.x, m.y, m.z, 1, 1)
	for _, ev := range drainEvs(pl.p) {
		if s, ok := ev.(attachproto.Sound); ok && s.EID == m.eid {
			return
		}
	}
	t.Fatal("no entity-attached sound")
}
