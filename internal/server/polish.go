package server

import (
	"log"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// Meta polish batch 1: the dragon's bossbar, sneaking poses relayed to other
// players, and dropped items that survive restarts.

const poseSneaking = 5 // (poseStanding comes from bed.go)

// Boss-bar domain events (rendered by render770 / the gateways).
func bossBarAdd(uuid [16]byte, title string, health float32) attachproto.BossBar {
	return attachproto.BossBar{UUID: uuid, Op: attachproto.BossBarAdd, Title: title, Health: health}
}

func bossBarHealth(uuid [16]byte, health float32) attachproto.BossBar {
	return attachproto.BossBar{UUID: uuid, Op: attachproto.BossBarHealth, Health: health}
}

func bossBarRemove(uuid [16]byte) attachproto.BossBar {
	return attachproto.BossBar{UUID: uuid, Op: attachproto.BossBarRemove}
}

// updateDragonBar keeps the End's players looking at the dragon's health.
func (h *hub) updateDragonBar(players map[int32]*tracked) {
	m := h.dragon
	for _, t := range players {
		if t.dim != 2 {
			if t.bossBarOn {
				t.bossBarOn = false
				t.p.trySendEv(bossBarRemove(dragonBarUUID))
			}
			continue
		}
		if m == nil {
			if t.bossBarOn {
				t.bossBarOn = false
				t.p.trySendEv(bossBarRemove(dragonBarUUID))
			}
			continue
		}
		frac := float32(m.health) / float32(dragonHealth)
		if !t.bossBarOn {
			t.bossBarOn = true
			t.p.trySendEv(bossBarAdd(dragonBarUUID, "Ender Dragon", frac))
			// Boss refresher: whatever the arrival flood did to the original
			// spawn packet, this player is now settled — give them a clean
			// destroy+spawn so the dragon exists client-side unconditionally.
			t.p.sendEv(entGone(m.eid))
			t.p.sendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, m.yaw, 0))
			log.Printf("end: dragon refresh sent to %q (dragon eid=%d, player eid=%d)", t.p.name, m.eid, t.p.eid)
		} else {
			t.p.trySendEv(bossBarHealth(dragonBarUUID, frac))
		}
	}
}

var dragonBarUUID = [16]byte{0xd7, 0xa6, 0x0e, 0x11, 0x11, 0x11, 0x41, 0x11, 0x81, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x01}

// poseMeta builds the entity-pose metadata (sneak/stand relay).
func poseMeta(eid int32, pose int32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexPose)
	b = protocol.AppendVarInt(b, metaTypePose)
	b = protocol.AppendVarInt(b, pose)
	return protocol.AppendU8(b, itemMetaEnd)
}

type evSneak struct {
	eid      int32
	sneaking bool
}

func (evSneak) isHubEvent() {}
