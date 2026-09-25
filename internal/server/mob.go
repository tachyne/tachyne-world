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
	restMin     = 80  // idle spell: 8-20 s…
	restMax     = 200 //
	strollMin   = 25  // …then a 2.5-4.5 s stroll, back to idle
	strollMax   = 45
	mobSpeed    = 0.09 // blocks per step (×10 = 0.9 blocks/sec grazing)
	panicTicks  = 20   // mob updates a panic-causing hurt is remembered (getLastDamageSource: 40 ticks)
	panicLegMax = 60   // updates before an unreached panic spot is dropped (the path is done)
)

var (
	entityCow = entityID("cow") // minecraft:entity_type ordinals (1.21.5)
)

type mob struct {
	// SpearUseGoal (spearmob.go): the goal's state while it runs, the tick the
	// spear was lowered (0 = raised) and who that charge has struck.
	spearGoal       *spearGoalState
	spearUseAt      uint64
	spearHits       map[int32]uint64
	invulnTicks     int32   // LivingEntity.invulnerableTime: 20 after a landed blow, counting down
	lastHurt        float64 // the raw amount of that blow: only a bigger blow's excess lands while > 10
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
	fleeX, fleeZ    float64  // the threat that set it panicking
	panicTX         float64  // the random spot it is running to (PanicGoal), when panicHasT
	panicTZ         float64
	panicHasT       bool
	panicLeg        int     // updates spent on the current panic leg
	hostile         bool    // hunts + attacks players (zombies) rather than grazing
	burning         bool    // on fire — rendered via entity flags (any ignite source)
	burnDelay       int     // seconds of dawn-ramp grace before this mob ignites
	fireSecs        int     // seconds of afterburn left (lava/fire/daylight); 1 HP/s, water clears
	submerged       int     // consecutive seconds fully underwater (land mobs drown past maxAir)
	dryTicks        int     // water animal: ticks out of water (air gone past its cap; a dolphin's moisture)
	convertIn       int     // zombie/husk: seconds left of the shaking conversion phase (0 = not converting)
	snowSecs        int     // skeleton: consecutive seconds standing in powder snow (Skeleton.inPowderSnowTime)
	strayIn         int     // skeleton: seconds left of the freeze conversion into a stray (0 = not converting)
	wetHurt         int     // water-sensitive mob: ticks until the wet hurts it again
	creakActive     bool    // creaking: IS_ACTIVE — awake and hunting since a player looked at it
	swell           int     // creeper: Creeper.swell, ticks into the fuse (explodes at creeperFuseTicks)
	swellDir        int8    // creeper: +1 swelling, else unwinding (DATA_SWELL_DIR; 0 reads as -1)
	swellHold       bool    // creeper: SwellGoal is running, which holds it still (Flag.MOVE)
	ignited         bool    // creeper: lit by flint and steel or a fire charge — it swells whatever happens
	anger           int     // spider: mob-updates it stays hostile in daylight after a hit
	stareTicks      int     // enderman: ticks a distant target has gone unwatched (teleportTowards)
	settled         int     // enderman: ticks since its target last changed (the daylight flight waits 600)
	dragonPhase     int     // ender dragon: which phase of the fight it is in
	dragonPhaseTick int     // ticks spent in the current phase
	dragonFlames    int     // breaths taken this perch
	dragonCharge    int     // ticks of fireball aim built up
	fangNextAt      uint64  // evoker: tick its fang spell comes off cooldown
	vexNextAt       uint64  // evoker: tick its summon spell comes off cooldown
	wololoTarget    int32   // evoker: the blue sheep its wololo is aimed at
	wololoWarm      int     // evoker: wololo warm-up ticks left
	wololoNextAt    uint64  // evoker: tick the wololo comes off cooldown
	vexLife         int     // summoned vex: ticks left before it expires (0 = unlimited)
	hitByPlayer     bool    // died within 100 ticks of a player's hurt: pays XP and player-kill loot (set by killMob)
	hurtByPlayer    int32   // LivingEntity.lastHurtByPlayer: the player it remembers hurting it (0 = an offline owner)
	hurtByPlayerTil uint64  // …remembered until this tick (lastHurtByPlayerMemoryTime); 0 = no memory
	lastAttacker    int32   // eid of the last entity that hurt it (plugin death event)
	lastDT          dmgType // the last damage type it took (the killing blow's, for loot conditions)
	lastDirect      int     // entity type of the projectile that struck the last blow (0 = none)
	looting         int     // killer's Looting level (stamped per hit, used at drop time)
	baby            bool    // ageable: half-size, grows up, no drops/XP
	growLeft        int     // ticks until a baby matures
	loveTicks       int     // courting window after love-food (hearts)
	breedTime       int     // BreedGoal.loveTime: ticks this pair has spent together
	followClock     int     // pet: ticks until FollowOwnerGoal re-decides (timeToRecalcPath)
	lovedBy         int32   // who fed the love-food (advancement credit)
	breedCD         int     // ticks before this parent may breed again
	parent          int32   // baby: the adult it is following (FollowParentGoal), 0 = none
	parentRecalc    int     // mob updates until the parent search runs again
	stroll          int     // wander spell: updates left walking before the next rest
	sheared         bool    // sheep: fleece off (regrows by grazing)
	color           int8    // sheep: fleece colour (0 white .. 15 black), dyeable
	collar          int8    // tamed wolf/cat: collar dye (DyeColor ordinal; red when tamed)
	soundSet        int8    // wolf: which of the seven WolfSoundVariants it was born with
	stew            int8    // brown mooshroom: the stew flower it was fed (stew.go), 0 = none
	customName      string  // name-tagged: shown above the mob, and it never despawns
	fromBucket      bool    // released from a mob bucket: persistent (Bucketable.setFromBucket)
	persistent      bool    // Mob.persistenceRequired: picked up gear (never despawns)
	pregnant        bool    // frog: IS_PREGNANT — carrying a clutch until it finds water to lay on
	aggressive      bool    // Mob.setAggressive: the zombie family's raised arms while it chases
	drifting        bool    // MoveThroughVillageGoal: walking to a spot in the village, not chasing
	drownedGoal     bool    // drowned: walking to water (by day) or to the beach (at night)
	strafeBack      bool    // RangedBowAttackGoal.strafingBackwards: drifting away while circling
	floatX          float64 // RandomFloatAroundGoal's wanted position (ghast)
	floatY          float64
	floatZ          float64
	floatSet        bool
	headNext        [2]int        // wither: nextHeadUpdate for each side head
	headIdle        [2]int        // …and idleHeadUpdates, the count before a bored shot
	headTarget      [2]int32      // …and the victim each has picked
	witherSmash     int           // WitherBoss.destroyBlocksTick: ticks until it levels its surroundings
	schoolLeader    int32         // fish: the leader it follows (FollowFlockLeaderGoal)
	schoolFollowers int           // …or how many follow IT
	schoolNext      int           // …and the ticks before it looks for a school again
	turtleHoming    bool          // turtle: swimming back to the beach it was born on
	phantomRadius   float64       // phantom: the circle it flies around its anchor
	phantomHigh     float64       // …how far above the target that anchor sits
	phantomCW       bool          // …which way round it goes
	phantomNext     int           // …ticks to the next swoop
	phantomSwoop    int           // …and the ticks left in the one it is flying
	wardenAnger     map[int32]int // warden: AngerManagement's grudge per suspect
	angerClock      int           // …and the ticks until the next decay
	piglinFlee      int           // piglin: ticks left avoiding a zombified piglin, a nemesis or hoglins
	piglinFleeX     float64       // …and what it is backing away from
	piglinFleeZ     float64
	piglinFleeFrom  int32  // …the mob it is avoiding, followed as it moves (0 = a fixed spot)
	noHunt          bool   // piglin: CannotHunt; hoglin: CannotBeHunted (a bastion's own; persisted)
	huntedUntil     uint64 // piglin: HUNTED_RECENTLY, the tick it lapses (a reload rolls it afresh)
	piglinFoe       int32  // piglin: the ATTACK_TARGET it last had (0 = none), for the dead-target rules
	celebrateUntil  uint64 // piglin: CELEBRATE_LOCATION, the tick it lapses (0 = not celebrating)
	celebratePos    blockPos
	fightBack       int32     // hoglin: the piglin that hit it and it now fights (ATTACK_TARGET from wasHurtBy)
	idleWalk        *idleWalk // piglin brute: the walk its idle RunOne picked; piglin: its celebration's (nil = none)
	homeToNext      uint64    // piglin brute: StrollToPoi's nextOkStartTime
	homeAroundNext  uint64    // …and StrollAroundPoi's
	golemGrudgeEID  int32     // iron golem: the player who hit it (HurtByTargetGoal)
	golemGrudgeLeft int       // …and the ticks it keeps after them
	variant         int32     // species variant (variant.go: coat/colour, horse colour|markings<<8, villager type); meaningful when variantSet
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
	flyPath                         []blockPos
	flyIdx                          int
	flyGoal                         blockPos
	flyStale                        int         // mob-updates since the path was computed
	size                            int         // slime: 4/2/1 (splits in half on death)
	cube                            sulfurState // sulfur cube: the swallowed block and what it does (sulfurcube.go)
	neutral                         bool        // enderman: peaceful until hit (anger flips it hostile)
	carriedBlock                    uint32      // enderman: the block state it's holding (0 = none)
	witherHealFrac                  float32     // wither: the part of a health point its regen has banked
	sonicCD                         int         // warden: mob-updates until the next sonic boom may start
	sonicRun                        int         // warden: mob-updates into a running sonic boom (0 = none)
	beamTarget                      int32       // guardian: the player the beam is locked on (0 = none)
	hideUntil                       uint64      // villager: heard a bell — stay at the bed until this tick
	beamTicks                       int         // guardian: GuardianAttackGoal.attackTime, in ticks
	digClock                        int         // warden: mob-updates with no target (digs away at the cap)
	patrolCaptain                   bool        // pillager patrol leader (carries the ominous banner)
	raidCenter                      blockPos    // raider: the raid this mob belongs to (zero = not a raider)
	raidWave                        int         // raider: the wave it came with (Raider.wave)
	celebrating                     bool        // raider: cheering a lost raid (IS_CELEBRATING)
	holdingGround                   bool        // pillager/vindicator: HoldGroundAttackGoal's stand-off
	idleSecs                        int         // seconds spent >32 blocks from every player (despawn clock)
	hopTicks                        int         // slime: updates left mid-bound (traveling)
	hopDelay                        int         // slime: updates until the next bound (grounded, still)
	strafeCW                        bool        // skeleton: current circling direction while shooting
	retaliates                      bool        // peaceful until hit, then hunts its attacker (wolf/goat)
	rider                           int32       // player eid riding this mob (0 = none); AI pauses while ridden
	standLeft, standNext            int         // horse: ticks left in a rear; RandomStandGoal's counter (horsestand.go)
	riders                          []int32     // happy ghast: up to 4 rider eids (riders[0] pilots); AI pauses while any aboard
	mount                           int32       // eid of the MOB this mob rides (raid ravager riders); 0 = none
	cart                            int32       // eid of the MINECART carrying this mob (scooped up by a rolling cart); 0 = none
	mobRider                        int32       // eid of the MOB riding this one (the reverse of mount); 0 = none
	mobRider2                       int32       // a camel's back seat: the second MOB aboard (a camel husk's parched); 0 = none
	caravanHead, caravanTail        int32       // llama caravans (caravan.go): the llama followed, and the one following
	caravanSpeed                    float64     // …the goal's speed modifier, 2.1 rising while it lags
	caravanGrace                    int         // …mob updates left to catch up once past 26 blocks
	navMount                        *mob        // the vehicle this rider steers (Mob.getNavigation hands a driver its vehicle's), nil = on foot
	mountDrives                     bool        // this rider's AI leads and its mount follows (a chicken jockey's zombie)
	jockey                          bool        // a chicken carrying a jockey: no eggs, despawns, ten experience
	spawnTick                       uint64      // Entity.tickCount's origin: the world tick it was created (resets on a reload, as vanilla's does)
	trap                            bool        // a skeleton horse waiting as a lightning trap (skeletontrap.go)
	savedMount                      int32       // a reloaded rider's vehicle by its OLD eid, relinked once the chunk is up (mobchunks.go)
	harness                         int32       // happy ghast: equipped harness item id (0 = none); gates riding
	oxidation                       int         // copper golem: weather stage 0 unaffected → 3 oxidized
	oxidizeAt                       uint64      // copper golem: tick of the next oxidation step
	waxed                           bool        // copper golem: honeycombed → never oxidizes
	carrying                        invStack    // copper golem: items in transit between chests
	sortGoal                        blockPos    // copper golem: the container it's walking to
	sortHasGoal                     bool        // copper golem: sortGoal is valid
	sortCD                          int         // copper golem: ticks until the next transport
	trident                         bool        // drowned: armed with a trident (throws it at range)
	canPickup                       bool        // may pick up dropped gear (spawn-time roll)
	gear                            [4]invStack // worn armor by slot (0 head,1 chest,2 legs,3 feet)
	spawnGear                       bool        // gear issued at spawn: drops at gearDrop (0 for ominous trial gear, 8.5% for natural spawns)
	gearDrop                        float32     // per-piece drop chance of spawn-issued gear
	charged                         bool        // creeper struck by lightning: a doubled blast (persisted)
	saddled                         bool        // a saddle is on: this mob can be mounted
	saddleSt                        invStack    // the saddle item (horse family; saddled mirrors it)
	boosting                        bool        // pig/strider: a food-on-a-stick sprint is running (ItemBasedSteering)
	boostTick                       int         // …how far into it, counted only while ridden
	boostTotal                      int         // …and the rolled length the client was told about
	armorSt                         invStack    // body armor / llama carpet / wolf armor
	armorNote                       int8        // wolf armor: 1 = cracked further, 2 = broke — the hub plays it next update
	chested                         bool        // donkey/mule/llama carrying a chest
	chest                           []invStack  // chest contents (columns×3)
	strength                        int8        // llama: chest columns (1-5)
	tamed                           bool        // wolf/cat/parrot tamed to an owner
	sitting                         bool        // tamed pet told to stay (right-click toggle)
	spawnInvuln                     int         // wither: ticks of spawn-charge invulnerability left
	owner                           int32       // owner player eid (0 = wild); pets follow this player
	ownerUUID                       [16]byte    // owner's stable identity (persisted; owner eid is re-resolved on join)
	path                            []pathPoint // A* route toward the current goal (nil = steer straight)
	pathIdx                         int         // index of the next waypoint to walk to
	pathGoal                        [2]int      // block goal the current path was computed for
	pathAt                          uint64      // tick the path was computed (staleness clock)
	usesDoors                       bool        // villager: may plan through + open wooden doors
	roamX, roamZ                    float64     // villager: current roam target (goal-directed wander)
	roamAt                          uint64      // tick to pick a fresh roam target
	golemStrolling                  bool        // iron golem: idle (strolling or heading home) at 0.6, not fighting
	bed                             blockPos    // villager: its bed (sleep anchor; zero = no schedule)
	work                            blockPos    // villager: its profession workstation (day work site)
	farmPos                         blockPos    // farmer: the plot it is tending (zero = none)
	farmWorked                      int         // farmer: ticks worked this session
	farmNext                        uint64      // farmer: tick it may start (or switch plots) again
	bmPos                           blockPos    // farmer: the crop it is bone-mealing (zero = none)
	bmWorked                        int         // farmer: ticks of this bone-meal session
	bmNext, bmLast                  uint64      // farmer: next pinch; last session's end
	vFood                           int         // villager: foodLevel (Villager.FOOD_POINTS eaten, digested at a birth)
	breedMate                       int32       // villager: BREED_TARGET (0 = none)
	breedAt                         uint64      // villager: VillagerMakeLove birthTimestamp
	breedLead                       bool        // villager: this half runs the courtship clock
	jobPos                          blockPos    // villager: POTENTIAL_JOB_SITE (zero = none)
	poiAt                           [poiGroups]uint64
	poiRetries                      [poiGroups]map[blockPos]*poiRetry // AcquirePoi's batch cache, per claim
	cantReachSince                  [poiGroups]uint64                 // CANT_REACH_WALK_TARGET_SINCE, per claim
	pathReached                     bool                              // the last planned path reached its goal
	meet                            blockPos                          // villager: the village meeting point (bell/well)
	sleeping                        bool                              // villager: lying in its bed through the night
	lastSlept                       uint64                            // villager: the tick it last lay down PLUS ONE (LAST_SLEPT; 0 = never)
	golemSeen                       uint64                            // villager: tick the GOLEM_DETECTED_RECENTLY memory runs out
	swims                           bool                              // water-bound: lives inside a water column (fish/squid)
	flies                           bool                              // free flight: no ground collision (bat/phantom/ghast)
	statik                          bool                              // anchored: never walks (shulker)
	climbing                        bool                              // spider: clinging to a wall right now (synced state)
	skittish                        bool                              // bolts from any close player (fox/ocelot/rabbit)
	hover                           float64                           // fliers: preferred altitude above the terrain
	held                            int32                             // rendered main-hand item (0 = empty)
	heldEnch                        enchList                          // enchantments on that item (spawn gear rolls them)
	heldDmg                         int                               // wear on that item
	heldCount                       int                               // how many it holds (0 = one): a hand takes a whole stack
	gearSure                        [5]bool                           // setGuaranteedDrop per slot (0-3 armour, 4 hand): picked up, so it always drops
	carry                           invStack                          // allay: the stack it has collected for its liked player
	allayPickupCD                   int                               // allay: ticks before it collects again (60 after a throw)
	allayNoteCD                     int                               // allay: ticks it keeps delivering to the liked note block (600 per note)
	allayNote                       blockPos                          // allay: that note block
	allayNoteDim                    int                               // …in this dimension (vanilla keeps a GlobalPos)
	dupCD                           int                               // allay: ticks until it may duplicate again (6000)
	dancing                         bool                              // allay: a jukebox plays within earshot; piglin: DANCING (DATA_IS_DANCING)
	frogEaten                       int8                              // slime/magma cube: eaten by a frog of variant-1 (froglight, no slime)
	sneezeAt                        uint64                            // baby panda: the tick its sneeze lands (0 = not sneezing)
	pandaFlags                      byte                              // panda: sneeze/roll/sit/on-back flags (DATA_ID_FLAGS)
	rollLeft                        int                               // panda: updates left in a roll
	rollDX, rollDZ                  float64                           // panda: the roll's heading
	lieCD                           uint64                            // panda: the tick it may lie on its back again
	dashCD                          int                               // camel: ticks left on the dash cooldown (flag drops at 50)
	poseTick                        int64                             // camel: LAST_POSE_CHANGE_TICK (negative while sitting; persisted)
	dashing                         bool                              // camel: the DASH flag is up
	puff                            int8                              // pufferfish: PUFF_STATE 0-2
	hasEgg                          bool                              // turtle: carrying an egg home (Turtle.HAS_EGG)
	carrotTicks                     int                               // rabbit: moreCarrotTicks (full after a bite)
	screaming                       bool                              // goat: the screaming variant (2% at spawn)
	breaksDoors                     bool                              // zombie: spawned able to break doors (f×10%)
	hidePos                         blockPos                          // skeleton: the shade it is heading for out of the sun (zero = none)
	begging                         bool                              // wolf: INTERESTED flag (head tilt) is up
	begTicks                        int                               // wolf: ticks of begging left
	begPlayer                       int32                             // wolf: who it is begging from
	begCalm                         int                               // wolf: updates until the goal is reconsidered
	sitPose                         bool                              // cat: setInSittingPose (sat on a chest/bed/furnace, not ordered)
	sitBlock                        blockPos                          // cat: the block it is heading for or sat on (zero = none)
	sitTry                          int                               // cat: MoveToBlockGoal tryTicks (up while walking, down while sat)
	sitStay                         int                               // cat: maxStayTicks
	sitNext                         int                               // cat: nextStartTick
	lying                           bool                              // cat: IS_LYING (on a bed)
	relaxOne                        bool                              // cat: RELAX_STATE_ONE (watching its owner before lying)
	relaxTicks                      int                               // cat: CatRelaxOnOwnerGoal onBedTicks (0 = goal idle)
	lieBlock                        blockPos                          // cat: CatLieOnBedGoal target (zero = none)
	lieTry, lieStay, lieNext        int                               // cat: CatLieOnBedGoal tryTicks / maxStayTicks / nextStartTick
	pandaEat                        int                               // panda: EAT_COUNTER (0 = not chewing)
	pandaSitCD                      uint64                            // panda: the tick PandaSitGoal may start again
	avoidEID                        int32                             // AvoidEntityGoal: the mob being kept clear of
	avoidX, avoidZ                  float64                           // AvoidEntityGoal: the spot it is walking to
	avoidLeft                       int                               // AvoidEntityGoal: updates left on that path (0 = idle)
	avoidWalk, avoidSprint          float64                           // AvoidEntityGoal: the rule's speed modifiers
	avoidPlayer                     bool                              // AvoidEntityGoal: avoidEID is a player, not a mob
	brzState                        int8                              // breeze: standing / inhaling / jumping / shooting
	brzTicks                        int                               // breeze: ticks into the inhale or the shot
	brzJumpCD, brzShootCD           int                               // breeze: BREEZE_JUMP_COOLDOWN / BREEZE_SHOOT_COOLDOWN
	brzShootWindow                  int                               // breeze: BREEZE_SHOOT memory ticks left
	brzJumpX, brzJumpY              float64                           // breeze: BREEZE_JUMP_TARGET
	brzJumpZ                        float64
	brzVX, brzVY, brzVZ             float64 // breeze: the jump's motion, per tick
	brzSlide                        bool    // breeze: Slide walk target set
	brzSlideX, brzSlideZ            float64
	brzSlideTicks                   int
	drinkTicks                      int      // witch: ticks left on the bottle (0 = not drinking)
	drinkKind                       int8     // witch: the potion being drunk
	ravAttackTick                   int      // ravager: AttackTick (a bite\'s pause)
	ravStunTick                     int      // ravager: StunTick (a shield stopped it)
	ravRoarTick                     int      // ravager: RoarTick (the roar lands at 10)
	overworldTicks                  int      // piglin/brute/hoglin: TimeInOverworld (zombifies past 300)
	immuneZombify                   bool     // piglin/brute/hoglin: IsImmuneToZombification
	hogPacified                     int      // hoglin: ticks of REPELLENT_PACIFY left
	hogRetreat                      int      // hoglin: AVOID_TARGET ticks left
	hogRetreatX, hogRetreatZ        float64  // hoglin: what it retreats from
	hogRepellent                    blockPos // hoglin: NEAREST_REPELLENT
	hogRepelled                     bool     // hoglin: a repellent is in range
	castLeft                        int      // spellcaster: casting-arms ticks left (DATA_SPELL_CASTING_ID)
	illSpell                        int8     // illusioner: the spell warming up
	illWarmup                       int      // illusioner: ticks until it lands
	illMirrorNext, illBlindNext     uint64   // illusioner: the tick each spell may next start
	illBlindLast                    int32    // illusioner: the last target blinded (never twice)
	traderDespawn                   int      // wandering trader + its llamas: DespawnDelay ticks left (0 = none)
	traderDrink                     int8     // wandering trader: what it is drinking (potion / milk)
	axDead                          int      // axolotl: PLAY_DEAD_TICKS left (0 = not playing dead)
	axHurt                          bool     // axolotl: a blow landed since the last update (hurtServer's roll pending)
	axHurtDmg                       float64  // axolotl: that blow's damage
	axHuntCD                        uint64   // axolotl: HAS_HUNTING_COOLDOWN until this tick
	axTarget                        int32    // axolotl: ATTACK_TARGET
	axBiteCD                        int      // axolotl: ticks until the next bite
	silverHurt                      bool     // silverfish: hurt since the last update (notifyHurt pending)
	silverWake                      int      // silverfish: lookForFriends ticks
	bearStanding                    bool     // polar bear: DATA_STANDING_ID (rearing up before a bite)
	squidHurt                       bool     // squid: hurt since the last update (spawnInk pending)
	glowDark                        int      // glow squid: DATA_DARK_TICKS_REMAINING
	endermiteLife                   int      // endermite: Lifetime ticks (discarded at 2400 unless persistent)
	shPeek                          int8     // shulker: DATA_PEEK_ID (0 closed, 30 a glimpse, 100 open)
	shPeekTicks                     int      // shulker: ShulkerPeekGoal ticks left
	shAttack                        int      // shulker: ShulkerAttackGoal attackTime
	shHurt                          bool     // shulker: hurt since the last update (the teleport roll)
	shArmored                       bool     // shulker: the covered armour has been installed once
	grazeTicks                      int      // sheep: EatBlockGoal eatAnimationTick
	striderCold                     bool     // strider: DATA_SUFFOCATING (off lava)
	golemFlower                     int      // iron golem: offerFlowerTick
	batResting                      bool     // bat: DATA_ID_FLAGS resting (hanging under a block)
	tadpoleAge                      int      // tadpole: Age (a frog at 24000)
	vexCharging                     bool     // vex: DATA_FLAGS charging
	croakLeft                       int      // frog: ticks of the CROAKING pose still to run
	frogLand                        blockPos // frog: TryFindLand's walk target
	frogLandSet                     bool
	frogLandNext, frogLandUntil     uint64   // frog: TryFindLand's next search; when it gives up the walk
	flyAimY                         float64  // a flier being led somewhere: the height it rises or sinks to
	flyAimAt                        uint64   // …the tick that was last set (stale after a few ticks)
	followBoat                      int32    // dolphin: the boat whose rider it keeps pace with (0 = none)
	followAhead                     bool     // …heading past the boat rather than to its stern
	followRecalc                    int      // …mob updates to the next re-aim
	slimeHeading                    float64  // slime/magma cube: SlimeRandomDirectionGoal's chosen heading (radians)
	llamaWolf                       int32    // llama: the wild wolf LlamaAttackWolfGoal has it spitting at
	slimeHeadingLeft                int      // …ticks before it picks another
	vexWant                         bool     // vex: VexMoveControl has a wanted point (vex.go)
	vexWX, vexWY, vexWZ, vexSpeed   float64  // …the point and the speed modifier
	vexVX, vexVY, vexVZ             float64  // …its per-tick velocity
	vexOrigin                       blockPos // …the bound origin its drift circles (the summoning evoker)
	vexHasOrigin                    bool
	vexOwner                        int32      // vex: the evoker that summoned it (0 = none)
	vexExpired                      bool       // vex: limited life run out (now taking damage)
	phantomCatAt                    uint64     // phantom: the tick of the next cat search
	phantomScared                   bool       // phantom: a cat was within sixteen at the last search
	wolfPrey                        int32      // wolf: the mob it hunts (0 = none)
	wolfBiteCD                      int        // wolf: ticks until the next bite
	goatJumpCD                      int        // goat: LONG_JUMP_COOLDOWN_TICKS
	goatJumpSet                     bool       // goat: the first cooldown has been rolled (initMemories)
	goatPrep                        int        // goat: PREPARE_JUMP_DURATION ticks left (crouched)
	goatJumping                     bool       // goat: LONG_JUMP_MID_JUMP
	goatJumpX, goatJumpY, goatJumpZ float64    // goat: the chosen landing
	goatVX, goatVY, goatVZ          float64    // goat: the jump's motion, per tick
	leaping                         bool       // LeapAtTargetGoal: mid-spring
	leapVX, leapVY, leapVZ          float64    // the spring's motion, per tick
	ghastCharge                     int        // ghast: GhastShootFireballGoal chargeTime
	blazeStep                       int        // blaze: BlazeAttackGoal attackStep
	blazeTime                       int        // blaze: attackTime
	blazeCharged                    bool       // blaze: DATA_FLAGS charged
	villagerHurt                    bool       // villager: hurt since the last update (HurtBySensor)
	villagerHurtLeft                int        // villager: HURT_BY memory ticks left
	vHurtBy                         int32      // villager: HURT_BY_ENTITY, until it calms down
	angryAt                         int32      // provoked animal: the player it holds a grudge against (NeutralMob)
	llamaDefending                  bool       // trader llama: its target is its trader's attacker, not its own
	vPanicLeft                      int        // villager: updates before its panic walk target is dropped
	cbState                         int8       // pillager: CrossbowState (uncharged / charging / charged / ready)
	cbTicks                         int        // pillager: charge ticks so far, or the aim delay left
	handActive                      bool       // LivingEntity hand-active flag (a bow drawn, a crossbow loading)
	witchHealCD                     int        // raid witch: NearestHealableRaiderTargetGoal cooldown (no player attacks meanwhile)
	raidRecruitAt                   uint64     // PathfindToRaidGoal: the tick its next recruitment sweep is due
	patrolTarget                    blockPos   // LongDistancePatrolGoal: where the patrol is headed (zero = none)
	patrolLeg                       blockPos   // …and the ten-block waypoint it is walking to right now
	patrolling                      bool       // PatrollingMonster.patrolling
	patrolCooldown                  uint64     // NAVIGATION_FAILED_COOLDOWN: no patrol steering until this tick
	wardenPose                      int32      // the warden's set-piece animation (0 = none; Pose ids)
	wardenPoseLeft                  int        // …and the updates left in it
	wardenSniffCD                   int        // TryToSniff.SNIFF_COOLDOWN, in updates
	wardenTarget                    int32      // who it last roared at (0 = nobody)
	wardenDisturb                   blockPos   // warden: DISTURBANCE_LOCATION (where it goes to look)
	wardenDisturbTil                uint64     // …remembered until this tick
	wardenTouchTil                  uint64     // warden: TOUCH_COOLDOWN
	zpHeld                          bool       // zombified piglin: anger held while it has a target
	homePos                         blockPos   // Mob.homePosition (an elder guardian's monument spot)
	homeR                           int        // …homeRadius; 0 = no home
	zpAlertIn                       int        // …updates to its next pack call (ALERT_INTERVAL)
	zpSoundIn                       int        // …ticks to its first angry grunt (FIRST_ANGER_SOUND_DELAY)
	playMate                        int32      // baby villager: the child it is chasing (0 = none)
	playFlee                        bool       // …or running away from one, toward
	playX, playZ                    float64    // …this spot
	trusted                         [2]string  // fox: the players it trusts (DATA_TRUSTED_ID_0/1), by name; persisted
	dolphinSwimmer                  int32      // dolphin: the swimming player it keeps company (0 = none)
	dolphinPlayEID                  int32      // dolphin: the floating item it is playing with (0 = none)
	doorPos                         blockPos   // zombie: the door it is beating on (lower half; zero = none)
	doorTicks                       int        // zombie: ticks spent on it
	eggPos                          blockPos   // zombie: the turtle-egg clutch it is after (zero = none)
	eggNext                         int        // zombie: ticks until the next egg search
	eggTry                          int        // zombie: ticks spent trying to reach the clutch
	eggStamp                        int        // zombie: ticks spent stamping on it
	doorStage                       int8       // zombie: the crack stage last shown (-1 = none)
	hornsGone                       int8       // goat: horns lost to ramming (0-2; one in ten spawns with one gone)
	ramCD                           int        // goat: ticks before it may ram again
	ramPhase                        int8       // goat: idle / walking to its start / lowering its head / charging
	ramStart                        blockPos   // goat: where the charge begins
	ramTX, ramTZ                    float64    // goat: the target's position when the ram was chosen
	ramDX, ramDZ                    float64    // goat: the charge direction
	ramTicks                        int        // goat: ticks in the current phase
	raidTarget                      blockPos   // rabbit: the farmland it is raiding
	raidRest                        int        // rabbit: ticks before it looks for a garden again
	layCounter                      int        // turtle: ticks spent digging the nest
	inflate, deflate                int        // pufferfish: its inflate and deflate clocks (ticks)
	stingCD                         int        // pufferfish: ticks before it stings again
	offhand                         invStack   // piglin: the gold it is admiring (rendered in the off hand)
	admireUntil                     uint64     // piglin: the tick the admiring ends (0 = not admiring)
	admireOffUntil                  uint64     // piglin: no admiring until this tick (hit by a player)
	hoard                           []invStack // piglin: loved items it kept; dropped on death
	gotFish                         bool       // dolphin: fed a fish, leading to treasure
	treasureX                       int        // dolphin: the shipwreck it leads to (valid while gotFish)
	treasureZ                       int
	foxFlags                        int8       // fox: DATA_FLAGS (crouching 4, interested 8, pouncing 16, sleeping 32)
	foxEatTicks                     int        // fox: ticks since it last ate (eats a held food past 600)
	foxSleepIn                      int        // fox: ticks of quiet before it lies down
	armState                        int8       // armadillo: 0 idle, 1 rolling, 2 scared, 3 unrolling (DATA_STATE)
	armStateAt                      uint64     // armadillo: the tick the state began
	armDangerUntil                  uint64     // armadillo: DANGER_DETECTED_RECENTLY expiry
	armScuteAt                      uint64     // armadillo: the tick the next scute drops (0 = unset)
	sniffState                      int8       // sniffer: 0 idle, 1 walking to a dig site, 2 digging
	sniffStart                      uint64     // sniffer: the tick the dig began
	sniffUntil                      uint64     // sniffer: the tick the dig ends
	sniffCD                         int        // sniffer: ticks before it sniffs again (9600 after a dig)
	sniffTarget                     blockPos   // sniffer: the block it digs (the floor block)
	sniffExplored                   []blockPos // sniffer: the last 20 dig sites, never dug twice
	nautAngryAt                     int32      // nautilus: ANGRY_AT, whoever last hurt it (player or mob eid)
	nautAngryUntil                  uint64     // nautilus: the tick ANGRY_AT expires
	nautTarget                      int32      // nautilus: ATTACK_TARGET (player or mob eid; 0 = none)
	nautTargetCD                    int        // nautilus: ATTACK_TARGET_COOLDOWN, ticks before it looks for a pufferfish
	chargeCD                        int        // nautilus: CHARGE_COOLDOWN_TICKS
	charging                        bool       // nautilus: mid-ChargeAttack
	chargeSX, chargeSY, chargeSZ    float64    // nautilus: where the charge began
	chargeVX, chargeVY, chargeVZ    float64    // nautilus: the charge's fixed velocity, per tick
	ty                              float64    // hunted target's feet height (fliers dive to it)
	living                                     // attributes + status effects, shared with players
	dmgFrac                         float64    // fractional damage carry (vanilla HP is float, ours int)
	attackCD                        int        // mob-updates left before this mob can melee again
	hasTarget                       bool       // a player is within aggro range this update
	portalCool                      int        // ticks before it may take a portal again (Entity.portalCooldown)
	guardMoving                     bool       // guardian: DATA_ID_MOVING as last broadcast (spikes folded)
	temper                          int        // horse/donkey/mule: how close it is to giving in (0..100)
	seeTime                         int        // ranged goals: ticks the target has been in (positive) or out of (negative) sight
	targetEID                       int32      // hostile: the player it hunts (TargetGoal's target; 0 = none)
	unseenTicks                     int        // hostile: ticks that player has been out of sight
	tempted                         bool       // following a player's held food (temptStep)
	temptCalm                       int        // updates left before it can be tempted again
	temptPX, temptPZ, temptPY       float64    // cat/ocelot: where the tempting player stood (canScare)
	temptYaw, temptPitch            float32    // …and how they were facing
	heartBound                      bool       // creaking: a standing heart is keeping it alive
	heartHit                        bool       // …and it took a blow the heart must answer for
	frozen                          bool       // creaking: a player is watching, so it cannot move
	tx, tz                          float64    // that target's position (set by acquireTarget)
	dim                             int        // dimension this mob lives in (0 overworld, 1 nether)
	preyTarget                      int32      // the creature it hunts when no player is near (0 = none)
	converting                      int        // zombie villager: ticks left in its cure (0 = not curing)
	curer                           string     // zombie villager: who fed it the golden apple
	gossipAt                        uint64     // villager: tick of its last chat (Villager.lastGossipTime)
	gossipDecayAt                   uint64     // villager: tick its gossip last faded (a day apart)
	giftAt                          uint64     // villager: tick its next Hero of the Village gift may be thrown
	profession                      int        // villager: index into professionNames/villagerTrades
	tradeLevel                      int        // villager merchant tier 1-5 (novice..master)
	tradeXP                         int        // trade experience toward the next tier
	offers                          []mobOffer // this villager's unlocked trades (+ per-offer uses)
	restocksToday                   int        // villager: restocks done this day (vanilla ≤2/day)
	merchantTimer                   int32      // villager: ticks left on Villager.updateMerchantTimer
	levelUpPending                  bool       // villager: increaseProfessionLevelOnUpdate
	lastRestockTick                 uint64     // villager: tick of the last restock (2400-tick spacing gate)
	lastWorkCheck                   uint64     // villager: WorkAtPoi's lastCheck (every 300 ticks at most)
	showTrades                      tradeShow  // villager: ShowTradesToPlayer's run (villagershowtrades.go)
	lastRestockDay                  uint64     // villager: day count at the last shouldRestock check
	gossip                          gossipBook // villager: what it holds about each player (persisted)
	home                            blockPos   // villager house / golem well — the anchor to drift back to

	ovrSpeed    float64 // >0: plugin speed override — survives behavior-driven speed resets
	ovrDamage   float64 // >0: plugin melee-damage override (hostileMelee honors it)
	uuid        [16]byte
	x, y, z     float64
	yaw         float32
	syaw        float32 // last broadcast head yaw (only resend on change)
	headYaw     float32 // where the head is pointed (LookControl); the body yaw when nothing is watched
	sheadYaw    float32 // last broadcast head yaw
	wasWet      bool    // last step's water state, for the splash on entry
	lookTicks   int32   // ticks left on a look goal (0 = neither is running)
	lookEID     int32   // the player being watched (0 = a fixed direction)
	lookDX      float64 // RandomLookAroundGoal's direction
	lookDZ      float64
	vx, vz      float64
	vy          float64  // vertical velocity (swimmers/fliers only)
	geyserFly   bool     // lifted by a geyser's column (potentsulfur.go) until it comes down
	geyserVY    float64  // …its vertical motion per tick
	geyserFall  float64  // …and the fall distance it has built up
	pushX       float64  // crowding shove (push.go), held apart from the steering
	pushZ       float64  // velocity so a shoved mob does not turn to face the shove
	cramCD      int      // mob-updates until this mob can take cramming damage again
	leash       int32    // eid holding this mob's lead (player or knot); 0 = free
	leashPos    blockPos // the fence knot's block, when the holder is one (persisted)
	sx, sy, sz  float64  // last broadcast position (for delta moves)
	moveDist    float32  // Entity.moveDist: 0.6 × distance walked, for footsteps
	ambientTime int32    // Mob.ambientSoundTime: the idle-voice counter (negative right after a call)
	nextStep    float32  // Entity.nextStep: the moveDist at which the next footstep plays (0 = fresh: 1)
}

