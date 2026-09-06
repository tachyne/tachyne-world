package server

import (
	"encoding/binary"
	"github.com/tachyne/tachyne-world/internal/attribute"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	"github.com/tachyne/tachyne-world/plugin"
)

// Mobs are server-controlled entities — the foundation for the living world
// (and, later, LLM NPCs). They use the same Spawn Entity + relative-move packets
// as players; the hub owns them and steps their behaviour on the tick loop.
// This first cut gives each one a simple wander; flocking/herding/hunting
// primitives plug in where stepWander is called.

const (
	mobMoveInterval = 2 // step + broadcast mob movement every N ticks
	// Vanilla-measured duty cycle (oracle diff, MECHANICS "Vanilla oracle"):
	// unprovoked mobs IDLE most of the time and stroll briefly — passives drift
	// ~0.16 b/s perceived (≈15-20% moving), unaggroed hostiles ~0.05 b/s. Idle
	// is the default state; the stroll is the exception. Updates run at 10/s.
	restMin    = 80  // idle spell: 8-20 s…
	restMax    = 200 //
	strollMin  = 25  // …then a 2.5-4.5 s stroll, back to idle
	strollMax  = 45
	mobSpeed   = 0.09 // blocks per step (×10 = 0.9 blocks/sec grazing)
	panicTicks = 40   // flee steps after a hit (decremented per mob update ≈ 4s)
)

var (
	entityCow = entityID("cow") // minecraft:entity_type ordinals (1.21.5)
)

