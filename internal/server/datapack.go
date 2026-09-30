package server

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Data packs, as vanilla's PackRepository keeps them: the built-in
// "vanilla" pack plus every folder or .zip with a pack.mcmeta in the
// world's datapacks/ directory (FolderRepositorySource, ids "file/<name>").
// The selection is an ordered list, bottom first — a later pack's file wins
// over an earlier one's — and it is persisted in settings.json the way
// level.dat keeps DataPacks{Enabled, Disabled}.
//
// Of a pack's data the engine loads what it can use: functions
// (data/<ns>/function/**.mcfunction) and function tags
// (data/<ns>/tags/function/**.json). Every other registry a pack carries —
// recipes, loot tables, advancements, worldgen — is listed but not applied;
// /datapack list says so.

const (
	// The 26.3 data pack format (version.json pack_version.data_major/minor).
	dataPackMajor = 121
	dataPackMinor = 0
	// lastPreMinorDataFormat is PackFormat.lastPreMinorVersion(SERVER_DATA):
	// the last format declared without a minor version.
	lastPreMinorDataFormat = 81

	vanillaPackID = "vanilla"
	// maxFormatMinor stands in for Integer.MAX_VALUE in a format's minor.
	maxFormatMinor = 1<<31 - 1
)

// worldFeatures are the feature flags this world has enabled: vanilla's
// own, none of the experiments.
var worldFeatures = map[string]bool{"minecraft:vanilla": true}

// packFormat is PackFormat: a major and a minor version.
type packFormat struct{ major, minor int }

func (a packFormat) cmp(b packFormat) int {
	switch {
	case a.major != b.major:
		return a.major - b.major
	case a.minor < b.minor:
		return -1
	case a.minor > b.minor:
		return 1
	}
	return 0
}

func (a packFormat) String() string {
	if a.minor == maxFormatMinor {
		return fmt.Sprintf("%d.*", a.major)
	}
	return fmt.Sprintf("%d.%d", a.major, a.minor)
}

// packCompat is PackCompatibility.
type packCompat int

const (
	packCompatible packCompat = iota
	packTooOld
	packTooNew
	packUnknown
)

// note is the pack.incompatible.* line the pack list shows beside an
// incompatible pack ("" when it is compatible).
func (c packCompat) note() string {
	switch c {
	case packTooOld:
		return "(Made for an older version of Minecraft)"
	case packTooNew:
		return "(Made for a newer version of Minecraft)"
	case packUnknown:
		return "(Broken or incompatible)"
	}
	return ""
}

// dataPack is one pack the repository knows.
type dataPack struct {
	id       string // "vanilla" or "file/<name>"
	title    string
	desc     string
	builtin  bool
	path     string // the folder or the .zip
	zip      bool
	compat   packCompat
	features []string // pack.mcmeta features.enabled
}

// featuresOK reports whether every feature the pack requests is enabled.
func (p *dataPack) featuresOK() bool { return len(p.missingFeatures()) == 0 }

func (p *dataPack) missingFeatures() []string {
	var out []string
	for _, f := range p.features {
		if !worldFeatures[nsID(f)] {
			out = append(out, nsID(f))
		}
	}
	return out
}

// chatLink is Pack.getChatLink as plain text: the id decorated with its
// source, in square brackets — [vanilla (built-in)], [file/x (world)].
func (p *dataPack) chatLink() string {
	src := "world"
	if p.builtin {
		src = "built-in"
	}
	return "[" + p.id + " (" + src + ")]"
}

func vanillaPack() *dataPack {
	return &dataPack{id: vanillaPackID, title: "Default", desc: "The default data for Minecraft", builtin: true, compat: packCompatible}
}

