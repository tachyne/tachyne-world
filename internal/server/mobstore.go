package server

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
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
	mu     sync.Mutex
	path   string
	m      mobFile
	seeded map[[2]int32]bool // chunks already given their one-time chunk-generation herd
	bg     bgWriter          // the periodic save runs off the hub goroutine
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
	// VillagePlaced lists, per village well ("x,y,z"), the template entities
	// already placed (type@x,y,z), so each is placed once.
	VillagePlaced map[string][]string `json:"villagePlaced,omitempty"`
	// Mansions lists the (x,z) of woodland mansions already populated with
	// illagers, so a cleared mansion stays cleared across restarts.
	Mansions [][2]int `json:"mansions,omitempty"`
	// Bastions lists the (x,z) of bastion remnants already seeded with their
	// piglins and hoglins — a cleared bastion stays cleared.
	Bastions [][2]int `json:"bastions,omitempty"`
	// Huts lists the (x,z) of swamp huts already seeded with their witch and cat.
	Huts [][2]int `json:"huts,omitempty"`
	// EndCities lists the (x,z) of End cities already seeded with their
	// shulkers and the ship's elytra.
	EndCities [][2]int `json:"end_cities,omitempty"`
	// OceanRuins lists the (x,z) of ocean ruin sites already seeded with
	// their drowned — a cleared ruin stays cleared.
	OceanRuins [][2]int `json:"ocean_ruins,omitempty"`
	// Raids in progress (their raiders are ordinary saved mobs carrying Raid).
	Raids []savedRaid `json:"raids,omitempty"`
	// Seeded is the permanent set of chunks that have already received their
	// one-time vanilla chunk-generation herd. Persisted (was in-memory, reset
	// every restart) so a rollout never re-lays herds on a chunk whose animals
	// have since died or wandered — the unbounded-accumulation source.
	Seeded [][2]int32 `json:"seeded,omitempty"`
}

