package worldgen

import "sync"

// Dungeons: vanilla's monster_room (ten tries a chunk, uniform from y=0 to
// the top) and monster_room_deep (four, from six above the bottom to y=-1),
// both MonsterRoomFeature. A try opens a room 5–7 wide and deep only where
// its floor and ceiling are solid all over and one to five two-high gaps
// open in its walls at floor level (so it meets a cave); the walls turn to
// cobblestone, the floor mostly to mossy cobblestone, any wall or floor
// cell over nothing to air (the floor's random gaps), the inside to air;
// then two rounds of three tries each put a chest against exactly one
// wall, and the spawner goes in the middle with a skeleton, zombie (twice
// as likely) or spider.
//
// A room is decided on the chunk's terrain as generated before decoration
// (the owRegion scratch view), and each origin chunk's tries run in order
// on their own stream, a placed room written into the view the later tries
// read. Every chunk pass and the server's spawner and loot lookups
// therefore see the same rooms without generating anything. A room with a
// player's build or a dug-out cell in its box is left out.
//
// The engine's old dungeons (one room per 48-block cell at 28%) are gone,
// except one a player has touched — built in or dug into — which stays as
// it was (legacyDungeonIn in structures.go).

// monsterRoomPlacement is one of the two placed features.
type monsterRoomPlacement struct {
	count  int
	height oreHeight
	salt   int64
}

var monsterRoomPlacements = [2]monsterRoomPlacement{
	{10, oreUniform(oreAbs(0), oreBelowTop(0)), 0xD0_0001},    // monster_room
	{4, oreUniform(oreAboveBottom(6), oreAbs(-1)), 0xD0_0002}, // monster_room_deep
}

// monsterRoomMobs is MonsterRoomFeature.MOBS as Dungeon.Mob values
// (0 zombie, 1 skeleton, 2 spider): skeleton, zombie, zombie, spider.
var monsterRoomMobs = [4]int{1, 0, 0, 2}

// roomKey caches one generator's rooms per origin chunk (legacy: per old
// 48-block cell).
type roomKey struct {
	g      *Generator
	cx, cz int32
	legacy bool
}

var (
	roomMu    sync.Mutex
	roomCache = map[roomKey][]Dungeon{}
)

const roomCacheCap = 1 << 15

func roomCacheGet(k roomKey) ([]Dungeon, bool) {
	roomMu.Lock()
	defer roomMu.Unlock()
	d, ok := roomCache[k]
	return d, ok
}

func roomCachePut(k roomKey, d []Dungeon) {
	roomMu.Lock()
	defer roomMu.Unlock()
	if len(roomCache) >= roomCacheCap {
		roomCache = map[roomKey][]Dungeon{}
	}
	roomCache[k] = d
}

// forgetRooms drops a generator's cached rooms: its edit overlay changed,
// and the build guard may answer differently.
func forgetRooms(g *Generator) {
	roomMu.Lock()
	defer roomMu.Unlock()
	for k := range roomCache {
		if k.g == g {
			delete(roomCache, k)
		}
	}
	for k := range geodeCache { // geode.go: the same guard
		if k.g == g {
			delete(geodeCache, k)
		}
	}
}

// monsterRooms is origin chunk (cx, cz)'s placed rooms, cached.
func (g *Generator) monsterRooms(cx, cz int32) []Dungeon {
	k := roomKey{g: g, cx: cx, cz: cz}
	if d, ok := roomCacheGet(k); ok {
		return d
	}
	ox, oz := int(cx)*16, int(cz)*16
	view := &owRegion{g: g, baseX: ox, baseZ: oz, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}}
	top := g.Ceiling()
	var out []Dungeon
	for _, p := range monsterRoomPlacements {
		r := newTreeRNG(g.seed^p.salt, ox, oz)
		for i := 0; i < p.count; i++ {
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			y := p.height.sample(r, top)
			d, ok := monsterRoom(view, r, x, y, z)
			if !ok {
				continue
			}
			if g.touchedIn(d.X-d.W-1, d.Y-1, d.Z-d.D-1, d.X+d.W+1, d.Y+4, d.Z+d.D+1) {
				continue // a player's build or dig in its box
			}
			for c, s := range d.cells {
				view.capture[c] = s
			}
			out = append(out, d)
		}
	}
	roomCachePut(k, out)
	return out
}

