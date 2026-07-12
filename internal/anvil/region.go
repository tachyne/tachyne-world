package anvil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
)

const sectorSize = 4096

// WriteRegion writes one region file (32×32 chunks) at path. chunks maps
// LOCAL chunk coords (0–31) to uncompressed chunk NBT; absent entries are
// simply not present in the region (vanilla semantics). stamps carries each
// chunk's modify time (epoch seconds) for the header — renderers use it to
// skip unchanged chunks, so incremental exports keep old stamps for
// unchanged content.
func WriteRegion(path string, chunks map[[2]int][]byte, stamps map[[2]int]uint32) error {
	var locations [sectorSize]byte
	var timestamps [sectorSize]byte
	body := &bytes.Buffer{}

	sector := uint32(2) // header occupies sectors 0 and 1
	for lz := 0; lz < 32; lz++ {
		for lx := 0; lx < 32; lx++ {
			raw, ok := chunks[[2]int{lx, lz}]
			if !ok {
				continue
			}
			timestamp := stamps[[2]int{lx, lz}]
			var cbuf bytes.Buffer
			zw := zlib.NewWriter(&cbuf)
			zw.Write(raw)
			zw.Close()

			// Payload: u32 length (compression byte + data), u8 scheme (2 =
			// zlib), data; padded to whole sectors.
			payload := make([]byte, 0, cbuf.Len()+5)
			payload = be32(payload, uint32(cbuf.Len()+1))
			payload = append(payload, 2)
			payload = append(payload, cbuf.Bytes()...)
			sectors := (len(payload) + sectorSize - 1) / sectorSize
			if sectors > 255 {
				return fmt.Errorf("chunk %d,%d: %d sectors exceeds region limit", lx, lz, sectors)
			}
			payload = append(payload, make([]byte, sectors*sectorSize-len(payload))...)

			i := 4 * (lz*32 + lx)
			locations[i] = byte(sector >> 16)
			locations[i+1] = byte(sector >> 8)
			locations[i+2] = byte(sector)
			locations[i+3] = byte(sectors)
			timestamps[i] = byte(timestamp >> 24)
			timestamps[i+1] = byte(timestamp >> 16)
			timestamps[i+2] = byte(timestamp >> 8)
			timestamps[i+3] = byte(timestamp)

			body.Write(payload)
			sector += uint32(sectors)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(locations[:]); err != nil {
		return err
	}
	if _, err := f.Write(timestamps[:]); err != nil {
		return err
	}
	_, err = f.Write(body.Bytes())
	return err
}

// WriteLevelDat writes a minimal level.dat (gzip NBT) that marks the save as
// a fully-initialized 1.21.11 world with the given name and spawn.
func WriteLevelDat(path, name string, spawnX, spawnY, spawnZ int32) error {
	b := nbtCompound(nil, "")
	b = nbtCompound(b, "Data")
	b = nbtInt(b, "DataVersion", DataVersion1_21_11)
	b = nbtInt(b, "version", 19133) // Anvil format marker
	b = nbtCompound(b, "Version")
	b = nbtInt(b, "Id", DataVersion1_21_11)
	b = nbtString(b, "Name", "1.21.11")
	b = nbtString(b, "Series", "main")
	b = nbtByte(b, "Snapshot", 0)
	b = end(b)
	b = nbtString(b, "LevelName", name)
	b = nbtByte(b, "initialized", 1)
	b = nbtInt(b, "GameType", 0)
	b = nbtByte(b, "Difficulty", 2)
	b = nbtByte(b, "hardcore", 0)
	b = nbtLong(b, "Time", 0)
	b = nbtLong(b, "DayTime", 6000)
	b = nbtLong(b, "LastPlayed", int64(0))
	b = nbtInt(b, "SpawnX", spawnX)
	b = nbtInt(b, "SpawnY", spawnY)
	b = nbtInt(b, "SpawnZ", spawnZ)
	b = nbtFloat(b, "SpawnAngle", 0)
	b = end(b)
	b = end(b)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	if _, err := gw.Write(b); err != nil {
		return err
	}
	return gw.Close()
}
