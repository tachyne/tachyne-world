package worldgen

// The mansion piece placer (vanilla MansionPiecePlacer): walks the solved grid
// and emits placed template pieces. Positions are world-space; each piece is a
// template name + world position + rotation + mirror, stamped via Template.StampAt.

type mansionPiece struct {
	tmpl     string
	pos      [3]int
	rot, mir int
}

type mansionPlacer struct {
	rng            *jigsawRNG
	startX, startY int
	pieces         []mansionPiece
}

var mNone = mdir{0, 0, 0} // "no direction" sentinel (object4 == null)

func (p *mansionPlacer) add(tmpl string, pos [3]int, rot, mir int) {
	p.pieces = append(p.pieces, mansionPiece{tmpl, pos, rot, mir})
}

// pos helpers (BlockPos.relative / above with a rotation-rotated direction).
func rel(pos [3]int, d mdir, n int) [3]int {
	return [3]int{pos[0] + d.sx*n, pos[1] + d.up*n, pos[2] + d.sz*n}
}
func above(pos [3]int, n int) [3]int { return [3]int{pos[0], pos[1] + n, pos[2]} }

// gzpt = StructureTemplate.getZeroPositionWithTransform.
func gzpt(pos [3]int, mir, rot, sizeX, sizeZ int) [3]int {
	sizeX--
	sizeZ--
	i := 0
	if mir == mirFB {
		i = sizeX
	}
	j := 0
	if mir == mirLR {
		j = sizeZ
	}
	switch rot & 3 {
	case 3: // COUNTERCLOCKWISE_90
		return [3]int{pos[0] + j, pos[1], pos[2] + sizeX - i}
	case 1: // CLOCKWISE_90
		return [3]int{pos[0] + sizeZ - j, pos[1], pos[2] + i}
	case 2: // CLOCKWISE_180
		return [3]int{pos[0] + sizeX - i, pos[1], pos[2] + sizeZ - j}
	default: // NONE
		return [3]int{pos[0] + i, pos[1], pos[2] + j}
	}
}

// rotatePosOrigin = BlockPos.rotate(rotation) about the origin.
func rotatePosOrigin(p [3]int, rot int) [3]int {
	x, y, z := transformPos(p[0], p[1], p[2], rot, mirNone)
	return [3]int{x, y, z}
}

// ---- floor room-name pickers (FloorRoomCollection) --------------------------

func (p *mansionPlacer) get1x1(floor int) string {
	if floor == 0 {
		return "1x1_a" + digit(p.rng.intn(5)+1)
	}
	return "1x1_b" + digit(p.rng.intn(4)+1)
}
func (p *mansionPlacer) get1x1Secret(floor int) string {
	return "1x1_as" + digit(p.rng.intn(4)+1)
}
func (p *mansionPlacer) get1x2Side(floor int, stairs bool) string {
	if floor == 0 {
		return "1x2_a" + digit(p.rng.intn(9)+1)
	}
	if stairs {
		return "1x2_c_stairs"
	}
	return "1x2_c" + digit(p.rng.intn(4)+1)
}
func (p *mansionPlacer) get1x2Front(floor int, stairs bool) string {
	if floor == 0 {
		return "1x2_b" + digit(p.rng.intn(5)+1)
	}
	if stairs {
		return "1x2_d_stairs"
	}
	return "1x2_d" + digit(p.rng.intn(5)+1)
}
func (p *mansionPlacer) get1x2Secret(floor int) string {
	if floor == 0 {
		return "1x2_s" + digit(p.rng.intn(2)+1)
	}
	return "1x2_se1"
}
func (p *mansionPlacer) get2x2(floor int) string {
	if floor == 0 {
		return "2x2_a" + digit(p.rng.intn(4)+1)
	}
	return "2x2_b" + digit(p.rng.intn(5)+1)
}
func (p *mansionPlacer) get2x2Secret(floor int) string { return "2x2_s1" }

