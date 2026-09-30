package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// /function, /return, /schedule, /reload and /datapack (FunctionCommand,
// ReturnCommand, ScheduleCommand, ReloadCommand, DataPackCommand).

// initDataPacks is the boot-time pack setup: the repository read, the
// saved selection applied (configurePackRepository), the functions loaded,
// and the load tag due on the first tick. It runs before the hub starts.
func (s *Server) initDataPacks() {
	if s.DataPackDir == "" && s.WorldFile != "" {
		s.DataPackDir = filepath.Join(filepath.Dir(s.WorldFile), "datapacks")
	}
	h := s.hub
	cfg := h.rules.DataPacks
	if cfg == nil {
		cfg = &dataPackConfig{Enabled: []string{vanillaPackID}} // DataPackConfig.DEFAULT
	}
	avail := discoverPacks(s.DataPackDir)
	selected := configurePacks(avail, cfg)
	lib := buildLibrary(selectedPacks(avail, selected))
	lib.available = avail
	lib.config = packConfigFor(avail, selected)
	h.rules.DataPacks = lib.config
	h.functions.Store(lib)
	h.fnPostReload.Store(true)
	log.Printf("datapacks: %d enabled %v, %d functions, %d function tags", len(selected), selected, len(lib.functions), len(lib.tags))
}

// reloadDataPacks is MinecraftServer.reloadResources: the packs re-read,
// the selection set (packs no longer on disk drop out), the functions and
// tags replaced, the load tag due again, and the selection saved.
func (s *Server) reloadDataPacks(selected []string) *functionLibrary {
	s.packMu.Lock()
	defer s.packMu.Unlock()
	avail := discoverPacks(s.DataPackDir)
	var sel []string
	for _, id := range selected {
		if findPack(avail, id) != nil && !hasPackID(sel, id) {
			sel = append(sel, id)
		}
	}
	lib := buildLibrary(selectedPacks(avail, sel))
	lib.available = avail
	lib.config = packConfigFor(avail, sel)
	h := s.hub
	h.functions.Store(lib)
	cfg := lib.config
	h.post(evRunOnHub{fn: func() {
		h.rules.DataPacks = cfg
		h.fnPostReload.Store(true)
		h.saveRules()
	}})
	log.Printf("datapacks: reloaded %d packs %v, %d functions, %d function tags", len(sel), sel, len(lib.functions), len(lib.tags))
	return lib
}

// resolveFunctionArg is FunctionArgument: an id names one function, #id a
// tag's functions. name is the id as feedback shows it.
func (s *Server) resolveFunctionArg(arg string) (name string, fns []*mcFunction, isTag bool, fail string) {
	lib := s.hub.functions.Load()
	if strings.HasPrefix(arg, "#") {
		id, ok := parseResID(arg[1:])
		if !ok {
			return "", nil, true, "Invalid ID"
		}
		if !lib.hasTag(id) {
			return id, nil, true, fmt.Sprintf("Unknown function tag '%s'", id)
		}
		return id, lib.tag(id), true, ""
	}
	id, ok := parseResID(arg)
	if !ok {
		return "", nil, false, "Invalid ID"
	}
	f := lib.function(id)
	if f == nil {
		return id, nil, false, "Unknown function " + id
	}
	return id, []*mcFunction{f}, false, ""
}

