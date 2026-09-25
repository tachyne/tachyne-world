package server

import (
	"math"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Durability + armor. Tools wear by vanilla's per-family rates (mineWear,
// attackWear), breaking (vanishing) at their items.json max. Armor absorbs
// mob damage with the vanilla 1.9+ formula — reduction from total defense
// points, softened by how hard the hit is, capped at 80% — and every hit wears
// each worn piece. The damage value rides the minecraft:damage component so
// every client renders the normal durability bar.

// evToolWear: a survival player finished mining a real block — wear the tool
// that did it (the connection knows the held slot; the hub owns the stack).
type evToolWear struct {
	eid  int32
	slot int
	n    int // points of wear; 0 means 1
}

// toolKind is which tool family an item belongs to, by its registry name.
func toolKind(item int32) string {
	name := itemNameOf[item]
	for _, k := range []string{"_sword", "_pickaxe", "_axe", "_shovel", "_hoe", "_spear"} {
		if strings.HasSuffix(name, k) {
			return k[1:]
		}
	}
	return name
}

// mineWear is what breaking a block costs the item that broke it:
// Item.mineBlock's Tool.damagePerBlock — one for a pickaxe, axe, shovel or
// hoe, two for a sword, a mace or a trident — on any block that is not
// broken instantly; ShearsItem.mineBlock's one on anything but fire. An item
// with no TOOL component (a spear, a flint and steel, a bow) is not worn by
// digging at all.
func mineWear(item int32, broken uint32) int {
	switch kind := toolKind(item); kind {
	case "shears":
		if isFire(broken) {
			return 0
		}
		return 1
	case "pickaxe", "axe", "shovel", "hoe", "sword", "mace", "trident":
		if worldgen.Hardness(broken) == 0 {
			return 0
		}
		if kind == "sword" || kind == "mace" || kind == "trident" {
			return 2
		}
		return 1
	}
	return 0
}

// attackWear is Weapon.itemDamagePerAttack: two for a pickaxe, axe, shovel or
// hoe (ToolMaterial's tools), one for a sword, spear, mace or trident, and
// nothing for an item that is no weapon.
func attackWear(item int32) int {
	switch toolKind(item) {
	case "pickaxe", "axe", "shovel", "hoe":
		return 2
	case "sword", "spear", "mace", "trident":
		return 1
	}
	return 0
}

func (evToolWear) isHubEvent() {}

// applyToolWear adds wear to a hotbar stack, destroying it when it runs out.
func (h *hub) applyToolWear(t *tracked, slot, n int) {
	s := t.handStack(slot) // a hotbar slot, or the offhand a shield was raised in
	if !isSurvival(t.gamemode) || t.dead || s == nil {
		return
	}
	if s.item != 0 {
		h.advance(h.playersRef, t, "item_durability_changed", advMatch{item: s.item})
	}
	max, ok := itemMaxDurability[s.item]
	if !ok || s.count == 0 {
		return
	}
	if lvl := s.enchLvl(enchUnbreaking); lvl > 0 {
		if n = h.unbreakingKept(n, float64(lvl)/float64(lvl+1)); n == 0 {
			return // unbreaking ate every point of the wear
		}
	}
	if s.dmg += n; s.dmg >= max {
		// Stats.ITEM_BROKEN, and the snap everyone nearby hears.
		h.incStat(t, attachproto.StatBroken, s.item, 1)
		h.playSoundDim(h.playersRef, t.dim, "minecraft:entity.item.break", sndPlayer, t.x, t.y, t.z, 0.8, 0.8+h.rng.Float32()*0.4)
		*s = invStack{} // the tool breaks
	}
	if slot == offhandSlot {
		h.sendOffhand(t)
		return
	}
	h.sendSlot(t, slot)
}

// armorReduce applies the vanilla armor formula to incoming damage:
//
//	damage * (1 - min(20, max(points/5, points - damage/(2+toughness/4))) / 25)
func (t *tracked) armorReduce(dmg float32) float32 {
	t.refreshArmorAttrs() // pick up a piece equipped since the last tick
	points, tough := t.armorPoints(), t.armorToughness()
	d := float64(dmg)
	if points > 0 {
		def := math.Min(20, math.Max(points/5, points-d/(2+tough/4)))
		d *= 1 - def/25
	}
	// The protection ENCHANTMENTS used to be folded in here. They are a
	// separate step in vanilla (armour absorption then magic absorption) and
	// they now run centrally in damageOf, which also reaches the damage that
	// skips armour but not protection — falling being the obvious one.
	return float32(d)
}

// damageResistantItems are the items carrying vanilla's damage_resistant
// component, which spares them from wear by a damage type in the tag it names.
// Every damageable one is netherite, and the tag is is_fire — which is what
// makes netherite gear survive a lava bath that would eat diamond.
//
// The component is declared in code rather than a datapack, so unlike the
// damage tags it cannot be generated; the list is the sixteen fireResistant()
// items, minus the ones with no durability to lose.
var damageResistantItems = func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{
		"netherite_sword", "netherite_shovel", "netherite_pickaxe",
		"netherite_axe", "netherite_hoe", "netherite_helmet",
		"netherite_chestplate", "netherite_leggings", "netherite_boots",
		"netherite_horse_armor",
	} {
		if id, ok := itemByName[n]; ok {
			m[int32(id)] = true
		}
	}
	return m
}()

// resistsWear reports whether this item shrugs off wear from this sort of
// damage — vanilla ItemStack.canBeHurtBy, inverted.
func resistsWear(item int32, dt dmgType) bool {
	return damageResistantItems[item] && dt.has(tagIsFire)
}