// discoverPacks is PackRepository.reload: the built-in pack and every pack
// in dir, sorted by id as the repository's TreeMap keeps them. An entry
// without a readable pack.mcmeta is not a pack.
func discoverPacks(dir string) []*dataPack {
	out := []*dataPack{vanillaPack()}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("datapacks: failed to create %s: %v", dir, err)
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("datapacks: failed to list packs in %s: %v", dir, err)
		}
		for _, e := range ents {
			if p := readPackEntry(dir, e.Name()); p != nil {
				out = append(out, p)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// readPackEntry reads one datapacks/ entry: a folder holding pack.mcmeta or
// a .zip file (PackDetector); nil when it is neither or its metadata is
// unreadable.
func readPackEntry(dir, name string) *dataPack {
	full := filepath.Join(dir, name)
	st, err := os.Stat(full)
	if err != nil {
		return nil
	}
	p := &dataPack{id: "file/" + name, title: name, path: full}
	var raw []byte
	switch {
	case st.IsDir():
		if raw, err = os.ReadFile(filepath.Join(full, "pack.mcmeta")); err != nil {
			return nil
		}
	case st.Mode().IsRegular() && strings.HasSuffix(name, ".zip"):
		p.zip = true
		zr, zerr := zip.OpenReader(full)
		if zerr != nil {
			log.Printf("datapacks: can't open pack archive %s: %v", full, zerr)
			return nil
		}
		raw, err = fs.ReadFile(zr, "pack.mcmeta")
		zr.Close()
		if err != nil {
			log.Printf("datapacks: missing metadata in pack %s", p.id)
			return nil
		}
	default:
		return nil
	}
	desc, compat, features, ok := readPackMeta(raw)
	if !ok {
		log.Printf("datapacks: missing or unreadable metadata in pack %s", p.id)
		return nil
	}
	p.desc, p.compat, p.features = desc, compat, features
	if compat != packCompatible {
		log.Printf("datapacks: pack %s is incompatible with this version %s", p.id, compat.note())
	}
	return p
}

// readPackMeta reads pack.mcmeta: the description, the compatibility its
// declared formats give against this version, and the features it
// requests. ok is false when there is no "pack" section with a
// description — Pack.readPackMetadata's null. A pack whose format fields
// do not validate falls back to the description alone and is UNKNOWN, as
// PackMetadataSection.FALLBACK_TYPE makes it.
func readPackMeta(raw []byte) (desc string, compat packCompat, features []string, ok bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return "", 0, nil, false
	}
	var pack map[string]json.RawMessage
	if json.Unmarshal(top["pack"], &pack) != nil || pack == nil {
		return "", 0, nil, false
	}
	d, has := pack["description"]
	if !has {
		return "", 0, nil, false
	}
	var dv any
	if json.Unmarshal(d, &dv) != nil {
		return "", 0, nil, false
	}
	desc = componentText(dv)
	if f, has := top["features"]; has {
		var fm struct {
			Enabled []string `json:"enabled"`
		}
		if json.Unmarshal(f, &fm) == nil {
			features = fm.Enabled
		}
	}
	lo, hi, err := packRange(pack)
	if err != nil {
		return desc, packUnknown, features, true
	}
	return desc, compatFor(lo, hi), features, true
}

// compatFor is PackCompatibility.forVersion against this version's format.
func compatFor(lo, hi packFormat) packCompat {
	game := packFormat{dataPackMajor, dataPackMinor}
	switch {
	case lo.major == maxFormatMinor:
		return packUnknown
	case hi.cmp(game) < 0:
		return packTooOld
	case game.cmp(lo) < 0:
		return packTooNew
	}
	return packCompatible
}

// componentText flattens a text component (a string, an object or a list)
// to its plain text.
func componentText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, e := range c {
			b.WriteString(componentText(e))
		}
		return b.String()
	case map[string]any:
		s, _ := c["text"].(string)
		if s == "" {
			s, _ = c["translate"].(string)
		}
		if extra, ok := c["extra"].([]any); ok {
			s += componentText(extra)
		}
		return s
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// packRange is PackFormat.packCodec(SERVER_DATA): the range of formats the
// pack declares, from min_format/max_format, the older supported_formats
// or the single pack_format — IntermediaryFormat.validate with the pack
// codec's settings (a pack_format field, no old field required).
func packRange(pack map[string]json.RawMessage) (lo, hi packFormat, err error) {
	var minF, maxF *packFormat
	if raw, ok := pack["min_format"]; ok {
		f, ok := formatValue(raw, 0)
		if !ok {
			return lo, hi, errors.New("bad min_format")
		}
		minF = &f
	}
	if raw, ok := pack["max_format"]; ok {
		f, ok := formatValue(raw, maxFormatMinor)
		if !ok {
			return lo, hi, errors.New("bad max_format")
		}
		maxF = &f
	}
	var format *int
	if raw, ok := pack["pack_format"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			return lo, hi, errors.New("bad pack_format")
		}
		format = &n
	}
	var sup *[2]int
	if raw, ok := pack["supported_formats"]; ok {
		a, b, ok := intRange(raw)
		if !ok {
			return lo, hi, errors.New("bad supported_formats")
		}
		sup = &[2]int{a, b}
	}
	const last = lastPreMinorDataFormat
	checkFormat := func(min, max int) error {
		if format == nil {
			return nil
		}
		if *format < min || *format > max {
			return fmt.Errorf("pack declared support for versions %d to %d but declared main format is %d", min, max, *format)
		}
		if *format < 15 {
			return errors.New("multi-version packs cannot support minimum version of less than 15")
		}
		return nil
	}
	switch {
	case (minF == nil) != (maxF == nil):
		return lo, hi, errors.New("pack missing field, must declare both min_format and max_format")
	case minF != nil:
		if minF.cmp(*maxF) > 0 {
			return lo, hi, errors.New("min_format is greater than max_format")
		}
		if minF.major > last {
			if sup != nil {
				return lo, hi, errors.New("supported_formats is deprecated")
			}
		} else {
			if sup == nil || sup[0] != minF.major || (sup[1] != maxF.major && sup[1] != last) || format == nil {
				return lo, hi, errors.New("version declaration mismatch")
			}
		}
		if err := checkFormat(minF.major, maxF.major); err != nil {
			return lo, hi, err
		}
		return *minF, *maxF, nil
	case sup != nil:
		if sup[1] > last {
			return lo, hi, errors.New("missing mandatory fields min_format and max_format")
		}
		if format == nil {
			return lo, hi, errors.New("missing pack_format")
		}
		if err := checkFormat(sup[0], sup[1]); err != nil {
			return lo, hi, err
		}
		return packFormat{sup[0], 0}, packFormat{sup[1], 0}, nil
	case format != nil:
		if *format > last {
			return lo, hi, errors.New("missing mandatory fields min_format and max_format")
		}
		return packFormat{*format, 0}, packFormat{*format, 0}, nil
	}
	return lo, hi, errors.New("missing format version information")
}

