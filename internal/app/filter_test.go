package app

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/mahmood/dojo/internal/store"
)

func testModel() *Model {
	m := &Model{filter: textinput.New(),
		diffFilter: map[string]bool{}, tagFilter: map[string]bool{}}
	m.problems = []store.Problem{
		{ID: 1, FrontendID: 1, Title: "Two Sum", Difficulty: "Easy", Status: "solved",
			Acceptance: 50, Tags: []string{"Array", "Hash Table"}},
		{ID: 2, FrontendID: 2, Title: "Add Two Numbers", Difficulty: "Medium", Status: "todo",
			Acceptance: 40, Tags: []string{"Linked List", "Math"}},
		{ID: 3, FrontendID: 3, Title: "Median of Two Sorted Arrays", Difficulty: "Hard", Status: "todo",
			Acceptance: 35, Tags: []string{"Array", "Binary Search"}},
	}
	return m
}

func (m *Model) visibleIDs() []int64 {
	var out []int64
	for _, i := range m.visible {
		out = append(out, m.problems[i].ID)
	}
	return out
}

func eq(a []int64, b ...int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDifficultyToggle(t *testing.T) {
	m := testModel()
	m.diffFilter["Hard"] = true
	m.applyFilter()
	if !eq(m.visibleIDs(), 3) {
		t.Fatalf("hard filter: got %v", m.visibleIDs())
	}
}

func TestQueryTokens(t *testing.T) {
	m := testModel()
	m.filter.SetValue("d:e")
	m.applyFilter()
	if !eq(m.visibleIDs(), 1) {
		t.Fatalf("d:e → got %v", m.visibleIDs())
	}
	m.filter.SetValue("t:array s:todo")
	m.applyFilter()
	if !eq(m.visibleIDs(), 3) {
		t.Fatalf("t:array s:todo → got %v", m.visibleIDs())
	}
	m.filter.SetValue("median")
	m.applyFilter()
	if !eq(m.visibleIDs(), 3) {
		t.Fatalf("fuzzy median → got %v", m.visibleIDs())
	}
}

func TestTagFilterAndUnsolved(t *testing.T) {
	m := testModel()
	m.tagFilter["Array"] = true
	m.applyFilter()
	if !eq(m.visibleIDs(), 1, 3) {
		t.Fatalf("tag Array → got %v", m.visibleIDs())
	}
	m.hideSolved = true
	m.applyFilter()
	if !eq(m.visibleIDs(), 3) {
		t.Fatalf("tag Array + unsolved → got %v", m.visibleIDs())
	}
}

func TestSortByAcceptance(t *testing.T) {
	m := testModel()
	m.sortMode = 2 // acceptance desc = "easiest first"
	m.applyFilter()
	if !eq(m.visibleIDs(), 1, 2, 3) {
		t.Fatalf("acc desc → got %v", m.visibleIDs())
	}
	m.sortMode = 1
	m.applyFilter()
	if !eq(m.visibleIDs(), 3, 2, 1) {
		t.Fatalf("acc asc → got %v", m.visibleIDs())
	}
}