// monsterRoom is MonsterRoomFeature.place at (x, y, z) against the view,
// which it does not change: the room's writes come back in d.cells.
func monsterRoom(view *owRegion, r TreeRNG, x, y, z int) (Dungeon, bool) {
	xr := r.Intn(2) + 2
	zr := r.Intn(2) + 2
	minX, maxX, minZ, maxZ := -xr-1, xr+1, -zr-1, zr+1
	holes := 0
	for dx := minX; dx <= maxX; dx++ {
		for dz := minZ; dz <= maxZ; dz++ {
			if (dx == minX || dx == maxX || dz == minZ || dz == maxZ) &&
				view.read(x+dx, y, z+dz) == Air && view.read(x+dx, y+1, z+dz) == Air {
				holes++
			}
		}
	}
	if holes < 1 || holes > 5 {
		return Dungeon{}, false
	}
	for dx := minX; dx <= maxX; dx++ {
		for dz := minZ; dz <= maxZ; dz++ {
			if !solid(view.read(x+dx, y-1, z+dz)) || !solid(view.read(x+dx, y+4, z+dz)) {
				return Dungeon{}, false
			}
		}
	}
	cells := map[[3]int]uint32{}
	rd := func(x, y, z int) uint32 {
		if s, ok := cells[[3]int{x, y, z}]; ok {
			return s
		}
		return view.read(x, y, z)
	}
	safeSet := func(x, y, z int, s uint32) bool {
		if inAnyRange(rd(x, y, z), featuresCannotReplace) {
			return false
		}
		cells[[3]int{x, y, z}] = s
		return true
	}
	for dx := minX; dx <= maxX; dx++ {
		for dy := 3; dy >= -1; dy-- {
			for dz := minZ; dz <= maxZ; dz++ {
				wx, wy, wz := x+dx, y+dy, z+dz
				st := rd(wx, wy, wz)
				if dx == minX || dy == -1 || dz == minZ || dx == maxX || dz == maxZ {
					switch {
					case wy >= MinY && !solid(rd(wx, wy-1, wz)):
						cells[[3]int{wx, wy, wz}] = Air // over nothing: the floor's gaps
					case solid(st) && !isChestState(st):
						if dy == -1 && r.Intn(4) != 0 {
							safeSet(wx, wy, wz, MossyCobblestone)
						} else {
							safeSet(wx, wy, wz, Cobblestone)
						}
					}
				} else if !isChestState(st) && st != Spawner {
					safeSet(wx, wy, wz, Air)
				}
			}
		}
	}
	d := Dungeon{X: x, Y: y, Z: z, W: xr, D: zr, Exists: true, cells: cells}
	for cc := 0; cc < 2; cc++ {
		for i := 0; i < 3; i++ {
			xc := x + r.Intn(xr*2+1) - xr
			zc := z + r.Intn(zr*2+1) - zr
			if rd(xc, y, zc) != Air {
				continue
			}
			walls := 0
			for _, h := range horizontalNESW {
				if solid(rd(xc+h[0], y, zc+h[1])) {
					walls++
				}
			}
			if walls != 1 {
				continue
			}
			if safeSet(xc, y, zc, reorientChest(rd, xc, y, zc)) {
				d.Chests = append(d.Chests, [3]int{xc, y, zc})
			}
			break
		}
	}
	safeSet(x, y, z, Spawner)
	d.Mob = monsterRoomMobs[r.Intn(len(monsterRoomMobs))]
	return d, true
}

// horizontalNESW is Direction.Plane.HORIZONTAL's order: north, east,
// south, west, as (dx, dz).
var horizontalNESW = [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

var roomChestFacing = [4]string{"north", "east", "south", "west"}

// reorientChest is StructurePiece.reorient for a chest: face away from its
// one full-block neighbour; with none or several, north unless that is
// blocked, then south, then west, then east, as vanilla turns it. A chest
// beside another chest keeps the default.
func reorientChest(rd func(x, y, z int) uint32, x, y, z int) uint32 {
	solidAt := -1
	for i, h := range horizontalNESW {
		s := rd(x+h[0], y, z+h[1])
		if isChestState(s) {
			return ChestNorth
		}
		if IsFullCube(s) {
			if solidAt >= 0 {
				solidAt = -1
				break
			}
			solidAt = i
		}
	}
	if solidAt >= 0 {
		return withProps("chest", "facing", roomChestFacing[(solidAt+2)%4])
	}
	dir := 0 // north
	blocked := func(d int) bool { h := horizontalNESW[d]; return IsFullCube(rd(x+h[0], y, z+h[1])) }
	if blocked(dir) {
		dir = (dir + 2) % 4
	}
	if blocked(dir) {
		dir = (dir + 1) % 4 // clockwise
	}
	if blocked(dir) {
		dir = (dir + 2) % 4
	}
	return withProps("chest", "facing", roomChestFacing[dir])
}

// placeMonsterRooms stamps the 3×3 origin chunks' rooms into this chunk
// (a room reaches four blocks from its origin).
func (g *Generator) placeMonsterRooms(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			for _, d := range g.monsterRooms(cx+dcx, cz+dcz) {
				for c, s := range d.cells {
					if lx, lz := c[0]-baseX, c[2]-baseZ; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
						setSectionBlock(ch, lx, c[1], lz, s, true)
					}
				}
			}
		}
	}
}

// DungeonsNear is every dungeon whose spawner lies within r blocks (a
// square) of (x, z): the placed monster rooms and the old rooms kept for
// the players who touched them. The server's spawners and loot use it.
func (g *Generator) DungeonsNear(x, z, r int) []Dungeon {
	if g.nether || g.end {
		return nil
	}
	var out []Dungeon
	in := func(d Dungeon) bool { return d.X >= x-r && d.X <= x+r && d.Z >= z-r && d.Z <= z+r }
	for cx := int32(floorDiv16(x - r)); cx <= int32(floorDiv16(x+r)); cx++ {
		for cz := int32(floorDiv16(z - r)); cz <= int32(floorDiv16(z+r)); cz++ {
			for _, d := range g.monsterRooms(cx, cz) {
				if in(d) {
					out = append(out, d)
				}
			}
		}
	}
	for ox := cellOrigin(x-r, dungeonCell); ox <= x+r; ox += dungeonCell {
		for oz := cellOrigin(z-r, dungeonCell); oz <= z+r; oz += dungeonCell {
			if d := g.keptLegacyDungeon(ox, oz); d.Exists && in(d) {
				out = append(out, d)
			}
		}
	}
	return out
}

// DungeonAt is the dungeon whose spawner is at (x, y, z).
func (g *Generator) DungeonAt(x, y, z int) (Dungeon, bool) {
	for _, d := range g.DungeonsNear(x, z, 0) {
		if d.X == x && d.Y == y && d.Z == z {
			return d, true
		}
	}
	return Dungeon{}, false
}

// DungeonChestAt reports whether (x, y, z) is a dungeon's chest.
func (g *Generator) DungeonChestAt(x, y, z int) bool {
	for _, d := range g.DungeonsNear(x, z, 4) {
		for _, c := range d.Chests {
			if c == [3]int{x, y, z} {
				return true
			}
		}
	}
	return false
}
