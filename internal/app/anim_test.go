package app

import (
	"strings"
	"testing"
	"time"
)

func TestEntranceCurve(t *testing.T) {
	if e := entrance(0); e != 0 {
		t.Errorf("entrance(0) = %v, want 0", e)
	}
	if e := entrance(entranceDur); e != 1 {
		t.Errorf("entrance(full) = %v, want 1", e)
	}
	if e := entrance(2 * entranceDur); e != 1 {
		t.Errorf("entrance must clamp at 1, got %v", e)
	}
	prev := -1.0
	for ms := 0; ms <= 600; ms += 50 {
		e := entrance(time.Duration(ms) * time.Millisecond)
		if e < prev {
			t.Fatalf("entrance not monotonic at %dms: %v < %v", ms, e, prev)
		}
		prev = e
	}
}

func TestCountUpAndStagger(t *testing.T) {
	if v := countUp(100, 0.5); v != 50 {
		t.Errorf("countUp(100, .5) = %d, want 50", v)
	}
	if v := countUp(7, 1); v != 7 {
		t.Errorf("countUp full = %d, want 7", v)
	}
	for i := 0; i < 12; i++ {
		if s := stagger(1, i, 12); s != 1 {
			t.Errorf("stagger(1, %d, 12) = %v, want 1 (everything shown at rest)", i, s)
		}
		if s := stagger(0, i, 12); s != 0 {
			t.Errorf("stagger(0, %d, 12) = %v, want 0", i, s)
		}
	}
}

func TestBeltForXP(t *testing.T) {
	cases := []struct {
		xp    int
		name  string
		level int
		next  string // "" = terminal belt
	}{
		{0, "white", 1, "yellow"},
		{99, "white", 1, "yellow"},
		{100, "yellow", 2, "orange"},
		{2500, "purple", 6, "brown"},
		{8999, "red", 8, "black"},
		{9000, "black", 9, ""},
		{50000, "black", 9, ""},
	}
	for _, c := range cases {
		cur, next, lv := beltForXP(c.xp)
		if cur.name != c.name || lv != c.level {
			t.Errorf("beltForXP(%d) = %s lv%d, want %s lv%d", c.xp, cur.name, lv, c.name, c.level)
		}
		if c.next == "" && next != nil {
			t.Errorf("beltForXP(%d): black belt must have no next, got %s", c.xp, next.name)
		}
		if c.next != "" && (next == nil || next.name != c.next) {
			t.Errorf("beltForXP(%d): next = %v, want %s", c.xp, next, c.next)
		}
	}
}

func TestGridReflow(t *testing.T) {
	titles := []string{"belt", "today", "progress", "heatmap", "study lists",
		"forecast", "train now", "recent", "trophies"}
	for _, w := range []int{130, 90, 60} {
		m := dashModel()
		m.width = w
		out := m.viewDashboard()
		for _, title := range titles {
			if !strings.Contains(out, title) {
				t.Errorf("width %d: dashboard missing card %q", w, title)
			}
		}
	}
}

func TestConfettiLineDeterministic(t *testing.T) {
	if confettiLine(30, 5) != confettiLine(30, 5) {
		t.Error("confetti must be deterministic per frame")
	}
}
