package server

import (
	"encoding/json"
	"testing"

	"tachyne/internal/world"
)

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"action":"idle"}`:                  `{"action":"idle"}`,
		"Sure! {\"action\":\"wander\"} okay": `{"action":"wander"}`,
		"```json\n{\"action\":\"say\"}\n```": `{"action":"say"}`,
	}
	for in, want := range cases {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNPCActionParse(t *testing.T) {
	var a npcAction
	if err := json.Unmarshal([]byte(`{"action":"move_to","x":5,"z":-3}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Action != "move_to" || a.X == nil || *a.X != 5 || a.Z == nil || *a.Z != -3 {
		t.Fatalf("parsed wrong: %+v", a)
	}
	var say npcAction
	json.Unmarshal([]byte(`{"action":"say","text":"hello there"}`), &say)
	if say.Action != "say" || say.Text != "hello there" {
		t.Fatalf("say parsed wrong: %+v", say)
	}
}

func TestMemoryDedup(t *testing.T) {
	// The real spam Bram produced: one fact, reworded over and over.
	facts := []string{
		"Casey loves cats and dreams of having a black cat with their castle.",
		"Casey mentioned loving cats and hoping to have a black cat when they build their castle.",
		"Casey is really into having a black cat when they build their dream castle.",
		"Casey loves cats and dreams of having a black cat when they build their dream castle.",
	}
	var mem []string
	for _, f := range facts {
		mem, _ = addMemory(mem, f)
	}
	if len(mem) != 1 {
		t.Errorf("reworded duplicates should collapse to 1, got %d: %v", len(mem), mem)
	}
	mem, added := addMemory(mem, "Steve is a redstone engineer who builds big farms.")
	if !added || len(mem) != 2 {
		t.Errorf("a genuinely different fact should be added, got added=%v len=%d", added, len(mem))
	}
}

func TestNPCBehaviorSteersTowardTarget(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	n := h.spawnNPC(players, "Bram", "a farmer", 0, 0)
	n.targetX, n.targetZ, n.hasTarget = 10, 0, true

	vx, vz := npcBehavior{}.steer(h, n.mob)
	if vx <= 0 {
		t.Errorf("NPC should steer toward +x target, got vx=%v", vx)
	}
	if vz != 0 {
		t.Errorf("target is due +x, expected vz≈0, got %v", vz)
	}
	// With no target it idles.
	n.hasTarget = false
	if vx, vz := (npcBehavior{}).steer(h, n.mob); vx != 0 || vz != 0 {
		t.Errorf("idle NPC should not move, got (%v,%v)", vx, vz)
	}
}
