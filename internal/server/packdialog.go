package server

import (
	"bytes"
	"encoding/json"
	"log"
	"sort"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Data-pack dialogs (data/<ns>/dialog/<id>.json): entries a pack adds to,
// or replaces in, the minecraft:dialog registry, and the dialog tags
// (minecraft:pause_screen_additions, minecraft:quick_actions …) that put
// them in the client's pause menu and quick-actions key. The registry is a
// synced one: the client is given the entries in its configuration phase
// (ConfigData.Registries) and the tags with every other tag. A reload whose
// entries differ from the ones the players were configured with sends each
// of them back through configuration (reconfigure.go), the only way a
// client takes new registry entries.

// buildPackDialogs reads a load's dialog files: each checked against the
// dialog codec's shape, its references allowed to name the load's own
// dialogs and dialog tags. A file that does not decode is left out.
func buildPackDialogs(files map[string]packFile, tagFiles map[string][]packFile) map[string]json.RawMessage {
	if len(files) == 0 {
		return nil
	}
	scope := &dialogScope{ids: map[string]bool{}, tags: map[string]bool{}}
	for id := range files {
		scope.ids[id] = true
	}
	for id := range tagFiles {
		scope.tags[id] = true
	}
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := map[string]json.RawMessage{}
	for _, id := range ids {
		f := files[id]
		var m map[string]any
		if err := json.Unmarshal(f.data, &m); err != nil {
			log.Printf("datapacks: couldn't parse dialog %s from %s: %v", id, f.pack, err)
			continue
		}
		if why := scope.check(m); why != "" {
			log.Printf("datapacks: couldn't load dialog %s from %s: %s", id, f.pack, why)
			continue
		}
		raw, err := json.Marshal(m) // compact, and in a stable form to compare loads by
		if err != nil {
			continue
		}
		out[id] = raw
	}
	return out
}

// dialogIDSet is the ids of a load's dialogs.
func dialogIDSet(d map[string]json.RawMessage) map[string]bool {
	out := make(map[string]bool, len(d))
	for id := range d {
		out[id] = true
	}
	return out
}

// packRegistryEntries is the registry entries a load adds for the client's
// configuration phase: its dialogs, by id.
func packRegistryEntries(pc *packContent) []attachproto.RegistryEntries {
	if pc == nil || len(pc.dialogs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(pc.dialogs))
	for id := range pc.dialogs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	set := attachproto.RegistryEntries{Registry: "minecraft:dialog"}
	for _, id := range ids {
		set.Entries = append(set.Entries, attachproto.RegistryEntry{Name: id, Data: pc.dialogs[id]})
	}
	return []attachproto.RegistryEntries{set}
}

// packRegistriesDiffer is whether two loads give a client different
// registry entries.
func packRegistriesDiffer(a, b *packContent) bool {
	var da, db map[string]json.RawMessage
	if a != nil {
		da = a.dialogs
	}
	if b != nil {
		db = b.dialogs
	}
	if len(da) != len(db) {
		return true
	}
	for id, x := range da {
		y, ok := db[id]
		if !ok || !bytes.Equal(x, y) {
			return true
		}
	}
	return false
}
