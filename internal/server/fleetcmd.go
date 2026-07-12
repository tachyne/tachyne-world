package server

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// /plugin — the one in-game plugin command (op-only). Daemon operations
// forward over the bus to every shard's tachyne-plugin-manager manager (plain op =
// fleet broadcast, at.<manager>.<op> = one shard); registry operations ride
// the same path. The engine forwards and prints; it never builds or runs
// daemon code itself.
//
//	/plugin list                       compiled-in set + fleet daemon
//	                                   inventory (flags OUTDATED shards)
//	/plugin search <query>             search the configured plugin registries
//	/plugin info <name>                one plugin's registry card
//	/plugin rate <name> <1-5>          rate a plugin (per shard host)
//	/plugin install <module|name> …    install everywhere (registry names resolve)
//	/plugin uninstall <name>           remove everywhere
//	/plugin restart <name>             restart everywhere
//	/plugin upgrade <name>             PROGRESSIVE fleet upgrade: one shard at
//	                                   a time, verified healthy before the next
const fleetWindow = 2 * time.Second

// managerReply is the common envelope every manager reply carries.
type managerReply struct {
	OK      bool   `json:"ok"`
	Manager string `json:"manager"`
	Error   string `json:"error"`
	Daemons []struct {
		Manager  string `json:"manager"`
		Name     string `json:"name"`
		Module   string `json:"module"`
		Version  string `json:"version"`
		Built    string `json:"built"`
		Latest   string `json:"latest"`
		Outdated bool   `json:"outdated"`
		Status   string `json:"status"`
		Restarts int    `json:"restarts"`
	} `json:"daemons"`
	Plugins []struct {
		Module      string  `json:"module"`
		Name        string  `json:"name"`
		Type        string  `json:"type"`
		Description string  `json:"description"`
		Latest      string  `json:"latest"`
		Installs    int     `json:"installs"`
		Rating      float64 `json:"rating"`
		Ratings     int     `json:"ratings"`
	} `json:"plugins"`
}

func parseReplies(raws []json.RawMessage) []managerReply {
	out := make([]managerReply, 0, len(raws))
	for _, raw := range raws {
		var r managerReply
		if json.Unmarshal(raw, &r) == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manager < out[j].Manager })
	return out
}

func (s *Server) cmdPlugin(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) == 0 {
		p.tell("Usage: /plugin <list|search <q>|info <name>|rate <name> <1-5>|install <module|name> [args…]|uninstall <name>|restart <name>|upgrade <name>>")
		return
	}
	switch args[0] {
	case "list":
		for _, line := range s.pluginsSummary() {
			p.tell(line)
		}
		s.daemonList(p)
	case "search":
		s.daemonSearch(p, strings.Join(args[1:], " "))
	case "info":
		if len(args) != 2 {
			p.tell("Usage: /plugin info <name>")
			return
		}
		s.daemonInfo(p, args[1])
	case "rate":
		if len(args) != 3 {
			p.tell("Usage: /plugin rate <name> <1-5>")
			return
		}
		s.daemonRate(p, args[1], args[2])
	case "install":
		if len(args) < 2 {
			p.tell("Usage: /plugin install <module|name> [args…]")
			return
		}
		module, version := args[1], ""
		if i := strings.Index(module, "@"); i >= 0 {
			module, version = module[:i], module[i+1:]
		}
		payload := map[string]any{"args": args[2:], "version": version}
		if strings.Contains(module, "/") {
			payload["module"] = module
		} else {
			payload["name"] = module // registry name — managers resolve it
		}
		s.daemonFleetOp(p, "install", payload)
	case "uninstall", "restart":
		if len(args) != 2 {
			p.tell(fmt.Sprintf("Usage: /plugin %s <name>", args[0]))
			return
		}
		s.daemonFleetOp(p, args[0], map[string]any{"name": args[1]})
	case "upgrade":
		if len(args) != 2 {
			p.tell("Usage: /plugin upgrade <name>  (progressive: one shard at a time)")
			return
		}
		go s.daemonProgressiveUpgrade(p, args[1]) // multi-shard walk — off the session loop
	default:
		p.tell("Usage: /plugin <list|search|info|rate|install|uninstall|restart|upgrade>")
	}
}

