package worldgen

import "math"

// Dripstone — the dripstone caves' three features, per vanilla's
// placements: dripstone clusters (SpeleothemClusterFeature ×48–96: a patch
// of columns each growing a stalactite and a stalagmite of biased height,
// some with a pool on the floor), large dripstone (LargeDripstoneFeature
// ×10–48: a wide stalactite and stalagmite pair whose profile follows
// vanilla's curve, leaning with the wind), and pointed dripstone
// (SpeleothemFeature ×192–256, one to five each about a spot: a spike of
// one or two on a floor or a ceiling, seeding dripstone blocks around its
// root).

var (
	DripstoneBlock   = blockBase("dripstone_block")
	pointedDripstone = blockID("pointed_dripstone")
)

func dripstoneReplaceable(s uint32) bool { return inAnyRange(s, caveBaseStone) } // #dripstone_replaceable = #base_stone_overworld
func isDripBase(s uint32) bool           { return s == DripstoneBlock || dripstoneReplaceable(s) }
func emptyOrWater(s uint32) bool         { return s == Air || s == Water }
func emptyOrWaterOrLava(s uint32) bool   { return s == Air || s == Water || s == Lava }

// gaussian is Mth.normal's draw from a TreeRNG (Box–Muller).
func gaussian(r TreeRNG) float64 {
	u1 := r.Float64()
	if u1 < 1e-12 {
		u1 = 1e-12
	}
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*r.Float64())
}

func clampedNormal(r TreeRNG, mean, dev, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, mean+gaussian(r)*dev))
}

// columnScan is Column.scan: from an inside cell, up and down to at most
// range for an edge; the floor and ceiling found (ok=false when absent).
func (reg *owRegion) columnScan(x, y, z, rng int, inside, edge func(uint32) bool) (floor, ceiling int, hasFloor, hasCeil, ok bool) {
	if !inside(reg.read(x, y, z)) {
		return 0, 0, false, false, false
	}
	py := y
	for i := 1; i < rng && inside(reg.read(x, py, z)); i++ {
		py++
	}
	if edge(reg.read(x, py, z)) {
		ceiling, hasCeil = py, true
	}
	py = y
	for i := 1; i < rng && inside(reg.read(x, py, z)); i++ {
		py--
	}
	if edge(reg.read(x, py, z)) {
		floor, hasFloor = py, true
	}
	return floor, ceiling, hasFloor, hasCeil, true
}

// dripstoneFeatures adds the dripstone caves' features to a chunk's draws.
func (g *Generator) dripstoneFeatures(r TreeRNG, reg *owRegion, ox, oz int) {
	rangeY := func() int { return MinY + r.Intn(256-MinY+1) }
	drip := func(x, y, z int) bool { return reg.caveBiomeAt(x, y, z) == "minecraft:dripstone_caves" }
	for i, n := 0, 48+r.Intn(49); i < n; i++ { // DRIPSTONE_CLUSTER
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if drip(x, y, z) {
			g.speleothemCluster(r, reg, x, y, z)
		}
	}
	for i, n := 0, 10+r.Intn(39); i < n; i++ { // LARGE_DRIPSTONE
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if drip(x, y, z) {
			g.largeDripstone(r, reg, x, y, z)
		}
	}
	for i, n := 0, 192+r.Intn(65); i < n; i++ { // POINTED_DRIPSTONE
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		for k, m := 0, 1+r.Intn(5); k < m; k++ {
			px := x + int(clampedNormal(r, 0, 3, -10, 10))
			pz := z + int(clampedNormal(r, 0, 3, -10, 10))
			py := y + int(clampedNormal(r, 0, 0.6, -2, 2))
			if !drip(px, py, pz) {
				continue
			}
			if r.Intn(2) == 0 { // the selector: a spike on the floor
				if fy, ok := reg.scanForWet(px, py, pz, -1, 12); ok {
					g.pointedDripstone(r, reg, px, fy+1, pz)
				}
			} else { // or under the ceiling
				if cy, ok := reg.scanForWet(px, py, pz, +1, 12); ok {
					g.pointedDripstone(r, reg, px, cy-1, pz)
				}
			}
		}
	}
}

// scanForWet is EnvironmentScanPlacement through air or water for a solid.
func (reg *owRegion) scanForWet(x, y, z, dy, max int) (int, bool) {
	if !emptyOrWater(reg.read(x, y, z)) {
		return 0, false
	}
	for i := 0; i < max; i++ {
		if solid(reg.read(x, y, z)) {
			return y, true
		}
		y += dy
		if y < MinY || y >= MinY+len(reg.ch.Sections)*16 {
			return 0, false
		}
		if !emptyOrWater(reg.read(x, y, z)) {
			break
		}
	}
	if solid(reg.read(x, y, z)) {
		return y, true
	}
	return 0, false
}

