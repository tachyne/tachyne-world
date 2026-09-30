package server

import (
	"fmt"
	"math"
	"sort"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// Particle options, as /particle takes them: ParticleArgument reads the type
// and then its options as SNBT, which each ParticleOptions class's CODEC
// decodes. The attach Particles frame carries them typed (ParticleOptions)
// and the gateways write each type's stream form for the client's version.

// particleOptKind is which ParticleOptions class a type decodes with.
type particleOptKind int

const (
	optBlock       particleOptKind = iota // BlockParticleOption: block_state
	optDust                               // DustParticleOptions: color (RGB), scale
	optDustTrans                          // DustColorTransitionOptions: from_color, to_color, scale
	optColor                              // ColorParticleOption: color (ARGB)
	optSpell                              // SpellParticleOption: color (RGB, -1), power (1)
	optPower                              // PowerParticleOption: power (1)
	optItem                               // ItemParticleOption: item
	optSculkCharge                        // SculkChargeParticleOptions: roll
	optShriek                             // ShriekParticleOption: delay
	optTrail                              // TrailParticleOption: target, color, duration
	optVibration                          // VibrationParticleOption: destination, arrival_in_ticks
)

// particleOptTypes are the types that take options, by name, with their
// canonical ids.
var particleOptTypes = map[string]struct {
	pid  int32
	kind particleOptKind
}{
	"block":                 {protocol.ParticleBlock770, optBlock},
	"block_marker":          {protocol.ParticleBlockMarker770, optBlock},
	"falling_dust":          {protocol.ParticleFallingDust770, optBlock},
	"dust_pillar":           {protocol.ParticleDustPillar770, optBlock},
	"block_crumble":         {protocol.ParticleBlockCrumble770, optBlock},
	"dust":                  {protocol.ParticleDust770, optDust},
	"dust_color_transition": {protocol.ParticleDustColorTransition770, optDustTrans},
	"entity_effect":         {protocol.ParticleEntityEffect770, optColor},
	"tinted_leaves":         {protocol.ParticleTintedLeaves770, optColor},
	"flash":                 {protocol.ParticleFlash770, optColor},
	"effect":                {protocol.ParticleEffect770, optSpell},
	"instant_effect":        {protocol.ParticleInstantEffect770, optSpell},
	"dragon_breath":         {protocol.ParticleDragonBreath770, optPower},
	"item":                  {protocol.ParticleItem770, optItem},
	"sculk_charge":          {protocol.ParticleSculkCharge770, optSculkCharge},
	"shriek":                {protocol.ParticleShriek770, optShriek},
	"trail":                 {protocol.ParticleTrail770, optTrail},
	"vibration":             {protocol.ParticleVibration770, optVibration},
}

// parseParticleOptions decodes one option type's SNBT (nil when none was
// given, which only the types whose every field is optional accept).
func parseParticleOptions(kind particleOptKind, tag map[string]any) (*attachproto.ParticleOptions, string) {
	o := &attachproto.ParticleOptions{}
	need := func(key string) (any, string) {
		v, ok := tag[key]
		if !ok {
			return nil, fmt.Sprintf("Can't parse particle options: No key %s in MapLike", key)
		}
		return v, ""
	}
	scale := func() string {
		v, msg := need("scale")
		if msg != "" {
			return msg
		}
		f, ok := snbtFloat(v)
		if !ok {
			return "Can't parse particle options: scale is not a number"
		}
		// ScalableParticleOptionsBase.SCALE: clamped to [0.01, 4].
		o.Scale = float32(math.Min(math.Max(f, 0.01), 4))
		return ""
	}
	switch kind {
	case optBlock:
		v, msg := need("block_state")
		if msg != "" {
			return nil, msg
		}
		st, ok := particleBlockState(v)
		if !ok {
			return nil, "Can't parse particle options: unknown block_state"
		}
		o.State = int32(st)
	case optDust:
		v, msg := need("color")
		if msg != "" {
			return nil, msg
		}
		c, ok := snbtRGB(v)
		if !ok {
			return nil, "Can't parse particle options: color is not a colour"
		}
		o.Color = c
		if msg := scale(); msg != "" {
			return nil, msg
		}
	case optDustTrans:
		for _, f := range []struct {
			key string
			dst *int32
		}{{"from_color", &o.Color}, {"to_color", &o.ToColor}} {
			v, msg := need(f.key)
			if msg != "" {
				return nil, msg
			}
			c, ok := snbtRGB(v)
			if !ok {
				return nil, "Can't parse particle options: " + f.key + " is not a colour"
			}
			*f.dst = c
		}
		if msg := scale(); msg != "" {
			return nil, msg
		}
	case optColor:
		v, msg := need("color")
		if msg != "" {
			return nil, msg
		}
		c, ok := snbtARGB(v)
		if !ok {
			return nil, "Can't parse particle options: color is not a colour"
		}
		o.Color = c
	case optSpell:
		o.Color, o.Power = -1, 1 // SpellParticleOption's defaults
		if v, ok := tag["color"]; ok {
			c, ok := snbtRGB(v)
			if !ok {
				return nil, "Can't parse particle options: color is not a colour"
			}
			o.Color = c
		}
		if v, ok := tag["power"]; ok {
			f, ok := snbtFloat(v)
			if !ok {
				return nil, "Can't parse particle options: power is not a number"
			}
			o.Power = float32(f)
		}
	case optPower:
		o.Power = 1
		if v, ok := tag["power"]; ok {
			f, ok := snbtFloat(v)
			if !ok {
				return nil, "Can't parse particle options: power is not a number"
			}
			o.Power = float32(f)
		}
	case optItem:
		v, msg := need("item")
		if msg != "" {
			return nil, msg
		}
		id, count, ok := particleItem(v)
		if !ok {
			return nil, "Can't parse particle options: unknown item"
		}
		o.Item = &attachproto.ItemStack{ID: id, Count: count}
	case optSculkCharge:
		v, msg := need("roll")
		if msg != "" {
			return nil, msg
		}
		f, ok := snbtFloat(v)
		if !ok {
			return nil, "Can't parse particle options: roll is not a number"
		}
		o.Roll = float32(f)
	case optShriek:
		v, msg := need("delay")
		if msg != "" {
			return nil, msg
		}
		n, ok := snbtInt(v)
		if !ok {
			return nil, "Can't parse particle options: delay is not a whole number"
		}
		o.Delay = int32(n)
	case optTrail:
		v, msg := need("target")
		if msg != "" {
			return nil, msg
		}
		x, y, z, ok := snbtVec3(v)
		if !ok {
			return nil, "Can't parse particle options: target is not a position"
		}
		o.TX, o.TY, o.TZ = x, y, z
		cv, msg := need("color")
		if msg != "" {
			return nil, msg
		}
		c, ok := snbtRGB(cv)
		if !ok {
			return nil, "Can't parse particle options: color is not a colour"
		}
		o.Color = c
		dv, msg := need("duration")
		if msg != "" {
			return nil, msg
		}
		n, ok := snbtInt(dv)
		if !ok || n <= 0 { // ExtraCodecs.POSITIVE_INT
			return nil, "Can't parse particle options: duration must be positive"
		}
		o.Ticks = int32(n)
	case optVibration:
		v, msg := need("destination")
		if msg != "" {
			return nil, msg
		}
		dest, ok := v.(map[string]any)
		if !ok {
			return nil, "Can't parse particle options: destination is not a position source"
		}
		typ, _ := dest["type"].(string)
		if strings.TrimPrefix(typ, "minecraft:") != "block" {
			// An entity source (source_entity by UUID) has no way to
			// name an engine entity from a command yet.
			return nil, "Can't parse particle options: only a block destination is supported"
		}
		x, y, z, ok := snbtVec3(dest["pos"])
		if !ok {
			return nil, "Can't parse particle options: destination pos is not a position"
		}
		o.TX, o.TY, o.TZ = math.Floor(x), math.Floor(y), math.Floor(z)
		tv, msg := need("arrival_in_ticks")
		if msg != "" {
			return nil, msg
		}
		n, ok := snbtInt(tv)
		if !ok {
			return nil, "Can't parse particle options: arrival_in_ticks is not a whole number"
		}
		o.Ticks = int32(n)
	}
	return o, ""
}

// snbtRGB is ExtraCodecs.RGB_COLOR_CODEC: a packed int, or [r, g, b] as
// floats from 0 to 1.
func snbtRGB(v any) (int32, bool) {
	if n, ok := v.(int64); ok {
		return int32(n) & 0xFFFFFF, true
	}
	l, ok := snbtFloats(v, 3)
	if !ok {
		return 0, false
	}
	return int32(unitByte(l[0])<<16 | unitByte(l[1])<<8 | unitByte(l[2])), true
}

// snbtARGB is ExtraCodecs.ARGB_COLOR_CODEC: a packed int, or [r, g, b, a]
// as floats from 0 to 1.
func snbtARGB(v any) (int32, bool) {
	if n, ok := v.(int64); ok {
		return int32(n), true
	}
	l, ok := snbtFloats(v, 4)
	if !ok {
		return 0, false
	}
	return int32(uint32(unitByte(l[3]))<<24 | uint32(unitByte(l[0]))<<16 | uint32(unitByte(l[1]))<<8 | uint32(unitByte(l[2]))), true
}

// unitByte is ARGB.as8BitChannel: a 0–1 float as 0–255.
func unitByte(f float64) int {
	return int(math.Floor(math.Min(math.Max(f, 0), 1) * 255))
}

// snbtFloats reads a list of n numbers (a plain list or a typed array).
func snbtFloats(v any, n int) ([]float64, bool) {
	items, ok := v.([]any) // parseSNBT gives lists and typed arrays alike as []any
	if !ok {
		return nil, false
	}
	if len(items) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, it := range items {
		f, ok := snbtFloat(it)
		if !ok {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// snbtVec3 reads Vec3.CODEC / BlockPos.CODEC: three numbers.
func snbtVec3(v any) (x, y, z float64, ok bool) {
	l, ok := snbtFloats(v, 3)
	if !ok {
		return 0, 0, 0, false
	}
	return l[0], l[1], l[2], true
}

// particleBlockState is BlockState's codec as BlockParticleOption reads it:
// a state string ("minecraft:oak_log[axis=x]") or {Name, Properties}.
func particleBlockState(v any) (uint32, bool) {
	switch b := v.(type) {
	case string:
		return parseBlockState(b)
	case map[string]any:
		name, _ := b["Name"].(string)
		if name == "" {
			return 0, false
		}
		props, _ := b["Properties"].(map[string]any)
		if len(props) == 0 {
			return parseBlockState(name)
		}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, props[k]))
		}
		return parseBlockState(name + "[" + strings.Join(parts, ",") + "]")
	}
	return 0, false
}

// particleItem is ItemStackTemplate's codec: an item id, or {id, count}.
// Components are not carried.
func particleItem(v any) (int32, int32, bool) {
	var id string
	count := int64(1)
	switch it := v.(type) {
	case string:
		id = it
	case map[string]any:
		id, _ = it["id"].(string)
		if c, ok := it["count"]; ok {
			n, ok := snbtInt(c)
			if !ok || n < 1 {
				return 0, 0, false
			}
			count = n
		}
	default:
		return 0, 0, false
	}
	item, ok := itemByName[strings.TrimPrefix(id, "minecraft:")]
	if !ok || item == 0 {
		return 0, 0, false
	}
	return item, int32(count), true
}
