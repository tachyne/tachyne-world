package server

import "math"

// The sun's angle through the day, and the eased curve behind it. Vanilla
// keyframes SUN_ANGLE on the DAY timeline — 0° at noon (tick 6000) running
// round to 360° a day later — and eases the whole track with a symmetric
// cubic bezier, which is what makes the sun crawl near the horizon and race
// overhead. The daylight detector reads it, so getting the curve right is
// what makes a detector's output rise and fall the way vanilla's does.

// cubicBezierEase is EasingType.CubicBezier.apply: solve the x curve for t by
// Newton-Raphson (four rounds, as vanilla does), then sample the y curve.
func cubicBezierEase(x1, y1, x2, y2, x float64) float64 {
	curve := func(c1, c2 float64) (a, b, c float64) {
		return 3*c1 - 3*c2 + 1, -6*c1 + 3*c2, 3 * c1
	}
	xa, xb, xc := curve(x1, x2)
	ya, yb, yc := curve(y1, y2)
	sample := func(a, b, c, t float64) float64 { return ((a*t+b)*t + c) * t }
	grad := func(a, b, c, t float64) float64 { return (3*a*t+2*b)*t + c }
	t := x
	for i := 0; i < 4; i++ {
		g := grad(xa, xb, xc, t)
		if g < 1e-5 {
			break
		}
		t -= (sample(xa, xb, xc, t) - x) / g
	}
	return sample(ya, yb, yc, t)
}

// sunAngle is the sun's angle in RADIANS at a day time: 0 at noon, growing
// through the afternoon, night and morning back to 0 a day later.
func sunAngle(dayTime int64) float64 {
	// The track runs from the noon keyframe to the next one, eased.
	frac := float64((dayTime%dayLengthTicks+dayLengthTicks-6000)%dayLengthTicks) / dayLengthTicks
	// symmetricCubicBezier(0.362, 0.241) — Timelines.DAY's easing.
	eased := cubicBezierEase(0.362, 0.241, 1-0.362, 1-0.241, frac)
	return eased * 2 * math.Pi
}
