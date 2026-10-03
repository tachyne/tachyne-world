package worldgen

import (
	"encoding/json"
	"math"
	"strconv"
	"sync"
)

// Where vanilla's structures start: ChunkGenerator.createStructures over a
// dimension's structure sets. A set's placement says which chunks may hold
// one of its structures — a random-spread grid cell's one chunk, salted
// per set (RandomSpreadStructurePlacement), thinned by a frequency reducer
// and kept clear of another set (exclusion zones), or the stronghold rings
// (ConcentricRingsStructurePlacement), nudged toward preferred biomes. A
// set of several structures picks between them by weight, trying each
// until one fits its biome; each structure type finds its start position
// its own way, and the start's biome must be one of the structure's.
//
// The placement grid, the reducers, the exclusion zones, the set's pick
// and the ring positions are pure functions of the world seed and the
// biome source, so they match the server exactly; the start positions read
// the terrain before features (VanillaTerrain).

// VanillaStart is a structure start: which structure, in which chunk, and
// the position its biome was checked at.
type VanillaStart struct {
	Structure string
	Set       string
	ChunkX    int32
	ChunkZ    int32
	X, Y, Z   int
	// Rotation is the start's rotation where finding its position drew
	// one (0 none, 1 clockwise 90, 2 180, 3 counterclockwise 90), else -1.
	Rotation int
}

// vanillaStructs places one dimension's structure starts.
type vanillaStructs struct {
	seed     int64
	d        *vpData
	sets     []*vpSet
	possible map[string]bool
	biome    func(qx, qy, qz int) string // the noise biome at a quart cell (short name)
	// fixed is the biome of a single-biome world (FixedBiomeSource, whose
	// biome search is its own); "" for a multi-noise one.
	fixed    string
	terrain  VanillaTerrain // nil: no terrain (tests): the sea level everywhere
	minY     int
	height   int
	seaLevel int

	ringsOnce sync.Once
	rings     map[string][]*vpRing
}

// newVanillaStructs builds the placement for a dimension ("overworld",
// "nether", "end"). biome answers noise biomes by quart cell (short names);
// terrain gives heights and columns (nil in tests: sea level everywhere).
func newVanillaStructs(seed int64, dim string, biome func(qx, qy, qz int) string, terrain VanillaTerrain) *vanillaStructs {
	d := mustVPData()
	v := &vanillaStructs{seed: seed, d: d, possible: map[string]bool{}, biome: biome, terrain: terrain,
		minY: MinY, height: SectionCount * 16, seaLevel: SeaLevel}
	switch dim {
	case "nether":
		v.minY, v.height, v.seaLevel = 0, 128, 32
	case "end":
		v.minY, v.height, v.seaLevel = 0, 128, 0
	}
	if terrain != nil {
		// WorldGenerationContext: the level's limits within the noise
		// settings' own.
		lo := max(terrain.MinY(), v.minY)
		v.height = min(terrain.Ceiling(), v.minY+v.height) - lo
		v.minY, v.seaLevel = lo, terrain.SeaLevel()
	}
	for _, b := range d.Possible[dim] {
		v.possible[b] = true
	}
	// ChunkGeneratorStructureState: the sets with a structure that could
	// start in one of the dimension's biomes, in registry order.
	for _, name := range d.SetNames {
		set := d.Sets[name]
		for _, e := range set.Structures {
			if v.anyPossible(d.Structures[e.Name]) {
				v.sets = append(v.sets, set)
				break
			}
		}
	}
	return v
}

func (v *vanillaStructs) anyPossible(s *vpStructure) bool {
	for b := range s.Biomes {
		if v.possible[b] {
			return true
		}
	}
	return false
}

