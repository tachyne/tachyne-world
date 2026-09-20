package server

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A report carries the blocks around the reporter, so a description like
// "pistons adjacent to a dust line are not working" arrives as something that
// can be rebuilt rather than guessed at.
func TestBugReportCapturesTheBuild(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pl.dim = dimOverworld
	pl.x, pl.y, pl.z = 400.5, 180, 400.5
	pl.yaw, pl.pitch = 90, 10

	// Something distinctive beside the reporter: a lever and a line of dust.
	lever := worldgen.BlockBase("lever")
	li, _ := worldgen.InfoForState(lever)
	lever = setBoolProp(worldgen.SetProperty(li, lever, "face", "floor"), "powered", true)
	h.world.SetBlock(401, 180, 400, lever)
	for i := 2; i <= 4; i++ {
		h.world.SetBlock(400+i, 180, 400, worldgen.BlockBase("redstone_wire"))
	}

	id := h.fileBugReport(players, pl, "pistons next to my dust line do nothing")
	if id != 1 {
		t.Fatalf("first report should be #1, got %d", id)
	}
	got := h.bugs.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected one report, got %d", len(got))
	}
	r := got[0]
	if r.Player != pl.p.name || !strings.Contains(r.Text, "dust line") {
		t.Errorf("who and what should be kept verbatim: %+v", r)
	}
	if r.Dim != dimOverworld || r.X != 400.5 {
		t.Errorf("where should be kept: dim %d at %v,%v,%v", r.Dim, r.X, r.Y, r.Z)
	}
	// The region must actually contain the build, and be replayable.
	if r.Size != [3]int{bugRegionXZ*2 + 1, bugRegionDown + bugRegionUp + 1, bugRegionXZ*2 + 1} {
		t.Errorf("region size %v", r.Size)
	}
	joined := strings.Join(r.Blocks, " ")
	if !strings.Contains(joined, "lever") {
		t.Error("the lever beside the reporter should be in the capture")
	}
	if !strings.Contains(joined, "redstone_wire") {
		t.Error("the dust line should be in the capture")
	}
	// A lever that is ON must say so — the properties are the whole point.
	if !strings.Contains(joined, "powered=true") {
		t.Errorf("non-default properties should be recorded, got %q", firstMatch(r.Blocks, "lever"))
	}
	// Run-length encoding should keep it small rather than one entry per block.
	if cells := r.Size[0] * r.Size[1] * r.Size[2]; len(r.Blocks) > cells/4 {
		t.Errorf("%d runs for %d cells — the encoding is not compressing", len(r.Blocks), cells)
	}
}

// describeState names a block and only the properties that differ from its
// own default, so a capture stays readable.
func TestDescribeStateIsMinimal(t *testing.T) {
	plain := worldgen.BlockID("stone")
	if got := describeState(plain); got != "stone" {
		t.Errorf("a default block is just its name, got %q", got)
	}
	lever := worldgen.BlockID("lever")
	on := setBoolProp(lever, "powered", true)
	got := describeState(on)
	if !strings.HasPrefix(got, "lever[") || !strings.Contains(got, "powered=true") {
		t.Errorf("a changed property should show, got %q", got)
	}
	if strings.Count(got, "=") > 2 {
		t.Errorf("only what differs should show, got %q", got)
	}
}

func firstMatch(runs []string, want string) string {
	for _, r := range runs {
		if strings.Contains(r, want) {
			return r
		}
	}
	return ""
}

// A captured region has to come back as the states it went in as, or the
// fixture bugrepro.py emits rebuilds the wrong world.
func TestRegionRoundTrips(t *testing.T) {
	w := world.New(1)
	const ox, oy, oz = 500, 180, 500
	lever := worldgen.BlockBase("lever")
	li, _ := worldgen.InfoForState(lever)
	lever = setBoolProp(worldgen.SetProperty(li, lever, "face", "floor"), "powered", true)
	want := map[[3]int]uint32{
		{0, 0, 0}: worldgen.Stone,
		{1, 1, 0}: lever,
		{2, 1, 0}: worldgen.BlockBase("redstone_wire"),
		{3, 1, 1}: worldgen.BlockID("oak_door"),
	}
	for d := range want {
		w.SetBlock(ox+d[0], oy+d[1], oz+d[2], want[d])
	}
	size := [3]int{5, 3, 5}
	runs := encodeRegion(w, [3]int{ox, oy, oz}, size)

	// Replay into a fresh world and compare.
	w2 := world.New(1)
	var cells []string
	for _, r := range runs {
		n, name, _ := strings.Cut(r, "x")
		c, err := strconv.Atoi(n)
		if err != nil {
			t.Fatalf("bad run %q", r)
		}
		for i := 0; i < c; i++ {
			cells = append(cells, name)
		}
	}
	if len(cells) != size[0]*size[1]*size[2] {
		t.Fatalf("%d cells for a %v region", len(cells), size)
	}
	for dy := 0; dy < size[1]; dy++ {
		for dz := 0; dz < size[2]; dz++ {
			for dx := 0; dx < size[0]; dx++ {
				w2.SetBlock(ox+dx, oy+dy, oz+dz, stateFromDescription(cells[(dy*size[2]+dz)*size[0]+dx]))
			}
		}
	}
	for d, st := range want {
		if got := w2.At(ox+d[0], oy+d[1], oz+d[2]); got != st {
			t.Errorf("at %v: replayed %d, captured %d (%s)", d, got, st, describeState(st))
		}
	}
}

// A reply reaches a player who is here, and waits for one who is not — the
// usual case, since whoever answers a report is rarely online at the same
// time as whoever filed it.
func TestReplyReachesOnlineAndWaitsForOffline(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	here := survPlayer(h)
	here.p.name = "Here"
	players[here.p.eid] = here
	here.p.out = make(chan outPkt, 16)

	if delivered := h.queueReply(players, "Here", "fixed, deploying now", 0); !delivered {
		t.Error("a player who is online should get it straight away")
	}
	if delivered := h.queueReply(players, "Away", "fixed that too", 0); delivered {
		t.Error("a player who is not online cannot be delivered to")
	}

	// The absent one gets it when they arrive.
	away := survPlayer(h)
	away.p.name = "Away"
	away.p.out = make(chan outPkt, 16)
	h.deliverQueuedReplies(away)
	close(away.p.out)
	got := 0
	for range away.p.out {
		got++
	}
	if got == 0 {
		t.Error("the queued reply should arrive on join")
	}
	// And only once.
	away2 := survPlayer(h)
	away2.p.name = "Away"
	away2.p.out = make(chan outPkt, 16)
	h.deliverQueuedReplies(away2)
	close(away2.p.out)
	for range away2.p.out {
		t.Error("a queued reply should not be delivered twice")
	}
}

// Answering a report records the answer against it, so the list shows what
// has been dealt with.
func TestReplyNotesTheReport(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = 600.5, 180, 600.5
	pl.p.out = make(chan outPkt, 16)

	id := h.fileBugReport(players, pl, "lichen sprouts sideways")
	h.queueReply(players, pl.p.name, "the fence connector was treating it as a fence", id)

	got := h.bugs.snapshot()
	if len(got) != 1 || !strings.Contains(got[0].Note, "fence connector") {
		t.Errorf("the answer should be recorded on the report, got %+v", got)
	}
}
