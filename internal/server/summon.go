package server

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// /summon <entity> [<pos>] [<nbt>] (SummonCommand): any mob the engine runs,
// and the other entities it has a model for — a lightning bolt, primed TNT,
// an experience orb, an armour stand, a boat or minecart, a firework rocket,
// a dropped item (whose NBT names it), a falling block, evoker fangs and the
// thrown and shot projectiles. The entity appears at the source's position
// unless one is given, facing south (yaw 0) unless its NBT turns it.
//
// The NBT is read the way each entity's readAdditionalSaveData reads it, for
// the keys the engine models (summonNBTKeys). A key the engine has no model
// for is refused by name rather than silently dropped, so a command that
// cannot do what it says fails instead of half-working.

// notSummonable are the types EntityType.Builder.noSummon marks: vanilla's
// entity argument refuses them outright.
var notSummonable = entitySet("player", "fishing_bobber")

// notInPeaceful are the types EntityType.Builder.notInPeaceful marks:
// SummonCommand refuses them on Peaceful.
var notInPeaceful = entitySet("blaze", "bogged", "breeze", "cave_spider", "creaking", "creeper", "drowned",
	"elder_guardian", "enderman", "endermite", "evoker", "ghast", "giant", "guardian", "hoglin", "husk",
	"illusioner", "magma_cube", "parched", "phantom", "piglin_brute", "pillager", "ravager", "silverfish",
	"skeleton", "slime", "spider", "stray", "vex", "vindicator", "warden", "witch", "wither",
	"wither_skeleton", "zoglin", "zombie", "zombie_villager", "zombified_piglin")

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
	id, ok := parseResourceID(args[0])
	name, vanillaNS := strings.CutPrefix(id, "minecraft:")
	if _, known := entityByName[name]; !ok || !vanillaNS || !known {
		p.tell(fmt.Sprintf("Can't find element '%s' of type 'minecraft:entity_type'", id))
		return
	}
	if notSummonable[entityByName[name]] {
		p.tell(fmt.Sprintf("Can't summon entity of type %s", id))
		return
	}
	et, ok := summonableType(name)
	if !ok { // a vanilla type the engine has no model for (a display, a marker, …)
		p.tell("Unable to summon entity")
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
	} else if len(rest) > 0 {
		p.tell(usage) // the NBT only follows a position
		return
	}
	e := evSummon{etype: et, x: x, y: y, z: z, dim: p.dim, by: p.eid}
	if len(rest) > 0 {
		v, err := parseSNBT(strings.Join(rest, " "))
		m, isMap := v.(map[string]any)
		if err != nil || !isMap {
			p.tell("Invalid NBT: " + strings.Join(rest, " "))
			return
		}
		if msg := summonNBTError(et, m); msg != "" {
			p.tell(msg)
			return
		}
		e.nbt = m
		if _, has := m["data"]; has { // Entity.customData keeps its tag types
			tv, err := parseSNBTTyped(strings.Join(rest, " "))
			tm, _ := tv.(map[string]any)
			d, isMap := tm["data"].(map[string]any)
			if err != nil || !isMap {
				p.tell("Invalid NBT: data must be a compound")
				return
			}
			e.data = d
		}
	}
	if !inSpawnableBounds(x, y, z) {
		p.tell("Invalid position for summon")
		return
	}
	s.hub.post(e)
}

// inSpawnableBounds is Level.isInSpawnableBounds for the block holding the
// position: inside the thirty-million-block world horizontally, and within
// twenty million of zero vertically.
func inSpawnableBounds(x, y, z float64) bool {
	bx, by, bz := math.Floor(x), math.Floor(y), math.Floor(z)
	return bx >= -30000000 && bx < 30000000 && bz >= -30000000 && bz < 30000000 &&
		by >= -20000000 && by < 20000000
}

// summonIgnoredNBT are keys the command itself overwrites (the id it was
// given, the position it places at): vanilla accepts and discards them.
var summonIgnoredNBT = keySet("id", "Pos")

// summonMobNBT is what every mob takes: Entity's name, silence, invulnerability,
// tags, rotation and motion, LivingEntity's health, Mob's NoAI and
// persistence.
var summonMobNBT = keySet("CustomName", "CustomNameVisible", "NoAI", "Silent", "Invulnerable",
	"PersistenceRequired", "Rotation", "Motion", "Tags", "Health", "data")

