package main

// Compiled-in plugins: blank-import a plugin package here and it registers
// itself (plugin.Register from its init). This file is the single place the
// server binary selects its plugin set — the Caddy/Dragonfly model. Plugins
// stay inert unless configured; see docs/PLUGINS.md.
import (
	_ "github.com/tachyne/tachyne-world/plugins/example"
)
