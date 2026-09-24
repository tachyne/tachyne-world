package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func effectCmdFixture(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	return h, players, pl
}

// chatSince collects the chat lines queued to a player.
func chatSince(pl *tracked) (lines []string, effects []attachproto.Effect) {
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.Chat:
			lines = append(lines, e.Text)
		case attachproto.Effect:
			effects = append(effects, e)
		}
	}
	return
}

// /effect give @s speed infinite: the effect never runs down, the client is
// told -1, and it survives a save.
func TestEffectGiveInfinite(t *testing.T) {
	h, players, pl := effectCmdFixture(t)
	drainEvs(pl.p)
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, id: effSpeed, secs: effInfinite, amp: 1, hide: true})
	lines, effs := chatSince(pl)
	if len(lines) != 1 || lines[0] != "Applied effect Speed to "+pl.p.name {
		t.Errorf("feedback %q", lines)
	}
	if len(effs) != 1 || effs[0].Ticks != -1 || !effs[0].NoParticles || effs[0].Amp != 1 {
		t.Errorf("effect frame %+v, want Ticks -1, amp 1, no particles", effs)
	}
	for i := 0; i < 5000; i++ {
		h.updateEffects(players)
	}
	if e := pl.effects[effSpeed]; e == nil || e.left != effInfinite {
		t.Fatalf("after 5000 ticks speed is %+v, want still infinite", e)
	}
	saved := savedEffectsOf(pl)
	pl.effects = nil
	restoreSavedEffects(pl, saved)
	if e := pl.effects[effSpeed]; e == nil || !e.infinite() || !e.noParticles {
		t.Errorf("reloaded speed %+v, want infinite with hidden particles", e)
	}
}

// A stronger, finite effect over an infinite one hides it; when the stronger
// one runs out the infinite one comes back. A weaker finite one never
// replaces the infinite duration.
func TestInfiniteEffectHiddenStack(t *testing.T) {
	h, players, pl := effectCmdFixture(t)
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, id: effSpeed, secs: effInfinite})
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, id: effSpeed, secs: 1, amp: 2})
	if e := pl.effects[effSpeed]; e.amp != 2 || e.left != 20 || e.hidden == nil || !e.hidden.infinite() {
		t.Fatalf("speed %+v, want III for 20 ticks over a hidden infinite I", e)
	}
	for i := 0; i < 20; i++ {
		h.updateEffects(players)
	}
	if e := pl.effects[effSpeed]; e == nil || e.amp != 0 || !e.infinite() {
		t.Fatalf("after the III ran out speed is %+v, want the infinite I back", e)
	}
	drainEvs(pl.p)
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, id: effSpeed, secs: 30})
	if lines, _ := chatSince(pl); len(lines) != 1 || !strings.HasPrefix(lines[0], "Unable to apply") {
		t.Errorf("a shorter equal effect over an infinite one: %q, want the failure", lines)
	}
}

// An infinite periodic effect still fires: its cadence reads the tick count.
func TestInfiniteRegenerationHeals(t *testing.T) {
	h, players, pl := effectCmdFixture(t)
	pl.health = 10
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, id: effRegen, secs: effInfinite})
	for i := 0; i < 100; i++ {
		h.tick.Add(1)
		h.updateEffects(players)
	}
	if pl.health != 12 {
		t.Errorf("infinite Regeneration I healed to %v over 100 ticks, want 12 (one every 50)", pl.health)
	}
}

// /effect clear with and without an effect, and the default duration.
func TestEffectClearAndDefaults(t *testing.T) {
	h, players, pl := effectCmdFixture(t)
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, id: effHaste, secs: -2})
	if e := pl.effects[effHaste]; e == nil || e.left != 600 {
		t.Fatalf("default haste %+v, want 600 ticks", e)
	}
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, id: effLuck, secs: 10})
	drainEvs(pl.p)
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, clear: true, one: true, id: effHaste})
	if lines, _ := chatSince(pl); len(lines) != 1 || lines[0] != "Removed effect Haste from "+pl.p.name {
		t.Errorf("clear haste feedback %q", lines)
	}
	if pl.effects[effHaste] != nil || pl.effects[effLuck] == nil {
		t.Fatal("clearing haste touched the wrong effects")
	}
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, clear: true, one: true, id: effHaste})
	if lines, _ := chatSince(pl); len(lines) != 1 || lines[0] != "Target doesn't have the requested effect" {
		t.Errorf("clearing a missing effect: %q", lines)
	}
	h.effectCommand(players, evEffect{target: "@s", by: pl.p.eid, clear: true})
	if lines, _ := chatSince(pl); len(lines) != 1 || lines[0] != "Removed every effect from "+pl.p.name || len(pl.effects) != 0 {
		t.Errorf("clear all: %q, %d left", lines, len(pl.effects))
	}
}

// An entity selector reaches mobs; the undead refuse regeneration.
func TestEffectCommandOnMobs(t *testing.T) {
	h, players, pl := effectCmdFixture(t)
	z := h.spawnHostileYIn(players, entityZombie, dimOverworld, 2.5, 180, 2.5)
	if z == nil {
		t.Fatal("no zombie")
	}
	drainEvs(pl.p)
	h.effectCommand(players, evEffect{target: "@e[type=zombie]", by: pl.p.eid, id: effStrength, secs: effInfinite})
	if lines, _ := chatSince(pl); len(lines) != 1 || lines[0] != "Applied effect Strength to Zombie" {
		t.Errorf("feedback %q", lines)
	}
	for i := 0; i < 3000; i++ {
		h.updateMobEffects(players)
	}
	if e := z.effects[effStrength]; e == nil || !e.infinite() {
		t.Errorf("zombie strength %+v, want infinite", e)
	}
	h.effectCommand(players, evEffect{target: "@e[type=zombie]", by: pl.p.eid, id: effRegen, secs: 10})
	if lines, _ := chatSince(pl); len(lines) != 1 || !strings.HasPrefix(lines[0], "Unable to apply") {
		t.Errorf("regeneration on a zombie: %q, want the failure", lines)
	}
}
