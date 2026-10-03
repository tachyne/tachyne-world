package worldgen

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// The vanilla generator's placement data: 26.3's structure sets and
// structures, every biome's feature lists in the eleven decoration steps,
// the placed features with their placement modifiers, the configured
// features, the tags those name and each dimension's possible biomes in
// biome-source order — baked from the server jar by
// scripts/gen_vanilla_placement.py. vanillastructs.go places structure
// starts from it, vanilladecorate.go the features.

//go:embed vanilladata/placement.json
var vanillaPlacementJSON []byte

// Decoration steps (GenerationStep.Decoration, in order).
const (
	stepRawGeneration = iota
	stepLakes
	stepLocalModifications
	stepUndergroundStructures
	stepSurfaceStructures
	stepStrongholds
	stepUndergroundOres
	stepUndergroundDecoration
	stepFluidSprings
	stepVegetalDecoration
	stepTopLayerModification
	vpStepCount
)

var vpStepNames = [vpStepCount]string{"raw_generation", "lakes", "local_modifications",
	"underground_structures", "surface_structures", "strongholds", "underground_ores",
	"underground_decoration", "fluid_springs", "vegetal_decoration", "top_layer_modification"}

// vpSetEntry is one structure of a structure set and its weight.
type vpSetEntry struct {
	Name   string
	Weight int
}

// vpSet is a structure_set: its placement and its structures.
type vpSet struct {
	Name string
	// Type is "random_spread" or "concentric_rings".
	Type       string
	Salt       int32
	Frequency  float32 // 1 = no reduction
	Reduction  string  // frequency_reduction_method
	Exclusion  string  // exclusion_zone.other_set ("" = none)
	ExclChunks int32
	Locate     [3]int // locate_offset
	// random_spread
	Spacing, Separation int32
	Triangular          bool
	// concentric_rings
	Distance, Spread, Count int
	Preferred               map[string]bool
	Structures              []vpSetEntry
}

// vpStructure is a structure: its type, the biomes it may start in, its
// decoration step and the fields its start position needs.
type vpStructure struct {
	Name   string
	Type   string
	Biomes map[string]bool
	Step   int
	raw    map[string]json.RawMessage
}

// vpPlacedFeature is a placed feature: the configured feature it places
// (a name, or "" with Inline set) and its placement modifiers.
type vpPlacedFeature struct {
	Name      string
	Feature   string
	Inline    json.RawMessage
	Modifiers []vpModifier
}

// vpData is the parsed placement data.
type vpData struct {
	Sets       map[string]*vpSet
	SetNames   []string // sorted: the registry's order
	Structures map[string]*vpStructure
	// StructNames is the structure registry's order (sorted by id, as the
	// server loads it from its data pack), which numbers structures within
	// a decoration step.
	StructNames []string
	Biomes      map[string][][]string // biome → step → placed feature names
	Placed      map[string]*vpPlacedFeature
	Features    map[string]json.RawMessage
	BlockTags   map[string][]string
	FluidTags   map[string][]string
	BiomeTags   map[string][]string
	Noises      map[string]vnNoiseParams
	Possible    map[string][]string // dimension → possible biomes, source order
	// StartPools are the jigsaw structures' start pools: each element's
	// weight, template size and start-jigsaw anchors.
	StartPools map[string][]vpPoolElem
	// TemplateSizes are the sizes of templates code picks between before
	// it places them (the ruined portals).
	TemplateSizes map[string][3]int
	// StateProviders is the block_state_provider registry.
	StateProviders map[string]json.RawMessage
}

// vpPoolElem is one element of a jigsaw start pool.
type vpPoolElem struct {
	Type     string   `json:"type"`
	Weight   int      `json:"weight"`
	Location string   `json:"location"`
	Size     [3]int   `json:"size"`
	Anchors  [][3]int `json:"anchors"`
}

var (
	vpDataOnce sync.Once
	vpDataVal  *vpData
	vpDataErr  error
)

// vanillaPlacementData parses the embedded data once.
func vanillaPlacementData() (*vpData, error) {
	vpDataOnce.Do(func() { vpDataVal, vpDataErr = parseVPData(vanillaPlacementJSON) })
	return vpDataVal, vpDataErr
}

// mustVPData is vanillaPlacementData for callers that cannot go on without
// it (the data is embedded; a failure is a build defect).
func mustVPData() *vpData {
	d, err := vanillaPlacementData()
	if err != nil {
		panic("vanilla placement data: " + err.Error())
	}
	return d
}

