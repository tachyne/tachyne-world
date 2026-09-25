package server

// remote.go bridges attach sessions (gateway players) into the hub. A remote
// player IS a hub player: the hub emits typed domain events to its out
// channel, and the pump below serializes them as attach frames.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/tachyne/tachyne-world/internal/attach"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// JoinRemote gives an attach session full hub presence (visible to and seeing
// every other player and mob, chat included). emit receives domain frames.
func (s *Server) JoinRemote(id attach.Identity, emit func(typ byte, payload []byte)) (attach.Remote, error) {
	name := id.Name
	ids.learn(name, id.UUID, id.Edition) // usercache.json: this name is this UUID now
	p := newPlayer(s.hub.mintPlayerEID(), name, id.UUID)
	s.claimLegacyData(name, p.key())
	if id.Edition == "bedrock" {
		s.claimBedrockRename(name, p.key())
	}
	s.adoptIdentity(p, id)
	x, y, z := s.joinSpawn()
	var yaw, pitch float32
	// Vanilla logs a returning player back in where they logged out. Restore
	// their last OVERWORLD position; a new player, or one who logged out in the
	// nether/end (a fresh join into another dimension needs the dimension-switch
	// machinery), uses world spawn instead. Bed/anchor is a DEATH concern.
	if s.hub.invs != nil {
		if px, py, pz, pyaw, ppitch, pdim, ok := s.hub.invs.savedPos(p.key()); ok && pdim == 0 {
			x, y, z, yaw, pitch = px, py, pz, pyaw, ppitch
		}
	}
	p.x, p.y, p.z, p.yaw, p.pitch = x, y, z, yaw, pitch
	r := &remotePlayer{s: s, p: p, emit: emit, x: x, y: y, z: z, gm: -1}
	go r.decodeLoop()
	mode := s.modes.pin(p.key()) // a later /defaultgamemode is for new players only
	// Join-time extras the TCP path sends in handlePlay: tab-completion tree
	// and the mode's abilities (creative flight).
	r.emitEvNow(attachproto.CommandTree{Data: r.s.commandTreeBytes()})
	r.emitEvNow(abilitiesFor(mode))
	r.emitEvNow(opLevelEvent(p.eid, s.isOp(p.name)))
	s.hub.post(evJoin{p: p, x: x, y: y, z: z, yaw: yaw, pitch: pitch, gamemode: mode})
	return r, nil
}

type remotePlayer struct {
	s       *Server
	p       *player
	emit    func(byte, []byte)
	x, y, z float64
	gm      int32 // resume: explicit gamemode from the migration snapshot (< 0 = use modeStore)
}

func (r *remotePlayer) EID() int32                   { return r.p.eid }
func (r *remotePlayer) Spawn() (x, y, z float64)     { return r.x, r.y, r.z }
func (r *remotePlayer) Death() *attachproto.DeathPos { return r.s.deathOf(r.p.key()) }
func (r *remotePlayer) Gamemode() int32 {
	if r.gm >= 0 {
		return r.gm
	}
	return int32(r.s.modes.get(r.p.key()))
}

