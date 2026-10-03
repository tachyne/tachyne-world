package worldgen

import (
	"encoding/json"
	"strings"
)

// The Nether's and the End's features for the vanilla generator's
// placement pass: 26.3's general-purpose features (random neighbour spread,
// overlay, single-block pillars, projected patchy squares, stepped column
// clusters, replace blobs, deltas) ported draw for draw, the huge fungi
// through the engine's HugeFungusFeature port, and the End's chorus
// plants, return gateways and small islands through the engine's End
// decorators — all on the feature's own stream.

// vpManhattan is BlockPos.manhattanOrdered: the cells round an origin in
// rings of growing Manhattan distance (x, then y, then z and its mirror),
// within the reaches and to maxDepth; fn returns false to stop.
func vpManhattan(o vpPos, rx, ry, rz, maxDepth int, fn func(p vpPos) bool) {
	for depth := 0; depth <= maxDepth; depth++ {
		mx := min(rx, depth)
		for x := -mx; x <= mx; x++ {
			my := min(ry, depth-abs(x))
			for y := -my; y <= my; y++ {
				z := depth - abs(x) - abs(y)
				if z > rz {
					continue
				}
				if !fn(vpPos{o.x + x, o.y + y, o.z + z}) {
					return
				}
				if z != 0 && !fn(vpPos{o.x + x, o.y + y, o.z - z}) {
					return
				}
			}
		}
	}
}

func vpDist(a, b vpPos) int { return abs(a.x-b.x) + abs(a.y-b.y) + abs(a.z-b.z) }

// vpExtra3 is the parsed config of this file's features.
type vpExtra3 struct {
	pr                  vpStateProvider
	accepted            [][2]uint32
	canReplace, through vpPredicate
	attempts, xz, y     vpIntProvider
	dir                 [3]int
	chance              float32
	cap                 *vpPlacedFeature
	size                vpIntProvider
	maxProjection       int
	target, replace     uint32
	radius              vpIntProvider
	contents, rim       uint32
	rimSize             vpIntProvider
	cannotOn            [][2]uint32
	clusterReach, count vpIntProvider
	columnReach, height vpIntProvider
	warped              bool
}

func (f *vpFeature) extra3() *vpExtra3 {
	f.once.Do(f.parse)
	if f.ext3 != nil {
		return f.ext3
	}
	return &vpExtra3{}
}

// parseExtra3 reads this file's configs (called from parse).
func (f *vpFeature) parseExtra3() bool {
	x := &vpExtra3{}
	ip := func(k string) vpIntProvider {
		p, err := parseVPInt(f.raw[k])
		if err != nil {
			return vpConstInt(0)
		}
		return p
	}
	pred := func(k string) vpPredicate {
		if p, err := parseVPPredicate(f.raw[k]); err == nil {
			return p
		}
		return vpTrue{}
	}
	blocks := func(k string) [][2]uint32 {
		var one string
		if json.Unmarshal(f.raw[k], &one) == nil {
			if strings.HasPrefix(one, "#") {
				return vpTagRangesOf(vpShort(one[1:]))
			}
			return vpBlockRanges([]string{one})
		}
		var many []string
		_ = json.Unmarshal(f.raw[k], &many)
		return vpBlockRanges(many)
	}
	switch f.typ {
	case "random_neighbor_spread":
		x.pr = parseVPStateProvider(f.raw["block"])
		x.accepted = blocks("accepted_neighbors")
		x.canReplace = pred("can_replace")
		x.attempts, x.xz, x.y = ip("attempts"), ip("xz_offset"), ip("y_offset")
	case "single_block_pillar":
		x.pr = parseVPStateProvider(f.raw["block"])
		x.canReplace = pred("can_replace")
		var d string
		_ = json.Unmarshal(f.raw["direction"], &d)
		if d == "" {
			d = "up"
		}
		x.dir = vpDirections[d]
		x.chance = 1
		if r, ok := f.raw["chance_to_continue"]; ok {
			_ = json.Unmarshal(r, &x.chance)
		}
		if r, ok := f.raw["cap_feature"]; ok {
			x.cap = vpPlacedRef(r)
		}
	case "projected_random_patchy_square":
		x.pr = parseVPStateProvider(f.raw["block"])
		x.through = pred("project_through")
		x.size = ip("size")
		_ = json.Unmarshal(f.raw["max_projection_height"], &x.maxProjection)
	case "netherrack_replace_blobs":
		x.target = vpParseState(f.raw["target"])
		x.replace = vpParseState(f.raw["state"])
		x.radius = ip("radius")
	case "delta_feature":
		x.contents = vpParseState(f.raw["contents"])
		x.rim = vpParseState(f.raw["rim"])
		x.size, x.rimSize = ip("size"), ip("rim_size")
	case "stepped_column_cluster":
		x.pr = parseVPStateProvider(f.raw["block"])
		x.through = pred("continue_through")
		x.canReplace = pred("can_replace")
		x.cannotOn = blocks("cannot_place_on")
		x.clusterReach, x.count = ip("cluster_reach"), ip("column_count")
		x.columnReach, x.height = ip("column_reach"), ip("height")
	case "huge_fungus":
		var hat string
		_ = json.Unmarshal(f.raw["hat_state"], &hat)
		x.warped = strings.Contains(hat, "warped")
	default:
		return false
	}
	f.ext3 = x
	return true
}

