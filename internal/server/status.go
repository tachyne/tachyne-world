package server

import (
	"fmt"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The server-list roster. A gateway cannot answer this from its own books:
// it holds only the clients pinned to its own protocol range, and a client
// pings whichever gateway matches its version — so the world is asked, over
// the attach protocol, and the answer is the same roster /list reads.
//
// statusMaxPlayers is a display figure: nothing here caps a join. Sharded,
// the count has to span pods, and SHARDING.md §5.2 already settles where it
// comes from — the shared player store (`home_sid >= 0`), the same source
// /list is specified to use — not a fan-out across the peer mesh, which only
// reaches neighbours within the awareness radius.
const (
	statusMaxPlayers  = 100
	statusSampleLimit = 12 // MinecraftServer.MAX_STATUS_PLAYER_SAMPLE
)

// statusRoster answers a gateway's status query. A hub too busy to answer
// reports an empty roster rather than holding the ping open — a server-list
// entry that will not draw is worse than a stale count.
func (s *Server) statusRoster() attachproto.Status {
	st := attachproto.Status{Max: statusMaxPlayers}
	s.hub.runOnHub(func() {
		st.Online = len(s.hub.playersRef)
		// buildPlayerStatus: up to twelve names, taken as a window starting
		// at a random offset so a big server shows a different dozen each
		// time rather than always the same head of the list.
		n := st.Online
		if n > statusSampleLimit {
			n = statusSampleLimit
		}
		start := 0
		if st.Online > n {
			start = s.hub.rng.Intn(st.Online - n + 1)
		}
		var i int
		for _, t := range s.hub.playersRef {
			if i >= start && len(st.Sample) < n {
				st.Sample = append(st.Sample, attachproto.StatusPlayer{
					Name: t.p.name, ID: dashedUUID(t.p.uuid)})
			}
			i++
		}
	})
	return st
}

// dashedUUID renders a raw uuid in the 8-4-4-4-12 form the status schema wants.
func dashedUUID(u [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
