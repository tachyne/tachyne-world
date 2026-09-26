package server

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Raids — a faithful core of the vanilla 1.21.5 Raid.java. Killing a
// pillager-patrol captain grants Bad Omen; carrying it into a village consumes
// the omen and triggers a raid: waves of illagers spawn around the village and
// attack. Clear every wave to win. (Hero of the Village, the warning bell, and
// save/restore persistence are follow-ups.)

const (
	raidSpawnRadius     = 24   // raiders spawn within this of the village centre
	raidBarRange        = 64   // players within this see the raid boss bar
	raidNoPlayerTimeout = 2400 // ticks with nobody near before a raid gives up (2 min)
	badOmenSecs         = 6000 // Bad Omen lasts long enough to walk to a village
	raidOmenSecs        = 30   // the fuse between reaching the village and the horn
)

// raiderWaves[etype][wave 1..7] = how many of that raider spawn in that wave
// (Raid.RaiderType.spawnsPerWaveBeforeBonus, vanilla 1.21.5; bonus spawns on
// higher difficulty are a follow-up).
var raiderWaves = map[int][8]int{
	entityPillager:   {0, 4, 3, 3, 4, 4, 4, 2},
	entityVindicator: {0, 0, 2, 0, 1, 4, 2, 5},
	entityEvoker:     {0, 0, 0, 0, 0, 1, 1, 2},
	entityWitch:      {0, 0, 0, 0, 3, 0, 0, 1},
	entityRavager:    {0, 0, 0, 1, 0, 1, 0, 2},
}

// raiderOrder is the order a wave's raiders spawn in (Raid.RaiderType's
// declaration order), which decides the wave's leader: the first one that
// can lead.
var raiderOrder = []int{entityVindicator, entityEvoker, entityPillager, entityWitch, entityRavager}

// canBeLeader is PatrollingMonster.canBeLeader: every raider but the witch
// and the ravager can carry the banner.
func canBeLeader(etype int) bool {
	return etype != entityWitch && etype != entityRavager
}

// makeCaptain puts the ominous banner on a raider's head and makes it its
// group's leader (Raid.setLeader, PatrollingMonster.finalizeSpawn): the
// banner shows to everyone who sees it later and always drops with it
// (a drop chance of 2.0).
func (h *hub) makeCaptain(players map[int32]*tracked, m *mob) {
	m.patrolCaptain = true
	m.gear[0] = ominousBanner()
	m.gearSure[0] = true
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
}

// isCaptain is Raider.isCaptain: the group's leader, wearing the ominous
// banner (ItemStack.matches: a plain or renamed banner is not it).
func isCaptain(m *mob) bool {
	return m.patrolCaptain && m.gear[0].count == 1 && sameItemComponents(m.gear[0], ominousBanner())
}

type raid struct {
	center      blockPos
	uuid        [16]byte
	omenLevel   int            // the Raid Omen level that started it (Raid.raidOmenLevel, 1-5)
	bonusDone   bool           // the omen-level bonus wave has spawned
	pending     int            // restored raid: saved raiders whose chunks have not loaded yet
	wave        int            // waves spawned so far
	numGroups   int            // total waves (by difficulty)
	waveSpawned int            // raiders spawned in the current wave (for the bar)
	alive       map[int32]bool // raider eids currently spawned
	shown       map[int32]bool // player eids currently shown the bar
	idleTicks   int            // ticks with no player near
	secsActive  int            // Raid.ticksActive, in the 1 Hz updates (48000 ticks = 2400)
	lostLeft    int            // seconds the defeat bar still shows (0 = not lost)
	wonLeft     int            // seconds the victory bar still shows (0 = not won)
	cooldown    int            // seconds until the next wave (Raid.raidCooldownTicks / 20)
	totalHealth float64        // the wave's raiders' health when they joined (Raid.totalHealth)
	title       string         // the bar's current name
}

// raidTimeoutSecs is Raid.tick's 48000-tick limit, in 1 Hz updates.
const raidTimeoutSecs = 48000 / 20

// raidDefeatSecs is how long the defeat bar stays up (postRaidTicks ≥ 600);
// a victory celebrates as long (celebrationTicks).
const raidDefeatSecs = 600 / 20

// raidCooldownSecs is the pause before each wave (raidCooldownTicks 300), the
// bar filling as it runs down.
const raidCooldownSecs = 300 / 20

// isVillage is ServerLevel.isVillage: a village point of interest (a bed,
// a bell, a workstation) in the centre's section or one next to it.
func (h *hub) isVillage(center blockPos) bool { return h.closeToVillage(center, 1) }

