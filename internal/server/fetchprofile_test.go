package server

import (
	"strings"
	"testing"
)

// /fetchprofile through the dispatcher: by name (an online player, and one
// only the name cache knows), by id, from an entity; the failures in
// vanilla's words; and the profile component the actions carry.
func TestFetchProfile(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	onHub(t, h, func() {
		ps["bob"].props = []skinProperty{{Name: "textures", Value: "dGV4dHVyZXM=", Signature: "c2ln"}}
	})
	dave, _ := parseUUIDString("0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0")
	ids.learn("Dave", dave)
	s.handleCommand(alice, "fetchprofile name bob")
	s.handleCommand(alice, "fetchprofile name Dave")
	s.handleCommand(alice, "fetchprofile id "+uuidString(ps["carol"].uuid))
	s.handleCommand(alice, "fetchprofile name nobody_here")
	s.handleCommand(alice, "fetchprofile id 00000000-0000-0000-0000-00000000abcd")
	s.handleCommand(alice, "fetchprofile entity bob")
	s.handleCommand(alice, "summon cow ~ ~ ~")
	s.handleCommand(alice, "fetchprofile entity @e[type=cow,limit=1]")
	settle(t, h, logs, "FP")
	a := linesBetween(logs["alice"], "", "FP")
	for _, want := range []string{
		"Resolved profile for name bob: [Copy Component] [Give Item] [Summon Mannequin] [Copy bob]",
		"Resolved profile for name Dave: [Copy Component] [Give Item] [Summon Mannequin] [Copy Dave]",
		"Resolved profile for ID " + uuidString(ps["carol"].uuid) + ": [Copy Component] [Give Item] [Summon Mannequin] [Copy carol]",
		"Failed to resolve profile for name nobody_here",
		"Failed to resolve profile for ID 00000000-0000-0000-0000-00000000abcd",
		"Resolved profile for entity bob: [Copy Component] [Give Item] [Summon Mannequin] [Copy bob]",
		"Entity Cow has no profile",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	got := profileSNBT(gameProfile{id: dave, name: "Dave", props: []skinProperty{{Name: "textures", Value: "v", Signature: "s"}}})
	if want := `{id:[I;253635900,1264216440,-2020170316,-1009589776],name:"Dave",properties:[{name:"textures",value:"v",signature:"s"}]}`; got != want {
		t.Errorf("profile SNBT\n got %s\nwant %s", got, want)
	}
	if _, err := parseSNBT(got); err != nil || !strings.HasPrefix(got, "{id:[I;") {
		t.Errorf("the profile does not read back: %v", err)
	}
}