// cmdFunction is /function <name> [<arguments> | with <source> [<path>]].
func (s *Server) cmdFunction(p *player, args []string) {
	const usage = "Usage: /function <name> [<arguments>|with (block <pos>|entity <target>|storage <id>) [<path>]]"
	if len(args) == 0 {
		p.tell(usage)
		return
	}
	name, fns, _, fail := s.resolveFunctionArg(args[0])
	if fail != "" {
		p.tell(fail)
		return
	}
	if len(fns) == 0 {
		p.tell("Can't find any functions for name " + name)
		return
	}
	var fargs map[string]any
	switch {
	case len(args) == 1:
	case args[1] == "with":
		m, msg := s.functionArgsFrom(p, args[2:])
		if msg != "" {
			p.tell(msg)
			return
		}
		fargs = m
	case len(args) == 2:
		v, err := parseSNBT(args[1])
		if err != nil {
			p.tell(err.Error())
			return
		}
		m, ok := v.(map[string]any)
		if !ok {
			p.tell("Expected compound tag")
			return
		}
		fargs = m
	default:
		p.tell(usage)
		return
	}

	if e := s.execFor(p); e != nil {
		s.functionInContext(e, fns, fargs)
		return
	}

	// A new execution context: "Running function …", the instantiation (a
	// macro's arguments), the calls, then each call's result.
	if len(fns) == 1 {
		s.ok(p, "Running function "+fns[0].id)
	} else {
		ids := make([]string, len(fns))
		for i, f := range fns {
			ids[i] = f.id
		}
		s.ok(p, "Running functions "+strings.Join(ids, ", "))
	}
	var insts []fnInstance
	for _, f := range fns {
		lines, err := f.instantiate(fargs)
		if err != nil {
			msg := fmt.Sprintf("Failed to instantiate function %s: %v", f.id, err)
			s.onHub(func(map[int32]*tracked) { cmdFail(p, msg) }) // after the line above
			break
		}
		insts = append(insts, fnInstance{id: f.id, lines: lines})
	}
	if len(insts) == 0 {
		return
	}
	var lim fnLimits
	if !s.hubSync(func() { p.fnSilent.Add(1); lim = s.hub.fnLimitsNow() }) {
		return
	}
	frames := s.runFunctions(p, insts, lim)
	s.hub.post(evRunOnHub{fn: func() { p.fnSilent.Add(-1) }}) // after every hub half the lines posted
	for i, f := range frames {
		if f.returned {
			s.ok(p, fmt.Sprintf("Function %s returned %d", insts[i].id, f.value))
		}
	}
}

// functionInContext is a /function run by a function: the calls join the
// running context, silently. Under `return run` the calls' results become
// the result of the frame that ran the return.
func (s *Server) functionInContext(e *fnExec, fns []*mcFunction, fargs map[string]any) {
	returnMode := e.returnRun
	e.returnRun = false
	parent := e.top()
	for _, f := range fns {
		lines, err := f.instantiate(fargs)
		if err != nil {
			return // FunctionInstantiationException: the command fails, silently
		}
		fr := s.fnCall(e, fnInstance{id: f.id, lines: lines})
		if returnMode && parent != nil && fr.returned {
			parent.returned, parent.success, parent.value = true, fr.success, fr.value
		}
		if e.stopped {
			return
		}
	}
}

// functionArgsFrom is `function … with <source> [path]`: the compound a
// block entity, an entity or command storage holds, or the one under path
// in it. The engine has no /data storage, so a storage is always empty,
// as vanilla's CommandStorage answers for one never written.
func (s *Server) functionArgsFrom(p *player, spec []string) (map[string]any, string) {
	const usage = "Usage: /function <name> with (block <pos>|entity <target>|storage <id>) [<path>]"
	if len(spec) < 2 {
		return nil, usage
	}
	var pathArg string
	var got map[string]any
	var msg string
	switch spec[0] {
	case "storage":
		if _, ok := parseResID(spec[1]); !ok {
			return nil, "Invalid ID"
		}
		if len(spec) > 3 {
			return nil, usage
		}
		if len(spec) == 3 {
			pathArg = spec[2]
		}
		got = map[string]any{}
	case "entity":
		if len(spec) > 3 {
			return nil, usage
		}
		if len(spec) == 3 {
			pathArg = spec[2]
		}
		target := spec[1]
		if !s.hubSync(func() {
			players := s.hub.playersRef
			s.hub.runHubCmd(players, func(players map[int32]*tracked) {
				ens := s.hub.commandEntitiesAll(players, p.eid, target)
				switch {
				case len(ens) == 0:
					msg = "No entity was found"
				case len(ens) > 1:
					msg = "Only one entity is allowed, but the provided selector allows more than one"
				default:
					got = s.hub.entityNBTView(ens[0])
				}
			})
		}) {
			return nil, errConsoleBusy.Error()
		}
	case "block":
		if len(spec) < 4 || len(spec) > 5 {
			return nil, usage
		}
		if len(spec) == 5 {
			pathArg = spec[4]
		}
		coords := spec[1:4]
		if !s.hubSync(func() {
			players := s.hub.playersRef
			s.hub.runHubCmd(players, func(players map[int32]*tracked) {
				t := players[p.eid]
				if t == nil {
					msg = "No entity was found"
					return
				}
				x, y, z, ok := parsePosition(coords, t.x, t.y, t.z, t.yaw, t.pitch)
				if !ok {
					msg = "Invalid position"
					return
				}
				w := s.hub.worldFor(t.dim)
				if w == nil {
					msg = "The target block is not a block entity"
					return
				}
				bx, by, bz := int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))
				got = s.hub.blockEntityNBT(simPos{dim: t.dim, blockPos: blockPos{bx, by, bz}}, w.At(bx, by, bz))
				if got == nil {
					msg = "The target block is not a block entity"
				}
			})
		}) {
			return nil, errConsoleBusy.Error()
		}
	default:
		return nil, usage
	}
	if msg != "" {
		return nil, msg
	}
	if pathArg == "" {
		return got, ""
	}
	return nbtPathCompound(got, pathArg)
}

