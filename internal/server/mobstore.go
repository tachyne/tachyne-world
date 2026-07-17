package server

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"sync"
)

// Mob persistence: live mobs are saved to mobs.json and reconstructed at boot so
// entities survive a pod restart. v2 keys the store BY CHUNK and loads/unloads
// mobs with their chunk (vanilla's chunk-entity model): a mob whose chunk leaves
// every player's range is written to its chunk bucket and dropped from the live
// set, and reloaded when the chunk comes back — so the live, ticking set stays
// bounded by the loaded area instead of growing with everything ever explored.
//
// Only per-instance mutable state is stored; everything static (species speed,
// base health, sounds, archetype/behaviour) re-derives from etype on load.
//
// Scope: all non-dying, pod-owned mobs EXCEPT the bosses (ender dragon,
// wither) and LLM NPCs (villager-bodied, but owned by the npc registry).
// Villagers persist as of v2.1 — profession, merchant tier/XP and their
// exact offer list (with per-offer uses) ride along, plus schedule anchors;
// populated villages are marked so a restart never double-populates.
// Persisted eids are meaningless across boots — a fresh eid is minted on
// load, the uuid re-derived, and a pet's owner is stored by player UUID and
// re-resolved to a live eid when that player joins.

type mobStore struct {
	mu   sync.Mutex
	path string
	m    mobFile
}

type mobFile struct {
	// Chunks buckets saved mobs by "cx,cz". A loaded chunk has NO entry here
	// (its mobs are live); an unloaded chunk holds the mobs waiting to reload.
	Chunks map[string][]savedMob `json:"chunks,omitempty"`
	// Mobs is the v1 flat format — read once and migrated into Chunks on load.
	Mobs []savedMob `json:"mobs,omitempty"`
	// Villages lists the wells of villages already populated, so a restart
	// does not spawn a second population on top of the reloaded one.
	Villages [][3]int `json:"villages,omitempty"`
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

	// Villager merchant identity (v2.1). Offers are saved as FULL trades, not
	// table indices — the live unlock rotation keys off the eid, which is
	// reminted every load, so re-rolling would shuffle a villager's stock.
	Profession int          `json:"prof,omitempty"`
	TradeLevel int          `json:"tlvl,omitempty"`
	TradeXP    int          `json:"txp,omitempty"`
	Offers     []savedOffer `json:"offers,omitempty"`

	// Anchors: villager schedule sites + the golem/villager home.
	Home [3]int `json:"home,omitempty"`
	Bed  [3]int `json:"bed,omitempty"`
	Work [3]int `json:"work,omitempty"`
	Meet [3]int `json:"meet,omitempty"`
}

// savedOffer is one merchant offer flattened:
// {inItem, inCount, outItem, outCount, maxUses, xp, uses}.
type savedOffer [7]int32

func packOffer(o mobOffer) savedOffer {
	t := o.trade
	return savedOffer{t.inItem, t.inCount, t.outItem, t.outCount, t.maxUses, t.xp, o.uses}
}

func unpackOffer(s savedOffer) mobOffer {
	return mobOffer{trade: vTrade{inItem: s[0], inCount: s[1], outItem: s[2],
		outCount: s[3], maxUses: s[4], xp: s[5]}, uses: s[6]}
}

func packPos(p blockPos) [3]int   { return [3]int{p.x, p.y, p.z} }
func unpackPos(a [3]int) blockPos { return blockPos{a[0], a[1], a[2]} }

func mobChunkKey(cx, cz int32) string {
	return strconv.Itoa(int(cx)) + "," + strconv.Itoa(int(cz))
}

func newMobStore(path string) *mobStore {
	s := &mobStore{path: path}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	if s.m.Chunks == nil {
		s.m.Chunks = map[string][]savedMob{}
	}
	// Migrate the v1 flat list into per-chunk buckets by saved position.
	for _, sm := range s.m.Mobs {
		k := mobChunkKey(int32(chunkFloor(sm.X)), int32(chunkFloor(sm.Z)))
		s.m.Chunks[k] = append(s.m.Chunks[k], sm)
	}
	s.m.Mobs = nil
	return s
}

// take returns and removes a chunk's saved mobs (called when the chunk reloads).
func (s *mobStore) take(cx, cz int32) []savedMob {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := mobChunkKey(cx, cz)
	mobs := s.m.Chunks[k]
	delete(s.m.Chunks, k)
	return mobs
}

// has reports whether a chunk currently holds saved (unloaded) mobs.
func (s *mobStore) has(cx, cz int32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m.Chunks[mobChunkKey(cx, cz)]) > 0
}

// stash writes a chunk's saved mobs (called when the chunk unloads); an empty
// slice clears the bucket.
func (s *mobStore) stash(cx, cz int32, mobs []savedMob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m.Chunks == nil {
		s.m.Chunks = map[string][]savedMob{}
	}
	k := mobChunkKey(cx, cz)
	if len(mobs) == 0 {
		delete(s.m.Chunks, k)
		return
	}
	s.m.Chunks[k] = mobs
}

// bucketLive snapshots the currently-live mobs into their chunk buckets (the
// autosave / shutdown crash-window save). Chunks in `active` that hold no live
// mob are cleared, so a loaded-then-emptied chunk never resurrects dead mobs;
// unloaded chunks (not in `active`) keep the buckets stash() already wrote.
func (s *mobStore) bucketLive(mobs map[int32]*mob, keep func(*mob) bool, active map[[2]int32]bool) {
	live := map[string][]savedMob{}
	for _, m := range mobs {
		if keep(m) {
			k := mobChunkKey(int32(chunkFloor(m.x)), int32(chunkFloor(m.z)))
			live[k] = append(live[k], toSavedMob(m))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m.Chunks == nil {
		s.m.Chunks = map[string][]savedMob{}
	}
	for k, v := range live {
		s.m.Chunks[k] = v
	}
	for c := range active {
		k := mobChunkKey(c[0], c[1])
		if _, ok := live[k]; !ok {
			delete(s.m.Chunks, k)
		}
	}
}

// recordVillages snapshots the populated-village set for the next flush.
func (s *mobStore) recordVillages(done map[blockPos]bool) {
	vs := make([][3]int, 0, len(done))
	for w := range done {
		vs = append(vs, packPos(w))
	}
	s.mu.Lock()
	s.m.Villages = vs
	s.mu.Unlock()
}

// villages returns the persisted populated-village wells (boot restore).
func (s *mobStore) villages() [][3]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Villages
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
	sm.Profession, sm.TradeLevel, sm.TradeXP = m.profession, m.tradeLevel, m.tradeXP
	for _, o := range m.offers {
		sm.Offers = append(sm.Offers, packOffer(o))
	}
	sm.Home, sm.Bed, sm.Work, sm.Meet = packPos(m.home), packPos(m.bed), packPos(m.work), packPos(m.meet)
	return sm
}
