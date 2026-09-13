package server

import "testing"

// An idle evoker turns a blue sheep red after its warm-up, and not a
// sheep of any other colour.
func TestEvokerWololo(t *testing.T) {
	h, m, pl, players := evokerSetup(t)
	delete(players, pl.p.eid) // nobody to fight
	m.fangNextAt, m.vexNextAt = ^uint64(0), ^uint64(0)
	blue := h.spawnMobIn(players, entitySheep, 0, 4, 181, 0)
	white := h.spawnMobIn(players, entitySheep, 0, -4, 181, 0)
	if blue == nil || white == nil {
		t.Fatal("sheep spawn returned nil")
	}
	blue.color, white.color = fleeceBlue, 0
	h.gridDirty()

	h.evokerCast(players, m)
	if m.wololoTarget != blue.eid || m.wololoWarm != wololoWarmup {
		t.Fatalf("no wololo begun: target %d warm %d", m.wololoTarget, m.wololoWarm)
	}
	if m.castLeft != wololoCastAnim {
		t.Errorf("casting arms %d, want %d", m.castLeft, wololoCastAnim)
	}
	if blue.color != fleeceBlue {
		t.Fatal("the sheep changed before the warm-up")
	}
	for i := 0; i < wololoWarmup/mobMoveInterval; i++ {
		h.evokerCast(players, m)
	}
	if blue.color != fleeceRed {
		t.Errorf("blue sheep is colour %d after the spell, want red", blue.color)
	}
	if white.color != 0 {
		t.Error("the white sheep was touched")
	}
	// Off cooldown again a red sheep is nothing to it.
	m.wololoNextAt = 0
	if h.wololoStart(players, m) {
		t.Error("wololo began with no blue sheep about")
	}
}

// Mob griefing off keeps the sheep blue.
func TestWololoRespectsMobGriefing(t *testing.T) {
	h, m, pl, players := evokerSetup(t)
	delete(players, pl.p.eid)
	blue := h.spawnMobIn(players, entitySheep, 0, 4, 181, 0)
	blue.color = fleeceBlue
	h.gridDirty()
	h.rules.MobGriefing = false
	if h.wololoStart(players, m) {
		t.Error("wololo began with mob griefing off")
	}
}
