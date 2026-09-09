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
// Biome and point-of-interest forms are not offered.

const locateRadius = 100 * 16

func (s *Server) cmdLocate(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
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
	p.tell(fmt.Sprintf("The nearest minecraft:%s is at [%d, ~, %d] (%d blocks away)", id, x, z, dist))
}
