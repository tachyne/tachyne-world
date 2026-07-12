package anvil

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func propsMap(e stateEntry) map[string]string {
	m := map[string]string{}
	for _, p := range e.props {
		m[p[0]] = p[1]
	}
	return m
}

// Values verified against the vanilla datagen report (blocks.json 1.21.11).
func TestDecodeState(t *testing.T) {
	if e := decodeState(0); e.name != "minecraft:air" || len(e.props) != 0 {
		t.Fatalf("state 0 = %+v", e)
	}
	if e := decodeState(1); e.name != "minecraft:stone" {
		t.Fatalf("state 1 = %+v", e)
	}
	// oak_stairs 3706 = facing north, half top, shape straight, waterlogged true.
	e := decodeState(3706)
	if e.name != "minecraft:oak_stairs" {
		t.Fatalf("state 3706 = %+v", e)
	}
	m := propsMap(e)
	if m["facing"] != "north" || m["half"] != "top" || m["shape"] != "straight" || m["waterlogged"] != "true" {
		t.Fatalf("oak_stairs props %v", m)
	}
	// chest 3790 = facing north, type right, waterlogged true (alphabetical
	// enumeration — the property order in the block definition differs).
	m = propsMap(decodeState(3790))
	if m["facing"] != "north" || m["type"] != "right" || m["waterlogged"] != "true" {
		t.Fatalf("chest props %v", m)
	}
	// Beyond the table: air, not a panic.
	last := blockDefs[len(blockDefs)-1]
	if e := decodeState(last.base + 100000); e.name != "minecraft:air" {
		t.Fatalf("out of range = %+v", e)
	}
}

func TestPackNoStraddle(t *testing.T) {
	vals := make([]uint32, 4096)
	for i := range vals {
		vals[i] = uint32(i % 30) // needs 5 bits
	}
	packed := pack(vals, 5)
	if want := (4096 + 11) / 12; len(packed) != want { // 12 values per long
		t.Fatalf("len %d want %d", len(packed), want)
	}
	for i, v := range vals {
		got := (packed[i/12] >> ((i % 12) * 5)) & 0x1f
		if got != uint64(v) {
			t.Fatalf("value %d: got %d want %d", i, got, v)
		}
	}
	// Top 4 bits of each long unused (12*5=60).
	for i, l := range packed {
		if l>>60 != 0 {
			t.Fatalf("long %d straddles: %x", i, l)
		}
	}
}

// --- minimal disk-NBT reader (test oracle) ---

type nbtReader struct {
	*bytes.Reader
	t *testing.T
}

