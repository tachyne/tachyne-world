package server

import (
	"hash/fnv"
	"math"
	"sort"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/attribute"
	api "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Attribute sync (ServerEntity.addPairing / sendChanges): a viewer that
// starts tracking a living entity gets every syncable attribute it carries,
// and afterwards the ones that changed. The client steers a ridden mount by
// its MOVEMENT_SPEED and JUMP_STRENGTH, draws a player's hearts from
// MAX_HEALTH and reaches by the interaction ranges, so without this the
// engine's rolled horses and its attribute pipeline stopped at the server.

// syncableAttrs is Attributes.java's setSyncable(true) set.
var syncableAttrs = map[api.ID]bool{
	api.Armor: true, api.ArmorToughness: true, api.AttackSpeed: true, api.BlockBreakSpeed: true,
	api.BlockInteractionRange: true, api.BurningTime: true, api.ExplosionKnockbackResistance: true,
	api.EntityInteractionRange: true, api.FallDamageMultiplier: true, api.FlyingSpeed: true,
	api.Gravity: true, api.JumpStrength: true, api.Luck: true, api.MaxAbsorption: true,
	api.MaxHealth: true, api.MiningEfficiency: true, api.MovementEfficiency: true,
	api.MovementSpeed: true, api.OxygenBonus: true, api.SafeFallDistance: true, api.Scale: true,
	api.SneakingSpeed: true, api.StepHeight: true, api.SubmergedMiningSpeed: true,
	api.SweepingDamageRatio: true, api.WaterMovementEfficiency: true,
}

// attributeFrame is the snapshot of every syncable attribute the map holds,
// sorted by name so the frame (and its fingerprint) is stable.
func attributeFrame(eid int32, a *attribute.Map) attachproto.EntityAttributes {
	fr := attachproto.EntityAttributes{EID: eid}
	a.Each(func(id api.ID, in *attribute.Instance) {
		if !syncableAttrs[id] {
			return
		}
		snap := attachproto.AttributeSnapshot{Name: string(id), Base: in.Base()}
		for _, m := range in.Modifiers() {
			snap.Modifiers = append(snap.Modifiers, attachproto.AttributeModifier{ID: m.Source, Amount: m.Amount, Op: int32(m.Op)})
		}
		fr.Attrs = append(fr.Attrs, snap)
	})
	sort.Slice(fr.Attrs, func(i, j int) bool { return fr.Attrs[i].Name < fr.Attrs[j].Name })
	return fr
}

// attrFingerprint hashes a frame's contents: equal frames, equal prints.
func attrFingerprint(fr attachproto.EntityAttributes) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	put := func(v float64) {
		u := math.Float64bits(v)
		for i := range buf {
			buf[i] = byte(u >> (8 * i))
		}
		h.Write(buf[:])
	}
	for _, a := range fr.Attrs {
		h.Write([]byte(a.Name))
		put(a.Base)
		for _, m := range a.Modifiers {
			h.Write([]byte(m.ID))
			put(m.Amount)
			h.Write([]byte{byte(m.Op)})
		}
	}
	return h.Sum64()
}

// mobAttrFrame is a mob's frame (empty when nothing syncable was ever set).
// The engine keeps a mob's MOVEMENT_SPEED in per-update steps (attrToStep ×
// vanilla's figure); the client wants vanilla's, so the base and any flat
// modifier are scaled back — proportional modifiers mean the same in either.
func mobAttrFrame(m *mob) attachproto.EntityAttributes {
	if m.attrs == nil {
		return attachproto.EntityAttributes{EID: m.eid}
	}
	fr := attributeFrame(m.eid, m.attrs)
	for i := range fr.Attrs {
		if fr.Attrs[i].Name != string(api.MovementSpeed) {
			continue
		}
		fr.Attrs[i].Base /= attrToStep
		for j := range fr.Attrs[i].Modifiers {
			if fr.Attrs[i].Modifiers[j].Op == int32(api.AddValue) {
				fr.Attrs[i].Modifiers[j].Amount /= attrToStep
			}
		}
	}
	return fr
}

// playerAttrFrame is a player's frame.
func playerAttrFrame(t *tracked) attachproto.EntityAttributes {
	return attributeFrame(t.p.eid, t.playerAttrs())
}

// syncAttributes is the once-a-second sendChanges: any living entity whose
// syncable attributes differ from what was last sent gets a fresh frame —
// to its viewers, and for a player to itself as well.
func (h *hub) syncAttributes(players map[int32]*tracked) {
	for _, t := range players {
		fr := playerAttrFrame(t)
		if fp := attrFingerprint(fr); fp != t.attrSent {
			t.attrSent = fp
			t.p.trySendEv(fr)
			h.toNearbyEv(players, t.dim, t.x, t.z, fr)
		}
	}
	for _, m := range h.mobs {
		if m.attrs == nil || m.dying > 0 {
			continue
		}
		fr := mobAttrFrame(m)
		if fp := attrFingerprint(fr); fp != m.attrSent {
			m.attrSent = fp
			if len(fr.Attrs) > 0 {
				h.toNearbyEv(players, m.dim, m.x, m.z, fr)
			}
		}
	}
}

// sendAttrsTo is the pairing half for one viewer: the entity's current
// frame, when it carries anything.
func sendAttrsTo(viewer *tracked, fr attachproto.EntityAttributes) {
	if len(fr.Attrs) > 0 {
		viewer.p.trySendEv(fr)
	}
}
