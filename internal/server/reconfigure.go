package server

// reconfigure.go sends a player back through the configuration phase and
// places them again, as vanilla does with
// ServerGamePacketListenerImpl.switchToConfig: the player leaves the level
// (removePlayerFromWorld → PlayerList.remove: saved, unmounted, gone from
// everyone's tab list), the client is sent start_configuration and runs the
// configuration phase again with the world's current data, and when it
// finishes, PlayerList.placeNewPlayer loads the player back in where they
// were — a fresh join in every respect but the connection. Only a session
// whose gateway handles it (attach FeatureReconfigure) can be sent there.
//
// It also carries a data pack's tags to the clients in play
// (PlayerList.reloadResources' ClientboundUpdateTagsPacket).

import (
	"fmt"
	"log"
	"sort"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// rejoinState is where a reconfigured player stood when they left the
// level: the hub writes it, the session reads it to place them again.
type rejoinState struct {
	x, y, z    float64
	yaw, pitch float32
	dim        int
	gamemode   int
}

// packTagSets is a pack load's tags as the gateways take them: every tag the
// packs changed from vanilla's, with its members as it now stands (an
// empty list for a vanilla tag that no longer loads), one set per registry
// named by its data-pack folder, registries and tags in a stable order.
// The set is always whole: a tag it does not name is the built-in one.
func packTagSets(pc *packContent) []attachproto.TagSet {
	if pc == nil {
		return nil
	}
	changed := pc.tags.changedTags()
	regs := make([]string, 0, len(changed))
	for reg := range changed {
		regs = append(regs, reg)
	}
	sort.Strings(regs)
	out := make([]attachproto.TagSet, 0, len(regs))
	for _, reg := range regs {
		ids := make([]string, 0, len(changed[reg]))
		for id := range changed[reg] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		set := attachproto.TagSet{Registry: reg, Tags: make([]attachproto.Tag, 0, len(ids))}
		for _, id := range ids {
			members := changed[reg][id]
			if members == nil {
				members = []string{}
			}
			set.Tags = append(set.Tags, attachproto.Tag{Name: id, Entries: members})
		}
		out = append(out, set)
	}
	return out
}

// currentConfigData is what a configuration phase is given now: the
// dimension table and the installed pack load's tags.
func currentConfigData() attachproto.ConfigData {
	return attachproto.ConfigData{Dimensions: dimensionTable(), Tags: packTagSets(currentPack())}
}

// reconfigure is switchToConfig for one player: false when their gateway
// cannot reconfigure them, or they are not in the level.
func (h *hub) reconfigure(players map[int32]*tracked, t *tracked) bool {
	p := t.p
	if !p.reconfigure || p.exec != nil || players[p.eid] != t {
		return false
	}
	if !p.configuring.CompareAndSwap(false, true) {
		return false
	}
	p.rejoin.Store(&rejoinState{x: t.x, y: t.y, z: t.z, yaw: t.yaw, pitch: t.pitch, dim: t.dim, gamemode: t.gamemode})
	h.stopDig(players, p.eid)
	h.onLeave(players, p) // removePlayerFromWorld: saved, and gone for everyone else
	// Nothing else goes to this player until they are placed again: they
	// are out of the player map, and the gateway holds play packets.
	p.trySendEv(attachproto.StartConfiguration{ConfigData: currentConfigData()})
	log.Printf("reconfigure: %s sent back to configuration", p.name)
	return true
}

// resetView forgets every chunk the client held: it discarded its level, so
// no entity is shown to it until the chunk under it arrives again.
func (p *player) resetView() {
	p.view.Lock()
	p.view.sent = map[[3]int32]bool{}
	p.view.Unlock()
}

// Configured makes a remote player an attach.Reconfigurable: the client
// finished configuration, and the player is placed again
// (PlayerList.placeNewPlayer) where they left — the login (MsgRejoin from
// the session's join state), what JoinRemote sends a newcomer, the position
// and rotation (ServerGamePacketListenerImpl.teleport), then the hub's own
// join, which sends everything else and puts them back in the player map.
func (r *remotePlayer) Configured(c attachproto.Configured, welcome func() attachproto.Welcome) {
	p := r.p
	st := p.rejoin.Load()
	if st == nil || !p.configuring.Load() {
		return // not sent to configuration: a stray answer
	}
	if c.View > 0 {
		p.setViewDist(c.View)
	}
	p.resetView()
	p.dim = st.dim
	p.x, p.y, p.z, p.yaw, p.pitch = st.x, st.y, st.z, st.yaw, st.pitch
	p.setHubPos(p.x, p.z) // the chunk stream opens where they stand
	r.x, r.y, r.z, r.gm = st.x, st.y, st.z, int32(st.gamemode)
	r.emitEvNow(attachproto.Rejoin{Welcome: welcome()})
	r.emitEvNow(attachproto.CommandTree{Data: r.s.commandTreeFor(r.s.opLevel(p.name))}) // the tree for its level
	r.emitEvNow(abilitiesFor(st.gamemode))
	r.emitEvNow(opLevelEvent(p.eid, r.s.opLevel(p.name)))
	r.emitEvNow(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	p.configuring.Store(false)
	r.s.hub.post(evJoin{p: p, x: st.x, y: st.y, z: st.z, yaw: st.yaw, pitch: st.pitch, dim: st.dim, gamemode: st.gamemode})
}

// onPackRegistriesChanged is where a reload's tags reach the clients
// (PlayerList.reloadResources broadcasts ClientboundUpdateTagsPacket): every
// session whose gateway can take them is sent the load's whole tag set,
// which the gateway merges over its built-in tags. A reload changes no
// registry entries here (packs add none the engine loads), so — as in
// vanilla, where /reload and /datapack enable only resend tags and recipes
// — nobody is reconfigured; a later load that adds registry entries is what
// would call reconfigure for each player. Players who join or reconfigure
// later are given the same set in their configuration data.
func (h *hub) onPackRegistriesChanged(players map[int32]*tracked, pc *packContent) {
	upd := attachproto.UpdateTags{Tags: packTagSets(pc)}
	sent := 0
	for _, t := range players {
		if t.p.reconfigure && !t.p.bedrock && t.p.exec == nil {
			t.p.trySendEv(upd)
			sent++
		}
	}
	changed := 0
	for _, set := range upd.Tags {
		changed += len(set.Tags)
	}
	log.Printf("datapacks: %d changed tags sent to %d of %d players", changed, sent, len(players))
}

// ---- /debugconfig ----------------------------------------------------------------

// evDebugConfig is DebugConfigCommand's config: send a player back to
// configuration.
type evDebugConfig struct {
	by     *player
	target string
}

func (evDebugConfig) isHubEvent() {}

// cmdDebugConfig is /debugconfig config <target> (LEVEL_ADMINS, vanilla's
// development command for exactly this round trip).
func (s *Server) cmdDebugConfig(p *player, args []string) {
	if len(args) != 2 || args[0] != "config" {
		p.tell("Usage: /debugconfig config <target>")
		return
	}
	s.hub.post(evDebugConfig{by: p, target: args[1]})
}

func (h *hub) onDebugConfig(players map[int32]*tracked, e evDebugConfig) {
	targets := h.commandTargets(players, e.by.eid, e.target)
	switch {
	case len(targets) == 0:
		e.by.trySendEv(chatEv("No player was found"))
		return
	case len(targets) > 1: // EntityArgument.player()
		e.by.trySendEv(chatEv("Only one player is allowed, but the provided selector allows more than one"))
		return
	}
	t := targets[0]
	name, id := t.p.name, uuidString(t.p.uuid)
	if !h.reconfigure(players, t) {
		e.by.trySendEv(chatEv(fmt.Sprintf("Can't switch player %s(%s) to config mode", name, id)))
		return
	}
	h.cmdSuccess(players, e.by, fmt.Sprintf("Switched player %s(%s) to config mode", name, id), false)
	setCmdResult(e.by, 1)
}
