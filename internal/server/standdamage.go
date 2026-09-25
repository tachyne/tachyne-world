package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ArmorStand.hurtServer: a stand is not a mob and takes no ordinary damage.
// A mob's blow does nothing with mob_griefing off; a bypassing kill removes
// it; an explosion breaks it, dropping its gear but not itself; fire lights
// it, and burning then eats four of its twenty health a second until it
// falls apart; a player's blow (or a mace, a spear, a player's blast)
// wobbles it and a second within five ticks breaks it; an arrow, a trident,
// a fireball, a wither skull or a wind charge breaks it outright.

const standHealth = 20.0

// standHurt applies one damage event to a stand.
func (h *hub) standHurt(players map[int32]*tracked, st *armorStand, dt dmgType, by *tracked, byMob bool) {
	if h.armorStands[st.eid] == nil {
		return
	}
	switch {
	case byMob && !h.rules.MobGriefing:
		return
	case dt.has(tagBypassesInvulnerability):
		h.breakStand(players, st, false, false)
		return
	case dt.has(tagIsExplosion):
		h.breakStand(players, st, false, true) // brokenByAnything
		return
	case dt.has(tagIgnitesArmorStands):
		if st.fire > 0 {
			h.standCauseDamage(players, st, 0.15)
		} else {
			h.igniteStand(players, st, 100) // igniteForSeconds(5)
		}
		return
	case dt.has(tagBurnsArmorStands):
		if standHealth-st.hurt > 0.5 {
			h.standCauseDamage(players, st, 4)
		}
		return
	}
	breaks, kills := dt.has(tagCanBreakArmorStand), dt.has(tagAlwaysKillsArmorStands)
	if !breaks && !kills {
		return
	}
	if by != nil && by.gamemode == gmAdventure {
		return // !mayBuild
	}
	if by != nil && by.gamemode == gmCreative {
		h.playSoundDim(players, st.dim, "minecraft:entity.armor_stand.break", sndBlock, st.x, st.y, st.z, 1, 1)
		h.breakStand(players, st, false, false)
		return
	}
	now := h.tick.Load()
	if !kills && (st.lastHit == 0 || now-st.lastHit > standBreakWindow) {
		st.lastHit = now
		h.toTracking(players, st.eid, st.dim, st.x, st.z, attachproto.EntityStatus{EID: st.eid, Status: entityStatusStandWobble})
		return
	}
	h.breakStand(players, st, true, true) // brokenByPlayer
}

// standCauseDamage is causeDamage: at half a point of health left it breaks.
func (h *hub) standCauseDamage(players map[int32]*tracked, st *armorStand, dmg float64) {
	if st.hurt += dmg; standHealth-st.hurt <= 0.5 {
		h.breakStand(players, st, false, true)
	}
}

// igniteStand sets it burning (the flames show on the client).
func (h *hub) igniteStand(players map[int32]*tracked, st *armorStand, ticks int) {
	was := st.fire > 0
	st.fire = max(st.fire, ticks)
	if !was {
		h.toTracking(players, st.eid, st.dim, st.x, st.z, metaEv(fireMetadata(st.eid, true)))
	}
}

// tickStands is each stand's Entity.baseTick for fire: lava lights it for
// fifteen seconds (lava's own damage is nothing to a stand), fire hurts it
// (lighting it first), water puts it out, and burning out of the lava
// costs it a hurt every twenty ticks.
func (h *hub) tickStands(players map[int32]*tracked) {
	for _, st := range h.armorStands {
		w := h.worldFor(st.dim)
		if w == nil {
			continue
		}
		x, y, z := floorInt(st.x), floorInt(st.y), floorInt(st.z)
		wet, inLava := false, false
		for dy := 0; dy <= 1; dy++ {
			switch c := w.At(x, y+dy, z); {
			case worldgen.IsLava(c):
				inLava = true
				h.igniteStand(players, st, 300)
			case isFire(c) || c == soulFire:
				h.standHurt(players, st, dtInFire, nil, false)
			case worldgen.HoldsWater(c):
				wet = true
			}
			if h.armorStands[st.eid] == nil {
				break
			}
		}
		if h.armorStands[st.eid] == nil {
			continue
		}
		if st.fire > 0 && wet {
			st.fire = 0
			h.toTracking(players, st.eid, st.dim, st.x, st.z, metaEv(fireMetadata(st.eid, false)))
			continue
		}
		if st.fire > 0 {
			if st.fire%20 == 0 && !inLava { // baseTick: no burn while in the lava itself
				h.standHurt(players, st, dtOnFire, nil, false)
			}
			st.fire--
			if st.fire == 0 && h.armorStands[st.eid] != nil {
				h.toTracking(players, st.eid, st.dim, st.x, st.z, metaEv(fireMetadata(st.eid, false)))
			}
		}
	}
}

// explosionHitsStands breaks the stands a blast reaches (ArmorStand does
// not ignore an explosion that affects block-like entities; a wind burst
// does not).
func (h *hub) explosionHitsStands(players map[int32]*tracked, dim int, cx, cy, cz, power float64, dt dmgType) {
	if h.blastSrc.direct == entityWindCharge || h.blastSrc.direct == entityBreezeWindCharge {
		return
	}
	for _, st := range h.armorStands {
		if st.dim == dim && dist3(st.x, st.y+0.99, st.z, cx, cy, cz) <= power*2 {
			h.standHurt(players, st, dt, nil, h.blastSrc.causerMob)
		}
	}
}

// arrowHitsStand is a projectile meeting a stand (0.5 across, 1.975 tall).
func (h *hub) arrowHitsStand(players map[int32]*tracked, a *arrowEntity, px, py, pz float64) bool {
	if a.pearl || a.xpBottle || a.breath || a.splash {
		return false
	}
	for _, st := range h.armorStands {
		if st.dim != a.dim || absF(px-st.x) > 0.55 || absF(pz-st.z) > 0.55 || py < st.y-0.3 || py > st.y+2.275 {
			continue
		}
		h.standHurt(players, st, projectileDamageOf(a), players[a.shooter], a.mobShot)
		return true
	}
	return false
}
