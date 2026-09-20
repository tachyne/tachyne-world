package server

import (
	"math"
	"testing"
)

// doWaterSplashEffect's own formula: horizontal motion counts for a fifth of
// vertical, so a dive from height is loud and wading in is quiet. Past a
// quarter it is the heavier splash.
func TestSplashVolumeAndSound(t *testing.T) {
	for _, tc := range []struct {
		name       string
		vx, vy, vz float64
		wantVol    float32
		player     string
		generic    string
	}{
		{"wading in", 0.1, 0, 0.1, float32(math.Min(1, math.Sqrt(0.01*0.2+0+0.01*0.2)*0.2)),
			"minecraft:entity.player.splash", "minecraft:entity.generic.splash"},
		{"a dive", 0, -2, 0, float32(math.Min(1, 2*0.2)),
			"minecraft:entity.player.splash.high_speed", "minecraft:entity.generic.splash"},
	} {
		got := splashVolume(tc.vx, tc.vy, tc.vz)
		if math.Abs(float64(got-tc.wantVol)) > 1e-6 {
			t.Errorf("%s: volume %.4f, want %.4f", tc.name, got, tc.wantVol)
		}
		if s := splashSound(got, true); s != tc.player {
			t.Errorf("%s: a player makes %q, want %q", tc.name, s, tc.player)
		}
		// Everything else uses the one generic splash for both speeds.
		if s := splashSound(got, false); s != tc.generic {
			t.Errorf("%s: a mob makes %q, want %q", tc.name, s, tc.generic)
		}
	}
	// The volume is capped at 1 however fast it arrives.
	if v := splashVolume(0, -50, 0); v != 1 {
		t.Errorf("a terminal-velocity splash is %v, want it capped at 1", v)
	}
}

// LivingEntity.getFallDamageSound: the big one past four points of damage.
func TestFallDamageSoundSplit(t *testing.T) {
	for _, tc := range []struct {
		damage float64
		want   string
	}{
		{1, "minecraft:entity.generic.small_fall"},
		{4, "minecraft:entity.generic.small_fall"}, // four is NOT past four
		{5, "minecraft:entity.generic.big_fall"},
		{20, "minecraft:entity.generic.big_fall"},
	} {
		if got := fallDamageSound(tc.damage); got != tc.want {
			t.Errorf("%.0f damage plays %q, want %q", tc.damage, got, tc.want)
		}
	}
}

// Each boss's bar is its own: the dragon pink with music and world fog, the
// wither purple and screen-darkening, a raid red and notched into ten. They
// were all drawn purple and solid, so you could not tell them apart.
func TestEachBossHasItsOwnBar(t *testing.T) {
	for _, tc := range []struct {
		name string
		look bossLook
		want bossLook
	}{
		{"dragon", dragonBarLook, bossLook{0, 0, 0x02 | 0x04}},
		{"wither", witherBarLook, bossLook{5, 0, 0x01}},
		{"raid", raidBarLook, bossLook{2, 2, 0}},
	} {
		if tc.look != tc.want {
			t.Errorf("%s bar is %+v, want %+v", tc.name, tc.look, tc.want)
		}
	}
	if dragonBarLook == witherBarLook || witherBarLook == raidBarLook {
		t.Error("two bosses share a bar")
	}
	// …and the frame carries it.
	ev := bossBarAdd([16]byte{1}, "Ender Dragon", 0.5, dragonBarLook)
	if ev.Color != dragonBarLook.colour || ev.Overlay != dragonBarLook.overlay || ev.Flags != dragonBarLook.flags {
		t.Errorf("the frame dropped the look: %+v", ev)
	}
}
