package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Walking over fences. A fence, a wall and a closed fence gate collide up
// to 1.5 blocks, so Entity.collide's step-up carries a mob over one only
// when its STEP_HEIGHT reaches 1.5 — the camel's, and nobody else's by
// default. The camel also plans routes across them (Camel.createNavigation:
// setCanWalkOverFences), where every other walker's WalkNodeEvaluator reads
// a fence as PathType.FENCE, a wall. Every other mob still goes round.

// tallCollisionTop is a fence's, wall's or closed gate's collision height.
const tallCollisionTop = 1.5

// stepsOverFences reports a mob whose STEP_HEIGHT carries it up onto a
// fence top.
func (m *mob) stepsOverFences() bool {
	return m.attrs != nil && m.attrs.Peek(attr.StepHeight) >= tallCollisionTop
}

// plansOverFences is setCanWalkOverFences: the camels' navigation.
func (m *mob) plansOverFences() bool {
	return m.etype == entityCamel || m.etype == entityCamelHusk
}

// fenceTopAt is the floor a fence-stepping mob stands on in column (x, z)
// when the cell at its feet level y is a single fence, wall or closed gate
// with room above for its body: y + 1.5. A fence stacked two high, or one
// under a low roof, has none.
func (h *hub) fenceTopAt(m *mob, x, y, z int) (float64, bool) {
	w := h.worldFor(m.dim)
	if w == nil || !worldgen.IsTallCollision(w.At(x, y, z)) {
		return 0, false
	}
	top := float64(y) + tallCollisionTop
	for cy := y + 1; float64(cy) < top+m.box().h; cy++ {
		if worldgen.Collides(w.At(x, cy, z)) {
			return 0, false
		}
	}
	return top, true
}

// fencePather lets A* route a camel over single fences: such a column is no
// obstacle, and its floor is one block up (the fence top at 1.5, which a
// jumpSize of floor(1.5) = 1 reaches, as findAcceptedNode's tryJumpOn does).
type fencePather struct {
	pather
	bodyH float64
}

// overFence reports a column whose floor is a single fence with room above.
func (f fencePather) overFence(x, z int) bool {
	y := f.pather.MobFeet(x, z)
	if !worldgen.IsTallCollision(f.pather.Block(x, y, z)) {
		return false
	}
	for cy := y + 1; float64(cy) < float64(y)+tallCollisionTop+f.bodyH; cy++ {
		if worldgen.Collides(f.pather.Block(x, cy, z)) {
			return false
		}
	}
	return true
}

func (f fencePather) TallObstacle(x, z int) bool {
	if f.overFence(x, z) {
		return false
	}
	return f.pather.TallObstacle(x, z)
}

func (f fencePather) MobFeet(x, z int) int {
	if f.overFence(x, z) {
		return f.pather.MobFeet(x, z) + 1
	}
	return f.pather.MobFeet(x, z)
}

// fenceRiseOK is mobStepOK's answer for a fence-stepping mob moving onto a
// fence top at the given floor: the rise within its STEP_HEIGHT.
func (m *mob) fenceRiseOK(top float64) bool {
	return top-m.y <= m.attrs.Peek(attr.StepHeight)+1e-9 && top-m.y >= -pathMaxFall
}
