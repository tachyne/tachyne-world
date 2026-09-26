package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// enderFixture is the prey fixture's stone floor at y=179 with an enderman
// on it and a survival player eight blocks east who is tracking it, at
// midnight so the daylight flight stays out of it.
func enderFixture(t *testing.T) (*hub, map[int32]*tracked, *mob, *tracked) {
	t.Helper()
	h, players := preyFixture(t)
	h.dayTime.Store(18000)
	a := survPlayer(h)
	a.p.eid = 900
	a.x, a.y, a.z = 8.5, 180, 0.5
	players[a.p.eid] = a
	m := h.spawnHostileY(players, entityEnderman, -3.5, 180, 0.5)
	if m == nil {
		t.Fatal("no enderman")
	}
	a.yaw, a.pitch = -90, 0 // facing +x, away from it
	trackEnderman(a, m)
	return h, players, m, a
}

func trackEnderman(p *tracked, m *mob) {
	if p.tracked == nil {
		p.tracked = map[int32]bool{}
	}
	p.tracked[m.eid] = true
}

// aimAtEnderEyes puts p's crosshair on the enderman's eyes, from p's real eyes.
func aimAtEnderEyes(p *tracked, m *mob) {
	dx, dy, dz := m.x-p.x, m.y+mobEyeHeight(m)-playerEyeY(p), m.z-p.z
	p.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	p.pitch = float32(-math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi)
}

// enderSteps runs whole hub updates, keeping the players alive (the fight
// is not the point).
func enderSteps(h *hub, players map[int32]*tracked, n int) {
	for i := 0; i < n; i++ {
		h.tick.Add(mobMoveInterval)
		for _, p := range players {
			p.health = 20
		}
		h.updateMobs(players)
	}
}

// enderFlagFrames drains p's queue for the CREEPY/STARED_AT frames about
// eid, each as its entries in order ({index, value} pairs).
func enderFlagFrames(p *tracked, eid int32) [][][2]byte {
	var out [][][2]byte
	for {
		select {
		case pkt := <-p.p.out:
			fr, ok := pkt.ev.(attachproto.EntityMeta)
			if !ok || fr.EID != eid {
				continue
			}
			var entries [][2]byte
			b := fr.Meta
			for len(b) >= 3 && b[0] != itemMetaEnd {
				if (b[0] != metaIndexEndermanCreepy && b[0] != metaIndexEndermanStared) || b[1] != metaTypeBool {
					entries = nil
					break
				}
				entries = append(entries, [2]byte{b[0], b[2]})
				b = b[3:]
			}
			if entries != nil {
				out = append(out, entries)
			}
		default:
			return out
		}
	}
}