// daemonList gathers the whole fleet's inventory.
func (s *Server) daemonList(p *player) {
	raws, err := s.hub.bus.requestMany("mc.plugin.list", map[string]any{}, fleetWindow)
	if err != nil {
		p.tell("Daemon managers unreachable: " + err.Error())
		return
	}
	replies := parseReplies(raws)
	if len(replies) == 0 {
		p.tell("No daemon managers on the bus.")
		return
	}
	total, stale := 0, 0
	for _, r := range replies {
		if len(r.Daemons) == 0 {
			p.tell(fmt.Sprintf("[%s] no daemons", r.Manager))
			continue
		}
		for _, d := range r.Daemons {
			total++
			cur := d.Version
			if cur == "" {
				cur = d.Built
			}
			if cur == "" {
				cur = "latest"
			}
			line := fmt.Sprintf("[%s] %s — %s@%s [%s, %d restarts]",
				r.Manager, d.Name, d.Module, cur, d.Status, d.Restarts)
			if d.Outdated {
				stale++
				line += fmt.Sprintf(" *** OUTDATED (latest %s)", d.Latest)
			}
			p.tell(line)
		}
	}
	if stale > 0 {
		p.tell(fmt.Sprintf("%d of %d daemons outdated — /plugin upgrade <name> rolls the fleet progressively.", stale, total))
	}
}

// daemonSearch asks one manager to query the registries (all managers see
// the same registries — a single reply suffices).
func (s *Server) daemonSearch(p *player, q string) {
	raw, err := s.hub.bus.request("mc.plugin.search", map[string]any{"q": q})
	if err != nil {
		p.tell("Daemon managers unreachable: " + err.Error())
		return
	}
	var r managerReply
	if json.Unmarshal(raw, &r) != nil || !r.OK {
		p.tell("Search failed: " + r.Error)
		return
	}
	if len(r.Plugins) == 0 {
		p.tell("No plugins found for " + q)
		return
	}
	for i, pl := range r.Plugins {
		if i == 8 {
			p.tell(fmt.Sprintf("…and %d more.", len(r.Plugins)-8))
			break
		}
		p.tell(fmt.Sprintf("%s (%s) %s — %s [%d installs, %.1f★×%d]",
			pl.Name, pl.Type, pl.Latest, pl.Description, pl.Installs, pl.Rating, pl.Ratings))
	}
}

// daemonInfo shows one plugin's full registry card.
func (s *Server) daemonInfo(p *player, name string) {
	raw, err := s.hub.bus.request("mc.plugin.info", map[string]any{"name": name})
	if err != nil {
		p.tell("Daemon managers unreachable: " + err.Error())
		return
	}
	var r managerReply
	if json.Unmarshal(raw, &r) != nil || !r.OK || len(r.Plugins) == 0 {
		p.tell("Info failed: " + r.Error)
		return
	}
	pl := r.Plugins[0]
	p.tell(fmt.Sprintf("%s (%s) — %s", pl.Name, pl.Type, pl.Description))
	p.tell(fmt.Sprintf("  module %s", pl.Module))
	p.tell(fmt.Sprintf("  latest %s · %d installs · %.1f★ (%d ratings)",
		pl.Latest, pl.Installs, pl.Rating, pl.Ratings))
	p.tell(fmt.Sprintf("  install: /plugin install %s", pl.Name))
}

