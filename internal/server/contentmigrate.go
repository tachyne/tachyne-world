package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// A canonical version bump renumbers the built-in registries — items, blocks,
// block states, entity types, custom stats — and the stores persist those ids
// raw. contentMigrator rewrites every one of them from the version a save was
// written in to the current canonical one, using the translation tables: the
// old version is just a client version, and UnmapID takes its ids to ours.
//
// It finds the ids by TYPE, not by field name, so a field added later is
// covered without anyone remembering to list it:
//
//   - stackRow and containerRow are defined types; their item columns are
//     known (packStack): col 0 and the four pot sherds.
//   - potSherds is four item ids.
//   - a scalar field names its registry with a `mig` struct tag — "item",
//     "entity" or "state" — and "item0" marks rows of (item, …) arrays.
//
// TestContentMigratorClassifiesEveryInt fails on any integer field in a store
// that is neither tagged nor listed as not-an-id, so a new field has to be
// classified before it can ship.
//
// The data-driven registries — enchantments, banner patterns, trims, mob
// variants, instruments — are numbered by the registry the engine declares,
// not by the content version, so a bump does not move them and nothing here
// touches them. Keep them append-only.

// contentProtocols maps a canonical content version to the protocol whose
// translation tables describe its ids.
var contentProtocols = map[string]int32{
	"1.21.5":  770,
	"1.21.11": 774,
	"26.3":    777,
}

type contentMigrator struct {
	proto   int32
	changed int
	err     error
}

func newContentMigrator(from string) (*contentMigrator, error) {
	p, ok := contentProtocols[from]
	if !ok {
		return nil, fmt.Errorf("content migration: no protocol known for version %q", from)
	}
	return &contentMigrator{proto: p}, nil
}

// remap takes one id from the old version's registry to the canonical one.
// An id the canonical registry has no counterpart for cannot be kept, and
// turning it into something else would be worse, so it fails the migration.
func (m *contentMigrator) remap(reg protocol.IDSpace, id int64) int64 {
	if protocol.IDAdded(reg, m.proto, int32(id)) {
		if m.err == nil {
			m.err = fmt.Errorf("content migration: id %d of registry %d has no canonical counterpart", id, reg)
		}
		return id
	}
	n := int64(protocol.UnmapID(reg, m.proto, int32(id)))
	if n != id {
		m.changed++
	}
	return n
}

var (
	stackRowType     = reflect.TypeOf(stackRow{})
	containerRowType = reflect.TypeOf(containerRow{})
	potSherdsType    = reflect.TypeOf(potSherds{})
)

// stackItemCols are the stackRow columns that hold item ids (packStack).
var stackItemCols = []int{0, 32, 33, 34, 35}

// walk rewrites every id reachable from v, which must be addressable.
func (m *contentMigrator) walk(v reflect.Value) {
	switch v.Type() {
	case stackRowType:
		m.items(v, stackItemCols, 0)
		return
	case containerRowType:
		m.items(v, stackItemCols, 1) // slot index, then the stackRow
		return
	case potSherdsType:
		m.items(v, []int{0, 1, 2, 3}, 0)
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			m.walk(v.Elem())
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if tag, ok := t.Field(i).Tag.Lookup("mig"); ok {
				m.tagged(v.Field(i), tag)
			} else {
				m.walk(v.Field(i))
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			m.walk(v.Index(i))
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			e := reflect.New(it.Value().Type()).Elem()
			e.Set(it.Value())
			m.walk(e)
			v.SetMapIndex(it.Key(), e)
		}
	}
}

// items remaps the item ids at cols (offset by off) of an int array. Item 0
// is the empty stack and stays empty.
func (m *contentMigrator) items(v reflect.Value, cols []int, off int) {
	for _, c := range cols {
		f := v.Index(c + off)
		if f.Int() != 0 {
			f.SetInt(m.remap(protocol.RegItem, f.Int()))
		}
	}
}