// pointedState is createPointedBlock, waterlogged where the cell is water.
func (reg *owRegion) pointedState(x, y, z int, down bool, thickness string) uint32 {
	dir := "up"
	if down {
		dir = "down"
	}
	wet := "false"
	if reg.read(x, y, z) == Water {
		wet = "true"
	}
	return withProps("pointed_dripstone", "vertical_direction", dir, "thickness", thickness, "waterlogged", wet)
}

// growSpeleothem is SpeleothemUtils.growSpeleothem: from start toward the
// tip, base and middles, a frustum, and the tip (merged or not), given a
// base block behind the start.
func (g *Generator) growSpeleothem(reg *owRegion, x, y, z int, down bool, height int, merged bool) {
	dy := 1
	if down {
		dy = -1
	}
	if !isDripBase(reg.read(x, y-dy, z)) {
		return
	}
	var parts []string
	if height >= 3 {
		parts = append(parts, "base")
		for i := 0; i < height-3; i++ {
			parts = append(parts, "middle")
		}
	}
	if height >= 2 {
		parts = append(parts, "frustum")
	}
	if height >= 1 {
		if merged {
			parts = append(parts, "tip_merge")
		} else {
			parts = append(parts, "tip")
		}
	}
	for _, t := range parts {
		reg.set(x, y, z, reg.pointedState(x, y, z, down, t))
		y += dy
	}
}

// placeBaseIfPossible turns replaceable rock into dripstone block.
func (reg *owRegion) placeBaseIfPossible(x, y, z int) bool {
	if dripstoneReplaceable(reg.read(x, y, z)) {
		reg.set(x, y, z, DripstoneBlock)
		return true
	}
	return false
}

// speleothemCluster is SpeleothemClusterFeature with DRIPSTONE_CLUSTER's
// configuration (search 12, height 3–6, radius 2–8, height diff 1,
// deviation 3, layer 2–4, density 0.3–0.7, wetness N(0.1..0.3), chance at
// the edge 0.1 over 3, height bias over 8).
func (g *Generator) speleothemCluster(r TreeRNG, reg *owRegion, x, y, z int) {
	if !emptyOrWater(reg.read(x, y, z)) {
		return
	}
	height := 3 + r.Intn(4)
	wetness := clampedNormal(r, 0.1, 0.3, 0.1, 0.9)
	density := 0.3 + r.Float64()*0.4
	xr, zr := 2+r.Intn(7), 2+r.Intn(7)
	chanceAt := func(dx, dz int) float64 { // getChanceOfStalagmiteOrStalactite
		d := xr - absInt(dx)
		if e := zr - absInt(dz); e < d {
			d = e
		}
		t := float64(d) / 3
		if t > 1 {
			t = 1
		}
		return 0.1 + t*0.9
	}
	speleoHeight := func(dx, dz, maxH int) int {
		if r.Float64() > density {
			return 0
		}
		dist := absInt(dx) + absInt(dz)
		t := float64(dist) / 8
		if t > 1 {
			t = 1
		}
		mean := float64(maxH)/2*(1-t) + 0
		return int(clampedNormal(r, mean, 3, 0, float64(maxH)))
	}
	for dx := -xr; dx <= xr; dx++ {
		for dz := -zr; dz <= zr; dz++ {
			chance := chanceAt(dx, dz)
			px, pz := x+dx, z+dz
			floor, ceil, hasFloor, hasCeil, ok := reg.columnScan(px, y, pz, 12, emptyOrWater, func(s uint32) bool { return !emptyOrWater(s) })
			if !ok || (!hasFloor && !hasCeil) {
				continue
			}
			if r.Float64() < wetness && hasFloor && g.canPlacePool(reg, px, floor, pz) { // a pool in the floor
				reg.set(px, floor, pz, Water)
				floor--
			}
			stalactite := 0
			if hasCeil && r.Float64() < chance && reg.read(px, ceil, pz) != Lava {
				for i, n := 0, 2+r.Intn(3); i < n; i++ { // the ceiling turns to dripstone
					if !reg.placeBaseIfPossible(px, ceil+i, pz) {
						break
					}
				}
				maxH := height
				if hasFloor && ceil-floor < maxH {
					maxH = ceil - floor
				}
				stalactite = speleoHeight(dx, dz, maxH)
			}
			stalagmite := 0
			if hasFloor && r.Float64() < chance && reg.read(px, floor, pz) != Lava {
				for i, n := 0, 2+r.Intn(3); i < n; i++ {
					if !reg.placeBaseIfPossible(px, floor-i, pz) {
						break
					}
				}
				if hasCeil {
					stalagmite = stalactite + (r.Intn(3) - 1)
					if stalagmite < 0 {
						stalagmite = 0
					}
				} else {
					stalagmite = speleoHeight(dx, dz, height)
				}
			}
			if hasCeil && hasFloor && ceil-stalactite <= floor+stalagmite { // they would meet: split the gap
				lowest := ceil - stalactite
				if lowest < floor+1 {
					lowest = floor + 1
				}
				highest := floor + stalagmite
				if highest > ceil-1 {
					highest = ceil - 1
				}
				bottom := lowest + r.Intn(highest+1-lowest+1)
				stalactite = ceil - bottom
				stalagmite = bottom - 1 - floor
			}
			merge := r.Intn(2) == 0 && stalactite > 0 && stalagmite > 0 && hasCeil && hasFloor && stalactite+stalagmite == ceil-floor-1
			if hasCeil {
				g.growSpeleothem(reg, px, ceil-1, pz, true, stalactite, merge)
			}
			if hasFloor {
				g.growSpeleothem(reg, px, floor+1, pz, false, stalagmite, merge)
			}
		}
	}
}

