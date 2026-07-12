package main

import "github.com/tachyne/tachyne-world/servercmd"

// The stock world-pod binary: the engine entrypoint (servercmd) plus the
// plugin set selected in plugins.go. Custom binaries with third-party
// plugins are assembled the same way by cmd/tachyne-build.
func main() { servercmd.Main() }
