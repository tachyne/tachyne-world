package main

import (
	"fmt"
	"os"

	"tachyne/internal/world"
)

func main() {
	st := world.NewFileStore(os.Args[1])
	edits, err := st.Load()
	if err != nil {
		panic(err)
	}
	total, portals := 0, 0
	for cp, m := range edits {
		for idx, state := range m {
			total++
			if state == 6043 || state == 6044 {
				portals++
				ly := idx/256 - 64
				lz := (idx % 256) / 16
				lx := idx % 16
				fmt.Printf("portal block at (%d,%d,%d) state=%d\n", int(cp[0])*16+lx, ly, int(cp[1])*16+lz, state)
			}
		}
	}
	fmt.Printf("total edits=%d portal blocks=%d chunks=%d\n", total, portals, len(edits))
}
