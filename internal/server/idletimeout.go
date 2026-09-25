package server

import (
	"fmt"
	"strconv"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /setidletimeout <minutes> (SetPlayerIdleTimeoutCommand) and the kick it
// arms: a player who has done nothing for that many minutes is disconnected
// with "You have been idle for too long!" (ServerGamePacketListenerImpl.tick).
// Zero turns it off. The setting is kept with the world, as the dedicated
// server keeps player-idle-timeout in its properties.

// touch is ServerPlayer.resetLastActionTime. Safe from any goroutine.
func (p *player) touch() { p.lastAction.Store(time.Now().UnixMilli()) }

// countsAsActivity is which serverbound actions reset the idle clock: the
// handlers that call resetLastActionTime (input, digging and using, attacks
// and interactions, container clicks and buttons, signs, respawn and stats
// requests). Steering a boat or a ridden vehicle does not.
func countsAsActivity(v any) bool {
	switch v.(type) {
	case attachproto.Input, attachproto.PlayerAction, attachproto.UseItem, attachproto.UseEntity,
		attachproto.WindowClick, attachproto.Craft, attachproto.SelTrade, attachproto.Enchant,
		attachproto.SignUpdate, attachproto.RespawnReq, attachproto.StatsReq:
		return true
	}
	return false
}

type evSetIdleTimeout struct {
	by      int32
	minutes int
}

func (evSetIdleTimeout) isHubEvent() {}

func (s *Server) cmdSetIdleTimeout(p *player, args []string) {
	if !s.isOp(p.name) { // LEVEL_ADMINS
		p.tell("You don't have permission.")
		return
	}
	if len(args) != 1 {
		p.tell("Usage: /setidletimeout <minutes>")
		return
	}
	n, err := strconv.ParseInt(args[0], 10, 32)
	if err != nil {
		p.tell(fmt.Sprintf("Invalid integer '%s'", args[0]))
		return
	}
	if n < 0 {
		p.tell(fmt.Sprintf("Integer must not be less than 0, found %d", n))
		return
	}
	s.hub.post(evSetIdleTimeout{by: p.eid, minutes: int(n)})
}

func (h *hub) applySetIdleTimeout(players map[int32]*tracked, e evSetIdleTimeout) {
	h.rules.IdleTimeout = e.minutes
	h.saveRules()
	if e.minutes > 0 {
		h.cmdOK(players, e.by)(fmt.Sprintf("The player idle timeout is now %d minute(s)", e.minutes))
	} else {
		h.cmdOK(players, e.by)("The player idle timeout is now disabled")
	}
}

// idleKick disconnects whoever has been idle past the timeout. A player
// watching the credits is left alone (wonGame).
func (h *hub) idleKick(players map[int32]*tracked) {
	mins := h.rules.IdleTimeout
	if mins <= 0 {
		return
	}
	now := time.Now().UnixMilli()
	for _, t := range players {
		last := t.p.lastAction.Load()
		if last > 0 && now-last > int64(mins)*60_000 && !t.wonGame {
			t.p.trySendEv(attachproto.Disconnect{Reason: "You have been idle for too long!"})
			t.p.disconnect()
		}
	}
}
