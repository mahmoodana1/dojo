package app

import (
	"strings"
	"testing"

	"github.com/mahmood/dojo/internal/store"
)

func dashModel() *Model {
	return &Model{
		width: 120, height: 40,
		quote: "test quote",
		lists: []store.ProblemList{
			{ID: 1, Name: "First 50", Total: 50, Solved: 3},
			{ID: 2, Name: "Blind 75", Total: 75, Solved: 1},
		},
		stats: store.DashboardStats{
			Total: 100, Solved: 4, StreakDays: 2,
			Activity:   make([]int, store.ActivityDays),
			DiffTotal:  map[string]int{"Easy": 40, "Medium": 40, "Hard": 20},
			DiffSolved: map[string]int{"Easy": 3, "Medium": 1},
			Due: []store.Problem{
				{ID: 10, FrontendID: 207, Title: "Course Schedule", Difficulty: "Medium"},
			},
			Suggested: []store.Problem{
				{ID: 11, FrontendID: 1, Title: "Two Sum", Difficulty: "Easy"},
				{ID: 12, FrontendID: 9001, Title: "Order of the Runes", Difficulty: "Hard"},
			},
		},
	}
}

func TestDashItemsOrderAndKinds(t *testing.T) {
	m := dashModel()
	items := m.dashItems()
	// All problems + 2 lists + 1 due + 2 suggested
	if len(items) != 6 {
		t.Fatalf("want 6 items, got %d", len(items))
	}
	if items[0].listIdx != -1 || items[0].prob != nil {
		t.Errorf("item 0 should be the All-problems scope: %+v", items[0])
	}
	if items[1].listIdx != 0 || items[2].listIdx != 1 {
		t.Errorf("items 1-2 should be the lists: %+v %+v", items[1], items[2])
	}
	for i := 3; i < 6; i++ {
		if items[i].prob == nil {
			t.Errorf("item %d should carry a problem", i)
		}
	}
	if items[3].prob.ID != 10 || items[5].prob.ID != 12 {
		t.Errorf("due-then-suggested order broken: %v %v", items[3].prob.ID, items[5].prob.ID)
	}
}

func TestClampDash(t *testing.T) {
	m := dashModel()
	m.dashCursor = 99
	m.clampDash()
	if m.dashCursor != 5 {
		t.Errorf("cursor should clamp to last item (5), got %d", m.dashCursor)
	}
	m.stats.Due, m.stats.Suggested, m.lists = nil, nil, nil
	m.clampDash()
	if m.dashCursor != 0 {
		t.Errorf("cursor should clamp to 0 with only the All row, got %d", m.dashCursor)
	}
}

func TestViewDashboardRenders(t *testing.T) {
	m := dashModel()
	for cursor := 0; cursor < 6; cursor++ {
		m.dashCursor = cursor
		out := m.viewDashboard()
		for _, want := range []string{"study lists", "due for review", "sensei suggests",
			"today", "streak", "progress", "Course Schedule", "Two Sum", "test quote"} {
			if !strings.Contains(out, want) {
				t.Fatalf("cursor %d: dashboard missing %q", cursor, want)
			}
		}
	}
	// empty state: no data at all must still render
	empty := &Model{width: 80, height: 24, quote: "q"}
	if out := empty.viewDashboard(); !strings.Contains(out, "nothing due") {
		t.Error("empty dashboard should show the clear-queue message")
	}
}
