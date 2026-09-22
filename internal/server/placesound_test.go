package server

import (
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BlockItem.place's voice, read off the sound type: the place event at
// (volume+1)/2 and 0.8 of the type's pitch. Note bamboo_sapling — its step
// sound is the bamboo one while its place sound is its own, which is why
// the place name has to come from the table rather than from rewriting
// ".step" into ".place".
func TestBlockPlaceSoundMatchesVanilla(t *testing.T) {
	for _, tc := range []struct {
		block      string
		want       string
		vol, pitch float32
	}{
		{"oak_planks", "minecraft:block.wood.place", 1, 0.8},
		{"stone", "minecraft:block.stone.place", 1, 0.8},
		{"iron_block", "minecraft:block.iron.place", 1, 0.8}, // IRON, not METAL
		{"rail", "minecraft:block.metal.place", 1, 1.2},      // METAL's pitch is 1.5
		{"anvil", "minecraft:block.anvil.place", 0.65, 0.8},  // ANVIL's volume is 0.3
		{"white_wool", "minecraft:block.wool.place", 1, 0.8},
		{"bamboo_sapling", "minecraft:block.bamboo_sapling.place", 1, 0.8},
		{"glass", "minecraft:block.glass.place", 1, 0.8},
		{"sand", "minecraft:block.sand.place", 1, 0.8},
	} {
		name, vol, pitch := blockPlaceSound(worldgen.BlockBase(tc.block))
		if name != tc.want {
			t.Errorf("%s places with %q, want %q", tc.block, name, tc.want)
		}
		if vol != tc.vol || pitch != tc.pitch {
			t.Errorf("%s places at %v/%v, want %v/%v", tc.block, vol, pitch, tc.vol, tc.pitch)
		}
	}
	// A block absent from the table is stone, as the generated map's header says.
	if name, _, _ := blockPlaceSound(worldgen.BlockBase("bedrock")); name != "minecraft:block.stone.place" {
		t.Errorf("an untyped block should place like stone, got %q", name)
	}
	// SoundEvents.EMPTY means silence, and comes back with no name to send.
	// A dried ghast places silently while making a step sound; a cactus
	// flower is the other way round — which is the whole reason the two
	// tables are generated apart rather than derived from each other.
	if name, _, _ := blockPlaceSound(worldgen.BlockBase("dried_ghast")); name != "" {
		t.Errorf("a dried ghast should place in silence, got %q", name)
	}
	if name, _, _ := blockPlaceSound(worldgen.BlockBase("cactus_flower")); name != "minecraft:block.cactus_flower.place" {
		t.Errorf("a cactus flower has a place sound but no step sound, got %q", name)
	}
}

// The step table and the place table disagree for more than one sound type,
// so deriving one from the other is wrong wherever they do.
func TestPlaceSoundsAreNotStepSounds(t *testing.T) {
	var differ int
	for typ, place := range soundTypePlaces {
		step := soundTypeSteps[typ].name
		if step == "" {
			t.Errorf("sound type %s has a place sound but no step sound", typ)
		}
		// The two tables are keyed the same; the names differ where vanilla
		// borrows one family's step and keeps its own place.
		if place != step[:len(step)-len(".step")]+".place" {
			differ++
		}
	}
	if differ == 0 {
		t.Fatal("no sound type distinguishes its place sound from its step sound — the table is suspect")
	}
	if len(soundTypePlaces) != len(soundTypeSteps) {
		t.Fatalf("%d place sounds for %d step sounds", len(soundTypePlaces), len(soundTypeSteps))
	}
}

// End to end through handlePlace: the bystander hears the block go down and
// the placer does not, because their own client played it on prediction.
func TestPlaceSoundReachesOthersNotThePlacer(t *testing.T) {
	s, h, p := breakPlaceServer(t)

	// A second player standing beside the placer.
	other := newPlayer(h.allocEID(), "bystander", [16]byte{2})
	other.x, other.y, other.z = p.x+2, p.y, p.z
	h.post(evJoin{p: other, x: other.x, y: other.y, z: other.z, gamemode: gmCreative})
	waitJoined(t, h, "bystander")

	p.setHotbarSlot(0, itemByName["oak_planks"])
	p.held = 0
	x, y, z := int(p.x), int(p.y)-1, int(p.z)
	s.world.SetBlock(x, y, z, worldgen.BlockBase("stone"))
	s.world.SetBlock(x, y+1, z, worldgen.Air)

	drainOut(p)
	drainOut(other)
	s.handlePlace(p, placeBody(x, y, z, 1)) // click the top face

	if got := s.world.Block(x, y+1, z); got != worldgen.BlockBase("oak_planks") {
		t.Fatalf("the planks never went down: state %d", got)
	}
	if !heardSound(other, "minecraft:block.wood.place") {
		t.Error("the bystander should hear the planks go down")
	}
	if heardSound(p, "minecraft:block.wood.place") {
		t.Error("the placer's own client played it — the server must not send it again")
	}
}

func drainOut(p *player) {
	for {
		select {
		case <-p.out:
		default:
			return
		}
	}
}

// heardSound waits briefly for a named sound to reach this player.
func heardSound(p *player, name string) bool {
	deadline := time.Now().Add(hubTestWait)
	for time.Now().Before(deadline) {
		select {
		case pkt := <-p.out:
			if ev, ok := pkt.ev.(attachproto.Sound); ok && ev.Name == name {
				return true
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	return false
}