// placeExtra3 places this file's features; handled false otherwise.
func (e *vpExec) placeExtra3(f *vpFeature, r *vwRandom, p vpPos) (bool, bool) {
	switch f.typ {
	case "overlay":
		any := false
		for _, pf := range f.choose {
			if e.placePlaced(pf, r, p) {
				any = true
			}
		}
		return any, true
	case "random_neighbor_spread":
		return e.placeNeighborSpread(f.extra3(), r, p), true
	case "single_block_pillar":
		return e.placePillar(f.extra3(), r, p), true
	case "projected_random_patchy_square":
		return e.placePatchySquare(f.extra3(), r, p), true
	case "netherrack_replace_blobs":
		return e.placeReplaceBlobs(f.extra3(), r, p), true
	case "delta_feature":
		return e.placeDelta(f.extra3(), r, p), true
	case "stepped_column_cluster":
		return e.placeSteppedColumns(f.extra3(), r, p), true
	case "huge_fungus":
		return PlaceHugeFungus(vpTreeRNG{r}, p.x, p.y, p.z, f.extra3().warped, false, e.fungusDriver()), true
	case "end_spike":
		return false, true // the End's generator stamps the spikes itself
	case "end_platform":
		for dz := -2; dz <= 2; dz++ {
			for dx := -2; dx <= 2; dx++ {
				for dy := -1; dy < 3; dy++ {
					s := Air
					if dy == -1 {
						s = Obsidian
					}
					e.set(p.x+dx, p.y+dy, p.z+dz, s)
				}
			}
		}
		return true, true
	case "chorus_plant", "end_gateway", "end_island":
		if e.g == nil {
			return false, true
		}
		reg := &endRegion{g: e.g, ch: e.ch, baseX: e.baseX, baseZ: e.baseZ}
		switch f.typ {
		case "chorus_plant":
			if reg.read(p.x, p.y-1, p.z) == EndStone && reg.read(p.x, p.y, p.z) == Air {
				e.g.chorusPlant(vpTreeRNG{r}, reg, p.x, p.y, p.z)
				return true, true
			}
			return false, true
		case "end_gateway":
			e.g.endGatewayFrame(reg, p.x, p.y, p.z)
		case "end_island":
			e.g.endIsland(vpTreeRNG{r}, reg, p.x, p.y, p.z)
		}
		return true, true
	}
	return false, false
}

// fungusDriver is the engine's FungusDriver over this placement: reads
// of the terrain and the feature's own writes, writes clipped to the chunk.
func (e *vpExec) fungusDriver() FungusDriver {
	return FungusDriver{
		Read:    e.get,
		Set:     e.set,
		Destroy: func(x, y, z int) { e.set(x, y, z, Air) },
		InWorld: func(y int) bool { return !e.ctx.outside(y) },
	}
}

