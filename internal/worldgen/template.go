package worldgen

import (
	_ "embed"
	"encoding/json"
	"log"
	"strconv"
)

// Vanilla structure templates. gen_structures.py bakes the real 1.21.11
// structure NBTs (palette + blocks + chest positions) into structures.json; this
// resolves each palette entry to a tachyne block state (per rotation) and stamps
// the real vanilla layout into a chunk. This is the vanilla-faithful replacement
// for the hand-built stand-ins — a template IS the exact vanilla room.

//go:embed structdata/structures.json
var structuresJSON []byte

const tmplSkip = 0xFFFFFFFF // palette markers (structure_void/block, jigsaw): place nothing

type paletteEntry struct {
	Name  string            `json:"name"`
	Props map[string]string `json:"props,omitempty"`
}

// jigsawBlock is a connection point on a template piece (see JigsawBlock in the
// vanilla source): Front/Top orientation, the Pool to draw the connecting piece
// from, the Name this block advertises, the Target name it must connect to, and
// the Final state that replaces it once placed.
type jigsawBlock struct {
	Pos    [3]int `json:"pos"`
	Front  string `json:"front"`
	Top    string `json:"top"`
	Joint  string `json:"joint"`
	Name   string `json:"name"`
	Pool   string `json:"pool"`
	Target string `json:"target"`
	Final  string `json:"final"`
}

// Template is one parsed structure piece. resolved[rot][paletteIdx] is the
// tachyne state to place for that palette entry at that rotation (or tmplSkip).
type Template struct {
	Size      [3]int         `json:"size"`
	Palette   []paletteEntry `json:"palette"`
	Blocks    [][4]int       `json:"blocks"`    // x,y,z,paletteIdx
	Chests    [][3]int       `json:"chests"`    // template-local chest positions
	ChestLoot []string       `json:"chestloot"` // per-chest loot table (aligned with Chests; "" = assigned by code)
	MobSpawns [][4]int       `json:"mobspawns"` // x,y,z,type illager markers (mansion): 0=evoker 1=vindicator 2=allay
	Beds      [][3]int       `json:"beds"`      // bed HEAD cells → one villager home each
	JobSites  [][4]int       `json:"jobsites"`  // x,y,z,profession
	Bells     [][3]int       `json:"bells"`
	Jigsaws   []jigsawBlock  `json:"jigsaws"`
	name      string         // the template's location key (set at init; for loot inference)
	resolved  [4][]uint32
}

type poolElement struct {
	Location   string `json:"location"`
	Weight     int    `json:"weight"`
	Projection string `json:"projection"`
}
type templatePool struct {
	Elements []poolElement `json:"elements"`
	Fallback string        `json:"fallback"`
}

var (
	templates map[string]*Template
	pools     map[string]*templatePool
)

func init() {
	var data struct {
		Templates map[string]*Template     `json:"templates"`
		Pools     map[string]*templatePool `json:"pools"`
	}
	if err := json.Unmarshal(structuresJSON, &data); err != nil {
		log.Printf("structure templates: %v", err)
		return
	}
	templates, pools = data.Templates, data.Pools
	for name, t := range templates {
		t.name = name
		for rot := 0; rot < 4; rot++ {
			t.resolved[rot] = make([]uint32, len(t.Palette))
			for i, p := range t.Palette {
				t.resolved[rot][i] = resolveState(p, rot)
			}
		}
	}
}

// TemplateByName returns a baked template (nil if absent).
func TemplateByName(name string) *Template { return templates[name] }

// resolveState maps a palette entry to a tachyne state at the given rotation,
// applying every listed property (rotated) onto the block base. Markers place
// nothing.
func resolveState(p paletteEntry, rot int) uint32 {
	name := trimNS(p.Name)
	switch name {
	case "structure_void", "structure_block", "jigsaw":
		return tmplSkip
	}
	base := safeBase(name)
	if base == tmplSkip {
		return tmplSkip
	}
	info, ok := InfoForState(base)
	if !ok || len(p.Props) == 0 {
		return base
	}
	state := base
	for k, v := range p.Props {
		k2, v2 := rotateProp(k, v, rot)
		if info.HasProperty(k2) {
			state = SetProperty(info, state, k2, v2)
		}
	}
	return state
}

