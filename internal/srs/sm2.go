// Package srs implements SM-2 spaced repetition, adapted for algorithm
// practice: the "grade" is derived from how the solve actually went
// (pass/fail, hints used, time spent) instead of self-assessment.
package srs

import "time"

const (
	// MaxIntervalDays bounds how far ahead a card can be scheduled. SM-2
	// multiplies the interval by the ease on every success, so without a
	// ceiling the two compound geometrically: about fifteen clean solves
	// puts a card tens of millions of days out and it never resurfaces.
	// A year is the useful limit here — an algorithm problem you have not
	// seen in twelve months is one you no longer know.
	MaxIntervalDays = 365

	// MaxEase bounds the multiplier itself. The floor at 1.3 was always
	// enforced; without a matching ceiling the ease climbs 0.1 per clean
	// solve forever, so a card recovering from a lapse would leap straight
	// back to the interval cap.
	MaxEase = 3.0
)

type Card struct {
	Ease         float64
	IntervalDays int
	Reps         int
	Lapses       int
	Due          time.Time
}

func New() Card { return Card{Ease: 2.5} }

// Review applies SM-2. quality is in [0,5]; below 3 counts as a lapse and
// resets the interval so the problem resurfaces tomorrow.
func Review(c Card, quality int, now time.Time) Card {
	if quality < 0 {
		quality = 0
	}
	if quality > 5 {
		quality = 5
	}
	if quality < 3 {
		c.Reps = 0
		c.Lapses++
		c.IntervalDays = 1
	} else {
		switch c.Reps {
		case 0:
			c.IntervalDays = 1
		case 1:
			c.IntervalDays = 6
		default:
			c.IntervalDays = int(float64(c.IntervalDays)*c.Ease + 0.5)
		}
		if c.IntervalDays > MaxIntervalDays {
			c.IntervalDays = MaxIntervalDays
		}
		c.Reps++
	}
	q := float64(5 - quality)
	c.Ease += 0.1 - q*(0.08+q*0.02)
	if c.Ease < 1.3 {
		c.Ease = 1.3
	}
	if c.Ease > MaxEase {
		c.Ease = MaxEase
	}
	c.Due = now.AddDate(0, 0, c.IntervalDays)
	return c
}

// Quality maps solve outcome -> SM-2 grade.
//
//	failed                        -> 1
//	passed, heavy hints/overtime  -> 3
//	passed, some help or slow     -> 4
//	passed clean and under budget -> 5
func Quality(passed bool, hintsUsed int, minutes, budgetMinutes float64) int {
	if !passed {
		return 1
	}
	over := budgetMinutes > 0 && minutes > budgetMinutes
	switch {
	case hintsUsed == 0 && !over:
		return 5
	case hintsUsed <= 1 && !over:
		return 4
	case hintsUsed <= 1 || !over:
		return 3
	default:
		return 3
	}
}
