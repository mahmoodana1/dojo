package store

import (
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustExec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func seedProblem(t *testing.T, s *Store, id int64, title, status string) {
	mustExec(t, s, `INSERT INTO problems (id, frontend_id, slug, title, difficulty, status)
		VALUES (?,?,?,?, 'Easy', ?)`, id, id, title+"-slug", title, status)
}

func TestDashboardWidgetStats(t *testing.T) {
	s := testStore(t)
	seedProblem(t, s, 1, "Two Sum", "solved")
	seedProblem(t, s, 2, "Add Two Numbers", "attempted")
	seedProblem(t, s, 3, "Course Schedule", "todo")

	now := time.Now()
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	rfc := func(offset time.Duration) string { return now.Add(offset).UTC().Format(time.RFC3339) }

	// XP over three days, with a 2-day run and a gap
	mustExec(t, s, `INSERT INTO daily_stats (day, solved, attempts, minutes, xp) VALUES
		(?, 2, 3, 50, 120), (?, 1, 1, 10, 40), (?, 3, 4, 90, 200)`,
		day(-5), day(-4), day(0))

	// attempts: first attempt on p1 failed, then passed; p2 passed first try
	att := `INSERT INTO attempts (problem_id, started_at, finished_at, language,
		passed, cases_passed, cases_total, wall_ms) VALUES (?,?,?,?,?,?,?,?)`
	mustExec(t, s, att, 1, rfc(-3*time.Hour), rfc(-3*time.Hour+40*time.Minute), "python", 0, 1, 3, 900)
	mustExec(t, s, att, 1, rfc(-2*time.Hour), rfc(-2*time.Hour+9*time.Minute), "python", 1, 3, 3, 700)
	mustExec(t, s, att, 2, rfc(-1*time.Hour), rfc(-1*time.Hour+25*time.Minute), "cpp", 1, 4, 4, 1200)

	// reviews: one overdue, one tomorrow, one day after tomorrow, one far out
	rev := `INSERT INTO reviews (problem_id, due_at) VALUES (?, ?)`
	mustExec(t, s, rev, 1, rfc(-24*time.Hour))
	mustExec(t, s, rev, 2, rfc(26*time.Hour))
	mustExec(t, s, rev, 3, rfc(50*time.Hour))

	st, err := s.Dashboard()
	if err != nil {
		t.Fatal(err)
	}

	if st.TotalXP != 360 {
		t.Errorf("TotalXP = %d, want 360", st.TotalXP)
	}
	if st.Forecast[0] != 1 {
		t.Errorf("Forecast[0] (overdue) = %d, want 1", st.Forecast[0])
	}
	if got := st.Forecast[1] + st.Forecast[2] + st.Forecast[3]; got != 2 {
		t.Errorf("upcoming buckets = %d, want the 26h and 50h reviews (2)", got)
	}
	sum := 0
	for _, n := range st.Forecast {
		sum += n
	}
	if sum != 3 {
		t.Errorf("forecast total = %d, want all 3 pending reviews", sum)
	}

	if len(st.Recent) != 3 {
		t.Fatalf("Recent = %d rows, want 3", len(st.Recent))
	}
	if st.Recent[0].Title != "Add Two Numbers" || !st.Recent[0].Passed {
		t.Errorf("Recent[0] should be the newest (cpp pass), got %+v", st.Recent[0])
	}

	r := st.Records
	if r.FastestTitle != "Two Sum" || r.FastestMin < 8.9 || r.FastestMin > 9.1 {
		t.Errorf("fastest = %q %.1fm, want Two Sum ~9m", r.FastestTitle, r.FastestMin)
	}
	if r.BestDay != 3 || r.BestDayDate != day(0) {
		t.Errorf("best day = %d on %s, want 3 on %s", r.BestDay, r.BestDayDate, day(0))
	}
	if r.LongestRun != 2 {
		t.Errorf("longest run = %d, want 2 (the -5/-4 pair)", r.LongestRun)
	}
	if r.FirstTryPct != 50 {
		t.Errorf("first-try = %d%%, want 50 (1 of 2 problems)", r.FirstTryPct)
	}
}

func TestDashboardEmptyDB(t *testing.T) {
	s := testStore(t)
	st, err := s.Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalXP != 0 || len(st.Recent) != 0 || st.Records.BestDay != 0 {
		t.Errorf("empty DB should produce zero widgets: %+v", st)
	}
	if st.Records.FirstTryPct != -1 {
		t.Errorf("FirstTryPct = %d, want -1 (no attempts)", st.Records.FirstTryPct)
	}
}