func (r *nbtReader) u8() byte {
	b, err := r.ReadByte()
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

func (r *nbtReader) u16() uint16 {
	var v uint16
	if err := binary.Read(r, binary.BigEndian, &v); err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *nbtReader) str() string {
	n := r.u16()
	buf := make([]byte, n)
	io.ReadFull(r, buf)
	return string(buf)
}

// value reads one tag payload of the given type into a Go value.
func (r *nbtReader) value(typ byte) any {
	switch typ {
	case tagByte:
		return int8(r.u8())
	case tagShort:
		var v int16
		binary.Read(r, binary.BigEndian, &v)
		return v
	case tagInt:
		var v int32
		binary.Read(r, binary.BigEndian, &v)
		return v
	case tagLong:
		var v int64
		binary.Read(r, binary.BigEndian, &v)
		return v
	case tagFloat:
		var v float32
		binary.Read(r, binary.BigEndian, &v)
		return v
	case tagDouble:
		var v float64
		binary.Read(r, binary.BigEndian, &v)
		return v
	case tagString:
		return r.str()
	case tagByteArray:
		var n int32
		binary.Read(r, binary.BigEndian, &n)
		buf := make([]byte, n)
		io.ReadFull(r, buf)
		return buf
	case tagIntArray:
		var n int32
		binary.Read(r, binary.BigEndian, &n)
		out := make([]int32, n)
		binary.Read(r, binary.BigEndian, &out)
		return out
	case tagLongArray:
		var n int32
		binary.Read(r, binary.BigEndian, &n)
		out := make([]uint64, n)
		binary.Read(r, binary.BigEndian, &out)
		return out
	case tagList:
		elem := r.u8()
		var n int32
		binary.Read(r, binary.BigEndian, &n)
		out := make([]any, n)
		for i := range out {
			out[i] = r.value(elem)
		}
		return out
	case tagCompound:
		m := map[string]any{}
		for {
			t := r.u8()
			if t == tagEnd {
				return m
			}
			name := r.str()
			m[name] = r.value(t)
		}
	default:
		r.t.Fatalf("unexpected tag type %d", typ)
		return nil
	}
}

// parseNBT parses a named-root compound document.
func parseNBT(t *testing.T, raw []byte) map[string]any {
	r := &nbtReader{bytes.NewReader(raw), t}
	if typ := r.u8(); typ != tagCompound {
		t.Fatalf("root tag %d", typ)
	}
	r.str() // root name
	m := r.value(tagCompound).(map[string]any)
	if r.Len() != 0 {
		t.Fatalf("%d trailing bytes", r.Len())
	}
	return m
}

func TestChunkNBTReparse(t *testing.T) {
	// Two sections: bottom = solid stone with one oak-stairs state, top = air.
	secs := make([][4096]uint32, 2)
	for i := range secs[0] {
		secs[0][i] = 1 // stone
	}
	secs[0][0] = 3706 // oak_stairs at local (0, 0, 0)
	biomes := []string{"minecraft:desert", "minecraft:plains"}
	var hm [256]int16
	for i := range hm {
		hm[i] = int16(worldgen.MinY + 15)
	}
	sky := make([][4096]uint8, 2)
	blk := make([][4096]uint8, 2)
	for i := range sky[1] {
		sky[1][i] = 15
	}
	blk[0][5] = 13

	root := parseNBT(t, ChunkNBT(-3, 7, worldgen.MinY, secs, biomes, hm, sky, blk))

	if v := root["DataVersion"].(int32); v != DataVersion1_21_11 {
		t.Fatalf("DataVersion %d", v)
	}
	if root["xPos"].(int32) != -3 || root["zPos"].(int32) != 7 || root["yPos"].(int32) != -4 {
		t.Fatalf("pos %v %v %v", root["xPos"], root["zPos"], root["yPos"])
	}
	if root["Status"].(string) != "minecraft:full" {
		t.Fatalf("status %v", root["Status"])
	}

	sections := root["sections"].([]any)
	if len(sections) != 2 {
		t.Fatalf("%d sections", len(sections))
	}
	s0 := sections[0].(map[string]any)
	if y := s0["Y"].(int8); y != -4 {
		t.Fatalf("section 0 Y=%d", y)
	}
	bs := s0["block_states"].(map[string]any)
	pal := bs["palette"].([]any)
	if len(pal) != 2 {
		t.Fatalf("palette size %d", len(pal))
	}
	// Palette is first-encountered order: block 0 is the stairs.
	p0 := pal[0].(map[string]any)
	if p0["Name"].(string) != "minecraft:oak_stairs" {
		t.Fatalf("palette[0] %v", p0)
	}
	props := p0["Properties"].(map[string]any)
	if props["facing"].(string) != "north" || props["waterlogged"].(string) != "true" {
		t.Fatalf("stairs properties %v", props)
	}
	if p1 := pal[1].(map[string]any); p1["Name"].(string) != "minecraft:stone" {
		t.Fatalf("palette[1] %v", p1)
	}
	data := bs["data"].([]uint64)
	if want := 4096 / 16; len(data) != want { // 2 states -> 4 bits -> 16/long
		t.Fatalf("data longs %d want %d", len(data), want)
	}
	if data[0]&0xf != 0 { // block 0 is the stairs (palette index 0)
		t.Fatalf("block 0 index %d", data[0]&0xf)
	}
	if (data[0]>>4)&0xf != 1 { // block 1 is stone
		t.Fatalf("block 1 index %d", (data[0]>>4)&0xf)
	}
	biome := s0["biomes"].(map[string]any)["palette"].([]any)
	if biome[0].(string) != "minecraft:desert" {
		t.Fatalf("biome %v", biome)
	}
	if _, ok := s0["SkyLight"]; ok {
		t.Fatal("all-zero SkyLight should be omitted")
	}
	bl := s0["BlockLight"].([]byte)
	if bl[2] != 0xd0 { // level 13 at odd index 5 -> high nibble of byte 2
		t.Fatalf("BlockLight byte 2 = %#x", bl[2])
	}

	s1 := sections[1].(map[string]any)
	bs1 := s1["block_states"].(map[string]any)
	if _, ok := bs1["data"]; ok {
		t.Fatal("single-palette section should omit data")
	}
	if sl := s1["SkyLight"].([]byte); sl[0] != 0xff {
		t.Fatalf("SkyLight %#x", sl[0])
	}

	hms := root["Heightmaps"].(map[string]any)
	mb := hms["MOTION_BLOCKING"].([]uint64)
	// 2 sections -> 33 values -> 6 bits -> 10 per long -> 26 longs for 256.
	if len(mb) != 26 {
		t.Fatalf("heightmap longs %d", len(mb))
	}
	if v := mb[0] & 0x3f; v != 16 { // (MinY+15) - MinY + 1
		t.Fatalf("heightmap[0] = %d", v)
	}
}

func TestRegionRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.0.0.mca")
	chunkA := ChunkNBT(0, 0, worldgen.MinY, make([][4096]uint32, 1), []string{"minecraft:plains"}, [256]int16{}, nil, nil)
	chunkB := ChunkNBT(31, 31, worldgen.MinY, make([][4096]uint32, 1), []string{"minecraft:plains"}, [256]int16{}, nil, nil)
	err := WriteRegion(path, map[[2]int][]byte{
		{0, 0}:   chunkA,
		{31, 31}: chunkB,
	}, map[[2]int]uint32{{0, 0}: 12345, {31, 31}: 12345})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%sectorSize != 0 {
		t.Fatalf("file size %d not sector-aligned", len(raw))
	}

	readChunk := func(lx, lz int) []byte {
		i := 4 * (lz*32 + lx)
		off := int(raw[i])<<16 | int(raw[i+1])<<8 | int(raw[i+2])
		count := int(raw[i+3])
		if off < 2 || count == 0 {
			t.Fatalf("chunk %d,%d: bad location %d/%d", lx, lz, off, count)
		}
		ts := binary.BigEndian.Uint32(raw[sectorSize+i:])
		if ts != 12345 {
			t.Fatalf("timestamp %d", ts)
		}
		sec := raw[off*sectorSize:]
		n := binary.BigEndian.Uint32(sec)
		if sec[4] != 2 {
			t.Fatalf("compression scheme %d", sec[4])
		}
		zr, err := zlib.NewReader(bytes.NewReader(sec[5 : 4+n]))
		if err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	if !bytes.Equal(readChunk(0, 0), chunkA) {
		t.Fatal("chunk 0,0 roundtrip mismatch")
	}
	if !bytes.Equal(readChunk(31, 31), chunkB) {
		t.Fatal("chunk 31,31 roundtrip mismatch")
	}
	// Empty slots stay empty.
	i := 4 * (5*32 + 5)
	if raw[i] != 0 || raw[i+3] != 0 {
		t.Fatal("empty slot has a location")
	}
	// Both chunks re-parse as NBT.
	parseNBT(t, readChunk(0, 0))
}
