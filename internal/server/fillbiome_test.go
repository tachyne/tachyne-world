package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /fillbiome through the dispatcher: the quarts in the box change and are
// counted, the viewers are sent the chunk's section biomes, a replace filter
// (a biome or a tag) limits it, and the volume cap and unknown ids refuse.
func TestCommandFillBiome(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		"fillbiome 0 64 0 15 79 15 minecraft:the_void",
		"fillbiome 0 64 0 15 79 15 minecraft:plains replace minecraft:desert",
		"fillbiome 0 64 0 15 79 15 minecraft:plains replace #minecraft:is_overworld",
		"fillbiome 0 0 0 40 40 40 minecraft:desert",
		"fillbiome 0 64 0 1 64 1 minecraft:nope",
		"fillbiome 0 64 0 1 64 1 minecraft:desert replace #minecraft:nope",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "B1")
	a := linesBetween(logs["alice"], "", "B1")
	for _, want := range []string{
		"64 biome entry/entries set between 0, 64, 0 and 12, 76, 12",
		"No biome entries were changed",
		"Too many blocks in the specified volume (maximum 32768, but specified 68921)",
		"Can't find element 'minecraft:nope' of type 'minecraft:worldgen/biome'",
		"Can't find tag 'minecraft:nope' of type 'minecraft:worldgen/biome'",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	if n := strings.Count(strings.Join(a, "\n"), "No biome entries were changed"); n != 2 {
		t.Errorf("%d unchanged replies, want 2 (a desert filter and an is_overworld one over the void)", n)
	}
	sec := (70 - worldgen.MinY) / 16
	onHub(t, h, func() {
		if got := h.world.BiomeAt3D(5, 70, 5); got != "minecraft:the_void" {
			t.Errorf("biome at 5,70,5 = %q", got)
		}
		if got := h.world.BiomeAt3D(5, 90, 5); got == "minecraft:the_void" {
			t.Error("the fill reached above the box")
		}
	})
	logs["alice"].mu.Lock()
	frames := append([]attachproto.ChunksBiomes(nil), logs["alice"].biomes...)
	logs["alice"].mu.Unlock()
	sent := false
	for _, f := range frames {
		for _, c := range f.Chunks {
			if c.CX == 0 && c.CZ == 0 && sec < len(c.Biomes) && c.Biomes[sec] == "minecraft:the_void" {
				sent = true
			}
		}
	}
	if !sent {
		t.Errorf("no chunks-biomes frame showed chunk 0,0 as the void: %+v", frames)
	}

	// Back to plains from the void only: all 64 change again.
	s.handleCommand(alice, "fillbiome 0 64 0 15 79 15 minecraft:plains replace minecraft:the_void")
	settle(t, h, logs, "B2")
	if b := linesBetween(logs["alice"], "B1", "B2"); !hasLine(b, "64 biome entry/entries set between 0, 64, 0 and 12, 76, 12") {
		t.Errorf("replace by biome: %q", b)
	}
}