// nbtPathCompound follows a dotted NBT path of plain keys (a.b.c) to a
// compound — the part of NbtPathArgument the engine reads.
func nbtPathCompound(root map[string]any, path string) (map[string]any, string) {
	if strings.ContainsAny(path, "[]{}\"'") {
		return nil, "Invalid NBT path element"
	}
	var cur any = root
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok || key == "" {
			return nil, "Found no elements matching " + path
		}
		v, ok := m[key]
		if !ok {
			return nil, "Found no elements matching " + path
		}
		cur = v
	}
	m, ok := cur.(map[string]any)
	if !ok {
		return nil, "Invalid argument type: " + nbtTypeName(cur) + ". Expected Compound"
	}
	return m, ""
}

// nbtTypeName is Tag.getType().getName() for a parsed value.
func nbtTypeName(v any) string {
	switch v.(type) {
	case string:
		return "String"
	case int64, int, int32:
		return "Int"
	case float64, float32:
		return "Double"
	case bool:
		return "Byte"
	case []any:
		return "List"
	}
	return "Compound"
}

// cmdReturn is /return <value> | fail | run <command>.
func (s *Server) cmdReturn(p *player, line string) {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "return"))
	e := s.execFor(p)
	var f *fnFrame
	if e != nil {
		f = e.top()
	}
	switch {
	case rest == "run" || strings.HasPrefix(rest, "run "):
		sub := strings.TrimSpace(rest[3:])
		if sub == "" {
			p.tell("Unknown or incomplete command. See below for error")
			return
		}
		if f == nil {
			s.handleCommand(p, sub) // outside a function it is the command itself
			return
		}
		f.done = true
		if !e.spend() {
			return
		}
		root := sub
		if i := strings.IndexAny(sub, " \t"); i >= 0 {
			root = sub[:i]
		}
		switch root {
		case "function":
			e.returnRun = true
			s.handleCommand(p, sub)
			e.returnRun = false
		case "return":
			s.handleCommand(p, sub) // its value is this frame's
		default:
			// The dispatcher here reports no command results, so a command
			// that ran counts as a success of 1.
			s.handleCommand(p, sub)
			if !f.returned {
				f.returned, f.success, f.value = true, true, 1
			}
		}
	case rest == "fail":
		if f != nil {
			f.done, f.returned, f.success, f.value = true, true, false, 0
		}
	default:
		n, err := strconv.ParseInt(rest, 10, 32)
		if err != nil {
			if rest == "" {
				p.tell("Usage: /return <value>|fail|run <command>")
			} else {
				p.tell(fmt.Sprintf("Invalid integer '%s'", rest))
			}
			return
		}
		if f != nil {
			f.done, f.returned, f.success, f.value = true, true, true, int(n)
		}
	}
}

// cmdSchedule is /schedule function <function> <time> [append|replace] and
// /schedule clear <function>.
func (s *Server) cmdSchedule(p *player, args []string) {
	const usage = "Usage: /schedule function <function> <time> [append|replace] | /schedule clear <function>"
	h := s.hub
	if len(args) == 2 && args[0] == "clear" {
		id, ok := parseResID(args[1])
		if !ok {
			p.tell("Invalid ID")
			return
		}
		s.onHub(func(players map[int32]*tracked) {
			n := h.sched.remove(id)
			if n == 0 {
				cmdFail(p, "No schedules with ID "+id)
				return
			}
			h.cmdSuccess(players, p, fmt.Sprintf("Removed %d schedule(s) with ID %s", n, id), true)
			h.saveRules()
		})
		return
	}
	if len(args) < 3 || len(args) > 4 || args[0] != "function" {
		p.tell(usage)
		return
	}
	replace := true
	if len(args) == 4 {
		switch args[3] {
		case "append":
			replace = false
		case "replace":
		default:
			p.tell(usage)
			return
		}
	}
	name, fns, isTag, fail := s.resolveFunctionArg(args[1])
	if fail != "" {
		p.tell(fail)
		return
	}
	ticks, ok := parseTimeTicks(args[2:3])
	if !ok {
		p.tell("Invalid unit")
		return
	}
	if ticks < 0 {
		p.tell(fmt.Sprintf("The tick count must not be less than 0, found %d", ticks))
		return
	}
	if ticks == 0 {
		p.tell("Can't schedule for current tick")
		return
	}
	var cb schedCallback
	var schedID, what string
	if isTag {
		cb, schedID, what = schedCallback{Tag: true, ID: name}, "#"+name, "tag"
	} else {
		if fns[0].macro {
			p.tell("Can't schedule a macro")
			return
		}
		cb, schedID, what = schedCallback{ID: name}, name, "function"
	}
	s.onHub(func(players map[int32]*tracked) {
		at := h.tick.Load() + uint64(ticks)
		if replace {
			h.sched.remove(schedID)
		}
		h.sched.schedule(schedID, at, cb)
		h.cmdSuccess(players, p, fmt.Sprintf("Scheduled %s '%s' in %d tick(s) at gametime %d", what, name, ticks, at), true)
		h.saveRules()
	})
}

