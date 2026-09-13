package server

import "math"

// The wandering trader (WanderingTraderSpawner, WanderingTrader,
// VillagerTrades.WANDERING_TRADER_TRADES): every twenty minutes the world
// rolls for one — a quarter chance the first time, rising by a quarter
// each miss to three quarters — and one time in ten it appears within
// forty-eight blocks of a random player with two leashed trader llamas,
// nine trades drawn from vanilla's three pools (two it buys, two rare
// wares, five common ones), and forty minutes to live. At night it drinks
// invisibility; at dawn, milk.

const (
	traderTickDelay    = 1200  // DEFAULT_TICK_DELAY
	traderSpawnDelay   = 24000 // DEFAULT_SPAWN_DELAY
	traderChanceMin    = 25
	traderChanceMax    = 75
	traderChanceStep   = 25
	traderOneIn        = 10 // SPAWN_ONE_IN_X_CHANCE
	traderSpawnTries   = 10 // NUMBER_OF_SPAWN_ATTEMPTS
	traderSpawnRange   = 48
	traderLlamaRange   = 4
	traderDespawnTicks = 48000
	traderDrinkTicks   = 32 // a potion's / a milk bucket's use duration
	traderInvisSecs    = 180
	traderDrinkPotion  = 1
	traderDrinkMilk    = 2
)

// traderListing is one ItemListing: buy = EmeraldForItems (the player pays
// count of item for emeralds), else ItemsForEmeralds (emeralds for count).
type traderListing struct {
	item    string
	cost    int32 // items the player pays (buy) or emeralds (sell)
	count   int32 // emeralds paid out (buy) or items handed over (sell)
	maxUses int32
	xp      int32
	buy     bool
}

