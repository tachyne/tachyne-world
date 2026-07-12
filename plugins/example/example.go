// Package example is the reference tachyne plugin. It exercises every part
// of the plugin API — events (observe, cancel, mutate), commands, the
// scheduler, config, and the KV store — while staying inert until an
// operator configures it (all features default off).
//
// Enable it by compiling it in (cmd/server/plugins.go blank-imports it) and
// configuring plugins/example/config.json, e.g.:
//
//	{
//	  "greeting": "Welcome to the server, %s!",
//	  "protect_below_y": 40,
//	  "announce_minutes": 30,
//	  "announcement": "Tip: this server runs tachyne."
//	}
package example

import (
	"fmt"
	"strconv"

	"tachyne/plugin"
)

func init() { plugin.Register(&Example{}) }

type config struct {
	Greeting        string `json:"greeting"`         // join message; %s = player name ("" = off)
	ProtectBelowY   int    `json:"protect_below_y"`  // cancel non-op break/place below this Y (0 = off)
	AnnounceMinutes int    `json:"announce_minutes"` // repeating broadcast cadence (0 = off)
	Announcement    string `json:"announcement"`
}

type Example struct {
	cfg config
	srv plugin.Server
}

func (e *Example) Name() string { return "example" }

func (e *Example) Enable(ctx plugin.Context) error {
	if err := ctx.Config(&e.cfg); err != nil {
		return err
	}
	e.srv = ctx.Server()
	store := ctx.Store()
	logf := ctx.Logger().Printf

	// Join greeting + persistent per-player join counter (KV store).
	plugin.On(ctx.Events(), plugin.Monitor, false, func(ev *plugin.PlayerJoinEvent) {
		var joins int
		store.Get("joins:"+ev.Name, &joins)
		joins++
		store.Set("joins:"+ev.Name, joins)
		if e.cfg.Greeting == "" {
			return
		}
		if p, ok := e.srv.Player(ev.Name); ok {
			p.SendMessage(fmt.Sprintf(e.cfg.Greeting, ev.Name) +
				fmt.Sprintf(" (visit #%d)", joins))
		}
	})

	// Depth protection: cancel break/place below the configured Y for
	// non-ops — the classic region-protection hook, one dimension deep.
	if e.cfg.ProtectBelowY > 0 {
		plugin.On(ctx.Events(), plugin.Normal, true, func(ev *plugin.BlockBreakEvent) {
			if ev.Y < e.cfg.ProtectBelowY && !e.srv.IsOp(ev.Name) {
				ev.SetCancelled(true)
				if p, ok := e.srv.Player(ev.Name); ok {
					p.SendMessage(fmt.Sprintf("Blocks below y=%d are protected.", e.cfg.ProtectBelowY))
				}
			}
		})
		plugin.On(ctx.Events(), plugin.Normal, true, func(ev *plugin.BlockPlaceEvent) {
			if ev.Y < e.cfg.ProtectBelowY && !e.srv.IsOp(ev.Name) {
				ev.SetCancelled(true)
			}
		})
	}

	// Repeating announcement (scheduler demo).
	if e.cfg.AnnounceMinutes > 0 && e.cfg.Announcement != "" {
		ticks := e.cfg.AnnounceMinutes * 60 * 20
		ctx.Scheduler().Every(ticks, func() { e.srv.BroadcastMessage(e.cfg.Announcement) })
	}

	// /storm — thunderstorm now; /sun — clear skies + morning (weather+time).
	if err := ctx.RegisterCommand(plugin.Command{
		Name: "storm", Help: "start a thunderstorm", OpOnly: true,
		Run: func(c plugin.CommandContext) {
			c.Server().SetWeather("thunder", -1)
			c.Reply("Storm's coming.")
		},
	}); err != nil {
		return err
	}
	if err := ctx.RegisterCommand(plugin.Command{
		Name: "sun", Help: "clear skies and morning light", OpOnly: true,
		Run: func(c plugin.CommandContext) {
			c.Server().SetWeather("clear", -1)
			c.Server().SetTime(1000)
			c.Reply("Sky cleared.")
		},
	}); err != nil {
		return err
	}

	// /buff <damage> [radius] — creature-stat overlay on nearby mobs.
	if err := ctx.RegisterCommand(plugin.Command{
		Name: "buff", Usage: "<damage> [radius]", Help: "buff nearby mobs' melee damage", OpOnly: true,
		Run: func(c plugin.CommandContext) {
			args := c.Args()
			if len(args) == 0 {
				c.Reply("Usage: /buff <damage> [radius]")
				return
			}
			dmg, err := strconv.ParseFloat(args[0], 64)
			if err != nil || dmg <= 0 {
				c.Reply("Usage: /buff <damage> [radius]")
				return
			}
			radius := 16.0
			if len(args) > 1 {
				if r, err := strconv.ParseFloat(args[1], 64); err == nil && r > 0 {
					radius = r
				}
			}
			px, _, pz, pdim := c.Sender().Pos()
			n := 0
			for _, m := range c.Server().Mobs() {
				x, _, z, dim := m.Pos()
				if dim != pdim {
					continue
				}
				if dx, dz := x-px, z-pz; dx*dx+dz*dz > radius*radius {
					continue
				}
				m.SetMeleeDamage(dmg)
				m.SetMaxHealth(m.MaxHealth()*2, true)
				n++
			}
			c.Reply(fmt.Sprintf("Buffed %d mobs (damage %g, radius %g).", n, dmg, radius))
		},
	}); err != nil {
		return err
	}

	logf("enabled (protect<%d, greet=%v, announce=%dm)",
		e.cfg.ProtectBelowY, e.cfg.Greeting != "", e.cfg.AnnounceMinutes)
	return nil
}

func (e *Example) Disable() {}