// rotateProp rotates a directional property value clockwise by `rot` quarter
// turns (facing/axis/rotation); other properties pass through unchanged.
func rotateProp(name, val string, rot int) (string, string) {
	if rot == 0 {
		return name, val
	}
	switch name {
	case "facing":
		order := []string{"north", "east", "south", "west"} // clockwise
		for i, d := range order {
			if d == val {
				return name, order[(i+rot)%4]
			}
		}
	case "north", "east", "south", "west":
		// Connection booleans (panes/fences/walls/iron bars/redstone): the
		// PROPERTY NAME rotates — a north connection becomes east at 90° CW, etc.
		// Each source direction maps to a distinct target, so applying all four
		// in any order yields the correct rotated shape.
		order := []string{"north", "east", "south", "west"}
		for i, d := range order {
			if d == name {
				return order[(i+rot)&3], val
			}
		}
	case "axis":
		if rot%2 == 1 {
			if val == "x" {
				return name, "z"
			} else if val == "z" {
				return name, "x"
			}
		}
	case "rotation": // signs/banners: 0..15, 4 per quarter turn
		if n, err := strconv.Atoi(val); err == nil {
			return name, strconv.Itoa((n + 4*rot) & 15)
		}
	}
	return name, val
}

// Mirror modes (vanilla Mirror), applied BEFORE rotation.
const (
	mirNone = 0
	mirLR   = 1 // LEFT_RIGHT: flips the Z axis (north↔south)
	mirFB   = 2 // FRONT_BACK: flips the X axis (east↔west)
)

// transformPos applies vanilla's template transform (mirror then rotation about
// the origin, StructureTemplate.transform with pivot 0,0,0) to a local position.
// The result can be negative — the placer's anchor positions account for that.
func transformPos(x, y, z, rot, mir int) (int, int, int) {
	switch mir {
	case mirLR:
		z = -z
	case mirFB:
		x = -x
	}
	switch rot & 3 {
	case 1: // CLOCKWISE_90
		return -z, y, x
	case 2: // CLOCKWISE_180
		return -x, y, -z
	case 3: // COUNTERCLOCKWISE_90
		return z, y, -x
	}
	return x, y, z
}

// mirrorProp reflects a directional property (facing/connection/axis) for a
// mirror, BEFORE rotation. LEFT_RIGHT flips north↔south, FRONT_BACK flips
// east↔west. (Stair shape, door hinge and sign rotation mirroring are not
// handled — a minor cosmetic gap in mirrored rooms.)
func mirrorProp(name, val string, mir int) (string, string) {
	if mir == mirNone {
		return name, val
	}
	switch name {
	case "facing":
		switch mir {
		case mirLR:
			if val == "north" {
				return name, "south"
			} else if val == "south" {
				return name, "north"
			}
		case mirFB:
			if val == "east" {
				return name, "west"
			} else if val == "west" {
				return name, "east"
			}
		}
	case "north", "south":
		if mir == mirLR {
			if name == "north" {
				return "south", val
			}
			return "north", val
		}
	case "east", "west":
		if mir == mirFB {
			if name == "east" {
				return "west", val
			}
			return "east", val
		}
	}
	return name, val
}

// resolveStateM resolves a palette entry with mirror THEN rotation (vanilla
// state.mirror(mir).rotate(rot)).
func resolveStateM(p paletteEntry, rot, mir int) uint32 {
	name := trimNS(p.Name)
	switch name {
	case "structure_void", "structure_block", "jigsaw":
		return tmplSkip
	}
	base := safeBase(name)
	if base == tmplSkip {
		return tmplSkip
	}
	info, ok := InfoForState(base)
	if !ok || len(p.Props) == 0 {
		return base
	}
	state := base
	for k, v := range p.Props {
		k1, v1 := mirrorProp(k, v, mir)
		k2, v2 := rotateProp(k1, v1, rot)
		if info.HasProperty(k2) {
			state = SetProperty(info, state, k2, v2)
		}
	}
	return state
}

