package worldgen

// Block-state property arithmetic. A block's states form a contiguous range
// [Min, Min+∏radices); the state ID is a mixed-radix number over the property
// value indices, with the FIRST property most significant. So changing one
// property is just adding (newIndex-oldIndex)*multiplier, where multiplier is the
// product of the radices of the properties that follow it.

// HasProperty reports whether the block has a property of the given name.
func (info BlockInfo) HasProperty(name string) bool {
	for _, p := range info.Props {
		if p.Name == name {
			return true
		}
	}
	return false
}

// SetProperty returns state with property `name` set to `value`. state must lie
// in the block's range. An unknown property or value leaves state unchanged.

// GetProperty returns the current value of property `name` in state, or "" if the
// block has no such property.
func GetProperty(info BlockInfo, state uint32, name string) string {
	mult := 1
	for i := len(info.Props) - 1; i >= 0; i-- {
		p := info.Props[i]
		n := len(p.Vals)
		if p.Name == name {
			if idx := (int(state-info.Min) / mult) % n; idx >= 0 && idx < n {
				return p.Vals[idx]
			}
			return ""
		}
		mult *= n
	}
	return ""
}

func SetProperty(info BlockInfo, state uint32, name, value string) uint32 {
	mult := 1
	for i := len(info.Props) - 1; i >= 0; i-- {
		p := info.Props[i]
		n := len(p.Vals)
		if p.Name == name {
			vi := -1
			for j, v := range p.Vals {
				if v == value {
					vi = j
					break
				}
			}
			if vi < 0 {
				return state
			}
			cur := (int(state-info.Min) / mult) % n
			return uint32(int(state) + (vi-cur)*mult)
		}
		mult *= n
	}
	return state
}
