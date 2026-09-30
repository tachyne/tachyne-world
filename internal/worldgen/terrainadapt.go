package worldgen

import (
	"math"
	"strings"
)

// Terrain adaptation: vanilla's Beardifier for the structures that declare
// one in 26.3 — pillager outposts (beard_thin), ancient cities (beard_box),
// trial chambers (encapsulate), trail ruins and strongholds (bury).
//
// Vanilla adds the Beardifier to the terrain's density before any block is
// chosen: each rigid piece near the chunk adds a positive density below its
// ground (the terrain rises to meet it) and, for the beards, a negative one
// above (the terrain is shaved back); bury and encapsulate only ever add,
// closing the caves round a buried structure. The same formulas are here:
// the 24³ Gaussian beard kernel, the bury cone (1 − d/6), the per-kind
// distances (to the ground for bury and beard_thin, outside the box's
// vertical span for beard_box and encapsulate, halved for bury's y and all
// of encapsulate's axes) and the 0.8 weights. Every rigid piece of a jigsaw
// structure shares its start piece's ground level (its bottom plus one, as
// JigsawPlacement's groundLevelDelta works out); a stronghold piece's ground
// is its own bottom.
//
// tachyne's terrain is a heightfield carved by caves, not a density, so the
// adaptation is a post-pass over the generated chunk, run before the
// structures stamp: a cell's terrain density is modelled as a gradient of
// adaptGrad per block about the column's surface, capped at adaptSolid in
// rock, and adaptCave in a cave's air; a natural ground cell whose density
// plus the beard falls below zero becomes air, and an air, water or plant
// cell whose sum rises above zero becomes the column's own ground (its
// surface block on top where the new surface is near the old). Only natural
// ground is ever cut, a column holding a tree is never cut, and plants left
// over air are cleared.
//
// A structure with a player's build or dig anywhere within reach of its
// pieces is left unadapted, whole: the adaptation never lands in or under
// a build (buildguard.go).

const (
	adaptGrad  = 0.1 // terrain density per block about the surface
	adaptSolid = 0.3 // rock's density, and the most the gradient reaches
	adaptCave  = 0.3 // a cave's air
	adaptReach = 12  // BEARD_KERNEL_RADIUS: how far a piece reaches
)

// adaptKind is a TerrainAdjustment.
type adaptKind int

const (
	adaptBury adaptKind = iota
	adaptBeardThin
	adaptBeardBox
	adaptEncapsulate
)

// beardRigid is Beardifier.Rigid: a piece's box, its adjustment and its
// absolute ground level.
type beardRigid struct {
	box     fbox
	kind    adaptKind
	groundY int
}

// beardKernel is BEARD_KERNEL: exp(-d²/16) over a 24³ cube, the y offset
// by half a block, indexed [z][x][y].
var beardKernel = func() []float32 {
	k := make([]float32, 24*24*24)
	for zi := 0; zi < 24; zi++ {
		for xi := 0; xi < 24; xi++ {
			for yi := 0; yi < 24; yi++ {
				dx, dy, dz := float64(xi-12), float64(yi-12)+0.5, float64(zi-12)
				k[zi*24*24+xi*24+yi] = float32(math.Exp(-(dx*dx + dy*dy + dz*dz) / 16))
			}
		}
	}
	return k
}()

// buryContribution is getBuryContribution: a cone of radius six.
func buryContribution(dx, dy, dz float64) float64 {
	d2 := dx*dx + dy*dy + dz*dz
	if d2 >= 36 {
		return 0
	}
	return 1 - math.Sqrt(d2)/6
}

// beardContribution is getBeardContribution: positive below the ground,
// negative above, weighted by the kernel.
func beardContribution(dx, dy, dz, yToGround int) float64 {
	xi, yi, zi := dx+12, dy+12, dz+12
	if xi < 0 || xi >= 24 || yi < 0 || yi >= 24 || zi < 0 || zi >= 24 {
		return 0
	}
	dyo := float64(yToGround) + 0.5
	d2 := float64(dx*dx) + dyo*dyo + float64(dz*dz)
	v := -dyo / math.Sqrt(d2/2) / 2
	return v * float64(beardKernel[zi*24*24+xi*24+yi])
}

// beardValue is sampleValueUnchecked over the rigids.
func beardValue(rs []beardRigid, x, y, z int) float64 {
	sum := 0.0
	for i := range rs {
		r := &rs[i]
		b := r.box
		dx := max(0, max(b.x0-x, x-b.x1))
		dz := max(0, max(b.z0-z, z-b.z1))
		toGround := y - r.groundY
		switch r.kind {
		case adaptBury:
			sum += buryContribution(float64(dx), float64(toGround)/2, float64(dz))
		case adaptBeardThin:
			sum += beardContribution(dx, toGround, dz, toGround) * 0.8
		case adaptBeardBox:
			dy := max(0, max(r.groundY-y, y-b.y1))
			sum += beardContribution(dx, dy, dz, toGround) * 0.8
		case adaptEncapsulate:
			dy := max(0, max(b.y0-y, y-b.y1))
			sum += buryContribution(float64(dx)/2, float64(dy)/2, float64(dz)/2) * 0.8
		}
	}
	return sum
}

