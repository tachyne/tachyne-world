package server

import "testing"

func TestHudRender(t *testing.T) {
	v := HudView{X: 100.4, Y: 64, Z: -200.9, Yaw: 0, DayTime: 6000, Online: 3, Gamemode: gmCreative, Biome: "minecraft:snowy_plains", Shard: -1}
	got := renderHud(defaultHud(), v)
	want := "12:00   XYZ 100 64 -201   facing S   Snowy Plains   Creative   3 online"
	if got != want {
		t.Errorf("renderHud:\n got %q\nwant %q", got, want)
	}
	// Sharded render leads with the shard tag.
	sv := v
	sv.Shard = 1
	if got := renderHud(defaultHud(), sv); got != "shard 1   "+want {
		t.Errorf("sharded renderHud = %q", got)
	}
	if clockString(0) != "06:00" || clockString(18000) != "00:00" {
		t.Errorf("clock: 0=%q 18000=%q", clockString(0), clockString(18000))
	}
	if prettyBiome("minecraft:desert") != "Desert" {
		t.Errorf("prettyBiome desert = %q", prettyBiome("minecraft:desert"))
	}
}
