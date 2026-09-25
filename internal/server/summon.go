package server

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// /summon <entity> [<pos>] [<nbt>] (SummonCommand): any mob the engine runs,
// and the other entities it has — a lightning bolt, primed TNT, an
// experience orb, an armour stand, a boat or minecart, a firework rocket and
// a dropped item (whose NBT names it). The entity appears at the caller's
// position unless one is given, and a mob takes the common NBT: CustomName,
// Tags, Health, Rotation, PersistenceRequired.

// summonOther are the entity types /summon makes that are not mobs.
var summonOther = map[string]bool{
	"lightning_bolt": true, "tnt": true, "experience_orb": true, "armor_stand": true,
	"item": true, "firework_rocket": true,
}

func (s *Server) cmdSummon(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	usage := "Usage: /summon <entity> [<x> <y> <z>] [<nbt>]"
	if len(args) < 1 {
		p.tell(usage)
		return
	}
	name := strings.TrimPrefix(args[0], "minecraft:")
	et, ok := summonableType(name)
	if !ok {
		if id, isEnt := entityByName[name]; isEnt && (summonOther[name] || vehicleEntityType(id)) {
			et, ok = id, true
		}
	}
	if !ok {
		p.tell(fmt.Sprintf("Can't find element 'minecraft:%s' of type 'minecraft:entity_type'", name))
		return
	}
	x, y, z := p.x, p.y, p.z // SummonCommand: the source's position by default
	rest := args[1:]
	if len(rest) >= 3 {
		nx, ny, nz, ok := parsePosition(rest[:3], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			p.tell(usage)
			return
		}
		x, y, z, rest = nx, ny, nz, rest[3:]
	}
	e := evSummon{etype: et, x: x, y: y, z: z, dim: p.dim, by: p.eid}
	if len(rest) > 0 {
		v, err := parseSNBT(strings.Join(rest, " "))
		m, isMap := v.(map[string]any)
		if err != nil || !isMap {
			p.tell("Invalid NBT: " + strings.Join(rest, " "))
			return
		}
		e.nbt = m
	}
	s.hub.post(e)
}

// vehicleEntityType reports whether an entity type is a boat or minecart.
func vehicleEntityType(et int) bool {
	for _, v := range vehicleItems {
		if v == et {
			return true
		}
	}
	return false
}

// summonAt runs /summon on the hub.
func (h *hub) summonAt(players map[int32]*tracked, e evSummon) {
	name := entityTypeName(e.etype)
	fail := func() {
		if t := players[e.by]; t != nil {
			t.p.trySendEv(chatEv("Unable to summon entity"))
		}
	}
	yaw := e.yaw
	if rot, ok := e.nbt["Rotation"].([]any); ok && len(rot) > 0 {
		if f, ok := snbtFloat(rot[0]); ok {
			yaw = float32(f)
		}
	}
	switch {
	case name == "lightning_bolt":
		if e.dim != dimOverworld {
			fail()
			return
		}
		h.strikeLightning(players, e.x, e.y, e.z, false)
	case name == "tnt":
		fuse := 80
		if n, ok := snbtInt(e.nbt["fuse"]); ok {
			fuse = int(n)
		}
		// EntityType.create: a summoned charge is at rest where it was put
		// (the hop is TntBlock's, when a block is lit).
		pt := &primedTNT{eid: h.allocEID(), dim: e.dim, x: e.x, y: e.y, z: e.z, fuse: fuse}
		h.tnt = append(h.tnt, pt)
		h.showPrimedTNT(players, pt)
	case name == "experience_orb":
		value := 0
		if n, ok := snbtInt(e.nbt["Value"]); ok {
			value = int(n)
		}
		h.spawnXPOrbIn(players, e.dim, value, e.x, e.y, e.z)
	case name == "armor_stand":
		st := &armorStand{eid: h.allocEID(), dim: e.dim, x: e.x, y: e.y, z: e.z, yaw: yaw}
		st.name = nbtName(e.nbt)
		h.armorStands[st.eid] = st
		h.toNearbyEv(players, st.dim, st.x, st.z, h.standAddEv(st))
		if st.name != "" {
			h.toNearbyEv(players, st.dim, st.x, st.z, metaEv(nameMeta(st.eid, st.name)))
		}
	case name == "firework_rocket":
		h.spawnRocket(players, e.dim, e.x, e.y, e.z, 0, invStack{item: itemByName["firework_rocket"], count: 1})
	case name == "item":
		im, _ := e.nbt["Item"].(map[string]any)
		id, _ := im["id"].(string)
		item, ok := itemByName[strings.TrimPrefix(id, "minecraft:")]
		if !ok {
			fail() // an item entity with no item is discarded at once
			return
		}
		count := 1
		if n, ok := snbtInt(im["count"]); ok && n > 0 {
			count = int(n)
		}
		h.spawnItemIn(players, e.dim, item, count, e.x, e.y, e.z)
	case vehicleEntityType(e.etype):
		v := &vehicle{eid: h.allocEID(), dim: e.dim, etype: e.etype, x: e.x, y: e.y, z: e.z, sx: e.x, sy: e.y, sz: e.z}
		if !cartTypes[e.etype] {
			v.yaw, v.syaw, v.yawO = yaw, yaw, yaw
		}
		binary.BigEndian.PutUint32(v.uuid[12:], uint32(v.eid))
		if chestBoatTypes[e.etype] {
			v.chest = &chest{}
		}
		initCartKind(v)
		h.vehicles[v.eid] = v
		h.toNearbyEv(players, e.dim, e.x, e.z, entAdd(v.eid, e.etype, v.uuid, e.x, e.y, e.z, v.yaw, 0))
	default:
		if h.summonNonLivingAt(players, e) { // projectiles, end crystals, …
			h.cmdOK(players, e.by)("Summoned new " + mobDisplayName(e.etype))
			return
		}
		m := h.summonMob(players, e) // the hub runs this under the COMMAND spawn cause
		if m == nil {
			fail()
			return
		}
		h.applySummonNBT(players, m, e.nbt, yaw)
		name = mobDisplayName(m.etype)
		if m.customName != "" {
			name = m.customName
		}
		h.cmdOK(players, e.by)("Summoned new " + name)
		return
	}
	h.cmdOK(players, e.by)("Summoned new " + mobDisplayName(e.etype))
}

