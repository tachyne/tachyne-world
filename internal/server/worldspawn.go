package server

import (
	"log"
	"math"
	"strings"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A new world's spawn (MinecraftServer.setInitialSpawn). Vanilla picks it
// in two steps:
//
//  1. The generator's origin (NoiseBasedChunkGenerator.getOrigin →
//     NoiseSpawnFinder): the overworld's spawn_target asks for land
//     (continentalness from -0.11 up) away from a river valley (|ridges| at
//     least 0.16). Each candidate column is scored by how far its climate
//     misses the target, times 2048², plus its squared distance from 0,0 —
//     so the nearest fitting column wins — over a coarse radial search out
//     to 2048 blocks and then a fine one around the best.
//  2. A safe surface near it (PlayerSpawnFinder.getSpawnPosInChunk): the
//     origin's chunk, then the chunks spiralling out from it to ±5, each
//     scanned column by column for the first place a player stands on a
//     full top face with no fluid over it, per the chunk's heightmaps.
//
// A native world's climate is its own (worldgen/biomes.go), not vanilla's
// multi-noise router, so step 1 scores its resolved biome instead: an
// ocean misses the target by how deep it is, a river by a fixed step; a
// vanilla world scores its noise settings' spawn targets on the router
// itself. The search, the scoring and step 2 are vanilla's.
//
// It runs only for a NEW world: no spawn saved, none on the command line,
// and not one edit in any dimension. An existing world's spawn never moves.

const (
	spawnFinderMaxRadius = 2048 // NoiseSpawnFinder.MAX_RADIUS
	spawnChunkSpiral     = 11   // setInitialSpawn: Mth.square(11) chunks, ±5
)

// spawnFitness is SpawnTargetPoint.sampleFitness in the engine's climate:
// 0 where the column meets the target (dry land, no river), else how far
// it misses, in the quantised units vanilla squares.
func spawnFitness(w *world.World, x, z int) int64 {
	if f, ok := w.Gen().VanillaSpawnFitness(x, z); ok {
		return f // a vanilla world: the spawn targets on vanilla's own climate
	}
	name := w.BiomeAt(x, z)
	switch {
	case strings.HasSuffix(name, "river"):
		return 1600 * 1600 // |ridges| under 0.16: 0.16 short, quantised ×10000
	case strings.Contains(name, "ocean"):
		d := int64(worldgen.SeaLevel-w.GroundY(x, z)) * 100 // deeper misses by more
		if d < 1100 {
			d = 1100 // continentalness below -0.11: at least that far off
		}
		return d * d
	}
	return 0
}

// spawnCandidate is NoiseSpawnFinder.getSpawnPositionAndFitness: the
// column's fitness, sampled at its quart, weighted far above the squared
// distance from the world's origin.
func spawnCandidate(w *world.World, x, z int) (fitness int64) {
	qx, qz := x>>2<<2, z>>2<<2
	return spawnFitness(w, qx, qz)*spawnFinderMaxRadius*spawnFinderMaxRadius + int64(x)*int64(x) + int64(z)*int64(z)
}

// noiseSpawnOrigin is NoiseSpawnFinder.findSpawnPosition: the best of 0,0,
// a coarse radial search out to 2048 in steps of 512, and a fine one out
// to 512 in steps of 32 around the best so far.
func noiseSpawnOrigin(w *world.World) (x, z int) {
	bestF := spawnCandidate(w, 0, 0)
	radial := func(maxRadius, step float32) {
		ox, oz := x, z
		angle, radius := float32(0), step
		for radius <= maxRadius {
			cx := ox + int(math.Sin(float64(angle))*float64(radius))
			cz := oz + int(math.Cos(float64(angle))*float64(radius))
			if f := spawnCandidate(w, cx, cz); f < bestF {
				bestF, x, z = f, cx, cz
			}
			angle += step / radius
			if angle > math.Pi*2 {
				angle = 0
				radius += step
			}
		}
	}
	radial(2048, 512)
	radial(512, 32)
	return x, z
}

// levelRespawnPos is PlayerSpawnFinder.getLevelRespawnPos for the
// overworld: the top of the column's MOTION_BLOCKING heightmap, refused
// where the column is topped by a fluid (WORLD_SURFACE there, above the
// OCEAN_FLOOR), then the first full top face going down with no fluid on
// the way.
func levelRespawnPos(w *world.World, x, z int) (blockPos, bool) {
	topY := w.HeightAt(world.MotionBlocking, x, z)
	if topY < worldgen.MinY {
		return blockPos{}, false
	}
	surface := w.HeightAt(world.WorldSurface, x, z)
	if surface <= topY && surface > w.HeightAt(world.OceanFloor, x, z) {
		return blockPos{}, false
	}
	for y := topY + 1; y >= worldgen.MinY; y-- {
		st := w.At(x, y, z)
		if worldgen.HoldsWater(st) || worldgen.IsLava(st) {
			break
		}
		if worldgen.IsSturdyTop(st) {
			return blockPos{x, y + 1, z}, true
		}
	}
	return blockPos{}, false
}

// spawnPosInChunk is PlayerSpawnFinder.getSpawnPosInChunk: the first
// column, x-major, that gives a respawn position.
func spawnPosInChunk(w *world.World, cx, cz int) (blockPos, bool) {
	w.Reader(int32(cx), int32(cz)) // load it: the heightmaps are the chunk's
	for x := cx * 16; x < cx*16+16; x++ {
		for z := cz * 16; z < cz*16+16; z++ {
			if p, ok := levelRespawnPos(w, x, z); ok {
				return p, true
			}
		}
	}
	return blockPos{}, false
}

// initialWorldSpawn is setInitialSpawn's choice for an overworld: the
// generator's origin chunk at y 64 (getSpawnHeight), replaced by the first
// safe surface in the ±5 chunk spiral around it.
func initialWorldSpawn(w *world.World) blockPos {
	ox, oz := noiseSpawnOrigin(w)
	scx, scz := ox>>4, oz>>4
	spawn := blockPos{scx*16 + 8, 64, scz*16 + 8}
	xo, zo, dx, dz := 0, 0, 0, -1
	for i := 0; i < spawnChunkSpiral*spawnChunkSpiral; i++ {
		if xo >= -5 && xo <= 5 && zo >= -5 && zo <= 5 {
			if p, ok := spawnPosInChunk(w, scx+xo, scz+zo); ok {
				return p
			}
		}
		if xo == zo || xo < 0 && xo == -zo || xo > 0 && xo == 1-zo {
			dx, dz = -dz, dx
		}
		xo += dx
		zo += dz
	}
	// Nothing safe in 121 chunks: vanilla keeps y 64 there and lets the
	// joining player's spawn search fix the height; the engine stands the
	// point on the column's surface.
	spawn.y = int(w.SurfaceY(spawn.x, spawn.z))
	return spawn
}

// newWorldSpawn picks and saves a new world's spawn, when this is one:
// nothing saved, nothing on the command line, not sharded, and no edits
// anywhere. Reports whether it did.
func (s *Server) newWorldSpawn() bool {
	if s.world == nil || s.hub == nil || s.SpawnSet || s.Sharded || s.hub.rules.WorldSpawn != nil {
		return false
	}
	for _, w := range s.allDims() {
		if w.EditCount() > 0 {
			return false // an existing world: its spawn stays where it is
		}
	}
	p := initialWorldSpawn(s.world)
	sp := worldSpawnSave{X: p.x, Y: p.y, Z: p.z, Dim: dimOverworld}
	s.hub.rules.WorldSpawn = &sp
	s.hub.saveRules()
	s.restoreWorldSpawn()
	log.Printf("new world: spawn chosen at (%d, %d, %d)", p.x, p.y, p.z)
	return true
}
