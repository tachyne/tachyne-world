package server

import (
	"math"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// Armor stand poses (ArmorStand.ArmorStandPose): six Rotations, each an
// x/y/z in degrees, synced as DATA_HEAD_POSE … DATA_RIGHT_LEG_POSE and saved
// as the Pose compound. They come from /summon's NBT or from an armor stand
// item's entity_data, the only way vanilla sets them.

// standPart names the Pose compound's keys in DATA_*_POSE order.
var standPart = [6]string{"Head", "Body", "LeftArm", "RightArm", "LeftLeg", "RightLeg"}

// standPose is the six rotations in standPart order.
type standPose [6][3]float32

// defaultStandPose is ArmorStandPose.DEFAULT.
var defaultStandPose = standPose{
	{0, 0, 0}, {0, 0, 0}, {-10, 0, -10}, {-15, 0, 10}, {-1, 0, -1}, {1, 0, 1},
}

const (
	standMetaHeadPose = 16 // DATA_HEAD_POSE; the other five follow in standPart order
	metaTypeRotations = 9  // EntityDataSerializers.ROTATIONS
)

// rotationDeg is the Rotations constructor: a component outside the
// finite floats is 0, and the rest keep x % 360.
func rotationDeg(v float64) float32 {
	f := float32(v)
	if math.IsInf(float64(f), 0) || math.IsNaN(float64(f)) {
		return 0
	}
	return float32(math.Mod(float64(f), 360))
}

// parseStandPose reads a Pose compound (ArmorStandPose.CODEC:
// each part optional, defaulting to vanilla's; a part that is not a list of
// three numbers fails the whole compound, which then changes nothing).
func parseStandPose(v any) (standPose, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return standPose{}, false
	}
	p := defaultStandPose
	for i, k := range standPart {
		raw, present := m[k]
		if !present {
			continue
		}
		l, ok := raw.([]any)
		if !ok || len(l) != 3 {
			return standPose{}, false
		}
		for j, c := range l {
			f, ok := snbtFloat(c)
			if !ok {
				return standPose{}, false
			}
			p[i][j] = rotationDeg(f)
		}
	}
	return p, true
}

// appendStandPoseMeta appends the six pose entries of set_entity_data.
func appendStandPoseMeta(b []byte, p standPose) []byte {
	for i, r := range p {
		b = protocol.AppendU8(b, byte(standMetaHeadPose+i))
		b = protocol.AppendVarInt(b, metaTypeRotations)
		for _, c := range r {
			b = protocol.AppendF32(b, c)
		}
	}
	return b
}

// Stand tags an armor stand item carries in its entity_data component, as
// a canonical string (invStack stays comparable, and the string rides the
// name table into a saved row): the flag bits, then the pose's eighteen
// floats when it is not the default.
const (
	standTagSmall = 1 << iota
	standTagArms
	standTagNoBasePlate
	standTagMarker
	standTagInvisible
)

// standTagsFromEntityData reads an armor stand item's entity_data compound
// (EntityType.updateCustomEntityTag on a fresh stand): a compound for
// another entity type does nothing, and the tags it names replace a new
// stand's defaults.
func standTagsFromEntityData(m map[string]any) (string, bool) {
	id, _ := m["id"].(string)
	if strings.TrimPrefix(id, "minecraft:") != "armor_stand" {
		return "", false
	}
	st := &armorStand{}
	st.setStandFlagsFrom(m)
	return st.itemTags(), true
}

// itemTags is the stand's tags in the entity_data string form.
func (st *armorStand) itemTags() string {
	flags := 0
	for _, b := range []struct {
		on  bool
		bit int
	}{{st.small, standTagSmall}, {st.arms, standTagArms}, {st.noBasePlate, standTagNoBasePlate},
		{st.marker, standTagMarker}, {st.invisible, standTagInvisible}} {
		if b.on {
			flags |= b.bit
		}
	}
	s := strconv.Itoa(flags)
	if st.pose != nil && *st.pose != defaultStandPose {
		for _, r := range st.pose {
			for _, c := range r {
				s += " " + strconv.FormatFloat(float64(c), 'g', -1, 32)
			}
		}
	}
	return s
}

// applyItemTags sets the stand's flags and pose from an entity_data
// string; "" (no component) leaves the stand as it is.
func (st *armorStand) applyItemTags(s string) {
	if s == "" {
		return
	}
	f := strings.Fields(s)
	flags, err := strconv.Atoi(f[0])
	if err != nil {
		return
	}
	st.small, st.arms = flags&standTagSmall != 0, flags&standTagArms != 0
	st.noBasePlate, st.marker = flags&standTagNoBasePlate != 0, flags&standTagMarker != 0
	st.invisible = flags&standTagInvisible != 0
	if len(f) != 1+18 {
		return
	}
	var p standPose
	for i := range 18 {
		v, err := strconv.ParseFloat(f[1+i], 32)
		if err != nil {
			return
		}
		p[i/3][i%3] = float32(v)
	}
	st.setPose(p)
}

// poseOf is the stand's pose, the default when it was never set.
func (st *armorStand) poseOf() standPose {
	if st.pose == nil {
		return defaultStandPose
	}
	return *st.pose
}

// setPose keeps a pose only when it differs from the default.
func (st *armorStand) setPose(p standPose) {
	if p == defaultStandPose {
		st.pose = nil
		return
	}
	st.pose = &p
}
