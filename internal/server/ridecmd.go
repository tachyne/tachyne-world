package server

import "fmt"

// /ride <target> mount <vehicle> | /ride <target> dismount (RideCommand).
// A player boards a mob's rider seat; a mob boards another mob's mob seat
// (the jockey seat, the camel's two), and — as vanilla's Mob controls the
// vehicle it rides — its own AI then leads. The command forces the ride: no
// saddle, taming or reach check, only a free seat. Selectors resolve players
// and mobs, so boats and minecarts are not vehicles /ride can name.

type evRideCmd struct {
	by      int32
	target  string
	vehicle string // "" = dismount
}

func (evRideCmd) isHubEvent() {}

func (s *Server) cmdRide(p *player, args []string) {
	if !s.isOp(p.name) { // RideCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	switch {
	case len(args) == 3 && args[1] == "mount":
		s.hub.post(evRideCmd{by: p.eid, target: args[0], vehicle: args[2]})
	case len(args) == 2 && args[1] == "dismount":
		s.hub.post(evRideCmd{by: p.eid, target: args[0]})
	default:
		p.tell("Usage: /ride <target> mount <vehicle> | /ride <target> dismount")
	}
}

// rideVehicleName names what an entity is riding, "" when it rides nothing.
func (h *hub) rideVehicleName(en cmdEntity) string {
	var eid int32
	if en.t != nil {
		eid = en.t.ridingEID
	} else {
		eid = en.m.mount
	}
	if eid == 0 {
		return ""
	}
	if m := h.mobs[eid]; m != nil {
		return cmdEntity{m: m}.name()
	}
	if v := h.vehicles[eid]; v != nil {
		return mobDisplayName(v.etype)
	}
	return "an entity"
}

// singleEntity resolves an argument that must name exactly one entity,
// telling the caller why not.
func (h *hub) singleEntity(players map[int32]*tracked, by int32, arg string, tell func(string)) (cmdEntity, bool) {
	ens := h.commandEntities(players, by, arg)
	switch {
	case len(ens) == 0:
		tell("No entity was found")
		return cmdEntity{}, false
	case len(ens) > 1:
		tell("Only one entity is allowed, but the provided selector allows more than one")
		return cmdEntity{}, false
	}
	return ens[0], true
}

// applyRideCommand runs /ride on the hub.
func (h *hub) applyRideCommand(players map[int32]*tracked, e evRideCmd) {
	tell := cmdTeller(players, e.by)
	target, ok := h.singleEntity(players, e.by, e.target, tell)
	if !ok {
		return
	}
	if e.vehicle == "" {
		was := h.rideVehicleName(target)
		if was == "" {
			tell(target.name() + " is not riding any vehicle")
			return
		}
		if t := target.t; t != nil {
			if !h.leaveGhast(players, t) && !h.dismountMob(players, t) {
				h.dismount(players, t)
			}
		} else {
			h.unseatMob(players, target.m)
		}
		tell(fmt.Sprintf("%s stopped riding %s", target.name(), was))
		return
	}
	vehicle, ok := h.singleEntity(players, e.by, e.vehicle, tell)
	if !ok {
		return
	}
	if on := h.rideVehicleName(target); on != "" {
		tell(fmt.Sprintf("%s is already riding %s", target.name(), on))
		return
	}
	if vehicle.t != nil {
		tell("Players can't be ridden")
		return
	}
	v := vehicle.m
	if target.m != nil && h.carries(target.m, v) {
		tell("Can't mount entity on itself or any of its passengers")
		return
	}
	if target.dim() != vehicle.dim() {
		tell("Can't mount entity in different dimension")
		return
	}
	if !h.forceRide(players, target, v) {
		tell(fmt.Sprintf("%s couldn't start riding %s", target.name(), vehicle.name()))
		return
	}
	tell(fmt.Sprintf("%s started riding %s", target.name(), vehicle.name()))
}

// carries reports whether v is m or rides somewhere on m's stack of
// passengers (Entity.getSelfAndPassengers).
func (h *hub) carries(m, v *mob) bool {
	for cur := v; cur != nil; cur = h.mobs[cur.mount] {
		if cur == m {
			return true
		}
	}
	return false
}

// forceRide is startRiding(vehicle, force): the seat must be free, nothing
// else is asked. Reports whether the entity boarded.
func (h *hub) forceRide(players map[int32]*tracked, en cmdEntity, v *mob) bool {
	if v.dying != 0 {
		return false
	}
	if t := en.t; t != nil {
		if v.rider != 0 || v.mobRider != 0 || len(v.riders) > 0 {
			return false
		}
		v.rider = t.p.eid
		t.ridingEID = v.eid
		v.vx, v.vz, v.hasTarget = 0, 0, false
		t.x, t.y, t.z = v.x, v.y, v.z
		t.p.setHubPos(t.x, t.z)
		h.toTracking(players, v.eid, v.dim, v.x, v.z, passengersBody(v.eid, v.rider))
		h.advance(players, t, "started_riding", advMatch{})
		return true
	}
	if v.rider != 0 || !v.seatFree() {
		return false
	}
	h.mountMobOn(players, en.m, v, true)
	return true
}