// adaptGround is the natural ground the adaptation may cut: the stones,
// soils, sands, terracottas and ores the terrain and its features lay.
var adaptGround = func() map[uint32]bool {
	m := map[uint32]bool{}
	names := []string{"stone", "deepslate", "granite", "diorite", "andesite", "tuff", "calcite", "dirt",
		"coarse_dirt", "rooted_dirt", "grass_block", "podzol", "mycelium", "mud", "sand", "red_sand", "gravel",
		"sandstone", "red_sandstone", "clay", "snow_block", "packed_ice", "smooth_basalt", "dripstone_block",
		"moss_block", "coal_ore", "deepslate_coal_ore", "iron_ore", "deepslate_iron_ore", "copper_ore",
		"deepslate_copper_ore", "gold_ore", "deepslate_gold_ore", "redstone_ore", "deepslate_redstone_ore",
		"lapis_ore", "deepslate_lapis_ore", "diamond_ore", "deepslate_diamond_ore", "emerald_ore",
		"deepslate_emerald_ore", "raw_iron_block", "raw_copper_block"}
	for _, n := range AllBlockNames() {
		if n == "terracotta" || strings.HasSuffix(n, "_terracotta") && !strings.Contains(n, "glazed") {
			names = append(names, n)
		}
	}
	for _, n := range names {
		lo, hi := BlockRange(n)
		for s := lo; s <= hi; s++ {
			m[s] = true
		}
	}
	return m
}()

// adaptRigids is Beardifier.forStructuresInChunk: the rigid pieces within
// reach of the chunk, of every adapting structure no player has touched.
func (g *Generator) adaptRigids(cx, cz int32) []beardRigid {
	baseX, baseZ := int(cx)*16, int(cz)*16
	near := func(b fbox) bool {
		return b.x1 >= baseX-adaptReach && b.x0 <= baseX+15+adaptReach &&
			b.z1 >= baseZ-adaptReach && b.z0 <= baseZ+15+adaptReach
	}
	var out []beardRigid
	// add keeps a structure's near pieces unless a build or dig lies
	// within reach of any of its pieces.
	add := func(set []beardRigid, all fbox) {
		if len(set) == 0 {
			return
		}
		if g.touchedIn(all.x0-adaptReach, all.y0-adaptReach, all.z0-adaptReach,
			all.x1+adaptReach, all.y1+adaptReach, all.z1+adaptReach) {
			return
		}
		out = append(out, set...)
	}
	jigsaw := func(pieces []PlacedPiece, kind adaptKind) {
		if len(pieces) == 0 {
			return
		}
		ground := pieces[0].OY + 1 // the start's groundLevelDelta, shared down every rigid chain
		var set []beardRigid
		var all fbox
		first := true
		for i := range pieces {
			p := &pieces[i]
			if p.Tmpl == nil || p.TerrainMatch {
				continue
			}
			b := fbox{p.OX, p.OY, p.OZ, p.x1 - 1, p.y1 - 1, p.z1 - 1}
			if first {
				all, first = b, false
			} else {
				all = all.union(b)
			}
			if near(b) {
				set = append(set, beardRigid{b, kind, ground})
			}
		}
		add(set, all)
	}
	// siteNear skips assembling a small structure whose site is too far
	// for any of its pieces to reach the chunk.
	siteNear := func(x, z int) bool { return absInt(x-baseX-8) <= 96 && absInt(z-baseZ-8) <= 96 }
	for _, off := range cellNeighbours(outpostCell) {
		if p := g.OutpostIn(baseX+8+off[0], baseZ+8+off[1]); p.Exists && siteNear(p.X, p.Z) {
			jigsaw(g.AssembleOutpost(p), adaptBeardThin)
		}
	}
	if a := g.AncientCityIn(baseX+8, baseZ+8); a.Exists {
		jigsaw(g.AssembleAncientCity(a), adaptBeardBox)
	}
	if t := g.TrialChamberIn(baseX+8, baseZ+8); t.Exists {
		jigsaw(g.AssembleTrialChamber(t), adaptEncapsulate)
	}
	for _, off := range cellNeighbours(trailRuinsCell) {
		if t := g.TrailRuinsIn(baseX+8+off[0], baseZ+8+off[1]); t.Exists && siteNear(t.X, t.Z) {
			jigsaw(g.AssembleTrailRuins(t), adaptBury)
		}
	}
	for _, st := range g.StrongholdsNear(baseX+8, baseZ+8) {
		var set []beardRigid
		var all fbox
		for i, p := range st.pieces {
			if i == 0 {
				all = p.box
			} else {
				all = all.union(p.box)
			}
			if near(p.box) {
				set = append(set, beardRigid{p.box, adaptBury, p.box.y0}) // a plain piece: groundLevelDelta 0
			}
		}
		add(set, all)
	}
	return out
}

