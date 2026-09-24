package server

// A window click arrives as the client's declared result: each changed slot
// and the cursor as an item and a count. Everything else about a stack — a
// potion's type, a shulker box's or bundle's contents, dye, wear,
// enchantments, a lodestone's target, the repair cost — never crosses the
// wire, so the server must carry it from the stack the item came from.
// Vanilla runs the click on its own stacks (AbstractContainerMenu.doClick)
// and never has the question; the engine reconciles the client's answer
// against its stacks instead, and clickCarry is the pairing.
//
// Every destination draws its count from the old stacks of the same item,
// preferring those in another place — an item that is somewhere new came
// from somewhere else: a swap crosses, a shift-click moves, a split takes
// from the stack it split — and falling back to its own place's old stack.
// Its data is the first source's. What no destination takes is what left
// the window.

// clickSource is one old stack a click can draw from: the cursor (loc -1) or
// a changed slot, and how much of it no destination has taken yet.
type clickSource struct {
	loc  int16
	st   invStack
	left int
}

func stackCount(st invStack) int {
	if st.item == 0 || st.count <= 0 {
		return 0
	}
	return st.count
}

// clickCarry gives each declared destination (locs[i]: a slot, or -1 for the
// cursor) the full stack it holds after the click.
func clickCarry(srcs []*clickSource, dests []invStack, locs []int16) []invStack {
	out := make([]invStack, len(dests))
	for i, d := range dests {
		if d.item == 0 || d.count <= 0 {
			continue // empty
		}
		need := d.count
		var first *clickSource
		take := func(s *clickSource) {
			if need == 0 || s.left == 0 || s.st.item != d.item {
				return
			}
			n := min(need, s.left)
			s.left -= n
			need -= n
			if first == nil {
				first = s
			}
		}
		for _, s := range srcs { // from another place first
			if s.loc != locs[i] {
				take(s)
			}
		}
		for _, s := range srcs { // then what was already here
			if s.loc == locs[i] {
				take(s)
			}
		}
		if first == nil {
			out[i] = d // nothing to carry (a creative clone): the bare item
			continue
		}
		st := first.st
		st.count = d.count
		out[i] = st
	}
	return out
}
