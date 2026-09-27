package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// AbstractThrownPotion.onHitBlock: a bottle of water thrown at the floor
// douses the cell on the face it struck and the four cells around it —
// fire goes out, a lit candle is snuffed, a lit campfire goes dark — and
// leaves a lit campfire two cells away alone.
func TestWaterSplashDousesFireCandlesCampfires(t *testing.T) {
	h, players, pl := breezeRig(t)
	pl.pitch = 90
	litCamp := setBoolProp(worldgen.BlockBase("campfire"), "lit", true)
	litCandle := setBoolProp(worldgen.BlockBase("candle"), "lit", true)
	h.world.SetBlock(1, 180, 0, litCamp)
	h.world.SetBlock(-1, 180, 0, litCandle)
	h.world.SetBlock(0, 180, 1, fireDefault)
	h.world.SetBlock(3, 180, 0, litCamp)
	pl.inv.slots[0] = invStack{item: itemSplashPotion, count: 1, potion: potWater}
	h.throwSplashPotion(players, pl, 0)
	pl.x = 6.5 // step out of the bottle's way, so it breaks on the floor, not the thrower
	flyAll(h, players, 40)
	if len(h.arrows) != 0 {
		t.Fatal("the bottle never broke")
	}
	if st := h.world.Block(1, 180, 0); !isCampfireBlock(st) || boolProp(st, "lit") {
		t.Errorf("the campfire beside the splash was not doused: %d", st)
	}
	if st := h.world.Block(-1, 180, 0); !inRanges(candleRanges, st) || boolProp(st, "lit") {
		t.Errorf("the candle beside the splash was not snuffed: %d", st)
	}
	if st := h.world.Block(0, 180, 1); st != worldgen.Air {
		t.Errorf("the fire beside the splash still burns: %d", st)
	}
	if st := h.world.Block(3, 180, 0); !boolProp(st, "lit") {
		t.Error("a campfire out of the splash's reach went out")
	}
}

// Only a bottle of water douses: a thrown Swiftness leaves the campfire lit.
func TestOtherSplashLeavesCampfireLit(t *testing.T) {
	h, players, pl := breezeRig(t)
	pl.pitch = 90
	litCamp := setBoolProp(worldgen.BlockBase("campfire"), "lit", true)
	h.world.SetBlock(1, 180, 0, litCamp)
	pl.inv.slots[0] = invStack{item: itemSplashPotion, count: 1, potion: potSwiftness}
	h.throwSplashPotion(players, pl, 0)
	pl.x = 6.5
	flyAll(h, players, 40)
	if !boolProp(h.world.Block(1, 180, 0), "lit") {
		t.Error("a potion of Swiftness doused a campfire")
	}
}

// ThrownSplashPotion.onHitAsPotion doses every isAffectedByPotions living
// entity: a creative player is poisoned like a survival one; a spectator is
// never found.
func TestSplashDosesCreativeNotSpectator(t *testing.T) {
	h, players, pl := breezeRig(t)
	pl.gamemode = gmCreative
	spec := survPlayer(h)
	spec.x, spec.y, spec.z = 1.5, 180, 0.5
	spec.gamemode = gmSpectator
	spec.p.eid = pl.p.eid + 1
	players[spec.p.eid] = spec
	h.splashPotion(players, 0, 0.5, 180, 0.5, potPoison, false)
	if pl.hasEffect(effPoison) == 0 {
		t.Error("a creative player in the splash was not poisoned")
	}
	if spec.hasEffect(effPoison) != 0 {
		t.Error("a spectator was poisoned by a splash")
	}
}

// Splash potions carry no duration scale: a direct hit gives the drink's
// full duration, rounded (mapDuration's + 0.5), where tachyne gave three
// quarters of it.
func TestSplashFullDurationAtCentre(t *testing.T) {
	h, players, pl := breezeRig(t)
	h.splashPotion(players, 0, 0.5, 180, 0.5, potSwiftness, false)
	want := potionEffects(potSwiftness)[0].ticks
	if got := pl.effects[effSpeed].left; got != want {
		t.Fatalf("a direct splash of Swiftness gave %d ticks, want the full %d", got, want)
	}
}

// Axolotl.rehydrate: a splash of water hands an axolotl drying on land 1800
// ticks of air back.
func TestWaterSplashRehydratesAxolotl(t *testing.T) {
	h, players, _ := breezeRig(t)
	ax := h.spawnMob(players, entityAxolotl, 2.5, 180, 0.5)
	ax.dryTicks = 3000
	h.splashPotion(players, 0, 0.5, 180, 0.5, potWater, false)
	if ax.dryTicks != 3000-axolotlRehydrate {
		t.Fatalf("an axolotl splashed with water has %d dry ticks, want %d", ax.dryTicks, 3000-axolotlRehydrate)
	}
}
