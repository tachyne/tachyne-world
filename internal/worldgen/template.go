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
	Size     [3]int         `json:"size"`
	Palette  []paletteEntry `json:"palette"`
	Blocks   [][4]int       `json:"blocks"` // x,y,z,paletteIdx
	Chests   [][3]int       `json:"chests"` // template-local chest positions
	Jigsaws  []jigsawBlock  `json:"jigsaws"`
	resolved [4][]uint32
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
	for _, t := range templates {
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
