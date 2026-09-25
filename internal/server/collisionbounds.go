package server

// Block collision heights. The engine's own collision test is one bit per
// state (worldgen.Collides): full block or nothing. An entity that comes to
// rest ON a block needs the block's real height — a slab's half, a bed's
// nine sixteenths, a fence's block and a half — and collisionbounds_gen.go
// carries it for every state, from the running game (scripts/extract,
// field collisionBounds, via gen_collisionbounds.py).

// collisionRun is one run of collisionbounds_gen.go.
type collisionRun struct {
	lo, hi uint32
	empty  bool
	minY   float64
	maxY   float64
}

// collisionBounds is a state's collision shape bounds on the vertical axis
// (VoxelShape.bounds), ok false for an empty shape.
func collisionBounds(s uint32) (lo, hi float64, ok bool) {
	i, j := 0, len(collisionRuns)
	for i < j {
		m := (i + j) / 2
		if collisionRuns[m].hi < s {
			i = m + 1
		} else {
			j = m
		}
	}
	if i < len(collisionRuns) && collisionRuns[i].lo <= s {
		r := collisionRuns[i]
		if r.empty {
			return 0, 0, false
		}
		return r.minY, r.maxY, true
	}
	return 0, 1, true
}