type mob struct {
	eid             int32
	etype           int
	behavior        Behavior // per-tick steering primitive (wander/herd/…)
	herd            int      // index into hub.herds — the goal a herd mob steers toward
	reroute         int      // ticks left committed to an escape heading after a block
	health          int      // hit points; mob dies and drops loot at 0
	dying           int      // ticks left in the death animation (0 = alive); despawns at 0
	panic           int      // ticks left fleeing after being hit
	kb              int      // knockback updates left (velocity decays, no steering/clamp)
	rest            int      // grazing pause: updates left standing still (passive idling)
	fleeX, fleeZ    float64  // the threat to flee away from while panicking
	hostile         bool     // hunts + attacks players (zombies) rather than grazing
	burns           bool     // undead: catches fire in daylight (zombie/skeleton)
	burning         bool     // on fire — rendered via entity flags (any ignite source)
	burnDelay       int      // seconds of dawn-ramp grace before this mob ignites
	fireSecs        int      // seconds of afterburn left (lava/fire/daylight); 1 HP/s, water clears
	submerged       int      // consecutive seconds fully underwater (land mobs drown past maxAir)
	convertIn       int      // zombie/husk: seconds left of the shaking conversion phase (0 = not converting)
	fuse            int      // creeper: ticks left on a lit fuse (0 = not ignited)
	anger           int      // spider: mob-updates it stays hostile in daylight after a hit
	jumpStrength    float64  // horse family: how high it jumps (vanilla 0.4-1.0)
	dragonPhase     int      // ender dragon: which phase of the fight it is in
	dragonPhaseTick int      // ticks spent in the current phase
	dragonFlames    int      // breaths taken this perch
	dragonCharge    int      // ticks of fireball aim built up
	fangNextAt      uint64   // evoker: tick its fang spell comes off cooldown
	vexNextAt       uint64   // evoker: tick its summon spell comes off cooldown
	vexLife         int      // summoned vex: ticks left before it expires (0 = unlimited)
	hitByPlayer     bool     // a player has hit it — its death pays XP (vanilla rule)
	lastAttacker    int32    // eid of the last entity that hurt it (plugin death event)
	looting         int      // killer's Looting level (stamped per hit, used at drop time)
	baby            bool     // ageable: half-size, grows up, no drops/XP
	growLeft        int      // ticks until a baby matures
	loveTicks       int      // courting window after love-food (hearts)
	lovedBy         int32    // who fed the love-food (advancement credit)
	breedCD         int      // ticks before this parent may breed again
	stroll          int      // wander spell: updates left walking before the next rest
	sheared         bool     // sheep: fleece off (regrows by grazing)
	color           int8     // sheep: fleece colour (0 white .. 15 black), dyeable
	customName      string   // name-tagged: shown above the mob, and it never despawns
	fromBucket      bool     // released from a mob bucket: persistent (Bucketable.setFromBucket)
	persistent      bool     // Mob.persistenceRequired: picked up gear (never despawns)
	variant         int8     // frog / axolotl colour (variant.go); meaningful when variantSet
	variantSet      bool
	eggIn           int        // chicken: ticks until the next egg
	beeNectar       bool       // bee: carrying nectar home (fills the hive on delivery)
	beePollinate    int        // bee: seconds left hovering at its flower
	beeGoal         blockPos   // bee: the flower or hive it flies toward
	beeGoalKind     int        // bee: beeGoalKindNone/Flower/Hive
	beeHome         blockPos   // bee: its hive (beeHasHome)
	beeHasHome      bool       //
	beeNoEnter      int        // bee: seconds before it may re-enter a hive
	beeStingDie     int        // bee: seconds left to live after stinging
	beeCropsGrown   int8       // bee: crops boosted since its last pollination (cap 10)
	beeSentFlags    uint8      // bee: last synced flags byte (pollen coat / lost stinger)
	beeSentAngry    bool       // bee: last synced anger state (red eyes)
	beeTravel       int        // bee: mob-updates spent on the current trip (give-up timer)
	beeNoNectar     int        // bee: seconds foraging empty-handed (ticksWithoutNectarSinceExitingHive)
	beeStayOut      int        // bee: seconds barred from the hive after a sedated robbery
	beeLocateCD     int        // bee: seconds until it may look for a hive again
	beeBanned       []blockPos // bee: hives it could not reach (MAX_BLACKLISTED_TARGETS)
	// A flier's route through the air, and how far along it is. Set by the
	// behaviour, walked by flyMove — the node's own y IS the altitude the mob
	// wants, which is what stops the hover spring fighting the errand.
	flyPath         []blockPos
	flyIdx          int
	flyGoal         blockPos
	flyStale        int            // mob-updates since the path was computed
	size            int            // slime: 4/2/1 (splits in half on death)
	neutral         bool           // enderman: peaceful until hit (anger flips it hostile)
	carriedBlock    uint32         // enderman: the block state it's holding (0 = none)
	sonicCD         int            // warden: mob-updates until the next sonic boom
	beamTarget      int32          // guardian: the player the beam is locked on (0 = none)
	hideUntil       uint64         // villager: heard a bell — stay at the bed until this tick
	beamTicks       int            // guardian: GuardianAttackGoal.attackTime, in ticks
	digClock        int            // warden: mob-updates with no target (digs away at the cap)
	patrolCaptain   bool           // pillager patrol leader (carries the ominous banner)
	raidCenter      blockPos       // raider: the raid this mob belongs to (zero = not a raider)
	idleSecs        int            // seconds spent >32 blocks from every player (despawn clock)
	reinf           float64        // zombie SPAWN_REINFORCEMENTS_CHANCE (0 for non-zombies)
	hopTicks        int            // slime: updates left mid-bound (traveling)
	hopDelay        int            // slime: updates until the next bound (grounded, still)
	strafeCW        bool           // skeleton: current circling direction while shooting
	retaliates      bool           // peaceful until hit, then hunts its attacker (wolf/goat)
	rider           int32          // player eid riding this mob (0 = none); AI pauses while ridden
	riders          []int32        // happy ghast: up to 4 rider eids (riders[0] pilots); AI pauses while any aboard
	mount           int32          // eid of the MOB this mob rides (raid ravager riders); 0 = none
	cart            int32          // eid of the MINECART carrying this mob (scooped up by a rolling cart); 0 = none
	mobRider        int32          // eid of the MOB riding this one (the reverse of mount); 0 = none
	harness         int32          // happy ghast: equipped harness item id (0 = none); gates riding
	oxidation       int            // copper golem: weather stage 0 unaffected → 3 oxidized
	oxidizeAt       uint64         // copper golem: tick of the next oxidation step
	waxed           bool           // copper golem: honeycombed → never oxidizes
	carrying        invStack       // copper golem: items in transit between chests
	sortGoal        blockPos       // copper golem: the container it's walking to
	sortHasGoal     bool           // copper golem: sortGoal is valid
	sortCD          int            // copper golem: ticks until the next transport
	trident         bool           // drowned: armed with a trident (throws it at range)
	canPickup       bool           // may pick up dropped gear (spawn-time roll)
	gear            [4]invStack    // worn armor by slot (0 head,1 chest,2 legs,3 feet)
	saddled         bool           // a saddle is on: this mob can be mounted
	saddleSt        invStack       // the saddle item (horse family; saddled mirrors it)
	armorSt         invStack       // body armor / llama carpet
	chested         bool           // donkey/mule/llama carrying a chest
	chest           []invStack     // chest contents (columns×3)
	strength        int8           // llama: chest columns (1-5)
	tamed           bool           // wolf/cat/parrot tamed to an owner
	sitting         bool           // tamed pet told to stay (right-click toggle)
	spawnInvuln     int            // wither: ticks of spawn-charge invulnerability left
	owner           int32          // owner player eid (0 = wild); pets follow this player
	ownerUUID       [16]byte       // owner's stable identity (persisted; owner eid is re-resolved on join)
	path            []pathPoint    // A* route toward the current goal (nil = steer straight)
	pathIdx         int            // index of the next waypoint to walk to
	pathGoal        [2]int         // block goal the current path was computed for
	pathAt          uint64         // tick the path was computed (staleness clock)
	usesDoors       bool           // villager: may plan through + open wooden doors
	roamX, roamZ    float64        // villager: current roam target (goal-directed wander)
	roamAt          uint64         // tick to pick a fresh roam target
	bed             blockPos       // villager: its bed (sleep anchor; zero = no schedule)
	work            blockPos       // villager: its profession workstation (day work site)
	meet            blockPos       // villager: the village meeting point (bell/well)
	sleeping        bool           // villager: lying in its bed through the night
	swims           bool           // water-bound: lives inside a water column (fish/squid)
	flies           bool           // free flight: no ground collision (bat/phantom/ghast)
	statik          bool           // anchored: never walks (shulker)
	climbing        bool           // spider: clinging to a wall right now (synced state)
	skittish        bool           // bolts from any close player (fox/ocelot/rabbit)
	hover           float64        // fliers: preferred altitude above the terrain
	held            int32          // rendered main-hand item (0 = empty)
	ty              float64        // hunted target's feet height (fliers dive to it)
	living                         // attributes + status effects, shared with players
	dmgFrac         float64        // fractional damage carry (vanilla HP is float, ours int)
	attackCD        int            // mob-updates left before this mob can melee again
	hasTarget       bool           // a player is within aggro range this update
	heartBound      bool           // creaking: a standing heart is keeping it alive
	heartHit        bool           // …and it took a blow the heart must answer for
	frozen          bool           // creaking: a player is watching, so it cannot move
	tx, tz          float64        // that target's position (set by acquireTarget)
	dim             int            // dimension this mob lives in (0 overworld, 1 nether)
	villagerTarget  int32          // zombie: the villager it hunts when no player is near (0 = none)
	converting      int            // zombie villager: ticks left in its cure (0 = not curing)
	curer           string         // zombie villager: who fed it the golden apple
	cureRep         map[string]int // villager: gratitude gossip toward its curers (per session)
	profession      int            // villager: index into professionNames/villagerTrades
	tradeLevel      int            // villager merchant tier 1-5 (novice..master)
	tradeXP         int            // trade experience toward the next tier
	offers          []mobOffer     // this villager's unlocked trades (+ per-offer uses)
	restocksToday   int            // villager: restocks done this day (vanilla ≤2/day)
	lastRestockTick uint64         // villager: tick of the last restock (2400-tick spacing gate)
	gossip          map[string]int // villager: per-player TRADING reputation (in-session)
	home            blockPos       // villager house / golem well — the anchor to drift back to

	ovrSpeed   float64 // >0: plugin speed override — survives behavior-driven speed resets
	ovrDamage  float64 // >0: plugin melee-damage override (hostileMelee honors it)
	uuid       [16]byte
	x, y, z    float64
	yaw        float32
	syaw       float32 // last broadcast head yaw (only resend on change)
	vx, vz     float64
	vy         float64  // vertical velocity (swimmers/fliers only)
	pushX      float64  // crowding shove (push.go), held apart from the steering
	pushZ      float64  // velocity so a shoved mob does not turn to face the shove
	cramCD     int      // mob-updates until this mob can take cramming damage again
	leash      int32    // eid holding this mob's lead (player or knot); 0 = free
	leashPos   blockPos // the fence knot's block, when the holder is one (persisted)
	sx, sy, sz float64  // last broadcast position (for delta moves)
}