func keySet(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// summonNBTKeys is the NBT the engine models for one entity type, beyond the
// ignored pair.
func summonNBTKeys(et int) map[string]bool {
	switch {
	case et == entityTNT:
		return keySet("fuse", "Motion")
	case et == entityItem:
		return keySet("Item", "Motion")
	case et == entityXPOrb:
		return keySet("Value")
	case et == entityByName["armor_stand"]:
		return keySet("CustomName", "Rotation", "Small", "ShowArms", "NoBasePlate", "Marker", "Invisible")
	case cartTypes[et]:
		return nil
	case isBoatType(et):
		return keySet("Rotation")
	case et == entityFallingBlock:
		return keySet("BlockState", "Motion")
	case et == entityEvokerFangs:
		return keySet("Warmup")
	case summonThrown[et]:
		return keySet("Motion")
	case et == entityLightning || et == entityEndCrystal || et == entityFirework:
		return nil
	}
	keys := summonMobNBT
	var own []string
	switch {
	case eggOffspring[et]:
		own = append(own, "Age") // AgeableMob
	case eggSetBaby[et]:
		own = append(own, "IsBaby") // the zombie family, piglins, zoglins
	}
	switch et {
	case entitySheep:
		own = append(own, "Color")
	case entityCat, entityWolf, entityFrog, entityPig, entityCow, entityChicken:
		own = append(own, "variant") // VariantUtils: a registry key
	case entityAxolotl, entityHorse, entityLlama, entityTraderLlama, entityParrot:
		own = append(own, "Variant")
	case entityRabbit:
		own = append(own, "RabbitType")
	case entityFox, entityMooshroom:
		own = append(own, "Type")
	case entityCreeper:
		own = append(own, "powered")
	}
	if len(own) == 0 {
		return keys
	}
	out := keySet(own...)
	for k := range keys {
		out[k] = true
	}
	return out
}

// summonNBTError refuses the keys the engine does not model for the type,
// naming them; "" when every key is understood.
func summonNBTError(et int, nbt map[string]any) string {
	allowed := summonNBTKeys(et)
	var bad []string
	for k := range nbt {
		if !allowed[k] && !summonIgnoredNBT[k] {
			bad = append(bad, k)
		}
	}
	if im, ok := nbt["Item"].(map[string]any); ok && et == entityItem {
		for k := range im {
			if k != "id" && k != "count" {
				bad = append(bad, "Item."+k)
			}
		}
	}
	if len(bad) == 0 {
		return ""
	}
	sort.Strings(bad)
	return fmt.Sprintf("Unsupported NBT for minecraft:%s: %s (this server does not model it)",
		entityTypeName(et), strings.Join(bad, ", "))
}

// summonAt runs /summon on the hub.
func (h *hub) summonAt(players map[int32]*tracked, e evSummon) {
	var caller *player
	if t := players[e.by]; t != nil {
		caller = t.p
	}
	if h.rules.Difficulty == diffPeaceful && notInPeaceful[e.etype] {
		cmdFail(caller, "Monsters cannot be summoned in Peaceful difficulty")
		return
	}
	yaw, pitch, hasRot := nbtRotation(e.nbt)
	if !hasRot {
		yaw = e.yaw
	}
	vx, vy, vz, _ := nbtMotion(e.nbt)
	name := mobDisplayName(e.etype)
	switch et := e.etype; {
	case et == entityTNT:
		fuse := tntDefaultFuse
		if n, ok := snbtInt(e.nbt["fuse"]); ok {
			fuse = int(n)
		}
		// EntityType.create: a summoned charge is at rest where it was put
		// (the hop is TntBlock's, when a block is lit).
		pt := &primedTNT{eid: h.allocEID(), dim: e.dim, x: e.x, y: e.y, z: e.z, vx: vx, vy: vy, vz: vz, fuse: fuse}
		h.tnt = append(h.tnt, pt)
		h.showPrimedTNT(players, pt)
	case et == entityXPOrb:
		value := 0
		if n, ok := snbtInt(e.nbt["Value"]); ok {
			value = int(n)
		}
		h.spawnXPOrbIn(players, e.dim, value, e.x, e.y, e.z)
	case et == entityByName["armor_stand"]:
		st := &armorStand{eid: h.allocEID(), dim: e.dim, x: e.x, y: e.y, z: e.z, yaw: yaw}
		st.name = nbtName(e.nbt)
		st.setStandFlagsFrom(e.nbt)
		h.armorStands[st.eid] = st
		h.toNearbyEv(players, st.dim, st.x, st.z, h.standAddEv(st))
		if st.name != "" {
			h.toNearbyEv(players, st.dim, st.x, st.z, metaEv(nameMeta(st.eid, st.name)))
			name = st.name
		}
		if st.flagged() {
			h.toNearbyEv(players, st.dim, st.x, st.z, metaEv(standMeta(st)))
		}
	case et == entityItem:
		im, _ := e.nbt["Item"].(map[string]any)
		id, _ := im["id"].(string)
		item, ok := itemByName[strings.TrimPrefix(id, "minecraft:")]
		if !ok {
			cmdFail(caller, "Unable to summon entity") // an item entity with no item is discarded at once
			return
		}
		count := 1
		if n, ok := snbtInt(im["count"]); ok && n > 0 {
			count = int(n)
		}
		// ItemEntity.getName: the item's own name.
		h.spawnItemAt(players, e.dim, item, count, e.x, e.y, e.z, vx, vy, vz)
		name = strings.Trim(itemDisplay(item), "[]")
	case et == entityFallingBlock:
		state := uint32(0)
		if bs, ok := e.nbt["BlockState"].(map[string]any); ok {
			state, ok = blockStateNBT(bs)
			if !ok {
				cmdFail(caller, "Unable to summon entity")
				return
			}
		} else {
			state, _ = parseBlockState("sand") // FallingBlockEntity's default state
		}
		fb := &fallingBlock{eid: h.allocEID(), dim: e.dim, x: e.x, y: e.y, z: e.z, vx: vx, vy: vy, vz: vz,
			state: state, dropItem: true, hurtMax: fallDamageMaxDef}
		h.addFallingBlock(players, fb)
	case et == entityEvokerFangs:
		warmup := 0
		if n, ok := snbtInt(e.nbt["Warmup"]); ok {
			warmup = int(n)
		}
		h.addFang(players, e.dim, e.x, e.y, e.z, warmup, 0)
	case summonThrown[et]:
		a := h.launchProjectileIn(players, et, e.dim, e.x, e.y, e.z, vx, vy, vz)
		a.breaks = true // ThrowableProjectile.onHit: whatever it hits ends it
		switch et {
		case entityPearlProj:
			a.pearl = true // no owner to carry, so it just shatters
		case entityXPBottle:
			a.xpBottle, a.dmg = true, 0
		case entitySplashProj, entityLingerProj:
			a.splash, a.potion, a.lingering = true, potWater, et == entityLingerProj
		}
	case isBoatType(et):
		h.addVehicle(players, e.dim, et, e.x, e.y, e.z, yaw)
	case h.summonNonLivingAt(players, e): // carts, arrows, fireballs, end crystals, lightning, rockets
	default:
		m := h.summonMob(players, e) // the hub runs this under the COMMAND spawn cause
		if m == nil {
			cmdFail(caller, "Unable to summon entity")
			return
		}
		h.applySummonNBT(players, m, e.nbt)
		if len(e.data) > 0 {
			m.custom = tagCopy(e.data).(map[string]any)
		}
		if hasRot {
			m.yaw, m.headYaw, m.syaw = yaw, yaw, yaw
			m.snapLook()
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, yaw, pitch, false))
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entHead(m.eid, yaw))
		}
		if _, ok := e.nbt["Motion"]; ok {
			m.vx, m.vy, m.vz = vx, vy, vz
		}
		name = mobDisplayName(m.etype)
		if m.customName != "" {
			name = m.customName
		}
	}
	h.cmdOK(players, e.by)("Summoned new " + name)
}