// placeNeighborSpread is RandomNeighborSpreadFeature.
func (e *vpExec) placeNeighborSpread(x *vpExtra3, r *vwRandom, p vpPos) bool {
	e.set(p.x, p.y, p.z, vpGetState(x.pr, e.ctx, r, p))
	n := x.attempts.sample(r)
	for i := 0; i < n; i++ {
		dx := x.xz.sample(r)
		dy := x.y.sample(r)
		dz := x.xz.sample(r)
		q := vpPos{p.x + dx, p.y + dy, p.z + dz}
		if !x.canReplace.test(e.ctx, q) {
			continue
		}
		neighbours := 0
		for _, f := range vpFaces {
			if vpInRanges(x.accepted, e.get(q.x+f.d[0], q.y+f.d[1], q.z+f.d[2])) {
				neighbours++
			}
			if neighbours > 1 {
				break
			}
		}
		if neighbours == 1 {
			e.set(q.x, q.y, q.z, vpGetState(x.pr, e.ctx, r, q))
		}
	}
	return true
}

// placePillar is SingleBlockPillarFeature.
func (e *vpExec) placePillar(x *vpExtra3, r *vwRandom, p vpPos) bool {
	q := p
	for x.canReplace.test(e.ctx, q) && r.nextFloat() < x.chance && !e.ctx.outside(q.y) {
		e.set(q.x, q.y, q.z, vpGetState(x.pr, e.ctx, r, q))
		q = vpPos{q.x + x.dir[0], q.y + x.dir[1], q.z + x.dir[2]}
	}
	q = vpPos{q.x - x.dir[0], q.y - x.dir[1], q.z - x.dir[2]}
	if x.cap != nil {
		e.placePlaced(x.cap, r, q)
	}
	return true
}

// placePatchySquare is ProjectedRandomPatchySquare.
func (e *vpExec) placePatchySquare(x *vpExtra3, r *vwRandom, p vpPos) bool {
	size := x.size.sample(r)
	bound := size*size + 1
	for dx := -size; dx <= size; dx++ {
		for dz := -size; dz <= size; dz++ {
			if int(r.nextIntN(int32(bound))) >= bound-abs(dx)*abs(dz) {
				continue
			}
			b := vpPos{p.x + dx, p.y, p.z + dz}
			drop := x.maxProjection
			for x.through.test(e.ctx, vpPos{b.x, b.y - 1, b.z}) {
				b.y--
				if drop--; drop <= 0 {
					break
				}
			}
			if s, ok := x.pr(e.ctx, r, b); ok {
				e.set(b.x, b.y, b.z, s)
			}
		}
	}
	return true
}

// placeReplaceBlobs is ReplaceBlobsFeature.
func (e *vpExec) placeReplaceBlobs(x *vpExtra3, r *vwRandom, p vpPos) bool {
	target, _ := StateName(x.target)
	is := func(s uint32) bool { return vpIsBlock(s, target) }
	c := p
	c.y = min(max(c.y, e.ctx.minY+1), e.ctx.minY+e.ctx.height-1)
	for ; c.y > e.ctx.minY+1; c.y-- {
		if is(e.get(c.x, c.y, c.z)) {
			break
		}
	}
	if c.y <= e.ctx.minY+1 {
		return false
	}
	rx, ry, rz := x.radius.sample(r), x.radius.sample(r), x.radius.sample(r)
	maxR := max(rx, max(ry, rz))
	any := false
	vpManhattan(c, rx, ry, rz, rx+ry+rz, func(q vpPos) bool {
		if vpDist(q, c) > maxR {
			return false
		}
		if is(e.get(q.x, q.y, q.z)) {
			e.set(q.x, q.y, q.z, x.replace)
			any = true
		}
		return true
	})
	return any
}

// vpDeltaKeep is DeltaFeature.CANNOT_REPLACE.
var vpDeltaKeep = []string{"bedrock", "nether_bricks", "nether_brick_fence", "nether_brick_stairs", "nether_wart", "chest", "spawner"}

