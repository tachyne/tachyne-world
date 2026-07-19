# Vanilla-fidelity audit — confirmed deviations

Multi-agent audit 2026-07-19 (run wf_a8990249-25e): 9 subsystems diffed against the decompiled 26.2/1.21.11 source, every finding adversarially verified. **35 confirmed** (2 high / 26 medium / 7 low). mob-attributes and mob-AI passed clean.

> Worldgen terrain is deliberately original and out of scope. These are gameplay logic/number deviations from vanilla.

## [HIGH] enchant-brew-anvil: Anvil has no prior-work penalty and no "Too Expensive" (>=40) cap
- **tachyne** (`internal/server/anvil.go`:114): anvilResult() computes cost only from rename(+1), per-enchant level, and repair(+2); there is no accumulated REPAIR_COST component and no upper cap. An item can be enchant-merged/repaired unlimited times at flat cheap cost.
- **vanilla**: AnvilMenu.createResult accumulates l = input.REPAIR_COST + sacrifice.REPAIR_COST into the cost, sets the result's REPAIR_COST via calculateIncreasedRepairCost(n)=n*2+1 (doubles every anvil use), and blocks the result (result=EMPTY) when cost>=40 for non-creative players.
- **source**: AnvilMenu.java createResult() l+=REPAIR_COST (line 138), cost>=40 -> itemStack2=EMPTY (line 243-245), calculateIncreasedRepairCost n*2+1 (line 261-263)
- note: This is the core anvil balance mechanic (rising cost + hard cap); its absence makes anvils infinitely cheap to reuse.

## [HIGH] redstone-fluid-crop: Crops and melon/pumpkin stems grow on every random tick (no probability gate, no growth-speed)
- **tachyne** (`internal/server/grow.go`:171): tickCrop (grow.go:171): `if state < r[1] && h.skyLit(...) { setBlock(state+1) }` — advances one stage EVERY random tick, unconditionally. tickStem (grow.go:465) does the same for melon/pumpkin stems. getGrowthSpeed (hydration 3x, neighbour layout) is not implemented.
- **vanilla**: randomTick advances only when `random.nextInt((int)(25.0F / growthSpeed) + 1) == 0`, with growthSpeed from getGrowthSpeed (base 1; +3 for hydrated farmland below; ±neighbour bonuses; /2 for adjacent same crop). Effective per-tick odds ≈ 1/6 (ideal hydrated field) to 1/13 (dry). tachyne is effectively 1/1, so crops/stems mature ~6–13x too fast.
- **source**: CropBlock.randomTick (CropBlock.java:79-89) + getGrowthSpeed (CropBlock.java:100-142); StemBlock.randomTick uses the same 25/growthSpeed gate.
- note: Biggest fidelity gap in this subsystem. skyLit-vs-brightness>=9 is a separately-noted intentional proxy; the missing probability gate is the real deviation.

## [MEDIUM] combat: Sharpness bonus over-scaled (adds full level instead of 0.5·level+0.5)
- **tachyne** (`internal/server/combat.go`:152): base += float64(lvl)  — Sharpness I=+1, III=+3, V=+5 (integer level added straight to base damage, then cooldown-scaled AND crit-multiplied)
- **vanilla**: Sharpness damage bonus = 1.0 + 0.5·(level-1) = 0.5·level+0.5, so I=+1.0, III=+2.0, V=+3.0. Bonus (f2) is cooldown-scaled but added AFTER the crit ×1.5, so it is never crit-multiplied.
- **source**: sharpness.json effects.minecraft:damage add linear base 1.0 per_level_above_first 0.5; Player.java attack() L1007 (f2 *= f3) and L1034 (f4 = f + f2 computed after crit f *= 1.5f at L1037)
- note: Sharpness V melee does +5 vs vanilla +3, and tachyne also crit-multiplies the sharpness portion (×1.5) which vanilla never does. Code comment 'rounded up' is only true for levels 1-2.

