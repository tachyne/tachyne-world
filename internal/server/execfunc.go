package server

import "fmt"

// Binds /execute's function seam (execute.go) to the data pack runtime
// (fnexec.go, fncmds.go).
func init() {
	execShareContext = func(s *Server, from, to *player) func() { return s.shareFunctionContext(from, to) }
	execForkLimitFor = func(s *Server, p *player) (int, bool) {
		l, ok := s.fnLimitsFor(p)
		return l.forks, ok
	}
	execRunFunctions = execIfFunction
}

// execIfFunction is ExecuteCommand's function condition for one source:
// every function the argument names runs, silently, in one isolated call,
// and the first value any of them returns decides the source. When none
// returns a value the callback never fires and the source is dropped
// whichever way the condition reads (returned is false).
func execIfFunction(s *Server, src *player, arg string) (int, bool, string) {
	name, fns, _, fail := s.resolveFunctionArg(arg)
	if fail != "" {
		return 0, false, fail
	}
	if len(fns) == 0 {
		return 0, false, "Can't find any functions for name " + name
	}
	insts := make([]fnInstance, 0, len(fns))
	for _, f := range fns {
		lines, err := f.instantiate(nil)
		if err != nil {
			return 0, false, fmt.Sprintf("Failed to instantiate function %s: %v", f.id, err)
		}
		insts = append(insts, fnInstance{id: f.id, lines: lines})
	}
	var frames []*fnFrame
	if e := s.execFor(src); e != nil {
		for _, fi := range insts {
			fr := s.fnCall(e, fi)
			frames = append(frames, fr)
			if fr.returned || e.stopped {
				break
			}
		}
	} else {
		var lim fnLimits
		if !s.hubSync(func() { src.fnSilent.Add(1); lim = s.hub.fnLimitsNow() }) {
			return 0, false, ""
		}
		frames = s.runFunctions(src, insts, lim)
		s.hub.post(evRunOnHub{fn: func() { src.fnSilent.Add(-1) }})
	}
	for _, f := range frames {
		if f.returned {
			return f.value, true, ""
		}
	}
	return 0, false, ""
}
