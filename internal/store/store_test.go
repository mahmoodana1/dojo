package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mahmood/dojo/internal/srs"
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

func TestSolveCreditGrantedOnce(t *testing.T) {
	s := testStore(t)
	seedProblem(t, s, 1, "Two Sum", "todo")
	now := time.Now()

	attempt := func(passed bool, at time.Time) Attempt {
		return Attempt{ProblemID: 1, StartedAt: at.Add(-10 * time.Minute),
			FinishedAt: at, Language: "python", Passed: passed,
			CasesPassed: 3, CasesTotal: 3}
	}
	dailyRow := func() (solved, attempts, xp int) {
		t.Helper()
		if err := s.db.QueryRow(`SELECT solved, attempts, xp FROM daily_stats
			WHERE day = ?`, now.Format("2006-01-02")).Scan(&solved, &attempts, &xp); err != nil {
			t.Fatal(err)
		}
		return
	}

	// fail, then pass, then re-run the pass twice more (same day)
	for i, passed := range []bool{false, true, true, true} {
		if err := s.RecordAttempt(attempt(passed, now.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}

	solved, attempts, xp := dailyRow()
	if solved != 1 {
		t.Errorf("daily solved = %d, want 1 — re-runs must not re-register", solved)
	}
	if xp != 10 {
		t.Errorf("daily xp = %d, want 10 (credit once)", xp)
	}
	if attempts != 4 {
		t.Errorf("daily attempts = %d, want 4 — effort still counts", attempts)
	}

	// a pass on a later day is still no new solve credit
	if err := s.RecordAttempt(attempt(true, now.AddDate(0, 0, 1))); err != nil {
		t.Fatal(err)
	}
	var nextSolved, nextXP int
	if err := s.db.QueryRow(`SELECT solved, xp FROM daily_stats WHERE day = ?`,
		now.AddDate(0, 0, 1).Format("2006-01-02")).Scan(&nextSolved, &nextXP); err != nil {
		t.Fatal(err)
	}
	if nextSolved != 0 || nextXP != 0 {
		t.Errorf("next-day re-pass: solved=%d xp=%d, want 0/0", nextSolved, nextXP)
	}

	var status string
	var firstSolved *string
	if err := s.db.QueryRow(`SELECT status, first_solved_at FROM problems
		WHERE id = 1`).Scan(&status, &firstSolved); err != nil {
		t.Fatal(err)
	}
	if status != "solved" || firstSolved == nil {
		t.Errorf("problem should stay solved with first_solved_at set: %s %v", status, firstSolved)
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

// A schedule written before srs gained its bounds must be clamped when the
// database is opened, and its due date recomputed from the last attempt.
// The year-28705 row this reproduces did not parse as RFC3339, so GetReview
// read it as the zero time and the card was lost from the queue.
func TestMigrationRepairsRunawaySchedule(t *testing.T) {
	s := testStore(t)
	seedProblem(t, s, 1406, "Runaway", "todo")
	mustExec(t, s, `INSERT INTO attempts
		(problem_id, started_at, finished_at, language, passed, cases_passed, cases_total, wall_ms)
		VALUES (?, ?, ?, 'go', 1, 1, 1, 100)`,
		1406, "2026-08-16T19:10:45Z", "2026-08-16T20:10:45Z")
	mustExec(t, s, `INSERT INTO reviews (problem_id, ease, interval_days, reps, lapses, due_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		1406, 3.5600000000000014, 9744440, 16, 1, "28705-12-30T21:10:45Z")

	repairRunawaySchedules(s.db)

	card, err := s.GetReview(1406)
	if err != nil {
		t.Fatal(err)
	}
	if card.IntervalDays != srs.MaxIntervalDays {
		t.Errorf("interval = %d, want %d", card.IntervalDays, srs.MaxIntervalDays)
	}
	if card.Ease > srs.MaxEase {
		t.Errorf("ease = %v, want <= %v", card.Ease, srs.MaxEase)
	}
	want := time.Date(2027, 8, 16, 20, 10, 45, 0, time.UTC)
	if !card.Due.Equal(want) {
		t.Errorf("due = %v, want %v (last attempt + cap)", card.Due, want)
	}

	// Idempotent: a second open must not shift the repaired schedule again.
	repairRunawaySchedules(s.db)
	again, _ := s.GetReview(1406)
	if !again.Due.Equal(card.Due) {
		t.Errorf("re-running moved due from %v to %v", card.Due, again.Due)
	}
}

func TestDetailByFrontendID(t *testing.T) {
	s := testStore(t)
	mustExec(t, s, `INSERT INTO problems (id, frontend_id, slug, title, difficulty, hints_json)
		VALUES (77, 1, 'two-sum', 'Two Sum', 'Easy', '["use a map"]')`)
	mustExec(t, s, `INSERT INTO tags (slug, name) VALUES ('array', 'Array'), ('hash-table', 'Hash Table')`)
	mustExec(t, s, `INSERT INTO problem_tags (problem_id, tag_id) SELECT 77, id FROM tags`)

	d, err := s.DetailByFrontendID(1)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != 77 || d.Title != "Two Sum" || len(d.Hints) != 1 || len(d.Tags) != 2 {
		t.Errorf("DetailByFrontendID = %+v", d)
	}
	if _, err := s.DetailByFrontendID(999); err == nil {
		t.Error("missing problem: no error")
	}
}
