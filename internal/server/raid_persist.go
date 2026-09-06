package server

// Raid persistence: an active raid is saved with the mobs (its raiders carry
// the raid centre) and picked up again on boot, mid-wave.

type savedRaid struct {
	Center    [3]int `json:"center"`
	Omen      int    `json:"omen,omitempty"`
	BonusDone bool   `json:"bonus_done,omitempty"`
	Wave      int    `json:"wave"`
	NumGroups int    `json:"groups"`
	Spawned   int    `json:"spawned,omitempty"`
}

// recordRaids snapshots the active raids for the next flush.
func (s *mobStore) recordRaids(raids map[blockPos]*raid) {
	out := make([]savedRaid, 0, len(raids))
	for _, r := range raids {
		out = append(out, savedRaid{Center: packPos(r.center), Omen: r.omenLevel, BonusDone: r.bonusDone,
			Wave: r.wave, NumGroups: r.numGroups, Spawned: r.waveSpawned})
	}
	s.mu.Lock()
	s.m.Raids = out
	s.mu.Unlock()
}

func (s *mobStore) raids() []savedRaid {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Raids
}

// restoreRaids rebuilds the raids after the mobs are back: each raid's alive
// set is every loaded raider that carries its centre.
func (h *hub) restoreRaids(saved []savedRaid) {
	for _, sr := range saved {
		center := unpackPos(sr.Center)
		if center == (blockPos{}) || h.raids[center] != nil {
			continue
		}
		r := &raid{center: center, omenLevel: max(1, sr.Omen), bonusDone: sr.BonusDone, wave: sr.Wave,
			numGroups: sr.NumGroups, waveSpawned: sr.Spawned, alive: map[int32]bool{}, shown: map[int32]bool{}}
		if r.numGroups == 0 {
			r.numGroups = raidWaveCount(h.rules.Difficulty)
		}
		r.uuid = raidUUID(center)
		for _, m := range h.mobs {
			if m.raidCenter == center {
				r.alive[m.eid] = true
			}
		}
		// Raiders reload with their chunks; until they do the raid holds
		// its wave rather than declaring it cleared.
		if h.mobstore != nil {
			r.pending = h.mobstore.countRaiders(sr.Center) - len(r.alive)
		}
		h.raids[center] = r
	}
}

// countRaiders counts the saved mobs that belong to a raid centre.
func (s *mobStore) countRaiders(center [3]int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.m.Mobs {
		if s.m.Mobs[i].Raid == center {
			n++
		}
	}
	return n
}

// raiderReloaded re-attaches a reloaded raider to its raid.
func (h *hub) raiderReloaded(m *mob) {
	if m.raidCenter == (blockPos{}) {
		return
	}
	if r := h.raids[m.raidCenter]; r != nil {
		r.alive[m.eid] = true
		if r.pending > 0 {
			r.pending--
		}
	}
}