// spawnMob creates a server-controlled entity and shows it to nearby players.
// It defaults to neutral wander; callers (or the bus) assign a group behavior.
// Returns nil when a plugin MobSpawnEvent handler cancels the spawn.
func (h *hub) spawnMob(players map[int32]*tracked, etype int, x, y, z float64) *mob {
	return h.spawnMobIn(players, etype, 0, x, y, z)
}

// spawnMobIn spawns into an explicit dimension (nether mobs). The reported
// cause is h.spawnCause, whose zero value is SpawnNatural — command/bus entry
// points scope it with withSpawnCause so deep helpers report correctly.
func (h *hub) spawnMobIn(players map[int32]*tracked, etype, dim int, x, y, z float64) *mob {
	return h.spawnMobCause(players, etype, dim, x, y, z, h.spawnCause)
}

// withSpawnCause runs fn with the given spawn reason in force (hub goroutine
// only — this is a plain field, not a lock).
func (h *hub) withSpawnCause(c plugin.SpawnReason, fn func()) {
	old := h.spawnCause
	h.spawnCause = c
	fn()
	h.spawnCause = old
}

// spawnMobCause is the single spawn choke point, carrying the plugin-visible
// spawn reason. The mob is registered BEFORE the event fires so a handler can
// fetch its handle and adjust stats; a cancel unregisters it silently.
func (h *hub) spawnMobCause(players map[int32]*tracked, etype, dim int, x, y, z float64, cause plugin.SpawnReason) *mob {
	eid := h.allocEID()
	m := &mob{living: living{attrs: newMobAttributes(etype)}, eid: eid, etype: etype, dim: dim, behavior: wanderBehavior{}, health: mobHealth(etype), x: x, y: y, z: z, sx: x, sy: y, sz: z}
	binary.BigEndian.PutUint32(m.uuid[12:], uint32(eid)) // unique enough for the client
	if etype == entitySheep {
		m.color = h.rollSheepColor() // vanilla's spread: mostly white, pink 1-in-600
	}
	h.rollVariant(m) // frog by biome, axolotl by the 1-in-1200 blue roll
	h.mobs[eid] = m
	h.gridDirty()

	if !h.reloading && plugin.Has[*plugin.MobSpawnEvent](h.plugins) {
		ev := &plugin.MobSpawnEvent{EID: eid, Type: etype, TypeName: entityNameByID[etype],
			X: x, Y: y, Z: z, Dim: dim, Reason: cause}
		if !h.plugins.Fire(ev) {
			delete(h.mobs, eid)
			h.gridDirty()
			return nil
		}
	}
	h.toNearbyEv(players, dim, x, z, entAdd(eid, etype, m.uuid, x, y, z, 0, 0))
	if vm := variantMeta(m); vm != nil {
		h.toNearbyEv(players, dim, x, z, metaEv(vm))
	}
	return m
}