func digit(n int) string { return string(rune('0' + n)) }

// ---- createMansion ----------------------------------------------------------

func (p *mansionPlacer) createMansion(base [3]int, rot int, mg *mansionGrid) {
	// Entrance + ground/upper outer walls.
	wallPos := base
	p.entrance(&wallPos, rot)
	wallPos2 := above(base, 8)
	p.startX = mg.entranceX + 1
	p.startY = mg.entranceY + 1
	n2 := mg.entranceX + 1
	n3 := mg.entranceY
	rotW := rot
	p.traverseOuterWalls(&wallPos, &rotW, mg.baseGrid, mSouth, "wall_flat", p.startX, p.startY, n2, n3)
	rotW2 := rot
	p.traverseOuterWalls(&wallPos2, &rotW2, mg.baseGrid, mSouth, "wall_window", p.startX, p.startY, n2, n3)

	// Third-floor outer wall (walk from the one third-floor house cell).
	wallPos3 := above(base, 19)
	rotW3 := rot
	done := false
	for i := 0; i < mg.thirdFloorGrid.h && !done; i++ {
		for n := mg.thirdFloorGrid.w - 1; n >= 0 && !done; n-- {
			if !gridIsHouse(mg.thirdFloorGrid, n, i) {
				continue
			}
			wallPos3 = rel(wallPos3, mrotDir(rot, mSouth), 8+(i-p.startY)*8)
			wallPos3 = rel(wallPos3, mrotDir(rot, mEast), (n-p.startX)*8)
			p.traverseWallPiece(&wallPos3, rotW3, "wall_window")
			p.traverseOuterWalls(&wallPos3, &rotW3, mg.thirdFloorGrid, mSouth, "wall_window", n, i, n, i)
			done = true
		}
	}

	p.createRoof(above(base, 16), rot, mg.baseGrid, mg.thirdFloorGrid)
	p.createRoof(above(base, 27), rot, mg.thirdFloorGrid, nil)

	// Rooms + carpets + doors per floor.
	for n := 0; n < 3; n++ {
		floorBase := above(base, 8*n)
		if n == 2 {
			floorBase = above(base, 8*n+3)
		}
		sizeGrid := mg.floorRooms[n]
		wallGrid := mg.baseGrid
		if n == 2 {
			wallGrid = mg.thirdFloorGrid
		}
		carpetS := "carpet_south_2"
		carpetW := "carpet_west_2"
		if n == 0 {
			carpetS, carpetW = "carpet_south_1", "carpet_west_1"
		}
		for i := 0; i < wallGrid.h; i++ {
			for j := 0; j < wallGrid.w; j++ {
				if wallGrid.get(j, i) != 1 {
					continue
				}
				o := rel(floorBase, mrotDir(rot, mSouth), 8+(i-p.startY)*8)
				o = rel(o, mrotDir(rot, mEast), (j-p.startX)*8)
				p.add("corridor_floor", o, rot, mirNone)
				if wallGrid.get(j, i-1) == 1 || sizeGrid.get(j, i-1)&0x800000 == 0x800000 {
					p.add("carpet_north", above(rel(o, mrotDir(rot, mEast), 1), 1), rot, mirNone)
				}
				if wallGrid.get(j+1, i) == 1 || sizeGrid.get(j+1, i)&0x800000 == 0x800000 {
					p.add("carpet_east", above(rel(rel(o, mrotDir(rot, mSouth), 1), mrotDir(rot, mEast), 5), 1), rot, mirNone)
				}
				if wallGrid.get(j, i+1) == 1 || sizeGrid.get(j, i+1)&0x800000 == 0x800000 {
					p.add(carpetS, rel(rel(o, mrotDir(rot, mSouth), 5), mrotDir(rot, mWest), 1), rot, mirNone)
				}
				if wallGrid.get(j-1, i) == 1 || sizeGrid.get(j-1, i)&0x800000 == 0x800000 {
					p.add(carpetW, rel(rel(o, mrotDir(rot, mWest), 1), mrotDir(rot, mNorth), 1), rot, mirNone)
				}
			}
		}
		wallT := "indoors_wall_2"
		doorT := "indoors_door_2"
		if n == 0 {
			wallT, doorT = "indoors_wall_1", "indoors_door_1"
		}
		for i := 0; i < wallGrid.h; i++ {
			for j := 0; j < wallGrid.w; j++ {
				isThirdSpecial := n == 2 && wallGrid.get(j, i) == 3
				if wallGrid.get(j, i) != 2 && !isThirdSpecial {
					continue
				}
				code := sizeGrid.get(j, i)
				sizeCode := code & 0xF0000
				roomID := code & 0xFFFF
				special := isThirdSpecial && code&0x800000 == 0x800000
				var openings []mdir
				if code&0x200000 == 0x200000 {
					for _, d := range mHoriz {
						if wallGrid.get(j+d.sx, i+d.sz) == 1 {
							openings = append(openings, d)
						}
					}
				}
				door := mNone
				if len(openings) > 0 {
					door = openings[p.rng.intn(len(openings))]
				} else if code&0x100000 == 0x100000 {
					door = mUp
				}
				o := rel(floorBase, mrotDir(rot, mSouth), 8+(i-p.startY)*8)
				o = rel(o, mrotDir(rot, mEast), -1+(j-p.startX)*8)
				if gridIsHouse(wallGrid, j-1, i) && !mg.isRoomId(wallGrid, j-1, i, n, roomID) {
					t := wallT
					if door.eq(mWest) {
						t = doorT
					}
					p.add(t, o, rot, mirNone)
				}
				if wallGrid.get(j+1, i) == 1 && !special {
					o2 := rel(o, mrotDir(rot, mEast), 8)
					t := wallT
					if door.eq(mEast) {
						t = doorT
					}
					p.add(t, o2, rot, mirNone)
				}
				if gridIsHouse(wallGrid, j, i+1) && !mg.isRoomId(wallGrid, j, i+1, n, roomID) {
					o2 := rel(rel(o, mrotDir(rot, mSouth), 7), mrotDir(rot, mEast), 7)
					t := wallT
					if door.eq(mSouth) {
						t = doorT
					}
					p.add(t, o2, rotCompose(rot, 1), mirNone)
				}
				if wallGrid.get(j, i-1) == 1 && !special {
					o2 := rel(rel(o, mrotDir(rot, mNorth), 1), mrotDir(rot, mEast), 7)
					t := wallT
					if door.eq(mNorth) {
						t = doorT
					}
					p.add(t, o2, rotCompose(rot, 1), mirNone)
				}
				switch sizeCode {
				case 65536:
					p.addRoom1x1(o, rot, door, n)
				case 131072:
					if !door.eq(mNone) {
						d1, _ := mg.get1x2RoomDirection(wallGrid, j, i, n, roomID)
						stairs := code&0x400000 == 0x400000
						p.addRoom1x2(o, rot, d1, door, n, stairs)
					}
				case 262144:
					if !door.eq(mNone) && !door.eq(mUp) {
						d1 := door.cw()
						if !mg.isRoomId(wallGrid, j+d1.sx, i+d1.sz, n, roomID) {
							d1 = d1.opp()
						}
						p.addRoom2x2(o, rot, d1, door, n)
					} else if door.eq(mUp) {
						p.addRoom2x2Secret(o, rot, n)
					}
				}
			}
		}
	}
}