## [MEDIUM] combat: Knockback enchantment not applied to melee hits
- **tachyne** (`internal/server/combat.go`:216): power := 0.5; if t.sprinting { power = 1.0 }  — only the sprint bonus affects mob knockback; no enchKnockback constant exists anywhere in the codebase
- **vanilla**: getKnockback = ATTACK_KNOCKBACK(0) + Knockback enchant (linear base 1.0 per_level 1.0 → +1 block/level) + (sprinting?1:0); applied as knockback(f6*0.5, …), i.e. Knockback II adds ~1.0 block of extra impulse
- **source**: knockback.json effects.minecraft:knockback add linear base 1.0 per_level_above_first 1.0; LivingEntity.getKnockback() L1424; Player.attack() L1048 (f6 = getKnockback + (bl2?1:0)) → entity.knockback(f6*0.5,…) L1051
- note: The Knockback enchant is completely inert on swords in melee.

## [MEDIUM] combat: Power / Punch enchantments not applied to bow arrows
- **tachyne** (`internal/server/bow.go`:96): dmg := int(math.Ceil(2 * v)); if power >= 1 { dmg += rng.Intn(dmg/2+2) }  — no Power (damage) or Punch (knockback) term
- **vanilla**: onHitEntity: d = baseDamage(2.0) then d = modifyDamage(weapon,…) so Power adds base 1.0 + 0.5·(level-1) to d BEFORE ×speed (Power V ⇒ d=5 ⇒ ceil(3·5)=15 at full draw vs tachyne 6). Punch adds arrow knockback.
- **source**: power.json effects.minecraft:damage add linear base 1.0 per_level_above_first 0.5 (predicate direct_attacker is arrow); AbstractArrow.java onHitEntity L366-373 (d = baseDamage; d = EnchantmentHelper.modifyDamage(...); n = Mth.ceil(f*d))
- note: Player bow ignores its own enchantments; a Power V bow does the same damage as an unenchanted one.

## [MEDIUM] combat: No invulnerability frames / hurt cooldown on players or mobs
- **tachyne** (`internal/server/mob.go`:474): hurtBreach applies every hit in full immediately; hub.damage (survival.go:301) likewise. No invulnerableTime / lastHurt tracking on mob or tracked.
- **vanilla**: invulnerableTime=20 on each hit; while invulnerableTime>10 a further hit only deals (f - lastHurt) and only if f>lastHurt, else it is ignored (BYPASSES_COOLDOWN excepted).
- **source**: LivingEntity.java hurtServer() L1121-1133 (if invulnerableTime>10: if f<=lastHurt return false; else actuallyHurt(f-lastHurt)); else lastHurt=f; invulnerableTime=20
- note: Rapid or simultaneous same-tick damage sources (fire tick + arrow + melee) all land fully in tachyne instead of being capped to the largest within the 10-tick window.

## [MEDIUM] combat: Sweep attack fires while sprinting/moving and scales off Sharpness, not Sweeping Edge
- **tachyne** (`internal/server/combat.go`:225): gate: sword && charge>=0.9 && !crit && t.onGround (no sprint/speed check); sweep damage = 1 + enchLvl(enchSharpness), flat, not charge-scaled; targets picked by dist3(other,target) <= 1.5
- **vanilla**: bl6 also requires !bl2 (NOT a sprint-knockback hit) AND horizontalDistanceSqr < (speed*2.5)² (walking slowly). Sweep damage = getEnchantedDamage(target, 1.0 + SWEEPING_DAMAGE_RATIO·f) · f3 — base 1.0 plus the Sweeping-Edge attribute, charge-scaled; area = target bbox inflate(1,0.25,1) and within 9.0 (3 blocks) of the player
- **source**: Player.java attack() L1035 (bl6 = bl3 && !bl && !bl2 && onGround && horizDistSqr<square(speed*2.5) && SWORDS), L1060 (f7=1.0+SWEEPING_DAMAGE_RATIO*f), L1062-1068 (inflate(1,0.25,1), distanceToSqr<9.0, dmg = enchanted(f7)*f3)
- note: tachyne substitutes the Sharpness level for the Sweeping Edge attribute (a different enchant) and lets you sweep while sprinting/running, which vanilla forbids.