// cmdReload is /reload: the selection plus every pack found since that is
// not on the disabled list, reloaded; the load tag runs on the next tick.
func (s *Server) cmdReload(p *player, args []string) {
	if len(args) != 0 {
		p.tell("Usage: /reload")
		return
	}
	lib := s.hub.functions.Load()
	selected := lib.selectedIDs()
	var disabled []string
	if lib != nil && lib.config != nil {
		disabled = lib.config.Disabled
	}
	for _, pk := range discoverPacks(s.DataPackDir) {
		if !hasPackID(disabled, pk.id) && !hasPackID(selected, pk.id) {
			selected = append(selected, pk.id)
		}
	}
	s.ok(p, "Reloading!")
	s.reloadDataPacks(selected)
}

// currentSelection is the selection as the repository now sees it: the
// selected ids still on disk, and the packs found.
func (s *Server) currentSelection() ([]string, []*dataPack) {
	avail := discoverPacks(s.DataPackDir)
	var sel []string
	for _, id := range s.hub.functions.Load().selectedIDs() {
		if findPack(avail, id) != nil {
			sel = append(sel, id)
		}
	}
	return sel, avail
}

// cmdDatapack is /datapack list [available|enabled] | enable <name>
// [first|last|before <existing>|after <existing>] | disable <name> |
// create <id> <description>.
func (s *Server) cmdDatapack(p *player, args []string) {
	const usage = "Usage: /datapack list [available|enabled] | enable <name> [first|last|before <existing>|after <existing>] | disable <name> | create <id> <description>"
	if len(args) == 0 {
		p.tell(usage)
		return
	}
	switch args[0] {
	case "list":
		if len(args) > 2 || (len(args) == 2 && args[1] != "available" && args[1] != "enabled") {
			p.tell(usage)
			return
		}
		sel, avail := s.currentSelection()
		if len(args) == 1 || args[1] == "enabled" {
			s.listEnabledPacks(p, sel, avail)
		}
		if len(args) == 1 || args[1] == "available" {
			s.listAvailablePacks(p, sel, avail)
		}
	case "enable":
		if len(args) < 2 {
			p.tell(usage)
			return
		}
		sel, avail := s.currentSelection()
		pk, msg := packArg(avail, sel, args[1], true)
		if msg != "" {
			p.tell(msg)
			return
		}
		switch {
		case len(args) == 2 || (len(args) == 3 && args[2] == "last"):
			sel = append(sel, pk.id) // Pack.Position.TOP, and no pack is fixed at the top
		case len(args) == 3 && args[2] == "first":
			sel = append([]string{pk.id}, sel...)
		case len(args) == 4 && (args[2] == "before" || args[2] == "after"):
			ex, msg := packArg(avail, sel, args[3], false)
			if msg != "" {
				p.tell(msg)
				return
			}
			i := 0
			for i < len(sel) && sel[i] != ex.id {
				i++
			}
			if args[2] == "after" {
				i++
			}
			sel = append(sel[:i], append([]string{pk.id}, sel[i:]...)...)
		default:
			p.tell(usage)
			return
		}
		s.ok(p, "Enabling data pack "+pk.chatLink())
		s.reloadDataPacks(sel)
	case "disable":
		if len(args) != 2 {
			p.tell(usage)
			return
		}
		sel, avail := s.currentSelection()
		pk, msg := packArg(avail, sel, args[1], false)
		if msg != "" {
			p.tell(msg)
			return
		}
		sel = removeID(sel, pk.id)
		s.ok(p, "Disabling data pack "+pk.chatLink())
		s.reloadDataPacks(sel)
	case "create":
		if !s.hasPermission(p.name, permOwners) || p.permCap > 0 && p.permCap < permOwners {
			p.tell("You don't have permission.")
			return
		}
		if len(args) < 3 {
			p.tell(usage)
			return
		}
		s.createPack(p, unquoteArg(args[1]), unquoteArg(strings.Join(args[2:], " ")))
	default:
		p.tell(usage)
	}
}

