// Animation clock for the dashboard. All helpers are pure functions of
// elapsed time so views stay deterministic and testable; the only side effect
// is the tea.Tick chain that keeps frames flowing while the dashboard is up.
package app

import (
	"math"
	"time"
)

const (
	animFrame    = 83 * time.Millisecond // ~12 FPS while the dashboard is visible
	idleFrame    = 30 * time.Second      // browser screen: just keep the clock honest
	entranceDur  = 600 * time.Millisecond
	quotePeriod  = 20 * time.Second
	quoteFadeDur = 800 * time.Millisecond
	confettiDur  = 1500 * time.Millisecond
)

// easeOutCubic maps linear 0..1 time onto a decelerating curve.
func easeOutCubic(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	u := 1 - t
	return 1 - u*u*u
}

// entrance is the master 0..1 factor for opening animations.
func entrance(elapsed time.Duration) float64 {
	return easeOutCubic(float64(elapsed) / float64(entranceDur))
}

// pulse oscillates 0..1 with the given period — the streak flame's heartbeat.
func pulse(elapsed, period time.Duration) float64 {
	if period <= 0 {
		return 1
	}
	phase := float64(elapsed%period) / float64(period)
	return (1 + math.Sin(2*math.Pi*phase)) / 2
}

// countUp animates an integer from 0 to target under factor f.
func countUp(target int, f float64) int {
	return int(float64(target)*f + 0.5)
}

// stagger returns the 0..1 progress of item i of n under a global factor f,
// so items reveal one after another (heat-map columns, list rows).
func stagger(f float64, i, n int) float64 {
	if n <= 1 {
		return f
	}
	per := 1.0 / float64(n)
	local := (f - float64(i)*per*0.5) / (1 - float64(n-1)*per*0.5)
	return math.Max(0, math.Min(1, local))
}