// summonThrown are the thrown projectiles /summon makes beyond the arrows and
// fireballs summonNonLivingAt places: each shatters on whatever it hits.
var summonThrown = entitySet("ender_pearl", "experience_bottle", "splash_potion", "lingering_potion")

// nbtRotation is Entity's Rotation list: yaw, then pitch.
func nbtRotation(nbt map[string]any) (yaw, pitch float32, ok bool) {
	rot, isList := nbt["Rotation"].([]any)
	if !isList || len(rot) == 0 {
		return 0, 0, false
	}
	f, fok := snbtFloat(rot[0])
	if !fok {
		return 0, 0, false
	}
	yaw = float32(f)
	if len(rot) > 1 {
		if p, pok := snbtFloat(rot[1]); pok {
			pitch = float32(max(-90, min(90, p))) // Entity.setXRot clamps
		}
	}
	return yaw, pitch, true
}

// nbtMotion is Entity's Motion list, blocks per tick. Vanilla drops any axis
// beyond ten (Entity.load: fixed at zero).
func nbtMotion(nbt map[string]any) (vx, vy, vz float64, ok bool) {
	mo, isList := nbt["Motion"].([]any)
	if !isList || len(mo) != 3 {
		return 0, 0, 0, false
	}
	var v [3]float64
	for i, c := range mo {
		f, fok := snbtFloat(c)
		if !fok {
			return 0, 0, 0, false
		}
		if math.Abs(f) <= 10 {
			v[i] = f
		}
	}
	return v[0], v[1], v[2], true
}