// updateMobs steps each mob's behaviour and broadcasts its movement. The behavior
// decides the desired velocity; the hub applies the shared physics (momentum, speed
// cap, terrain collision) so every primitive moves consistently.
func (h *hub) updateMobs(players map[int32]*tracked) {
	h.updateHerdTargets()
	h.pushMobs(players) // crowding: mobs standing in one another shove apart
	for _, m := range h.mobs {
		if m == h.dragon {
			continue // the dragon flies on updateDragon's physics alone —
			//          shared gravity/ground-snap would pin it into the island
		}
		if m.dying > 0 { // playing the death animation — hold still, then despawn + drop
			if m.dying -= mobMoveInterval; m.dying <= 0 {
				h.despawnMob(players, m)
			}
			continue
		}
		if m.mount != 0 { // riding another mob (raid ravager rider)
			v := h.mobs[m.mount]
			if v == nil || v.dying > 0 {
				m.mount = 0 // vehicle gone — dismount and resume as a normal mob
			} else {
				// Glued to the vehicle: the client renders us seated from the
				// passengers frame, so the server just keeps us co-located and
				// skips independent movement/AI.
				m.x, m.y, m.z, m.dim = v.x, v.y+mountRideHeight, v.z, v.dim
				continue
			}
		}
		if m.cart != 0 { // aboard a minecart: carried until the cart breaks
			v := h.vehicles[m.cart]
			if v == nil {
				m.cart = 0
			} else {
				m.x, m.y, m.z, m.dim = v.x, v.y+cartRideHeight, v.z, v.dim
				continue
			}
		}
		if m.rider != 0 || len(m.riders) > 0 {
			continue // a ridden mount is client-driven (applyMountMove) — AI paused
		}
		if m.spawnInvuln > 0 {
			continue // wither charging its spawn: hold still until updateWithers releases it
		}
		if m.frozen {
			m.vx, m.vz = 0, 0
			continue // a watched creaking is a statue
		}
		if m.tamed { // a pet follows its owner (or sits)
			if h.petAcquire(players, m) {
				m.vx, m.vz = 0, 0 // sitting: stay put
				continue
			}
			if m.hasTarget {
				m.rest = 0
			}
		} else if m.hostile {
			h.acquireTarget(players, m) // pick a player to hunt this update
			if m.hasTarget {
				m.rest = 0 // a resting hostile wakes the instant prey appears
			}
		}
		if m.loveTicks > 0 {
			m.rest = 0 // courtship overrides idling (vanilla goal priority)
		}
		// Skittish species bolt from any close survival player (vanilla foxes/
		// ocelots avoid players outright — no hit needed).
		if m.skittish && m.panic == 0 && m.kb == 0 {
			if t := h.nearestHuntable(players, m.dim, m.x, m.z, 5); t != nil {
				m.panic, m.fleeX, m.fleeZ = panicTicks/2, t.x, t.z
			}
		}
		// Villagers run a daily schedule: at night they lie in their bed (held
		// still); by day they open the wooden door in their way BEFORE the step
		// below, so an open door (not a wall) is what the walk test sees this tick.
		if m.usesDoors {
			if m.bed != (blockPos{}) && h.villagerSleep(players, m) {
				continue // asleep in bed — no movement this tick
			}
			h.villagerDoors(players, m)
		}
		switch {
		case m.kb > 0:
			// Airborne from a hit: ride the impulse out (no steering, no speed
			// clamp — knockback is meant to exceed walking speed), decaying fast.
			m.kb--
			m.vx *= 0.6
			m.vz *= 0.6
		case m.etype == entitySlime || m.etype == entityMagmaCube:
			h.slimeHop(players, m) // hop-pause locomotion (vanilla SlimeMoveControl)
		case m.panic > 0:
			// Spooked: bolt directly away from the threat at PanicGoal's
			// vanilla 2.0× speed modifier (chickens flap off at 1.4×),
			// ignoring herd steering until the panic wears off.
			m.panic--
			flee := m.moveSpeed() * 2
			if m.etype == entityChicken {
				flee = m.moveSpeed() * 1.4
			}
			dx, dz := m.x-m.fleeX, m.z-m.fleeZ
			if d := math.Hypot(dx, dz); d > 1e-6 {
				m.vx, m.vz = dx/d*flee, dz/d*flee
			}
		case m.reroute > 0:
			// Committed to an escape heading (just after a block): keep it instead
			// of re-steering, so the mob walks away from an obstacle rather than
			// vibrating against it as cohesion keeps repulling it back.
			m.reroute--
		case m.rest > 0:
			// Idle — the DEFAULT state (vanilla-measured: mobs stand around
			// ~80-90% of the time). When the spell ends, arm the next stroll.
			m.rest--
			m.vx *= 0.6
			m.vz *= 0.6
			if m.rest == 0 {
				m.stroll = strollMin + h.rng.Intn(strollMax-strollMin)
			}
		default:
			// Stroll exhausted → back to idling — EXCEPT while hunting or
			// courting, which override the idle cycle (vanilla goal priority:
			// chase/breed goals outrank RandomStroll). Hostiles without a
			// target rest twice as long (vanilla unaggroed hostiles barely
			// drift at all).
			busy := (m.hostile && m.hasTarget) || m.loveTicks > 0
			if m.stroll <= 0 && !busy {
				m.rest = restMin + h.rng.Intn(restMax-restMin)
				if m.hostile {
					m.rest *= 2
				}
				continue
			}
			if m.stroll > 0 {
				m.stroll--
			}
			dvx, dvz := m.behavior.steer(h, m)
			m.vx = m.vx*0.85 + dvx*0.15 // momentum → smooth
			m.vz = m.vz*0.85 + dvz*0.15
			if cap := m.moveSpeed(); math.Hypot(m.vx, m.vz) > cap {
				sp := math.Hypot(m.vx, m.vz)
				m.vx, m.vz = m.vx/sp*cap, m.vz/sp*cap
			}
		}

		// Move, by locomotion mode: walkers collide with terrain, fliers float
		// free, swimmers stay inside their water column, anchored mobs hold.
		// The crowding shove rides on top of the steering: vanilla applies it
		// after the AI has moved (pushEntities is the tail of aiStep), and it
		// deliberately escapes the speed clamp above — squeezing out of a
		// packed pen is meant to outrun a walk.
		nx, nz := m.x+m.vx+m.pushX, m.z+m.vz+m.pushZ
		fnx, fnz := int(math.Floor(nx)), int(math.Floor(nz))
		switch {
		case m.statik:
			m.vx, m.vz = 0, 0 // anchored (shulker)
		case m.flies:
			h.flyMove(m, nx, nz, fnx, fnz)
		case m.swims:
			h.swimMove(m, nx, nz, fnx, fnz)
		case isAmphibious(m.etype) && h.inWater(m.dim, m.x, m.y, m.z):
			// Drowned.travelInWater: in water it swims rather than walking the
			// floor, which is what lets it come up at a target instead of
			// trudging along the seabed under them.
			h.swimMove(m, nx, nz, fnx, fnz)
		default:
			// Walk — but never onto water, into a tree, or up/down a step taller
			// than one block (mobStepOK holds the rules). When blocked, commit to
			// a fresh random heading for a while to escape.
			//
			// The shove gets a second chance first: a mob being pressed into a
			// wall by the crowd should slide along it, as vanilla's axis-separated
			// collision does, not read the wall as "my route is blocked" and pick
			// a random new heading. Dropping the shove and retrying the mob's own
			// step keeps a herd against a fence from twitching.
			stepOK := h.mobStepOK(m, nx, nz)
			if !stepOK && (m.pushX != 0 || m.pushZ != 0) {
				nx, nz = m.x+m.vx, m.z+m.vz
				stepOK = h.mobStepOK(m, nx, nz)
			}
			switch {
			case stepOK && h.ownedAt(nx, nz):
				m.x, m.z = nx, nz
			case stepOK && h.migrateMobAcross(players, m, nx, nz):
				continue // stepped into a neighbour shard — handed off, done this tick
			case climbsWalls(m.etype):
				// Spider.tick: setClimbing(horizontalCollision) — a spider is
				// climbing exactly when it walked into something, and then goes
				// UP rather than looking for a way round.
				h.setClimbing(players, m, true)
				m.y++
			default:
				ang := h.rng.Float64() * 2 * math.Pi
				m.vx, m.vz = math.Cos(ang)*m.moveSpeed(), math.Sin(ang)*m.moveSpeed()
				m.reroute = 15 + h.rng.Intn(15)
			}
			if climbsWalls(m.etype) && stepOK {
				h.setClimbing(players, m, false) // nothing in the way any more
			}
			// Seat the feet on the real (edit-aware) floor every tick, so digging
			// the block under a mob drops it and a placed block lifts it — but never
			// onto a fence (the floor scan excludes fence-tops, so placing a fence
			// on a mob doesn't teleport it up onto the fence and strand it). The
			// floor is found from the mob's own height, NOT the column surface —
			// seating against the surface teleported every cave mob into daylight.
			oldY := m.y
			m.y = float64(h.worldFor(m.dim).MobFeetFrom(int(math.Floor(m.x)), int(math.Floor(m.z)), int(math.Floor(m.y))))
			if fell := oldY - m.y; fell > mobSafeFall { // the ground dropped out under it
				h.mobFall(players, m, fell)
			}
		}
		if m.vx != 0 || m.vz != 0 {
			m.yaw = float32(math.Atan2(-m.vx, m.vz) * 180 / math.Pi)
		}
		// A standing NPC turns to face the nearest player (it's listening to you).
		if _, isNPC := h.npcs[m.eid]; isNPC && math.Hypot(m.vx, m.vz) < 0.02 {
			if t := h.nearestPlayer(players, m.x, m.z, npcFaceRange); t != nil {
				m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
			}
		}

		// Only emit when the mob actually moved — broadcasting a no-op move
		// every tick for every mob overflows slow clients' send queues. The
		// event carries the absolute position; each viewer's renderer derives
		// its own relative deltas.
		if m.x != m.sx || m.y != m.sy || m.z != m.sz {
			m.sx, m.sy, m.sz = m.x, m.y, m.z
			h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		}
		// Facing only when it changed meaningfully (saves packets). BOTH the head
		// rotation and a zero-delta move ride the same latch: the move carries the
		// BODY yaw, without which a mob turning in place (tracking a target while
		// stationary) renders with a frozen torso — visible as a direction snap at
		// a shard crossing, where the shadow pipeline had been streaming the live
		// body yaw all along.
		if math.Abs(float64(m.yaw-m.syaw)) > 8 {
			m.syaw = m.yaw
			h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
			h.toNearbyEv(players, m.dim, m.x, m.z, entHead(m.eid, m.yaw))
		}
		if m.etype == entityEnderDragon {
			continue // the dragon flies on its own update (updateDragon)
		}
		if m.etype == entityIronGolem {
			h.golemMelee(players, m) // the guardian punches hostiles (not hostile itself)
		}
		if m.etype == entityEnderman {
			h.endermanCarry(players, m) // pick up / put down blocks (even while neutral)
		}
		if m.hostile {
			switch m.etype {
			case entitySkeleton, entityStray, entityBogged, entityPillager, entityIllusioner:
				h.skeletonShoot(players, m) // ranged: arrows from bow distance
			case entityBlaze:
				h.blazeShoot(players, m) // ranged: fireballs
			case entityGhast:
				h.ghastShoot(players, m) // ranged: explosive fireballs
			case entityBreeze:
				h.breezeShoot(players, m) // ranged: wind charges
			case entityWither:
				h.witherShoot(players, m) // ranged: wither skulls
			case entityShulker:
				h.shulkerShoot(players, m) // ranged: homing bullets (ours: straight)
			case entityLlama, entityTraderLlama:
				h.llamaSpit(players, m) // ranged: the spit IS the llama's only attack
			case entityEvoker:
				h.evokerCast(players, m) // fangs + vex summoning
			case entityWarden:
				h.wardenTick(players, m) // darkness aura + sonic boom + dig-away
			case entityGuardian, entityElderGuardian:
				h.guardianTick(players, m) // beam attack (+ elder mining-fatigue aura)
			case entityCreeper:
				h.creeperFuse(players, m) // fuse + swell + bang
			case entityWitch:
				h.witchThrow(players, m) // splash potions from a distance
			case entityDrowned:
				if m.trident {
					h.drownedThrow(players, m) // ranged: hurl a trident
				} else {
					h.mobMelee(players, m)
				}
			default:
				h.mobMelee(players, m) // bite a player in reach (on cooldown)
			}
		}
	}
}