// ResumeRemote binds a reconnecting gateway session to a player that was migrated
// to this pod (Hello{Purpose:"resume", token}). It claims the pending snapshot,
// recreates the player with the SAME session-stable eid, and resumes it — no
// fresh spawn, no "joined" broadcast; health/food/effects/inventory/xp and
// gamemode come from the snapshot, not the on-disk store.
func (s *Server) ResumeRemote(id attach.Identity, token string, emit func(typ byte, payload []byte)) (attach.Remote, error) {
	name, uuid := id.Name, id.UUID
	ids.learn(name, uuid, id.Edition)
	ps, ok := s.hub.claimPending(token)
	if !ok {
		return nil, fmt.Errorf("resume: no pending handover for token %q", token)
	}
	p := newPlayer(ps.EID, name, uuid)
	s.adoptIdentity(p, id)
	p.x, p.y, p.z = ps.X, ps.Y, ps.Z
	r := &remotePlayer{s: s, p: p, emit: emit, x: ps.X, y: ps.Y, z: ps.Z, gm: ps.Gamemode}
	go r.decodeLoop()
	mode := int(ps.Gamemode)
	r.emitEvNow(attachproto.CommandTree{Data: r.s.commandTreeBytes()})
	r.emitEvNow(abilitiesFor(mode))
	r.emitEvNow(opLevelEvent(p.eid, s.isOp(p.name)))
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
	case attachproto.PaddleBoat:
		h.post(evPaddleBoat{eid: p.eid, left: e.Left, right: e.Right})
	case attachproto.PickItem:
		h.post(evPickItem{eid: p.eid, e: e})
	case attachproto.SwingAction:
		h.post(evArmSwing{eid: p.eid, hand: e.Hand}) // handleAnimate → LivingEntity.swing
	case attachproto.UseItem:
		p.noteAck(e.Seq) // vanilla acks use_item's prediction sequence too
		// The client says which hand it used; the offhand is where a shield
		// lives, and food, rockets and throwables work from it too.
		item, slot := p.heldItem(), int32(p.held)
		off := e.Hand == handOffhand // Player.getUsedItemHand: what the use draws on
		if off {
			item, slot = p.offhandItem(), offhandSlot
		}
		if equipSlotOnUse(item) >= 0 { // armour in hand goes on
			h.post(evEquipHeld{eid: p.eid})
			return
		}
		if spearOf(item) != nil && e.Hand != handOffhand { // a spear is lowered for the charge
			h.post(evSpearUse{eid: p.eid})
			return
		}
		switch item {
		case itemBow:
			h.post(evBowStart{eid: p.eid, off: off})
		case itemCrossbow:
			h.post(evXbowUse{eid: p.eid, off: off})
		case itemTrident:
			h.post(evTridentUse{eid: p.eid, off: off})
		case itemFishingRod:
			h.post(evFishUse{eid: p.eid})
		case itemCarrotOnStick, itemWarpedFungusStick:
			h.post(evSteerBoost{eid: p.eid, slot: int(slot)})
		case itemBucket: // aiming at a fluid: the client sends plain use_item
			h.post(evBucketFill{eid: p.eid, slot: slot})
		case int32(itemGlassBottle): // BottleItem.use: dragon's breath, else water
			h.post(evFillBottle{eid: p.eid, slot: slot})
		case itemShield:
			h.post(evBlockStart{eid: p.eid, hand: e.Hand})
		case itemSnowball, itemEgg, itemBlueEgg, itemBrownEgg:
			h.post(evThrow{eid: p.eid, item: item, off: off})
		case itemSplashPotion, itemLingerPotion:
			h.post(evThrowPotion{eid: p.eid, slot: int(slot)})
		case itemXPBottle:
			h.post(evThrowXPBottle{eid: p.eid, off: off})
		case itemSpyglass:
			h.post(evSpyglass{eid: p.eid, off: off})
		case itemFireworkRocket:
			h.post(evUseFirework{eid: p.eid, off: off})
		case itemGoatHorn:
			h.post(evUseHorn{eid: p.eid, off: off})
		case itemFrogspawn: // placed on the water surface, not against a face
			h.post(evPlaceOnWater{eid: p.eid})
		case itemEnderPearl:
			h.post(evThrowPearl{eid: p.eid, off: off})
		case itemWindCharge:
			h.post(evThrowWindCharge{eid: p.eid, off: off})
		case itemEnderEye:
			h.post(evThrowEye{eid: p.eid, off: off})
		case itemEmptyMap:
			h.post(evUseMap{eid: p.eid, slot: slot})
		case itemWrittenBook, itemWritableBook:
			r.emitEvNow(attachproto.OpenBook{Hand: 0}) // the reader/editor UI is client-side
		default:
			// BoatItem.use: the crosshair is on water, which the client
			// reports as a plain use because a fluid is not a clickable block.
			if _, isVeh := vehicleItems[item]; isVeh {
				h.post(evPlaceVehicleLook{eid: p.eid, item: item, slot: slot})
				return
			}
			if spawnEggEntity[item] != 0 { // SpawnEggItem.use: the look ray onto a fluid
				h.post(evSpawnEggLook{eid: p.eid, slot: slot})
				return
			}
			h.post(evEat{eid: p.eid, slot: int(slot)})
		}
	case attachproto.UseEntity:
		switch {
		case e.Attack:
			h.post(evAttack{attacker: p.eid, target: e.Target})
		case e.Hand == 1:
			// The client sends an OFF_HAND interact only after the main
			// hand's passed. Mob interaction here reads the main hand, so
			// running it again would repeat the main-hand action (a
			// sitting pet toggled twice, a second feed); until offhand
			// items on mobs are threaded through, the offhand click is
			// the pass it was on the client.
		default:
			h.post(evInteractMob{eid: p.eid, target: e.Target, sneak: p.sneaking})
		}
	case attachproto.VehicleMove:
		h.post(evVehicleMove{eid: p.eid, x: e.X, y: e.Y, z: e.Z, yaw: e.Yaw})
	case attachproto.SelTrade:
		h.post(evSelTrade{eid: p.eid, slot: e.Slot})
	case attachproto.Input:
		// Since 1.21.6 the shift key comes only in player_input (the sneak
		// actions left player_command): it is what crouches, and what makes a
		// click place against a chest instead of opening it.
		if e.Sneak != p.sneaking {
			p.sneaking = e.Sneak
			h.post(evSneak{eid: p.eid, sneaking: e.Sneak})
		}
		h.post(evInput{eid: p.eid, in: e})
	case attachproto.WindowClick:
		ev := evClick{eid: p.eid, windowID: e.ID, slot: int16(e.Slot), mode: e.Mode, button: e.Button,
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
		if name, ok := anvilName(e.Name); ok {
			h.post(evRename{eid: p.eid, name: name})
		}
	case attachproto.Enchant:
		h.post(evEnchant{eid: p.eid, button: e.Button})
	case attachproto.SetBeacon:
		h.post(evSetBeacon{eid: p.eid, primary: e.Primary, secondary: e.Secondary})
	case attachproto.BundleSelect:
		h.post(evBundleSelect{eid: p.eid, slot: e.Slot, selected: e.Selected})
	case attachproto.SlotState:
		h.post(evSlotState{eid: p.eid, slot: e.Slot, enable: e.State})
	case attachproto.EditBook:
		h.post(evEditBook{eid: p.eid, slot: e.Slot, pages: e.Pages, title: e.Title, hasTitle: e.HasTitle})
	case attachproto.PlayerAction:
		switch e.Action { // 0 sneak, 1 unsneak, 2 leave bed, 3/4 sprint
		case 0: // PRESS_SHIFT_KEY (1.21.5 clients; later ones send the shift bit)
			p.sneaking = true
			h.post(evSneak{eid: p.eid, sneaking: true})
		case 1:
			p.sneaking = false
			h.post(evSneak{eid: p.eid, sneaking: false})
		case 2:
			h.post(evStopSleep{eid: p.eid})
		case 3:
			p.sprinting = true
		case 4:
			p.sprinting = false
		case 5: // START_RIDING_JUMP: a camel dashes (horses jump client-side)
			h.post(evRidingJump{eid: p.eid})
		case 7: // OPEN_INVENTORY: E while riding opens the mount's screen
			h.post(evOpenMountInv{eid: p.eid})
		case 8: // START_FALL_FLYING: the jump pressed in mid-air on an elytra
			h.post(evFallFly{eid: p.eid})
		}
	case attachproto.RespawnReq:
		h.post(evRespawn{eid: p.eid})
	case attachproto.StatsReq:
		h.post(evStatsReq{eid: p.eid})
	case attachproto.PlayerAbilities:
		h.post(evAbilities{eid: p.eid, flying: e.Flying})
	case attachproto.TeleportToEntity:
		h.post(evTeleportToEntity{eid: p.eid, uuid: e.UUID})
	case attachproto.PlayerLoaded:
		h.post(evClientLoaded{eid: p.eid})
	case attachproto.Latency:
		p.latency.Store(max(e.MS, 0))
	case attachproto.GameRuleReq:
		// ServerGamePacketListenerImpl.handleClientCommand
		// REQUEST_GAMERULE_VALUES: answered for a gamemaster only.
		if r.s.isOp(p.name) {
			r.s.onHub(func(players map[int32]*tracked) {
				p.trySendEv(attachproto.GameRuleValues{Values: h.gameRuleValues()})
			})
		}
	case attachproto.RecipeSettingChange:
		h.post(evRecipeSettings{eid: p.eid, book: e.Book, open: e.Open, filter: e.Filter})
	case attachproto.RecipeSeen:
		h.post(evRecipeSeen{eid: p.eid, id: e.ID})
	case attachproto.SignUpdate:
		h.post(evSignUpdate{eid: p.eid, x: int(e.X), y: int(e.Y), z: int(e.Z), front: e.Front, lines: e.Lines})
	case attachproto.CreativeSlot:
		if r.s.modes.get(p.key()) != gmCreative {
			return
		}
		r.s.applyCreativeSlot(p, int16(e.Slot), e.Item.ID, int(e.Item.Count), e.PaintingVariant)
	}
}