// daemonRate submits a rating through the manager (one rating per shard
// host — re-rating replaces).
func (s *Server) daemonRate(p *player, name, starsArg string) {
	stars := 0
	fmt.Sscanf(starsArg, "%d", &stars)
	if stars < 1 || stars > 5 {
		p.tell("Usage: /plugin rate <name> <1-5>")
		return
	}
	raw, err := s.hub.bus.request("mc.plugin.rate", map[string]any{"name": name, "stars": stars})
	if err != nil {
		p.tell("Daemon managers unreachable: " + err.Error())
		return
	}
	var r managerReply
	if json.Unmarshal(raw, &r) != nil || !r.OK {
		p.tell("Rating failed: " + r.Error)
		return
	}
	p.tell(fmt.Sprintf("Rated %s %d★.", name, stars))
}

// daemonFleetOp broadcasts a mutating op and reports per-manager outcomes.
func (s *Server) daemonFleetOp(p *player, op string, payload map[string]any) {
	raws, err := s.hub.bus.requestMany("mc.plugin."+op, payload, fleetWindow)
	if err != nil {
		p.tell("Daemon managers unreachable: " + err.Error())
		return
	}
	replies := parseReplies(raws)
	if len(replies) == 0 {
		p.tell("No daemon managers on the bus.")
		return
	}
	for _, r := range replies {
		if r.OK {
			p.tell(fmt.Sprintf("[%s] %s: done", r.Manager, op))
		} else {
			p.tell(fmt.Sprintf("[%s] %s: %s", r.Manager, op, r.Error))
		}
	}
}

// daemonProgressiveUpgrade rolls one daemon across the fleet, one manager
// at a time: upgrade (rebuild@latest + boot), verify the daemon reports
// running, then move on; any failure stops the roll so a bad release never
// takes the whole fleet down. Runs off the session goroutine.
func (s *Server) daemonProgressiveUpgrade(p *player, name string) {
	tell := func(f string, a ...any) { p.trySendEv(chatEv(fmt.Sprintf(f, a...))) }

	raws, err := s.hub.bus.requestMany("mc.plugin.list", map[string]any{}, fleetWindow)
	if err != nil {
		tell("Daemon managers unreachable: %v", err)
		return
	}
	var managers []string
	for _, r := range parseReplies(raws) {
		for _, d := range r.Daemons {
			if d.Name == name {
				managers = append(managers, r.Manager)
			}
		}
	}
	if len(managers) == 0 {
		tell("No shard runs a daemon named %q.", name)
		return
	}
	tell("Rolling %s across %d shard(s): %s", name, len(managers), strings.Join(managers, ", "))

	for i, mgr := range managers {
		tell("[%d/%d] %s: upgrading…", i+1, len(managers), mgr)
		raw, err := s.hub.bus.request("mc.plugin.at."+mgr+".upgrade", map[string]any{"name": name})
		if err != nil {
			tell("[%d/%d] %s: unreachable (%v) — roll STOPPED, %d shard(s) untouched.",
				i+1, len(managers), mgr, err, len(managers)-i-1)
			return
		}
		var r managerReply
		if json.Unmarshal(raw, &r) != nil || !r.OK {
			tell("[%d/%d] %s: %s — roll STOPPED, %d shard(s) untouched.",
				i+1, len(managers), mgr, r.Error, len(managers)-i-1)
			return
		}
		if !s.daemonHealthy(mgr, name, 30*time.Second) {
			tell("[%d/%d] %s: %s did not come back healthy — roll STOPPED, %d shard(s) untouched.",
				i+1, len(managers), mgr, name, len(managers)-i-1)
			return
		}
		tell("[%d/%d] %s: healthy.", i+1, len(managers), mgr)
	}
	tell("Fleet upgrade of %s complete (%d shard(s)).", name, len(managers))
}

// daemonHealthy polls one manager until the daemon reports running.
func (s *Server) daemonHealthy(mgr, name string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		raw, err := s.hub.bus.request("mc.plugin.at."+mgr+".list", map[string]any{})
		if err == nil {
			var r managerReply
			if json.Unmarshal(raw, &r) == nil {
				for _, d := range r.Daemons {
					if d.Name == name && d.Status == "running" {
						return true
					}
				}
			}
		}
		time.Sleep(time.Second)
	}
	return false
}