// packArg is DataPackCommand.getPack: an available pack, enabled already
// when enabling is false and not yet when it is true, and one whose
// features this world has.
func packArg(avail []*dataPack, sel []string, arg string, enabling bool) (*dataPack, string) {
	id := unquoteArg(arg)
	pk := findPack(avail, id)
	if pk == nil {
		return nil, fmt.Sprintf("Unknown data pack '%s'", id)
	}
	enabled := hasPackID(sel, id)
	if enabling && enabled {
		return nil, fmt.Sprintf("Pack '%s' is already enabled!", id)
	}
	if !enabling && !enabled {
		return nil, fmt.Sprintf("Pack '%s' is not enabled!", id)
	}
	if miss := pk.missingFeatures(); len(miss) > 0 {
		return nil, fmt.Sprintf("Pack '%s' cannot be enabled, since required flags are not enabled in this world: %s!", id, strings.Join(miss, ", "))
	}
	return pk, ""
}

// packListEntry is a pack's link in a list, with its incompatibility (what
// the link's hover shows in vanilla) beside it.
func packListEntry(pk *dataPack) string {
	if n := pk.compat.note(); n != "" {
		return pk.chatLink() + " " + n
	}
	return pk.chatLink()
}

func (s *Server) listEnabledPacks(p *player, sel []string, avail []*dataPack) {
	packs := selectedPacks(avail, sel)
	if len(packs) == 0 {
		s.info(p, "There are no data packs enabled")
		return
	}
	links := make([]string, len(packs))
	for i, pk := range packs {
		links[i] = packListEntry(pk)
	}
	s.info(p, fmt.Sprintf("There are %d data pack(s) enabled: %s", len(packs), strings.Join(links, ", ")))
	// What the engine does not load, said once per pack that carries it.
	lib := s.hub.functions.Load()
	if lib == nil {
		return
	}
	for _, pk := range packs {
		if kinds := lib.unapplied[pk.id]; len(kinds) > 0 {
			s.info(p, fmt.Sprintf("%s also carries %s: this server loads only a pack's functions and function tags, so that data is not applied", pk.chatLink(), strings.Join(kinds, ", ")))
		}
	}
}

func (s *Server) listAvailablePacks(p *player, sel []string, avail []*dataPack) {
	var links []string
	for _, pk := range avail {
		if !hasPackID(sel, pk.id) && pk.featuresOK() {
			links = append(links, packListEntry(pk))
		}
	}
	if len(links) == 0 {
		s.info(p, "There are no more data packs available")
		return
	}
	s.info(p, fmt.Sprintf("There are %d data pack(s) available: %s", len(links), strings.Join(links, ", ")))
}

// createPack is /datapack create: an empty pack folder with a pack.mcmeta
// declaring this version's format, not enabled.
func (s *Server) createPack(p *player, id, desc string) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\:*?\"<>|") {
		p.tell(fmt.Sprintf("Invalid characters in new pack name '%s'", id))
		return
	}
	for _, r := range id {
		if r < ' ' {
			p.tell(fmt.Sprintf("Invalid characters in new pack name '%s'", id))
			return
		}
	}
	if s.DataPackDir == "" {
		p.tell(fmt.Sprintf("Can't create pack with name '%s'. Check logs", id))
		return
	}
	dir := filepath.Join(s.DataPackDir, id)
	if _, err := os.Stat(dir); err == nil {
		p.tell(fmt.Sprintf("Pack with name '%s' already exists", id))
		return
	}
	meta := map[string]any{"pack": map[string]any{"description": desc, "min_format": dataPackMajor, "max_format": dataPackMajor}}
	raw, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		log.Printf("datapacks: failed to create pack at %s: %v", dir, err)
		p.tell(fmt.Sprintf("Can't create pack with name '%s'. Check logs", id))
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.mcmeta"), raw, 0o644); err != nil {
		log.Printf("datapacks: failed to create pack at %s: %v", dir, err)
		p.tell(fmt.Sprintf("Can't create pack with name '%s'. Check logs", id))
		return
	}
	s.ok(p, fmt.Sprintf("Created new empty pack with name '%s'", id))
}
