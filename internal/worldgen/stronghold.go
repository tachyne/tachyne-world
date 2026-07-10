package worldgen

// Strongholds: buried stone-brick portal rooms, far from spawn, holding the
// ring of twelve end-portal frames. Pure seed functions like every structure.

const (
	strongholdCell = 1536
	strongholdOdds = 0.6
	strongholdY    = 10 // portal-room floor (absolute — deep under any terrain)
)

var (
	EndPortalFrame = blockBase("end_portal_frame") // + eye(2)×4 + facing(4); eye=true is the LOW half
)

// Stronghold describes one generated portal room.
type Stronghold struct {
	X, Z   int // portal-room centre (the 3x3 portal interior centre)
	Y      int // room floor
	Exists bool
}

// StrongholdIn rolls the stronghold for the cell containing (wx,wz). The cell
// around the origin never generates one (vanilla: the first ring is ~1500+).
func (g *Generator) StrongholdIn(wx, wz int) Stronghold {
	ox, oz := cellOrigin(wx, strongholdCell), cellOrigin(wz, strongholdCell)
	if ox == cellOrigin(0, strongholdCell) && oz == cellOrigin(0, strongholdCell) {
		return Stronghold{}
	}
	if hash01(g.seed, ox, oz, 0x57011) >= strongholdOdds {
		return Stronghold{}
	}
	x := ox + 200 + int(hash01(g.seed, ox, oz, 0x5702)*float64(strongholdCell-400))
	z := oz + 200 + int(hash01(g.seed, ox, oz, 0x5703)*float64(strongholdCell-400))
	return Stronghold{X: x, Z: z, Y: strongholdY, Exists: true}
}

// FramePositions lists the twelve end-portal frames around the 3x3 interior,
// with each frame's block state (facing inward; ~10% generate with an eye).
func (st Stronghold) FramePositions(seed int64) [12]struct {
	X, Y, Z int
	State   uint32
} {
	var out [12]struct {
		X, Y, Z int
		State   uint32
	}
	// facing values [north south west east] → index; frames face the centre.
	i := 0
	add := func(x, z int, facing uint32) {
		eye := uint32(4) // eye=false half
		if hash01(seed, x, z, 0x5704) < 0.1 {
			eye = 0
		}
		out[i].X, out[i].Y, out[i].Z = x, st.Y, z
		out[i].State = EndPortalFrame + eye + facing
		i++
	}
	for dx := -1; dx <= 1; dx++ {
		add(st.X+dx, st.Z-2, 1) // north row faces south
		add(st.X+dx, st.Z+2, 0) // south row faces north
	}
	for dz := -1; dz <= 1; dz++ {
		add(st.X-2, st.Z+dz, 3) // west column faces east
		add(st.X+2, st.Z+dz, 2) // east column faces west
	}
	return out
}

// stampStrongholds writes the portal room parts inside this chunk.
func (g *Generator) stampStrongholds(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for ddx := -1; ddx <= 1; ddx++ {
		for ddz := -1; ddz <= 1; ddz++ {
			st := g.StrongholdIn(baseX+8+ddx*strongholdCell, baseZ+8+ddz*strongholdCell)
			if !st.Exists {
				continue
			}
			g.stampPortalRoom(ch, baseX, baseZ, st)
		}
	}
}

func (g *Generator) stampPortalRoom(ch *Chunk, baseX, baseZ int, st Stronghold) {
	const half = 7 // 15x15 shell
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := baseX+lx, baseZ+lz
			dx, dz := wx-st.X, wz-st.Z
			if dx < -half || dx > half || dz < -half || dz > half {
				continue
			}
			wall := dx == -half || dx == half || dz == -half || dz == half
			for wy := st.Y - 1; wy <= st.Y+6; wy++ {
				switch {
				case wy == st.Y-1 || wy == st.Y+6 || wall:
					b := StoneBricks
					switch r := hash01(g.seed, wx*3+wy, wz*7, 0x5705); {
					case r < 0.2:
						b = MossyStoneBricks
					case r < 0.35:
						b = CrackedStoneBricks
					}
					setSectionBlock(ch, lx, wy, lz, b, true)
				default:
					setSectionBlock(ch, lx, wy, lz, Air, true)
				}
			}
			// The portal dais: lava pool under the 3x3 interior.
			if dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1 {
				setSectionBlock(ch, lx, st.Y-1, lz, Lava, true)
			}
			if (dx == 0 || dz == 0) && (dx == -half+1 || dx == half-1 || dz == -half+1 || dz == half-1) {
				setSectionBlock(ch, lx, st.Y+3, lz, Torch, true) // wall-ish light
			}
		}
	}
	// The frame ring (overwrites whatever the shell pass wrote).
	for _, f := range st.FramePositions(g.seed) {
		setSectionBlock(ch, f.X-baseX, f.Y, f.Z-baseZ, f.State, true)
	}
}
