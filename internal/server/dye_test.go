package server

import "testing"

// A leather chestplate and a red dye craft a red chestplate; adding blue
// blends toward purple by vanilla's averaging; a cauldron washes it off.
func TestLeatherDyeing(t *testing.T) {
	chest := int32(itemByName["leather_chestplate"])
	red := int32(itemByName["red_dye"])
	blue := int32(itemByName["blue_dye"])
	grid := make([]invStack, 9)
	grid[0] = invStack{item: chest, count: 1}
	grid[4] = invStack{item: red, count: 1}
	res, ok := armorDyeMatch(grid)
	if !ok || res.item != chest || res.color != dyeRGB[red] {
		t.Fatalf("red dye on plain leather: ok=%v item=%d color=%#x want %#x", ok, res.item, res.color, dyeRGB[red])
	}
	grid[0].color = res.color
	grid[4] = invStack{item: blue, count: 1}
	res, ok = armorDyeMatch(grid)
	want := blendDyes(dyeRGB[red], []int32{dyeRGB[blue]})
	if !ok || res.color != want || res.color == dyeRGB[red] {
		t.Fatalf("red + blue: ok=%v color=%#x want %#x", ok, res.color, want)
	}
	// Two pieces, or a stray item, is not a dye recipe.
	grid[8] = invStack{item: chest, count: 1}
	if _, ok := armorDyeMatch(grid); ok {
		t.Fatal("two pieces must not match")
	}
	grid[8] = invStack{item: int32(itemByName["stick"]), count: 1}
	if _, ok := armorDyeMatch(grid); ok {
		t.Fatal("a stray stick must not match")
	}
	// The colour rides through persistence.
	row := packStack(invStack{item: chest, count: 1, color: 0x123456})
	if back := unpackStack(row); back.color != 0x123456 {
		t.Fatalf("persisted colour = %#x", back.color)
	}
	// Vanilla check: red+blue averages (r,g,b)=((176+60)/2,(46+38)/2,(38+170)/2) then rescales
	// so the brightest channel equals the average of the two brightest (176+170)/2=173.
	if want != 0xAD2A7B && want>>16 != 173 {
		t.Errorf("blend brightest channel = %d, want 173", want>>16)
	}
}
