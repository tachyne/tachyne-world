package server

// Mob buckets — the vanilla Bucketable contract and MobBucketItem. A water
// bucket used on a cod, salmon, pufferfish, tropical fish, axolotl or tadpole
// picks the mob up (Bucketable.bucketMobPickup): pickup sound, the water
// bucket becomes that mob's bucket, filled_bucket fires, any lead drops, the
// mob is discarded. Pouring the bucket out places its water (MobBucketItem
// content is WATER, so the nether boils it off like any water bucket) and
// spawns the mob in the cell, flagged fromBucket so it never despawns
// (Mob.checkDespawn honours requiresCustomPersistence/persistenceRequired,
// which setFromBucket sets on every Bucketable).
//
// The bucket carries the mob's variant (an axolotl's colour) and a baby's
// age; its Health is not carried, so a released mob comes out at full health.

// mobBucketOf maps a bucketable species to its bucket item and the two
// sounds (getPickupSound / MobBucketItem.emptySound).
type mobBucket struct {
	item        int32
	fill, empty string
}

var (
	mobBucketBySpecies = map[int]mobBucket{}
	speciesByMobBucket = map[int32]int{}
)

func init() {
	add := func(etype int, item, fill, empty string) {
		id, ok := itemByName[item]
		if !ok || etype == 0 {
			return
		}
		mobBucketBySpecies[etype] = mobBucket{item: id,
			fill: "minecraft:item.bucket.fill_" + fill, empty: "minecraft:item.bucket.empty_" + empty}
		speciesByMobBucket[id] = etype
	}
	add(entityCod, "cod_bucket", "fish", "fish")
	add(entitySalmon, "salmon_bucket", "fish", "fish")
	add(entityPufferfish, "pufferfish_bucket", "fish", "fish")
	add(entityTropicalFish, "tropical_fish_bucket", "fish", "fish")
	add(entityAxolotl, "axolotl_bucket", "axolotl", "axolotl")
	add(entityTadpole, "tadpole_bucket", "tadpole", "tadpole")
	// The sulfur cube bucket is scooped with an EMPTY bucket and pours no
	// fluid, so its two ends are sulfurcube.go's; it is listed here so the
	// use routing knows it for a mob bucket.
	add(entitySulfurCube, "sulfur_cube_bucket", "sulfur_cube", "sulfur_cube")
}

// isMobBucket reports whether an item is one of the six mob buckets.
func isMobBucket(item int32) bool {
	_, ok := speciesByMobBucket[item]
	return ok
}

// tryBucketMob is Bucketable.bucketMobPickup: a water bucket on a bucketable
// mob scoops it. Returns whether the interaction was consumed.
func (h *hub) tryBucketMob(players map[int32]*tracked, t *tracked, m *mob) bool {
	if heldStack(t).item != itemBucketH2O || m.dying > 0 {
		return false // canBePickedUpWithBucket: WATER_BUCKET only; isAlive
	}
	mb, ok := mobBucketBySpecies[m.etype]
	if !ok {
		return false
	}
	h.playSoundDim(players, m.dim, mb.fill, sndNeutral, m.x, m.y, m.z, 1, 1)
	filled := invStack{item: mb.item, count: 1}
	if m.variantSet { // saveToBucketTag: the axolotl's colour rides in the bucket
		filled.cube.variant = m.variant + 1
	}
	if m.baby {
		filled.cube.age = -int32(max(1, m.growLeft))
	}
	h.giveFilledStack(players, t, int32(t.p.heldSlot()), filled)
	h.advance(players, t, "filled_bucket", advMatch{item: mb.item})
	h.dropLeash(players, m, true) // Leashable.dropLeash: the lead pops out
	h.removeMob(players, m)
	return true
}

// releaseBucketMob is MobBucketItem.checkExtraContent: after the bucket's
// water has been poured (or boiled off), spawn the mob in the cell with the
// bucket's empty sound in place of the water one.
func (h *hub) releaseBucketMob(players map[int32]*tracked, dim int, item int32, data cubeContent, x, y, z int) {
	etype := speciesByMobBucket[item]
	mb := mobBucketBySpecies[etype]
	m := h.spawnSpecies(players, etype, dim, float64(x)+0.5, float64(y), float64(z)+0.5)
	if m != nil {
		m.fromBucket = true
		// loadFromBucketTag: the variant and age it went in with.
		if data.variant > 0 {
			m.variant, m.variantSet = data.variant-1, true
			if meta := variantMeta(m); meta != nil {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(meta))
			}
		}
		if data.age < 0 && !m.baby {
			m.baby, m.growLeft = true, int(-data.age)
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
		}
	}
	h.playSoundDim(players, dim, mb.empty, sndNeutral, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
}