// savedMob is the flattened, scalar/packed twin of *mob (cf. savedStand). Item
// stacks ride through packStack (stackRow); the owner is a hex UUID string.
type savedMob struct {
	Etype   int     `json:"t" mig:"entity"`
	Dim     int     `json:"d,omitempty"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Yaw     float32 `json:"yaw,omitempty"`
	Health  int     `json:"hp"`
	Max     int     `json:"max,omitempty"`
	DmgFrac float64 `json:"df,omitempty"`

	Baby   bool `json:"baby,omitempty"`
	Jockey bool `json:"jockey,omitempty"` // a chicken that carried a jockey
	Trap   bool `json:"trap,omitempty"`   // a skeleton horse still armed as a trap
	// Mounts: the rider's vehicle by the eid it had when saved, relinked on
	// reload when both come back in the same chunk batch.
	EID         int32    `json:"eid,omitempty"`
	Mount       int32    `json:"mount,omitempty"`
	MountDrives bool     `json:"drives,omitempty"`
	GrowLeft    int      `json:"grow,omitempty"`
	LoveTicks   int      `json:"love,omitempty"`
	BreedCD     int      `json:"bcd,omitempty"`
	Sheared     bool     `json:"shear,omitempty"`
	Color       int8     `json:"color,omitempty"`   // sheep fleece colour
	Collar      int8     `json:"collar,omitempty"`  // pet collar dye
	Stew        int8     `json:"stew,omitempty"`    // brown mooshroom's stored stew flower
	CustomName  string   `json:"name,omitempty"`    // name tag; also makes the mob persistent
	Tags        []string `json:"tags,omitempty"`    // scoreboard tags (/tag)
	FromBucket  bool     `json:"bucket,omitempty"`  // released from a mob bucket: persistent
	Variant     int32    `json:"variant,omitempty"` // species variant + 1 (0 = unset; rows without one re-roll on load)
	Persistent  bool     `json:"persist,omitempty"` // picked up gear: persistenceRequired
	EggIn       int      `json:"egg,omitempty"`
	Size        int      `json:"size,omitempty"`

	Hostile       bool   `json:"host,omitempty"`
	Anger         int    `json:"anger,omitempty"`
	Neutral       bool   `json:"neut,omitempty"`
	PatrolCaptain bool   `json:"capt,omitempty"`
	CarriedBlk    uint32 `json:"eblk,omitempty" mig:"state"` // enderman: held block state

	Oxidation     int         `json:"ox,omitempty"`
	Waxed         bool        `json:"wax,omitempty"`
	Carrying      stackRow    `json:"carry,omitempty"`
	Trident       bool        `json:"tri,omitempty"`
	CanPickup     bool        `json:"pick,omitempty"`
	Gear          [4]stackRow `json:"gear,omitempty"`
	Saddled       bool        `json:"sad,omitempty"`
	SaddleSt      stackRow    `json:"sadst,omitempty"`
	ArmorSt       stackRow    `json:"armst,omitempty"`
	Chested       bool        `json:"chd,omitempty"`
	Chest         []stackRow  `json:"chest,omitempty"`
	Strength      int8        `json:"str,omitempty"`
	Held          int32       `json:"held,omitempty" mig:"item"`
	HSpeed        float64     `json:"hspeed,omitempty"`         // horse family: rolled MOVEMENT_SPEED base (per-step units)
	HJump         float64     `json:"hjump,omitempty"`          // horse family: rolled JUMP_STRENGTH base
	HasEgg        bool        `json:"has_egg,omitempty"`        // turtle: carrying an egg home
	Screaming     bool        `json:"screaming,omitempty"`      // goat: the screaming variant
	SoundSet      int8        `json:"soundset,omitempty"`       // wolf: its WolfSoundVariant (0 = classic)
	LastSlept     uint64      `json:"lastslept,omitempty"`      // villager: LAST_SLEPT (the iron-golem quorum)
	BreaksDoors   bool        `json:"breaks_doors,omitempty"`   // zombie: can break doors
	PoseTick      int64       `json:"pose_tick,omitempty"`      // camel: LastPoseTick
	RavStun       int         `json:"rav_stun,omitempty"`       // ravager: StunTick
	RavRoar       int         `json:"rav_roar,omitempty"`       // ravager: RoarTick
	Overworld     int         `json:"overworld,omitempty"`      // piglin/hoglin: TimeInOverworld
	ImmuneZombify bool        `json:"immune_zombify,omitempty"` // IsImmuneToZombification
	NoHunt        bool        `json:"no_hunt,omitempty"`        // piglin: CannotHunt; hoglin: CannotBeHunted
	TraderDespawn int         `json:"trader_despawn,omitempty"` // wandering trader / llama: DespawnDelay
	Lifetime      int         `json:"lifetime,omitempty"`       // endermite: Lifetime
	TadpoleAge    int         `json:"tadpole_age,omitempty"`    // tadpole: Age
	Trusted       []string    `json:"trusted,omitempty"`        // fox: trusted player names
	HornsGone     int8        `json:"horns_gone,omitempty"`     // goat: horns rammed off
	HeldSt        stackRow    `json:"held_st,omitempty"`        // the held item with its enchantments (when it has any)
	GearSure      [5]bool     `json:"gear_sure,omitempty"`      // guaranteed drops per slot (picked-up gear)
	Carry         stackRow    `json:"allay_carry,omitempty"`    // allay: collected stack
	DupCD         int         `json:"dupcd,omitempty"`          // allay: duplication cooldown
	SniffCD       int         `json:"sniffcd,omitempty"`        // sniffer: ticks until the next dig
	Hoard         []stackRow  `json:"hoard,omitempty"`          // piglin: the gold it keeps (its off-hand item folded in)
	Harness       int32       `json:"harn,omitempty" mig:"item"`
	// A lead tied to a FENCE survives a restart; one held by a player does not,
	// because the leash drops the moment its holder disconnects (vanilla's
	// tickLeash gives up as soon as the two cannot interact). So the only thing
	// worth storing is the knot's block, and the knot itself is rebuilt from
	// whichever mobs still name it — exactly as vanilla discards a knot with
	// nothing tied to it.
	LeashPos *[3]int `json:"leash,omitempty"`

	// Sulfur cube: the swallowed block (nil = none), a lit fuse's ticks left
	// plus one (0 = unlit) and the fuse it was lit with, and the pickup timer.
	CubeBody    *stackRow `json:"cube,omitempty"`
	CubeFuse    int       `json:"cfuse,omitempty"`
	CubeMaxFuse int       `json:"cmaxfuse,omitempty"`
	CubePickup  int       `json:"cpick,omitempty"`

	Tamed     bool    `json:"tame,omitempty"`
	Sitting   bool    `json:"sit,omitempty"`
	OwnerUUID string  `json:"owner,omitempty"`
	OvrSpeed  float64 `json:"ovs,omitempty"`
	OvrDamage float64 `json:"ovd,omitempty"`

	// Villager merchant identity (v2.1). Offers are saved as FULL trades, not
	// table indices: a tier's listings are drawn at random and several of them
	// roll their result (an enchantment, a map, a dye), so re-deriving an
	// offer from the table would hand the villager different stock every load.
	Profession int          `json:"prof,omitempty"`
	Food       int          `json:"food,omitempty"` // villager: foodLevel
	TradeLevel int          `json:"tlvl,omitempty"`
	TradeXP    int          `json:"txp,omitempty"`
	Restocks   int          `json:"rst,omitempty"`  // villager: restocks taken today
	LastStock  uint64       `json:"lrst,omitempty"` // villager: tick of the last one
	Offers     []savedOffer `json:"offers,omitempty"`
	Raid       [3]int       `json:"raid,omitempty"`    // raider: the raid centre it belongs to
	RaidWave   int          `json:"rwave,omitempty"`   // raider: the wave it came with
	Restrict   [3]int       `json:"rhome,omitempty"`   // Mob home_pos (an elder guardian's)
	RestrictR  int          `json:"rhomer,omitempty"`  // …home_radius; 0 = none
	Gossip     gossipBook   `json:"gossip,omitempty"`  // villager: what it holds about each player
	Converting int          `json:"conv,omitempty"`    // zombie villager: cure ticks left
	Charged    bool         `json:"charged,omitempty"` // creeper: struck by lightning
	Curer      string       `json:"curer,omitempty"`   // zombie villager: who started the cure

	// Anchors: villager schedule sites + the golem/villager home.
	Home [3]int `json:"home,omitempty"`
	Bed  [3]int `json:"bed,omitempty"`
	Work [3]int `json:"work,omitempty"`
	Meet [3]int `json:"meet,omitempty"`
}

// savedOffer is one merchant offer. It started life as a flat int32 array —
// {inItem, inCount, outItem, outCount, maxUses, xp, uses, demand}, later
// extended with the second item cost and one packed enchantment — and an offer
// carrying several enchantments (a villager's EnchantedItemForEmeralds gear)
// no longer fits a fixed-width row. Offers are written as the named object
// below; UnmarshalJSON still accepts either legacy array, so existing worlds
// load unchanged.
type savedOffer struct {
	In      int32   `json:"i" mig:"item"`
	InN     int32   `json:"ic"`
	Out     int32   `json:"o" mig:"item"`
	OutN    int32   `json:"oc"`
	MaxUses int32   `json:"mu"`
	XP      int32   `json:"xp"`
	Uses    int32   `json:"u,omitempty"`
	Demand  int32   `json:"d,omitempty"`
	C2Item  int32   `json:"c2,omitempty" mig:"item"`
	C2N     int32   `json:"c2n,omitempty"`
	Ench    []int32 `json:"e,omitempty"`   // one id<<8|lvl per enchantment
	MapID   int32   `json:"m,omitempty"`   // a treasure map's map id
	Name    string  `json:"n,omitempty"`   // and the name it carries
	Color   int32   `json:"col,omitempty"` // dyed leather armour
	Stew    int8    `json:"st,omitempty"`  // a suspicious stew's effect row
	Potion  int8    `json:"po,omitempty"`  // a tipped arrow's potion
	Mult    int32   `json:"pm,omitempty"`  // priceMultiplier in hundredths
}

// UnmarshalJSON accepts the historical array form as well as the object one.
func (s *savedOffer) UnmarshalJSON(b []byte) error {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			var a [11]int32 // a shorter array leaves the tail zero
			if err := json.Unmarshal(b, &a); err != nil {
				return err
			}
			*s = savedOffer{In: a[0], InN: a[1], Out: a[2], OutN: a[3], MaxUses: a[4],
				XP: a[5], Uses: a[6], Demand: a[7], C2Item: a[8], C2N: a[9]}
			if a[10] != 0 {
				s.Ench = []int32{a[10]}
			}
			return nil
		}
		break
	}
	type plain savedOffer // shed this method, then decode the object
	return json.Unmarshal(b, (*plain)(s))
}

func packOffer(o mobOffer) savedOffer {
	t := o.trade
	s := savedOffer{In: t.inItem, InN: t.inCount, Out: t.outItem, OutN: t.outCount,
		MaxUses: t.maxUses, XP: t.xp, Uses: o.uses, Demand: o.demand,
		C2Item: o.cost2Item, C2N: o.cost2Count, MapID: o.outMapID, Name: o.outName,
		Color: o.outColor, Stew: o.outStew, Potion: o.outPotion, Mult: o.trade.mult100}
	for _, e := range o.outEnchs {
		if e.lvl == 0 {
			break
		}
		s.Ench = append(s.Ench, int32(uint8(e.id))<<8|int32(uint8(e.lvl)))
	}
	return s
}

func unpackOffer(s savedOffer) mobOffer {
	o := mobOffer{trade: vTrade{inItem: s.In, inCount: s.InN, outItem: s.Out,
		outCount: s.OutN, maxUses: s.MaxUses, xp: s.XP}, uses: s.Uses, demand: s.Demand,
		cost2Item: s.C2Item, cost2Count: s.C2N, outMapID: s.MapID, outName: s.Name,
		outColor: s.Color, outStew: s.Stew, outPotion: s.Potion}
	if o.trade.mult100 = s.Mult; o.trade.mult100 == 0 {
		o.trade.mult100 = defaultPriceMult100 // an offer stored before it was per-listing
	}
	for i, e := range s.Ench {
		if i >= len(o.outEnchs) || e == 0 {
			break
		}
		o.outEnchs[i] = enchApply{id: int8(e >> 8), lvl: int8(e & 0xff)}
	}
	return o
}

func packPos(p blockPos) [3]int   { return [3]int{p.x, p.y, p.z} }
func unpackPos(a [3]int) blockPos { return blockPos{a[0], a[1], a[2]} }

func mobChunkKey(cx, cz int32) string {
	return strconv.Itoa(int(cx)) + "," + strconv.Itoa(int(cz))
}

func newMobStore(path string) *mobStore {
	s := &mobStore{path: path}
	if path != "" {
		if err := loadStore(path, &s.m); err != nil {
			log.Fatal(err)
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
	s.seeded = make(map[[2]int32]bool, len(s.m.Seeded))
	for _, c := range s.m.Seeded {
		s.seeded[c] = true
	}
	return s
}

// seededSet returns a copy of the persisted seeded-chunk set for the hub to own
// (boot restore — the hub then marks + persists new ones through recordSeeded).
func (s *mobStore) seededSet() map[[2]int32]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[[2]int32]bool, len(s.seeded))
	for k := range s.seeded {
		out[k] = true
	}
	return out
}

// recordSeeded folds the hub's live seeded set into the persisted one (union,
// never shrinks — a chunk seeded once stays seeded forever). Called before flush.
func (s *mobStore) recordSeeded(set map[[2]int32]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seeded == nil {
		s.seeded = map[[2]int32]bool{}
	}
	for k := range set {
		s.seeded[k] = true
	}
}

// wipeWild is a one-time maintenance pass (behind -wipe-wild) that removes ALL
// naturally-spawned mobs — wild passives and hostiles — keeping only the
// village-tied and player-owned set (villagers, iron/snow golems, wandering
// traders, tamed pets). Every currently-populated chunk is marked permanently
// seeded so the vanilla chunk-generation pass never re-lays a herd there,
// leaving repopulation to the capped per-tick natural spawner alone. Returns the
// persisted-mob count before/after.
func (s *mobStore) wipeWild() (before, after int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seeded == nil {
		s.seeded = map[[2]int32]bool{}
	}
	for key, bucket := range s.m.Chunks {
		before += len(bucket)
		if cx, cz, ok := parseChunkKey(key); ok {
			s.seeded[[2]int32{int32(cx), int32(cz)}] = true // populated once — never re-seed
		}
		out := bucket[:0:0]
		for _, m := range bucket {
			if keepMob(m) {
				out = append(out, m)
			}
		}
		after += len(out)
		if len(out) == 0 {
			delete(s.m.Chunks, key)
		} else {
			s.m.Chunks[key] = out
		}
	}
	return before, after
}

// cullSpawnCows is a one-time maintenance pass (behind -cull-spawn-cows):
// it removes the wild members of one species within radius blocks of the
// origin in the overworld — what the boot-seeded herds left behind — keeping
// tamed and named ones. Idempotent.
func (s *mobStore) cullSpawnCows(etype, radius int) (before, after int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r2 := float64(radius * radius)
	for key, bucket := range s.m.Chunks {
		before += len(bucket)
		out := bucket[:0:0]
		for _, m := range bucket {
			if m.Etype == etype && m.Dim == 0 && !m.Tamed && m.CustomName == "" && m.X*m.X+m.Z*m.Z <= r2 {
				continue
			}
			out = append(out, m)
		}
		after += len(out)
		if len(out) == 0 {
			delete(s.m.Chunks, key)
		} else {
			s.m.Chunks[key] = out
		}
	}
	return before, after
}

// cullSpecies removes the WILD members of the named species from the saved
// mobs: untamed, unnamed, and not persistence-required (picked-up gear). It is
// the general form of cullSpawnCows — a one-time maintenance sweep for a
// species that has built up past what it should be, run once from a flag and
// then taken back out of the manifest.
//
// A culled mob simply stops existing, exactly as a despawn does: vanilla's
// Mob.checkDespawn discards without drops, so an enderman's carried block is
// lost the same way it would be if the mob had despawned on its own.
func (s *mobStore) cullSpecies(etypes map[int]bool) (before, after, removed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, bucket := range s.m.Chunks {
		before += len(bucket)
		out := bucket[:0:0]
		for _, m := range bucket {
			if etypes[m.Etype] && !m.Tamed && m.CustomName == "" && !m.Persistent {
				removed++
				continue
			}
			out = append(out, m)
		}
		after += len(out)
		if len(out) == 0 {
			delete(s.m.Chunks, key)
		} else {
			s.m.Chunks[key] = out
		}
	}
	return before, after, removed
}

// keepMob reports whether a saved mob survives the wild wipe: the village-tied
// and player-owned set (mirrors hub.spawnExempt — the vanilla persistence /
// MISC category that never despawns).
func keepMob(m savedMob) bool {
	switch m.Etype {
	case entityVillager, entityIronGolem, entitySnowGolem, entityWanderingTrader:
		return true
	}
	return m.Tamed
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
func (s *mobStore) recordVillages(done map[blockPos]bool, placed map[blockPos]map[string]bool) {
	vs := make([][3]int, 0, len(done))
	for w := range done {
		vs = append(vs, packPos(w))
	}
	pl := make(map[string][]string, len(placed))
	for w, keys := range placed {
		ks := make([]string, 0, len(keys))
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		pl[fmt.Sprintf("%d,%d,%d", w.x, w.y, w.z)] = ks
	}
	s.mu.Lock()
	s.m.Villages = vs
	s.m.VillagePlaced = pl
	s.mu.Unlock()
}

// villagePlaced returns the persisted placed-entity record (boot restore).
func (s *mobStore) villagePlaced() map[blockPos]map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[blockPos]map[string]bool{}
	for k, keys := range s.m.VillagePlaced {
		var w blockPos
		if _, err := fmt.Sscanf(k, "%d,%d,%d", &w.x, &w.y, &w.z); err != nil {
			continue
		}
		m := map[string]bool{}
		for _, key := range keys {
			m[key] = true
		}
		out[w] = m
	}
	return out
}

// villages returns the persisted populated-village wells (boot restore).
func (s *mobStore) villages() [][3]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Villages
}

// recordMansions snapshots the populated-mansion set for the next flush.
func (s *mobStore) recordMansions(done map[[2]int32]bool) {
	ms := make([][2]int, 0, len(done))
	for k := range done {
		ms = append(ms, [2]int{int(k[0]), int(k[1])})
	}
	s.mu.Lock()
	s.m.Mansions = ms
	s.mu.Unlock()
}

// mansions returns the persisted populated-mansion set (boot restore).
func (s *mobStore) mansions() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Mansions
}

// recordBastions snapshots the seeded-bastion set for the next flush.
func (s *mobStore) recordBastions(done map[[2]int32]bool) {
	bs := make([][2]int, 0, len(done))
	for k := range done {
		bs = append(bs, [2]int{int(k[0]), int(k[1])})
	}
	s.mu.Lock()
	s.m.Bastions = bs
	s.mu.Unlock()
}

func (s *mobStore) bastions() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Bastions
}

// recordHuts snapshots the seeded-swamp-hut set for the next flush.
func (s *mobStore) recordHuts(done map[[2]int32]bool) {
	hs := make([][2]int, 0, len(done))
	for k := range done {
		hs = append(hs, [2]int{int(k[0]), int(k[1])})
	}
	s.mu.Lock()
	s.m.Huts = hs
	s.mu.Unlock()
}

func (s *mobStore) huts() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Huts
}

// recordEndCities snapshots the seeded-End-city set for the next flush.
func (s *mobStore) recordEndCities(done map[[2]int32]bool) {
	cs := make([][2]int, 0, len(done))
	for k := range done {
		cs = append(cs, [2]int{int(k[0]), int(k[1])})
	}
	s.mu.Lock()
	s.m.EndCities = cs
	s.mu.Unlock()
}

func (s *mobStore) endCities() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.EndCities
}

// recordOceanRuins snapshots the seeded-ocean-ruin set for the next flush.
func (s *mobStore) recordOceanRuins(done map[[2]int32]bool) {
	rs := make([][2]int, 0, len(done))
	for k := range done {
		rs = append(rs, [2]int{int(k[0]), int(k[1])})
	}
	s.mu.Lock()
	s.m.OceanRuins = rs
	s.mu.Unlock()
}

func (s *mobStore) oceanRuins() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.OceanRuins
}

// removeNear deletes persisted mobs of the given types within radius r of
// (wx,wz) — used to clear a removed structure's stranded mobs. Returns the count.
func (s *mobStore) removeNear(wx, wz, r int, etypes ...int) int {
	want := map[int]bool{}
	for _, e := range etypes {
		want[e] = true
	}
	r2 := float64(r * r)
	removed := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, bucket := range s.m.Chunks {
		out := bucket[:0:0]
		for _, m := range bucket {
			if want[m.Etype] {
				if dx, dz := m.X-float64(wx), m.Z-float64(wz); dx*dx+dz*dz <= r2 {
					removed++
					continue
				}
			}
			out = append(out, m)
		}
		if len(out) == 0 {
			delete(s.m.Chunks, key)
		} else {
			s.m.Chunks[key] = out
		}
	}
	return removed
}

// cullAnimals is a one-time maintenance pass (behind -cull-animals) to undo the
// pre-fix herd-doubling, which multiplied EVERY persisted species and spread
// them across many chunks. It keeps wild mobs in only ~1/coverMod of chunks
// (coverage thinning — the doubling's wide spread is the real problem), and
// caps each species to capN in the chunks it keeps. Tamed mobs and villagers
// are always kept, everywhere. Returns the persisted-mob count before/after.
// Idempotent: re-running on an already-culled store makes no further change.
func (s *mobStore) cullAnimals(capN, coverMod int) (before, after int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, bucket := range s.m.Chunks {
		before += len(bucket)
		keep := true
		if coverMod > 1 {
			if cx, cz, ok := parseChunkKey(key); ok {
				keep = (((cx*31+cz)%coverMod)+coverMod)%coverMod == 0
			}
		}
		out := bucket[:0:0]
		perSpecies := map[int]int{}
		for _, m := range bucket {
			if m.Tamed || m.Etype == entityVillager {
				out = append(out, m) // never cull pets or merchants
				continue
			}
			if !keep {
				continue // a thinned chunk keeps no wild mobs
			}
			perSpecies[m.Etype]++
			if perSpecies[m.Etype] <= capN {
				out = append(out, m)
			}
		}
		after += len(out)
		if len(out) == 0 {
			delete(s.m.Chunks, key)
		} else {
			s.m.Chunks[key] = out
		}
	}
	return before, after
}

// parseChunkKey splits a "cx,cz" bucket key back into ints.
func parseChunkKey(key string) (cx, cz int, ok bool) {
	comma := strings.IndexByte(key, ',')
	if comma < 0 {
		return 0, 0, false
	}
	x, err1 := strconv.Atoi(key[:comma])
	z, err2 := strconv.Atoi(key[comma+1:])
	return x, z, err1 == nil && err2 == nil
}

// flush atomically writes the document (temp + rename), like every other store.
func (s *mobStore) flush() {
	s.bg.wait()
	s.writeNow()
}

// flushAsync is the thirty-second save; see containerStore.flushAsync.
func (s *mobStore) flushAsync() { s.bg.run(s.writeNow) }

func (s *mobStore) writeNow() {
	s.mu.Lock()
	s.m.Seeded = s.m.Seeded[:0]
	for k := range s.seeded {
		s.m.Seeded = append(s.m.Seeded, k)
	}
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return
	}
	writeStore(path, data)
}

// toSavedMob flattens a live mob into its persisted row.
func toSavedMob(m *mob) savedMob {
	sm := savedMob{
		Etype: m.etype, Dim: m.dim, X: m.x, Y: m.y, Z: m.z, Yaw: m.yaw,
		Health: m.health, Max: m.maxHP(), DmgFrac: m.dmgFrac,
		Baby: m.baby, Jockey: m.jockey, Trap: m.trap, EID: m.eid, Mount: m.mount, MountDrives: m.mountDrives, GrowLeft: m.growLeft, LoveTicks: m.loveTicks, BreedCD: m.breedCD,
		Sheared: m.sheared, EggIn: m.eggIn, Size: m.size,
		Color: m.color, Collar: m.collar, Stew: m.stew, CustomName: m.customName, Tags: sortedTags(m.tags), FromBucket: m.fromBucket, Variant: packVariant(m),
		Persistent: m.persistent,
		Hostile:    m.hostile, Anger: m.anger, Neutral: m.neutral, PatrolCaptain: m.patrolCaptain,
		CarriedBlk: m.carriedBlock,
		Oxidation:  m.oxidation, Waxed: m.waxed, Carrying: packStack(m.carrying),
		Trident: m.trident, CanPickup: m.canPickup,
		Saddled: m.saddled, SaddleSt: packStack(m.saddleSt), ArmorSt: packStack(m.armorSt),
		Chested: m.chested, Strength: m.strength, Held: m.held, Harness: m.harness,
		Carry: packStack(m.carry), DupCD: m.dupCD, SniffCD: m.sniffCD, Hoard: packHoard(m),
		LeashPos: leashSavePos(m),
		Tamed:    m.tamed, Sitting: m.sitting, OvrSpeed: m.ovrSpeed, OvrDamage: m.ovrDamage, HasEgg: m.hasEgg, Screaming: m.screaming, SoundSet: m.soundSet, LastSlept: m.lastSlept, HornsGone: m.hornsGone, BreaksDoors: m.breaksDoors, PoseTick: m.poseTick, RavStun: m.ravStunTick, RavRoar: m.ravRoarTick, Overworld: m.overworldTicks, ImmuneZombify: m.immuneZombify, NoHunt: m.noHunt, TraderDespawn: m.traderDespawn, Lifetime: m.endermiteLife, TadpoleAge: m.tadpoleAge, Trusted: trustedList(m),
	}
	for i := range m.gear {
		sm.Gear[i] = packStack(m.gear[i])
	}
	if m.etype == entitySulfurCube {
		if m.cube.body.item != 0 {
			r := packStack(m.cube.body)
			sm.CubeBody = &r
		}
		if m.cube.lit {
			sm.CubeFuse, sm.CubeMaxFuse = m.cube.fuse+1, m.cube.maxFuse
		}
		sm.CubePickup = m.cube.pickup
	}
	if m.held != 0 {
		sm.HeldSt = packStack(m.heldStack()) // its enchantments, wear and count
	}
	sm.GearSure = m.gearSure
	if horseFamily(m.etype) {
		sm.HSpeed, sm.HJump = m.mobAttrs().Get(attr.MovementSpeed).Base(), m.jumpStrength()
	}
	for _, c := range m.chest {
		sm.Chest = append(sm.Chest, packStack(c))
	}
	if m.tamed {
		sm.OwnerUUID = hex.EncodeToString(m.ownerUUID[:])
	}
	sm.Profession, sm.TradeLevel, sm.TradeXP = m.profession, m.tradeLevel, m.tradeXP
	sm.Restocks, sm.LastStock = m.restocksToday, m.lastRestockTick
	sm.Food = m.vFood
	sm.Converting, sm.Curer = m.converting, m.curer
	sm.Charged = m.charged
	if len(m.gossip) > 0 {
		sm.Gossip = gossipBook{}
		for k, v := range m.gossip {
			sm.Gossip[k] = v
		}
	}
	sm.Raid = packPos(m.raidCenter)
	sm.RaidWave = m.raidWave
	if m.homeR > 0 {
		sm.Restrict, sm.RestrictR = [3]int{m.homePos.x, m.homePos.y, m.homePos.z}, m.homeR
	}
	for _, o := range m.offers {
		sm.Offers = append(sm.Offers, packOffer(o))
	}
	sm.Home, sm.Bed, sm.Work, sm.Meet = packPos(m.home), packPos(m.bed), packPos(m.work), packPos(m.meet)
	return sm
}

// packHoard is a piglin's kept gold plus whatever it was admiring.
func packHoard(m *mob) []stackRow {
	var out []stackRow
	for _, st := range m.hoard {
		out = append(out, packStack(st))
	}
	if m.offhand.item != 0 {
		out = append(out, packStack(m.offhand))
	}
	return out
}
