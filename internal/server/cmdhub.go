package server

// evHubCmd carries the hub half of a session-side command: the closure runs
// on the hub goroutine, in event order, with the live player map, so a
// command can read and change hub-owned state and answer through cmdOK /
// cmdInfo without an event type of its own.
type evHubCmd struct {
	fn func(players map[int32]*tracked)
}

func (evHubCmd) isHubEvent() {}

// onHub posts fn to run on the hub goroutine.
func (s *Server) onHub(fn func(players map[int32]*tracked)) {
	s.hub.post(evHubCmd{fn: fn})
}

// cmdFail is a command's failure line: never suppressed by the feedback
// rules, and only its caller sees it.
func cmdFail(p *player, text string) {
	if p != nil {
		p.trySendEv(chatEv(text))
	}
}
