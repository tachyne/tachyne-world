package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// drainFXSounds splits what was queued for a player into level events and
// named sounds.
func drainFXSounds(pl *tracked) (fx []attachproto.WorldFX, sounds []string) {
	for {
		select {
		case pkt := <-pl.p.out:
			switch ev := pkt.ev.(type) {
			case attachproto.WorldFX:
				fx = append(fx, ev)
			case attachproto.Sound:
				sounds = append(sounds, ev.Name)
			}
		default:
			return fx, sounds
		}
	}
}

// throwDown has the player throw at their feet and flies the throw out.
func throwDown(t *testing.T, throw func(h *hub, players map[int32]*tracked, pl *tracked)) ([]attachproto.WorldFX, []string) {
	t.Helper()
	h, players, pl := breezeRig(t)
	pl.pitch = 90
	throw(h, players, pl)
	if len(h.arrows) != 1 {
		t.Fatalf("the throw launched %d projectiles", len(h.arrows))
	}
	drainFXSounds(pl)
	flyAll(h, players, 40)
	if len(h.arrows) != 0 {
		t.Fatal("the bottle never broke")
	}
	return drainFXSounds(pl)
}

func hasFX(fx []attachproto.WorldFX, event, data int32) bool {
	for _, e := range fx {
		if e.Event == event && e.Data == data {
			return true
		}
	}
	return false
}

func noBreakSound(t *testing.T, sounds []string) {
	t.Helper()
	for _, s := range sounds {
		if s == "minecraft:entity.splash_potion.break" {
			t.Error("the break sound was played directly as well as by its level event")
		}
	}
}

// ThrownExperienceBottle.onHit: the green spell splash (2002) and the
// breaking-glass sound event (1053).
func TestXPBottleSplashes(t *testing.T) {
	fx, sounds := throwDown(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		pl.inv.slots[0] = invStack{item: itemXPBottle, count: 1}
		h.throwXPBottle(players, pl)
	})
	if !hasFX(fx, worldEventPotionSplash, xpBottleSplashColor) {
		t.Errorf("no green splash 2002 from the bottle: %+v", fx)
	}
	if !hasFX(fx, worldEventSplashSound, 0) {
		t.Errorf("no splash sound event 1053 from the bottle: %+v", fx)
	}
	noBreakSound(t, sounds)
}

// AbstractThrownPotion.onHit: the splash's particles and its sound are two
// events; an instant brew uses 2007/1054, any other 2002/1053.
func TestSplashPotionSoundIsItsLevelEvent(t *testing.T) {
	for _, c := range []struct {
		kind       int8
		ev, sndEvt int32
	}{
		{potHealing, worldEventInstantSplash, worldEventInstantSound},
		{potSwiftness, worldEventPotionSplash, worldEventSplashSound},
	} {
		fx, sounds := throwDown(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
			pl.inv.slots[0] = invStack{item: itemSplashPotion, count: 1, potion: c.kind}
			h.throwSplashPotion(players, pl, 0)
		})
		if !hasFX(fx, c.ev, potionColor(c.kind)) || !hasFX(fx, c.sndEvt, 0) {
			t.Errorf("potion %d: want events %d and %d, got %+v", c.kind, c.ev, c.sndEvt, fx)
		}
		noBreakSound(t, sounds)
	}
}
