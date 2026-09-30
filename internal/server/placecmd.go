package server

import (
	"fmt"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /place feature|jigsaw|structure|template (PlaceCommand). The generator lays
// the blocks out (worldgen/placecmd.go) and they go into the live world as
// edits: StructureTemplate.placeInWorld with UPDATE_CLIENTS, so the viewers
// see them and no neighbour updates run. Features are the trees and huge
// mushrooms the engine grows; structures are the jigsaw-built ones and the
// igloo; jigsaws and templates are every pool and template the engine has.

const placeUsage = "Usage: /place feature <feature> [<pos>] | jigsaw <pool> <target> <max_depth> [<position>] | " +
	"structure <structure> [<pos>] | template <template> [<pos> [<rotation> [<mirror> [<integrity> [<seed> [strict]]]]]]"

// placeReq is one parsed /place.
type placeReq struct {
	by        int32
	kind      string // feature, jigsaw, structure, template
	id        string // the feature, pool, structure or template (namespaced)
	target    string // jigsaw: the start jigsaw's name
	depth     int    // jigsaw: max_depth
	pos       blockPos
	rot, mir  int
	integrity float32
	seed      int64
}

// vanillaStructures are the structure ids vanilla's registry holds, so an id
// the engine cannot start is told apart from one that does not exist.
var vanillaStructures = keySet(
	"abandoned_camp_bamboo_jungle", "abandoned_camp_birch_forest", "abandoned_camp_cherry_grove", "abandoned_camp_dappled_forest",
	"abandoned_camp_flower_forest", "abandoned_camp_forest", "abandoned_camp_meadow", "abandoned_camp_old_growth_birch_forest",
	"abandoned_camp_old_growth_pine_taiga", "abandoned_camp_old_growth_spruce_taiga", "abandoned_camp_pale_garden", "abandoned_camp_savanna",
	"abandoned_camp_snowy_taiga", "abandoned_camp_sparse_jungle", "abandoned_camp_swamp", "abandoned_camp_taiga",
	"abandoned_camp_windswept_forest", "abandoned_camp_wooded_badlands", "ancient_city", "bastion_remnant", "buried_treasure",
	"desert_pyramid", "end_city", "fortress", "igloo", "jungle_pyramid", "mansion", "mineshaft", "mineshaft_mesa", "monument",
	"nether_fossil", "ocean_ruin_cold", "ocean_ruin_warm", "pillager_outpost", "ruined_portal", "ruined_portal_desert",
	"ruined_portal_jungle", "ruined_portal_mountain", "ruined_portal_nether", "ruined_portal_ocean", "ruined_portal_swamp",
	"shipwreck", "shipwreck_beached", "stronghold", "swamp_hut", "trail_ruins", "trial_chambers", "village_desert",
	"village_plains", "village_savanna", "village_snowy", "village_taiga")

var templateRotations = map[string]int{"none": 0, "clockwise_90": 1, "180": 2, "counterclockwise_90": 3}
var templateMirrors = map[string]int{"none": worldgen.MirrorNone, "left_right": worldgen.MirrorLeftRight, "front_back": worldgen.MirrorFrontBack}

func parsePlace(p *player, args []string) (placeReq, string) {
	if len(args) < 2 {
		return placeReq{}, placeUsage
	}
	r := placeReq{by: p.eid, kind: args[0], integrity: 1,
		pos: blockPos{floorInt(p.x), floorInt(p.y), floorInt(p.z)}}
	id, ok := parseResourceID(args[1])
	if !ok {
		return placeReq{}, "Invalid ID: " + args[1]
	}
	r.id = id
	rest := args[2:]
	// readPos takes an optional block position at the start of rest.
	readPos := func() bool {
		if len(rest) == 0 {
			return true
		}
		if len(rest) < 3 {
			return false
		}
		x, y, z, ok := parsePosition(rest[:3], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			return false
		}
		r.pos = blockPos{floorInt(x), floorInt(y), floorInt(z)}
		rest = rest[3:]
		return true
	}
	switch r.kind {
	case "feature", "structure":
		if !readPos() || len(rest) != 0 {
			return placeReq{}, placeUsage
		}
	case "jigsaw":
		if len(rest) < 2 {
			return placeReq{}, placeUsage
		}
		target, ok := parseResourceID(rest[0])
		if !ok {
			return placeReq{}, "Invalid ID: " + rest[0]
		}
		n, err := strconv.Atoi(rest[1])
		if err != nil {
			return placeReq{}, "Expected integer"
		}
		if n < 1 || n > 20 {
			return placeReq{}, fmt.Sprintf("Integer must be between 1 and 20, found %d", n)
		}
		r.target, r.depth, rest = target, n, rest[2:]
		if !readPos() || len(rest) != 0 {
			return placeReq{}, placeUsage
		}
	case "template":
		if !readPos() {
			return placeReq{}, placeUsage
		}
		if len(rest) > 0 {
			rot, ok := templateRotations[rest[0]]
			if !ok {
				return placeReq{}, "Invalid rotation: " + rest[0]
			}
			r.rot, rest = rot, rest[1:]
		}
		if len(rest) > 0 {
			mir, ok := templateMirrors[rest[0]]
			if !ok {
				return placeReq{}, "Invalid mirror: " + rest[0]
			}
			r.mir, rest = mir, rest[1:]
		}
		if len(rest) > 0 {
			f, err := strconv.ParseFloat(rest[0], 32)
			if err != nil {
				return placeReq{}, "Expected float"
			}
			if f < 0 || f > 1 {
				return placeReq{}, fmt.Sprintf("Float must be between 0.0 and 1.0, found %s", jFloat(float32(f)))
			}
			r.integrity, rest = float32(f), rest[1:]
		}
		if len(rest) > 0 {
			n, err := strconv.ParseInt(rest[0], 10, 32)
			if err != nil {
				return placeReq{}, "Expected integer"
			}
			r.seed, rest = n, rest[1:]
		}
		// strict (UPDATE_KNOWN_SHAPE): the placement already runs no shape
		// or neighbour updates, so it changes nothing here.
		if len(rest) == 1 && rest[0] == "strict" {
			rest = rest[1:]
		}
		if len(rest) != 0 {
			return placeReq{}, placeUsage
		}
	default:
		return placeReq{}, placeUsage
	}
	return r, ""
}

func (s *Server) cmdPlace(p *player, args []string) {
	if !s.isOp(p.name) { // PlaceCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	r, msg := parsePlace(p, args)
	if msg != "" {
		p.tell(msg)
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		if msg := s.hub.runPlace(players, r); msg != "" {
			cmdFail(p, msg)
		}
	})
}

// runPlace executes a parsed /place on the hub; a non-empty string is the
// failure line.
func (h *hub) runPlace(players map[int32]*tracked, r placeReq) string {
	t := players[r.by]
	if t == nil {
		return ""
	}
	dim := t.dim
	w := h.worldFor(dim)
	if w == nil {
		return "That position is not loaded"
	}
	name := strings.TrimPrefix(r.id, "minecraft:")
	at := fmt.Sprintf("%d, %d, %d", r.pos.x, r.pos.y, r.pos.z)
	// checkLoaded over the chunks from a to b.
	loaded := func(ax, az, bx, bz int) bool {
		return h.cloneLoaded(dim, blockPos{ax, 0, az}, blockPos{bx, 0, bz})
	}
	switch r.kind {
	case "feature":
		_, tree := worldgen.TreeFeatures[name]
		mushroom := name == "huge_brown_mushroom" || name == "huge_red_mushroom"
		if !tree && !mushroom || !strings.HasPrefix(r.id, "minecraft:") {
			return fmt.Sprintf("Can't find element '%s' in registry 'minecraft:worldgen/configured_feature'", r.id)
		}
		if !loaded(r.pos.x-16, r.pos.z-16, r.pos.x+16, r.pos.z+16) {
			return "That position is not loaded"
		}
		ok := false
		if tree {
			ok = h.placeLiveTree(players, dim, r.pos.x, r.pos.y, r.pos.z, name)
		} else {
			ok = h.growHugeMushroom(players, dim, r.pos.x, r.pos.y, r.pos.z, name == "huge_brown_mushroom")
		}
		if !ok {
			return "Failed to place feature"
		}
		h.cmdOK(players, r.by)(fmt.Sprintf("Placed \"%s\" at %s", r.id, at))
	case "jigsaw":
		if !worldgen.HasPool(name) || !strings.HasPrefix(r.id, "minecraft:") {
			return fmt.Sprintf("There is no template pool with type \"%s\"", r.id)
		}
		if !loaded(r.pos.x, r.pos.z, r.pos.x, r.pos.z) {
			return "That position is not loaded"
		}
		pieces, ok := w.Gen().PlaceJigsawPieces(name, r.target, r.pos.x, r.pos.y, r.pos.z, r.depth, h.rng.Int63())
		if !ok {
			return "Failed to generate jigsaw"
		}
		h.placePieces(players, dim, pieces, "")
		h.cmdOK(players, r.by)("Generated jigsaw at " + at)
	case "structure":
		if !vanillaStructures[name] || !strings.HasPrefix(r.id, "minecraft:") {
			return fmt.Sprintf("There is no structure with type \"%s\"", r.id)
		}
		pieces, ok := w.Gen().PlaceStructurePieces(name, r.pos.x, r.pos.z)
		if !ok {
			return "Failed to place structure"
		}
		x0, z0, x1, z1, _ := worldgen.PiecesBounds(pieces)
		if !loaded(x0, z0, x1, z1) {
			return "That position is not loaded"
		}
		fallback := ""
		if name == "igloo" {
			fallback = "chests/igloo_chest" // IglooPieces sets the laboratory chest's table itself
		}
		h.placePieces(players, dim, pieces, fallback)
		h.cmdOK(players, r.by)(fmt.Sprintf("Generated structure \"%s\" at %s", r.id, at))
	case "template":
		tm := worldgen.TemplateByName(name)
		if tm == nil || !strings.HasPrefix(r.id, "minecraft:") {
			return fmt.Sprintf("There is no template with ID \"%s\"", r.id)
		}
		if !loaded(r.pos.x, r.pos.z, r.pos.x+tm.Size[0], r.pos.z+tm.Size[2]) {
			return "That position is not loaded"
		}
		if len(tm.Blocks) == 0 {
			return "Failed to place template"
		}
		var keep func() bool
		if r.integrity < 1 {
			// BlockRotProcessor over StructureBlockEntity.createRandom(seed):
			// one source for the whole template, a fresh one each time when
			// the seed is 0.
			seed := r.seed
			if seed == 0 {
				seed = h.rng.Int63()
			}
			rnd := newLegacyRandom(seed)
			keep = func() bool { return rnd.nextFloat() <= r.integrity }
		}
		cells := tm.PlaceCells(r.pos.x, r.pos.y, r.pos.z, r.rot, r.mir, keep)
		h.placeEdits(players, dim, cells)
		h.stockLootChests(dim, tm.PlaceChests(r.pos.x, r.pos.y, r.pos.z, r.rot, r.mir), "")
		h.cmdOK(players, r.by)(fmt.Sprintf("Loaded template \"%s\" at %s", r.id, at))
	}
	return ""
}

// nextFloat is BitRandomSource.nextFloat.
func (r *legacyRandom) nextFloat() float32 {
	return float32(r.next(24)) * 0x1p-24
}

// placePieces lays structure pieces into the live world: each chunk they
// reach is stamped as the generator would stamp it, over what the world
// holds there now, and the cells that came out different are written as
// edits. fallback is the loot table for a chest its template leaves to code.
func (h *hub) placePieces(players map[int32]*tracked, dim int, pieces []worldgen.PlacedPiece, fallback string) int {
	w := h.worldFor(dim)
	g := w.Gen()
	x0, z0, x1, z1, ok := worldgen.PiecesBounds(pieces)
	if !ok {
		return 0
	}
	var cells []worldgen.TemplateCell
	for cx := int32(x0 >> 4); cx <= int32(x1>>4); cx++ {
		for cz := int32(z0 >> 4); cz <= int32(z1>>4); cz++ {
			ch := w.Chunk(cx, cz) // a copy: the generated base with the edits over it
			before := make([][4096]uint32, len(ch.Sections))
			copy(before, ch.Sections)
			g.StampPieces(ch, cx, cz, pieces)
			for s := range ch.Sections {
				for i, st := range ch.Sections[s] {
					if st == before[s][i] {
						continue
					}
					lx, lz, ly := i&15, (i>>4)&15, i>>8
					cells = append(cells, worldgen.TemplateCell{
						X: int(cx)*16 + lx, Y: worldgen.MinY + s*16 + ly, Z: int(cz)*16 + lz, State: st})
				}
			}
		}
	}
	n := h.placeEdits(players, dim, cells)
	h.stockLootChests(dim, g.PieceLootChests(pieces), fallback)
	return n
}

// placeBroadcastMax is how many placed blocks go out as single block
// updates; a bigger placement has its viewers re-request their chunks.
const placeBroadcastMax = 4096

// placeEdits writes placed cells into the world as UPDATE_CLIENTS does: the
// viewers see them, no neighbour or shape updates follow, and a block entity
// the placement replaces goes without spilling (placeInWorld clears it).
func (h *hub) placeEdits(players map[int32]*tracked, dim int, cells []worldgen.TemplateCell) int {
	w := h.worldFor(dim)
	big := len(cells) > placeBroadcastMax
	n := 0
	for _, c := range cells {
		if !h.inWorldYIn(dim, c.Y) {
			continue
		}
		old := w.At(c.X, c.Y, c.Z)
		pos := simPos{dim: dim, blockPos: blockPos{c.X, c.Y, c.Z}}
		if hasBlockEntity(old) {
			h.discardBlockEntity(pos)
		}
		if old == c.State {
			continue
		}
		w.SetBlock(c.X, c.Y, c.Z, c.State)
		if !big {
			h.broadcastBlockIn(players, dim, c.X, c.Y, c.Z, c.State)
		}
		n++
	}
	if big {
		h.resyncChunks(players, dim)
	}
	return n
}

// resyncChunks has everyone in a dimension re-request the chunks in their
// view, for a change too big to send block by block.
func (h *hub) resyncChunks(players map[int32]*tracked, dim int) {
	for _, t := range players {
		if t.dim == dim {
			t.p.sendEvReliable(attachproto.Resync{})
		}
	}
}

// stockLootChests gives each placed loot container its template's table, as
// the template's LootTable tag does, filled at once. A container the
// template leaves to code takes fallback, or stays empty.
func (h *hub) stockLootChests(dim int, chests []worldgen.LootChest, fallback string) {
	w := h.worldFor(dim)
	for _, lc := range chests {
		table := lc.Table
		if table == "" {
			table = fallback
		}
		if table == "" || !isChestLikeContainer(w.At(lc.X, lc.Y, lc.Z)) {
			continue
		}
		pos := simPos{dim: dim, blockPos: blockPos{lc.X, lc.Y, lc.Z}}
		c := &chest{}
		h.chests[pos] = c
		h.fillChest(c, strings.TrimPrefix(table, "minecraft:"), pos.blockPos)
	}
}
