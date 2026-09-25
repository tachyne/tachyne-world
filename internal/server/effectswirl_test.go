package server

import (
	"bytes"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// swirlsSeen drains a player's queue for the last effect-swirl frame about eid.
func swirlsSeen(pl *tracked, eid int32) []byte {
	var last []byte
	for {
		select {
		case pkt := <-pl.p.out:
			if m, ok := pkt.ev.(attachproto.EntityMeta); ok && m.EID == eid && len(m.Meta) > 0 && m.Meta[0] == metaEffectParticles {
				last = m.Meta
			}
		default:
			return last
		}
	}
}

// Drinking Speed shows its swirls (entity_effect in the effect's colour,
// full alpha) to the drinker and to a player beside them; a beacon's
// ambient effect is faint (alpha 38) and flags the entity all-ambient;
// hideParticles shows nothing; Oozing swirls its own particle; the last
// effect going clears the list.
func TestEffectSwirls(t *testing.T) {
	h := newHub(world.New(1))
	pl, near := survPlayer(h), survPlayer(h)
	near.p.eid = pl.p.eid + 50
	players := map[int32]*tracked{pl.p.eid: pl, near.p.eid: near}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	near.x, near.y, near.z = 3.5, 80, 0.5

	h.applyEffect(players, pl, effSpeed, 0, 30)
	want := []byte{metaEffectParticles, metaTypeParticles, 1, particleEntityEffect, 0xff, 0x33, 0xeb, 0xff, metaEffectAmbience, metaTypeBool, 0, 0xff}
	if got := swirlsSeen(near, pl.p.eid); !bytes.Equal(got, want) {
		t.Fatalf("the neighbour saw %x, want %x", got, want)
	}
	if got := swirlsSeen(pl, pl.p.eid); !bytes.Equal(got, want) {
		t.Fatalf("the drinker saw %x, want %x", got, want)
	}

	h.removeEffect(pl, effSpeed)
	h.applyEffectFrom(players, pl, effHaste, 0, 30, true) // a beacon
	want = []byte{metaEffectParticles, metaTypeParticles, 1, particleEntityEffect, 38, 0xd9, 0xc0, 0x43, metaEffectAmbience, metaTypeBool, 1, 0xff}
	if got := swirlsSeen(near, pl.p.eid); !bytes.Equal(got, want) {
		t.Fatalf("a beacon's haste: %x, want %x", got, want)
	}

	h.addEffect(players, pl, effOozing, activeEffect{amp: 0, left: 200})
	h.addEffect(players, pl, effNightVision, activeEffect{amp: 0, left: 200, noParticles: true})
	got := swirlsSeen(near, pl.p.eid)
	if len(got) < 3 || got[2] != 2 || !bytes.Contains(got, []byte{49}) {
		t.Fatalf("haste + oozing + hidden night vision: %x, want two particles, oozing's 49 among them", got)
	}

	h.clearEffects(pl)
	want = []byte{metaEffectParticles, metaTypeParticles, 0, metaEffectAmbience, metaTypeBool, 1, 0xff}
	if got := swirlsSeen(near, pl.p.eid); !bytes.Equal(got, want) {
		t.Fatalf("after clearing: %x, want %x", got, want)
	}
}
