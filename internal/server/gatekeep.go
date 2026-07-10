package server

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// Whitelist + bans: plain JSON files an admin can edit, plus /whitelist,
// /ban and /pardon commands. The gate runs at login, after online auth (so
// bans stick to verified identities when online mode is on).

type gatekeeper struct {
	mu        sync.Mutex
	path      string
	Whitelist struct {
		Enabled bool     `json:"enabled"`
		Names   []string `json:"names"`
	} `json:"whitelist"`
	Bans []string `json:"bans"`
}

func newGatekeeper(path string) *gatekeeper {
	g := &gatekeeper{path: path}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, g)
		}
	}
	return g
}

func (g *gatekeeper) save() {
	if g.path == "" {
		return
	}
	data, _ := json.MarshalIndent(g, "", "  ")
	os.WriteFile(g.path, data, 0o644)
}

func containsFold(list []string, name string) bool {
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// gatekeep returns a denial reason for a name, or "" to admit.
func (s *Server) gatekeep(name string) string {
	if s.gate == nil {
		return ""
	}
	s.gate.mu.Lock()
	defer s.gate.mu.Unlock()
	if containsFold(s.gate.Bans, name) {
		return "You are banned from this server."
	}
	if s.gate.Whitelist.Enabled && !containsFold(s.gate.Whitelist.Names, name) {
		return "You are not whitelisted on this server."
	}
	return ""
}

// cmdWhitelist: /whitelist add|remove|on|off|list <name>
func (s *Server) cmdWhitelist(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if s.gate == nil {
		p.tell("No gatekeeper file configured.")
		return
	}
	g := s.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case len(args) == 1 && args[0] == "on":
		g.Whitelist.Enabled = true
	case len(args) == 1 && args[0] == "off":
		g.Whitelist.Enabled = false
	case len(args) == 1 && args[0] == "list":
		p.tell("Whitelist (" + onOff(g.Whitelist.Enabled) + "): " + strings.Join(g.Whitelist.Names, ", "))
		return
	case len(args) == 2 && args[0] == "add":
		if !containsFold(g.Whitelist.Names, args[1]) {
			g.Whitelist.Names = append(g.Whitelist.Names, args[1])
		}
	case len(args) == 2 && args[0] == "remove":
		out := g.Whitelist.Names[:0]
		for _, n := range g.Whitelist.Names {
			if !strings.EqualFold(n, args[1]) {
				out = append(out, n)
			}
		}
		g.Whitelist.Names = out
	default:
		p.tell("Usage: /whitelist on|off|list|add <name>|remove <name>")
		return
	}
	g.save()
	p.tell("Whitelist updated.")
}

func (s *Server) cmdBan(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if s.gate == nil || len(args) != 1 {
		p.tell("Usage: /ban <name>")
		return
	}
	s.gate.mu.Lock()
	if !containsFold(s.gate.Bans, args[0]) {
		s.gate.Bans = append(s.gate.Bans, args[0])
	}
	s.gate.save()
	s.gate.mu.Unlock()
	p.tell(args[0] + " is banned.")
}

func (s *Server) cmdPardon(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if s.gate == nil || len(args) != 1 {
		p.tell("Usage: /pardon <name>")
		return
	}
	s.gate.mu.Lock()
	out := s.gate.Bans[:0]
	for _, n := range s.gate.Bans {
		if !strings.EqualFold(n, args[0]) {
			out = append(out, n)
		}
	}
	s.gate.Bans = out
	s.gate.save()
	s.gate.mu.Unlock()
	p.tell(args[0] + " is pardoned.")
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
