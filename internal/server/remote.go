package server

// remote.go bridges attach sessions (gateway players) into the hub. A remote
// player IS a hub player: the hub emits typed domain events to its out
// channel, and the pump below serializes them as attach frames.

import (
	"encoding/json"
	"fmt"

	"github.com/tachyne/tachyne-world/internal/attach"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// JoinRemote gives an attach session full hub presence (visible to and seeing
// every other player and mob, chat included). emit receives domain frames.
func (s *Server) JoinRemote(name string, uuid [16]byte, emit func(typ byte, payload []byte)) (attach.Remote, error) {
	p := newPlayer(s.hub.mintPlayerEID(), name, uuid)
	x, y, z := 0.5, s.world.SurfaceY(0, 0), 0.5
	if s.SpawnSet {
		x, y, z = s.SpawnX, s.SpawnY, s.SpawnZ
	}
	p.x, p.y, p.z = x, y, z
	r := &remotePlayer{s: s, p: p, emit: emit, x: x, y: y, z: z, gm: -1}
	go r.decodeLoop()
	mode := s.modes.get(name)
	// Join-time extras the TCP path sends in handlePlay: tab-completion tree
	// and the mode's abilities (creative flight).
	r.emitEvNow(attachproto.CommandTree{Data: r.s.commandTreeBytes()})
	r.emitEvNow(abilitiesFor(mode))
	s.hub.post(evJoin{p: p, x: x, y: y, z: z, gamemode: mode})
	return r, nil
}

type remotePlayer struct {
	s       *Server
	p       *player
	emit    func(byte, []byte)
	x, y, z float64
	gm      int32 // resume: explicit gamemode from the migration snapshot (< 0 = use modeStore)
}

func (r *remotePlayer) EID() int32               { return r.p.eid }
func (r *remotePlayer) Spawn() (x, y, z float64) { return r.x, r.y, r.z }
func (r *remotePlayer) Gamemode() int32 {
	if r.gm >= 0 {
		return r.gm
	}
	return int32(r.s.modes.get(r.p.name))
}

// ResumeRemote binds a reconnecting gateway session to a player that was migrated
// to this pod (Hello{Purpose:"resume", token}). It claims the pending snapshot,
// recreates the player with the SAME session-stable eid, and resumes it — no
// fresh spawn, no "joined" broadcast; health/food/effects/inventory/xp and
// gamemode come from the snapshot, not the on-disk store.
func (s *Server) ResumeRemote(name string, uuid [16]byte, token string, emit func(typ byte, payload []byte)) (attach.Remote, error) {
	ps, ok := s.hub.claimPending(token)
	if !ok {
		return nil, fmt.Errorf("resume: no pending handover for token %q", token)
	}
	p := newPlayer(ps.EID, name, uuid)
	p.x, p.y, p.z = ps.X, ps.Y, ps.Z
	r := &remotePlayer{s: s, p: p, emit: emit, x: ps.X, y: ps.Y, z: ps.Z, gm: ps.Gamemode}
	go r.decodeLoop()
	mode := int(ps.Gamemode)
	r.emitEvNow(attachproto.CommandTree{Data: r.s.commandTreeBytes()})
	r.emitEvNow(abilitiesFor(mode))
	psCopy := ps
	s.hub.post(evJoin{p: p, x: ps.X, y: ps.Y, z: ps.Z, yaw: ps.Yaw, pitch: ps.Pitch, dim: int(ps.Dim), gamemode: mode, resume: &psCopy})
	return r, nil
}
func (r *remotePlayer) Chat(text string) {
	// Raw text + sender: the hub formats "<name> …" after the plugin chat
	// event, so handlers can rewrite or cancel the message.
	r.s.hub.post(evChat{from: r.p, text: text})
}
func (r *remotePlayer) Command(cmd string) { r.s.handleCommand(r.p, cmd) }

// Action receives a typed serverbound action and posts the same hub events
// dispatchPlay raises for a TCP connection (which still parses its own wire
// until stage 6c deletes it).
func (r *remotePlayer) Action(v any) {
	p, h := r.p, r.s.hub
	switch e := v.(type) {
	case attachproto.UseItem:
		switch p.heldItem() {
		case itemBow:
			h.post(evBowStart{eid: p.eid})
		case itemShield:
			h.post(evBlockStart{eid: p.eid})
		case itemSnowball, itemEgg:
			h.post(evThrow{eid: p.eid, item: p.heldItem()})
		case itemEnderPearl:
			h.post(evThrowPearl{eid: p.eid})
		case itemEnderEye:
			h.post(evThrowEye{eid: p.eid})
		case itemEmptyMap:
			h.post(evUseMap{eid: p.eid})
		default:
			h.post(evEat{eid: p.eid, slot: p.held})
		}
	case attachproto.UseEntity:
		if e.Attack {
			h.post(evAttack{attacker: p.eid, target: e.Target})
		} else {
			h.post(evInteractMob{eid: p.eid, target: e.Target, sneak: p.sneaking})
		}
	case attachproto.VehicleMove:
		h.post(evVehicleMove{eid: p.eid, x: e.X, y: e.Y, z: e.Z, yaw: e.Yaw})
	case attachproto.SelTrade:
		h.post(evSelTrade{eid: p.eid, slot: e.Slot})
	case attachproto.Input:
		if e.Sneak {
			h.post(evDismount{eid: p.eid})
		}
	case attachproto.WindowClick:
		ev := evClick{eid: p.eid, windowID: e.ID, slot: int16(e.Slot), mode: e.Mode,
			cursor: invStack{item: e.Cursor.ID, count: int(e.Cursor.Count)}}
		for _, c := range e.Changed {
			ev.changed = append(ev.changed, slotChange{slot: int16(c.Slot),
				st: invStack{item: c.Item.ID, count: int(c.Item.Count)}})
		}
		h.post(ev)
	case attachproto.Craft:
		h.post(evCraftRequest{eid: p.eid, windowID: e.Window, recipeID: e.Recipe})
	case attachproto.WindowClose:
		h.post(evCloseWin{eid: p.eid})
	case attachproto.NameItem:
		if len(e.Name) <= anvilMaxName {
			h.post(evRename{eid: p.eid, name: e.Name})
		}
	case attachproto.Enchant:
		h.post(evEnchant{eid: p.eid, button: e.Button})
	case attachproto.SetBeacon:
		h.post(evSetBeacon{eid: p.eid, primary: e.Primary, secondary: e.Secondary})
	case attachproto.PlayerAction:
		switch e.Action { // 0 sneak, 1 unsneak, 2 leave bed, 3/4 sprint
		case 0:
			p.sneaking = true
		case 1:
			p.sneaking = false
		case 2:
			h.post(evStopSleep{eid: p.eid})
		case 3:
			p.sprinting = true
		case 4:
			p.sprinting = false
		}
	case attachproto.RespawnReq:
		h.post(evRespawn{eid: p.eid})
	case attachproto.StatsReq:
		h.post(evStatsReq{eid: p.eid})
	case attachproto.RecipeSettingChange:
		h.post(evRecipeSettings{eid: p.eid, book: e.Book, open: e.Open, filter: e.Filter})
	case attachproto.RecipeSeen:
		h.post(evRecipeSeen{eid: p.eid, id: e.ID})
	case attachproto.SignUpdate:
		h.post(evSignUpdate{eid: p.eid, x: int(e.X), y: int(e.Y), z: int(e.Z), front: e.Front, lines: e.Lines})
	case attachproto.CreativeSlot:
		if r.s.modes.get(p.name) != gmCreative {
			return
		}
		r.s.applyCreativeSlot(p, int16(e.Slot), e.Item.ID, int(e.Item.Count), e.PaintingVariant)
	}
}

func (r *remotePlayer) Leave() { r.s.hub.post(evLeave{p: r.p}); close(r.p.quit) }
func (r *remotePlayer) Move(x, y, z float64, yaw, pitch float32, onGround bool) {
	r.p.x, r.p.y, r.p.z = x, y, z
	// The session player's look direction feeds placement orientation
	// (vanilla UseOnContext.getRotation() = the player's live yaw): sign
	// rotation, stairs/bed/furnace facing. Only x/y/z were carried over in
	// the domain-events refactor, freezing p.yaw at 0 — every yaw-derived
	// placement silently faced the yaw-0 direction until this line.
	r.p.yaw, r.p.pitch = yaw, pitch
	r.s.hub.post(evMove{eid: r.p.eid, x: x, y: y, z: z, yaw: yaw, pitch: pitch, onGround: onGround})
	r.s.checkPendingDim(r.p) // portal dwell fires on the movement cadence, like playLoop
}

// Dig/Place/HeldSlot re-encode the domain frame into the serverbound body the
// existing handlers parse — scaffolding with the same deletion story as the
// decoder below.
func (r *remotePlayer) Dig(d attachproto.Dig) {
	b := protocol.AppendVarInt(nil, d.Status)
	b = protocol.AppendPosition(b, d.X, d.Y, d.Z)
	b = append(b, byte(d.Face))
	b = protocol.AppendVarInt(b, 0) // seq: the gateway acks locally
	r.s.handleDig(r.p, b)
}

func (r *remotePlayer) Place(pl attachproto.Place) {
	b := protocol.AppendVarInt(nil, pl.Hand)
	b = protocol.AppendPosition(b, pl.X, pl.Y, pl.Z)
	b = protocol.AppendVarInt(b, pl.Face)
	b = protocol.AppendF32(b, pl.CX)
	b = protocol.AppendF32(b, pl.CY)
	b = protocol.AppendF32(b, pl.CZ)
	b = protocol.AppendBool(b, pl.Inside)
	b = protocol.AppendBool(b, false) // world border hit
	b = protocol.AppendVarInt(b, 0)   // seq: gateway acks locally
	r.s.handlePlace(r.p, b)
}

func (r *remotePlayer) HeldSlot(slot int16) {
	r.p.handleHeldItem(protocol.AppendI16(nil, slot))
	// Same side effects as the TCP dispatch: switching slots lowers a bow /
	// stops eating, and everyone else should see the new held item.
	r.s.hub.post(evStopEat{eid: r.p.eid})
	r.s.hub.post(evHeldChange{eid: r.p.eid})
}

// decodeLoop pumps the hub's outbound event queue into attach frames.
func (r *remotePlayer) decodeLoop() {
	send := func(typ byte, v any) {
		payload, err := json.Marshal(v)
		if err == nil {
			r.emit(typ, payload)
		}
	}
	for {
		select {
		case pkt := <-r.p.out:
			if pkt.ev != nil {
				r.emitEv(pkt.ev, send)
			}
		case <-r.p.quit:
			return
		}
	}
}

// emitEvNow serializes one typed event synchronously — for the join sequence,
// before the decode loop is pumping.
func (r *remotePlayer) emitEvNow(ev any) {
	r.emitEv(ev, func(typ byte, v any) {
		if payload, err := json.Marshal(v); err == nil {
			r.emit(typ, payload)
		}
	})
}

// emitEv maps a typed domain event to its attach frame. Events ARE the attach
// types, so this is only a type→frame-id dispatch.
func (r *remotePlayer) emitEv(ev any, send func(byte, any)) {
	switch ev.(type) {
	case attachproto.Rehome:
		send(attachproto.MsgRehome, ev)
	case attachproto.PlayerInfo:
		send(attachproto.MsgPlayerInfo, ev)
	case attachproto.PlayerGone:
		send(attachproto.MsgPlayerGone, ev)
	case attachproto.EntityAdd:
		send(attachproto.MsgEntityAdd, ev)
	case attachproto.EntityMove:
		send(attachproto.MsgEntityMove, ev)
	case attachproto.EntityHead:
		send(attachproto.MsgEntityHead, ev)
	case attachproto.EntityRemove:
		send(attachproto.MsgEntityRemove, ev)
	case attachproto.Chat:
		send(attachproto.MsgChat, ev)
	case attachproto.AdvTree:
		send(attachproto.MsgAdvTree, ev)
	case attachproto.AdvProgress:
		send(attachproto.MsgAdvProgress, ev)
	case attachproto.Stats:
		send(attachproto.MsgStats, ev)
	case attachproto.RecipeSettings:
		send(attachproto.MsgRecipeSettings, ev)
	case attachproto.Objective:
		send(attachproto.MsgObjective, ev)
	case attachproto.DisplaySlot:
		send(attachproto.MsgDisplaySlot, ev)
	case attachproto.Score:
		send(attachproto.MsgScore, ev)
	case attachproto.Team:
		send(attachproto.MsgTeam, ev)
	case attachproto.MapData:
		send(attachproto.MsgMapData, ev)
	case attachproto.SignText:
		send(attachproto.MsgSignText, ev)
	case attachproto.SignEditor:
		send(attachproto.MsgSignEditor, ev)
	case attachproto.BossBar:
		send(attachproto.MsgBossBar, ev)
	case attachproto.Time:
		send(attachproto.MsgTime, ev)
	case attachproto.Health:
		send(attachproto.MsgHealth, ev)
	case attachproto.XP:
		send(attachproto.MsgXP, ev)
	case attachproto.Effect:
		send(attachproto.MsgEffect, ev)
	case attachproto.Hurt:
		send(attachproto.MsgHurt, ev)
	case attachproto.Death:
		send(attachproto.MsgDeath, ev)
	case attachproto.Equipment:
		send(attachproto.MsgEquipment, ev)
	case attachproto.EntityMeta:
		send(attachproto.MsgEntityMeta, ev)
	case attachproto.WindowOpen:
		send(attachproto.MsgWindowOpen, ev)
	case attachproto.WindowItems:
		send(attachproto.MsgWindowItems, ev)
	case attachproto.WindowSlot:
		send(attachproto.MsgWindowSlot, ev)
	case attachproto.WindowData:
		send(attachproto.MsgWindowData, ev)
	case attachproto.HeldSync:
		send(attachproto.MsgHeldSync, ev)
	case attachproto.Collect:
		send(attachproto.MsgCollect, ev)
	case attachproto.Sound:
		send(attachproto.MsgSound, ev)
	case attachproto.Particles:
		send(attachproto.MsgParticles, ev)
	case attachproto.WorldFX:
		send(attachproto.MsgWorldFX, ev)
	case attachproto.BlockSet:
		send(attachproto.MsgBlockSet, ev)
	case attachproto.GameEvent:
		send(attachproto.MsgGameEvent, ev)
	case attachproto.Abilities:
		send(attachproto.MsgAbilities, ev)
	case attachproto.Passengers:
		send(attachproto.MsgPassengers, ev)
	case attachproto.VehicleMove:
		send(attachproto.MsgVehicleMove, ev)
	case attachproto.Velocity:
		send(attachproto.MsgVelocity, ev)
	case attachproto.Trades:
		send(attachproto.MsgTrades, ev)
	case attachproto.CursorItem:
		send(attachproto.MsgCursorItem, ev)
	case attachproto.Difficulty:
		send(attachproto.MsgDifficulty, ev)
	case attachproto.CommandTree:
		send(attachproto.MsgCommandTree, ev)
	case attachproto.Dimension:
		send(attachproto.MsgDimension, ev)
	case attachproto.Teleport:
		send(attachproto.MsgTeleport, ev)
	case attachproto.EntityStatus:
		send(attachproto.MsgEntityStatus, ev)
	case attachproto.Swing:
		send(attachproto.MsgSwing, ev)
	case attachproto.RecipeBook:
		send(attachproto.MsgRecipeBook, ev)
	case attachproto.Resync:
		send(attachproto.MsgResync, ev)
	}
}
