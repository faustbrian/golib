// Package netflix contains the pinned Gradient2 reference equation used by the
// comparison harness. It is not a port of Netflix limiter lifecycle or JVM
// runtime behavior.
package netflix

import "math"

// Gradient2 is a transparent translation of the pinned Netflix update state.
type Gradient2 struct {
	longRTT  float64
	samples  int
	estimate float64
	minimum  float64
	maximum  float64
}

// New constructs the fixed comparison profile.
func New(initial, minimum, maximum int) *Gradient2 {
	return &Gradient2{
		estimate: float64(initial), minimum: float64(minimum), maximum: float64(maximum),
	}
}

// Update applies one aggregate RTT and in-flight sample.
func (algorithm *Gradient2) Update(rtt float64, inflight int) {
	if algorithm.samples < 10 {
		algorithm.samples++
		algorithm.longRTT += (rtt - algorithm.longRTT) / float64(algorithm.samples)
	} else {
		algorithm.longRTT += (2.0 / 21.0) * (rtt - algorithm.longRTT)
	}
	if rtt > 0 && algorithm.longRTT/rtt > 2 {
		algorithm.longRTT *= 0.95
	}
	if float64(inflight) < algorithm.estimate/2 {
		return
	}
	gradient := math.Max(0.5, math.Min(1, 1.5*algorithm.longRTT/rtt))
	target := algorithm.estimate*gradient + 4
	algorithm.estimate = math.Max(
		algorithm.minimum,
		math.Min(algorithm.maximum, algorithm.estimate*0.8+target*0.2),
	)
}

// Limit returns the floor of the current fractional estimate, matching the
// pinned implementation's public integer result.
func (algorithm *Gradient2) Limit() int { return int(math.Floor(algorithm.estimate)) }