// closeToVillage is ServerLevel.isCloseToVillage: a village point of
// interest within n sections of the position's.
func (h *hub) closeToVillage(pos blockPos, n int) bool {
	w := h.poiWorld(dimOverworld)
	if w == nil {
		return false
	}
	cx, cy, cz := pos.x>>4, pos.y>>4, pos.z>>4
	return len(w.POIsNear(pos.x, pos.y, pos.z, 16*(n+1), func(p world.POI) bool {
		return poiKindIsVillage(p.Kind) && abs(p.X>>4-cx) <= n && abs(p.Y>>4-cy) <= n && abs(p.Z>>4-cz) <= n
	})) > 0
}

// nearbyVillageSection is moveRaidCenterToNearbyVillageSection's search: of
// the sections in the 5×5×5 cube around the centre's, the nearest village
// one, by its centre.
func (h *hub) nearbyVillageSection(center blockPos) (blockPos, bool) {
	cx, cy, cz := center.x>>4, center.y>>4, center.z>>4
	best, bestD, found := blockPos{}, 0, false
	for sx := cx - 2; sx <= cx+2; sx++ {
		for sy := cy - 2; sy <= cy+2; sy++ {
			for sz := cz - 2; sz <= cz+2; sz++ {
				c := blockPos{sx*16 + 8, sy*16 + 8, sz*16 + 8}
				if !h.isVillage(c) {
					continue
				}
				dx, dy, dz := c.x-center.x, c.y-center.y, c.z-center.z
				d := dx*dx + dy*dy + dz*dz
				if !found || d < bestD {
					best, bestD, found = c, d, true
				}
			}
		}
	}
	return best, found
}

// raidUUID is the raid's boss-bar identity, stable per village centre.
func raidUUID(center blockPos) [16]byte {
	var u [16]byte
	binary.BigEndian.PutUint32(u[8:], 0x52414944) // "RAID"
	binary.BigEndian.PutUint32(u[12:], uint32(center.x*31+center.z))
	return u
}

// raidWaveCount is the wave total by difficulty (Raid.getNumGroups).
func raidWaveCount(diff int) int {
	switch diff {
	case diffEasy:
		return 3
	case diffHard:
		return 7
	default:
		return 5
	}
}

// startRaid begins a raid at a village centre (no-op if one is already active).
func (h *hub) startRaid(players map[int32]*tracked, center blockPos) {
	h.startRaidLevel(players, center, 1)
}

// startRaidLevel starts a raid at a Raid Omen level: above one it brings a
// bonus wave after the last, and a stronger Hero of the Village.
func (h *hub) startRaidLevel(players map[int32]*tracked, center blockPos, omenLevel int) {
	if h.raids[center] != nil {
		return
	}
	omenLevel = max(1, min(raidOmenMax, omenLevel))
	for _, t := range players { // Raid.absorbRaidOmen: everyone the raid wakes for is a trigger
		if t.dim == dimOverworld && t.hasEffect(effRaidOmen) > 0 {
			h.incCustom(t, "raid_trigger", 1)
		}
	}
	r := &raid{center: center, numGroups: raidWaveCount(h.rules.Difficulty), omenLevel: omenLevel,
		alive: map[int32]bool{}, shown: map[int32]bool{}}
	r.uuid = raidUUID(center)
	h.raids[center] = r
	r.cooldown = raidCooldownSecs // Raid(): the first wave waits too
	h.broadcastChat(players, "A raid has begun!")
	log.Printf("raid at (%d,%d): %d waves", center.x, center.z, r.numGroups)
}

