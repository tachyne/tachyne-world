package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// /daemon — in-game control of the tachyne-daemon manager over the bus.
// The engine forwards to mc.daemon.* (request-reply) and prints the answer;
// it never builds or runs daemon code itself. Op-only: installing a daemon
// executes fetched code on the manager's host.
//
//	/daemon list
//	/daemon install <module[@version]> [args…]
//	/daemon uninstall <name>
//	/daemon restart <name>       (unpinned daemons rebuild = hot-reload)
func (s *Server) cmdDaemon(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) == 0 {
		p.tell("Usage: /daemon <list|install <module[@ver]> [args…]|uninstall <name>|restart <name>>")
		return
	}
	var (
		op      = args[0]
		payload any
	)
	switch op {
	case "list":
		payload = map[string]any{}
	case "install":
		if len(args) < 2 {
			p.tell("Usage: /daemon install <module[@version]> [args…]")
			return
		}
		module, version := args[1], ""
		if i := strings.Index(module, "@"); i >= 0 {
			module, version = module[:i], module[i+1:]
		}
		payload = map[string]any{"module": module, "version": version, "args": args[2:]}
	case "uninstall", "restart":
		if len(args) != 2 {
			p.tell(fmt.Sprintf("Usage: /daemon %s <name>", op))
			return
		}
		payload = map[string]any{"name": args[1]}
	default:
		p.tell("Usage: /daemon <list|install|uninstall|restart>")
		return
	}

	raw, err := s.hub.bus.request("mc.daemon."+op, payload)
	if err != nil {
		p.tell("Daemon manager unreachable: " + err.Error())
		return
	}
	var r struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Data  struct {
			Daemons []struct {
				Name     string `json:"name"`
				Module   string `json:"module"`
				Version  string `json:"version"`
				Status   string `json:"status"`
				Restarts int    `json:"restarts"`
			} `json:"daemons"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		p.tell("Daemon manager replied garbage: " + err.Error())
		return
	}
	if !r.OK {
		p.tell("Daemon manager: " + r.Error)
		return
	}
	if op != "list" {
		p.tell(fmt.Sprintf("Daemon %s: done.", op))
		return
	}
	if len(r.Data.Daemons) == 0 {
		p.tell("No daemons installed.")
		return
	}
	for _, d := range r.Data.Daemons {
		v := d.Version
		if v == "" {
			v = "latest"
		}
		p.tell(fmt.Sprintf("%s — %s@%s [%s, %d restarts]", d.Name, d.Module, v, d.Status, d.Restarts))
	}
}