// canPlacePool is SpeleothemClusterFeature.canPlacePool: a floor cell that
// is not water or dripstone, no water above, stone or water on all four
// sides and below.
func (g *Generator) canPlacePool(reg *owRegion, x, y, z int) bool {
	s := reg.read(x, y, z)
	if s == Water || s == DripstoneBlock || (s >= pointedLo && s <= pointedHi) {
		return false
	}
	if reg.read(x, y+1, z) == Water {
		return false
	}
	adj := func(px, py, pz int) bool {
		n := reg.read(px, py, pz)
		return inAnyRange(n, caveBaseStone) || n == Water
	}
	for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if !adj(x+o[0], y, z+o[1]) {
			return false
		}
	}
	return adj(x, y-1, z)
}

// pointedDripstone is SpeleothemFeature: a spike where a base lies above
// or below, its root patch spreading dripstone into the rock (0.7 a side,
// 0.5 further, 0.5 further), two tall one time in five when the way is
// clear.
func (g *Generator) pointedDripstone(r TreeRNG, reg *owRegion, x, y, z int) {
	if !emptyOrWater(reg.read(x, y, z)) {
		return
	}
	above, below := isDripBase(reg.read(x, y+1, z)), isDripBase(reg.read(x, y-1, z))
	var down bool
	switch {
	case above && below:
		down = r.Intn(2) == 0
	case above:
		down = true
	case below:
		down = false
	default:
		return
	}
	ry := y + 1
	if !down {
		ry = y - 1
	}
	reg.placeBaseIfPossible(x, ry, z)
	dirs := [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	for _, o := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		if r.Float64() > 0.7 {
			continue
		}
		p1 := [3]int{x + o[0], ry, z + o[1]}
		reg.placeBaseIfPossible(p1[0], p1[1], p1[2])
		if r.Float64() > 0.5 {
			continue
		}
		d := dirs[r.Intn(6)]
		p2 := [3]int{p1[0] + d[0], p1[1] + d[1], p1[2] + d[2]}
		reg.placeBaseIfPossible(p2[0], p2[1], p2[2])
		if r.Float64() > 0.5 {
			continue
		}
		d = dirs[r.Intn(6)]
		reg.placeBaseIfPossible(p2[0]+d[0], p2[1]+d[1], p2[2]+d[2])
	}
	height := 1
	ty := y - 1
	if !down {
		ty = y + 1
	}
	if r.Float64() < 0.2 && emptyOrWater(reg.read(x, ty, z)) {
		height = 2
	}
	g.growSpeleothem(reg, x, y, z, down, height, false)
}