// formatValue reads a min_format/max_format value: a number, or a list of
// one or two non-negative numbers (major, minor); a missing minor is
// defaultMinor.
func formatValue(raw json.RawMessage, defaultMinor int) (packFormat, bool) {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return packFormat{n, defaultMinor}, n >= 0
	}
	var l []int
	if json.Unmarshal(raw, &l) != nil || len(l) < 1 || len(l) > 256 {
		return packFormat{}, false
	}
	for _, v := range l {
		if v < 0 {
			return packFormat{}, false
		}
	}
	if len(l) > 1 {
		return packFormat{l[0], l[1]}, true
	}
	return packFormat{l[0], defaultMinor}, true
}

// intRange reads InclusiveRange.codec(INT): a number, a [min, max] pair or
// {min_inclusive, max_inclusive}; min may not exceed max.
func intRange(raw json.RawMessage) (int, int, bool) {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n, n, true
	}
	var l []int
	if json.Unmarshal(raw, &l) == nil {
		if len(l) != 2 || l[0] > l[1] {
			return 0, 0, false
		}
		return l[0], l[1], true
	}
	var o struct {
		Min *int `json:"min_inclusive"`
		Max *int `json:"max_inclusive"`
	}
	if json.Unmarshal(raw, &o) != nil || o.Min == nil || o.Max == nil || *o.Min > *o.Max {
		return 0, 0, false
	}
	return *o.Min, *o.Max, true
}

