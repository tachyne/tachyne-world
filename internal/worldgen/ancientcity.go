package worldgen

// Ancient City — a rare deep_dark chamber: a deepslate-brick vault with
// reinforced-deepslate corner pillars and central dais, a sculk-carpeted floor
// studded with can-summon shriekers and a catalyst, soul lanterns, and a loot
// chest routed to the vanilla chests/ancient_city table. A faithful-feel
// stand-in for the vanilla jigsaw megastructure (no template pieces here), gated
// to where the deep_dark biome actually generates so it fits its surroundings.

const (
	ancientCityCell   = 512  // one candidate per 512-block cell
	ancientCityOdds   = 0.55 // of qualifying (deep_dark) cells
	ancientCityRadius = 8    // half-footprint → a 17×17 vault
	ancientCityY      = -48  // deep in the deep-dark band
)

// AncientCity is a placed vault (or the zero value when a cell has none).
type AncientCity struct {
	X, Y, Z        int
	ChestX, ChestY int
	ChestZ         int
	Exists         bool
}

// AncientCityIn returns the vault owning the cell that contains (wx,wz), if any.
// Deterministic: existence, position, and layout are pure functions of the cell.
func (g *Generator) AncientCityIn(wx, wz int) AncientCity {
	ox, oz := cellOrigin(wx, ancientCityCell), cellOrigin(wz, ancientCityCell)
	if hash01(g.seed, ox, oz, 0xAC00) >= ancientCityOdds {
		return AncientCity{}
	}
	x := ox + 96 + int(hash01(g.seed, ox, oz, 0xAC01)*float64(ancientCityCell-192))
	z := oz + 96 + int(hash01(g.seed, ox, oz, 0xAC02)*float64(ancientCityCell-192))
	if g.caveBiome(x, z, ancientCityY) != "minecraft:deep_dark" {
		return AncientCity{} // only where deep_dark actually lives
	}
	return AncientCity{
		X: x, Y: ancientCityY, Z: z,
		ChestX: x + 3, ChestY: ancientCityY + 1, ChestZ: z,
		Exists: true,
	}
}

// stampAncientCity stamps the portion of the vault (if any) that overlaps this
// chunk. The 96-block cell margin keeps a vault well clear of cell borders, so
// only the chunk-centre's own cell can own it.
func (g *Generator) stampAncientCity(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	c := g.AncientCityIn(baseX+8, baseZ+8)
	if !c.Exists {
		return
	}
	brick := blockBase("deepslate_bricks")
	tiles := blockBase("deepslate_tiles")
	reinf := blockBase("reinforced_deepslate")
	lantern := blockBase("soul_lantern")
	R := ancientCityRadius
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := baseX+lx, baseZ+lz
			dx, dz := wx-c.X, wz-c.Z
			if dx < -R || dx > R || dz < -R || dz > R {
				continue
			}
			edge := dx == -R || dx == R || dz == -R || dz == R
			for wy := c.Y - 1; wy <= c.Y+6; wy++ {
				switch {
				case wy == c.Y-1, wy == c.Y+6, edge: // foundation, ceiling, walls
					chSet(ch, lx, wy, lz, brick)
				case wy == c.Y: // floor: sculk carpet checker-boarded with tiles
					if (dx+dz)&1 == 0 {
						chSet(ch, lx, wy, lz, wgSculk)
					} else {
						chSet(ch, lx, wy, lz, tiles)
					}
				default: // hollow the interior
					chSet(ch, lx, wy, lz, Air)
				}
			}
			switch {
			case (dx == R-1 || dx == -(R-1)) && (dz == R-1 || dz == -(R-1)): // corner pillars
				for wy := c.Y; wy <= c.Y+5; wy++ {
					chSet(ch, lx, wy, lz, reinf)
				}
				chSet(ch, lx, c.Y+5, lz, lantern)
			case dx == 0 && dz == 0: // central dais + catalyst
				chSet(ch, lx, c.Y, lz, reinf)
				chSet(ch, lx, c.Y+1, lz, wgSculkCatalyst+1)
			case wx == c.ChestX && wz == c.ChestZ: // loot chest
				chSet(ch, lx, c.Y+1, lz, ChestNorth)
			case !edge && sculk01(g.seed, wx, c.Y, wz, 0xAC10) < 0.04: // scattered shriekers
				chSet(ch, lx, c.Y+1, lz, wgSculkShrieker+3) // can_summon → Warden path
			}
		}
	}
}