// spawnMobIn creates a server-controlled entity in a dimension and shows it
// to nearby players. It defaults to neutral wander; callers (or the bus)
// assign a group behavior. Returns nil when a plugin MobSpawnEvent handler
// cancels the spawn. There is no overworld shorthand: the ones this and its
// siblings had (spawnMob, spawnItem, playSound, spawnXPOrb) put Nether and
// End mobs, drops and sounds in the overworld wherever a caller forgot it
// was not there. Tests keep them as overworld fixtures (overworld_test.go). The reported
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
	m := &mob{living: living{attrs: newMobAttributes(etype)}, eid: eid, etype: etype, dim: dim, behavior: wanderBehavior{}, health: mobHealth(etype), x: x, y: y, z: z, sx: x, sy: y, sz: z, spawnTick: h.tick.Load()}
	binary.BigEndian.PutUint32(m.uuid[12:], uint32(eid)) // unique enough for the client
	if etype == entitySheep {
		m.color = h.rollSheepColor() // vanilla's spread: mostly white, pink 1-in-600
	}
	h.rollVariant(m) // frog by biome, axolotl by the 1-in-1200 blue roll
	if etype == entitySulfurCube && !h.reloading {
		h.initSulfurCube(m, false) // setSpawnSize, whatever brought it (an egg, /summon, a split resizes it after)
	}
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
	// The spawn itself is not broadcast: syncTracking announces it to the
	// players who can see it, in full, on the next pass (entityview.go).
	return m
}