// terrainAdaptOff skips the adaptation — for the tests that compare.
var terrainAdaptOff bool

// adaptTerrain runs the adaptation over the chunk (see the file comment).
func (g *Generator) adaptTerrain(ch *Chunk, cx, cz int32) {
	if g.nether || g.end || terrainAdaptOff {
		return
	}
	if rs := g.adaptRigids(cx, cz); len(rs) > 0 {
		g.adaptChunk(ch, cx, cz, rs)
	}
}

// adaptChunk adapts every column of the chunk a rigid reaches.
func (g *Generator) adaptChunk(ch *Chunk, cx, cz int32, rs []beardRigid) {
	top := MinY + len(ch.Sections)*16 - 1
	baseX, baseZ := int(cx)*16, int(cz)*16
	var local []beardRigid
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			x, z := baseX+lx, baseZ+lz
			// The rigids that reach this column, and the heights they reach.
			local = local[:0]
			y0, y1 := top+1, MinY-1
			for _, r := range rs {
				dx := max(0, max(r.box.x0-x, x-r.box.x1))
				dz := max(0, max(r.box.z0-z, z-r.box.z1))
				if dx >= adaptReach || dz >= adaptReach {
					continue
				}
				local = append(local, r)
				y0 = min(y0, min(r.box.y0, r.groundY)-adaptReach)
				y1 = max(y1, max(r.box.y1, r.groundY)+adaptReach)
			}
			if len(local) == 0 {
				continue
			}
			y0, y1 = max(y0, MinY+1), min(y1, top)
			g.adaptColumn(ch, lx, lz, x, z, y0, y1, local)
		}
	}
}

// adaptColumn adapts one column between y0 and y1.
func (g *Generator) adaptColumn(ch *Chunk, lx, lz, x, z, y0, y1 int, rs []beardRigid) {
	col := g.columnAt(x, z)
	tree := false
	for y := y0; y <= min(y1+adaptReach, MinY+len(ch.Sections)*16-1) && !tree; y++ {
		s := sectionBlockAt(ch, lx, y, lz)
		tree = IsLog(s) || IsLeaves(s)
	}
	changed := false
	cut := map[int]bool{} // the cells cut to air
	for y := y0; y <= y1; y++ {
		cur := sectionBlockAt(ch, lx, y, lz)
		var t float64
		ground := adaptGround[cur]
		switch {
		case ground:
			t = math.Max(math.Min(adaptGrad*(float64(col.h)-0.5-float64(y)), adaptSolid), 0.05)
		case cur == Air || IsFluid(cur) || IsReplaceable(cur) && y >= col.h:
			if y >= col.h {
				t = adaptGrad * (float64(col.h) - 0.5 - float64(y))
			} else {
				t = -adaptCave
			}
		default:
			continue // a tree, a feature, anything not the terrain's: left alone
		}
		b := beardValue(rs, x, y, z)
		switch {
		case ground && !tree && t+b < 0:
			setSectionBlock(ch, lx, y, lz, Air, true)
			changed, cut[y] = true, true
		case !ground && t+b > 0:
			fill := col.subBlock(y)
			if y < col.h {
				fill = col.block(y)
			}
			setSectionBlock(ch, lx, y, lz, fill, true)
			changed = true
		}
	}
	if !changed {
		return
	}
	// Clear the plants that stood on a cut cell (both halves of a double
	// plant), then dress the new surface near the old one with the
	// column's top block.
	for y := y0; y <= y1; y++ {
		if !cut[y] {
			continue
		}
		for py := y + 1; py <= min(y+2, MinY+len(ch.Sections)*16-1); py++ {
			s := sectionBlockAt(ch, lx, py, lz)
			if s == Air || IsFluid(s) || !IsReplaceable(s) || cut[py] {
				break
			}
			setSectionBlock(ch, lx, py, lz, Air, true)
		}
	}
	for y := min(y1+1, MinY+len(ch.Sections)*16-2); y >= y0; y-- {
		s := sectionBlockAt(ch, lx, y, lz)
		above := sectionBlockAt(ch, lx, y+1, lz)
		if adaptGround[s] && (above == Air || IsReplaceable(above)) && y >= col.h-4 && y <= col.h+adaptReach {
			if IsDirtTag(s) || s == col.subBlock(y) {
				setSectionBlock(ch, lx, y, lz, col.topBlock(), true)
			}
			break
		}
	}
}