// dataPackConfig is DataPackConfig as level.dat keeps it.
type dataPackConfig struct {
	Enabled  []string `json:"Enabled"`
	Disabled []string `json:"Disabled"`
}

// findPack is PackRepository.getPack.
func findPack(avail []*dataPack, id string) *dataPack {
	for _, p := range avail {
		if p.id == id {
			return p
		}
	}
	return nil
}

func hasPackID(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}

// configurePacks is MinecraftServer.configurePackRepository at boot: the
// saved enabled list (missing packs dropped), then every available pack
// not on the disabled list and not yet selected, and vanilla alone when
// nothing is left.
func configurePacks(avail []*dataPack, cfg *dataPackConfig) []string {
	var selected []string
	for _, id := range cfg.Enabled {
		switch {
		case hasPackID(selected, id):
		case findPack(avail, id) == nil:
			log.Printf("datapacks: missing data pack %s", id)
		default:
			selected = append(selected, id)
		}
	}
	for _, p := range avail {
		if hasPackID(cfg.Disabled, p.id) {
			continue
		}
		isSelected := hasPackID(selected, p.id)
		if !isSelected && p.featuresOK() {
			log.Printf("datapacks: found new data pack %s, loading it automatically", p.id)
			selected = append(selected, p.id)
		}
		if isSelected && !p.featuresOK() {
			log.Printf("datapacks: pack %s requires features %v that are not enabled for this world, disabling pack", p.id, p.missingFeatures())
			selected = removeID(selected, p.id)
		}
	}
	if len(selected) == 0 {
		selected = []string{vanillaPackID}
	}
	return selected
}

func removeID(ids []string, id string) []string {
	out := ids[:0:0]
	for _, s := range ids {
		if s != id {
			out = append(out, s)
		}
	}
	return out
}

// packConfigFor is MinecraftServer.getSelectedPacks(…, true): the selection
// as enabled, every other available pack as disabled.
func packConfigFor(avail []*dataPack, selected []string) *dataPackConfig {
	cfg := &dataPackConfig{Enabled: append([]string{}, selected...), Disabled: []string{}}
	for _, p := range avail {
		if !hasPackID(selected, p.id) {
			cfg.Disabled = append(cfg.Disabled, p.id)
		}
	}
	return cfg
}

// selectedPacks maps selected ids to the available packs, in order.
func selectedPacks(avail []*dataPack, selected []string) []*dataPack {
	var out []*dataPack
	for _, id := range selected {
		if p := findPack(avail, id); p != nil {
			out = append(out, p)
		}
	}
	return out
}

// openPack opens a pack's files (nil for the built-in pack, whose data is
// the engine's own).
func openPack(p *dataPack) (fs.FS, func(), error) {
	if p.builtin {
		return nil, func() {}, nil
	}
	if p.zip {
		zr, err := zip.OpenReader(p.path)
		if err != nil {
			return nil, func() {}, err
		}
		return zr, func() { zr.Close() }, nil
	}
	return os.DirFS(p.path), func() {}, nil
}

// validNamespace / validIDPath are Identifier's character rules.
func validNamespace(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func validIDPath(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == '/') {
			return false
		}
	}
	return true
}

// parseResID reads an identifier argument: ns:path, or a bare path in the
// minecraft namespace. ok is false for characters an Identifier refuses.
func parseResID(s string) (string, bool) {
	ns, p := "minecraft", s
	if i := strings.IndexByte(s, ':'); i >= 0 {
		ns, p = s[:i], s[i+1:]
		if ns == "" {
			ns = "minecraft"
		}
	}
	if !validNamespace(ns) || !validIDPath(p) {
		return "", false
	}
	return ns + ":" + p, true
}

// packFile is one file of a selected pack's data.
type packFile struct {
	pack string // the pack's id
	data []byte
}