// wearArmor wears every equipped piece after a hit (vanilla: max(1, damage/4)
// durability each), destroying pieces that run out. The armor slots are only
// visible in window 0, so resync is skipped while a container is open (the
// next full window refresh covers it).
func (h *hub) wearArmor(players map[int32]*tracked, t *tracked, dmg float32, dt dmgType) {
	n := int(dmg) / 4
	if n < 1 {
		n = 1
	}
	for i := range t.armor {
		a := &t.armor[i]
		if a.count == 0 {
			continue
		}
		max, ok := itemMaxDurability[a.item]
		if !ok {
			continue
		}
		if resistsWear(a.item, dt) {
			continue // netherite in a fire: the set comes out unmarked
		}
		kept := h.unbreakingKept(n, armourUnbreakingChance(a.enchLvl(enchUnbreaking)))
		if kept == 0 {
			continue // unbreaking spared this piece
		}
		if a.dmg += kept; a.dmg >= max {
			*a = invStack{} // the piece shatters
		}
		if t.winID == 0 {
			h.sendWinSlot(t, int16(5+i), *a)
		}
	}
	h.broadcastEquipment(players, t) // shattered pieces vanish for onlookers too
}

// wearArmorSlot wears ONE armour piece by a fixed amount — what Thorns costs
// the piece that retaliated (vanilla ChangeItemDamage(2) on that slot alone,
// not on the whole set).
func (h *hub) wearArmorSlot(players map[int32]*tracked, t *tracked, slot, n int, dt dmgType) {
	a := &t.armor[slot]
	if a.count == 0 {
		return
	}
	max, ok := itemMaxDurability[a.item]
	if !ok {
		return
	}
	if resistsWear(a.item, dt) {
		return
	}
	if n = h.unbreakingKept(n, armourUnbreakingChance(a.enchLvl(enchUnbreaking))); n == 0 {
		return
	}
	if a.dmg += n; a.dmg >= max {
		*a = invStack{}
	}
	if t.winID == 0 {
		h.sendWinSlot(t, int16(5+slot), *a)
	}
	h.broadcastEquipment(players, t)
}

// mendingRepair spends experience on a damaged Mending item before it ever
// reaches the player's bar, and returns the experience left over.
//
// Mending was in the treasure pools, the fishing pools and the anvil's tables
// and was wired to nothing — the single most valuable enchantment in the game
// did not repair anything. Vanilla picks ONE damaged item carrying it at
// random from the equipped slots (both hands and the armour), mends two points per experience
// point, and recurses with whatever is left so a single orb can finish one
// item and start on the next.
func (h *hub) mendingRepair(t *tracked, xp int) int {
	if t.inv == nil {
		return xp
	}
	mended := false
	defer func() {
		if !mended {
			return
		}
		// The client sees the repair: the two hands, and the worn pieces in
		// the player's own window.
		if t.p != nil {
			h.sendHandSlot(t, t.p.heldSlot())
			h.sendHandSlot(t, offhandSlot)
		}
		if t.winID == 0 {
			for i := range t.armor {
				h.sendWinSlot(t, int16(5+i), t.armor[i])
			}
		}
	}()
	for xp > 0 {
		s := h.pickMendable(t)
		if s == nil {
			return xp
		}
		mended = true
		repair := min(xp*mendingPerPoint, s.dmg)
		s.dmg -= repair
		// Experience is spent in proportion to what it actually mended, so a
		// nearly-repaired item does not swallow a whole orb.
		spent := (repair + mendingPerPoint - 1) / mendingPerPoint
		if spent <= 0 {
			return xp
		}
		xp -= spent
	}
	return xp
}

// mendingPerPoint is vanilla's exchange rate: two durability per experience.
const mendingPerPoint = 2

// pickMendable chooses a damaged Mending item at random from the held slots
// and the worn armour, as vanilla's getRandomItemWith does.
func (h *hub) pickMendable(t *tracked) *invStack {
	// EnchantmentHelper.getRandomItemWith: EQUIPPED items only — the main
	// hand, the offhand and the armour. A tool idling elsewhere on the hotbar
	// is not mended.
	var found []*invStack
	for _, slot := range []int{t.p.heldSlot(), offhandSlot} {
		if s := t.handStack(slot); s != nil && s.count > 0 && s.dmg > 0 && s.enchLvl(enchMending) > 0 {
			found = append(found, s)
		}
	}
	for i := range t.armor {
		if s := &t.armor[i]; s.count > 0 && s.dmg > 0 && s.enchLvl(enchMending) > 0 {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return found[h.rng.Intn(len(found))]
}

// armourUnbreakingSpares is Unbreaking's armour rule: a piece is spared
// with chance 2·lvl/(5·lvl+5) — 20%, 27%, 30% — not the tool rule's
// lvl/(lvl+1) (armour takes the hit 60% of the time before the roll).
func (h *hub) armourUnbreakingSpares(lvl int) bool {
	if lvl <= 0 {
		return false
	}
	return h.rng.Float64() < armourUnbreakingChance(lvl)
}

// armourUnbreakingChance is the armour branch's remove_binomial chance.
func armourUnbreakingChance(lvl int) float64 {
	if lvl <= 0 {
		return 0
	}
	return float64(2*lvl) / float64(5*lvl+5)
}

// unbreakingKept is Unbreaking's remove_binomial item_damage effect: each
// of the n points of wear is removed with chance p on its own, so a
// two-point hit can be half spared — not one roll for the whole event.
func (h *hub) unbreakingKept(n int, p float64) int {
	if p <= 0 {
		return n
	}
	kept := 0
	for i := 0; i < n; i++ {
		if h.rng.Float64() >= p {
			kept++
		}
	}
	return kept
}
