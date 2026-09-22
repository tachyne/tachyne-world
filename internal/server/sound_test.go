package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-common/render770"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestSoundBodyInlineShape(t *testing.T) {
	pkt := render770.Sound(soundEv("minecraft:entity.generic.explode", sndBlock, 10, 64, -20, 4, 0.9))
	r := bytes.NewReader(pkt.Body)
	if id, _ := protocol.ReadVarInt(r); id != 0 {
		t.Fatalf("inline sound must lead with holder id 0, got %d", id)
	}
	name, _ := protocol.ReadString(r)
	if name != "minecraft:entity.generic.explode" {
		t.Fatalf("name = %q", name)
	}
	if fixed, _ := r.ReadByte(); fixed != 0 {
		t.Fatal("no fixed range expected")
	}
	if cat, _ := protocol.ReadVarInt(r); cat != sndBlock {
		t.Fatalf("category = %d", cat)
	}
	// x/y/z ×8 fixed point + f32 vol + f32 pitch + i64 seed = 12+4+4+8
	if r.Len() != 28 {
		t.Fatalf("trailing fixed fields = %d bytes, want 28", r.Len())
	}
}

func TestEverySpeciesHasSounds(t *testing.T) {
	for _, et := range []int{entityZombie, entitySkeleton, entitySpider, entityCreeper, entityCow} {
		hurt, death, _ := mobSounds(et)
		if hurt == "" || death == "" {
			t.Fatalf("mob type %d missing hurt/death sounds", et)
		}
	}
}

func TestBlockBreakEventShape(t *testing.T) {
	pkt := render770.WorldFX(blockBreakEvent(5, 64, -3, 10))
	if len(pkt.Body) != 4+8+4+1 {
		t.Fatalf("world_event body = %d bytes, want 17", len(pkt.Body))
	}
	if pkt.Body[3] != 0xD1 { // 2001 = 0x7D1 big-endian i32 low byte
		t.Fatalf("event id bytes wrong: % x", pkt.Body[:4])
	}
}

// A wolf is born with one of seven sound variants and keeps it: its hurt,
// death and idle noises all come from that set, and the idle roll is vanilla's
// three-way one (growl when angry, otherwise a pant or whine one time in
// three).
func TestWolfSoundVariants(t *testing.T) {
	h := newHub(world.New(1))

	for i, suffix := range wolfSoundSuffixes {
		m := &mob{etype: entityWolf, soundSet: int8(i)}
		hurt, death, ambient := h.mobSoundsFor(m)
		if hurt != "minecraft:entity.wolf"+suffix+".hurt" {
			t.Fatalf("variant %d hurt = %q", i, hurt)
		}
		if death != "minecraft:entity.wolf"+suffix+".death" {
			t.Fatalf("variant %d death = %q", i, death)
		}
		if ambient != "minecraft:entity.wolf"+suffix+".ambient" {
			t.Fatalf("variant %d ambient = %q", i, ambient)
		}
	}

	// An angry wolf growls, whatever else it would have said.
	angry := &mob{etype: entityWolf, soundSet: 4, targetEID: 7}
	for i := 0; i < 20; i++ {
		if got := h.wolfAmbient(angry); got != "minecraft:entity.wolf_grumpy.growl" {
			t.Fatalf("an angry wolf said %q", got)
		}
	}

	// A hurt tamed wolf whines where a healthy one pants — never both.
	hurtPet := &mob{etype: entityWolf, tamed: true, health: 5}
	healthy := &mob{etype: entityWolf, tamed: true, health: wolfTamedHealth}
	sawWhine, sawPant, sawAmbient := false, false, false
	for i := 0; i < 200; i++ {
		switch h.wolfAmbient(hurtPet) {
		case "minecraft:entity.wolf.whine":
			sawWhine = true
		case "minecraft:entity.wolf.ambient":
			sawAmbient = true
		case "minecraft:entity.wolf.pant":
			t.Fatal("a hurt tamed wolf panted instead of whining")
		}
		if h.wolfAmbient(healthy) == "minecraft:entity.wolf.pant" {
			sawPant = true
		}
	}
	if !sawWhine || !sawPant || !sawAmbient {
		t.Fatalf("idle roll never produced all three (whine=%v pant=%v ambient=%v)", sawWhine, sawPant, sawAmbient)
	}
}
