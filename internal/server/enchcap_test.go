package server

import "testing"

// A fully enchanted sword — seven enchantments — survives the persisted
// row and the item-entity record; the old four-slot cap is gone.
func TestSevenEnchantmentsPersist(t *testing.T) {
	st := invStack{item: itemByName["diamond_sword"], count: 1}
	for i, id := range []int8{enchSharpness, enchUnbreaking, 22, 18, enchFireAspect, enchKnockback, enchSweepingEdge} {
		st = withEnch(st, id, int8(1+i%3))
	}
	n := 0
	for _, e := range st.ench {
		if e.lvl > 0 {
			n++
		}
	}
	if n != 7 {
		t.Fatalf("withEnch should have taken all seven, got %d", n)
	}
	if back := unpackStack(packStack(st)); back.ench != st.ench {
		t.Fatalf("row round trip lost enchantments: %v vs %v", back.ench, st.ench)
	}
	if back := unpackEnch4(packEnch(st.ench), packEnchHi(st.ench), packEnch3(st.ench), packEnch4(st.ench)); back != st.ench {
		t.Fatalf("column round trip lost enchantments: %v vs %v", back, st.ench)
	}
	// A row written before the widening (28 columns) still decodes its four.
	full := packStack(st)
	var row stackRow
	copy(row[:28], full[:28])
	if got := unpackStack(row); got.ench[3] != st.ench[3] || got.ench[4].lvl != 0 {
		t.Fatalf("an old row keeps its first four and no more: %v", got.ench)
	}
}
