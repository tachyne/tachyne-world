package server

import (
	"errors"
	"log"
	"time"
)

var errSaveTimeout = errors.New("the hub did not finish saving in time")

// /save-all, /save-off and /save-on (SaveAllCommand, SaveOffCommand,
// SaveOnCommand). Owner-level commands; the engine has one operator level.
//
// /save-off is vanilla's noSave: the periodic saves of the world's block
// and chunk data — block edits, container and block-entity contents, mobs
// — pause, while player data keeps saving. /save-all saves everything now
// whatever the switch says, and a shutdown still saves, as vanilla clears
// noSave on the way down. The switch is not remembered across a restart.

func (s *Server) cmdSaveAll(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) > 1 || (len(args) == 1 && args[0] != "flush") {
		p.tell("Usage: /save-all [flush]")
		return
	}
	s.info(p, "Saving the game (this may take a moment!)")
	if err := s.saveEverything(); err != nil {
		log.Printf("save-all failed: %v", err)
		p.tell("Unable to save the game (is there enough disk space?)")
		return
	}
	s.ok(p, "Saved the game")
}

// saveEverything writes all hub state and every dimension's edits now and
// waits for it — the flush form; the engine's saves are all synchronous.
func (s *Server) saveEverything() error {
	if s.hub != nil {
		done := make(chan struct{})
		s.hub.post(evSaveState{done: done})
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			return errSaveTimeout
		}
	}
	if s.nether != nil {
		if err := s.nether.Save(); err != nil {
			return err
		}
	}
	if s.end != nil {
		if err := s.end.Save(); err != nil {
			return err
		}
	}
	if s.world != nil {
		return s.world.Save()
	}
	return nil
}

func (s *Server) cmdSaveOff(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if !s.hub.saveOff.CompareAndSwap(false, true) {
		p.tell("Saving is already turned off")
		return
	}
	s.ok(p, "Automatic saving is now disabled")
}

func (s *Server) cmdSaveOn(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if !s.hub.saveOff.CompareAndSwap(true, false) {
		p.tell("Saving is already turned on")
		return
	}
	s.ok(p, "Automatic saving is now enabled")
}
