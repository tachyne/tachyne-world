package server

import (
	"fmt"
	"math"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /locate structure <id>: vanilla's LocateCommand for structures — the
// nearest site of the named structure within 100 chunks of the player,
// reported the way vanilla words it. Operators only (permission level 2).
// /locate biome <id> searches the way BiomeSource.findClosestBiome3d does.
// The point-of-interest form is not offered.

const locateRadius = 100 * 16

func (s *Server) cmdLocate(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) == 2 && args[0] == "biome" {
		s.locateBiome(p, args[1])
		return
	}
	if len(args) > 0 && args[0] == "structure" {
		args = args[1:]
	}
	if len(args) != 1 {
		p.tell("Usage: /locate structure <id>  (" + strings.Join(worldgen.StructureNames(), ", ") + ")")
		return
	}
	id := strings.TrimPrefix(args[0], "minecraft:")
	dim, ok := worldgen.StructureDim(id)
	if !ok {
		p.tell(fmt.Sprintf("There is no structure with type \"minecraft:%s\"", id))
		return
	}
	px, pz := int(math.Floor(p.x)), int(math.Floor(p.z))
	if dim != p.dim {
		p.tell(fmt.Sprintf("Could not find a structure of type \"minecraft:%s\" nearby", id))
		return
	}
	x, z, found := s.hub.worldFor(p.dim).Gen().LocateStructure(id, px, pz, locateRadius)
	if !found {
		p.tell(fmt.Sprintf("Could not find a structure of type \"minecraft:%s\" nearby", id))
		return
	}
	dx, dz := float64(x-px), float64(z-pz)
	dist := int(math.Floor(math.Sqrt(dx*dx + dz*dz)))
	s.info(p, fmt.Sprintf("The nearest minecraft:%s is at [%d, ~, %d] (%d blocks away)", id, x, z, dist))
}

// locateBiome is LocateCommand's biome form (findClosestBiome3d): columns in
// a square spiral out to 6400 blocks, 32 apart, and in each the heights 64
// apart, nearest the player's own first. It runs on the session's goroutine:
// a long search holds up nobody else.
func (s *Server) locateBiome(p *player, id string) {
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	const radius, stepXZ, stepY = 6400, 32, 64
	w := s.hub.worldFor(p.dim)
	g := w.Gen()
	ox, oy, oz := int(math.Floor(p.x)), int(math.Floor(p.y)), int(math.Floor(p.z))
	lo, hi := worldgen.MinY+1, worldgen.MinY+w.Ceiling()
	// Mth.outFromOrigin: the origin's height, then alternately above and below.
	var ys []int
	if oy >= lo && oy <= hi {
		ys = append(ys, oy)
	}
	for d := stepY; oy-d >= lo || oy+d <= hi; d += stepY {
		if oy+d <= hi {
			ys = append(ys, oy+d)
		}
		if oy-d >= lo {
			ys = append(ys, oy-d)
		}
	}
	try := func(dx, dz int) (int, int, int, bool) {
		x, z := ox+dx*stepXZ, oz+dz*stepXZ
		for _, y := range ys {
			if g.CaveBiomeAt(x, y, z) == id {
				return x, y, z, true
			}
		}
		return 0, 0, 0, false
	}
	n := radius / stepXZ
	x, y, z, found := try(0, 0)
	// BlockPos.spiralAround: ring by ring, east then south, west, north.
	for r := 1; r <= n && !found; r++ {
		for i := -r + 1; i <= r && !found; i++ {
			x, y, z, found = try(r, i)
		}
		for i := r - 1; i >= -r && !found; i-- {
			x, y, z, found = try(i, r)
		}
		for i := r - 1; i >= -r && !found; i-- {
			x, y, z, found = try(-r, i)
		}
		for i := -r + 1; i <= r && !found; i++ {
			x, y, z, found = try(i, -r)
		}
	}
	if !found {
		p.tell(fmt.Sprintf("Could not find a biome of type \"%s\" within reasonable distance", id))
		return
	}
	dx, dz := float64(x-ox), float64(z-oz)
	dist := int(math.Floor(math.Sqrt(dx*dx + dz*dz)))
	s.info(p, fmt.Sprintf("The nearest %s is at [%d, %d, %d] (%d blocks away)", id, x, y, z, dist))
}
