package server

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Operator and ban commands backed by tachyne-access, the one policy store
// both the gateways (identity) and the ingress (IP) enforce: /op, /deop,
// /ban, /pardon, /ban-ip, /pardon-ip, /banlist. With no access service
// configured, /ban and /pardon keep to the world's own gatekeeper file.

// onlineByName finds an online player's session (the hub owns the map, so
// this asks it).
func (s *Server) onlineByName(name string) (uuid, ip string, found bool) {
	done := make(chan struct{})
	s.onHub(func(players map[int32]*tracked) {
		defer close(done)
		for _, t := range players {
			if strings.EqualFold(t.p.name, name) {
				uuid, ip, found = uuidString(t.p.uuid), t.p.ip, true
				return
			}
		}
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	return
}

func (s *Server) accessCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// cmdOp is /op <player>: the op role in tachyne-access (OpCommand).
func (s *Server) cmdOp(p *player, args []string, grant bool) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if s.Access == nil || len(args) != 1 {
		p.tell("Usage: /op <player>  (needs the access service)")
		return
	}
	uuid, _, ok := s.onlineByName(args[0])
	if !ok {
		cmdFail(p, "That player does not exist") // the profile lookup needs them online here
		return
	}
	ctx, cancel := s.accessCtx()
	defer cancel()
	var err error
	if grant {
		err = s.Access.Grant(ctx, p.name, uuid, roleOp)
	} else {
		err = s.Access.Revoke(ctx, p.name, uuid, roleOp)
	}
	if err != nil {
		cmdFail(p, "The access service did not take it: "+err.Error())
		return
	}
	if grant {
		s.roleOps.Store(args[0], true)
		p.tell("Made " + args[0] + " a server operator")
	} else {
		s.roleOps.Delete(args[0])
		p.tell("Made " + args[0] + " no longer a server operator")
	}
}

// cmdBanAccess is /ban <player> [reason] against tachyne-access (BanPlayerCommands).
func (s *Server) cmdBanAccess(p *player, args []string) {
	if len(args) < 1 {
		p.tell("Usage: /ban <player> [reason]")
		return
	}
	reason := "Banned by an operator."
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}
	kind, value := "name", args[0]
	if uuid, _, ok := s.onlineByName(args[0]); ok {
		kind, value = "uuid", uuid
	}
	ctx, cancel := s.accessCtx()
	defer cancel()
	if _, err := s.Access.AddBan(ctx, p.name, kind, value, reason); err != nil {
		cmdFail(p, "The access service did not take it: "+err.Error())
		return
	}
	s.hub.post(evKick{by: p, name: args[0], reason: "You are banned from this server.\nReason: " + reason})
	p.tell(fmt.Sprintf("Banned %s: %s", args[0], reason))
}

// cmdBanIP is /ban-ip <address|player> [reason] (BanIpCommands): an ip ban,
// which the ingress enforces for every edition.
func (s *Server) cmdBanIP(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if s.Access == nil || len(args) < 1 {
		p.tell("Usage: /ban-ip <address|player> [reason]  (needs the access service)")
		return
	}
	ip := args[0]
	if net.ParseIP(ip) == nil {
		_, pip, ok := s.onlineByName(args[0])
		if !ok || pip == "" {
			cmdFail(p, "Invalid IP address or unknown player")
			return
		}
		ip = pip
	}
	reason := "Banned by an operator."
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}
	ctx, cancel := s.accessCtx()
	defer cancel()
	if _, err := s.Access.AddBan(ctx, p.name, "ip", ip, reason); err != nil {
		cmdFail(p, "The access service did not take it: "+err.Error())
		return
	}
	// Everyone online from that address goes too.
	s.onHub(func(players map[int32]*tracked) {
		for _, t := range players {
			if t.p.ip == ip {
				s.hub.onKick(players, evKick{by: p, name: t.p.name, reason: "You are banned from this server.\nReason: " + reason})
			}
		}
	})
	p.tell(fmt.Sprintf("Banned IP %s: %s", ip, reason))
}

// cmdPardonAccess is /pardon <player> and /pardon-ip <address>.
func (s *Server) cmdPardonAccess(p *player, args []string, ip bool) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) != 1 {
		p.tell("Usage: /pardon <player> | /pardon-ip <address>")
		return
	}
	ctx, cancel := s.accessCtx()
	defer cancel()
	bans, err := s.Access.Bans(ctx)
	if err != nil {
		cmdFail(p, "The access service did not answer: "+err.Error())
		return
	}
	uuid, _, _ := s.onlineByName(args[0])
	lifted := 0
	for _, b := range bans {
		match := false
		switch {
		case ip:
			match = b.Kind == "ip" && b.Value == args[0]
		default:
			match = (b.Kind == "name" && strings.EqualFold(b.Value, args[0])) || (b.Kind == "uuid" && uuid != "" && b.Value == uuid)
		}
		if match && !b.Revoked {
			if s.Access.RevokeBan(ctx, p.name, b.ID) == nil {
				lifted++
			}
		}
	}
	switch {
	case lifted == 0 && ip:
		cmdFail(p, "Nothing changed. That IP isn't banned")
	case lifted == 0:
		cmdFail(p, "Nothing changed. The player isn't banned")
	case ip:
		p.tell("Unbanned IP " + args[0])
	default:
		p.tell("Unbanned " + args[0])
	}
}

// cmdBanlist is /banlist [ips|players] (BanListCommands).
func (s *Server) cmdBanlist(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if s.Access == nil {
		p.tell("The ban list lives in the access service, which is not configured.")
		return
	}
	ctx, cancel := s.accessCtx()
	defer cancel()
	bans, err := s.Access.Bans(ctx)
	if err != nil {
		cmdFail(p, "The access service did not answer: "+err.Error())
		return
	}
	var lines []string
	for _, b := range bans {
		if b.Revoked {
			continue
		}
		if len(args) == 1 && ((args[0] == "ips") != (b.Kind == "ip")) {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s was banned by %s: %s", b.Value, b.IssuedBy, b.Reason))
	}
	if len(lines) == 0 {
		p.tell("There are no bans")
		return
	}
	p.tell(fmt.Sprintf("There are %d ban(s):", len(lines)))
	for _, l := range lines {
		p.tell(l)
	}
}
