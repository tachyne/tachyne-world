package server

// mobUpdate runs one goal update of every mob: the mobGoalInterval ticks of
// updateMobs it covers. Each mob's goals run on one of those ticks and its
// body moves on every one — what a single updateMobs call did when the mob
// loop itself ran every other tick.
func (h *hub) mobUpdate(players map[int32]*tracked) {
	for i := 0; i < mobGoalInterval; i++ {
		h.updateMobs(players)
	}
}
