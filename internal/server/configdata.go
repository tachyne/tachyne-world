package server

// configdata.go is what the world adds to a client's configuration phase
// (attach ConfigData): its dimension table — vanilla's LevelStem list,
// which the login packet's level list, respawn, GlobalPos, the
// dimension_type and world_clock registries and a chunk's sky light all
// read on the gateway.

import (
	"encoding/json"

	"github.com/tachyne/tachyne-world/internal/world"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// dimensionTable is the world's dimension table as the gateways take it:
// every dimension the engine knows, by the id the attach protocol carries,
// with its level key, dimension_type entry (inline data for a type the
// client's pack lacks), sky light and clock.
func dimensionTable() []attachproto.DimensionInfo {
	out := make([]attachproto.DimensionInfo, 0, len(world.Dimensions))
	for i := range world.Dimensions {
		d := &world.Dimensions[i]
		di := attachproto.DimensionInfo{
			ID: int32(d.ID), Key: d.Key, Type: d.TypeKey(),
			SkyLight: d.HasSkyLight, Clock: d.Clock,
		}
		if len(d.TypeData) > 0 {
			di.TypeData = json.RawMessage(d.TypeData)
		}
		out = append(out, di)
	}
	return out
}

// configData is the configuration a session's client is given at login
// (Welcome.Config): the dimension table, and the installed pack load's tags
// so a client joining after a /reload has the tags everyone else was sent.
func (s *Server) configData() *attachproto.ConfigData {
	cd := currentConfigData()
	return &cd
}

// Dim makes a remote player an attach.Dimensioned: the dimension the
// player is in, which the session's Welcome carries. The session goroutine
// owns p.dim (it is the one that moves the player between dimensions).
func (r *remotePlayer) Dim() int32 { return int32(r.p.dim) }