// placeDelta is DeltaFeature.
func (e *vpExec) placeDelta(x *vpExtra3, r *vwRandom, p vpPos) bool {
	contents, _ := StateName(x.contents)
	clear := func(q vpPos) bool {
		s := e.get(q.x, q.y, q.z)
		if vpIsBlock(s, contents) {
			return false
		}
		for _, k := range vpDeltaKeep {
			if vpIsBlock(s, k) {
				return false
			}
		}
		for i, f := range vpFaces {
			air := e.get(q.x+f.d[0], q.y+f.d[1], q.z+f.d[2]) == Air
			if air && i != 1 || !air && i == 1 {
				return false
			}
		}
		return true
	}
	spawnRim := r.nextDouble() < 0.9
	rimX, rimZ := 0, 0
	if spawnRim {
		rimX = x.rimSize.sample(r)
		rimZ = x.rimSize.sample(r)
	}
	hasRim := spawnRim && rimX != 0 && rimZ != 0
	rx, rz := x.size.sample(r), x.size.sample(r)
	limit := max(rx, rz)
	any := false
	vpManhattan(p, rx, 0, rz, rx+rz, func(q vpPos) bool {
		if vpDist(q, p) > limit {
			return false
		}
		if clear(q) {
			if hasRim {
				any = true
				e.set(q.x, q.y, q.z, x.rim)
			}
			o := vpPos{q.x + rimX, q.y, q.z + rimZ}
			if clear(o) {
				any = true
				e.set(o.x, o.y, o.z, x.contents)
			}
		}
		return true
	})
	return any
}

// placeSteppedColumns is SteppedColumnClusterFeature.
func (e *vpExec) placeSteppedColumns(x *vpExtra3, r *vwRandom, p vpPos) bool {
	canPlaceAt := func(q vpPos) bool {
		if !x.canReplace.test(e.ctx, q) {
			return false
		}
		b := e.get(q.x, q.y-1, q.z)
		return b != Air && !vpInRanges(x.cannotOn, b)
	}
	if !canPlaceAt(p) {
		return false
	}
	height := x.height.sample(r)
	reach := min(height, x.clusterReach.sample(r))
	count := x.count.sample(r)
	placed := false
	w := 2*reach + 1
	for i := 0; i < count; i++ {
		q := vpPos{p.x - reach + int(r.nextIntN(int32(w))), p.y + int(r.nextIntN(1)), p.z - reach + int(r.nextIntN(int32(w)))}
		up := height - vpDist(q, p)
		if up < 0 {
			continue
		}
		creach := x.columnReach.sample(r)
		for cz := q.z - creach; cz <= q.z+creach; cz++ {
			for cx := q.x - creach; cx <= q.x+creach; cx++ {
				c := vpPos{cx, q.y, cz}
				step := vpDist(c, q)
				var at vpPos
				found := false
				if x.canReplace.test(e.ctx, c) {
					for lim, cur := step, c; cur.y > e.ctx.minY+1 && lim > 0; cur.y-- {
						lim--
						if canPlaceAt(cur) {
							at, found = cur, true
							break
						}
					}
				} else {
					for lim, cur := step, c; cur.y <= e.ctx.minY+e.ctx.height-1 && lim > 0; cur.y++ {
						lim--
						s := e.get(cur.x, cur.y, cur.z)
						if vpInRanges(x.cannotOn, s) {
							break
						}
						if s == Air {
							at, found = cur, true
							break
						}
					}
				}
				if !found {
					continue
				}
				for n, cur := up-step/2, at; n >= 0; n-- {
					if x.canReplace.test(e.ctx, cur) {
						e.set(cur.x, cur.y, cur.z, vpGetState(x.pr, e.ctx, r, cur))
						cur.y++
						placed = true
					} else {
						if !x.through.test(e.ctx, cur) {
							break
						}
						cur.y++
					}
				}
			}
		}
	}
	return placed
}
