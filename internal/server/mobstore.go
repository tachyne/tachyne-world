package server

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
)

// Mob persistence: live mobs are snapshotted to mobs.json on the same autosave
// + SIGTERM cadence as containers/inventories, and reconstructed at boot so
// entities survive a pod restart (herds, farm animals, tamed pets) instead of
// vanishing. Only per-instance mutable state is stored — everything static
// (species speed, base health, sounds, archetype/behaviour) is re-derived from
// etype on load via the normal spawn paths.
//
// v1 scope: all non-dying, pod-owned mobs EXCEPT villagers (their trade state
// is deferred), the ender dragon and the wither (bosses), and LLM NPCs (a
// separate registry). Persisted eids are meaningless across boots — a fresh eid
// is minted on load, the uuid is re-derived from it, and a pet's owner is stored
// by player UUID and re-resolved to a live eid when that player joins.

type mobStore struct {
	mu   sync.Mutex
	path string
	m    mobFile
}

type mobFile struct {
	Mobs []savedMob `json:"mobs,omitempty"`
}

// savedMob is the flattened, scalar/packed twin of *mob (cf. savedStand). Item
// stacks ride through packStack ([13]int32); the owner is a hex UUID string.
type savedMob struct {
	Etype   int     `json:"t"`
	Dim     int     `json:"d,omitempty"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Yaw     float32 `json:"yaw,omitempty"`
	Health  int     `json:"hp"`
	Max     int     `json:"max,omitempty"`
	DmgFrac float64 `json:"df,omitempty"`

	Baby      bool `json:"baby,omitempty"`
	GrowLeft  int  `json:"grow,omitempty"`
	LoveTicks int  `json:"love,omitempty"`
	BreedCD   int  `json:"bcd,omitempty"`
	Sheared   bool `json:"shear,omitempty"`
	EggIn     int  `json:"egg,omitempty"`
	Size      int  `json:"size,omitempty"`

	Hostile       bool `json:"host,omitempty"`
	Anger         int  `json:"anger,omitempty"`
	Neutral       bool `json:"neut,omitempty"`
	PatrolCaptain bool `json:"capt,omitempty"`

	Oxidation int          `json:"ox,omitempty"`
	Waxed     bool         `json:"wax,omitempty"`
	Carrying  [13]int32    `json:"carry,omitempty"`
	Trident   bool         `json:"tri,omitempty"`
	CanPickup bool         `json:"pick,omitempty"`
	Gear      [4][13]int32 `json:"gear,omitempty"`
	Saddled   bool         `json:"sad,omitempty"`
	SaddleSt  [13]int32    `json:"sadst,omitempty"`
	ArmorSt   [13]int32    `json:"armst,omitempty"`
	Chested   bool         `json:"chd,omitempty"`
	Chest     [][13]int32  `json:"chest,omitempty"`
	Strength  int8         `json:"str,omitempty"`
	Held      int32        `json:"held,omitempty"`
	Harness   int32        `json:"harn,omitempty"`

	Tamed     bool    `json:"tame,omitempty"`
	Sitting   bool    `json:"sit,omitempty"`
	OwnerUUID string  `json:"owner,omitempty"`
	OvrSpeed  float64 `json:"ovs,omitempty"`
	OvrDamage float64 `json:"ovd,omitempty"`
}

func newMobStore(path string) *mobStore {
	s := &mobStore{path: path}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

// recordMobs snapshots the live mobs a predicate keeps into the in-memory
// document (no disk write — flush does that).
func (s *mobStore) recordMobs(mobs map[int32]*mob, keep func(*mob) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Mobs = s.m.Mobs[:0]
	for _, m := range mobs {
		if keep(m) {
			s.m.Mobs = append(s.m.Mobs, toSavedMob(m))
		}
	}
}

// saved returns the loaded rows for reconstruction (copied under the lock).
func (s *mobStore) saved() []savedMob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]savedMob, len(s.m.Mobs))
	copy(out, s.m.Mobs)
	return out
}

// flush atomically writes the document (temp + rename), like every other store.
func (s *mobStore) flush() {
	s.mu.Lock()
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, path)
	}
}

// toSavedMob flattens a live mob into its persisted row.
func toSavedMob(m *mob) savedMob {
	sm := savedMob{
		Etype: m.etype, Dim: m.dim, X: m.x, Y: m.y, Z: m.z, Yaw: m.yaw,
		Health: m.health, Max: m.maxHealth, DmgFrac: m.dmgFrac,
		Baby: m.baby, GrowLeft: m.growLeft, LoveTicks: m.loveTicks, BreedCD: m.breedCD,
		Sheared: m.sheared, EggIn: m.eggIn, Size: m.size,
		Hostile: m.hostile, Anger: m.anger, Neutral: m.neutral, PatrolCaptain: m.patrolCaptain,
		Oxidation: m.oxidation, Waxed: m.waxed, Carrying: packStack(m.carrying),
		Trident: m.trident, CanPickup: m.canPickup,
		Saddled: m.saddled, SaddleSt: packStack(m.saddleSt), ArmorSt: packStack(m.armorSt),
		Chested: m.chested, Strength: m.strength, Held: m.held, Harness: m.harness,
		Tamed: m.tamed, Sitting: m.sitting, OvrSpeed: m.ovrSpeed, OvrDamage: m.ovrDamage,
	}
	for i := range m.gear {
		sm.Gear[i] = packStack(m.gear[i])
	}
	for _, c := range m.chest {
		sm.Chest = append(sm.Chest, packStack(c))
	}
	if m.tamed {
		sm.OwnerUUID = hex.EncodeToString(m.ownerUUID[:])
	}
	return sm
}