// updateMobs steps each mob's behaviour and broadcasts its movement. The behavior
// decides the desired velocity; the hub applies the shared physics (momentum, speed
// cap, terrain collision) so every primitive moves consistently.
func (h *hub) updateMobs(players map[int32]*tracked) {
	h.updateHerdTargets()
	h.pushMobs(players) // crowding: mobs standing in one another shove apart
	for _, m := range h.mobs {
		if m.invulnTicks > 0 {
			m.invulnTicks -= mobMoveInterval
		}
		if m.armorNote != 0 {
			h.wolfArmorNote(players, m)
		}
		if m.etype == entityPanda && m.baby && m.dying == 0 {
			h.pandaSneezeTick(players, m)
		}
		if m.etype == entityPiglin && m.admireUntil != 0 && m.dying == 0 {
			h.piglinAdmireTick(players, m)
		}
		if m.etype == entityArmadillo && m.dying == 0 {
			h.armadilloTick(players, m)
		}
		if m.etype == entitySniffer && m.dying == 0 {
			h.snifferInterrupt(players, m) // a panic, a temptation or courting ends a sniff or a dig
		}
		if m == h.dragon {
			continue // the dragon flies on updateDragon's physics alone —
			//          shared gravity/ground-snap would pin it into the island
		}
		if m.dying > 0 { // playing the death animation — hold still, then despawn + drop
			if m.dying -= mobMoveInterval; m.dying <= 0 {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusPoof)) // LivingEntity.tickDeath
				h.despawnMob(players, m)
			}
			continue
		}
		// LivingEntity.aiStep's water check runs whatever the mob is doing.
		if waterSensitive(m.etype) && h.waterSensitiveTick(players, m) {
			continue // hurt to death, or an enderman teleported out of the wet
		}
		if m.mount != 0 { // riding another mob (raid ravager rider, jockey)
			v := h.mobs[m.mount]
			if v == nil || v.dying > 0 {
				m.mount, m.mountDrives, m.navMount = 0, false, nil // vehicle gone — dismount and resume as a normal mob
			} else if !m.mountDrives {
				// Glued to the vehicle: the client renders us seated from the
				// passengers frame, so the server just keeps us co-located and
				// skips independent movement — a skeleton jockey (or a camel
				// husk's parched) still shoots.
				m.x, m.y, m.z, m.dim = v.x, v.y+mountRideHeight, v.z, v.dim
				if m.hostile && skeletonKind(m.etype) {
					h.acquireTarget(players, m)
					h.skeletonShoot(players, m)
				}
				continue
			}
		}
		if m.mobRider != 0 { // carrying a mob
			for _, id := range m.mobPassengers() {
				if r := h.mobs[id]; r == nil || r.dying > 0 || r.mount != m.eid {
					h.freeMobSeat(players, m, id)
				}
			}
			if r := h.mobs[m.mobRider]; r != nil && r.mountDrives {
				m.x, m.y, m.z, m.dim = r.x, r.y, r.z, r.dim // the rider leads: carried under it
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
		if m.hasBody() {
			continue // a sulfur cube carrying a block has no goals: it is a ball (updateSulfurCubes moves it)
		}
		if isCamelKind(m.etype) && m.dashCD > 0 {
			h.camelDashTick(players, m)
			h.nautilusDashTick(players, m)
		}
		if _, ok := zombifiesOutside(m.etype); ok {
			h.zombifyTick(players, m) // out of the Nether, three hundred ticks and it turns
			if h.mobs[m.eid] == nil {
				continue
			}
		}
		if isCamelKind(m.etype) && m.rider == 0 && m.mobRider == 0 && h.camelSitStep(players, m) {
			continue // sat, folding or rising: refuseToMove (a ridden camel's client does this itself)
		}
		if m.etype == entityPufferfish {
			h.pufferStep(players, m)
		}
		if m.etype == entitySnowGolem {
			h.snowGolemStep(players, m)
		}
		if isEquine(m.etype) {
			h.horseStandTick(players, m) // rearing: its countdown and RandomStandGoal
		}
		if m.rider != 0 || len(m.riders) > 0 {
			// RunAroundLikeCrazyGoal is the one goal a ridden mount still
			// runs: an untamed horse is deciding whether to keep its rider.
			h.horseRideTick(players, m)
			continue // otherwise a ridden mount is client-driven (applyMountMove)
		}
		if m.spawnInvuln > 0 {
			continue // wither charging its spawn: hold still until updateWithers releases it
		}
		if m.frozen {
			m.vx, m.vz = 0, 0
			continue // a watched creaking is a statue
		}
		if m.tamed && !nautilusKind(m.etype) { // a pet follows its owner (or sits); a nautilus has no FollowOwner
			if h.petAcquire(players, m) {
				m.vx, m.vz = 0, 0 // sitting: stay put
				continue
			}
			if m.hasTarget {
				m.rest = 0
			}
		} else if m.hostile {
			if !h.provokedTarget(players, m) { // a provoked animal keeps to its attacker, then calms
				h.acquireTarget(players, m) // pick a player to hunt this update
			}
			if m.hasTarget {
				m.rest = 0                               // a resting hostile wakes the instant prey appears
				m.drifting, m.drownedGoal = false, false // a real quarry outranks any errand
			}
		}
		// AvoidEntityGoal — the mob-class registrations (a skeleton and a
		// wolf, a creeper and a cat, …) and the player ones (a rabbit, fox,
		// wild cat or ocelot, an evoker): pick a spot on the far side.
		h.avoidScan(players, m)
		// Villagers run a daily schedule: at night they lie in their bed (held
		// still); by day they open the wooden door in their way BEFORE the step
		// below, so an open door (not a wall) is what the walk test sees this tick.
		if t := h.tradingPartner(players, m); t != nil {
			// LookAndFollowTradingPlayerSink: a villager with its trade screen
			// open stands where it is and faces the customer — it does not
			// wander off to its bed or its workstation mid-deal.
			m.vx, m.vz = 0, 0
			m.headYaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
			m.yaw = m.headYaw
			continue
		}
		if m.etype == entityVillager {
			// Not reached while trading: the branch above continues out.
			h.villagerMerchantTick(players, m)
			h.villagerWorkTick(players, m)       // WorkAtPoi: at the job site in working hours
			h.villagerShowTradesTick(players, m) // ShowTradesToPlayer: holding up a trade to a player nearby
		}
		h.updateCelebration(players, m) // a lost raid's raiders with nothing to fight cheer
		h.updateHoldGround(players, m)  // a patrol's raider staring down a far target
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
		case m.etype == entitySulfurCube:
			h.sulfurHop(players, m) // the same hops, steered by its tempt and block-seeking goals
		case isLlama(m.etype) && h.caravanStep(m):
			// A llama following the one ahead of it in a caravan (priority 2,
			// above its panic).
		case (m.etype == entitySquid || m.etype == entityGlowSquid) && h.squidStep(players, m):
			// A squid jetting away from whatever hurt it.
		case m.etype == entityPiglin && h.piglinAvoidStep(players, m):
			// A piglin backing away from a soul light or a zombified piglin.
		case (spearWielder(m) || m.spearGoal != nil) && h.spearGoalStep(players, m):
			// A zombie, zombified piglin or piglin with a spear: closing,
			// charging with the spear lowered, and wheeling off for the next pass.
		case m.etype == entityPiglin && h.piglinCelebrateStep(players, m):
			// A piglin going to where its target fell, dancing after a hoglin.
		case m.etype == entityPiglinBrute && h.bruteIdleStep(m):
			// An idle brute keeping to its bastion: home, its fellows, a stroll.
		case (findsWater[m.etype] || m.etype == entityStrider) && h.findWaterStep(m):
			// A stranded water animal heading back to the water, or a strider
			// off the lava heading back to it.
		case schoolingFish[m.etype] && h.schoolStep(players, m):
			// A fish swimming after its shoal's leader.
		case (m.etype == entityGuardian || m.etype == entityElderGuardian) && h.guardianHomeStep(m):
			// A guardian outside its home swimming back (MoveTowardsRestriction).
		case m.etype == entityDrowned && h.drownedWaterStep(players, m):
			// A drowned going back to the water by day, or ashore after dark.
		case zombieKind(m.etype) && h.villageDriftStep(players, m):
			// A zombie walking through the village it stands in, after dark.
		case (m.etype == entityZoglin || m.etype == entityEnderman ||
			(m.etype == entityVindicator && m.customName == "Johnny")) && h.mobHuntStep(players, m):
			// A zoglin after anything living, an enderman after an endermite,
			// a vindicator named Johnny after everything.
		case m.etype == entityVillager && h.villagerPanicStep(players, m):
			// A villager running from a zombie, a pillager, or whatever hurt it.
		case m.etype == entityVillager && m.baby && h.villagerPlayStep(players, m):
			// Baby villagers playing tag with the other children. Below panic:
			// a frightened child runs from the zombie, not after its friend.
		case m.etype == entityVillager && h.villagerPickupStep(players, m):
			// A villager after a dropped item it wants (seeds, crops, bread).
		case m.etype == entityVillager && h.farmerBonemealStep(players, m):
			// A farmer feeding a growing crop bone meal.
		case m.etype == entityVillager && h.farmerStep(players, m):
			// A farmer harvesting and sowing its field.
		case m.etype == entityVillager && h.villagerJobWalk(players, m):
			// An unemployed villager walking to a free workstation, and claiming it.
		case m.etype == entityVillager && h.villagerBreedStep(players, m):
			// Two fed villagers courting, and a child if a bed is free.
		case m.etype == entityWolf && h.wolfHuntStep(players, m):
			// A wolf after a sheep, a skeleton, or whatever hurt its owner.
		case m.etype == entityPolarBear && h.bearFoxStep(players, m):
			// An adult polar bear after a fox it has seen.
		case m.etype == entityBat && h.batStep(players, m):
			// A bat hanging under a block.
		case m.etype == entityIronGolem && h.golemOfferTick(players, m):
			// A golem holding out its poppy to a villager.
		case m.etype == entitySheep && h.grazeStep(players, m):
			// A sheep with its head down in the grass.
		case m.etype == entitySilverfish && h.silverfishStep(players, m):
			// A silverfish burrowing into stone.
		case m.etype == entityAxolotl && h.axolotlStep(players, m):
			// An axolotl playing dead, or hunting.
		case (m.etype == entityWanderingTrader || m.etype == entityTraderLlama) && h.traderStep(players, m):
			// A trader drinking by the clock, or leaving when its time is up.
		case m.etype == entityHoglin && h.hoglinStep(players, m):
			// A hoglin walking off from warped fungus, or retreating from piglins.
		case m.etype == entityWarden && h.wardenStep(players, m):
			// A warden emerging, roaring, sniffing or burrowing stands still.
		case m.etype == entityRavager && h.ravagerStep(players, m):
			// A ravager stunned, roaring or mid-bite stands still.
		case m.etype == entityBreeze && h.breezeStep(players, m):
			// A breeze sliding, drawing breath, mid-jump or shooting.
		case m.holdingGround && h.holdGroundStep(m):
			// A patrolling pillager standing its ground, watching its target.
		case m.celebrating && h.celebrateStep(players, m):
			// A raider cheering the raid it won: standing, jumping, calling out.
		case h.raidPathStep(players, m):
			// A raider walking back to the raid it belongs to, gathering any
			// idle raider it passes on the way.
		case h.patrolStep(players, m):
			// A pillager patrol crossing the country toward its distant
			// target, the captain plotting the legs.
		case h.avoidStep(players, m):
			// Keeping clear of a mob its kind avoids, at the goal's pace.
		case nautilusKind(m.etype) && h.nautilusFightStep(players, m):
			// A nautilus charging whoever hurt it, or a pufferfish: the FIGHT
			// activity outranks its panic, which only walks it away.
		case m.panic > 0 || m.panicHasT:
			// Spooked: PanicGoal runs to one random spot after another
			// (DefaultRandomPos.getPos 5, 4) at the species' panic speed
			// while the hurt is under forty ticks old (shouldPanic), and
			// finishes the leg it is on when it runs out
			// (canContinueToUse: the path is not done).
			if m.panic > 0 {
				m.panic--
			}
			if m.panicHasT {
				if m.panicLeg++; m.panicLeg > panicLegMax || math.Hypot(m.panicTX-m.x, m.panicTZ-m.z) < 1 {
					m.panicHasT = false
				}
			}
			if !m.panicHasT && m.panic > 0 {
				m.panicTX, m.panicTZ, m.panicHasT = h.panicTarget(m)
				m.panicLeg = 0
			}
			if m.panicHasT {
				var vx, vz float64
				if m.flies || m.swims {
					vx, vz = straightSteer(m, m.panicTX, m.panicTZ, 0.1) // no ground path through water or air
				} else {
					vx, vz = h.pathSteer(m, m.panicTX, m.panicTZ)
				}
				m.vx, m.vz = vx*panicSpeed(m.etype), vz*panicSpeed(m.etype)
			} else {
				m.vx, m.vz = 0, 0 // nowhere to run: it stands (the goal has no target)
			}
		case h.breedApproachStep(m):
			// A courting animal walking to its mate (BreedGoal /
			// AnimalMakeLove): behind panic, ahead of temptation.
		case m.etype == entityPanda && h.pandaStep(players, m):
			// A panda held by its personality: sitting out a storm, lying
			// on its back, or tumbling.
		case m.etype == entityTurtle && h.turtleStep(players, m):
			// A turtle carrying an egg home, or digging its nest.
		case m.etype == entityCat && h.catRelaxStep(players, m):
			// A cat settling at the foot of its sleeping owner's bed.
		case m.etype == entityNautilus && h.nautilusLoveStep(m):
			// A courting nautilus swimming to its mate (AnimalMakeLove, 0.4),
			// ahead of its temptation.
		case h.temptStep(players, m):
			// Walking after a player's held food (TemptGoal / FollowTemptation):
			// behind panic, ahead of a baby's parent and the species' own errands.
		case m.etype == entityHappyGhast && h.ghastlingFollowPlayer(players, m):
			// A ghastling drifting after the nearest player (HappyGhastAi).
		case m.etype == entityRabbit && h.rabbitStep(players, m):
			// A rabbit after a grown carrot in somebody's garden.
		case m.etype == entityGoat && h.goatStep(players, m):
			// A goat lining up, lowering its head for, or charging a ram.
		case m.etype == entityGoat && h.goatJumpStep(players, m):
			// A goat crouched for, or mid-way through, a long jump.
		case skeletonKind(m.etype) && h.fleeSunStep(players, m):
			// A burning skeleton with nobody to shoot heading for shade.
		case zombieKind(m.etype) && h.zombieEggStep(players, m):
			// A zombie after a clutch of turtle eggs (above the hunt, as vanilla ranks it).
		case m.etype == entityCat && h.catLieStep(players, m):
			// A tamed cat walking to, or lying on, any bed.
		case m.etype == entityCat && h.catSitStep(players, m):
			// A tamed cat walking onto, or sat on, a chest, bed or lit furnace.
		case (m.etype == entityCat || m.etype == entityOcelot) && h.catHuntStep(players, m):
			// A wild cat after a rabbit, an ocelot after a chicken.
		case m.etype == entityDolphin && h.dolphinBreathe(players, m):
			// A dolphin low on air making for the surface.
		case m.etype == entityDolphin && h.dolphinStep(players, m):
			// A fed dolphin leading the way to a shipwreck.
		case m.etype == entityDolphin && h.dolphinSwimWithPlayer(players, m):
			// A dolphin keeping a swimmer company, Dolphin's Grace and all.
		case m.etype == entityDolphin && h.dolphinJumpStart(players, m):
			// A dolphin at the surface leaping clear of the water.
		case m.etype == entityDolphin && h.dolphinPlay(players, m):
			// A dolphin tossing a floating item about.
		case m.etype == entityDolphin && h.dolphinFollowBoat(players, m):
			// A dolphin racing a boat a player is rowing.
		case m.etype == entityFox && h.foxStep(players, m):
			// A fox asleep, stalking prey, or after a dropped item.
		case m.etype == entityArmadillo && m.armState != 0:
			m.vx, m.vz = 0, 0 // rolled up: it stays where it is
		case m.etype == entitySniffer && h.snifferStep(players, m):
			// A sniffer scenting, sniffing, walking to a scent, digging at it
			// or getting up again.
		case m.etype == entityFrog && h.frogStep(players, m):
			// A frog after a small slime or magma cube (FrogAi's tongue).
		case m.etype == entityFrog && h.frogFindLandStep(m):
			// A frog in the water making for the nearest bank (TryFindLand).
		case m.etype == entityFrog && m.croakLeft > 0 && h.frogCroakStep(players, m):
			// A frog croaking, still, for its sixty ticks (FrogAi's Croak).
		case m.etype == entityAllay && h.allayStep(players, m):
			// An allay with a job: collecting matching drops, delivering them,
			// or keeping near the player who handed it its item.
		case m.baby && h.followParentStep(m):
			// A baby trailing the nearest adult of its kind (FollowParentGoal /
			// BabyFollowAdult): steered straight at it, ahead of idling and
			// strolling, behind panic and knockback.
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
			// A pet after its owner is on FollowOwnerGoal, which outranks
			// strolling just as a hunt does; left out, a pet parked in the
			// idle cycle and only caught up through the teleport.
			// Courting is not here: with a mate about, breedApproachStep
			// walks it over; with none, BreedGoal never starts and the
			// animal strolls and idles like any other.
			busy := (m.hostile && m.hasTarget) || (m.tamed && m.hasTarget)
			if m.stroll <= 0 && !busy {
				m.rest = restMin + h.rng.Intn(restMax-restMin)
				if m.etype == entityFrog {
					h.frogIdleCroak(players, m) // the idle RunOne's pick: croak or just pause
				}
				if m.hostile {
					m.rest *= 2
				}
				continue
			}
			if m.stroll > 0 {
				m.stroll--
			}
			dvx, dvz := m.behavior.steer(h, m)
			// The attack goal's own speed modifier: a skeleton swinging a
			// sword closes at 1.2 of its pace (MeleeAttackGoal(1.2)).
			chase := 1.0
			if busy && m.hostile {
				chase = chaseSpeedMod(m)
			}
			dvx, dvz = dvx*chase, dvz*chase
			m.vx = m.vx*0.85 + dvx*0.15 // momentum → smooth
			m.vz = m.vz*0.85 + dvz*0.15
			// An amble runs at the stroll goal's own speed — a ravager
			// lumbers at 0.4 of its pace, a horse at 0.7 — while a mob with
			// somewhere to be (a hunt, a mate) moves at its full speed.
			cap := m.moveSpeed() * chase
			if !busy {
				cap *= h.strollSpeedFor(m)
			}
			if math.Hypot(m.vx, m.vz) > cap {
				sp := math.Hypot(m.vx, m.vz)
				m.vx, m.vz = m.vx/sp*cap, m.vz/sp*cap
			}
		}

		h.leapCheck(players, m) // LeapAtTargetGoal: a spider, wolf, cat, ocelot or fox springs at its target
		h.applyFluidPush(m)     // a current carries whatever is standing in it

		// Move, by locomotion mode: walkers collide with terrain, fliers float
		// free, swimmers stay inside their water column, anchored mobs hold.
		// The crowding shove rides on top of the steering: vanilla applies it
		// after the AI has moved (pushEntities is the tail of aiStep), and it
		// deliberately escapes the speed clamp above — squeezing out of a
		// packed pen is meant to outrun a walk.
		// Block.getSpeedFactor: soul sand and honey take a walker down to 0.4×
		// (Entity.getBlockSpeedFactor reads the feet cell, else the one below).
		sf := 1.0
		if !m.flies && !m.swims {
			sf = h.mobSpeedFactor(m)
		}
		sf *= h.webFactor(m)
		if m.flies {
			sf *= m.flyingFactor() // FlyingMoveControl: airborne, FLYING_SPEED sets the pace
		}
		nx, nz := m.x+m.vx*sf+m.pushX, m.z+m.vz*sf+m.pushZ
		fnx, fnz := int(math.Floor(nx)), int(math.Floor(nz))
		switch {
		case m.statik:
			m.vx, m.vz = 0, 0 // anchored (shulker)
		case m.geyserFly:
			m.vx, m.vz = 0, 0 // lifted by a geyser: geyserFlights moves it, tick by tick
		case m.leaping:
			h.leapFlight(players, m) // LeapAtTargetGoal's spring, gravity and all
		case m.etype == entityGoat && m.goatJumping:
			h.goatFlight(players, m) // the long jump's arc
		case m.etype == entityBreeze && m.brzState == brzJumping:
			h.breezeFlight(players, m) // the long jump's arc, gravity and all
		case m.etype == entityVex:
			h.vexFlight(players, m) // wanted-point flight through blocks, charges and drifts
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
				// A hunting zombie stopped by a closed wooden door beats on it
				// (BreakDoorGoal) instead of picking a new heading.
				if m.breaksDoors && m.hasTarget {
					if door, ok := h.doorAhead(m, nx, nz); ok && h.zombieBeatsDoor(players, m, door) {
						break
					}
				}
				ang := h.rng.Float64() * 2 * math.Pi
				m.vx, m.vz = math.Cos(ang)*m.moveSpeed(), math.Sin(ang)*m.moveSpeed()
				m.reroute = 15 + h.rng.Intn(15)
			}
			if stepOK && m.doorPos != (blockPos{}) {
				h.zombieStopDoor(players, m) // walked on: the door is no longer in the way
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
			fx, fz := int(math.Floor(m.x)), int(math.Floor(m.z))
			floor := float64(h.worldFor(m.dim).MobFeetFrom(fx, fz, int(math.Floor(m.y))))
			if lvl := m.hasEffect(effLevitation); lvl > 0 {
				// LivingEntity.travel under Levitation: each tick dy eases
				// toward 0.05 × level (dy += (target − dy) × 0.2) and nothing
				// pulls it down; a ceiling stops the rise. When the effect
				// ends the floor below takes it back — and the fall hurts.
				ht := m.box().h
				for i := 0; i < mobMoveInterval; i++ {
					m.vy += (0.05*float64(lvl) - m.vy) * 0.2
					top := int(math.Floor(m.y + m.vy + ht))
					if worldgen.Collides(h.worldFor(m.dim).At(fx, top, fz)) {
						m.vy = 0
						break
					}
					m.y += m.vy
				}
			} else if fl, ok := h.floatLevel(m, fx, fz, floor); ok && mobFloats(m) {
				// FloatGoal / Swim: in deep water it bobs up until its eyes clear the surface.
				if m.y < fl {
					m.y = math.Min(fl, m.y+floatRisePerUpd)
				} else {
					m.y = fl
				}
			} else if floor < m.y && m.gravity() <= 0 {
				// No GRAVITY (or less than none): nothing pulls it down onto
				// the lower floor, so it stays where it is, as a vanilla mob
				// on /attribute gravity 0 hangs in the air.
			} else {
				m.y = floor
				fell := oldY - m.y
				if fell > 0.5 {
					h.mobTrample(players, m, fell) // FarmlandBlock.fallOn
				}
				if fell > m.safeFallDistance() { // the ground dropped out under it
					h.mobFall(players, m, fell)
				}
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
		// LookAtPlayerGoal / RandomLookAroundGoal: the head watches a player
		// or glances around while the body goes on about its business.
		h.idleLook(players, m)

		// Only emit when the mob actually moved — broadcasting a no-op move
		// every tick for every mob overflows slow clients' send queues. The
		// event carries the absolute position; each viewer's renderer derives
		// its own relative deltas.
		if m.x != m.sx || m.y != m.sy || m.z != m.sz {
			h.mobFootsteps(players, m, m.x-m.sx, m.y-m.sy, m.z-m.sz)
			// A walking or swimming mob's STEP/SWIM vibration (Entity.move →
			// gameEvent), throttled like a player's; fliers make none.
			if (m.x != m.sx || m.z != m.sz) && !m.flies && !flyerSpecies(m.etype) && m.dim == dimOverworld && h.tick.Load() >= h.sculkStep[m.eid] {
				h.sculkStep[m.eid] = h.tick.Load() + 3
				h.vibStep(m.dim, m.x, m.y, m.z, m.eid)
			}
			m.sx, m.sy, m.sz = m.x, m.y, m.z
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		}
		// Facing only when it changed meaningfully (saves packets). BOTH the head
		// rotation and a zero-delta move ride the same latch: the move carries the
		// BODY yaw, without which a mob turning in place (tracking a target while
		// stationary) renders with a frozen torso — visible as a direction snap at
		// a shard crossing, where the shadow pipeline had been streaming the live
		// body yaw all along.
		if math.Abs(float64(m.yaw-m.syaw)) > 8 {
			m.syaw = m.yaw
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		}
		// The head is its own rotation: a mob watching a player turns it
		// without turning the body (vanilla's ClientboundRotateHeadPacket).
		if math.Abs(float64(m.headYaw-m.sheadYaw)) > 8 {
			m.sheadYaw = m.headYaw
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entHead(m.eid, m.headYaw))
		}
		if m.etype == entityEnderDragon {
			continue // the dragon flies on its own update (updateDragon)
		}
		if m.etype == entityIronGolem {
			h.golemMelee(players, m) // the guardian punches hostiles (not hostile itself)
		}
		if m.etype == entityEnderman {
			h.endermanCarry(players, m) // pick up / put down blocks (even while neutral)
			if h.endermanStareStep(players, m) {
				continue // EndermanFreezeWhenLookedAt: held by the stare
			}
		}
		if m.etype == entityFrog {
			h.frogLaySpawn(players, m) // a pregnant frog drops its clutch on the water beside it
		}
		h.updateAggression(players, m) // the zombie family's arms go up while it chases
		if m.etype == entityZombifiedPiglin {
			h.zombifiedPiglinAngerTick(players, m) // the angry pace and the first grunt
		}
		if m.etype == entityWolf {
			h.begStep(players, m) // head tilt at a held bone or meat (look only)
		}
		if m.etype == entityPolarBear {
			h.polarBearStep(players, m) // guarding a cub, rearing up before a bite
		}
		if m.etype == entityStrider {
			h.striderShiverTick(players, m) // cold off lava: the shiver and the slow walk
		}
		if m.etype == entityParrot {
			h.parrotImitateTick(players, m) // a monster's call, now and then
			if h.parrotLandOnShoulder(players, m) {
				continue // it rides its owner's shoulder now, not the world
			}
		}
		if m.etype == entityTadpole {
			h.tadpoleTick(players, m) // growing up
			if h.mobs[m.eid] == nil {
				continue
			}
		}
		if m.etype == entityPhantom && m.hasTarget && h.phantomFearsCats(players, m) {
			m.hasTarget = false // PhantomSweepAttackGoal: a cat about, the swoop is off
		}
		if m.etype == entityEndermite {
			h.endermiteTick(players, m) // two minutes to live
			if h.mobs[m.eid] == nil {
				continue
			}
		}
		if !m.hostile && isLlama(m.etype) {
			h.llamaWolfTick(players, m) // LlamaAttackWolfGoal: spits at wild wolves
		}
		if m.hostile {
			switch m.etype {
			case entitySkeleton, entityStray, entityBogged, entityParched:
				h.bowDrawTick(players, m)   // the pull before the shot
				h.skeletonShoot(players, m) // ranged: arrows from bow distance
			case entityPillager:
				if !m.holdingGround { // the crossbow goal waits out the stand-off
					h.pillagerTick(players, m) // the crossbow: draw, aim, fire
				}
			case entityIllusioner:
				h.illusionerTick(players, m) // mirror and blindness spells, then the bow
			case entityBlaze:
				h.blazeTick(players, m) // the flare, the volley of three, the rest
			case entityGhast:
				h.ghastTick(players, m) // the twenty-tick charge, the fireball, the rest
			case entityWither:
				h.witherShoot(players, m) // ranged: wither skulls
			case entityShulker:
				h.shulkerTick(players, m) // the shell, the bullets, the teleport
			case entityLlama, entityTraderLlama:
				h.llamaSpit(players, m) // ranged: the spit IS the llama's only attack
			case entityEvoker:
				h.evokerCast(players, m) // fangs + vex summoning
			case entityVex:
				// VexChargeAttackGoal: the blow lands in vexFlight, on contact.
			case entityWarden:
				h.wardenTick(players, m) // darkness aura + sonic boom + dig-away
			case entityGuardian, entityElderGuardian:
				h.guardianTick(players, m) // beam attack (+ elder mining-fatigue aura)
			case entityCreeper:
				h.creeperFuse(players, m) // fuse + swell + bang
			case entityWitch:
				h.witchTick(players, m) // splash potions from a distance
			case entityDrowned:
				if m.trident {
					h.drownedThrow(players, m) // ranged: hurl a trident
				} else {
					h.mobMelee(players, m)
				}
			case entityPiglin:
				switch {
				case m.held == itemCrossbow:
					h.piglinCrossbowTick(players, m) // CrossbowAttack: a crossbow piglin never melees
				case m.baby:
					// StartAttacking is gated on isAdult: a baby piglin never fights
				case spearOf(m.held) != nil:
					// SpearAttack's lowered spear is the whole attack: MeleeAttack
					// skips a piglin holding a kinetic weapon (canUseNonMeleeWeapon).
					h.mobSpearTick(players, m)
				default:
					h.mobMelee(players, m)
				}
			default:
				if m.spearGoal != nil {
					h.mobSpearTick(players, m) // SpearUseGoal outranks the melee goal: the charge is its attack
				} else {
					h.mobMelee(players, m) // bite a player in reach (on cooldown)
				}
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
	if h.floatSwims(m, fnx, fnz) {
		return hazardOK && !w.TallObstacle(fnx, fnz) // afloat: the water at its level is the floor it steps on
	}
	// Room for the body where it would stand. MobFeetFrom gives up on a
	// column buried eight deep and answers the mob's own height — a flat
	// step, straight into a tall wall, where the client draws it black. A
	// mob already wedged somewhere may still leave.
	fy := int(math.Floor(m.y))
	roomOK := h.bodyFits(m, fnx, fy+step, fnz) || !h.bodyFits(m, cx, fy, cz)
	return destOK && hazardOK && roomOK && step <= 1 && step >= -1 && !w.TallObstacle(fnx, fnz)
}

// bodyFits reports whether this mob's height of cells from feet y up is
// free of full cubes in column (x, z). Only whole blocks count: a door,
// gate or slab is the other step rules' business, and a full cube is
// what the body must never stand inside.
func (h *hub) bodyFits(m *mob, x, y, z int) bool {
	w := h.worldFor(m.dim)
	top := y + max(1, int(math.Ceil(m.box().h))) - 1
	for cy := y; cy <= top; cy++ {
		if worldgen.IsFullCube(w.At(x, cy, z)) {
			return false
		}
	}
	return true
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
	if m.sulfurHurtGate(dt) {
		return // a lit sulfur cube, or one whose block shrugs this off (sulfurcube.go)
	}
	m.lastDT, m.lastDirect = dt, 0 // what the loot tables ask of the killing blow (damage_source_properties)
	// A creaking with a standing heart cannot be hurt: the blow goes to the
	// heart instead. Recorded rather than acted on, because this is the mob's
	// own arithmetic with no hub in reach — the hub answers for it next tick.
	// Putting the check HERE rather than at the places that swing is the point:
	// there is no path to a creaking's health that skips this function.
	if m.heartBound && !dt.has(tagBypassesInvulnerability) {
		m.heartHit = true
		return
	}
	// LivingEntity.hurt's cooldown: for 10 ticks after a landed blow only a
	// bigger blow lands, and only its excess over the last one.
	if m.etype == entityAxolotl && attributedDamage(dt) { // Axolotl.hurtServer rolls play-dead; the hub does it next update
		m.axHurt, m.axHurtDmg = true, dmg
	}
	if m.etype == entityWither && m.witherSmash <= 0 {
		// WitherBoss.hurtServer: a blow arms the block-smashing timer; twenty
		// ticks later everything breakable around it comes down.
		m.witherSmash = 20
	}
	if m.etype == entitySilverfish { // Silverfish.hurtServer: notifyHurt
		m.silverHurt = true
	}
	if m.etype == entitySquid || m.etype == entityGlowSquid { // Squid.hurtServer: spawnInk + the flee goal
		m.squidHurt = true
	}
	if m.etype == entityShulker { // Shulker.hurtServer: the teleport roll
		m.shHurt = true
	}
	if m.etype == entityVillager { // HurtBySensor: the villager runs from what hurt it
		m.villagerHurt = true
	}
	if m.etype == entityWitch && dt.has(tagWitchResistantTo) {
		// Witch.hurtServer: a witch shrugs off #witch_resistant_to — the magic
		// damage her own potions deal — at fifteen percent. It is why throwing
		// potions at one barely works.
		dmg *= 0.15
	}
	if m.etype == entityWither && dt.has(tagWitherImmuneTo) {
		return // WitherBoss.hurtServer: #wither_immune_to never lands
	}
	if m.etype == entityArmadillo && m.armState == armScared {
		dmg = (dmg - 1) / 2 // Armadillo.hurtServer: rolled up, a blow loses a point and halves
		if dmg < 0 {
			dmg = 0
		}
	}
	// LivingEntity.hurt's cooldown: for ten ticks after a landed blow only a
	// bigger blow lands, and only its excess over the last one. It sits here,
	// where vanilla has it: after the per-species reductions a mob applies
	// before calling super (the armadillo's roll-up), and before the wolf's
	// armour, which vanilla soaks inside actuallyHurt. The counter is wound
	// down by updateMobs, so a test that hits without ticking must clear it.
	if m.invulnTicks > 10 {
		if dmg <= m.lastHurt {
			return
		}
		dmg, m.lastHurt = dmg-m.lastHurt, dmg
	} else {
		m.lastHurt, m.invulnTicks = dmg, 20
	}
	if m.etype == entityWolf && m.armorSt.item == itemWolfArmor && !dt.has(tagBypassesWolfArmor) {
		// Wolf.actuallyHurt: the armour takes the whole blow as durability
		// (ceil of the damage) and the wolf none of it, until it breaks.
		m.wolfArmorAbsorb(dmg)
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
	if m.schoolLeader != 0 { // leaving a shoal frees a place in it
		if leader := h.mobs[m.schoolLeader]; leader != nil && leader.schoolFollowers > 0 {
			leader.schoolFollowers--
		}
	}
	delete(h.mobs, m.eid)
	h.gridDirty()
	h.entityGone(players, m.dim, m.eid)
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
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		if m.burning { // one-shot fire flags can be dropped — re-assert while lit
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, true)))
		}
		if m.etype == entitySkeleton { // bow-in-hand is one-shot too — keep it honest
			h.toTracking(players, m.eid, m.dim, m.x, m.z, skeletonEquip(m.eid))
		}
		// One-shot mount/pet state re-asserted so a late-joining player sees the
		// saddle, rider and collar rather than a bare animal.
		if m.rider != 0 {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, passengersBody(m.eid, m.rider))
		}
		if len(m.riders) > 0 {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, passengersBody(m.eid, m.riders...))
		}
		if m.mobRider != 0 { // a mob rider (raid ravager) — re-assert for late joiners
			h.toTracking(players, m.eid, m.dim, m.x, m.z, passengersBody(m.eid, m.mobPassengers()...))
		}
		if m.harness != 0 {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, ghastHarnessEquip(m.eid, m.harness))
		}
		if m.etype == entityCopperGolem && m.oxidation > 0 {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(copperWeatherMeta(m.eid, int32(m.oxidation))))
		}
		if sm := speciesStateMeta(m); sm != nil { // a goat's horns, a turtle's egg
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(sm))
		}
		if m.etype == entityEnderman && m.carriedBlock != 0 {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(enderCarryMeta(m.eid, m.carriedBlock)))
		}
		if m.etype == entityBee { // pollen coat / red eyes are one-shot state too
			if m.beeSentFlags != 0 {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(beeFlagsMeta(m.eid, m.beeSentFlags)))
			}
			if m.beeSentAngry {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(beeAngerMeta(m.eid, m.anger)))
			}
		}
		if m.saddled { // the saddle is an EQUIPMENT slot on every species (1.21.5+)
			if horseFamily(m.etype) {
				h.horseEquipSync(players, m) // saddle + body armor together
			} else {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, saddleEquip(m.eid))
			}
		}
		if m.tamed {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(petMeta(m)))
		}
		if m.sleeping { // re-assert the lying pose so a late-joining player sees it
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(sleepMetadata(m.eid, m.bed)))
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

