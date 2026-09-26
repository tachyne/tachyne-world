package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /compute through the dispatcher: registry providers by id, the block
// source feeding a conditional's match_block, inline SNBT providers with a
// rounded float and a scale, Java's arithmetic failures, an unknown id, the
// score fallback, and the permission gate.
func TestComputeCommand(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice, carol := ps["alice"], ps["carol"]
	onHub(t, h, func() {
		h.world.SetBlock(3, 200, 3, worldgen.BlockBase("smoker"))
		h.world.SetBlock(4, 200, 3, worldgen.BlockBase("furnace"))
	})
	for _, line := range []string{
		"compute default integer minecraft:brewing/uses_default",
		"compute default float minecraft:cooking/speed_default",
		"compute block 3 200 3 float minecraft:cooking/speed_default",
		"compute block 3 200 3 integer cooking/time_coal",
		"compute block 4 200 3 integer cooking/time_coal",
		`compute default float {type:"minecraft:div",left:5,right:2}`,
		`compute default float {type:div,left:5,right:2} 10`,
		`compute default integer {type:"minecraft:div",left:1,right:0}`,
		`compute default integer {type:add,inputs:[2147483647,1]}`,
		`compute default integer {type:sub,left:-2147483648,right:1}`,
		`compute default float {type:sqrt,input:-1}`,
		`compute default float {type:ceil,input:{type:sqrt,input:-1}}`,
		"compute default integer minecraft:nope",
		"compute default integer 5",
		`compute entity bob integer {type:score,target:"target_entity",score:"kills",fallback:7}`,
		`compute default integer {type:pow,base:2,exponent:10}`,
		`compute default float {type:sin,input:0}`,
		`compute block 3 200 3 integer {type:number_dispatcher,cases:[{condition:{type:match_block,blocks:"#minecraft:mineable/pickaxe"},value:9}]}`,
		`compute default integer {type:unknown_thing}`,
	} {
		s.handleCommand(alice, line)
	}
	s.handleCommand(carol, "compute default integer minecraft:brewing/uses_default")
	settle(t, h, logs, "C1")
	a := linesBetween(logs["alice"], "", "C1")
	for _, want := range []string{
		"minecraft:brewing/uses_default returned value 20",
		"minecraft:cooking/speed_default returned value 1",
		"minecraft:cooking/speed_default returned value 2",
		"minecraft:cooking/time_coal returned value 800",
		"minecraft:cooking/time_coal returned value 1600",
		"Number provider returned value 2.5 (rounded to 2)",
		"Number provider returned value 2.5 (rounded to 25)",
		"Number provider returned invalid value (/ by zero)",
		"Number provider returned invalid value (Value 2147483648 can't be safely converted to int)",
		"Number provider returned invalid value (integer overflow)",
		"Number provider returned invalid value (NaN)",
		"Number provider returned value 0",
		"Can't find element 'minecraft:nope' in registry 'minecraft:context_int_provider'",
		"Can't find element 'minecraft:5' in registry 'minecraft:context_int_provider'",
		"Number provider returned value 7",
		"Number provider returned value 1024",
		"Number provider returned value 9",
		"Failed to parse structure: Unknown registry key in ResourceKey[minecraft:root / minecraft:context_int_provider_type]: minecraft:unknown_thing",
	} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	if c := linesBetween(logs["carol"], "", "C1"); !hasLine(c, "You don't have permission.") {
		t.Errorf("a non-operator ran /compute: %q", c)
	}
}

// Java's number spelling, which /compute's rounded and invalid lines use.
func TestJFloatJavaSpelling(t *testing.T) {
	for v, want := range map[float32]string{
		2.5: "2.5", 1: "1.0", 1e7: "1.0E7", 1.5e-4: "1.5E-4", 0.001: "0.001",
		-3: "-3.0", 123456.7: "123456.7",
	} {
		if got := jFloat(v); got != want {
			t.Errorf("jFloat(%v) = %q, want %q", v, got, want)
		}
	}
	if jFloat(float32(math.NaN())) != "NaN" || jFloat(float32(math.Inf(-1))) != "-Infinity" {
		t.Error("NaN or -Infinity misspelled")
	}
}