func (p *mansionPlacer) entrance(pos *[3]int, rot int) {
	d := mrotDir(rot, mWest)
	p.add("entrance", rel(*pos, d, 9), rot, mirNone)
	*pos = rel(*pos, mrotDir(rot, mSouth), 16)
}

func (p *mansionPlacer) traverseWallPiece(pos *[3]int, rot int, wallType string) {
	p.add(wallType, rel(*pos, mrotDir(rot, mEast), 7), rot, mirNone)
	*pos = rel(*pos, mrotDir(rot, mSouth), 8)
}

func (p *mansionPlacer) traverseTurn(pos *[3]int, rot *int) {
	*pos = rel(*pos, mrotDir(*rot, mSouth), -1)
	p.add("wall_corner", *pos, *rot, mirNone)
	*pos = rel(*pos, mrotDir(*rot, mSouth), -7)
	*pos = rel(*pos, mrotDir(*rot, mWest), -6)
	*rot = rotCompose(*rot, 1)
}

func (p *mansionPlacer) traverseInnerTurn(pos *[3]int, rot *int) {
	*pos = rel(*pos, mrotDir(*rot, mSouth), 6)
	*pos = rel(*pos, mrotDir(*rot, mEast), 8)
	*rot = rotCompose(*rot, 3)
}