## [MEDIUM] combat: Crit gate omits in-water / on-ladder / blindness conditions
- **tachyne** (`internal/server/combat.go`:168): crit = charge>=0.9 && t.airborne && t.y<t.peakY && !t.sprinting
- **vanilla**: crit requires f3>0.9 && fallDistance>0 && !onGround && !onClimbable && !isInWater && !hasEffect(BLINDNESS) && !isPassenger && target is LivingEntity && !isSprinting
- **source**: Player.java attack() L1030 (bl5 = bl = bl3 && fallDistance>0 && !onGround() && !onClimbable() && !isInWater() && !BLINDNESS && !isPassenger && LivingEntity && !isSprinting)
- note: tachyne allows critical hits while swimming or climbing a ladder; also uses >=0.9 vs vanilla strict >0.9.

## [MEDIUM] combat: Melee knockback has no vertical component and different base magnitude
- **tachyne** (`internal/server/combat.go`:219): m.vx, m.vz = dir*power (power 0.5 normal / 1.0 sprint); no vertical velocity set
- **vanilla**: every hit calls knockback(0.4, …): horizontal = normalize(dir)·0.4 and, when on ground, an upward pop vy = min(0.4, vy/2 + 0.4); the sprint/enchant knockback(f6*0.5) adds on top of that base 0.4
- **source**: LivingEntity.java hurtServer() L1147 (this.knockback(0.4f,d,d2)) and knockback() L1468-1479 (setDeltaMovement(vx/2 - vec.x, onGround?min(0.4,vy/2+d):vy, vz/2 - vec.z))
- note: tachyne mobs are shoved purely horizontally at a slightly higher base (0.5 vs 0.4) and never get the upward pop, so knockback feel and reach differ.

