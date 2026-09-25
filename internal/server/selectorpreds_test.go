package server

import "testing"

// The selector predicates through the dispatcher: gamemode, scores, team
// (and its negation), level, x/y/z with distance, a volume, sort=furthest
// with a limit across the result, and spaces inside the brackets.
func TestSelectorPredicates(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		"scoreboard objectives add k dummy",
		"scoreboard players set bob k 5",
		"team add red",
		"team join red carol",
		"xp set bob 10 levels",
		"summon pig 3 ~ 0",
		"summon pig 12 ~ 0",
		"tag @a[gamemode=creative] add c",
		"tag @a[scores={k=3..}] add s",
		"tag @a[team=red] add t",
		"tag @a[team=!red] add nt",
		"tag @a[level=5..] add lv",
		"tag @a[ name = bob , limit=1 ] add sp",
		"tag @a[x=100,y=0,z=100,distance=..5] add none",
		"tag @a[x=-1,y=-64,z=-1,dx=3,dy=400,dz=3] add vol",
		"tag @e[type=pig,sort=furthest,limit=1] add far",
		"tag @e[sort=nearest,limit=1,name=!alice] add one",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "S1")
	onHub(t, h, func() {
		tagged := func(tag string) map[string]bool {
			out := map[string]bool{}
			for _, tr := range h.playersRef {
				if tr.tags[tag] {
					out[tr.p.name] = true
				}
			}
			for _, m := range h.mobs {
				if m.tags[tag] {
					out[mobDisplayName(m.etype)+"@"+itoa(int(m.x))] = true
				}
			}
			return out
		}
		want := map[string][]string{
			"c": {"alice", "bob", "carol"}, "s": {"bob"}, "t": {"carol"}, "nt": {"alice", "bob"},
			"lv": {"bob"}, "sp": {"bob"}, "none": nil, "vol": {"alice", "bob", "carol"},
		}
		for tag, names := range want {
			got := tagged(tag)
			if len(got) != len(names) {
				t.Errorf("tag %s went to %v, want %v", tag, got, names)
				continue
			}
			for _, n := range names {
				if !got[n] {
					t.Errorf("tag %s went to %v, want %v", tag, got, names)
				}
			}
		}
		far := tagged("far")
		if len(far) != 1 {
			t.Errorf("sort=furthest,limit=1 tagged %v", far)
		}
		for _, m := range h.mobs {
			if m.tags["far"] && m.x < 10 {
				t.Error("sort=furthest picked the nearer pig")
			}
		}
		if one := tagged("one"); len(one) != 1 {
			t.Errorf("@e[limit=1] tagged %v, want exactly one entity", one)
		}
	})
}
