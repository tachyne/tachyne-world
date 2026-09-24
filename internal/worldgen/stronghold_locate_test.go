package worldgen

import "testing"

// StructurePlacement.getLocatePos: a stronghold is located at the minimum
// corner of the chunk it starts in (where its staircase is), which is where
// an eye of ender leads — not the portal room, which can be a hundred blocks
// on through the maze.
func TestStrongholdLocatesAtItsStartChunk(t *testing.T) {
	g := NewGenerator(1)
	found := 0
	for i := -3; i <= 3 && found < 3; i++ {
		for j := -3; j <= 3 && found < 3; j++ {
			st := g.StrongholdIn(i*strongholdCell, j*strongholdCell)
			if !st.Exists {
				continue
			}
			found++
			if st.LocX&15 != 0 || st.LocZ&15 != 0 {
				t.Errorf("locate pos %d,%d is not a chunk corner", st.LocX, st.LocZ)
			}
			if st.pieces[0].world(0, 0, 0)[0]>>4 != st.LocX>>4 && st.pieces[0].world(0, 0, 0)[2]>>4 != st.LocZ>>4 {
				t.Errorf("locate pos %d,%d is not the start piece's chunk", st.LocX, st.LocZ)
			}
		}
	}
	if found == 0 {
		t.Fatal("no stronghold found to check")
	}
}