// A player who holds its eyes becomes its target a moment later — that
// player, not the nearer one who is looking elsewhere. STARED_AT goes up
// first and CREEPY after (the order the client's stare sound needs), the
// chase speed goes on, the scream replaces the ambient sound, and the
// target is held out of sight and range until it cannot be attacked.
func TestEndermanStareTargetsTheStarer(t *testing.T) {
	h, players, m, a := enderFixture(t)
	b := survPlayer(h)
	b.p.eid = 901
	b.x, b.y, b.z = -1.5, 180, 0.5
	b.yaw = -90 // looking away
	players[b.p.eid] = b
	aimAtEnderEyes(a, m)

	enderSteps(h, players, 1)
	if m.enderPending != a.p.eid || m.hasTarget {
		t.Fatalf("the stare should start the look goal on the starer: pending=%d target=%v", m.enderPending, m.hasTarget)
	}
	enderSteps(h, players, endermanAggroDelay-1)
	if m.targetEID != a.p.eid || !m.enderLook {
		t.Fatalf("aggroTime later the starer is the target: target=%d look=%v", m.targetEID, m.enderLook)
	}
	frames := enderFlagFrames(a, m.eid)
	if len(frames) != 2 || frames[0][0] != [2]byte{metaIndexEndermanStared, 1} || frames[1][0] != [2]byte{metaIndexEndermanCreepy, 1} {
		t.Fatalf("want STARED_AT then CREEPY, got %v", frames)
	}
	if v := m.mobAttrs().Value(attr.MovementSpeed); math.Abs(v-0.45*attrToStep) > 1e-6 {
		t.Errorf("with a target it moves at 0.45, got %v", v/attrToStep)
	}
	if _, _, amb := h.mobSoundsFor(m); amb != "minecraft:entity.enderman.scream" {
		t.Errorf("a creepy enderman screams, got %q", amb)
	}
	// It stands frozen under the stare and looks back.
	aimAtEnderEyes(a, m)
	sx, sz := m.x, m.z
	enderSteps(h, players, 5)
	if m.angryAt != a.p.eid || m.anger < neutralAngerMin {
		t.Errorf("the target is the grudge: angryAt=%d anger=%d", m.angryAt, m.anger)
	}
	if !m.enderHeld || m.x != sx || m.z != sz {
		t.Errorf("stared at from twelve blocks it holds still: held=%v moved %.2f", m.enderHeld, math.Hypot(m.x-sx, m.z-sz))
	}
	if d := wrapDegrees(float64(m.headYaw - yawToward(m.x, m.z, a.x, a.z))); math.Abs(d) > 1 {
		t.Errorf("the frozen enderman looks at its starer: head %v off", d)
	}
	// A viewer who arrives now is shown both flags on pairing.
	c := survPlayer(h)
	c.p.eid = 902
	h.showMobTo(c, m)
	if fr := enderFlagFrames(c, m.eid); len(fr) != 1 || len(fr[0]) != 2 {
		t.Errorf("pairing should carry CREEPY and STARED_AT, got %v", fr)
	}

	// Out of sight and out of follow range: still the target.
	a.yaw = -90
	a.x = m.x + 100
	enderSteps(h, players, 20)
	if m.targetEID != a.p.eid {
		t.Fatalf("the look goal keeps its target with no sight or range test; target=%d", m.targetEID)
	}
	// Creative ends it, and the grudge with it.
	a.gamemode = gmCreative
	enderFlagFrames(a, m.eid)
	enderSteps(h, players, 2)
	if m.hasTarget || m.angryAt != 0 {
		t.Fatalf("a creative player is no target or grudge: target=%v angryAt=%d", m.hasTarget, m.angryAt)
	}
	if fr := enderFlagFrames(a, m.eid); len(fr) != 1 || len(fr[0]) != 2 || fr[0][0][1] != 0 || fr[0][1][1] != 0 {
		t.Errorf("both flags come down together, got %v", fr)
	}
	if v := m.mobAttrs().Value(attr.MovementSpeed); math.Abs(v-0.3*attrToStep) > 1e-6 {
		t.Errorf("without a target it moves at 0.3, got %v", v/attrToStep)
	}
	if _, _, amb := h.mobSoundsFor(m); amb != "minecraft:entity.enderman.ambient" {
		t.Errorf("a calm enderman's ambient, got %q", amb)
	}
}

// A blow makes the attacker the target and the grudge (20-39 s, held while
// the target is). Lost, the grudge runs down; while it lasts the player is
// taken up again on sight; then it is gone.
func TestEndermanHitHoldsAGrudge(t *testing.T) {
	h, players, m, a := enderFixture(t)
	a.x = m.x + 2
	h.attackMob(players, a.p.eid, m.eid)
	if m.targetEID != a.p.eid || m.angryAt != a.p.eid {
		t.Fatalf("the attacker is the target and the grudge: target=%d angryAt=%d", m.targetEID, m.angryAt)
	}
	if m.anger < neutralAngerMin || m.anger > neutralAngerMin+neutralAngerRange {
		t.Fatalf("PERSISTENT_ANGER_TIME is 20-39 s, got %d updates", m.anger)
	}
	enderSteps(h, players, endermanAggroDelay+1)
	if m.targetEID != a.p.eid || !m.enderLook {
		t.Fatalf("seen and angry at, the look goal takes the attacker over: target=%d look=%v", m.targetEID, m.enderLook)
	}
	// Dead: the target goes, the grudge stays.
	a.dead = true
	enderSteps(h, players, 1)
	if m.hasTarget || m.angryAt != a.p.eid || m.anger == 0 {
		t.Fatalf("a dead target is dropped, the grudge kept: target=%v angryAt=%d anger=%d", m.hasTarget, m.angryAt, m.anger)
	}
	a.dead = false
	a.x, a.z = m.x+2, m.z
	enderSteps(h, players, endermanAggroDelay+1)
	if m.targetEID != a.p.eid {
		t.Fatalf("back in sight while it is still angry, the player is the target again: %d", m.targetEID)
	}
	a.dead = true
	enderSteps(h, players, neutralAngerMin+neutralAngerRange+2)
	if m.anger != 0 || m.angryAt != 0 || m.hasTarget || m.enderSent != 0 {
		t.Fatalf("the grudge should have run out: anger=%d angryAt=%d target=%v flags=%b", m.anger, m.angryAt, m.hasTarget, m.enderSent)
	}
}

