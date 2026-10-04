package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mahmood/dojo/internal/runner"
	"github.com/mahmood/dojo/internal/store"
)

// solveModel is testModel backed by an empty store, for paths that open a
// problem (which looks up its code info).
func solveModel(t *testing.T) *Model {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := testModel()
	m.st = st
	m.keys = defaultKeyMap()
	m.width, m.height = 120, 40
	m.renderCache = map[int64]string{}
	m.editStarts = map[int64]time.Time{}
	m.reports = map[int64]runner.Report{}
	m.hintsUsed = map[int64]int{}
	m.applyFilter()
	return m
}

func keys(m *Model, ks ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range ks {
		_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	return cmd
}

func TestDDOnDashboardHidesNothing(t *testing.T) {
	m := solveModel(t)
	m.screen = screenDashboard
	keys(m, "d", "d")
	keys(m, "dd") // both keys in one read
	if len(m.hiddenUndo) != 0 {
		t.Errorf("dd on the dashboard hid %v", m.hiddenUndo)
	}
}

func TestDDInBrowserHidesAndUndoRestores(t *testing.T) {
	m := solveModel(t)
	m.screen = screenBrowser
	m.cursor = 1
	if cmd := keys(m, "dd"); cmd == nil {
		t.Fatal("dd produced no command")
	}
	if len(m.hiddenUndo) != 1 || m.hiddenUndo[0] != 2 {
		t.Fatalf("hiddenUndo = %v, want [2]", m.hiddenUndo)
	}
	if cmd := keys(m, "U"); cmd == nil {
		t.Fatal("U produced no command")
	}
	if len(m.hiddenUndo) != 0 {
		t.Errorf("undo left %v", m.hiddenUndo)
	}
	keys(m, "U")
	if m.status != "nothing to undo" {
		t.Errorf("status = %q", m.status)
	}
}

func TestEditorDoneJudgesTheEditedProblem(t *testing.T) {
	m := solveModel(t)
	m.screen = screenBrowser
	m.cursor = 0
	m.setDetail(m.selected()) // looking at problem 1…
	m.Update(editorDoneMsg{problemID: 3, lang: "python"})
	// …but the editor that closed was problem 3's
	if m.detail == nil || m.detail.ID != 3 {
		t.Fatalf("detail = %+v, want problem 3", m.detail)
	}
}

func TestReportForAnotherProblemGoesToStatus(t *testing.T) {
	m := solveModel(t)
	m.screen = screenBrowser
	m.setDetail(m.selected()) // problem 1
	rep := runner.Report{Passed: 2, Total: 3}
	m.Update(reportMsg{rep: rep, problemID: 2, label: "2. Add Two Numbers"})
	if m.report != nil {
		t.Error("problem 2's verdict shown on problem 1")
	}
	if !strings.Contains(m.status, "2. Add Two Numbers: FAIL 2/3") {
		t.Errorf("status = %q", m.status)
	}
	// opening problem 2 later shows it
	m.cursor = 1
	m.setDetail(m.selected())
	if m.report == nil || m.report.Passed != 2 {
		t.Errorf("verdict not kept for problem 2: %+v", m.report)
	}
}

func TestHintsSurviveMovingAway(t *testing.T) {
	m := solveModel(t)
	m.screen = screenBrowser
	m.content = map[int64]store.Content{1: {Hints: []string{"a", "b"}}}
	m.setDetail(m.selected())
	keys(m, "H")
	m.cursor = 1
	m.setDetail(m.selected())
	m.cursor = 0
	m.setDetail(m.selected())
	if m.revealedHints != 1 {
		t.Errorf("revealedHints = %d after coming back, want 1", m.revealedHints)
	}
}

func TestWindowName(t *testing.T) {
	if got := windowName(1, "Two Sum"); got != "✎1 Two Sum" {
		t.Errorf("windowName = %q", got)
	}
	got := windowName(1099, "Two Sum Less Than K Extra Long")
	if r := []rune(got); len(r) > 24 || !strings.HasSuffix(got, "…") {
		t.Errorf("long title not shortened: %q", got)
	}
}

func TestVerdictLineAndPlainReport(t *testing.T) {
	pass := runner.Report{Passed: 3, Total: 3}
	fail := runner.Report{Passed: 1, Total: 2, Results: []runner.CaseResult{
		{Index: 1, Pass: true}, {Index: 2, Expected: "[0,1]", Got: "[0,0]"}}}
	if verdictLine(&pass) != "PASS 3/3" || verdictLine(&fail) != "FAIL 1/2" {
		t.Errorf("verdict lines: %q %q", verdictLine(&pass), verdictLine(&fail))
	}
	if verdictLine(&runner.Report{Note: "x"}) != "not judged locally" {
		t.Error("note report")
	}
	txt := plainReport(&fail, "python", time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC))
	for _, want := range []string{"python", "FAIL 1/2", "case 2: FAIL", "expected:\n[0,1]", "got:\n[0,0]"} {
		if !strings.Contains(txt, want) {
			t.Errorf("plain report lacks %q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "\x1b[") {
		t.Error("plain report has ANSI escapes")
	}
}