// spawnWave spawns the next wave's raiders around the village.
func (h *hub) spawnWave(players map[int32]*tracked, r *raid) {
	r.wave++
	r.waveSpawned = 0
	if r.wave > 7 {
		return
	}
	// The omen-level bonus wave comes after the last regular one and takes
	// the final wave's counts (getDefaultNumSpawns with isBonusWave).
	bonus := r.omenLevel > 1 && r.wave > r.numGroups
	if bonus {
		r.bonusDone = true
	}
	leaderSet := false
	for _, etype := range raiderOrder {
		counts := raiderWaves[etype]
		ravagerN := 0 // successful ravagers this wave (first one gets the evoker rider)
		n := counts[r.wave]
		if bonus {
			n = counts[r.numGroups]
		}
		n += h.raidBonusSpawns(etype, r.wave, bonus)
		for i := 0; i < n; i++ {
			ang := h.rng.Float64() * 2 * math.Pi
			d := 8 + h.rng.Float64()*raidSpawnRadius
			x := r.center.x + int(math.Cos(ang)*d)
			z := r.center.z + int(math.Sin(ang)*d)
			if !h.world.Spawnable(x, z) {
				continue
			}
			m := h.spawnHostileYIn(players, etype, dimOverworld,
				float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
			if m == nil {
				continue
			}
			m.raidCenter, m.raidWave = r.center, r.wave
			if !leaderSet && canBeLeader(etype) {
				// Raid.spawnGroup: the wave's first raider that can lead
				// carries the ominous banner (Raid.setLeader).
				h.makeCaptain(players, m)
				leaderSet = true
			}
			h.applyRaidBuffs(m, r) // enchanted gear on the later waves
			r.alive[m.eid] = true
			r.waveSpawned++
			// Vanilla Raid.spawnGroup mounts a rider on each ravager on the
			// higher waves: a pillager on wave 5, an evoker (first ravager) or
			// vindicator (the rest) on wave 7+.
			if etype == entityRavager {
				if rt := raidRiderType(r.wave, ravagerN); rt != 0 {
					if rd := h.spawnHostileYIn(players, rt, m.dim, m.x, m.y, m.z); rd != nil {
						rd.raidCenter, rd.raidWave = r.center, r.wave
						rd.mount, m.mobRider = m.eid, rd.eid
						r.alive[rd.eid] = true
						r.waveSpawned++
						h.toTracking(players, m.eid, m.dim, m.x, m.z, passengersBody(m.eid, rd.eid))
					}
				}
				ravagerN++
			}
		}
	}
	// Raid.joinRaid: the bar measures the wave by its raiders' health.
	r.totalHealth = 0
	for eid := range r.alive {
		if m := h.mobs[eid]; m != nil {
			r.totalHealth += float64(m.health)
		}
	}
}

// raidRiderType picks the raider that mounts a ravager on a given wave, matching
// vanilla Raid.spawnGroup (getNumGroups NORMAL=5, HARD=7): a pillager on wave 5,
// and on wave 7+ an evoker on the first ravager, a vindicator on the rest.
// 0 = no rider.
func raidRiderType(wave, ravagerIdx int) int {
	switch {
	case wave == 5:
		return entityPillager
	case wave >= 7:
		if ravagerIdx == 0 {
			return entityEvoker
		}
		return entityVindicator
	}
	return 0
}

// updateRaids (1 Hz) refreshes the bar, spawns the next wave when the current is
// cleared, wins when all waves are down, and times out if nobody's around.
func (h *hub) updateRaids(players map[int32]*tracked) {
	for center, r := range h.raids {
		aliveN := 0
		for eid := range r.alive {
			if h.mobs[eid] == nil {
				delete(r.alive, eid)
			} else {
				aliveN++
			}
		}
		if r.lostLeft > 0 { // Raid.tick after a LOSS: the defeat bar, then gone
			if r.lostLeft--; r.lostLeft == 0 {
				h.endRaid(players, r)
				delete(h.raids, center)
			}
			continue
		}
		if r.wonLeft > 0 { // Raid.tick after a VICTORY: the celebration, then gone
			h.showRaidBar(players, r, 0)
			if r.wonLeft--; r.wonLeft == 0 {
				h.endRaid(players, r)
				delete(h.raids, center)
			}
			continue
		}
		// Raid.tick: a raid whose centre is no longer a village first moves to
		// the nearest village section around it (the villagers' beds broken,
		// the village still standing beside them)…
		if !h.isVillage(center) {
			if nc, ok := h.nearbyVillageSection(center); ok {
				delete(h.raids, center)
				r.center, center = nc, nc
				h.raids[nc] = r
			}
		}
		// …and is lost once a wave has come (stopped quietly before), and
		// every raid ends at 48000 ticks.
		if !h.isVillage(center) {
			if r.wave > 0 {
				r.lostLeft = raidDefeatSecs
				h.retitleRaid(players, r, "Raid - Defeat", aliveN)
			} else {
				h.endRaid(players, r)
				delete(h.raids, center)
			}
			continue
		}
		if r.secsActive++; r.secsActive >= raidTimeoutSecs {
			h.endRaid(players, r)
			delete(h.raids, center)
			continue
		}
		title := "Raid"
		if aliveN > 0 && aliveN <= 2 {
			title = fmt.Sprintf("Raid - Raiders Remaining: %d", aliveN)
		}
		h.retitleRaid(players, r, title, aliveN)
		h.showRaidBar(players, r, h.raidProgress(r, aliveN))
		if !h.anyPlayerNear(players, center, raidBarRange) {
			if r.idleTicks++; r.idleTicks > raidNoPlayerTimeout {
				h.endRaid(players, r)
				delete(h.raids, center)
			}
			continue
		}
		r.idleTicks = 0
		h.raidBell(players, r)
		if aliveN == 0 && r.pending > 0 {
			continue // a restored raid: its raiders are still in unloaded chunks
		}
		if aliveN == 0 { // wave cleared
			if r.wave >= r.numGroups && (r.omenLevel <= 1 || r.bonusDone) {
				// vanilla: everyone who helped (in the raid area) earns Hero of
				// the Village — 48000 ticks (40 min) at the omen level's
				// amplifier, which discounts villager trades via
				// updateSpecialPrices and moves the villagers to give gifts.
				for _, t := range players {
					if t.dim == 0 && dist3(t.x, t.y, t.z, float64(center.x), float64(center.y), float64(center.z)) <= raidBarRange {
						h.applyEffect(players, t, effHeroOfVillage, r.omenLevel-1, 2400)
						h.incCustom(t, "raid_win", 1)
					}
				}
				// Raid - Victory, emptied, for the celebration.
				r.wonLeft = raidDefeatSecs
				h.retitleRaid(players, r, "Raid - Victory", 0)
				h.showRaidBar(players, r, 0)
			} else if r.cooldown == 0 && r.wave > 0 {
				r.cooldown = raidCooldownSecs // the pause before the next wave
			}
		}
		if aliveN == 0 && r.cooldown > 0 && r.wonLeft == 0 {
			// raidCooldownTicks: the bar fills while it runs down, then the
			// wave comes.
			if r.cooldown--; r.cooldown == 0 {
				h.spawnWave(players, r)
			}
		}
	}
}

// showRaidBar sends/updates/removes the purple raid bar for nearby players.
func (h *hub) showRaidBar(players map[int32]*tracked, r *raid, frac float32) {
	title := r.title
	if title == "" {
		title = "Raid"
	}
	for _, t := range players {
		near := t.dim == 0 && dist3(t.x, t.y, t.z, float64(r.center.x), float64(r.center.y), float64(r.center.z)) <= raidBarRange
		if near {
			if !r.shown[t.p.eid] {
				r.shown[t.p.eid] = true
				t.p.trySendEv(bossBarAdd(r.uuid, title, frac, raidBarLook))
			} else {
				t.p.trySendEv(bossBarHealth(r.uuid, frac))
			}
		} else if r.shown[t.p.eid] {
			delete(r.shown, t.p.eid)
			t.p.trySendEv(bossBarRemove(r.uuid))
		}
	}
}

// retitleRaid renames the bar in place (ServerBossEvent.setName, then the
// progress). A no-op when the name is unchanged.
func (h *hub) retitleRaid(players map[int32]*tracked, r *raid, title string, aliveN int) {
	if r.title == title || (r.title == "" && title == "Raid") {
		r.title = title
		return
	}
	r.title = title
	frac := float32(0)
	if r.waveSpawned > 0 {
		frac = float32(aliveN) / float32(r.waveSpawned)
	}
	for eid := range r.shown {
		if t := players[eid]; t != nil {
			t.p.trySendEv(bossBarTitle(r.uuid, title))
			t.p.trySendEv(bossBarHealth(r.uuid, frac))
		}
	}
}

// endRaid clears the raid bar from everyone who saw it.
func (h *hub) endRaid(players map[int32]*tracked, r *raid) {
	for _, t := range players {
		if r.shown[t.p.eid] {
			t.p.trySendEv(bossBarRemove(r.uuid))
		}
	}
}

// broadcastChat sends a system message to every connected player.
func (h *hub) broadcastChat(players map[int32]*tracked, text string) {
	for _, t := range players {
		t.p.trySendEv(chatEv(text))
	}
}

// anyPlayerNear reports whether a live overworld player is within r of a point.
func (h *hub) anyPlayerNear(players map[int32]*tracked, pos blockPos, r float64) bool {
	for _, t := range players {
		if t.dim == 0 && !t.dead &&
			dist3(t.x, t.y, t.z, float64(pos.x), float64(pos.y), float64(pos.z)) <= r {
			return true
		}
	}
	return false
}

// checkRaidTrigger converts a player's Bad Omen into a Raid Omen when they
// reach a village — BadOmenMobEffect.applyEffectTick. The raid does NOT start
// here: the Raid Omen is a 30-second fuse, and the horn goes off when it
// expires (raidOmenExpired), which is the warning window vanilla gives you to
// get ready or get out.
func (h *hub) checkRaidTrigger(players map[int32]*tracked, t *tracked) {
	lvl := t.hasEffect(effBadOmen)
	if lvl == 0 || h.rules.Difficulty == diffPeaceful || !h.rules.Raids || t.gamemode == gmSpectator {
		return
	}
	// BadOmenMobEffect.applyEffectTick: the player's own block must be a
	// village (ServerLevel.isVillage: a village point of interest within a
	// section — a built village counts, a ruin does not), and the Raid Omen
	// remembers where they stood. Raids here are overworld-only.
	center := blockPos{floorInt(t.x), floorInt(t.y), floorInt(t.z)}
	if t.dim != dimOverworld || !h.isVillage(center) {
		return
	}
	h.removeEffect(t, effBadOmen)
	t.raidOmenPos, t.raidOmenSet = center, true
	h.applyEffect(players, t, effRaidOmen, lvl-1, raidOmenSecs)
}

// raidOmenExpired starts the raid the Raid Omen was counting down to
// (RaidOmenMobEffect.applyEffectTick, which fires on the final tick).
func (h *hub) raidOmenExpired(players map[int32]*tracked, t *tracked) {
	if !t.raidOmenSet {
		return
	}
	t.raidOmenSet = false
	h.startRaidLevel(players, t.raidOmenPos, max(1, t.hasEffect(effRaidOmen))) // hasEffect = amplifier+1 = the omen level
}

// raidNear reports whether an active raid's village is within vanilla's
// raid reach (Raids.getNearbyRaid: 96 blocks) of a point.
func (h *hub) raidNear(dim int, x, z float64) bool {
	if dim != dimOverworld {
		return false
	}
	for c := range h.raids {
		dx, dz := float64(c.x)-x, float64(c.z)-z
		if dx*dx+dz*dz <= 96*96 {
			return true
		}
	}
	return false
}

// raidEnchantOdds is Raid.getEnchantOdds: from Raid Omen level 2 up, a raider
// has a chance of coming out of the spawn with enchanted gear, and the chance
// climbs with the omen — a bad omen bought cheaply brings a softer raid than
// one carried in at full strength.
func raidEnchantOdds(omenLevel int) float64 {
	switch {
	case omenLevel == 2:
		return 0.10
	case omenLevel == 3:
		return 0.25
	case omenLevel == 4:
		return 0.50
	case omenLevel >= 5:
		return 0.75
	}
	return 0
}

// applyRaidBuffs is Pillager.applyRaidBuffs / Vindicator.applyRaidBuffs. The
// wave thresholds are vanilla's NORMAL and EASY group counts (5 and 3)
// whatever difficulty the raid is running at, so a hard raid's seventh wave
// is the dangerous one everywhere.
func (h *hub) applyRaidBuffs(m *mob, r *raid) {
	if h.rng.Float64() > raidEnchantOdds(r.omenLevel) {
		return
	}
	set := func(id int8, lvl int8) {
		m.heldEnch = enchApplyList([]enchInstance{{id: id, lvl: lvl}})
	}
	switch m.etype {
	case entityPillager:
		// A pillager's crossbow gets nothing before the fourth wave.
		switch {
		case r.wave > raidWaveCount(diffNormal):
			set(enchQuickCharge, 2)
		case r.wave > raidWaveCount(diffEasy):
			set(enchQuickCharge, 1)
		}
	case entityVindicator:
		// A vindicator's axe is always sharpened, harder after wave five.
		lvl := int8(1)
		if r.wave > raidWaveCount(diffNormal) {
			lvl = 2
		}
		set(enchSharpness, lvl)
	}
}

// raidProgress is the bar: during the cooldown, how far it has run
// ((300 - raidCooldownTicks) / 300); with raiders out, their health over the
// wave's (Raid.updateBossbar).
func (h *hub) raidProgress(r *raid, aliveN int) float32 {
	if aliveN == 0 && r.cooldown > 0 {
		return float32(raidCooldownSecs-r.cooldown) / raidCooldownSecs
	}
	if r.totalHealth <= 0 {
		return 0
	}
	hp := 0.0
	for eid := range r.alive {
		if m := h.mobs[eid]; m != nil {
			hp += float64(m.health)
		}
	}
	return float32(math.Min(1, math.Max(0, hp/r.totalHealth)))
}
