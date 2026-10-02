package world

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Dimensions are a table, not three hard-wired worlds: each entry is vanilla's
// LevelStem (an id, a registry key, a generator) with the DimensionType facts
// the engine reads. Adding a dimension is one entry here plus its generator;
// everything that runs "every dimension" or looks a world up by id walks this
// table.

// DimensionType describes one dimension the engine can run.
type DimensionType struct {
	ID   int    // the index every dim-carrying field (and the attach protocol) uses
	Key  string // registry key, as commands and GlobalPos carry it
	Name string // short name: logs, anvil-export's -dims, map tile paths
	// File is the edit store, beside the overworld's; its stem also names the
	// dimension's build-guard snapshot (guardsnap.go). The overworld's is only
	// the default: the server takes that path from its -world flag.
	File     string
	AnvilDir string // the vanilla save folder the dimension exports to ("" = the save root)
	// Open builds the dimension's world for a seed over an optional store.
	Open func(seed int64, store Store) (*World, error)

	// Type is the dimension's dimension_type entry ("" = its own Key, which
	// is what the three built-in dimensions use). TypeData is that type's
	// element in the data-pack JSON form when it is not one of the built-in
	// types (nil = the client's own pack has it); the gateways send it
	// inline in the dimension_type registry.
	Type     string
	TypeData []byte
	// Clock is the world_clock the dimension's time runs on
	// (DimensionType.defaultClock; "" = none, as in the Nether).
	Clock string

	CoordinateScale    float64 // horizontal scale against the overworld (portals, the travel landing)
	MinY               int     // the logical floor natural spawning draws from
	HasSkyLight        bool
	HasCeiling         bool
	BedWorks           bool // a bed sets a spawn here; anywhere else it detonates
	RespawnAnchorWorks bool
	WaterEvaporates    bool // ultrawarm: placed water, melted ice and wet sponges dry away
	FastLava           bool // lava flows and pushes at the Nether's pace
	PiglinsZombify     bool // piglins and hoglins turn to zombified kin over time
	CanStartRaid       bool // gameplay/can_start_raid: a Raid Omen may start a raid here
}

// HasWeather is Level.canHaveWeather: rain, snow and lightning happen only
// under an open sky, and never in the End.
func (d *DimensionType) HasWeather() bool {
	return d.HasSkyLight && !d.HasCeiling && d.ID != DimEnd
}

// The dimension ids. They are the table's indices and never renumber: saves
// and the attach protocol carry them.
const (
	DimOverworld = 0
	DimNether    = 1
	DimEnd       = 2
)

// Dimensions is every dimension the engine runs, indexed by ID.
var Dimensions = []DimensionType{
	{
		ID: DimOverworld, Key: "minecraft:overworld", Name: "overworld",
		File: "world.gob", AnvilDir: "", Open: NewWithStore, Clock: "minecraft:overworld",
		CoordinateScale: 1, MinY: worldgen.MinY, HasSkyLight: true,
		BedWorks: true, PiglinsZombify: true, CanStartRaid: true,
	},
	{
		ID: DimNether, Key: "minecraft:the_nether", Name: "nether",
		File: "nether.gob", AnvilDir: "DIM-1", Open: NewNether,
		CoordinateScale: 8, MinY: 0, HasCeiling: true,
		RespawnAnchorWorks: true, WaterEvaporates: true, FastLava: true,
	},
	{
		ID: DimEnd, Key: "minecraft:the_end", Name: "end",
		File: "end.gob", AnvilDir: "DIM1", Open: NewEnd, Clock: "minecraft:the_end",
		CoordinateScale: 1, MinY: 0, PiglinsZombify: true, CanStartRaid: true,
	},
}

// Dimension returns the dimension with id, or nil for an id the engine does
// not run.
func Dimension(id int) *DimensionType {
	if id < 0 || id >= len(Dimensions) {
		return nil
	}
	return &Dimensions[id]
}

// DimensionByKey resolves a registry key, with or without the "minecraft:"
// namespace.
func DimensionByKey(key string) *DimensionType {
	if len(key) < 10 || key[:10] != "minecraft:" {
		key = "minecraft:" + key
	}
	for i := range Dimensions {
		if Dimensions[i].Key == key {
			return &Dimensions[i]
		}
	}
	return nil
}

// TypeKey is the dimension's dimension_type entry.
func (d *DimensionType) TypeKey() string {
	if d.Type != "" {
		return d.Type
	}
	return d.Key
}

// DimensionByName resolves a short name ("overworld", "nether", "end").
func DimensionByName(name string) *DimensionType {
	for i := range Dimensions {
		if Dimensions[i].Name == name {
			return &Dimensions[i]
		}
	}
	return nil
}