// functionLibrary is what a load produced: the selection and its
// functions and function tags (ServerFunctionLibrary), plus the data the
// packs carry that this server does not apply.
type functionLibrary struct {
	packs     []*dataPack // the selection, bottom first
	available []*dataPack // the repository as this load saw it
	config    *dataPackConfig
	functions map[string]*mcFunction
	tags      map[string][]*mcFunction
	unapplied map[string][]string // pack id → the data kinds it carries that are not loaded
}

const (
	tagLoad = "minecraft:load"
	tagTick = "minecraft:tick"
)

// tag is ServerFunctionLibrary.getTag: the tag's functions, nil when there
// is no such tag.
func (l *functionLibrary) tag(id string) []*mcFunction {
	if l == nil {
		return nil
	}
	return l.tags[id]
}

func (l *functionLibrary) hasTag(id string) bool {
	if l == nil {
		return false
	}
	_, ok := l.tags[id]
	return ok
}

func (l *functionLibrary) function(id string) *mcFunction {
	if l == nil {
		return nil
	}
	return l.functions[id]
}

// selectedIDs is the selection's ids, bottom first.
func (l *functionLibrary) selectedIDs() []string {
	if l == nil {
		return []string{vanillaPackID}
	}
	out := make([]string, 0, len(l.packs))
	for _, p := range l.packs {
		out = append(out, p.id)
	}
	return out
}

// buildLibrary loads the selected packs' functions and function tags. A
// function comes from the top-most pack that has its file; a tag merges
// every pack's file for it bottom to top, a "replace": true file dropping
// what came before.
func buildLibrary(packs []*dataPack) *functionLibrary {
	lib := &functionLibrary{packs: packs, functions: map[string]*mcFunction{}, tags: map[string][]*mcFunction{}, unapplied: map[string][]string{}}
	fnFiles := map[string]packFile{}
	tagFiles := map[string][]packFile{}
	for _, p := range packs {
		fsys, closeFn, err := openPack(p)
		if err != nil {
			log.Printf("datapacks: failed to open %s: %v", p.id, err)
			continue
		}
		if fsys == nil {
			closeFn()
			continue
		}
		kinds := map[string]bool{}
		if _, err := fs.Stat(fsys, "data"); err == nil {
			_ = fs.WalkDir(fsys, "data", func(name string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				parts := strings.SplitN(name, "/", 3) // data, ns, rest
				if len(parts) < 3 || !strings.Contains(parts[2], "/") {
					return nil
				}
				ns, rest := parts[1], parts[2]
				switch {
				case strings.HasPrefix(rest, "function/") && strings.HasSuffix(rest, ".mcfunction"):
					id := strings.TrimSuffix(strings.TrimPrefix(rest, "function/"), ".mcfunction")
					if !validNamespace(ns) || !validIDPath(id) {
						log.Printf("datapacks: invalid function path %s in %s", name, p.id)
						return nil
					}
					if data, err := fs.ReadFile(fsys, name); err == nil {
						fnFiles[ns+":"+id] = packFile{pack: p.id, data: data}
					}
				case strings.HasPrefix(rest, "tags/function/") && strings.HasSuffix(rest, ".json"):
					id := strings.TrimSuffix(strings.TrimPrefix(rest, "tags/function/"), ".json")
					if !validNamespace(ns) || !validIDPath(id) {
						log.Printf("datapacks: invalid function tag path %s in %s", name, p.id)
						return nil
					}
					if data, err := fs.ReadFile(fsys, name); err == nil {
						tagFiles[ns+":"+id] = append(tagFiles[ns+":"+id], packFile{pack: p.id, data: data})
					}
				default:
					kinds[dataKind(rest)] = true
				}
				return nil
			})
		}
		closeFn()
		if len(kinds) > 0 {
			list := make([]string, 0, len(kinds))
			for k := range kinds {
				list = append(list, k)
			}
			sort.Strings(list)
			lib.unapplied[p.id] = list
		}
	}
	for id, f := range fnFiles {
		fn, err := compileFunction(id, string(f.data))
		if err != nil {
			log.Printf("datapacks: failed to load function %s (from %s): %v", id, f.pack, err)
			continue
		}
		lib.functions[id] = fn
	}
	lib.tags = buildFunctionTags(tagFiles, lib.functions)
	return lib
}