var traderPools = [3]struct {
	pick     int
	listings []traderListing
}{
	{2, []traderListing{ // the water potion's ItemCost needs a potion input: left out
		{"water_bucket", 1, 2, 1, 2, true}, {"milk_bucket", 1, 2, 1, 2, true}, {"fermented_spider_eye", 1, 3, 1, 2, true},
		{"baked_potato", 4, 1, 2, 1, true}, {"hay_block", 1, 1, 2, 1, true},
	}},
	{2, []traderListing{ // the enchanted iron pickaxe and the long invisibility potion carry components: left out
		{"packed_ice", 1, 1, 6, 1, false}, {"blue_ice", 6, 1, 6, 1, false}, {"gunpowder", 1, 4, 2, 1, false}, {"podzol", 3, 3, 6, 1, false},
		{"acacia_log", 1, 8, 4, 1, false}, {"birch_log", 1, 8, 4, 1, false}, {"dark_oak_log", 1, 8, 4, 1, false}, {"jungle_log", 1, 8, 4, 1, false},
		{"oak_log", 1, 8, 4, 1, false}, {"spruce_log", 1, 8, 4, 1, false}, {"cherry_log", 1, 8, 4, 1, false}, {"mangrove_log", 1, 8, 4, 1, false}, {"pale_oak_log", 1, 8, 4, 1, false},
	}},
	{5, []traderListing{
		{"tropical_fish_bucket", 3, 1, 4, 1, false}, {"pufferfish_bucket", 3, 1, 4, 1, false}, {"sea_pickle", 2, 1, 5, 1, false}, {"slime_ball", 4, 1, 5, 1, false},
		{"glowstone", 2, 1, 5, 1, false}, {"nautilus_shell", 5, 1, 5, 1, false}, {"fern", 1, 1, 12, 1, false}, {"sugar_cane", 1, 1, 8, 1, false},
		{"pumpkin", 1, 1, 4, 1, false}, {"kelp", 3, 1, 12, 1, false}, {"cactus", 3, 1, 8, 1, false}, {"dandelion", 1, 1, 12, 1, false},
		{"poppy", 1, 1, 12, 1, false}, {"blue_orchid", 1, 1, 8, 1, false}, {"allium", 1, 1, 12, 1, false}, {"azure_bluet", 1, 1, 12, 1, false},
		{"red_tulip", 1, 1, 12, 1, false}, {"orange_tulip", 1, 1, 12, 1, false}, {"white_tulip", 1, 1, 12, 1, false}, {"pink_tulip", 1, 1, 12, 1, false},
		{"oxeye_daisy", 1, 1, 12, 1, false}, {"cornflower", 1, 1, 12, 1, false}, {"lily_of_the_valley", 1, 1, 7, 1, false}, {"open_eyeblossom", 1, 1, 7, 1, false},
		{"wheat_seeds", 1, 1, 12, 1, false}, {"beetroot_seeds", 1, 1, 12, 1, false}, {"pumpkin_seeds", 1, 1, 12, 1, false}, {"melon_seeds", 1, 1, 12, 1, false},
		{"acacia_sapling", 5, 1, 8, 1, false}, {"birch_sapling", 5, 1, 8, 1, false}, {"dark_oak_sapling", 5, 1, 8, 1, false}, {"jungle_sapling", 5, 1, 8, 1, false},
		{"oak_sapling", 5, 1, 8, 1, false}, {"spruce_sapling", 5, 1, 8, 1, false}, {"cherry_sapling", 5, 1, 8, 1, false}, {"pale_oak_sapling", 5, 1, 8, 1, false},
		{"mangrove_propagule", 5, 1, 8, 1, false},
		{"red_dye", 1, 3, 12, 1, false}, {"white_dye", 1, 3, 12, 1, false}, {"blue_dye", 1, 3, 12, 1, false}, {"pink_dye", 1, 3, 12, 1, false},
		{"black_dye", 1, 3, 12, 1, false}, {"green_dye", 1, 3, 12, 1, false}, {"light_gray_dye", 1, 3, 12, 1, false}, {"magenta_dye", 1, 3, 12, 1, false},
		{"yellow_dye", 1, 3, 12, 1, false}, {"gray_dye", 1, 3, 12, 1, false}, {"purple_dye", 1, 3, 12, 1, false}, {"light_blue_dye", 1, 3, 12, 1, false},
		{"lime_dye", 1, 3, 12, 1, false}, {"orange_dye", 1, 3, 12, 1, false}, {"brown_dye", 1, 3, 12, 1, false}, {"cyan_dye", 1, 3, 12, 1, false},
		{"brain_coral_block", 3, 1, 8, 1, false}, {"bubble_coral_block", 3, 1, 8, 1, false}, {"fire_coral_block", 3, 1, 8, 1, false}, {"horn_coral_block", 3, 1, 8, 1, false},
		{"tube_coral_block", 3, 1, 8, 1, false}, {"vine", 1, 3, 4, 1, false}, {"pale_hanging_moss", 1, 3, 4, 1, false}, {"brown_mushroom", 1, 3, 4, 1, false},
		{"red_mushroom", 1, 3, 4, 1, false}, {"lily_pad", 1, 5, 2, 1, false}, {"small_dripleaf", 1, 2, 5, 1, false}, {"sand", 1, 8, 8, 1, false},
		{"red_sand", 1, 4, 6, 1, false}, {"pointed_dripstone", 1, 2, 5, 1, false}, {"rooted_dirt", 1, 2, 5, 1, false}, {"moss_block", 1, 2, 5, 1, false},
		{"pale_moss_block", 1, 2, 5, 1, false}, {"wildflowers", 1, 1, 12, 1, false}, {"tall_dry_grass", 1, 1, 12, 1, false}, {"firefly_bush", 3, 1, 12, 1, false},
	}},
}

// traderTrade turns a listing into an offer.
func traderTrade(l traderListing) vTrade {
	emerald := int32(itemByName["emerald"])
	if l.buy {
		return vTrade{inItem: int32(itemByName[l.item]), inCount: l.cost, outItem: emerald, outCount: l.count, maxUses: l.maxUses, xp: l.xp}
	}
	return vTrade{inItem: emerald, inCount: l.cost, outItem: int32(itemByName[l.item]), outCount: l.count, maxUses: l.maxUses, xp: l.xp}
}

// rollTraderOffers is WanderingTrader.updateTrades: pick per pool.
func (h *hub) rollTraderOffers(m *mob) {
	m.offers = nil
	for _, pool := range traderPools {
		idx := h.rng.Perm(len(pool.listings))
		for i := 0; i < pool.pick && i < len(idx); i++ {
			m.offers = append(m.offers, mobOffer{trade: traderTrade(pool.listings[idx[i]])})
		}
	}
}

