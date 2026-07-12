// Package anvil writes tachyne worlds out in the vanilla Anvil save format
// (region .mca files + level.dat), so external tools that read vanilla
// worlds — map renderers like BlueMap, most notably — can consume them.
//
// This is DISK NBT (every tag is named, the root compound carries a name),
// not the nameless-root network NBT the wire layer speaks; the two formats
// share tag ids but not framing, which is why these writers live here and
// not in tachyne-common.
package anvil

import "math"

// NBT tag ids (disk format).
const (
	tagEnd       = 0
	tagByte      = 1
	tagShort     = 2
	tagInt       = 3
	tagLong      = 4
	tagFloat     = 5
	tagDouble    = 6
	tagByteArray = 7
	tagString    = 8
	tagList      = 9
	tagCompound  = 10
	tagIntArray  = 11
	tagLongArray = 12
)

func be16(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }

func be32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func be64(b []byte, v uint64) []byte {
	return append(b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// header appends a named-tag header: type id + u16 name length + name.
func header(b []byte, typ byte, name string) []byte {
	b = append(b, typ)
	b = be16(b, uint16(len(name)))
	return append(b, name...)
}

func nbtByte(b []byte, name string, v int8) []byte {
	b = header(b, tagByte, name)
	return append(b, byte(v))
}

func nbtInt(b []byte, name string, v int32) []byte {
	b = header(b, tagInt, name)
	return be32(b, uint32(v))
}

func nbtLong(b []byte, name string, v int64) []byte {
	b = header(b, tagLong, name)
	return be64(b, uint64(v))
}

func nbtFloat(b []byte, name string, v float32) []byte {
	b = header(b, tagFloat, name)
	return be32(b, math.Float32bits(v))
}

func nbtString(b []byte, name, v string) []byte {
	b = header(b, tagString, name)
	b = be16(b, uint16(len(v)))
	return append(b, v...)
}

// str appends a bare (unnamed) string payload — for list elements.
func str(b []byte, v string) []byte {
	b = be16(b, uint16(len(v)))
	return append(b, v...)
}

func nbtByteArray(b []byte, name string, v []byte) []byte {
	b = header(b, tagByteArray, name)
	b = be32(b, uint32(len(v)))
	return append(b, v...)
}

func nbtLongArray(b []byte, name string, v []uint64) []byte {
	b = header(b, tagLongArray, name)
	b = be32(b, uint32(len(v)))
	for _, l := range v {
		b = be64(b, l)
	}
	return b
}

// nbtCompound opens a named compound; the caller appends its contents and
// closes with end(b).
func nbtCompound(b []byte, name string) []byte { return header(b, tagCompound, name) }

func end(b []byte) []byte { return append(b, tagEnd) }

// nbtList opens a named list of count elements of elemType; the caller
// appends the bare (unnamed) element payloads.
func nbtList(b []byte, name string, elemType byte, count int) []byte {
	b = header(b, tagList, name)
	b = append(b, elemType)
	return be32(b, uint32(count))
}
