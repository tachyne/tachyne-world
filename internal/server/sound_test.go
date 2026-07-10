package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-common/render770"
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