// nbtBool reads a boolean key (a byte in SNBT: 1b, true).
func nbtBool(nbt map[string]any, key string) (bool, bool) {
	n, ok := snbtInt(nbt[key])
	return n != 0, ok
}

// blockStateNBT is a BlockState compound: Name and optional Properties.
func blockStateNBT(bs map[string]any) (uint32, bool) {
	name, _ := bs["Name"].(string)
	if name == "" {
		return 0, false
	}
	arg := name
	if props, ok := bs["Properties"].(map[string]any); ok && len(props) > 0 {
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			v, _ := props[k].(string)
			parts = append(parts, k+"="+v)
		}
		arg += "[" + strings.Join(parts, ",") + "]"
	}
	return parseBlockState(arg)
}

// summonMob spawns a mob the way its kind is configured.
func (h *hub) summonMob(players map[int32]*tracked, e evSummon) *mob {
	return h.spawnConfigured(players, e.etype, e.dim, e.x, e.y, e.z)
}

// spawnConfigured is EntityType.spawn with finalizeSpawn: the mob comes out
// set up as its kind is — a zombie hostile and geared, a villager with its
// trades, a nether mob with its brain. /summon, spawn eggs and a dispensed
// egg all spawn this way.
func (h *hub) spawnConfigured(players map[int32]*tracked, etype, dim int, x, y, z float64) *mob {
	switch {
	case etype == entityEnderDragon:
		return h.spawnHostileYIn(players, etype, dim, x, y, z)
	case etype == entitySulfurCube:
		return h.spawnSulfurCube(players, dim, x, y, z, false)
	case etype == entityVillager || etype == entityIronGolem:
		m := h.spawnMobIn(players, etype, dim, x, y, z)
		if m != nil {
			h.configureVillageMob(players, m)
		}
		return m
	case netherConfigured(etype):
		m := h.spawnMobIn(players, etype, dim, x, y, z)
		if m != nil {
			h.configureNetherMob(players, m)
		}
		return m
	case isRosterPassive(etype):
		return h.spawnSpecies(players, etype, dim, x, y, z)
	case etype == entityPig || etype == entityCow || etype == entitySheep || etype == entityChicken:
		// The four farm animals have no roster row: without this they fell
		// through to the hostile path and came out chasing players.
		m := h.spawnSpecies(players, etype, dim, x, y, z)
		if m != nil && etype == entityChicken {
			m.eggIn = eggLayMin + h.rng.Intn(eggLayMax-eggLayMin) // Chicken's eggTime
		}
		return m
	}
	return h.spawnHostileYIn(players, etype, dim, x, y, z)
}

// applySummonNBT reads the NBT onto a summoned mob, as Entity, LivingEntity,
// Mob and the species' readAdditionalSaveData read it, and shows its
// viewers what changed.
func (h *hub) applySummonNBT(players map[int32]*tracked, m *mob, nbt map[string]any) {
	if nbt == nil {
		return
	}
	send := func(body []byte) { h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(body)) }
	if n := nbtName(nbt); n != "" {
		m.customName = n
		shown, _ := nbtBool(nbt, "CustomNameVisible")
		m.nameHidden = !shown // Entity: CustomNameVisible defaults to false
		send(nameMetaVis(m.eid, n, shown))
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
		send(mobHealthMeta(m.eid, m.health))
	}
	if on, _ := nbtBool(nbt, "PersistenceRequired"); on {
		m.persistent = true
	}
	if on, _ := nbtBool(nbt, "Invulnerable"); on {
		m.invulnerable = true
	}
	if on, _ := nbtBool(nbt, "Silent"); on {
		m.silent = true
		send(boolMeta(m.eid, metaIndexSilent, true))
	}
	if on, _ := nbtBool(nbt, "NoAI"); on {
		m.noAI = true
		m.vx, m.vy, m.vz = 0, 0, 0
		send(mobFlagsByte(m.eid, m.mobFlags()))
	}
	h.applySummonSpeciesNBT(players, m, nbt)
}