// mobStepOK reports whether a walker may stand at (nx, nz): never onto water,
// into a tree, or up/down a step taller than one block.
//
// Step height is measured at the mob's own level (MobFeetFrom), not the column
// surface — a cave zombie steps along the cave floor, and the cave wall reads
// as an impossible step instead of "the surface is 30 up". Surface mobs get the
// same answer either way.
func (h *hub) mobStepOK(m *mob, nx, nz float64) bool {
	w := h.worldFor(m.dim)
	fnx, fnz := int(math.Floor(nx)), int(math.Floor(nz))
	cx, cz := int(math.Floor(m.x)), int(math.Floor(m.z))
	step := w.MobFeetFrom(fnx, fnz, int(math.Floor(m.y))) - int(math.Floor(m.y))
	// A fence/wall/fence-gate is only one block of "step" but 1.5 blocks of
	// collision, so a land mob can't climb over it — treat it as a wall.
	// A mob whose CURRENT cell is unwalkable (knocked/summoned into water)
	// may move regardless — steering walks it ashore like vanilla wading;
	// without this it was permanently trapped, every destination rejected.
	destOK := w.Walkable(fnx, fnz) || !w.Walkable(cx, cz)
	// Even a blindly-wandering mob won't step into a hazard its kind treats as
	// impassable (lava/fire/cactus) — unless it's already standing in one, so a
	// mob knocked into lava can still scramble out. Striders/fire-immune mobs
	// use their own profile (lava is fine).
	prof := malusFor(m.etype)
	hazardOK := prof[pathHazardKind(w, fnx, fnz)] >= 0 || prof[pathHazardKind(w, cx, cz)] < 0
	return destOK && hazardOK && step <= 1 && step >= -1 && !w.TallObstacle(fnx, fnz)
}

// speedFor derives a species' per-step speed from its vanilla MOVEMENT_SPEED
// attribute (vanilla 1.21.5 createAttributes), converted at ×0.45 per
// step — calibrated so the cow (attr 0.20 → 0.09) matches the
// oracle-measured perceived drift. Vanilla's chase/stroll goals mostly use
// speedModifier 1.0, so hunting shares the same base.
func speedFor(etype int) float64 {
	switch etype {
	case entityCow, entityMagmaCube: // attr 0.20
		return 0.09
	case entitySheep, entityZombie, entityHusk, entityDrowned,
		entityZombifiedPiglin, entityBlaze: // attr 0.23
		return 0.104
	case entityPig, entityChicken, entitySkeleton, entityStray,
		entityCreeper, entityWitch, entityIronGolem: // attr 0.25
		return 0.112
	case entitySpider, entityEnderman: // attr 0.30
		return 0.135
	}
	if d := speciesOf(etype); d != nil { // roster species: from the table
		return d.stepSpeed()
	}
	return mobSpeed // grazing default (slimes hop; villagers set at spawn)
}

// hurt applies physical damage through the mob's armor, using vanilla's exact
// absorption (CombatRules.getDamageAfterAbsorb:
// clamp(armor − dmg/(2 + toughness/4), armor×0.2, 20)/25 of the damage is
// absorbed) and a fractional carry so integer HP still yields vanilla
// hits-to-kill (a 5-damage sword kills an armor-2 zombie in 5 hits at
// 4.92/hit, not 4).
//
// This is a melee blow. Anything that is not — an environmental hazard, a
// spell, an effect ticking down — names its damage type through hurtKind or
// the hub's hurtMobOf, which decide from the type whether armour applies at
// all. It used to be the caller's job to know, and to skip this function and
// write to health directly when the answer was no; that convention is what let
// a mob standing in lava take the full burn through a full set of diamond.
func (m *mob) hurt(dmg float64) { m.hurtBreach(dmg, 0) }

// hurtKind is hurt against a named damage type: armour applies only if the type
// says it does, and the specialised protection enchantments on the mob's gear
// guard what they should.
func (m *mob) hurtKind(dmg float64, dt dmgType) { m.hurtOf(dmg, 0, dt) }

