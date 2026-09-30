package server

import (
	"encoding/json"
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// A death reaches chat and the death screen as vanilla's translatable
// component (death.attack.<id> with the victim and killer), a mob killer
// by its type's translation key; the English stays beside it.
func TestDeathMessageIsTranslatable(t *testing.T) {
	h := newTestHub(world.New(1))
	h.rules.ShowDeathMsgs = true
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	drainEvs(pl.p)
	h.hurtBy(players, pl, 100, dtMobAttack, deathCause{by: mobDisplayName(entityZombie)})
	if !pl.dead {
		t.Fatal("the blow did not kill")
	}
	english := pl.p.name + " was slain by Zombie"
	var chat *attachproto.Chat
	var death *attachproto.Death
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.Chat:
			if e.Text == english {
				chat = &e
			}
		case attachproto.Death:
			death = &e
		}
	}
	if chat == nil {
		t.Fatal("no death line in chat")
	}
	var comp map[string]any
	if err := json.Unmarshal(chat.Component, &comp); err != nil || comp["translate"] != "death.attack.mob" ||
		!strings.Contains(string(chat.Component), `"entity.minecraft.zombie"`) {
		t.Errorf("the chat line should be death.attack.mob with the zombie's key: %s", chat.Component)
	}
	if death == nil || death.Message != english || death.Component == nil ||
		death.Component.Translate != "death.attack.mob" || len(death.Component.With) != 2 ||
		death.Component.With[0].Text != pl.p.name || death.Component.With[1].Translate != "entity.minecraft.zombie" {
		t.Fatalf("the death screen should carry the component: %+v", death)
	}
}

// The other forms: a named weapon in square brackets, a player killer
// literal, the bad respawn point's bracketed link.
func TestDeathMessageComponentForms(t *testing.T) {
	m := deathMessageOf("Wesley", deathCause{dt: dtPlayerAttack, by: "EdgeZA", weapon: "Excalibur"})
	c := m.component()
	if c.Translate != "death.attack.player.item" || len(c.With) != 3 || c.With[1].Text != "EdgeZA" ||
		c.With[2].Translate != "chat.square_brackets" || c.With[2].With[0].Text != "Excalibur" {
		t.Errorf("named weapon: %+v", c)
	}
	if got := m.english(); got != "Wesley was slain by EdgeZA using Excalibur" {
		t.Errorf("English changed: %q", got)
	}
	c = deathMessageOf("Wesley", deathCause{dt: dtBadRespawnPoint}).component()
	if c.Translate != "death.attack.badRespawnPoint.message" || len(c.With) != 2 ||
		c.With[1].With[0].Translate != "death.attack.badRespawnPoint.link" {
		t.Errorf("intentional game design: %+v", c)
	}
}

// The bed's refusals are vanilla's translatable overlays, and setting the
// spawn its translatable chat line.
func TestBedRefusalsAreTranslatable(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.dayTime.Store(1000) // day
	drainEvs(pl.p)
	h.handleUseBed(players, pl, tBedHead)
	var keys []string
	overlay := map[string]bool{}
	for _, ev := range drainEvs(pl.p) {
		if c, ok := ev.(attachproto.Chat); ok && len(c.Component) > 0 {
			var comp map[string]any
			if json.Unmarshal(c.Component, &comp) == nil {
				k, _ := comp["translate"].(string)
				keys = append(keys, k)
				overlay[k] = c.ActionBar
			}
		}
	}
	if a, ok := overlay["block.minecraft.bed.no_sleep"]; !ok || !a {
		t.Errorf("by day the bed refuses with the no_sleep overlay: %v", keys)
	}
	if a, ok := overlay["block.minecraft.set_spawn"]; !ok || a {
		t.Errorf("the spawn is set with a chat line: %v", keys)
	}
}