func (m *contentMigrator) tagged(v reflect.Value, tag string) {
	switch tag {
	case "item0":
		for i := 0; i < v.Len(); i++ {
			m.items(v.Index(i), []int{0}, 0)
		}
		return
	}
	var reg protocol.IDSpace
	switch tag {
	case "item":
		if v.Int() != 0 {
			v.SetInt(m.remap(protocol.RegItem, v.Int()))
		}
		return
	case "entity":
		reg = protocol.RegEntity
	case "state":
		reg = protocol.RegBlockState
	default:
		panic("content migration: unknown mig tag " + strconv.Quote(tag))
	}
	if v.CanUint() {
		if v.Uint() != 0 || reg != protocol.RegBlockState {
			v.SetUint(uint64(m.remap(reg, int64(v.Uint()))))
		}
		return
	}
	v.SetInt(m.remap(reg, v.Int()))
}

// statKeyRegistry is the registry a stats.json key's K indexes, by its stat
// type T (see stats.go): mined counts blocks, killed counts entity types.
func statKeyRegistry(t int) (protocol.IDSpace, bool) {
	switch {
	case t == 0:
		return protocol.RegBlock, true
	case t >= 1 && t <= 5:
		return protocol.RegItem, true
	case t == 6 || t == 7:
		return protocol.RegEntity, true
	case t == 8:
		return protocol.RegCustomStat, true
	}
	return 0, false
}

// stats rewrites the ids inside stats.json's "T:K" keys.
func (m *contentMigrator) stats(all map[string]map[string]int32) {
	for name, st := range all {
		out := make(map[string]int32, len(st))
		for k, v := range st {
			t, key, ok := strings.Cut(k, ":")
			ti, e1 := strconv.Atoi(t)
			ki, e2 := strconv.Atoi(key)
			reg, known := statKeyRegistry(ti)
			if !ok || e1 != nil || e2 != nil || !known {
				out[k] = v // not an id key: carried as is
				continue
			}
			nk := fmt.Sprintf("%d:%d", ti, m.remap(reg, int64(ki)))
			out[nk] += v
		}
		all[name] = out
	}
}

// contentFiles names the stores that persist built-in registry ids.
type contentFiles struct {
	Inventories, Containers, Mobs, Stats string
}

// migrateContent rewrites every store in files from version from to the
// current canonical ids. It is all-or-nothing: every store is decoded and
// remapped first, and only when all of them decoded and every id found a
// counterpart is anything written — each after a copy of the original is
// kept beside it. It decodes strictly rather than through the stores'
// loaders, which quarantine an unreadable file and carry on empty: here that
// would write an empty store and mark it migrated.
func migrateContent(files contentFiles, from, to string) (int, error) {
	m, err := newContentMigrator(from)
	if err != nil {
		return 0, err
	}
	type store struct {
		path string
		v    any
	}
	var (
		invs   map[string]*savedInv
		cont   containerFile
		mobs   mobFile
		stats  map[string]map[string]int32
		stores []store
	)
	for _, st := range []store{
		{files.Inventories, &invs}, {files.Containers, &cont},
		{files.Mobs, &mobs}, {files.Stats, &stats},
	} {
		if st.path == "" {
			continue
		}
		data, err := os.ReadFile(st.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("content migration: %w", err)
		}
		if err := json.Unmarshal(data, st.v); err != nil {
			return 0, fmt.Errorf("content migration: %s: %w", st.path, err)
		}
		if st.v == any(&stats) {
			m.stats(stats)
		} else {
			m.walk(reflect.ValueOf(st.v).Elem())
		}
		stores = append(stores, st)
	}
	if m.err != nil {
		return 0, m.err
	}
	for _, st := range stores {
		if err := copyFile(st.path, st.path+".pre-"+to); err != nil {
			return 0, fmt.Errorf("content migration: backing up %s: %w", st.path, err)
		}
	}
	for _, st := range stores {
		data, err := json.MarshalIndent(st.v, "", "  ")
		if err != nil {
			return 0, fmt.Errorf("content migration: %s: %w", st.path, err)
		}
		if err := writeAtomic(st.path, data); err != nil {
			return 0, fmt.Errorf("content migration: %s: %w", st.path, err)
		}
	}
	return m.changed, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeAtomic(dst, data)
}

// logContentMigration is the one line a migration leaves in the log.
func logContentMigration(n int, from, to string) {
	log.Printf("content id migration: remapped %d ids %s→%s", n, from, to)
}