func (r *remotePlayer) Leave() { r.s.hub.post(evLeave{p: r.p}); r.p.disconnect() }
func (r *remotePlayer) Move(x, y, z float64, yaw, pitch float32, onGround bool) {
	r.p.x, r.p.y, r.p.z = x, y, z
	// The session player's look direction feeds placement orientation
	// (vanilla UseOnContext.getRotation() = the player's live yaw): sign
	// rotation, stairs/bed/furnace facing. Only x/y/z were carried over in
	// the domain-events refactor, freezing p.yaw at 0 — every yaw-derived
	// placement silently faced the yaw-0 direction until this line.
	r.p.yaw, r.p.pitch = yaw, pitch
	// sprinting rides the move: player_command's start/stop sprint set it,
	// and the hub reads it for sprint hunger, knockback and the crit/sweep
	// rules. It was never passed, so the hub always saw a walking player.
	r.s.hub.post(evMove{eid: r.p.eid, x: x, y: y, z: z, yaw: yaw, pitch: pitch, onGround: onGround, sprinting: r.p.sprinting})
	r.s.checkPendingDim(r.p) // portal dwell fires on the movement cadence, like playLoop
}

// Dig/Place/HeldSlot re-encode the domain frame into the serverbound body the
// existing handlers parse — scaffolding with the same deletion story as the
// decoder below.
func (r *remotePlayer) Dig(d attachproto.Dig) {
	b := protocol.AppendVarInt(nil, d.Status)
	b = protocol.AppendPosition(b, d.X, d.Y, d.Z)
	b = append(b, byte(d.Face))
	b = protocol.AppendVarInt(b, d.Seq) // the world acks it, after processing
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
	b = protocol.AppendBool(b, false)    // world border hit
	b = protocol.AppendVarInt(b, pl.Seq) // the world acks it, after processing
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
	drainCrit := func() {
		for _, pkt := range r.p.takeCrit() {
			if pkt.ev != nil {
				r.emitEv(pkt.ev, send)
			}
		}
	}
	for {
		// `out` has priority: lifecycle frames only overflow into `crit` after
		// `out` was full, so they are always temporally later — drain `out` to
		// empty before touching `crit` to keep global order.
		select {
		case pkt := <-r.p.out:
			if pkt.ev != nil {
				r.emitEv(pkt.ev, send)
			}
			continue
		default:
		}
		drainCrit()
		select {
		case pkt := <-r.p.out:
			if pkt.ev != nil {
				r.emitEv(pkt.ev, send)
			}
		case <-r.p.critWake:
			// loop; drainCrit at the top of the next iteration handles it
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
	case attachproto.PlayerInfoMode:
		send(attachproto.MsgPlayerInfoMode, ev)
	case bundleOpen:
		send(attachproto.MsgBundleOpen, attachproto.BundleMark{})
	case bundleClose:
		send(attachproto.MsgBundleClose, attachproto.BundleMark{})
	case attachproto.Transfer:
		send(attachproto.MsgTransfer, ev)
	case attachproto.Camera:
		send(attachproto.MsgCamera, ev)
	case attachproto.TickingState:
		send(attachproto.MsgTickingState, ev)
	case attachproto.TickingStep:
		send(attachproto.MsgTickingStep, ev)
	case attachproto.Explode:
		send(attachproto.MsgExplode, ev)
	case attachproto.DamageEvent:
		send(attachproto.MsgDamageEvent, ev)
	case attachproto.PlayerInfoLatency:
		send(attachproto.MsgPlayerInfoLatency, ev)
	case attachproto.GameRuleValues:
		send(attachproto.MsgGameRuleValues, ev)
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
	case attachproto.StopSound:
		send(attachproto.MsgStopSound, ev)
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
	case attachproto.BlockEvent:
		send(attachproto.MsgBlockEvent, ev)
	case attachproto.BlockAck:
		send(attachproto.MsgBlockAck, ev)
	case attachproto.BlockSet:
		send(attachproto.MsgBlockSet, ev)
	case attachproto.GameEvent:
		send(attachproto.MsgGameEvent, ev)
	case attachproto.Abilities:
		send(attachproto.MsgAbilities, ev)
	case attachproto.Passengers:
		send(attachproto.MsgPassengers, ev)
	case attachproto.EntityLink:
		send(attachproto.MsgEntityLink, ev)
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
	case attachproto.CampfireItems:
		send(attachproto.MsgCampfireItems, ev)
	case attachproto.SpawnerData:
		send(attachproto.MsgSpawnerData, ev)
	case attachproto.DefaultSpawn:
		send(attachproto.MsgDefaultSpawn, ev)
	case attachproto.Title:
		send(attachproto.MsgTitle, ev)
	case attachproto.Disconnect:
		send(attachproto.MsgDisconnect, ev)
	case attachproto.ShelfItems:
		send(attachproto.MsgShelfItems, ev)
	case attachproto.MovingPiston:
		send(attachproto.MsgMovingPiston, ev)
	case attachproto.EntityAttributes:
		send(attachproto.MsgEntityAttributes, ev)
	case attachproto.BlockBreakProgress:
		send(attachproto.MsgBlockBreakProgress, ev)
	case attachproto.ItemCooldown:
		send(attachproto.MsgItemCooldown, ev)
	case attachproto.WindowCloseServer:
		send(attachproto.MsgWindowCloseServer, ev)
	case attachproto.WorldBorder:
		send(attachproto.MsgWorldBorder, ev)
	case attachproto.BannerPatterns:
		send(attachproto.MsgBannerPatterns, ev)
	case attachproto.HorseScreen:
		send(attachproto.MsgHorseScreen, ev)
	case attachproto.OpenBook:
		send(attachproto.MsgOpenBook, ev)
	case attachproto.Waypoint:
		send(attachproto.MsgWaypoint, ev)
	default:
		// A domain frame with no dispatch case would drop SILENTLY, so this is
		// a hard error in tests/dev: the emitEv switch must list every w→gw
		// frame the engine emits (four of today's features hit this gap).
		emitUnhandled(ev)
	}
}

// emitUnhandled records a frame type with no emitEv case. It never drops
// quietly: dev builds log it (a missing case = an invisible feature).
func emitUnhandled(ev any) {
	log.Printf("emitEv: BUG — no attach frame for %T (dropped)", ev)
}

// adoptIdentity gives a joining player what its gateway vouched for: the
// profile properties other clients draw its skin from, and its tachyne-access
// roles — "op" makes it an operator, alongside the -ops list.
func (s *Server) adoptIdentity(p *player, id attach.Identity) {
	p.bedrock = id.Edition == "bedrock"
	for _, pr := range id.Props {
		p.props = append(p.props, skinProperty{Name: pr.Name, Value: pr.Value, Signature: pr.Signature})
	}
	op := false
	for _, r := range id.Roles {
		if r == roleOp {
			op = true
		}
	}
	if op {
		s.roleOps.Store(id.Name, true)
	} else {
		s.roleOps.Delete(id.Name)
	}
}

// claimLegacyData gives a joining player whatever was saved under their name
// before the stores were keyed by UUID (playerkeys.go).
func (s *Server) claimLegacyData(name, key string) {
	var moved []string
	claim := func(what string, ok bool) {
		if ok {
			moved = append(moved, what)
		}
	}
	if s.modes != nil {
		claim("game mode", s.modes.claim(name, key))
	}
	if h := s.hub; h != nil {
		if h.invs != nil {
			claim("inventory", h.invs.claim(name, key))
		}
		if h.advs != nil {
			claim("advancements", h.advs.claim(name, key))
		}
		if h.statstore != nil {
			claim("stats", h.statstore.claim(name, key))
		}
		if h.rbstore != nil {
			claim("recipe book", h.rbstore.claim(name, key))
		}
		if h.spawns != nil {
			claim("spawn point", h.spawns.claim(name, key))
		}
	}
	if len(moved) > 0 {
		log.Printf("player %s (%s): moved their %s from the name key to the UUID", name, key, strings.Join(moved, ", "))
	}
}

// claimBedrockRename carries a Bedrock player across the gateway's move to
// Floodgate's identity ("." + gamertag, the XUID UUID): what was saved under
// the bare gamertag, and under the UUID that gamertag last joined with, is
// theirs, and the old UUID is recorded as moved so their pets follow.
func (s *Server) claimBedrockRename(name, key string) {
	bare := strings.TrimPrefix(name, ".")
	if bare == name {
		return // not a prefixed name: nothing was renamed
	}
	for _, old := range []string{bare, strings.ReplaceAll(bare, "_", " ")} {
		u, known := ids.cached(old)
		// A Java player who has joined with the bare name owns it. Entries
		// from before editions were cached are told apart by the UUID: a
		// Java player's (offline) one is derived from the name.
		if known && (u.Edition == "java" || (u.Edition == "" && u.UUID == offlineUUIDString(old))) {
			continue
		}
		s.claimLegacyData(old, key)
		if known && u.UUID != key {
			s.claimLegacyData(u.UUID, key)
			ids.recordMove(u.UUID, key)
		}
	}
}

// opLevelEvent is PlayerList.sendPlayerPermissionLevel: entity event 24 +
// the permission level, which unlocks F3+F4, F3+N and the gamerule screen
// on the client. Operators are level 4.
func opLevelEvent(eid int32, op bool) attachproto.EntityStatus {
	lvl := byte(0)
	if op {
		lvl = 4
	}
	return entityStatus(eid, 24+lvl)
}
