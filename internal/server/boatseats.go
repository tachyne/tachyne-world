package server

// A boat's two seats (AbstractBoat.getMaxPassengers; a chest boat keeps one,
// its cargo taking the other). A player climbs into the free seat. An empty,
// unsteered boat takes aboard a mob it touches, which is how villagers
// are moved about in vanilla: AbstractBoat.tick boards any living thing in
// its box inflated 0.2 that is narrower than the boat and not in
// #cannot_be_pushed_onto_boats, unless a player is steering. Whoever boarded
// first sits in front, and only a player in front can steer.

// boatTooWide are the mobs at least as wide as a boat (EntityType sizes ≥
// 1.375), which hasEnoughSpaceFor turns away.
var boatTooWide = entitySet("camel", "camel_husk", "donkey", "horse", "mule", "skeleton_horse", "zombie_horse",
	"hoglin", "zoglin", "iron_golem", "polar_bear", "ravager", "sniffer", "spider", "ghast", "happy_ghast",
	"giant", "ender_dragon", "elder_guardian")

// cannotBoard is #cannot_be_pushed_onto_boats (players are never mobs here).
var cannotBoard = entitySet("elder_guardian", "cod", "pufferfish", "salmon", "tropical_fish", "dolphin",
	"squid", "glow_squid", "tadpole", "creaking", "nautilus", "zombie_nautilus", "sulfur_cube")

func entitySet(names ...string) map[int]bool {
	out := map[int]bool{}
	for _, n := range names {
		if id, ok := entityByName[n]; ok {
			out[id] = true
		}
	}
	return out
}

// seats is how many passengers the vehicle takes.
func (v *vehicle) seats() int {
	if v.isBoat() && v.chest == nil {
		return 2
	}
	return 1
}

// aboard counts the passengers.
func (v *vehicle) aboard() int {
	n := 0
	if v.rider != 0 {
		n++
	}
	if v.rider2 != 0 {
		n++
	}
	if v.mobRider != 0 {
		n++
	}
	return n
}

// passengers is the passenger list in seat order: front first.
func (v *vehicle) passengers() []int32 {
	var out []int32
	if v.rider2 != 0 {
		return []int32{v.rider, v.rider2}
	}
	if v.mobFirst && v.mobRider != 0 {
		out = append(out, v.mobRider)
	}
	if v.rider != 0 {
		out = append(out, v.rider)
	}
	if !v.mobFirst && v.mobRider != 0 {
		out = append(out, v.mobRider)
	}
	return out
}

// fitsInBoat is hasEnoughSpaceFor plus the tag: narrower than the boat.
func fitsInBoat(m *mob) bool {
	if cannotBoard[m.etype] {
		return false
	}
	if m.etype == entitySlime || m.etype == entityMagmaCube {
		return m.size < 3 // 0.52 per size: size 3 is 1.56 wide
	}
	return !boatTooWide[m.etype] || m.baby
}

// boatPickup is the boarding half of AbstractBoat.tick.
func (h *hub) boatPickup(players map[int32]*tracked, v *vehicle) {
	if !v.isBoat() || v.mobRider != 0 || v.aboard() >= v.seats() {
		return
	}
	if v.rider != 0 && !v.mobFirst {
		return // a player steering: no new passengers
	}
	for _, m := range h.mobs {
		if m.dim != v.dim || m.dying > 0 || m.mount != 0 || m.cart != 0 || m.rider != 0 ||
			m.mobRider != 0 || len(m.riders) > 0 || !fitsInBoat(m) {
			continue
		}
		// The boat's box (1.375 wide, 0.5625 tall) inflated 0.2 sideways.
		if absF(m.x-v.x) > 0.8875 || absF(m.z-v.z) > 0.8875 || m.y > v.y+0.5525 || m.y+0.5 < v.y {
			continue
		}
		m.cart, v.mobRider = v.eid, m.eid
		v.mobFirst = v.rider == 0
		m.vx, m.vz, m.hasTarget = 0, 0, false
		h.toTracking(players, v.eid, v.dim, v.x, v.z, passengersBody(v.eid, v.passengers()...))
		h.startedRiding(players, v)
		return
	}
}

// startedRiding is StartRidingTrigger for every player aboard: fired when
// anything boards, and matched against the player's own vehicle.
func (h *hub) startedRiding(players map[int32]*tracked, v *vehicle) {
	var passenger string
	if m := h.mobs[v.mobRider]; m != nil {
		passenger = advEntityName[m.etype]
	}
	for _, id := range []int32{v.rider, v.rider2} {
		if t := players[id]; t != nil {
			h.advance(players, t, "started_riding", advMatch{vehicle: entityNameByID[v.etype], passenger: passenger})
		}
	}
}