// StampAt places a template so its transformed origin sits at world (px,py,pz),
// under rotation `rot` and mirror `mir` (vanilla pivot-0 transform). Returns the
// world positions of its chests. Only the portion in this chunk is written.
func (t *Template) StampAt(ch *Chunk, cx, cz int32, px, py, pz, rot, mir int) [][3]int {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, b := range t.Blocks {
		state := resolveStateM(t.Palette[b[3]], rot, mir)
		if state == tmplSkip {
			continue
		}
		tx, ty, tz := transformPos(b[0], b[1], b[2], rot, mir)
		wx, wy, wz := px+tx, py+ty, pz+tz
		setSectionBlock(ch, wx-baseX, wy, wz-baseZ, state, true)
	}
	var chests [][3]int
	for _, c := range t.Chests {
		tx, ty, tz := transformPos(c[0], c[1], c[2], rot, mir)
		chests = append(chests, [3]int{px + tx, py + ty, pz + tz})
	}
	return chests
}

// rotatePos rotates a template-local position clockwise by `rot` quarter turns,
// keeping it within the (rotated) bounding box.
func (t *Template) rotatePos(x, y, z, rot int) (int, int, int) {
	sx, sz := t.Size[0], t.Size[2]
	switch rot & 3 {
	case 1:
		return sz - 1 - z, y, x
	case 2:
		return sx - 1 - x, y, sz - 1 - z
	case 3:
		return z, y, sx - 1 - x
	}
	return x, y, z
}

// StampTemplate places the template with its min corner at world (ox,oy,oz),
// rotated, into the chunk. Returns the world positions of its chests (for loot
// routing). Only the portion overlapping this chunk is written.
func (t *Template) StampTemplate(ch *Chunk, cx, cz int32, ox, oy, oz, rot int) [][3]int {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, b := range t.Blocks {
		state := t.resolved[rot&3][b[3]]
		if state == tmplSkip {
			continue
		}
		rx, ry, rz := t.rotatePos(b[0], b[1], b[2], rot)
		wx, wy, wz := ox+rx, oy+ry, oz+rz
		setSectionBlock(ch, wx-baseX, wy, wz-baseZ, state, true)
	}
	var chests [][3]int
	for _, c := range t.Chests {
		rx, ry, rz := t.rotatePos(c[0], c[1], c[2], rot)
		chests = append(chests, [3]int{ox + rx, oy + ry, oz + rz})
	}
	return chests
}

// StampTemplateRot is StampTemplate with the vanilla ruined-structure decay:
// each block is dropped with probability (1-integrity) — the BlockRotProcessor's
// "ruined" incompleteness — except the chest (loot must stay reachable), and
// obsidian occasionally weeps (BlockAgeProcessor). Deterministic per rotSeed.
func (t *Template) StampTemplateRot(ch *Chunk, cx, cz int32, ox, oy, oz, rot int, rotSeed int64, integrity float64) [][3]int {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, b := range t.Blocks {
		state := t.resolved[rot&3][b[3]]
		if state == tmplSkip {
			continue
		}
		name := trimNS(t.Palette[b[3]].Name)
		rx, ry, rz := t.rotatePos(b[0], b[1], b[2], rot)
		wx, wy, wz := ox+rx, oy+ry, oz+rz
		if name != "chest" && name != "air" && integrity < 1 &&
			hash01(rotSeed, wx, wz*8192+wy, 0x2074) >= integrity {
			continue // rotted away
		}
		if name == "obsidian" && hash01(rotSeed, wx*3, (wz*8192+wy)*5, 0x2075) < 0.15 {
			state = CryingObsidian // aged
		}
		setSectionBlock(ch, wx-baseX, wy, wz-baseZ, state, true)
	}
	var chests [][3]int
	for _, c := range t.Chests {
		rx, ry, rz := t.rotatePos(c[0], c[1], c[2], rot)
		chests = append(chests, [3]int{ox + rx, oy + ry, oz + rz})
	}
	return chests
}

func trimNS(name string) string {
	if len(name) > 10 && name[:10] == "minecraft:" {
		return name[10:]
	}
	return name
}

// safeBase resolves a block name to its base state, tmplSkip if unknown.
func safeBase(name string) (b uint32) {
	defer func() {
		if recover() != nil {
			b = tmplSkip
		}
	}()
	return blockBase(name)
}