// dataKind names the registry a data file belongs to: its folder, with
// tags/ and worldgen/ kept to their second level.
func dataKind(rest string) string {
	parts := strings.Split(path.Dir(rest), "/")
	if (parts[0] == "tags" || parts[0] == "worldgen") && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// tagRef is one TagEntry: a function or a #tag, required or optional.
type tagRef struct {
	id       string
	tag      bool
	required bool
}

func (r tagRef) String() string {
	s := r.id
	if r.tag {
		s = "#" + s
	}
	if !r.required {
		s += "?"
	}
	return s
}

// parseTagFile reads a TagFile: {"values": [...], "replace": bool}; each
// value an id, a #tag, or {"id": …, "required": bool}.
func parseTagFile(data []byte) (refs []tagRef, replace bool, err error) {
	var tf struct {
		Values  []json.RawMessage `json:"values"`
		Replace bool              `json:"replace"`
	}
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, false, err
	}
	if tf.Values == nil {
		return nil, false, errors.New("no key values")
	}
	for _, raw := range tf.Values {
		ref := tagRef{required: true}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			var o struct {
				ID       string `json:"id"`
				Required *bool  `json:"required"`
			}
			if err := json.Unmarshal(raw, &o); err != nil || o.ID == "" {
				return nil, false, fmt.Errorf("bad tag entry %s", raw)
			}
			s = o.ID
			if o.Required != nil {
				ref.required = *o.Required
			}
		}
		if strings.HasPrefix(s, "#") {
			ref.tag, s = true, s[1:]
		}
		id, ok := parseResID(s)
		if !ok {
			return nil, false, fmt.Errorf("not a valid id: %s", s)
		}
		ref.id = id
		refs = append(refs, ref)
	}
	return refs, tf.Replace, nil
}

// buildFunctionTags is TagLoader.load + build for function tags: the files
// merge per tag, then each tag resolves after the tags it names. A tag
// missing a required function or tag is not loaded at all; optional ones
// that are missing are skipped. Functions appear once, in first order.
func buildFunctionTags(files map[string][]packFile, fns map[string]*mcFunction) map[string][]*mcFunction {
	entries := map[string][]tagRef{}
	for id, list := range files {
		var refs []tagRef
		for _, f := range list {
			r, replace, err := parseTagFile(f.data)
			if err != nil {
				log.Printf("datapacks: couldn't read tag list %s in data pack %s: %v", id, f.pack, err)
				continue
			}
			if replace {
				refs = nil
			}
			refs = append(refs, r...)
		}
		entries[id] = refs
	}
	built := map[string][]*mcFunction{}
	failed := map[string]bool{}
	visiting := map[string]bool{}
	var build func(id string) bool
	build = func(id string) bool {
		if _, ok := built[id]; ok {
			return true
		}
		if failed[id] || visiting[id] {
			return false
		}
		refs, ok := entries[id]
		if !ok {
			return false
		}
		visiting[id] = true
		defer delete(visiting, id)
		var out []*mcFunction
		seen := map[*mcFunction]bool{}
		add := func(f *mcFunction) {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
		var missing []string
		for _, r := range refs {
			if r.tag {
				if build(r.id) {
					for _, f := range built[r.id] {
						add(f)
					}
				} else if r.required {
					missing = append(missing, r.String())
				}
				continue
			}
			if f := fns[r.id]; f != nil {
				add(f)
			} else if r.required {
				missing = append(missing, r.String())
			}
		}
		if len(missing) > 0 {
			log.Printf("datapacks: couldn't load tag %s as it is missing following references: %s", id, strings.Join(missing, ", "))
			failed[id] = true
			return false
		}
		if out == nil {
			out = []*mcFunction{}
		}
		built[id] = out
		return true
	}
	for id := range entries {
		build(id)
	}
	return built
}