func (p *mansionPlacer) traverseOuterWalls(pos *[3]int, rot *int, g *simpleGrid, d mdir, wallType string, x, y, endX, endY int) {
	x5, y5 := x, y
	dStart := d
	for {
		if !gridIsHouse(g, x5+d.sx, y5+d.sz) {
			p.traverseTurn(pos, rot)
			d = d.cw()
			if !(x5 == endX && y5 == endY && dStart.eq(d)) {
				p.traverseWallPiece(pos, *rot, wallType)
			}
		} else if gridIsHouse(g, x5+d.sx, y5+d.sz) &&
			gridIsHouse(g, x5+d.sx+d.ccw().sx, y5+d.sz+d.ccw().sz) {
			p.traverseInnerTurn(pos, rot)
			x5 += d.sx
			y5 += d.sz
			d = d.ccw()
		} else {
			x5 += d.sx
			y5 += d.sz
			if !(x5 == endX && y5 == endY && dStart.eq(d)) {
				p.traverseWallPiece(pos, *rot, wallType)
			}
		}
		if x5 == endX && y5 == endY && dStart.eq(d) {
			break
		}
	}
}

func (p *mansionPlacer) addRoom1x1(pos [3]int, rot int, dir mdir, floor int) {
	rot2 := 0
	name := p.get1x1(floor)
	if !dir.eq(mEast) {
		if dir.eq(mNorth) {
			rot2 = rotCompose(rot2, 3)
		} else if dir.eq(mWest) {
			rot2 = rotCompose(rot2, 2)
		} else if dir.eq(mSouth) {
			rot2 = rotCompose(rot2, 1)
		} else {
			name = p.get1x1Secret(floor)
		}
	}
	bp := gzpt([3]int{1, 0, 0}, mirNone, rot2, 7, 7)
	rot2 = rotCompose(rot2, rot)
	bp = rotatePosOrigin(bp, rot)
	p.add(name, [3]int{pos[0] + bp[0], pos[1], pos[2] + bp[2]}, rot2, mirNone)
}

