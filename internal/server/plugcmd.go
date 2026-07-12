package server

import (
	"sort"
	"strings"

	"github.com/tachyne/tachyne-world/plugin"
)

// Plugin command dispatch. handleCommand runs on the SESSION goroutine; a
// plugin command (or a PlayerCommandEvent listener) needs the hub, so the
// session posts evCommand and blocks on the verdict — the same block-on-hub
// shape as fireSync. Commands nobody registered take the legacy switch with
// zero new cost.

type cmdVerdict struct {
	handled bool   // a plugin consumed the command (ran it, or cancelled it)
	line    string // possibly rewritten by a PlayerCommandEvent handler
}

type evCommand struct {
	p     *player
	line  string
	reply chan cmdVerdict // buffered, cap 1
}

func (evCommand) isHubEvent() {}

// pluginCommand gives plugins first crack at a command line. Fast path: no
// command listeners and not a plugin command → the legacy switch proceeds
// untouched.
func (s *Server) pluginCommand(p *player, line, name string) cmdVerdict {
	host := s.hub.plugHost
	if host == nil {
		return cmdVerdict{line: line}
	}
	if !plugin.Has[*plugin.PlayerCommandEvent](s.hub.plugins) {
		if _, isPlug := host.cmds[name]; !isPlug {
			return cmdVerdict{line: line}
		}
	}
	r := make(chan cmdVerdict, 1)
	s.hub.post(evCommand{p: p, line: line, reply: r})
	return <-r
}

// runPluginCommand is the hub-side half: fire the (cancellable, rewritable)
// command event, then run a matching plugin command on the hub goroutine.
func (h *hub) runPluginCommand(p *player, line string) cmdVerdict {
	if plugin.Has[*plugin.PlayerCommandEvent](h.plugins) {
		ev := &plugin.PlayerCommandEvent{EID: p.eid, Name: p.name, Line: line}
		if !h.plugins.Fire(ev) {
			return cmdVerdict{handled: true, line: line}
		}
		line = ev.Line
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return cmdVerdict{handled: true, line: line}
	}
	cmd := h.plugHost.cmds[fields[0]]
	if cmd == nil {
		return cmdVerdict{line: line} // rewritten or not, the legacy switch takes it
	}
	if cmd.OpOnly && !h.plugHost.s.isOp(p.name) {
		p.tell("You don't have permission.") // parity with the built-in op gate
		return cmdVerdict{handled: true, line: line}
	}
	cmd.Run(cmdCtx{h: h, p: p, args: fields[1:]})
	return cmdVerdict{handled: true, line: line}
}

// cmdCtx implements plugin.CommandContext for a running command.
type cmdCtx struct {
	h    *hub
	p    *player
	args []string
}

func (c cmdCtx) Sender() plugin.Player { return playerHandle{c.h.plugHost, c.p.eid} }
func (c cmdCtx) Args() []string        { return c.args }
func (c cmdCtx) Reply(text string)     { c.p.trySendEv(chatEv(text)) }
func (c cmdCtx) Server() plugin.Server { return srvFacade{c.h.plugHost} }

// pluginCommandNames lists registered plugin command names (no aliases),
// sorted for /help.
func (ph *pluginHost) pluginCommandNames() []string {
	seen := map[string]bool{}
	var names []string
	for n, c := range ph.cmds {
		if n == c.Name && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// pluginHelp is the /help suffix for plugin commands.
func (ph *pluginHost) pluginHelp() string {
	names := ph.pluginCommandNames()
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" | Plugins:")
	for _, n := range names {
		c := ph.cmds[n]
		b.WriteString(" /" + n)
		if c.Usage != "" {
			b.WriteString(" " + c.Usage)
		}
	}
	return b.String()
}

// allCommandNames is every completable command (built-ins + plugin names +
// aliases) for the tab-completion tree.
func (ph *pluginHost) allCommandNames() []string {
	extra := make([]string, 0, len(ph.cmds))
	for n := range ph.cmds {
		extra = append(extra, n)
	}
	sort.Strings(extra)
	return extra
}