## [MEDIUM] enchant-brew-anvil: Anvil enchantment-merge cost ignores per-enchantment anvil_cost and book halving
- **tachyne** (`internal/server/anvil.go`:141): For each merged enchantment: cost += int(lvl) — i.e. it charges the resulting level, treating every enchantment's anvil cost as 1 and never halving for a book source.
- **vanilla**: cost += enchantment.getAnvilCost() * resultLevel, and when the sacrifice is an enchanted book n10 = max(1, anvilCost/2). anvil_cost is 1 for sharpness/protection/efficiency/power but 2 (unbreaking), 4 (fortune/looting/mending/flame/sweeping_edge), 8 (silk_touch/infinity).
- **source**: AnvilMenu.java lines 206-210 (n10 = enchantment.getAnvilCost(); if book n10=max(1,n10/2); n3 += n10*n7); anvil_cost values from data/minecraft/enchantment/*.json (silk_touch=8, fortune=4, unbreaking=2)
- note: e.g. applying Silk Touch from a book should cost 4 levels (max(1,8/2)), tachyne charges 1.

## [MEDIUM] enchant-brew-anvil: Anvil cannot repair a tool with its raw material
- **tachyne** (`internal/server/anvil.go`:123): anvilResult only accepts a sacrifice that is the same item (sameItem) or an enchanted_book; a valid repair material (e.g. iron ingots on an iron pickaxe, diamonds on diamond gear) yields nothing.
- **vanilla**: If itemStack2.isDamageableItem() && itemStack.isValidRepairItem(sacrifice), the anvil repairs min(damageValue, maxDamage/4) per material unit, incrementing repairItemCountCost and cost by 1 per unit consumed.
- **source**: AnvilMenu.java createResult() lines 142-156 (isValidRepairItem branch, Math.min(damage, maxDamage/4), repairItemCountCost)

## [MEDIUM] enchant-brew-anvil: Brewing stand consumes a whole blaze powder per brew instead of 20 uses per powder
- **tachyne** (`internal/server/brewing.go`:103): On each completed brew it does b.slots[4].count-- (one blaze powder consumed per single brew batch).
- **vanilla**: Blaze powder in the fuel slot grants FUEL_USES=20: fuel is set to 20 and the powder shrinks by 1 only when fuel<=0; each brew start decrements the internal fuel counter by 1, so one blaze powder fuels 20 brews.
- **source**: BrewingStandBlockEntity.java FUEL_USES=20 (line 43), fuel=20; itemStack.shrink(1) when fuel<=0 (lines 110-112), --fuel per brew (line 129)

## [MEDIUM] enchant-brew-anvil: Enchant-table: all three offer costs derived from one shared roll, not three independent rolls
- **tachyne** (`internal/server/enchant.go`:170): Computes a single base = rng.Intn(8)+1 + b/2 + rng.Intn(b+1) and derives costs = {max(base/3,1), base*2/3+1, max(base,b*2)}, so the three slot costs are perfectly correlated.
- **vanilla**: slotsChanged reseeds from the enchantment seed then calls EnchantmentHelper.getEnchantmentCost(random, slot, power, item) once per slot (n=0,1,2); each call re-rolls random.nextInt(8)+1+(power>>1)+random.nextInt(power+1), so each slot's cost is an independent draw.
- **source**: EnchantmentMenu.java slotsChanged loop lines 117-123 calling getEnchantmentCost per slot; EnchantmentHelper.getEnchantmentCost lines 442-458
- note: The per-slot cost formula itself matches; only the sharing of one random draw across all three slots deviates.

## [MEDIUM] items-food-mining: Blast furnace / smoker halve fuel burn time — vanilla does NOT (2x fuel per item)
- **tachyne** (`internal/server/furnace.go`:82): cookerFuelTicks: t := fuelTicks[item]; if kind != cookFurnace { t /= 2 }  // 'blast furnace + smoker halve the burn duration (vanilla)'
- **vanilla**: getBurnDuration(fuelValues, itemStack) = fuelValues.burnDuration(itemStack) — identical for all furnace types; the 2x speed comes ONLY from the halved recipe cookingTime (100 vs 200), never from fuel. Blast recipe cook=100 with un-halved 1600t coal => 16 items/coal.
- **source**: AbstractFurnaceBlockEntity.java:250-252 getBurnDuration (26.2); :170 newLitTime=getBurnDuration(...); recipe cookingTime 100 for BLASTING/SMOKING. Cross-checked 1.21.5 AbstractFurnaceBlockEntity.java:185 (same, no type halving).
- note: Tachyne already applies the vanilla speed-up via the tables (blastResult/smokeResult Cook=100). Adding the fuel /=2 on top makes blast furnaces/smokers burn 2x the fuel per item vs vanilla (8 items/coal instead of 16) — the comment's premise 'vanilla halves the burn duration' is incorrect. Fix: delete the t/=2.

## [MEDIUM] redstone-fluid-crop: Powered/activator rails do not propagate power down the rail line (no 8-rail chain)
- **tachyne** (`internal/server/rail.go`:145): updateRail (rail.go:145): `powered = h.inputPower(pos.x,pos.y,pos.z,false) > 0` — each rail independently checks only its own directly-adjacent redstone signal.
- **vanilla**: PoweredRailBlock.updateState: powered = hasNeighborSignal(pos) OR findPoweredRailSignal(forward) OR findPoweredRailSignal(backward); findPoweredRailSignal recurses along the rail up to searchDepth 8, so one signal source keeps up to 9 consecutive powered rails energised (booster tracks).
- **source**: PoweredRailBlock.findPoweredRailSignal / isSameRailWithPower (PoweredRailBlock.java:30-140; `if (searchDepth >= 8) return false`).
- note: Only rails touching a source will power on; classic minecart booster segments won't stay lit past the first rail.

## [MEDIUM] redstone-fluid-crop: Comparator reacts after 1 tick instead of vanilla's 2-tick delay
- **tachyne** (`internal/server/redstone1b.go`:208): updateComparator (redstone1b.go:208) recomputes and applies output immediately when processed; comparator cells are only ever scheduled via scheduleAround(pos,1), giving a 1-game-tick reaction. useRedstone1b mode-toggle also schedules delay 1 (redstone1b.go:367).
- **vanilla**: ComparatorBlock.getDelay returns 2; DiodeBlock schedules the state flip `getDelay(state)` ticks out, so comparators have a fixed 2-game-tick delay.
- **source**: ComparatorBlock.getDelay (ComparatorBlock.java:46-48 returns 2); DiodeBlock.checkTickOnNeighbor/scheduleTick(getDelay).
- note: Repeaters correctly use 2*delay (redstone1b.go:113-120); the comparator's constant 2-tick delay is the one that's halved — affects comparator-based clocks/timing.

## [MEDIUM] redstone-fluid-crop: Redstone torch burn-out (anti-flicker) not implemented
- **tachyne** (`internal/server/redstone.go`:237): updateRedstone torch case (redstone.go:237-252) simply inverts the support signal every time it fires; a fast-toggling torch oscillates forever with no burnout.
- **vanilla**: RedstoneTorchBlock: if a torch toggles MAX_RECENT_TOGGLES=8 times within RECENT_TOGGLE_TIMER=60 game ticks it 'burns out' (unlit, ignores input) and reschedules a restart after RESTART_DELAY=160 ticks.
- **source**: RedstoneTorchBlock (RedstoneTorchBlock.java:28-29 MAX_RECENT_TOGGLES=8 / RECENT_TOGGLE_TIMER=60, :30 RESTART_DELAY=160; isToggledTooFrequently :133-143).
- note: A concrete constant/mechanic gap; matters for tight redstone clocks that vanilla would extinguish.

## [MEDIUM] spawning: Dungeon spawner emits 1-2 mobs per cycle instead of vanilla's 4
- **tachyne** (`internal/server/spawner.go`:64): for i := 0; i < 1+h.rng.Intn(2); i++ { ... spawnHostileY } — 1 or 2 mobs per activation
- **vanilla**: spawnCount = 4 (DEFAULT_SPAWN_COUNT); serverTick loops `for (int c = 0; c < this.spawnCount; c++)` attempting 4 spawns per cycle
- **source**: BaseSpawner.java:44 (DEFAULT_SPAWN_COUNT=4, field spawnCount=4 line 56) + serverTick loop line 103
- note: delay 200-800, requiredPlayerRange 16, maxNearbyEntities 6 all match; only the per-cycle spawn count is halved-or-worse, so tachyne monster rooms populate ~2-4x slower than vanilla.

## [MEDIUM] spawning: Drowned use land-monster spawn logic and skip vanilla's 1/15 & 1/40 rarity gates
- **tachyne** (`internal/server/spawn.go`:438): catMonster branch: drowned allowed if `isRiverBiome || y < SeaLevel-5` after darkEnoughToSpawn; placement uses spawnPositionOK (solid ground, non-water) — no water requirement, no probability roll
- **vanilla**: Drowned register IN_WATER placement; checkDrownedSpawnRules requires water below/at pos AND (river-tag biome ? random.nextInt(15)==0 : random.nextInt(40)==0 && isDeepEnoughToSpawn)
- **source**: SpawnPlacements.java:96 (IN_WATER) + Drowned.java:137-150 (checkDrownedSpawnRules)
- note: Net effect is structurally wrong both ways: tachyne's monster placement (spawnPositionOK requires non-water anchor + solid floor) means drowned essentially cannot spawn in ocean/river water at all, while the missing rarity gate would otherwise over-spawn them on land.

## [MEDIUM] spawning: Animal per-tick spawn light check subtracts day/night darkening; vanilla uses raw light
- **tachyne** (`internal/server/spawn.go`:447): catCreature: `h.rawBrightness(sky, block, -1) > 8` — rawBrightness subtracts skyDarken() (~11 at night)
- **vanilla**: Animal.isBrightEnoughToSpawn = `level.getRawBrightness(pos, 0) > 8` — skyDampen 0, i.e. max(block, rawSkyValue) with NO time-of-day reduction
- **source**: Animal.java:119-121 (getRawBrightness(pos,0)>8); getRawBrightness = max(block, sky - dampen), LevelLightEngine.java:148-151
- note: Consequence: on open surface at night tachyne needs skyValue-11>8 (impossible) so per-tick animal spawns are blocked at night; vanilla allows them (skyValue 15 > 8). Only affects the every-400-tick creature path, not chunk-gen herds.

## [MEDIUM] survival: Slow (80-tick) natural regen ignores the naturalRegeneration gamerule
- **tachyne** (`internal/server/survival.go`:130): survivalTick slow-regen branch: `if slow && !fastActive && t.food >= regenFood && t.health < maxHealth && t.health > 0 { heal 1; exhaustion += 6 }` — no h.rules.NaturalRegen check (only fastRegen at survival.go:76 checks it).
- **vanilla**: FoodData.tick gates BOTH regen branches on RULE_NATURAL_REGENERATION: the slow branch is `else if (bl && this.foodLevel >= 18 && serverPlayer.isHurt())` where bl = getBoolean(RULE_NATURAL_REGENERATION).
- **source**: FoodData.java:48 (bl assignment) and :56 (`else if (bl && this.foodLevel >= 18 && ...)`)
- note: With gamerule naturalRegeneration=false, tachyne still heals 1 HP every 4s from the slow branch; vanilla heals 0. fastRegen is correctly gated, so only the slow path leaks.

## [MEDIUM] survival: Damage exhaustion is a flat 0.1 for every damage source
- **tachyne** (`internal/server/survival.go`:319): h.damage() adds `t.exhaustion += 0.1` for ALL damage (void, fall, drown, starve, afterburn/on_fire, cactus, lava, attacks).
- **vanilla**: actuallyHurt calls `causeFoodExhaustion(damageSource.getFoodExhaustion())` — the per-damage-type exhaustion field, which is 0.0 for fall, drown, starve, on_fire (afterburn), out_of_world; 0.1 only for player/mob attack, cactus, in_fire, lava.
- **source**: Player.java:857 causeFoodExhaustion(damageSource.getFoodExhaustion()); exhaustion values from damagetypes_gen.go (fall/drown/starve/on_fire = 0, cactus/in_fire/lava/mob_attack/player_attack = 0.1)
- note: Environmental damage (falling, drowning, afterburn, starving, void) spuriously drains hunger in tachyne where vanilla drains none.

## [MEDIUM] survival: Mining XP only awarded for coal & diamond ore
- **tachyne** (`internal/server/xp.go`:188): xpForBlock handles CoalOre → rng(3) (0-2) and DiamondOre → 3+rng(5) (3-7); returns 0 for every other ore.
- **vanilla**: DropExperienceBlock XP ranges: coal UniformInt(0,2), diamond (3,7), emerald (3,7), lapis (2,5), nether_quartz (2,5), nether_gold (0,1); redstone_ore RedStoneOreBlock pops UniformInt(1,5).
- **source**: Blocks.java:361-362 (coal), 505-506 (diamond), 682-683 (emerald), 417-418 (lapis), 759 (nether_quartz), 363 (nether_gold), 573 (redstone via RedStoneOreBlock)
- note: Mining emerald, lapis, nether quartz, nether gold, and redstone ore yields no experience in tachyne.

## [MEDIUM] survival: Status-effect application cadence collapsed to 1 Hz
- **tachyne** (`internal/server/effects.go`:116): tickEffects runs at 1 Hz: Regen heals when `amp>=1 || left%3==0`; Poison bites every call (1 HP/s); Wither damages every call (1 HP/s).
- **vanilla**: shouldApplyEffectTickThisTick uses interval `50>>amp` (Regen), `25>>amp` (Poison), `40>>amp` (Wither) ticks.
- **source**: RegenerationMobEffect.java (50>>n2), PoisonMobEffect.java:29 (25>>n2), WitherMobEffect.java:27 (40>>n2)
- note: Wither I ticks 2x too fast (1/s vs 1/2s), Poison I too fast (1/s vs 1/1.25s), Regen I too slow (1/3s vs 1/2.5s); high-amplifier effects are capped far too slow. Comments acknowledge the 1 Hz approximation.

## [MEDIUM] villager-trading: Trade prices never adjust — demand/reputation/hero-of-the-village pricing not implemented
- **tachyne** (`internal/server/villager.go`:236): tradeResult() always requires exactly o.trade.inCount input items; sendTradeList hardcodes demand=0, specialPrice=0, priceMultiplier=0.05 on every offer (villager.go:191-193). The real cost never changes.
- **vanilla**: MerchantOffer.getCostA() = clamp(basePrice + max(0, floor(basePrice * demand * priceMultiplier)) + specialPriceDiff, 1, maxStack); demand grows each restock via updateDemand() ('demand += uses - (maxUses - uses)'), and updateSpecialPrices() applies reputation and Hero-of-the-Village discounts.
- **source**: MerchantOffer.java:119-126 (getCostA), :145-146 (updateDemand); Villager.java:441-458 (updateSpecialPrices)
- note: Heavily-traded offers should get more expensive; reputation/Hero discounts should lower prices. Tachyne prices are static. Note: the price multiplier of 0.05 is also flat for all offers whereas vanilla listings vary.

## [MEDIUM] villager-trading: Iron golem attack damage is a flat 8 instead of vanilla's randomized 7.5–21.5
- **tachyne** (`internal/server/villager.go`:149): o.hurt(8) — fixed 8 damage per golem punch
- **vanilla**: ATTACK_DAMAGE attribute = 15.0; doHurtTarget computes damage = attackDamage/2 + random.nextInt((int)attackDamage) = 7.5 + nextInt(15), i.e. a range of 7.5–21.5 (avg ~14.5).
- **source**: IronGolem.java:97 (ATTACK_DAMAGE 15.0), :193-194 (doHurtTarget damage formula)
- note: Tachyne golems hit for roughly half the vanilla average.

## [MEDIUM] villager-trading: Iron golem spawns unconditionally (one per village) rather than via villager gossip agreement
- **tachyne** (`internal/server/villager.go`:71): Exactly one iron golem spawned per village at population time, unconditionally, regardless of villager count; never spawns more afterward.
- **vanilla**: spawnGolemIfNeeded requires >=5 nearby villagers that each 'wantsToSpawnGolem' (within a 10-block inflated box) agreeing, gated by golem cooldown + LEGACY_IRON_GOLEM placement; driven continuously by gossip so villages spawn golems as they grow.
- **source**: Villager.java:836-848 (spawnGolemIfNeeded, villagersNeededToAgree=5); gossip() calls it at :822
- note: A 1-2 villager hamlet gets a golem it shouldn't; a large village never gets additional golems.

## [MEDIUM] villager-trading: Raid never spawns ravager riders on the higher waves
- **tachyne** (`internal/server/raid.go`:78): spawnWave spawns each raider type per raiderWaves counts only; no riders mounted on ravagers.
- **vanilla**: spawnGroup mounts a rider on each ravager: a pillager on wave==getNumGroups(NORMAL)=5, and an evoker (first ravager) / vindicator (subsequent) on wave>=getNumGroups(HARD)=7.
- **source**: Raid.java:544-562 (ravager rider logic)
- note: Wave composition arrays and difficulty wave counts (3/5/7) themselves match vanilla exactly. Difficulty bonus-spawns are noted as a follow-up in the code comment (documented).

## [LOW] items-food-mining: Dropped-item merge radius is a 1.0 sphere vs vanilla's flat 0.5-inflated AABB
- **tachyne** (`internal/server/item.go`:119): updateItems merge: if dx*dx+dy*dy+dz*dz > 1 { continue } — merges any two identical stacks within 1.0 block in ALL axes
- **vanilla**: getEntitiesOfClass(ItemEntity.class, getBoundingBox().inflate(0.5, 0.0, 0.5), ...) — item bbox (~0.25) inflated 0.5 horizontally and 0.0 vertically, and merging is gated to every `rate` ticks (tickCount % rate).
- **source**: ItemEntity.java:218 mergeNearby inflate(0.5,0.0,0.5); :175 tickCount%rate gate (26.2).
- note: Tachyne merges items up to 1 block apart vertically (vanilla merges only near-coplanar items) and every tick (no rate gate). Minor visual/stacking difference.

## [LOW] redstone-fluid-crop: Lava rising-flow 4x spread-delay slowdown not modeled
- **tachyne** (`internal/server/sim.go`:15): sim.go uses a flat `lavaDelay = 30` (sim.go:15) for every lava spread step in the overworld.
- **vanilla**: LavaFluid.getSpreadDelay returns getTickDelay (30 overworld) but multiplies by 4 (→120 ticks) when the new fluid height exceeds the old, it isn't falling, and random.nextInt(4)!=0 — lava spreads noticeably slower/erratically up-slope.
- **source**: LavaFluid.getSpreadDelay (LavaFluid.java:181-193) + getTickDelay (LavaFluid.java:176-178).
- note: Base overworld tick delay (30), dropOff (2/step), and slope-find distance (2) all match vanilla; only the situational 4x multiplier is absent.

## [LOW] spawning: Production (default) sampler omits the 24-block world-spawn exclusion
- **tachyne** (`internal/server/spawn.go`:253): spawnAttempt (default `-spawner tachyne` path) gates only on player distance (`d <= 576`); it never calls nearWorldSpawn, so mobs may spawn within 24 blocks of world spawn
- **vanilla**: isRightDistanceToPlayerAndSpawnPoint rejects when respawn point closerToCenterThan(pos, 24.0)
- **source**: NaturalSpawner.java:234-237 (respawnData.pos().closerToCenterThan(..., 24.0) -> false)
- note: Only the opt-in `-spawner vanilla` mode (spawnvanilla.go) applies the exclusion; the default sampler is what runs on the cluster.

## [LOW] spawning: Thundering sky brightness caps sky at 10 then subtracts skyDarken, instead of a fixed -10
- **tachyne** (`internal/server/spawn.go`:419): rawBrightness with skyCap=10: `s=min(sky,10); s-=skyDarken(); max(s,block)` — caps then applies day/night darkening again
- **vanilla**: thundering branch uses getMaxLocalRawBrightness(pos, 10) = getRawBrightness(pos, 10) = max(block, skyValue - 10): a fixed subtraction of 10, NOT a cap, and it replaces (not adds to) the normal skyDarken
- **source**: Monster.java:94 + LevelLightEngine.getRawBrightness LevelLightEngine.java:148-151 + LevelReader.java:169-172
- note: Both usually yield <=7 so monster spawn outcome rarely differs, but the formula is structurally different (double-counts darkening at night thunder: tachyne ~0 vs vanilla 5 at surface).

## [LOW] spawning: World-spawn exclusion measured in 2D (x,z); vanilla is 3D
- **tachyne** (`internal/server/spawnvanilla.go`:135): nearWorldSpawn: `dx,dz := x-spawnX, z-spawnZ; return dx*dx+dz*dz < 24*24` (ignores Y)
- **vanilla**: respawnData.pos().closerToCenterThan(Vec3(x+0.5, y, z+0.5), 24.0) — full 3D distance including Y
- **source**: NaturalSpawner.java:236
- note: Minor; only matters for deep-cave columns directly under spawn.

## [LOW] survival: Food eat time hardcoded to 32 ticks for all foods
- **tachyne** (`internal/server/survival.go`:416): eatDuration = 32 ticks applied to every food (updateEating/stopEating).
- **vanilla**: consume time is a per-item Consumable property (defaultFood = 1.6s/32t, but dried_kelp ≈0.865s/17t, honey_bottle & suspicious/mushroom stews 2s/40t, etc.).
- **source**: Consumables.java defaultFood() consumeSeconds; per-item overrides (e.g. DRIED_KELP, HONEY_BOTTLE)
- note: Borderline (consume-seconds is a data component). Dried kelp eats nearly 2x slower than vanilla; stews/honey slightly faster than they should be.

## [LOW] villager-trading: Villager restock is once-per-day at dawn, unconditional; vanilla allows up to twice per day gated at the workstation
- **tachyne** (`internal/server/villager.go`:61): restockOffers() resets every offer's uses to 0; called once on the dawn wake (villager_ai.go:96) for every villager unconditionally.
- **vanilla**: allowedToRestock() = numberOfRestocksToday==0 || (numberOfRestocksToday<2 && gameTime > lastRestockGameTime+2400): at most 2 restocks/day spaced >=2400 ticks apart, and restock() only runs from the WorkAtPoi behavior (villager must reach its job-site POI).
- **source**: Villager.java:396-398 (allowedToRestock), :364-374 (restock), :386-394 (needsToRestock)
- note: Comment acknowledges 'vanilla restocks at its workstation'; partially documented simplification.
