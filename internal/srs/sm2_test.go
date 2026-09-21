package srs

import (
	"testing"
	"time"
)

func TestReviewProgression(t *testing.T) {
	now := time.Now()
	c := New()
	c = Review(c, 5, now) // first clean solve
	if c.IntervalDays != 1 || c.Reps != 1 {
		t.Fatalf("after first pass: %+v", c)
	}
	c = Review(c, 5, now)
	if c.IntervalDays != 6 || c.Reps != 2 {
		t.Fatalf("after second pass: %+v", c)
	}
	c = Review(c, 5, now)
	if c.IntervalDays < 13 {
		t.Fatalf("third interval should grow by ease factor: %+v", c)
	}
	c = Review(c, 1, now) // lapse
	if c.IntervalDays != 1 || c.Reps != 0 || c.Lapses != 1 {
		t.Fatalf("lapse should reset: %+v", c)
	}
	if c.Ease < 1.3 {
		t.Fatalf("ease floor violated: %+v", c)
	}
}

// A long streak of clean solves must not schedule a card past the cap.
// Before the bounds existed this ran to millions of days, which is how
// live cards ended up due in the year 28705 and left the queue for good.
func TestIntervalNeverRunsAway(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New()
	for i := 1; i <= 50; i++ {
		c = Review(c, 5, now)
		if c.IntervalDays > MaxIntervalDays {
			t.Fatalf("review %d: interval %d days exceeds cap %d", i, c.IntervalDays, MaxIntervalDays)
		}
		if c.Ease > MaxEase {
			t.Fatalf("review %d: ease %.2f exceeds cap %.2f", i, c.Ease, MaxEase)
		}
	}
	if c.Due.After(now.AddDate(0, 0, MaxIntervalDays)) {
		t.Fatalf("due %v is past the cap measured from %v", c.Due, now)
	}
}

// Recovery from a lapse must still start over at one day, cap or no cap.
func TestLapseResetsUnderCap(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New()
	for i := 0; i < 30; i++ {
		c = Review(c, 5, now)
	}
	c = Review(c, 1, now)
	if c.IntervalDays != 1 || c.Reps != 0 {
		t.Fatalf("lapse after a long streak should reset: %+v", c)
	}
}

func TestQuality(t *testing.T) {
	cases := []struct {
		passed  bool
		hints   int
		minutes float64
		want    int
	}{
		{false, 0, 10, 1},
		{true, 0, 20, 5},
		{true, 1, 20, 4},
		{true, 3, 60, 3},
	}
	for _, tc := range cases {
		if got := Quality(tc.passed, tc.hints, tc.minutes, 35); got != tc.want {
			t.Errorf("Quality(%v,%d,%.0f) = %d, want %d", tc.passed, tc.hints, tc.minutes, got, tc.want)
		}
	}
}
