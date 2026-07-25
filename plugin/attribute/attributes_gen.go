// Code generated from the vanilla attribute registry. DO NOT EDIT.

package attribute

// Def is an attribute's registry definition: where it starts and the
// range a computed value is clamped into.
type Def struct {
	Default, Min, Max float64
}

// Defs is every attribute the game defines, keyed by id.
var Defs = map[ID]Def{
	"minecraft:air_drag_modifier":              {Default: 1, Min: 0, Max: 2048},
	"minecraft:armor":                          {Default: 0, Min: 0, Max: 30},
	"minecraft:armor_toughness":                {Default: 0, Min: 0, Max: 20},
	"minecraft:attack_damage":                  {Default: 2, Min: 0, Max: 2048},
	"minecraft:attack_knockback":               {Default: 0, Min: 0, Max: 5},
	"minecraft:attack_speed":                   {Default: 4, Min: 0, Max: 1024},
	"minecraft:below_name_distance":            {Default: 10, Min: 0, Max: 512},
	"minecraft:block_break_speed":              {Default: 1, Min: 0, Max: 1024},
	"minecraft:block_interaction_range":        {Default: 4.5, Min: 0, Max: 64},
	"minecraft:bounciness":                     {Default: 0, Min: 0, Max: 1},
	"minecraft:burning_time":                   {Default: 1, Min: 0, Max: 1024},
	"minecraft:camera_distance":                {Default: 4, Min: 0, Max: 32},
	"minecraft:entity_interaction_range":       {Default: 3, Min: 0, Max: 64},
	"minecraft:explosion_knockback_resistance": {Default: 0, Min: 0, Max: 1},
	"minecraft:fall_damage_multiplier":         {Default: 1, Min: 0, Max: 100},
	"minecraft:flying_speed":                   {Default: 0.4, Min: 0, Max: 1024},
	"minecraft:follow_range":                   {Default: 32, Min: 0, Max: 2048},
	"minecraft:friction_modifier":              {Default: 1, Min: 0, Max: 2048},
	"minecraft:gravity":                        {Default: 0.08, Min: -1, Max: 1},
	"minecraft:jump_strength":                  {Default: 0.42, Min: 0, Max: 32},
	"minecraft:knockback_resistance":           {Default: 0, Min: -2, Max: 1},
	"minecraft:luck":                           {Default: 0, Min: -1024, Max: 1024},
	"minecraft:max_absorption":                 {Default: 0, Min: 0, Max: 2048},
	"minecraft:max_health":                     {Default: 20, Min: 1, Max: 1024},
	"minecraft:mining_efficiency":              {Default: 0, Min: 0, Max: 1024},
	"minecraft:movement_efficiency":            {Default: 0, Min: 0, Max: 1},
	"minecraft:movement_speed":                 {Default: 0.7, Min: 0, Max: 1024},
	"minecraft:name_tag_distance":              {Default: 64, Min: 0, Max: 512},
	"minecraft:oxygen_bonus":                   {Default: 0, Min: 0, Max: 1024},
	"minecraft:safe_fall_distance":             {Default: 3, Min: -1024, Max: 1024},
	"minecraft:scale":                          {Default: 1, Min: 0.0625, Max: 16},
	"minecraft:sneaking_speed":                 {Default: 0.3, Min: 0, Max: 1},
	"minecraft:spawn_reinforcements":           {Default: 0, Min: 0, Max: 1},
	"minecraft:step_height":                    {Default: 0.6, Min: 0, Max: 10},
	"minecraft:submerged_mining_speed":         {Default: 0.2, Min: 0, Max: 20},
	"minecraft:sweeping_damage_ratio":          {Default: 0, Min: 0, Max: 1},
	"minecraft:tempt_range":                    {Default: 10, Min: 0, Max: 2048},
	"minecraft:water_movement_efficiency":      {Default: 0, Min: 0, Max: 1},
	"minecraft:waypoint_receive_range":         {Default: 0, Min: 0, Max: 6e+07},
	"minecraft:waypoint_transmit_range":        {Default: 0, Min: 0, Max: 6e+07},
}
