package server

import (
	"fmt"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /place feature|jigsaw|structure|template (PlaceCommand). The generator lays
// the blocks out (worldgen/placecmd.go, placestructs.go, placefeatures.go)
// and they go into the live world as edits: StructureTemplate.placeInWorld
// with UPDATE_CLIENTS, so the viewers see them and no neighbour updates
// run. Features are the trees and huge mushrooms the engine grows and the
// ores (the Overworld's and the Nether's), disks, springs, monster room,
// amethyst geode and sculk it decorates with, and the single plants 26.3's
// vegetation features are (placeveg.go); structures are the jigsaw-built
// ones, the igloo and the End city, and the code-built ones (temples, huts,
// wrecks, buried treasure, ruined portals, ocean ruins, the mansion, the
// monument, the fortress, the stronghold, mineshafts and the nether
// fossil) — with their loot, their spawners' mobs and the entities their
// generation places; jigsaws and templates are every pool and template the
// engine has.

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

// placeFeatureNames are the configured features /place grows through
// worldgen.PlaceFeatureStamp.
var placeFeatureNames = keySet(worldgen.PlaceFeatureNames()...)

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
		stamped := placeFeatureNames[name]
		veg := vegetationFeature(name)
		if !tree && !mushroom && !stamped && !veg || !strings.HasPrefix(r.id, "minecraft:") {
			return fmt.Sprintf("Can't find element '%s' in registry 'minecraft:worldgen/feature'", r.id)
		}
		if !loaded(r.pos.x-16, r.pos.z-16, r.pos.x+16, r.pos.z+16) {
			return "That position is not loaded"
		}
		ok := false
		switch {
		case tree:
			ok = h.placeLiveTree(players, dim, r.pos.x, r.pos.y, r.pos.z, name)
		case mushroom:
			ok = h.growHugeMushroom(players, dim, r.pos.x, r.pos.y, r.pos.z, name == "huge_brown_mushroom")
		case veg:
			ok = h.placeVegetation(players, dim, r.pos, name)
		default:
			fs, _ := w.Gen().PlaceFeatureStamp(name, r.pos.x, r.pos.y, r.pos.z, h.rng.Int63(), w.At)
			ok = h.placeFeatureStamp(players, dim, fs) > 0 // Feature.place: whether it set anything
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
		if pieces, ok := w.Gen().PlaceStructurePieces(name, r.pos.x, r.pos.z); ok {
			x0, z0, x1, z1, _ := worldgen.PiecesBounds(pieces)
			if !loaded(x0, z0, x1, z1) {
				return "That position is not loaded"
			}
			fallback := ""
			switch name {
			case "igloo":
				fallback = "chests/igloo_chest" // IglooPieces sets the laboratory chest's table itself
			case "end_city":
				fallback = "chests/end_city_treasure"
			}
			h.placePieces(players, dim, pieces, fallback)
			h.placePieceMobs(players, dim, name, pieces)
		} else if st, ok := w.Gen().PlaceStructureStamp(name, r.pos.x, r.pos.z); ok {
			if !loaded(st.X0, st.Z0, st.X1, st.Z1) {
				return "That position is not loaded"
			}
			h.placeStamp(players, dim, st)
		} else {
			return "Failed to place structure"
		}
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
	g := h.worldFor(dim).Gen()
	x0, z0, x1, z1, ok := worldgen.PiecesBounds(pieces)
	if !ok {
		return 0
	}
	cells := h.stampCells(dim, x0, z0, x1, z1, func(ch *worldgen.Chunk, cx, cz int32) { g.StampPieces(ch, cx, cz, pieces) })
	n := h.placeEdits(players, dim, cells)
	h.stockLootChests(dim, g.PieceLootChests(pieces), fallback)
	return n
}

// stampCells runs a stamp over every chunk the columns x0..x1, z0..z1
// reach, each stamped as the generator would stamp it over what the world
// holds there now, and gives the cells that came out different.
func (h *hub) stampCells(dim, x0, z0, x1, z1 int, stamp func(ch *worldgen.Chunk, cx, cz int32)) []worldgen.TemplateCell {
	w := h.worldFor(dim)
	var cells []worldgen.TemplateCell
	for cx := int32(x0 >> 4); cx <= int32(x1>>4); cx++ {
		for cz := int32(z0 >> 4); cz <= int32(z1>>4); cz++ {
			ch := w.Chunk(cx, cz) // a copy: the generated base with the edits over it
			before := make([][4096]uint32, len(ch.Sections))
			copy(before, ch.Sections)
			stamp(ch, cx, cz)
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
	return cells
}

// placeStamp lays a code-built structure into the live world (placePieces
// for a worldgen.StructureStamp): its blocks, its loot containers and chest
// minecarts, its spawners' mobs, and the mobs its generation places.
func (h *hub) placeStamp(players map[int32]*tracked, dim int, st worldgen.StructureStamp) int {
	n := h.placeEdits(players, dim, h.stampCells(dim, st.X0, st.Z0, st.X1, st.Z1, st.Stamp))
	h.stockLootChests(dim, st.Chests, "")
	h.stockLootBins(dim, st.Bins)
	for _, c := range st.Carts {
		h.spawnStructureCart(players, dim, c.X, c.Y, c.Z, strings.TrimPrefix(c.Table, "minecraft:"))
	}
	w := h.worldFor(dim)
	spawner := worldgen.BlockBase("spawner")
	for _, sp := range st.Spawners { // the cage's SpawnData, as the piece sets it
		if w.At(sp.X, sp.Y, sp.Z) == spawner {
			h.setSpawnerEntity(simPos{dim: dim, blockPos: blockPos{sp.X, sp.Y, sp.Z}}, sp.Entity)
		}
	}
	for _, m := range st.Mobs {
		h.placeStructureMob(players, dim, m)
	}
	return n
}

// placeFeatureStamp lays a configured feature into the live world: its
// blocks, its loot chests and its spawner's mob. The count is the blocks it
// changed.
func (h *hub) placeFeatureStamp(players map[int32]*tracked, dim int, fs worldgen.FeatureStamp) int {
	if fs.Stamp == nil {
		return 0
	}
	n := h.placeEdits(players, dim, h.stampCells(dim, fs.X0, fs.Z0, fs.X1, fs.Z1, fs.Stamp))
	if n == 0 {
		return 0
	}
	h.stockLootChests(dim, fs.Chests, "")
	w := h.worldFor(dim)
	spawner := worldgen.BlockBase("spawner")
	for _, sp := range fs.Spawners {
		if w.At(sp.X, sp.Y, sp.Z) == spawner {
			h.setSpawnerEntity(simPos{dim: dim, blockPos: blockPos{sp.X, sp.Y, sp.Z}}, sp.Entity)
		}
	}
	return n
}

// stockLootBins gives each placed dispenser its table, filled at once (a
// jungle temple's arrow traps).
func (h *hub) stockLootBins(dim int, bins []worldgen.LootChest) {
	w := h.worldFor(dim)
	for _, lb := range bins {
		state := w.At(lb.X, lb.Y, lb.Z)
		if !isDispenser(state) {
			continue
		}
		pos := simPos{dim: dim, blockPos: blockPos{lb.X, lb.Y, lb.Z}}
		b := h.binAt(pos, state)
		for i := range b.slots {
			b.slots[i] = invStack{}
		}
		h.fillSlots(b.slots, strings.TrimPrefix(lb.Table, "minecraft:"), pos.blockPos)
	}
}

// placeStructureMob places one mob a code-built structure's generation
// places: a swamp hut's witch and black cat (both persistent), a
// monument's elder guardians, an ocean ruin's persistent drowned, a
// mansion's illagers.
func (h *hub) placeStructureMob(players map[int32]*tracked, dim int, sm worldgen.StructureMob) {
	x, y, z := float64(sm.X)+0.5, float64(sm.Y), float64(sm.Z)+0.5
	switch sm.Entity {
	case "witch":
		if m := h.spawnSpecies(players, entityWitch, dim, x, y, z); m != nil {
			m.persistent = true
		}
	case "cat":
		if m := h.spawnSpecies(players, entityCat, dim, x, y, z); m != nil {
			m.persistent = true
			// Cat.finalizeSpawn: in a swamp hut, all black.
			m.variant, m.variantSet = catVariantID[catAllBlack], true
			if vm := variantMeta(m); vm != nil {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(vm))
			}
		}
	case "elder_guardian":
		h.spawnHostileYIn(players, entityElderGuardian, dim, x, y, z)
	case "drowned":
		if m := h.spawnHostileYIn(players, entityDrowned, dim, x, y, z); m != nil {
			m.persistent = true
			h.rollDrownedNautilus(players, m, true)
		}
	case "evoker", "vindicator", "allay":
		if et, ok := entityByName[sm.Entity]; ok {
			h.spawnMobIn(players, et, dim, x, y, z)
		}
	}
}

// placePieceMobs places the entities a jigsaw structure's templates carry,
// as StructureTemplate.placeEntities does when the pieces are placed: a
// village's villagers, golem, cats and animals, an igloo's villager and
// zombie villager, an outpost's caged golem and allays, a bastion's
// garrison, an End city's shulkers and the ship's elytra frame.
func (h *hub) placePieceMobs(players map[int32]*tracked, dim int, name string, pieces []worldgen.PlacedPiece) {
	switch name {
	case "bastion_remnant":
		for _, s := range worldgen.BastionPieceMobs(pieces) {
			et, ok := bastionMobTypes[s.Type]
			if !ok {
				continue
			}
			h.structureSpawn.on, h.structureSpawn.hand = true, bastionPieceHand[s.Piece]
			h.spawnSpecies(players, et, dim, float64(s.X)+0.5, float64(s.Y), float64(s.Z)+0.5)
			h.structureSpawn.on, h.structureSpawn.hand = false, 0
		}
		return
	case "end_city":
		for _, m := range worldgen.EndCityPieceMobs(pieces) {
			switch m.Type {
			case "shulker":
				h.spawnSpecies(players, entityShulker, dim, float64(m.X)+0.5, float64(m.Y), float64(m.Z)+0.5)
			case "elytra_frame":
				f := &itemFrame{eid: h.allocEID(), x: m.X, y: m.Y, z: m.Z, dim: dim,
					dir: endCityFrameDir[m.Rot&3], held: invStack{item: itemByName["elytra"], count: 1}}
				h.itemFrames[f.eid] = f
				h.showFrame(players, f)
			}
		}
		return
	}
	if len(pieces) == 0 {
		return
	}
	// The village's centre is a golem's home.
	home := worldgen.Village{X: pieces[0].OX, Y: pieces[0].OY, Z: pieces[0].OZ, Exists: true}
	for _, vm := range worldgen.PieceMobs(pieces) {
		switch vm.Type {
		case "cushion", "armor_stand": // not mobs
			continue
		}
		h.spawnVillageMobIn(players, dim, home, vm)
	}
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