// hurtBreach is hurt with a breach fraction subtracted from the armor's
// effectiveness (the mace's Breach enchant: −0.15 per level). breachFrac 0 is
// the normal path.
func (m *mob) hurtBreach(dmg, breachFrac float64) { m.hurtOf(dmg, breachFrac, dtPlayerAttack) }

// hurtOf is the full form: breach fraction and damage type.
func (m *mob) hurtOf(dmg, breachFrac float64, dt dmgType) {
	if m.spawnInvuln > 0 {
		return // wither spawn-charge: immune while it powers up
	}
	// A creaking with a standing heart cannot be hurt: the blow goes to the
	// heart instead. Recorded rather than acted on, because this is the mob's
	// own arithmetic with no hub in reach — the hub answers for it next tick.
	// Putting the check HERE rather than at the places that swing is the point:
	// there is no path to a creaking's health that skips this function.
	if m.heartBound && !dt.has(tagBypassesInvulnerability) {
		m.heartHit = true
		return
	}
	if !dt.has(tagBypassesEffects) && !dt.has(tagBypassesResistance) {
		dmg *= m.damageResistance() // the Resistance effect, as on a player
	}
	if armor := m.armorValue(); armor > 0 && !dt.has(tagBypassesArmor) {
		// Toughness used to be hardcoded to 0 here, so diamond and netherite
		// gear on a mob absorbed no better than leather.
		tough := m.armorToughness()
		reduced := math.Min(20, math.Max(armor-dmg/(2+tough/4), armor*0.2))
		frac := reduced / 25
		if breachFrac > 0 {
			frac = math.Max(0, frac-breachFrac) // Breach lets the hit ignore some armor
		}
		dmg *= 1 - frac
	}
	// The protection enchantments on the mob's own gear, same arithmetic as a
	// player's (vanilla runs this as a second, separate absorption step).
	if !dt.has(tagBypassesEffects) && !dt.has(tagBypassesEnchantments) {
		dmg = float64(applyProtection(float32(dmg), protectionPoints(m.gear[:], dt)))
	}
	dmg += m.dmgFrac
	whole := math.Floor(dmg)
	m.dmgFrac = dmg - whole
	m.health -= int(whole)
}

// grounded reports what the client should believe about this mob's footing.
// It is not cosmetic: the client animates a flying mob's WINGS only while it
// thinks the entity is airborne, so reporting a hovering bee, parrot, phantom
// or ghast as on-ground freezes its wings mid-flight (and misinforms the
// client's own motion interpolation). Free-flying mobs are never on the
// ground in this engine — m.flies means exactly "no ground collision".
func (m *mob) grounded() bool { return !m.flies }

// removeMob silently despawns a mob (no death animation, no loot) — used for
// out-of-range cleanup, where a loot shower would be wrong.
func (h *hub) removeMob(players map[int32]*tracked, m *mob) {
	delete(h.mobs, m.eid)
	h.gridDirty()
	h.toNearbyEv(players, m.dim, m.x, m.z, entGone(m.eid))
	h.shadowGoneAll(m.eid) // retract any cross-seam shadow of it
}

// broadcastSync resends every mob's authoritative absolute position to nearby
// players. Relative moves are lossy (best-effort send, dropped on a full queue),
// so this periodic snapshot self-heals any per-client drift — without it two
// clients that dropped different move packets show a mob at different spots.
func (h *hub) broadcastSync(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m == h.dragon {
			continue // the dragon's movement is NoSync (see updateDragon): 776
			//          clients lose entities to sync_entity_position, so it
			//          rides relative moves only — nothing to resync here.
		}
		h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		if m.burning { // one-shot fire flags can be dropped — re-assert while lit
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, true)))
		}
		if m.etype == entitySkeleton { // bow-in-hand is one-shot too — keep it honest
			h.toNearbyEv(players, m.dim, m.x, m.z, skeletonEquip(m.eid))
		}
		// One-shot mount/pet state re-asserted so a late-joining player sees the
		// saddle, rider and collar rather than a bare animal.
		if m.rider != 0 {
			h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, m.rider))
		}
		if len(m.riders) > 0 {
			h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, m.riders...))
		}
		if m.mobRider != 0 { // a mob rider (raid ravager) — re-assert for late joiners
			h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, m.mobRider))
		}
		if m.harness != 0 {
			h.toNearbyEv(players, m.dim, m.x, m.z, ghastHarnessEquip(m.eid, m.harness))
		}
		if m.etype == entityCopperGolem && m.oxidation > 0 {
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(copperWeatherMeta(m.eid, int32(m.oxidation))))
		}
		if m.etype == entityEnderman && m.carriedBlock != 0 {
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(enderCarryMeta(m.eid, m.carriedBlock)))
		}
		if m.etype == entityBee { // pollen coat / red eyes are one-shot state too
			if m.beeSentFlags != 0 {
				h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(beeFlagsMeta(m.eid, m.beeSentFlags)))
			}
			if m.beeSentAngry {
				h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(beeAngerMeta(m.eid, m.anger)))
			}
		}
		if m.saddled { // the saddle is an EQUIPMENT slot on every species (1.21.5+)
			if horseFamily(m.etype) {
				h.horseEquipSync(players, m) // saddle + body armor together
			} else {
				h.toNearbyEv(players, m.dim, m.x, m.z, saddleEquip(m.eid))
			}
		}
		if m.tamed {
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(petMeta(m)))
		}
		if m.sleeping { // re-assert the lying pose so a late-joining player sees it
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(sleepMetadata(m.eid, m.bed)))
		}
	}
	// Players relay to each other with the same lossy relative moves, so resync
	// each player's authoritative position to every other in-range player.
	// Equipment rides along: it's one-shot state a full send queue can drop,
	// so the periodic resync keeps everyone's worn armor/held item honest.
	for _, t := range players {
		sync := entMove(t.p.eid, t.x, t.y, t.z, t.yaw, t.pitch, true)
		equip := equipEv(t.p.eid, heldStack(t), t.offhand, t.armor)
		cx, cz := chunkFloor(t.x), chunkFloor(t.z)
		for eid, other := range players {
			if eid == t.p.eid || other.dim != t.dim {
				continue
			}
			if abs(chunkFloor(other.x)-cx) <= viewRadius && abs(chunkFloor(other.z)-cz) <= viewRadius {
				other.p.trySendEv(sync)
				other.p.trySendEv(equip)
			}
		}
	}
}

