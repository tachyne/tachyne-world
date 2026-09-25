package server

import (
	"testing"
	"time"
)

// /setidletimeout through the dispatcher: the setting is taken, a player
// idle past it is disconnected on the next second, one who acted is not,
// and moving through the session resets the clock while turning does not.
func TestSetIdleTimeout(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice, bob, carol := ps["alice"], ps["bob"], ps["carol"]
	s.handleCommand(carol, "setidletimeout 1")
	s.handleCommand(alice, "setidletimeout 1")
	settle(t, h, logs, "I1")
	if got := linesBetween(logs["carol"], "", "I1"); !hasLine(got, "You don't have permission.") {
		t.Errorf("a non-operator set the timeout: %q", got)
	}
	if got := linesBetween(logs["alice"], "", "I1"); !hasLine(got, "The player idle timeout is now 1 minute(s)") {
		t.Errorf("no success line: %q", got)
	}
	long := time.Now().Add(-2 * time.Minute).UnixMilli()
	bob.lastAction.Store(long)
	carol.lastAction.Store(long)
	r := &remotePlayer{s: s, p: carol}
	r.Move(carol.x, carol.y, carol.z, 90, 10, true) // turning only
	if carol.lastAction.Load() != long {
		t.Error("turning on the spot reset the idle clock")
	}
	r.Move(carol.x+1, carol.y, carol.z, 90, 10, true)
	if carol.lastAction.Load() == long {
		t.Error("walking did not reset the idle clock")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-bob.quit:
			select {
			case <-carol.quit:
				t.Fatal("an active player was kicked")
			case <-alice.quit:
				t.Fatal("an active player was kicked")
			default:
			}
			s.handleCommand(alice, "setidletimeout 0")
			settle(t, h, map[string]*chatLog{"alice": logs["alice"]}, "I2")
			if got := linesBetween(logs["alice"], "I1", "I2"); !hasLine(got, "The player idle timeout is now disabled") {
				t.Errorf("no disabled line: %q", got)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the idle player was never disconnected")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
