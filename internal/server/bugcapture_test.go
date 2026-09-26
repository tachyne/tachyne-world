package server

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// bugCapture is a /bug report's block capture as the report stores it: the
// region's low corner, its size, and run-length "Nxname[props]" strings in
// y, then z, then x order.
type bugCapture struct {
	Origin [3]int   `json:"origin"`
	Size   [3]int   `json:"size"`
	Blocks []string `json:"blocks"`
}

// stampBugCapture rebuilds a report's capture (testdata/<file>) where it was
// taken, loading the chunks around it first.
func stampBugCapture(t *testing.T, w *world.World, file string) bugCapture {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var c bugCapture
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	w.ForceLoad(c.Origin[0]+c.Size[0]/2, c.Origin[2]+c.Size[2]/2, 2)
	i := 0
	for _, run := range c.Blocks {
		n, name := 1, run
		if k := strings.IndexByte(run, 'x'); k > 0 {
			if v, err := strconv.Atoi(run[:k]); err == nil {
				n, name = v, run[k+1:]
			}
		}
		st, ok := parseBlockState(name)
		if !ok {
			t.Fatalf("capture block %q does not parse", name)
		}
		for ; n > 0; n-- {
			x, z, y := i%c.Size[0], (i/c.Size[0])%c.Size[2], i/(c.Size[0]*c.Size[2])
			w.SetBlock(c.Origin[0]+x, c.Origin[1]+y, c.Origin[2]+z, st)
			i++
		}
	}
	if want := c.Size[0] * c.Size[1] * c.Size[2]; i != want {
		t.Fatalf("capture holds %d cells, want %d", i, want)
	}
	return c
}