// herdRoamRadius bounds how far a herd's goal may drift from where the herd was
// rooted. Without it the walk below is a pure random walk with no restoring
// force, so a herd diffuses outward without limit — and since a herd's cows
// steer toward the goal, they follow it. That matters most when NOBODY is
// playing: the boot-seeded herds are spawned straight into h.mobs, never enter
// activeChunks, and reconcileMobChunks (the only unload path) is reached solely
// from naturalSpawn, which returns early with no players. So those herds tick
// forever, and every step reads the world at fresh coordinates, generating and
// caching terrain that nobody will ever see.
//
// The size is chosen against the chunk cache, not by feel: a herd can touch
// roughly π(r+16)²/256 chunks (the +16 is the cows' spread around the goal),
// and world.cacheCap budgets 256 MiB of generator output. At 64 the three boot
// herds can reach ~88 MiB even if their discs never overlap (they start within
// 60 blocks of each other, so in practice far less); at 128 they could fill the
// whole budget on their own, which is the very thing this bound exists to stop.
const herdRoamRadius = 64 // blocks from home; 4 chunks each way

// updateHerdTargets roams each herd's goal, so each group moves as one rather
// than scattering. Slow random walk that stays on land and near home.
func (h *hub) updateHerdTargets() {
	for _, hd := range h.herds {
		if h.rng.Intn(100) == 0 { // occasionally pick a new drift direction
			ang := h.rng.Float64() * 2 * math.Pi
			hd.vx, hd.vz = math.Cos(ang)*0.05, math.Sin(ang)*0.05
		}
		nx, nz := hd.x+hd.vx, hd.z+hd.vz
		// Turn back at the water's edge and at the edge of the home range, the
		// same way: reversing the drift rather than clamping the position keeps
		// the herd from sliding along an invisible wall.
		if !h.world.IsLand(int(math.Floor(nx)), int(math.Floor(nz))) ||
			sq(nx-hd.hx)+sq(nz-hd.hz) > herdRoamRadius*herdRoamRadius {
			hd.vx, hd.vz = -hd.vx, -hd.vz
			continue
		}
		hd.x, hd.z = nx, nz
	}
}

// spawnableAnimal reports whether an ANIMAL may spawn in a column: physically
// spawnable, on natural ground (grass/dirt), and open to the sky — vanilla's
// grass+light rule, and what keeps boot-seeded herds out of player builds
// (a roofed interior is never sky-exposed).
func (h *hub) spawnableAnimal(x, z int) bool {
	return h.spawnableAnimalFor(entityCow, x, z)
}

// spawnableAnimalFor is spawnableAnimal with the species' own spawnable-on
// rule (spawnspecies.go): a turtle herd wants beach sand, a mooshroom herd
// mycelium, a frog herd mud — none of which the generic grass/dirt check
// would ever accept.
func (h *hub) spawnableAnimalFor(etype, x, z int) bool {
	if !h.world.Spawnable(x, z) || !h.skyExposedColumn(x, z) {
		return false
	}
	feet := h.world.MobFeet(x, z)
	below := h.world.Block(x, feet-1, z)
	if ok, handled := creatureFloorOK(etype, below, feet); handled {
		return ok
	}
	switch below {
	case worldgen.GrassBlock, worldgen.Dirt:
		return true
	}
	return false
}

// spreadSpawn picks a distinct animal-spawnable column near (cx,cz) for a mob,
// so a herd fans out instead of stacking on one spot. Falls back to findLand if
// the patch is crowded. Marks the chosen column occupied.
func (h *hub) spreadSpawn(cx, cz int, occupied map[[2]int]bool) (int, int) {
	for try := 0; try < 40; try++ {
		x, z := cx+h.rng.Intn(11)-5, cz+h.rng.Intn(11)-5
		if h.spawnableAnimal(x, z) && !occupied[[2]int{x, z}] {
			occupied[[2]int{x, z}] = true
			return x, z
		}
	}
	x, z := h.findLand(cx+h.rng.Intn(11)-5, cz+h.rng.Intn(11)-5)
	occupied[[2]int{x, z}] = true
	return x, z
}

// findLand spirals out from (cx,cz) to the nearest walkable column (dry land,
// no tree) — a clear spot to root a mob.
func (h *hub) findLand(cx, cz int) (int, int) {
	for r := 0; r < 256; r++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if (dx == -r || dx == r || dz == -r || dz == r) && h.world.Spawnable(cx+dx, cz+dz) {
					return cx + dx, cz + dz
				}
			}
		}
	}
	return cx, cz
}

// toNearby sends a packet to every same-dimension player tracking the chunk
// at (x,z).
// toDim broadcasts to every player in a dimension, regardless of distance —
// for boss-scale entities (the dragon, its crystals) that must be visible
// across the whole End island.
// toDimEv broadcasts a domain event to every player in a dimension;
// toNearbyEv only to those whose interest window covers (x,z).
func (h *hub) toDimEv(players map[int32]*tracked, dim int, ev any) {
	for _, t := range players {
		if t.dim == dim {
			t.p.trySendEv(ev)
		}
	}
}

func (h *hub) toNearbyEv(players map[int32]*tracked, dim int, x, z float64, ev any) {
	cx, cz := chunkFloor(x), chunkFloor(z)
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		if abs(chunkFloor(t.x)-cx) <= viewRadius && abs(chunkFloor(t.z)-cz) <= viewRadius {
			t.p.trySendEv(ev)
		}
	}
}

// newMobAttributes is Mob.createMobAttributes for one species: the per-entity
// starting point, which is NOT the attribute registry's own defaults. Every one
// of these differs from the registry — follow range is 32 there but 16 on a
// vanilla Mob, and taking the registry's movement speed would have mobs
// crossing eight chunks a second — so the species baseline is the only safe
// thing to seed with.
func newMobAttributes(etype int) *attribute.Map {
	a := attribute.NewMap()
	a.SetBase(attr.FollowRange, aggroRange)
	a.SetBase(attr.MaxHealth, float64(mobHealth(etype)))
	a.SetBase(attr.MovementSpeed, speedFor(etype))
	a.SetBase(attr.AttackDamage, meleeDamageFor(etype))
	return a
}

// mobAttrs returns the mob's attribute map, seeding it from the species if the
// mob skipped the spawn path — a reload, or a test building one by hand.
func (m *mob) mobAttrs() *attribute.Map {
	if m.attrs == nil {
		m.attrs = newMobAttributes(m.etype)
	}
	return m.attrs
}