// applySummonSpeciesNBT reads the species' own keys: age, variant, colour.
func (h *hub) applySummonSpeciesNBT(players map[int32]*tracked, m *mob, nbt map[string]any) {
	send := func(body []byte) { h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(body)) }
	if age, ok := snbtInt(nbt["Age"]); ok && eggOffspring[m.etype] {
		// AgeableMob.setAge: below zero is a baby that grows up when it
		// reaches zero.
		baby := age < 0
		m.baby = baby
		m.growLeft = 0
		if baby {
			m.growLeft = int(-age)
		}
		send(mobBabyMeta(m, baby))
	}
	if baby, ok := nbtBool(nbt, "IsBaby"); ok && eggSetBaby[m.etype] && baby != m.baby {
		m.baby = baby
		if m.etype != entityZoglin {
			m.setBabySpeed(baby)
		}
		send(mobBabyMeta(m, baby))
	}
	if c, ok := snbtInt(nbt["Color"]); ok && m.etype == entitySheep {
		m.color = int8(c & 15) // DyeColor.LEGACY_ID_CODEC
		send(sheepMeta(m, m.sheared))
	}
	if on, ok := nbtBool(nbt, "powered"); ok && m.etype == entityCreeper {
		m.charged = on
		send(creeperPoweredMeta(m.eid, on))
	}
	if v, ok := summonVariant(m.etype, nbt); ok {
		m.variant, m.variantSet = v, true
		if vm := variantMeta(m); vm != nil {
			send(vm)
		}
	}
}

// summonVariant is the variant the species' NBT names, in the engine's
// numbering; ok false when there is none or the name is unknown (vanilla
// keeps the rolled one then).
func summonVariant(et int, nbt map[string]any) (int32, bool) {
	str := func(key string) string { s, _ := nbt[key].(string); return s }
	num := func(key string) (int32, bool) { n, ok := snbtInt(nbt[key]); return int32(n), ok }
	temp := map[string]int32{"minecraft:cold": tempCold, "minecraft:temperate": tempTemperate, "minecraft:warm": tempWarm}
	switch et {
	case entityCat:
		v, ok := catVariantID[nsID(str("variant"))]
		return v, ok
	case entityWolf:
		v, ok := wolfVariantID[nsID(str("variant"))]
		return v, ok
	case entityFrog:
		v, ok := map[string]int32{"minecraft:cold": frogCold, "minecraft:temperate": frogTemperate,
			"minecraft:warm": frogWarm}[nsID(str("variant"))]
		return v, ok
	case entityPig, entityCow, entityChicken:
		v, ok := temp[nsID(str("variant"))]
		return v, ok
	case entityAxolotl:
		if n, ok := num("Variant"); ok {
			return byIDOrZero(n, axolotlBlue+1), true
		}
	case entityHorse:
		return num("Variant")
	case entityLlama, entityTraderLlama:
		if n, ok := num("Variant"); ok {
			return byIDOrZero(n, llamaCoats), true
		}
	case entityParrot:
		if n, ok := num("Variant"); ok {
			return byIDOrZero(n, parrotColours), true
		}
	case entityRabbit:
		if n, ok := num("RabbitType"); ok {
			if n == rabbitEvil || (n >= rabbitBrown && n <= rabbitSalt) {
				return n, true
			}
			return rabbitBrown, true // Rabbit.Variant.LEGACY_CODEC: unknown ids are brown
		}
	case entityFox:
		v, ok := map[string]int32{"red": foxRed, "snow": foxSnow}[str("Type")]
		return v, ok
	case entityMooshroom:
		v, ok := map[string]int32{"red": mooshroomRed, "brown": mooshroomBrown}[str("Type")]
		return v, ok
	}
	return 0, false
}

// byIDOrZero is ByIdMap.continuous with the ZERO out-of-bounds strategy.
func byIDOrZero(n, size int32) int32 {
	if n < 0 || n >= size {
		return 0
	}
	return n
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