// set returns a structure set by name (nil if the dimension has none).
func (v *vanillaStructs) set(name string) *vpSet {
	for _, s := range v.sets {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// potentialChunk is RandomSpreadStructurePlacement.getPotentialStructureChunk:
// the one chunk of (cx, cz)'s grid cell that may hold the set's structure.
func (v *vanillaStructs) potentialChunk(s *vpSet, cx, cz int32) (int32, int32) {
	gx, gz := vpFloorDiv(cx, s.Spacing), vpFloorDiv(cz, s.Spacing)
	r := newVWLegacy(0)
	r.setLargeFeatureWithSalt(v.seed, gx, gz, s.Salt)
	limit := s.Spacing - s.Separation
	sx, sz := vpSpread(r, limit, s.Triangular), vpSpread(r, limit, s.Triangular)
	return gx*s.Spacing + sx, gz*s.Spacing + sz
}

func vpSpread(r *vwRandom, limit int32, tri bool) int32 {
	if tri {
		return (r.nextIntN(limit) + r.nextIntN(limit)) / 2
	}
	return r.nextIntN(limit)
}

func vpFloorDiv(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// isPlacementChunk is the placement's own test, before frequency and
// exclusion.
func (v *vanillaStructs) isPlacementChunk(s *vpSet, cx, cz int32) bool {
	if s.Type == "concentric_rings" {
		for _, p := range v.ringsNear(s, cx, cz, 0) {
			if p == [2]int32{cx, cz} {
				return true
			}
		}
		return false
	}
	px, pz := v.potentialChunk(s, cx, cz)
	return px == cx && pz == cz
}

// isStructureChunk is StructurePlacement.isStructureChunk: the placement,
// the frequency reduction and the exclusion zone.
func (v *vanillaStructs) isStructureChunk(s *vpSet, cx, cz int32) bool {
	if !v.isPlacementChunk(s, cx, cz) {
		return false
	}
	if s.Frequency < 1 && !vpReduce(s.Reduction, v.seed, s.Salt, cx, cz, s.Frequency) {
		return false
	}
	if s.Exclusion != "" {
		other := v.d.Sets[s.Exclusion]
		for x := cx - s.ExclChunks; x <= cx+s.ExclChunks; x++ {
			for z := cz - s.ExclChunks; z <= cz+s.ExclChunks; z++ {
				if v.isStructureChunk(other, x, z) {
					return false
				}
			}
		}
	}
	return true
}

// vpReduce is FrequencyReductionMethod.shouldGenerate.
func vpReduce(method string, seed int64, salt, cx, cz int32, p float32) bool {
	r := newVWLegacy(0)
	switch method {
	case "legacy_type_1": // the pillager outposts' reducer
		x, z := cx>>4, cz>>4
		r.setSeed(int64(x^z<<4) ^ seed)
		r.nextInt()
		return r.nextIntN(int32(1/p)) == 0
	case "legacy_type_2":
		r.setLargeFeatureWithSalt(seed, cx, cz, 10387320)
		return r.nextFloat() < p
	case "legacy_type_3":
		r.setLargeFeatureSeed(seed, cx, cz)
		return r.nextDouble() < float64(p)
	}
	r.setLargeFeatureWithSalt(seed, cx, cz, salt)
	return r.nextFloat() < p
}

// vpRing is one stronghold ring position before its biome search: the
// position the ring puts it at, and the seed of the search's own random
// (the ring stream's fork).
type vpRing struct {
	ix, iz   int32
	forkSeed int64
	once     sync.Once
	pos      [2]int32
}

// buildRings lays out every concentric-rings set's positions
// (ChunkGeneratorStructureState.generateRingPositions): the ring stream
// alone decides where each one starts and the seed of its biome search,
// so the searches — the costly part — run only for the positions a query
// comes near (ringAt).
func (v *vanillaStructs) buildRings() {
	v.rings = map[string][]*vpRing{}
	for _, s := range v.sets {
		if s.Type != "concentric_rings" || s.Count == 0 {
			continue
		}
		v.rings[s.Name] = vpRingLayout(s, v.seed)
	}
}

func vpRingLayout(s *vpSet, ringSeed int64) []*vpRing {
	dist, count, spread := s.Distance, s.Count, s.Spread
	r := newVWLegacySource(ringSeed)
	angle := r.nextDouble() * math.Pi * 2
	inCircle, circle := 0, 0
	out := make([]*vpRing, 0, count)
	for i := 0; i < count; i++ {
		d := float64(4*dist+dist*circle*6) + (r.nextDouble()-0.5)*(float64(dist)*2.5)
		ix := int32(javaRound(math.Cos(angle) * d))
		iz := int32(javaRound(math.Sin(angle) * d))
		out = append(out, &vpRing{ix: ix, iz: iz, forkSeed: r.nextLong()})
		angle += math.Pi * 2 / float64(spread)
		if inCircle++; inCircle == spread {
			circle++
			inCircle = 0
			spread += 2 * spread / (circle + 1)
			spread = min(spread, count-i)
			angle += r.nextDouble() * math.Pi * 2
		}
	}
	return out
}

// ringAt is one ring position after its biome search (run once).
func (v *vanillaStructs) ringAt(s *vpSet, p *vpRing) [2]int32 {
	p.once.Do(func() {
		p.pos = [2]int32{p.ix, p.iz}
		search := newVWLegacySource(p.forkSeed)
		if bx, bz, ok := v.findBiomeHorizontal(int(p.ix)<<4+8, 0, int(p.iz)<<4+8, 112, s.Preferred, search); ok {
			p.pos = [2]int32{int32(bx >> 4), int32(bz >> 4)}
		}
	})
	return p.pos
}

// ringPositions is every ring position of a set, searched (tests, /locate).
func (v *vanillaStructs) ringPositions(s *vpSet, ringSeed int64) [][2]int32 {
	var out [][2]int32
	for _, p := range vpRingLayout(s, ringSeed) {
		out = append(out, v.ringAt(s, p))
	}
	return out
}

// ringsNear is the searched ring positions of a set whose unsearched
// position lies within reach chunks of (cx, cz) — a search moves a
// position at most 112 blocks, seven chunks.
func (v *vanillaStructs) ringsNear(s *vpSet, cx, cz, reach int32) [][2]int32 {
	v.ringsOnce.Do(v.buildRings)
	var out [][2]int32
	for _, p := range v.rings[s.Name] {
		if dx, dz := p.ix-cx, p.iz-cz; dx >= -reach-7 && dx <= reach+7 && dz >= -reach-7 && dz <= reach+7 {
			out = append(out, v.ringAt(s, p))
		}
	}
	return out
}

// javaRound is Math.round(double).
func javaRound(x float64) int64 { return int64(math.Floor(x + 0.5)) }

// findBiomeHorizontal is BiomeSource.findBiomeHorizontal (not closest-first,
// one-quart steps): every quart cell on the square ring at the search
// radius is tried, and among the matches one is kept by reservoir draw.
func (v *vanillaStructs) findBiomeHorizontal(x, y, z, radius int, allowed map[string]bool, r *vwRandom) (int, int, bool) {
	if v.fixed != "" {
		// FixedBiomeSource: anywhere in the square, if the biome is wanted.
		if !allowed[v.fixed] {
			return 0, 0, false
		}
		bx := x - radius + int(r.nextIntN(int32(radius*2+1)))
		bz := z - radius + int(r.nextIntN(int32(radius*2+1)))
		return bx, bz, true
	}
	cx, cz, qr, qy := x>>2, z>>2, radius>>2, y>>2
	found := 0
	var rx, rz int
	ok := false
	for dz := -qr; dz <= qr; dz++ {
		for dx := -qr; dx <= qr; dx++ {
			if allowed[v.biome(cx+dx, qy, cz+dz)] {
				if !ok || r.nextIntN(int32(found+1)) == 0 {
					rx, rz, ok = (cx+dx)<<2, (cz+dz)<<2, true
				}
				found++
			}
		}
	}
	return rx, rz, ok
}

// RingPositions is every searched ring position of a set (nil for a
// dimension without it).
func (v *vanillaStructs) RingPositions(set string) [][2]int32 {
	s := v.set(set)
	if s == nil || s.Type != "concentric_rings" {
		return nil
	}
	v.ringsOnce.Do(v.buildRings)
	var out [][2]int32
	for _, p := range v.rings[set] {
		out = append(out, v.ringAt(s, p))
	}
	return out
}

// StartsIn is ChunkGenerator.createStructures for one chunk: every set's
// start there, in registry order.
func (v *vanillaStructs) StartsIn(cx, cz int32) []VanillaStart {
	var out []VanillaStart
	for _, s := range v.sets {
		if st, ok := v.SetStartIn(s, cx, cz); ok {
			out = append(out, st)
		}
	}
	return out
}

// SetStartIn is one set's start in a chunk, if it has one: the placement
// says yes, and a structure of the set fits — the only one, or the first
// of a weighted draw without replacement whose start lands in its biomes.
func (v *vanillaStructs) SetStartIn(s *vpSet, cx, cz int32) (VanillaStart, bool) {
	if !v.isStructureChunk(s, cx, cz) {
		return VanillaStart{}, false
	}
	if len(s.Structures) == 1 {
		return v.tryStart(s, s.Structures[0].Name, cx, cz)
	}
	opts := append([]vpSetEntry(nil), s.Structures...)
	r := newVWLegacy(0)
	r.setLargeFeatureSeed(v.seed, cx, cz)
	total := 0
	for _, o := range opts {
		total += o.Weight
	}
	for len(opts) > 0 {
		choice := int(r.nextIntN(int32(total)))
		idx := 0
		for _, o := range opts {
			choice -= o.Weight
			if choice < 0 {
				break
			}
			idx++
		}
		sel := opts[idx]
		if st, ok := v.tryStart(s, sel.Name, cx, cz); ok {
			return st, true
		}
		opts = append(opts[:idx], opts[idx+1:]...)
		total -= sel.Weight
	}
	return VanillaStart{}, false
}

// tryStart is Structure.generate's findValidGenerationPoint: the
// structure's start position, and its biome check.
func (v *vanillaStructs) tryStart(s *vpSet, name string, cx, cz int32) (VanillaStart, bool) {
	st := v.d.Structures[name]
	if st == nil || !v.anyPossible(st) {
		return VanillaStart{}, false
	}
	x, y, z, rot, ok := v.startPos(st, cx, cz)
	if !ok {
		return VanillaStart{}, false
	}
	if !st.Biomes[v.biome(x>>2, y>>2, z>>2)] {
		return VanillaStart{}, false
	}
	return VanillaStart{Structure: name, Set: s.Name, ChunkX: cx, ChunkZ: cz, X: x, Y: y, Z: z, Rotation: rot}, true
}

// baseHeight is getBaseHeight (the first free y) for a WG heightmap; with
// no terrain, the sea level.
func (v *vanillaStructs) baseHeight(x, z int, hm HeightmapType) int {
	if v.terrain == nil {
		return v.seaLevel
	}
	switch hm {
	case HeightOceanFloor, HeightOceanFloorWG:
		return v.terrain.Height(HeightOceanFloorWG, x, z)
	}
	return v.terrain.Height(HeightWorldSurfaceWG, x, z)
}

// startPos is the structure type's findGenerationPoint, as far as where
// its start lies: the position its biome is checked at.
func (v *vanillaStructs) startPos(st *vpStructure, cx, cz int32) (x, y, z, rot int, ok bool) {
	minX, minZ := int(cx)<<4, int(cz)<<4
	r := newVWLegacy(0) // Structure.GenerationContext.makeRandom
	r.setLargeFeatureSeed(v.seed, cx, cz)
	onTop := func(hm HeightmapType) (int, int, int, int, bool) {
		return minX + 8, v.baseHeight(minX+8, minZ+8, hm) - 1, minZ + 8, -1, true
	}
	corners := func(x0, z0, sx, sz int) int {
		lo := math.MaxInt
		for _, c := range [4][2]int{{x0, z0}, {x0, z0 + sz}, {x0 + sx, z0}, {x0 + sx, z0 + sz}} {
			lo = min(lo, v.baseHeight(c[0], c[1], HeightWorldSurfaceWG)-1)
		}
		return lo
	}
	switch st.Type {
	case "desert_pyramid", "jungle_temple":
		w, d := 21, 21
		if st.Type == "jungle_temple" {
			w, d = 12, 15
		}
		if corners(minX, minZ, w, d) < v.seaLevel {
			return 0, 0, 0, 0, false
		}
		return onTop(HeightWorldSurfaceWG)
	case "igloo", "swamp_hut":
		return onTop(HeightWorldSurfaceWG)
	case "buried_treasure", "ocean_ruin":
		return onTop(HeightOceanFloorWG)
	case "shipwreck":
		var beached bool
		vpRawField(st, "is_beached", &beached)
		if beached {
			return onTop(HeightWorldSurfaceWG)
		}
		return onTop(HeightOceanFloorWG)
	case "ocean_monument":
		// Every biome within 29 blocks of (9, sea level, 9) must be one a
		// monument may be surrounded by (getBiomesWithin's quart square).
		bx, bz := minX+9, minZ+9
		ok := map[string]bool{}
		for _, b := range v.d.BiomeTags["required_ocean_monument_surrounding"] {
			ok[b] = true
		}
		for qz := (bz - 29) >> 2; qz <= (bz+29)>>2; qz++ {
			for qx := (bx - 29) >> 2; qx <= (bx+29)>>2; qx++ {
				for qy := (v.seaLevel - 29) >> 2; qy <= (v.seaLevel+29)>>2; qy++ {
					if !ok[v.biome(qx, qy, qz)] {
						return 0, 0, 0, 0, false
					}
				}
			}
		}
		return onTop(HeightOceanFloorWG)
	case "woodland_mansion", "end_city":
		bx, bz := minX+7, minZ+7
		rot = int(r.nextIntN(4))
		ox, oz := 5, 5
		switch rot {
		case 1:
			ox = -5
		case 2:
			ox, oz = -5, -5
		case 3:
			oz = -5
		}
		y := corners(bx, bz, ox, oz)
		if y < 60 {
			return 0, 0, 0, 0, false
		}
		return bx, y, bz, rot, true
	case "stronghold":
		return minX, 0, minZ, -1, true
	case "fortress":
		return minX, 64, minZ, -1, true
	case "mineshaft":
		// The start sits under the room at (middle, 50) moved below sea
		// level with its pieces; the pieces are the structure code's, so
		// the check takes the room's own height.
		r.nextDouble()
		return minX + 8, 50, minZ, -1, true
	case "nether_fossil":
		return v.netherFossilStart(st, r, minX, minZ)
	case "ruined_portal":
		return v.ruinedPortalStart(st, r, minX, minZ)
	case "jigsaw":
		return v.jigsawStart(st, r, minX, minZ)
	}
	return minX + 8, v.seaLevel, minZ + 8, -1, true
}

func vpRawField(st *vpStructure, k string, v any) bool {
	raw, ok := st.raw[k]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// jigsawStart is JigsawStructure.findGenerationPoint up to its stub: the
// start height, the start piece's rotation and pick from its pool, the
// named start jigsaw it hangs from, and the piece's centre projected to
// the heightmap.
func (v *vanillaStructs) jigsawStart(st *vpStructure, r *vwRandom, minX, minZ int) (int, int, int, int, bool) {
	ctx := &vpCtx{minY: v.minY, height: v.height, seaLevel: v.seaLevel}
	var startY int
	if raw, ok := st.raw["start_height"]; ok {
		h, err := parseVPHeight(raw)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		startY = h.sample(r, ctx)
	}
	var pool, hm, anchorName string
	vpRawField(st, "start_pool", &pool)
	vpRawField(st, "project_start_to_heightmap", &hm)
	vpRawField(st, "start_jigsaw_name", &anchorName)
	rot := int(r.nextIntN(4))
	elems := v.d.StartPools[vpShort(pool)]
	total := 0
	for _, e := range elems {
		total += e.Weight
	}
	if total == 0 {
		return 0, 0, 0, 0, false
	}
	pick := int(r.nextIntN(int32(total)))
	var el vpPoolElem
	for _, e := range elems {
		if pick -= e.Weight; pick < 0 {
			el = e
			break
		}
	}
	if el.Type == "empty_pool_element" {
		return 0, 0, 0, 0, false
	}
	ax, ay, az := 0, 0, 0
	if anchorName != "" {
		if len(el.Anchors) == 0 {
			return 0, 0, 0, 0, false
		}
		a := el.Anchors[0]
		ax, ay, az = vpRotate(a[0], a[2], rot, 0, 0)
		ay = a[1]
	}
	// The piece at (minX, startY, minZ) moved so its anchor sits there.
	px, pz := minX-ax, minZ-az
	x0, z0, x1, z1 := vpRotatedBox(el.Size, rot, 0, 0, false)
	cx := (px + x1 + px + x0) / 2
	cz := (pz + z1 + pz + z0) / 2
	bottom := startY - ay
	if hm != "" {
		h, ok := vpHeightmapNames[hm]
		if !ok {
			return 0, 0, 0, 0, false
		}
		bottom = startY + v.baseHeight(cx, cz, h)
	}
	return cx, bottom + ay, cz, rot, true
}

// vpRotate is StructureTemplate.transform's rotation about a pivot (no
// mirror) of a horizontal position.
func vpRotate(x, z, rot, pivotX, pivotZ int) (int, int, int) {
	switch rot {
	case 1:
		return pivotX + pivotZ - z, 0, pivotZ - pivotX + x
	case 2:
		return pivotX + pivotX - x, 0, pivotZ + pivotZ - z
	case 3:
		return pivotX - pivotZ + z, 0, pivotX + pivotZ - x
	}
	return x, 0, z
}

// vpRotatedBox is a template's bounding box (horizontal) placed at the
// origin with a rotation (and FRONT_BACK mirror) about a pivot.
func vpRotatedBox(size [3]int, rot, pivotX, pivotZ int, mirror bool) (x0, z0, x1, z1 int) {
	ax, az := 0, 0
	bx, bz := size[0]-1, size[2]-1
	if mirror {
		ax, bx = -ax, -bx
	}
	ax, _, az = vpRotate(ax, az, rot, pivotX, pivotZ)
	bx, _, bz = vpRotate(bx, bz, rot, pivotX, pivotZ)
	return min(ax, bx), min(az, bz), max(ax, bx), max(az, bz)
}

// ruinedPortalStart is RuinedPortalStructure.findGenerationPoint up to its
// origin: the setup, air pocket, portal template, rotation and mirror
// draws, then the height its placement wants, settled onto ground three of
// the four bottom corners stand on.
func (v *vanillaStructs) ruinedPortalStart(st *vpStructure, r *vwRandom, minX, minZ int) (int, int, int, int, bool) {
	var setups []struct {
		Air       float32 `json:"air_pocket_probability"`
		Placement string  `json:"placement"`
		Weight    float32 `json:"weight"`
	}
	vpRawField(st, "setups", &setups)
	if len(setups) == 0 {
		return 0, 0, 0, 0, false
	}
	pick := 0
	if len(setups) > 1 {
		var total float32
		for _, s := range setups {
			total += s.Weight
		}
		f := r.nextFloat()
		pick = -1
		for i, s := range setups {
			if f -= s.Weight / total; f < 0 {
				pick = i
				break
			}
		}
		if pick < 0 {
			return 0, 0, 0, 0, false
		}
	}
	setup := setups[pick]
	air := false
	switch setup.Air {
	case 0:
	case 1:
		air = true
	default:
		air = r.nextFloat() < setup.Air
	}
	var loc string
	if r.nextFloat() < 0.05 {
		loc = "ruined_portal/giant_portal_" + strconv.Itoa(int(r.nextIntN(3))+1)
	} else {
		loc = "ruined_portal/portal_" + strconv.Itoa(int(r.nextIntN(10))+1)
	}
	size := v.d.TemplateSizes[loc]
	rot := int(r.nextIntN(4))
	mirror := r.nextFloat() >= 0.5
	pvx, pvz := size[0]/2, size[2]/2
	x0, z0, x1, z1 := vpRotatedBox(size, rot, pvx, pvz, mirror)
	x0, z0, x1, z1 = x0+minX, z0+minZ, x1+minX, z1+minZ
	ccx, ccz := x0+(x1-x0+1)/2, z0+(z1-z0+1)/2
	hm := HeightWorldSurfaceWG
	if setup.Placement == "on_ocean_floor" {
		hm = HeightOceanFloorWG
	}
	surface := v.baseHeight(ccx, ccz, hm) - 1
	ySpan := size[1]
	floor := v.minY + 15
	var y int
	switch setup.Placement {
	case "in_nether":
		switch {
		case air:
			y = int(r.nextIntN(69)) + 32
		case r.nextFloat() < 0.5:
			y = int(r.nextIntN(3)) + 27
		default:
			y = int(r.nextIntN(72)) + 29
		}
	case "in_mountain":
		y = vpWithin(r, 70, surface-ySpan)
	case "underground":
		y = vpWithin(r, floor, surface-ySpan)
	case "partly_buried":
		y = surface - ySpan + int(r.nextIntN(7)) + 2
	default:
		y = surface
	}
	if v.terrain != nil {
		cols := [4][2]int{{x0, z0}, {x1, z0}, {x0, z1}, {x1, z1}}
		for ; y > floor; y-- {
			n := 0
			for _, c := range cols {
				if vpOpaqueFor(hm, v.terrain.BlockAt(c[0], y, c[1])) {
					if n++; n == 3 {
						return minX, y, minZ, rot, true
					}
				}
			}
		}
	}
	return minX, y, minZ, rot, true
}

func vpWithin(r *vwRandom, lo, hi int) int {
	if lo < hi {
		return int(r.nextIntN(int32(hi-lo+1))) + lo
	}
	return hi
}

// vpOpaqueFor is the heightmap's isOpaque predicate for terrain blocks:
// WORLD_SURFACE_WG counts anything but air, OCEAN_FLOOR_WG only blocks
// that block motion (not fluids).
func vpOpaqueFor(hm HeightmapType, s uint32) bool {
	if hm == HeightOceanFloorWG || hm == HeightOceanFloor {
		return s != Air && !IsFluid(s)
	}
	return s != Air
}

// netherFossilStart is NetherFossilStructure.findGenerationPoint: a random
// column in the chunk, a random height, then down to the first air over
// soul sand or a sturdy top above the lava sea.
func (v *vanillaStructs) netherFossilStart(st *vpStructure, r *vwRandom, minX, minZ int) (int, int, int, int, bool) {
	x := minX + int(r.nextIntN(16))
	z := minZ + int(r.nextIntN(16))
	ctx := &vpCtx{minY: v.minY, height: v.height, seaLevel: v.seaLevel}
	y := 0
	if raw, ok := st.raw["height"]; ok {
		h, err := parseVPHeight(raw)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		y = h.sample(r, ctx)
	}
	if v.terrain == nil {
		return x, y, z, -1, y > v.seaLevel
	}
	for y > v.seaLevel {
		cur := v.terrain.BlockAt(x, y, z)
		y--
		below := v.terrain.BlockAt(x, y, z)
		if cur == Air && (vpIsBlock(below, "soul_sand") || IsFaceSturdy(below, FaceUp)) {
			break
		}
	}
	if y <= v.seaLevel {
		return 0, 0, 0, 0, false
	}
	return x, y, z, -1, true
}

// Locate is ChunkGenerator.findNearestMapStructure as /locate runs it (no
// references): the stronghold rings by distance to their chunk's centre
// at y=32; the random-spread sets ring by ring of grid cells outward from
// the origin's chunk, each set's first start in ring order, the nearest of
// those once any set has one. The result is the start chunk's locate
// position (its corner plus the set's locate offset).
func (v *vanillaStructs) Locate(wanted map[string]bool, x, y, z, maxRadius int) (lx, lz int, name string, ok bool) {
	dist := func(px, py, pz int) float64 {
		dx, dy, dz := float64(px-x), float64(py-y), float64(pz-z)
		return dx*dx + dy*dy + dz*dz
	}
	startOf := func(s *vpSet, cx, cz int32) (VanillaStart, bool) {
		st, ok := v.SetStartIn(s, cx, cz)
		return st, ok && wanted[st.Structure]
	}
	best := math.MaxFloat64
	var spread []*vpSet
	for _, s := range v.sets {
		has := false
		for _, e := range s.Structures {
			if wanted[e.Name] && v.anyPossible(v.d.Structures[e.Name]) {
				has = true
			}
		}
		if !has {
			continue
		}
		if s.Type == "concentric_rings" {
			v.ringsOnce.Do(v.buildRings)
			found := false
			closest := math.MaxFloat64
			var fx, fz int
			var fn string
			for _, p := range v.rings[s.Name] {
				c := v.ringAt(s, p)
				d := dist(int(c[0])*16+8, 32, int(c[1])*16+8)
				if found && d >= closest {
					continue
				}
				if st, hit := startOf(s, c[0], c[1]); hit {
					found, closest = true, d
					fx, fz, fn = int(c[0])*16+s.Locate[0], int(c[1])*16+s.Locate[2], st.Structure
				}
			}
			if found {
				if d := dist(fx, s.Locate[1], fz); d < best {
					best, lx, lz, name, ok = d, fx, fz, fn, true
				}
			}
			continue
		}
		spread = append(spread, s)
	}
	if len(spread) == 0 {
		return
	}
	ocx, ocz := int32(x>>4), int32(z>>4)
	for r := int32(0); r <= int32(maxRadius); r++ {
		found := false
		for _, s := range spread {
		ring:
			for dx := -r; dx <= r; dx++ {
				for dz := -r; dz <= r; dz++ {
					if dx != -r && dx != r && dz != -r && dz != r {
						continue
					}
					pcx, pcz := v.potentialChunk(s, ocx+s.Spacing*dx, ocz+s.Spacing*dz)
					if st, hit := startOf(s, pcx, pcz); hit {
						found = true
						px, py, pz := int(pcx)*16+s.Locate[0], s.Locate[1], int(pcz)*16+s.Locate[2]
						if d := dist(px, py, pz); d < best {
							best, lx, lz, name, ok = d, px, pz, st.Structure, true
						}
						break ring
					}
				}
			}
		}
		if found {
			return lx, lz, name, true
		}
	}
	return lx, lz, name, ok
}