// followRange is the mob's FOLLOW_RANGE: how far it hunts. Backed by the
// attribute map so equipment, effects and plugins can modify it, rather than
// being a bare field only the spawn path could set.
func (m *mob) followRange() float64 { return m.mobAttrs().Value(attr.FollowRange) }

// setFollowRange sets the mob's base FOLLOW_RANGE.
func (m *mob) setFollowRange(v float64) { m.mobAttrs().SetBase(attr.FollowRange, v) }

// maxHP is the mob's MAX_HEALTH, as an int because health is tracked in whole
// hit points. Backed by the attribute map, so a plugin raising it and a future
// health-boost effect go through the same place.
func (m *mob) maxHP() int { return int(m.mobAttrs().Value(attr.MaxHealth)) }

// setMaxHP sets the base MAX_HEALTH.
func (m *mob) setMaxHP(v int) { m.mobAttrs().SetBase(attr.MaxHealth, float64(v)) }

// gearArmorSource is the modifier a mob's worn armour contributes to ARMOR.
// Vanilla does the same thing — equipment is a modifier, not a change to the
// base — which is what lets the piece be taken off again without arithmetic.
const gearArmorSource = "equipment:armor"

// armorValue is the mob's ARMOR: its species base plus whatever it is wearing.
func (m *mob) armorValue() float64 { return m.mobAttrs().Value(attr.Armor) }

// setBaseArmor sets the species' own ARMOR, below any worn piece.
func (m *mob) setBaseArmor(v float64) { m.mobAttrs().SetBase(attr.Armor, v) }

// refreshGearArmor re-derives everything a mob's worn armour contributes:
// armour points, toughness, and the attribute effects of the enchantments on
// those pieces. Recomputing beats adding a delta at each equip: a reloaded mob
// comes back wearing its saved gear with no equip event to replay, and used to
// lose the protection entirely.
//
// This is the mob half of the player's refreshArmorAttrs + refreshEnchantAttrs.
// Mobs pick up dropped gear, so enchanted armour on a mob is reachable in play
// and used to count for nothing beyond its raw points.
func (m *mob) refreshGearArmor() {
	pts, tough := 0.0, 0.0
	for _, g := range m.gear {
		if g.item == 0 {
			continue
		}
		if p, ok := armorInfo[g.item]; ok {
			pts += float64(p.Points)
			tough += p.Toughness
		}
	}
	a := m.mobAttrs()
	setEquip(a.Get(attr.Armor), pts)
	setEquip(a.Get(attr.ArmorToughness), tough)
	m.refreshGearEnchants()
}

// refreshGearEnchants applies the attribute effects of the enchantments on a
// mob's worn armour, summed across pieces exactly as the player path does.
func (m *mob) refreshGearEnchants() {
	a := m.mobAttrs()
	for id, mods := range enchantAttributes {
		lvl := 0
		for _, g := range m.gear {
			if g.item != 0 {
				lvl += g.enchLvl(int8(id))
			}
		}
		src := enchantSource(id)
		for _, mod := range mods {
			in := a.Get(mod.id)
			if lvl == 0 {
				in.RemoveModifier(src)
				continue
			}
			in.AddModifier(attr.Modifier{Source: src, Amount: mod.perLvl * float64(lvl), Op: mod.op})
		}
	}
}

// armorValue and armorToughness are the mob's ARMOR / ARMOR_TOUGHNESS.
func (m *mob) armorToughness() float64 { return m.mobAttrs().Value(attr.ArmorToughness) }

// babySpeedSource is vanilla's SPEED_MODIFIER_BABY: babies move at 1.5× on a
// multiply-base modifier, not by rewriting the base. Keeping it a modifier is
// what stops a later behaviour swap — which resets the base — from silently
// turning a baby zombie back into an adult-paced one.
const babySpeedSource = "baby"

// moveSpeed is the mob's MOVEMENT_SPEED.
//
// The unit here is tachyne's per-update step in blocks, NOT vanilla's raw
// attribute number — the movement integrator runs at 10 updates/s and every
// speed table in the engine is already calibrated in those units (attrToStep
// is the conversion where a vanilla figure is the source). Modifiers are
// unaffected by the choice: vanilla's speed modifiers are proportional
// (multiply-base or multiply-total), so they mean the same thing in either
// scale.
func (m *mob) moveSpeed() float64 { return m.mobAttrs().Value(attr.MovementSpeed) }

// setMoveSpeed sets the base MOVEMENT_SPEED, in per-update blocks.
func (m *mob) setMoveSpeed(v float64) { m.mobAttrs().SetBase(attr.MovementSpeed, v) }

// setBabySpeed applies or clears the baby speed modifier.
func (m *mob) setBabySpeed(on bool) {
	in := m.mobAttrs().Get(attr.MovementSpeed)
	if !on {
		in.RemoveModifier(babySpeedSource)
		return
	}
	in.AddModifier(attr.Modifier{Source: babySpeedSource, Amount: 0.5, Op: attr.AddMultipliedBase})
}

// kbResist is the mob's KNOCKBACK_RESISTANCE: the FRACTION of an incoming
// shove it shrugs off, 0 (no resistance) through 1 (immovable). Vanilla
// applies it as power *= 1 - resistance, so the values in between matter —
// a ravager at 0.75 still slides a little.
func (m *mob) kbResist() float64 { return m.mobAttrs().Value(attr.KnockbackResistance) }

// setKBResist sets the base KNOCKBACK_RESISTANCE.
func (m *mob) setKBResist(v float64) { m.mobAttrs().SetBase(attr.KnockbackResistance, v) }

// kbScale is the multiplier an incoming knockback impulse survives — the
// LivingEntity.knockback / AbstractArrow form, floored at 0.
func (m *mob) kbScale() float64 { return math.Max(0, 1-m.kbResist()) }

// attackDamage is the mob's ATTACK_DAMAGE base. Species that add a flat bonus
// on top (the magma cube's +2) do it at the point of use, as vanilla does.
func (m *mob) attackDamage() float64 { return m.mobAttrs().Value(attr.AttackDamage) }

// setAttackDamage sets the base ATTACK_DAMAGE.
func (m *mob) setAttackDamage(v float64) { m.mobAttrs().SetBase(attr.AttackDamage, v) }

// refreshBabySpeed re-asserts the baby modifier from the mob's current flag.
// Only the zombie family carries it in vanilla — baby animals walk at adult
// pace — which is the hostile-and-baby case here. Needed wherever the flag is
// assigned rather than rolled: a reload, or a drowning conversion.
func (m *mob) refreshBabySpeed() { m.setBabySpeed(m.baby && m.hostile) }