// toOthersNear is toNearbyEv for something a player did that their own
// client has already drawn (ChunkMap.broadcast, which leaves the entity
// itself out): an arm swing, the cracks of their own dig.
func (h *hub) toOthersNear(players map[int32]*tracked, self int32, dim int, x, z float64, ev any) {
	cx, cz := chunkFloor(x), chunkFloor(z)
	for eid, t := range players {
		if eid == self || t.dim != dim {
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
	if kb := attackKnockbackFor(etype); kb > 0 {
		a.SetBase(attr.AttackKnockback, kb)
	}
	if sh := stepHeightFor(etype); sh > 0 {
		a.SetBase(attr.StepHeight, sh)
	}
	// The two species families that fall better than everything else: a fox
	// lands from five blocks unhurt, and the equines from six and then take
	// half of what is left.
	switch {
	case etype == entityFox:
		a.SetBase(attr.SafeFallDistance, 5)
	case isEquine(etype):
		a.SetBase(attr.SafeFallDistance, 6)
		a.SetBase(attr.FallDamageMultiplier, 0.5)
	}
	if fs := flyingSpeedFor(etype); fs > 0 {
		a.SetBase(attr.FlyingSpeed, fs)
	}
	// TEMPT_RANGE: ten for the animals (the attribute's default), sixteen for
	// the happy ghast, eight for the sulfur cube.
	switch etype {
	case entityHappyGhast:
		a.SetBase(attr.TemptRange, 16)
	case entitySulfurCube:
		a.SetBase(attr.TemptRange, sulfurTemptRange)
	}
	return a
}

// flyingSpeedFor is the species' FLYING_SPEED, for the six that carry one:
// the fliers FlyingMoveControl moves (bee, parrot, allay, wither) and the two
// ghasts, whose own move controls read it. Every other flier moves by rules
// of its own and never looks at the attribute.
func flyingSpeedFor(etype int) float64 {
	switch etype {
	case entityBee, entityWither:
		return 0.6
	case entityParrot:
		return 0.4
	case entityAllay:
		return 0.1
	case entityGhast:
		return 0.06
	case entityHappyGhast:
		return 0.05
	}
	return 0
}

// flyingFactor is how much faster or slower than its species a flier moves
// through the air: FLYING_SPEED over the species' own figure. The engine's
// flight is calibrated per species in its step units, so the attribute acts
// as the ratio vanilla's speed × FLYING_SPEED would give — exactly 1 until a
// command, an effect or a plugin changes it.
func (m *mob) flyingFactor() float64 {
	def := flyingSpeedFor(m.etype)
	if def == 0 || m.attrs == nil {
		return 1
	}
	v := m.attrs.Peek(attr.FlyingSpeed)
	if v == def {
		return 1
	}
	return v / def
}

// stepHeightFor is the species' STEP_HEIGHT, which vanilla moves off the 0.6
// default for exactly twelve of them. It is a SYNCED attribute, so it is not
// decoration even though the server's own walkers use a flat one-block climb:
// a ridden mount is moved by the riding client, and that client reads this to
// decide what it can walk over. A camel on 0.6 has to jump the fence its 1.5
// is famous for strolling across.
func stepHeightFor(etype int) float64 {
	switch {
	case etype == entityCamel || etype == entityCamelHusk:
		return 1.5 // Camel overrides the horse base
	case etype == entityCreaking:
		return 1.0625
	case etype == entityAxolotl, etype == entityFrog, etype == entityTurtle,
		etype == entityIronGolem, etype == entityCopperGolem, etype == entityEnderman,
		etype == entityRavager, etype == entityDrowned:
		return 1
	case isEquine(etype):
		return 1 // AbstractHorse.createBaseHorseAttributes
	}
	return 0 // the registry default (0.6)
}

// isEquine is the AbstractHorse family, which shares its fall tolerance.
func isEquine(etype int) bool {
	switch etype {
	case entityHorse, entityDonkey, entityMule, entitySkeletonHorse, entityZombieHorse,
		entityLlama, entityTraderLlama, entityCamel, entityCamelHusk:
		return true
	}
	return false
}

// attackKnockbackFor is the species' ATTACK_KNOCKBACK base. Vanilla leaves it
// at zero for almost everything — the four that set it are the ones whose
// whole character is sending you flying. It was never set here at all, which
// among other things made a hoglin's throw (HoglinBase.throwTarget, which
// reads this attribute directly) a no-op: the hoglin bit you and you stayed
// exactly where you were.
func attackKnockbackFor(etype int) float64 {
	switch etype {
	case entityRavager, entityWarden:
		return 1.5
	case entityHoglin, entityZoglin:
		return 1
	}
	return 0
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

// gearKnockbackSource is the netherite set's KNOCKBACK_RESISTANCE modifier.
const gearKnockbackSource = "equipment:knockback"

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
	if m.etype == entityWolf && m.armorSt.item == itemWolfArmor {
		pts += wolfArmorPoints // ArmorMaterials.ARMADILLO_SCUTE, the body slot
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
func (m *mob) moveSpeed() float64 {
	if v := m.navMount; v != nil {
		return v.mobAttrs().Value(attr.MovementSpeed) // a driving rider moves at its vehicle's pace
	}
	return m.mobAttrs().Value(attr.MovementSpeed)
}

// setMoveSpeed sets the base MOVEMENT_SPEED, in per-update blocks.
func (m *mob) setMoveSpeed(v float64) { m.mobAttrs().SetBase(attr.MovementSpeed, v) }

// setBabySpeed applies or clears the baby speed modifier.
func (m *mob) setBabySpeed(on bool) {
	in := m.mobAttrs().Get(attr.MovementSpeed)
	if !on {
		in.RemoveModifier(babySpeedSource)
		return
	}
	in.AddModifier(attr.Modifier{Source: babySpeedSource, Amount: babySpeedBonus(m.etype), Op: attr.AddMultipliedBase})
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

// heldStack is the mob's main-hand item as a stack: what the equipment frame
// renders and what drops when it dies.
func (m *mob) heldStack() invStack {
	return invStack{item: m.held, count: max(b2i(m.held != 0), m.heldCount*b2i(m.held != 0)), ench: m.heldEnch, dmg: m.heldDmg}
}

// mobSpeedFactor is Entity.getBlockSpeedFactor for a mob: the feet cell's
// factor, or when that is 1 the cell below's; soul sand and honey are 0.4.
func (h *hub) mobSpeedFactor(m *mob) float64 {
	w := h.worldFor(m.dim)
	fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	factor := func(s uint32) float64 {
		if s == worldgen.SoulSand || isHoneyBlock(s) {
			return 0.4
		}
		return 1
	}
	feet := w.At(fx, fy, fz)
	if worldgen.IsWater(feet) {
		return factor(feet)
	}
	if f := factor(feet); f != 1 {
		return f
	}
	return factor(w.At(fx, fy-1, fz))
}

// entityGone announces a removal to every player in the dimension rather
// than only those near the spot. Removals happen precisely when nobody is
// near: a mob despawns once the closest player is past 128 blocks, and a
// chunk's mobs unload five seconds after the chunk leaves the player's
// view — both well outside the six-chunk interest radius an ordinary
// broadcast is culled to, so the frame reached nobody at all. Since the
// engine never tells a client to forget a chunk, that frame is the only
// chance a viewer has to drop the entity; without it the client keeps
// what it last saw, standing still and unhittable, because the server no
// longer has that id. Adds stay culled to the interest radius, as they
// should be — it is only the goodbye that has to travel.
func (h *hub) entityGone(players map[int32]*tracked, dim int, eid int32) {
	for _, t := range players {
		delete(t.tracked, eid) // the viewer has been told: the tracker must not say it twice
	}
	h.toDimEv(players, dim, entGone(eid))
}