// summonMob spawns a mob the way its kind is configured.
func (h *hub) summonMob(players map[int32]*tracked, e evSummon) *mob {
	switch {
	case e.etype == entityEnderDragon:
		return h.spawnHostileYIn(players, e.etype, e.dim, e.x, e.y, e.z)
	case e.etype == entitySulfurCube:
		return h.spawnSulfurCube(players, e.dim, e.x, e.y, e.z, false)
	case e.etype == entityVillager || e.etype == entityIronGolem:
		m := h.spawnMobIn(players, e.etype, e.dim, e.x, e.y, e.z)
		if m != nil {
			h.configureVillageMob(players, m)
		}
		return m
	case netherConfigured(e.etype):
		m := h.spawnMobIn(players, e.etype, e.dim, e.x, e.y, e.z)
		if m != nil {
			h.configureNetherMob(players, m)
		}
		return m
	case isRosterPassive(e.etype):
		return h.spawnSpecies(players, e.etype, e.dim, e.x, e.y, e.z)
	}
	return h.spawnHostileYIn(players, e.etype, e.dim, e.x, e.y, e.z)
}

// applySummonNBT reads the common entity NBT onto a summoned mob.
func (h *hub) applySummonNBT(players map[int32]*tracked, m *mob, nbt map[string]any, yaw float32) {
	if nbt == nil {
		return
	}
	if n := nbtName(nbt); n != "" {
		m.customName = n
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(nameMeta(m.eid, n)))
	}
	if tags, ok := nbt["Tags"].([]any); ok {
		for _, tg := range tags {
			if s, ok := tg.(string); ok {
				if m.tags == nil {
					m.tags = map[string]bool{}
				}
				m.tags[s] = true
			}
		}
	}
	if hp, ok := snbtFloat(nbt["Health"]); ok && hp > 0 {
		m.health = min(int(hp+0.5), m.maxHP())
	}
	if p, ok := snbtInt(nbt["PersistenceRequired"]); ok && p != 0 {
		m.persistent = true
	}
	if _, ok := nbt["Rotation"]; ok {
		m.yaw = yaw
	}
}

// nbtName is CustomName as a string or a text component's text.
func nbtName(nbt map[string]any) string {
	switch n := nbt["CustomName"].(type) {
	case string:
		return n
	case map[string]any:
		s, _ := n["text"].(string)
		return s
	}
	return ""
}

// snbtFloat reads a number from a parsed value.
func snbtFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