// largeDripstone is LargeDripstoneFeature with LARGE_DRIPSTONE's
// configuration (search 30, radius 3–19 clamped 3–16, height scale
// 0.4–2.0, radius/height 0.33, stalactite bluntness 0.3–0.9, stalagmite
// 0.4–1.0, wind 0–0.3, wind from radius 4 and bluntness 0.6).
func (g *Generator) largeDripstone(r TreeRNG, reg *owRegion, x, y, z int) {
	if !emptyOrWater(reg.read(x, y, z)) {
		return
	}
	floor, ceil, hasFloor, hasCeil, ok := reg.columnScan(x, y, z, 30, emptyOrWater, func(s uint32) bool { return isDripBase(s) || s == Lava })
	if !ok || !hasFloor || !hasCeil {
		return
	}
	caveH := ceil - floor - 1
	if caveH < 4 {
		return
	}
	maxR := int(float64(caveH) * 0.33)
	if maxR < 3 {
		maxR = 3
	}
	if maxR > 16 {
		maxR = 16
	}
	radius := 3 + r.Intn(maxR-3+1)
	type drip struct {
		rootY     int
		up        bool
		radius    int
		bluntness float64
		scale     float64
	}
	stalactite := drip{ceil - 1, false, radius, 0.3 + r.Float64()*0.6, 0.4 + r.Float64()*1.6}
	stalagmite := drip{floor + 1, true, radius, 0.4 + r.Float64()*0.6, 0.4 + r.Float64()*1.6}
	heightAt := func(d drip, cr float64) int {
		if cr == 0 {
			// getSpeleothemHeight at the centre: r → 0 makes log(r) → −∞; vanilla's
			// float maths lands on the large finite value scale·(0 − 0 − ⅓·log r)
			cr = 0.001
		}
		rr := cr / float64(d.radius) * 0.384
		h := d.scale * (0.75*math.Pow(rr, 4.0/3) - math.Pow(rr, 2.0/3) - math.Log(rr)/3)
		if h < 0 {
			h = 0
		}
		return int(h / 0.384 * float64(d.radius))
	}
	// wind
	var windX, windZ float64
	windOn := stalactite.radius >= 4 && stalactite.bluntness >= 0.6 && stalagmite.radius >= 4 && stalagmite.bluntness >= 0.6
	if windOn {
		speed := r.Float64() * 0.3
		dir := r.Float64() * math.Pi
		windX, windZ = math.Cos(dir)*speed, math.Sin(dir)*speed
	}
	maxOff := 16 - radius
	offset := func(px, py, pz int) (int, int) {
		if !windOn {
			return px, pz
		}
		dy := float64(y - py)
		dx := int(math.Floor(windX * dy))
		dz := int(math.Floor(windZ * dy))
		if dx < -maxOff {
			dx = -maxOff
		} else if dx > maxOff {
			dx = maxOff
		}
		if dz < -maxOff {
			dz = -maxOff
		} else if dz > maxOff {
			dz = maxOff
		}
		return px + dx, pz + dz
	}
	embedded := func(cx, cy, cz, rad int) bool { // isCircleMostlyEmbeddedInStone
		if emptyOrWaterOrLava(reg.read(cx, cy, cz)) {
			return false
		}
		for a := 0.0; a < 2*math.Pi; a += 6.0 / float64(rad) {
			dx, dz := int(math.Cos(a)*float64(rad)), int(math.Sin(a)*float64(rad))
			if emptyOrWaterOrLava(reg.read(cx+dx, cy, cz+dz)) {
				return false
			}
		}
		return true
	}
	settle := func(d *drip) bool { // moveBackUntilBaseIsInsideStoneAndShrinkRadiusIfNecessary
		for d.radius > 1 {
			ny := d.rootY
			tries := heightAt(*d, 0)
			if tries > 10 {
				tries = 10
			}
			for i := 0; i < tries; i++ {
				if reg.read(x, ny, z) == Lava {
					return false
				}
				wx, wz := offset(x, ny, z)
				if embedded(wx, ny, wz, d.radius) {
					d.rootY = ny
					return true
				}
				if d.up {
					ny--
				} else {
					ny++
				}
			}
			d.radius /= 2
		}
		return false
	}
	place := func(d drip) {
		for dx := -d.radius; dx <= d.radius; dx++ {
			for dz := -d.radius; dz <= d.radius; dz++ {
				cr := math.Sqrt(float64(dx*dx + dz*dz))
				if cr > float64(d.radius) {
					continue
				}
				h := heightAt(d, cr)
				if h <= 0 {
					continue
				}
				if r.Float64() < 0.2 {
					h = int(float64(h) * (0.8 + r.Float64()*0.2))
				}
				px, py, pz := x+dx, d.rootY, z+dz
				out := false
				maxY := 1 << 30
				if d.up {
					maxY = reg.col(px, pz).h
				}
				for i := 0; i < h && py < maxY; i++ {
					wx, wz := offset(px, py, pz)
					s := reg.read(wx, py, wz)
					if emptyOrWaterOrLava(s) {
						out = true
						reg.set(wx, py, wz, DripstoneBlock)
					} else if out && inAnyRange(s, caveBaseStone) {
						break
					}
					if d.up {
						py++
					} else {
						py--
					}
				}
			}
		}
	}
	okT := settle(&stalactite)
	okM := settle(&stalagmite)
	if okT {
		place(stalactite)
	}
	if okM {
		place(stalagmite)
	}
}

var pointedLo, pointedHi = BlockRange("pointed_dripstone")