// traderSpawnerTick is WanderingTraderSpawner.tick, run every 1200 ticks.
func (h *hub) traderSpawnerTick(players map[int32]*tracked) {
	if !h.rules.DoTraderSpawning {
		return
	}
	if h.rules.TraderSpawnDelay == 0 && h.rules.TraderSpawnChance == 0 {
		h.rules.TraderSpawnDelay, h.rules.TraderSpawnChance = traderSpawnDelay, traderChanceMin
	}
	h.rules.TraderSpawnDelay -= traderTickDelay
	if h.rules.TraderSpawnDelay > 0 {
		h.saveRules()
		return
	}
	h.rules.TraderSpawnDelay = traderSpawnDelay
	if !h.rules.DoMobSpawning {
		h.saveRules()
		return
	}
	n := h.rules.TraderSpawnChance
	h.rules.TraderSpawnChance = min(traderChanceMax, max(traderChanceMin, h.rules.TraderSpawnChance+traderChanceStep))
	if h.rng.Intn(100) <= n && h.traderSpawn(players) {
		h.rules.TraderSpawnChance = traderChanceMin
	}
	h.saveRules()
}

// traderSpawn is WanderingTraderSpawner.spawn: near a random player, one
// time in ten, with two leashed llamas.
func (h *hub) traderSpawn(players map[int32]*tracked) bool {
	t := h.randomPlayer(players)
	if t == nil {
		return true
	}
	if h.rng.Intn(traderOneIn) != 0 {
		return false
	}
	if t.dim != 0 {
		return false
	}
	x, z, ok := h.traderSpawnPosNear(int(math.Floor(t.x)), int(math.Floor(t.z)), traderSpawnRange)
	if !ok {
		return false
	}
	m := h.spawnSpecies(players, entityWanderingTrader, 0, float64(x)+0.5, float64(h.world.SurfaceY(x, z)), float64(z)+0.5)
	if m == nil {
		return false
	}
	h.rollTraderOffers(m)
	m.traderDespawn = traderDespawnTicks
	for i := 0; i < 2; i++ {
		if lx, lz, ok := h.traderSpawnPosNear(x, z, traderLlamaRange); ok {
			if l := h.spawnSpecies(players, entityTraderLlama, 0, float64(lx)+0.5, float64(h.world.SurfaceY(lx, lz)), float64(lz)+0.5); l != nil {
				l.traderDespawn = traderDespawnTicks
				h.setLeash(players, l, m.eid)
			}
		}
	}
	return true
}

// traderSpawnPosNear is findSpawnPositionNear: ten tries at a random
// column within n, standing room and all.
func (h *hub) traderSpawnPosNear(x, z, n int) (int, int, bool) {
	for i := 0; i < traderSpawnTries; i++ {
		nx := x + h.rng.Intn(2*n+1) - n
		nz := z + h.rng.Intn(2*n+1) - n
		if h.world.Spawnable(nx, nz) {
			return nx, nz, true
		}
	}
	return 0, 0, false
}

// traderStep runs each mob update for a trader (and its llamas): the
// despawn clock, and the potion or milk by the time of day.
func (h *hub) traderStep(players map[int32]*tracked, m *mob) bool {
	if m.traderDespawn > 0 {
		m.traderDespawn -= mobMoveInterval
		if m.traderDespawn <= 0 {
			h.despawnMob(players, m)
			return true
		}
	}
	if m.etype != entityWanderingTrader {
		return false
	}
	if m.drinkTicks > 0 {
		m.drinkTicks -= mobMoveInterval
		m.vx, m.vz = 0, 0
		if m.drinkTicks > 0 {
			return true
		}
		m.held = 0
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
		switch m.traderDrink {
		case traderDrinkPotion:
			h.applyMobEffect(players, m, effInvisibility, 0, traderInvisSecs)
			h.playSoundDim(players, m.dim, "minecraft:entity.wandering_trader.disappeared", sndNeutral, m.x, m.y, m.z, 1, 0.9+h.rng.Float32()*0.2)
		case traderDrinkMilk:
			h.removeMobEffect(players, m, effInvisibility)
			h.playSoundDim(players, m.dim, "minecraft:entity.wandering_trader.reappeared", sndNeutral, m.x, m.y, m.z, 1, 0.9+h.rng.Float32()*0.2)
		}
		m.traderDrink = 0
		return true
	}
	invisible := m.hasEffect(effInvisibility) > 0
	switch {
	case !h.isDayTime() && !invisible: // isDarkOutside: the invisibility potion
		m.traderDrink, m.drinkTicks, m.held = traderDrinkPotion, traderDrinkTicks, itemPotion
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: itemPotion, count: 1, potion: potInvisibility}, invStack{}, m.gear))
		return true
	case h.isDayTime() && invisible: // isBrightOutside: the milk
		m.traderDrink, m.drinkTicks, m.held = traderDrinkMilk, traderDrinkTicks, int32(itemByName["milk_bucket"])
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: 1}, invStack{}, m.gear))
		return true
	}
	return false
}