func vpShort(name string) string { return strings.TrimPrefix(name, "minecraft:") }

func parseVPData(raw []byte) (*vpData, error) {
	var in struct {
		Sets map[string]struct {
			Placement  map[string]json.RawMessage `json:"placement"`
			Structures [][2]json.RawMessage       `json:"structures"`
		} `json:"structure_sets"`
		Structures map[string]map[string]json.RawMessage `json:"structures"`
		Biomes     map[string][][]string                 `json:"biomes"`
		Placed     map[string]struct {
			Feature   json.RawMessage   `json:"feature"`
			Placement []json.RawMessage `json:"placement"`
		} `json:"placed_features"`
		Features  map[string]json.RawMessage `json:"features"`
		BlockTags map[string][]string        `json:"block_tags"`
		FluidTags map[string][]string        `json:"fluid_tags"`
		BiomeTags map[string][]string        `json:"biome_tags"`
		Noises    map[string]struct {
			BaseOctave int       `json:"base_octave"`
			BaseAmp    float64   `json:"base_amplitude"`
			Octaves    int       `json:"octave_count"`
			Mods       []float64 `json:"amplitude_modifiers"`
		} `json:"noises"`
		Possible      map[string][]string        `json:"possible_biomes"`
		StartPools    map[string][]vpPoolElem    `json:"start_pools"`
		TemplateSizes map[string][3]int          `json:"template_sizes"`
		Providers     map[string]json.RawMessage `json:"state_providers"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	d := &vpData{
		Sets: map[string]*vpSet{}, Structures: map[string]*vpStructure{}, Biomes: in.Biomes,
		Placed: map[string]*vpPlacedFeature{}, Features: in.Features, BlockTags: in.BlockTags,
		FluidTags: in.FluidTags, BiomeTags: in.BiomeTags, Noises: map[string]vnNoiseParams{}, Possible: in.Possible,
		StartPools: in.StartPools, TemplateSizes: in.TemplateSizes, StateProviders: in.Providers,
	}
	for name, n := range in.Noises {
		d.Noises[name] = vnNoiseParams{baseOctave: n.BaseOctave, baseAmp: n.BaseAmp, octaves: n.Octaves, mods: n.Mods}
	}
	for name, s := range in.Sets {
		p := s.Placement
		set := &vpSet{Name: name, Frequency: 1, Reduction: "default"}
		get := func(k string, v any) error {
			if r, ok := p[k]; ok {
				if err := json.Unmarshal(r, v); err != nil {
					return fmt.Errorf("structure set %s %s: %w", name, k, err)
				}
			}
			return nil
		}
		var spread string
		var excl struct {
			Other  string `json:"other_set"`
			Chunks int32  `json:"chunk_count"`
		}
		var pref []string
		var locate []int
		for _, e := range []error{get("type", &set.Type), get("salt", &set.Salt), get("frequency", &set.Frequency),
			get("frequency_reduction_method", &set.Reduction), get("exclusion_zone", &excl), get("locate_offset", &locate),
			get("spacing", &set.Spacing), get("separation", &set.Separation), get("spread_type", &spread),
			get("distance", &set.Distance), get("spread", &set.Spread), get("count", &set.Count),
			get("preferred_biomes", &pref)} {
			if e != nil {
				return nil, e
			}
		}
		set.Triangular = spread == "triangular"
		set.Exclusion, set.ExclChunks = excl.Other, excl.Chunks
		if len(locate) == 3 {
			set.Locate = [3]int{locate[0], locate[1], locate[2]}
		}
		if pref != nil {
			set.Preferred = map[string]bool{}
			for _, b := range pref {
				set.Preferred[b] = true
			}
		}
		for _, e := range s.Structures {
			var n string
			var w int
			if err := json.Unmarshal(e[0], &n); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(e[1], &w); err != nil {
				return nil, err
			}
			set.Structures = append(set.Structures, vpSetEntry{n, w})
		}
		d.Sets[name] = set
		d.SetNames = append(d.SetNames, name)
	}
	sort.Strings(d.SetNames)
	for name, s := range in.Structures {
		st := &vpStructure{Name: name, Biomes: map[string]bool{}, raw: s}
		var bl []string
		var step string
		if err := json.Unmarshal(s["type"], &st.Type); err != nil {
			return nil, fmt.Errorf("structure %s: %w", name, err)
		}
		if err := json.Unmarshal(s["biomes"], &bl); err != nil {
			return nil, fmt.Errorf("structure %s biomes: %w", name, err)
		}
		if err := json.Unmarshal(s["step"], &step); err != nil {
			return nil, fmt.Errorf("structure %s step: %w", name, err)
		}
		for _, b := range bl {
			st.Biomes[b] = true
		}
		st.Step = -1
		for i, n := range vpStepNames {
			if n == step {
				st.Step = i
			}
		}
		if st.Step < 0 {
			return nil, fmt.Errorf("structure %s: unknown step %q", name, step)
		}
		d.Structures[name] = st
		d.StructNames = append(d.StructNames, name)
	}
	sort.Strings(d.StructNames)
	for name, p := range in.Placed {
		pf := &vpPlacedFeature{Name: name}
		var fname string
		if err := json.Unmarshal(p.Feature, &fname); err == nil {
			pf.Feature = vpShort(fname)
		} else {
			pf.Inline = p.Feature
		}
		for _, m := range p.Placement {
			mod, err := parseVPModifier(m)
			if err != nil {
				return nil, fmt.Errorf("placed feature %s: %w", name, err)
			}
			pf.Modifiers = append(pf.Modifiers, mod)
		}
		d.Placed[name] = pf
	}
	for b, steps := range d.Biomes {
		for _, step := range steps {
			for _, f := range step {
				if d.Placed[f] == nil {
					return nil, fmt.Errorf("biome %s names unknown placed feature %s", b, f)
				}
			}
		}
	}
	return d, nil
}

// vpStepFeatures is FeatureSorter.buildFeaturesPerStep: the global order of
// every placed feature the given biomes list, per decoration step — the
// order whose index salts each feature's seed (setFeatureSeed). sources is
// the biome source's possible biomes in its own order (the order decides
// the indices); biomes gives each one's per-step lists. It reports a
// feature-order cycle as an error, as the server refuses to start.
func vpStepFeatures(sources []string, biomes map[string][][]string) ([][]string, error) {
	type node struct {
		index, step int
		name        string
	}
	less := func(a, b node) bool {
		if a.step != b.step {
			return a.step < b.step
		}
		return a.index < b.index
	}
	key := func(n node) [2]int { return [2]int{n.step, n.index} }
	featureIndex := map[string]int{}
	nodes := map[[2]int]node{}
	edges := map[[2]int]map[[2]int]bool{}
	maxStep := 0
	for _, src := range sources {
		steps := biomes[src]
		if len(steps) > maxStep {
			maxStep = len(steps)
		}
		var list []node
		for i, step := range steps {
			for _, f := range step {
				idx, ok := featureIndex[f]
				if !ok {
					idx = len(featureIndex)
					featureIndex[f] = idx
				}
				list = append(list, node{idx, i, f})
			}
		}
		for i, n := range list {
			k := key(n)
			if _, ok := nodes[k]; !ok {
				nodes[k] = n
			}
			if edges[k] == nil {
				edges[k] = map[[2]int]bool{}
			}
			if i < len(list)-1 {
				edges[k][key(list[i+1])] = true
			}
		}
	}
	sortedKeys := func(m map[[2]int]bool) []node {
		out := make([]node, 0, len(m))
		for k := range m {
			out = append(out, nodes[k])
		}
		sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
		return out
	}
	all := make([]node, 0, len(nodes))
	for _, n := range nodes {
		all = append(all, n)
	}
	sort.Slice(all, func(i, j int) bool { return less(all[i], all[j]) })
	discovered := map[[2]int]bool{}
	visiting := map[[2]int]bool{}
	var order []node
	var dfs func(n node) bool
	dfs = func(n node) bool {
		k := key(n)
		if discovered[k] {
			return false
		}
		if visiting[k] {
			return true
		}
		visiting[k] = true
		for _, next := range sortedKeys(edges[k]) {
			if dfs(next) {
				return true
			}
		}
		delete(visiting, k)
		discovered[k] = true
		order = append(order, n)
		return false
	}
	for _, n := range all {
		if !discovered[key(n)] && dfs(n) {
			return nil, fmt.Errorf("feature order cycle")
		}
	}
	out := make([][]string, maxStep)
	for i := len(order) - 1; i >= 0; i-- {
		n := order[i]
		out[n.step] = append(out[n.step], n.name)
	}
	return out, nil
}
