package anvil

import "math/bits"

// DataVersion1_21_11 is the world DataVersion of Minecraft 1.21.11 (from the
// server jar's version.json), the engine's canonical version.
const DataVersion1_21_11 = 4671

// pack packs values little-endian-within-long at the given bit width, no
// value straddling longs — the 1.16+ Anvil packing.
func pack(values []uint32, width int) []uint64 {
	vpl := 64 / width
	out := make([]uint64, (len(values)+vpl-1)/vpl)
	for i, v := range values {
		out[i/vpl] |= uint64(v) << ((i % vpl) * width)
	}
	return out
}

// nibbles folds 4096 per-block light levels into the 2048-byte half-byte
// array (even index in the low nibble). Returns nil if every level is zero.
func nibbles(levels *[4096]uint8) []byte {
	any := false
	for _, l := range levels {
		if l != 0 {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	out := make([]byte, 2048)
	for i, l := range levels {
		out[i/2] |= (l & 0x0f) << ((i % 2) * 4)
	}
	return out
}

// appendBlockStates appends the block_states compound for one section:
// a palette of distinct states (as name+properties entries) plus the packed
// index array (omitted when the palette is a single state).
func appendBlockStates(b []byte, sec *[4096]uint32) []byte {
	palette := []uint32{}
	index := map[uint32]uint32{}
	idx := make([]uint32, 4096)
	for i, s := range sec {
		j, ok := index[s]
		if !ok {
			j = uint32(len(palette))
			index[s] = j
			palette = append(palette, s)
		}
		idx[i] = j
	}

	b = nbtCompound(b, "block_states")
	b = nbtList(b, "palette", tagCompound, len(palette))
	for _, s := range palette {
		e := decodeState(s)
		b = nbtString(b, "Name", e.name)
		if len(e.props) > 0 {
			b = nbtCompound(b, "Properties")
			for _, p := range e.props {
				b = nbtString(b, p[0], p[1])
			}
			b = end(b)
		}
		b = end(b) // list elements are bare compounds
	}
	if len(palette) > 1 {
		width := bits.Len(uint(len(palette) - 1))
		if width < 4 {
			width = 4
		}
		b = nbtLongArray(b, "data", pack(idx, width))
	}
	return end(b)
}

// ChunkNBT composes one full Anvil chunk (uncompressed NBT, named root).
// secs/biomes/sky/blk are bottom-up per section; hm is the highest non-air
// world-Y per column (index lz*16+lx, minY-1 for all-air); sky/blk may be
// nil to skip light.
func ChunkNBT(cx, cz int32, minY int, secs [][4096]uint32, biomes []string,
	hm [256]int16, sky, blk [][4096]uint8) []byte {

	b := nbtCompound(nil, "")
	b = nbtInt(b, "DataVersion", DataVersion1_21_11)
	b = nbtInt(b, "xPos", cx)
	b = nbtInt(b, "yPos", int32(minY>>4))
	b = nbtInt(b, "zPos", cz)
	b = nbtString(b, "Status", "minecraft:full")
	b = nbtLong(b, "LastUpdate", 0)
	b = nbtLong(b, "InhabitedTime", 0)
	b = nbtByte(b, "isLightOn", 1)

	b = nbtList(b, "sections", tagCompound, len(secs))
	for s := range secs {
		b = nbtByte(b, "Y", int8(minY>>4+s))
		b = appendBlockStates(b, &secs[s])
		b = nbtCompound(b, "biomes")
		biome := "minecraft:plains"
		if s < len(biomes) && biomes[s] != "" {
			biome = biomes[s]
		}
		b = nbtList(b, "palette", tagString, 1)
		b = str(b, biome)
		b = end(b)
		if s < len(sky) {
			if n := nibbles(&sky[s]); n != nil {
				b = nbtByteArray(b, "SkyLight", n)
			}
		}
		if s < len(blk) {
			if n := nibbles(&blk[s]); n != nil {
				b = nbtByteArray(b, "BlockLight", n)
			}
		}
		b = end(b)
	}

	// Both vanilla surface heightmaps get our single "highest non-air"
	// column map — close enough for renderers.
	heights := make([]uint32, 256)
	for i, h := range hm {
		if int(h) >= minY {
			heights[i] = uint32(int(h) - minY + 1)
		}
	}
	width := bits.Len(uint(len(secs)*16 + 1))
	packed := pack(heights, width)
	b = nbtCompound(b, "Heightmaps")
	b = nbtLongArray(b, "MOTION_BLOCKING", packed)
	b = nbtLongArray(b, "WORLD_SURFACE", packed)
	b = end(b)

	return end(b)
}