func (p *mansionPlacer) addRoom1x2(pos [3]int, rot int, d, door mdir, floor int, stairs bool) {
	name := p.get1x2Side(floor, stairs)
	nameFront := p.get1x2Front(floor, stairs)
	secret := p.get1x2Secret(floor)
	switch {
	case door.eq(mEast) && d.eq(mSouth):
		p.add(name, rel(pos, mrotDir(rot, mEast), 1), rot, mirNone)
	case door.eq(mEast) && d.eq(mNorth):
		q := rel(rel(pos, mrotDir(rot, mEast), 1), mrotDir(rot, mSouth), 6)
		p.add(name, q, rot, mirLR)
	case door.eq(mWest) && d.eq(mNorth):
		q := rel(rel(pos, mrotDir(rot, mEast), 7), mrotDir(rot, mSouth), 6)
		p.add(name, q, rotCompose(rot, 2), mirNone)
	case door.eq(mWest) && d.eq(mSouth):
		q := rel(pos, mrotDir(rot, mEast), 7)
		p.add(name, q, rot, mirFB)
	case door.eq(mSouth) && d.eq(mEast):
		q := rel(pos, mrotDir(rot, mEast), 1)
		p.add(name, q, rotCompose(rot, 1), mirLR)
	case door.eq(mSouth) && d.eq(mWest):
		q := rel(pos, mrotDir(rot, mEast), 7)
		p.add(name, q, rotCompose(rot, 1), mirNone)
	case door.eq(mNorth) && d.eq(mWest):
		q := rel(rel(pos, mrotDir(rot, mEast), 7), mrotDir(rot, mSouth), 6)
		p.add(name, q, rotCompose(rot, 1), mirFB)
	case door.eq(mNorth) && d.eq(mEast):
		q := rel(rel(pos, mrotDir(rot, mEast), 1), mrotDir(rot, mSouth), 6)
		p.add(name, q, rotCompose(rot, 3), mirNone)
	case door.eq(mSouth) && d.eq(mNorth):
		q := rel(rel(pos, mrotDir(rot, mEast), 1), mrotDir(rot, mNorth), 8)
		p.add(nameFront, q, rot, mirNone)
	case door.eq(mNorth) && d.eq(mSouth):
		q := rel(rel(pos, mrotDir(rot, mEast), 7), mrotDir(rot, mSouth), 14)
		p.add(nameFront, q, rotCompose(rot, 2), mirNone)
	case door.eq(mWest) && d.eq(mEast):
		q := rel(pos, mrotDir(rot, mEast), 15)
		p.add(nameFront, q, rotCompose(rot, 1), mirNone)
	case door.eq(mEast) && d.eq(mWest):
		q := rel(rel(pos, mrotDir(rot, mWest), 7), mrotDir(rot, mSouth), 6)
		p.add(nameFront, q, rotCompose(rot, 3), mirNone)
	case door.eq(mUp) && d.eq(mEast):
		q := rel(pos, mrotDir(rot, mEast), 15)
		p.add(secret, q, rotCompose(rot, 1), mirNone)
	case door.eq(mUp) && d.eq(mSouth):
		q := rel(pos, mrotDir(rot, mEast), 1)
		p.add(secret, q, rot, mirNone)
	}
}

func (p *mansionPlacer) addRoom2x2(pos [3]int, rot int, d, door mdir, floor int) {
	n, n2 := 0, 0
	rot2 := rot
	mir := mirNone
	switch {
	case door.eq(mEast) && d.eq(mSouth):
		n = -7
	case door.eq(mEast) && d.eq(mNorth):
		n, n2, mir = -7, 6, mirLR
	case door.eq(mNorth) && d.eq(mEast):
		n, n2, rot2 = 1, 14, rotCompose(rot, 3)
	case door.eq(mNorth) && d.eq(mWest):
		n, n2, rot2, mir = 7, 14, rotCompose(rot, 3), mirLR
	case door.eq(mSouth) && d.eq(mWest):
		n, n2, rot2 = 7, -8, rotCompose(rot, 1)
	case door.eq(mSouth) && d.eq(mEast):
		n, n2, rot2, mir = 1, -8, rotCompose(rot, 1), mirLR
	case door.eq(mWest) && d.eq(mNorth):
		n, n2, rot2 = 15, 6, rotCompose(rot, 2)
	case door.eq(mWest) && d.eq(mSouth):
		n, mir = 15, mirFB
	}
	q := rel(rel(pos, mrotDir(rot, mEast), n), mrotDir(rot, mSouth), n2)
	p.add(p.get2x2(floor), q, rot2, mir)
}

func (p *mansionPlacer) addRoom2x2Secret(pos [3]int, rot, floor int) {
	q := rel(pos, mrotDir(rot, mEast), 1)
	p.add(p.get2x2Secret(floor), q, rot, mirNone)
}