// The grudge rides the save (angry_at by UUID, the time in Anger), comes
// back as the target when that player is here, and follows the player to
// a new eid after a reconnect.
func TestEndermanGrudgeSurvivesAReload(t *testing.T) {
	h, players, m, a := enderFixture(t)
	a.x = m.x + 2
	h.attackMob(players, a.p.eid, m.eid)
	sm := toSavedMob(m)
	if sm.AngryAt == "" || sm.Anger != m.anger {
		t.Fatalf("the save carries the grudge: %q anger=%d", sm.AngryAt, sm.Anger)
	}
	r := h.reloadMob(players, &sm)
	if r.angryUUID != a.p.uuid || r.anger != m.anger || r.targetEID != a.p.eid {
		t.Fatalf("reloaded: uuid ok=%v anger=%d target=%d", r.angryUUID == a.p.uuid, r.anger, r.targetEID)
	}
	delete(players, a.p.eid)
	a.p.eid = 950
	players[a.p.eid] = a
	if g := h.endermanGrudge(players, r); g != a || r.angryAt != 950 {
		t.Fatalf("the grudge follows the player to their new eid: %v %d", g != nil, r.angryAt)
	}
}

// isBeingStaredBy: from the player's real eyes (a crouch is lower), not
// through a wall, and never through #gaze_disguise_equipment.
func TestEndermanStareTest(t *testing.T) {
	h, _, m, a := enderFixture(t)
	a.x = m.x + 10
	aimAtEnderEyes(a, m)
	if !h.endermanStaredBy(m, a) {
		t.Fatal("a crosshair on its eyes is a stare")
	}
	h.world.SetBlock(int(math.Floor(m.x))+5, 182, 0, worldgen.BlockBase("stone"))
	if h.endermanStaredBy(m, a) {
		t.Fatal("a wall between them blocks the stare")
	}
	h.world.SetBlock(int(math.Floor(m.x))+5, 182, 0, worldgen.BlockID("air"))
	for _, it := range worldgen.ItemTag("gaze_disguise_equipment") {
		a.armor[0] = invStack{item: int32(itemByName[it]), count: 1}
		if h.endermanStaredBy(m, a) {
			t.Fatalf("%s is a disguise", it)
		}
	}
	a.armor[0] = invStack{}

	// A crouching player a block and a half off with their eyes level with
	// the enderman's: level is a stare, but the same look from standing
	// height is not.
	a.x, a.y = m.x+1.5, m.y+mobEyeHeight(m)-playerEyeSneak
	a.p.sneaking, a.sneaking = true, true
	a.yaw, a.pitch = 90, 0
	if !h.endermanStaredBy(m, a) {
		t.Fatal("a crouching player's eyes are lower")
	}
	a.p.sneaking, a.sneaking = false, false
	if h.endermanStaredBy(m, a) {
		t.Fatal("from standing height that look passes over its eyes")
	}
}

