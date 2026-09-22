package server

// The salmon's size. Vanilla rolls one of three sizes when a salmon spawns and
// syncs it as DATA_TYPE — the same index the tropical fish's variant uses,
// because both are AbstractFish and its FROM_BUCKET field takes 16. Without it
// every salmon in the world is the same medium one, and the little ones that
// should be half-size never appear.

// Salmon.Variant ids, in enum order.
const (
	salmonSmall  = 0
	salmonMedium = 1
	salmonLarge  = 2
)

// salmonSizeWeights is Salmon.finalizeSpawn's weighted list: 30 small, 50
// medium, 15 large.
var salmonSizeWeights = [...]struct {
	size, weight int32
}{{salmonSmall, 30}, {salmonMedium, 50}, {salmonLarge, 15}}

// rollSalmonSize draws one size from that list.
func (h *hub) rollSalmonSize() int32 {
	total := int32(0)
	for _, e := range salmonSizeWeights {
		total += e.weight
	}
	r := int32(h.rng.Intn(int(total)))
	for _, e := range salmonSizeWeights {
		if r -= e.weight; r < 0 {
			return e.size
		}
	}
	return salmonMedium
}

// salmonScale is Variant.boundingBoxScale — what the client scales the model
// and the hitbox by.
func salmonScale(variant int32) float32 {
	switch variant {
	case salmonSmall:
		return 0.5
	case salmonLarge:
		return 1.5
	}
	return 1
}