// The freeze and the close blink are for its target only: another player's
// stare does neither.
func TestEndermanFreezeIsForItsTarget(t *testing.T) {
	h, players, m, a := enderFixture(t)
	a.x = m.x + 10
	aimAtEnderEyes(a, m)
	enderSteps(h, players, endermanAggroDelay)
	if m.targetEID != a.p.eid || !m.enderHeld {
		t.Fatalf("setup: target=%d held=%v", m.targetEID, m.enderHeld)
	}
	b := survPlayer(h)
	b.p.eid = 901
	b.x, b.y, b.z = m.x, 180, m.z-3
	players[b.p.eid] = b
	a.yaw = -90 // a looks away; b stares from three blocks
	aimAtEnderEyes(b, m)
	sx, sz := m.x, m.z
	h.endermanTarget(players, m)
	if m.enderHeld {
		t.Error("a stare from somebody other than its target does not hold it")
	}
	if m.x != sx || m.z != sz {
		t.Error("nor does it blink away from them")
	}
	// Its own target at three blocks: it blinks away.
	moved := false
	for i := 0; i < 20 && !moved; i++ {
		a.x, a.z = m.x+3, m.z
		aimAtEnderEyes(a, m)
		h.endermanTarget(players, m)
		moved = m.x != sx || m.z != sz
	}
	if !moved {
		t.Error("its target staring from under four blocks makes it blink away")
	}
}

// customServerAiStep rolls every TICK, two to an update, and it takes an
// enderman whose target has gone unchanged for 600 ticks — a look-goal
// target, angry or not.
func TestEndermanDaylightFlightRollsEveryTick(t *testing.T) {
	h, players, m, a := enderFixture(t)
	h.dayTime.Store(6000)
	a.x = m.x + 20
	h.tick.Store(10000)
	f := h.lightMagic(m)
	if f <= endermanDayLightMin || !h.skyExposed(m) {
		t.Skipf("fixture not lit by the sky: %v", f)
	}
	const n = 4000
	fired := 0
	for i := 0; i < n; i++ {
		m.x, m.y, m.z = -3.5, 180, 0.5
		h.endermanSetTarget(m, a)
		m.enderLook, m.enderTargetAt, m.stareTicks, m.anger, m.angryAt = true, 1, 0, 300, a.p.eid
		h.endermanTarget(players, m)
		if !m.hasTarget {
			fired++
		}
	}
	p := (f - 0.4) * 2 / 30
	want := n * (1 - (1-p)*(1-p))
	if math.Abs(float64(fired)-want) > 5*math.Sqrt(want) {
		t.Fatalf("fired %d of %d, want ~%.0f (two rolls an update at p=%.3f)", fired, n, want, p)
	}
	// A target set within the last 600 ticks keeps it.
	h.endermanSetTarget(m, a)
	m.enderLook = true
	for i := 0; i < 200; i++ {
		h.endermanTarget(players, m)
	}
	if !m.hasTarget {
		t.Fatal("a target set this tick holds off the daylight flight for 600 ticks")
	}
}

// teleportTowards steps back along the full 3D line from the target's eyes,
// and the look goal's teleportTime resets only when a blink lands.
func TestEndermanTeleportTowards(t *testing.T) {
	h, players, m, a := enderFixture(t)
	// A target straight overhead: the old flat vector had nothing to go on.
	a.x, a.y, a.z = m.x, m.y+40, m.z
	oy := m.y
	moved := false
	for i := 0; i < 50 && !moved; i++ {
		// Above the floor there is nothing to land on but the floor itself
		// under the aim point — which must be ABOVE the enderman to find it.
		m.x, m.y, m.z = -3.5, 180, 0.5
		dir := (m.y + m.box().h*0.5 - playerEyeY(a))
		if dir >= 0 {
			t.Fatal("setup: the target is above")
		}
		moved = h.endermanTeleportTowards(players, m, a)
	}
	if !moved || m.y != oy {
		t.Fatalf("a blink towards an overhead target aims up and drops to the floor: moved=%v y=%v", moved, m.y)
	}

	// Far off and not looking, the blink aimed into a chunk that is not
	// loaded, where it cannot land: teleportTime keeps counting past the
	// delay.
	m.x, m.y, m.z = 40.5, 180, 0.5
	a.x, a.y, a.z = 3000, 180, 0.5
	a.yaw = -90
	h.endermanSetTarget(m, a)
	m.enderLook, m.stareTicks = true, 0
	for i := 0; i < endermanChaseDelay+10; i++ {
		h.endermanTarget(players, m)
	}
	if m.stareTicks < endermanChaseDelay+10 {
		t.Fatalf("a blink that failed must not reset teleportTime: %d", m.stareTicks)
	}
}
