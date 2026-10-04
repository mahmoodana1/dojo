// Package store is the single owner of SQLite access. UI code never touches
// SQL; it calls methods here and receives plain structs.
package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mahmood/dojo/internal/runner"
	"github.com/mahmood/dojo/internal/srs"
)

//go:embed schema.sql
var schema string

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	migrate(db)
	return &Store{db: db}, nil
}

// migrate adds columns to databases created by an older dojo. schema.sql is
// all CREATE IF NOT EXISTS, so it never alters an existing table; these do.
// A duplicate-column error means the migration already ran — ignore it.
func migrate(db *sql.DB) {
	for _, ddl := range []string{
		`ALTER TABLE problems ADD COLUMN snippets_json TEXT`,
	} {
		_, _ = db.Exec(ddl)
	}
	repairRunawaySchedules(db)
}

// repairRunawaySchedules clamps rows written before srs gained its bounds.
//
// The scheduler multiplied interval by ease on every success with no ceiling
// on either, so a well-known card could reach millions of days and fall out
// of the queue permanently — one row here was due in the year 28705, which
// does not even parse as RFC3339, so GetReview silently read it as the zero
// time. Both statements are value clamps, so re-running them is a no-op.
//
// Only a row whose interval actually changes gets a new due date, computed
// from its last attempt. Rewriting due_at for a row that merely drifted in
// ease would shift a perfectly good schedule for no reason.
func repairRunawaySchedules(db *sql.DB) {
	_, _ = db.Exec(`
		UPDATE reviews SET ease = MIN(ease, ?) WHERE ease > ?`,
		srs.MaxEase, srs.MaxEase)

	_, _ = db.Exec(`
		UPDATE reviews
		SET due_at = strftime('%Y-%m-%dT%H:%M:%SZ',
		        COALESCE(
		            (SELECT MAX(finished_at) FROM attempts a
		             WHERE a.problem_id = reviews.problem_id),
		            CURRENT_TIMESTAMP),
		        '+' || ? || ' days'),
		    interval_days = ?
		WHERE interval_days > ?`,
		srs.MaxIntervalDays, srs.MaxIntervalDays, srs.MaxIntervalDays)
}

func (s *Store) Close() error { return s.db.Close() }

type Problem struct {
	ID         int64
	FrontendID int64
	Slug       string
	Title      string
	Difficulty string
	Status     string
	PaidOnly   bool
	Hidden     bool
	Acceptance float64
	Tags       []string
}

// FilterText is what fuzzy search matches against.
func (p Problem) FilterText() string {
	return fmt.Sprintf("%d %s %s %s", p.FrontendID, p.Title, p.Difficulty, strings.Join(p.Tags, " "))
}

func (s *Store) ListProblems(showHidden bool) ([]Problem, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.frontend_id, p.slug, p.title, p.difficulty, p.status,
		       p.paid_only, p.hidden, COALESCE(p.acceptance_rate, 0),
		       COALESCE(group_concat(t.name, ','), '')
		FROM problems p
		LEFT JOIN problem_tags pt ON pt.problem_id = p.id
		LEFT JOIN tags t          ON t.id = pt.tag_id
		WHERE (? OR p.hidden = 0)
		GROUP BY p.id
		ORDER BY p.frontend_id`, showHidden)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Problem
	for rows.Next() {
		var p Problem
		var tags string
		if err := rows.Scan(&p.ID, &p.FrontendID, &p.Slug, &p.Title, &p.Difficulty,
			&p.Status, &p.PaidOnly, &p.Hidden, &p.Acceptance, &tags); err != nil {
			return nil, err
		}
		if tags != "" {
			p.Tags = strings.Split(tags, ",")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type ProblemDetail struct {
	Problem
	Description string
	Hints       []string
	Notes       string
}

func (s *Store) GetDetail(id int64) (*ProblemDetail, error) {
	var d ProblemDetail
	var desc sql.NullString
	var hints string
	err := s.db.QueryRow(`
		SELECT id, frontend_id, slug, title, difficulty, status,
		       COALESCE(description_md, ''), hints_json, notes_md
		FROM problems WHERE id = ?`, id).
		Scan(&d.ID, &d.FrontendID, &d.Slug, &d.Title, &d.Difficulty, &d.Status,
			&desc, &hints, &d.Notes)
	if err != nil {
		return nil, err
	}
	d.Description = desc.String
	_ = json.Unmarshal([]byte(hints), &d.Hints)
	return &d, nil
}

// DetailByFrontendID looks a problem up by the number LeetCode shows, tags
// included — what the tmux helpers (sensei, the switcher) know a problem by.
func (s *Store) DetailByFrontendID(fid int64) (*ProblemDetail, error) {
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM problems WHERE frontend_id = ?`, fid).Scan(&id); err != nil {
		return nil, fmt.Errorf("problem %d: %w", fid, err)
	}
	d, err := s.GetDetail(id)
	if err != nil {
		return nil, err
	}
	var tags string
	err = s.db.QueryRow(`
		SELECT COALESCE(group_concat(t.name, ','), '')
		FROM problem_tags pt JOIN tags t ON t.id = pt.tag_id
		WHERE pt.problem_id = ?`, id).Scan(&tags)
	if err != nil {
		return nil, err
	}
	if tags != "" {
		d.Tags = strings.Split(tags, ",")
	}
	return d, nil
}

// Content is the heavyweight part of a problem, preloaded in bulk at startup
// so opening any problem is a memory lookup instead of a query.
type Content struct {
	Description string
	Hints       []string
	Notes       string
}

func (s *Store) AllContent() (map[int64]Content, error) {
	rows, err := s.db.Query(
		`SELECT id, COALESCE(description_md, ''), hints_json, notes_md FROM problems`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]Content, 4096)
	for rows.Next() {
		var id int64
		var c Content
		var hints string
		if err := rows.Scan(&id, &c.Description, &hints, &c.Notes); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(hints), &c.Hints)
		out[id] = c
	}
	return out, rows.Err()
}

func (s *Store) SetHidden(id int64, hidden bool) error {
	_, err := s.db.Exec(`UPDATE problems SET hidden = ? WHERE id = ?`, hidden, id)
	return err
}

func (s *Store) TestCases(problemID int64) ([]runner.Case, error) {
	rows, err := s.db.Query(`
		SELECT input, expected, is_hidden,
		       COALESCE(args_json, ''), COALESCE(expected_json, '')
		FROM test_cases WHERE problem_id = ? ORDER BY ordinal`, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []runner.Case
	for rows.Next() {
		var c runner.Case
		if err := rows.Scan(&c.Input, &c.Expected, &c.Hidden, &c.ArgsJSON, &c.ExpectedJSON); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// JudgeInfo returns the judging mode and raw signature metadata for a problem.
func (s *Store) JudgeInfo(problemID int64) (mode, metaJSON string, err error) {
	var meta sql.NullString
	err = s.db.QueryRow(
		`SELECT COALESCE(judge_mode, 'raw'), meta_json FROM problems WHERE id = ?`,
		problemID).Scan(&mode, &meta)
	return mode, meta.String, err
}

// CodeInfo is everything needed to produce a problem's editor stub and pick
// how it is judged: LeetCode's own per-language snippets plus the signature
// metadata dojo falls back to when a snippet has not been fetched.
func (s *Store) CodeInfo(problemID int64) (runner.CodeInfo, error) {
	var meta, snips sql.NullString
	var info runner.CodeInfo
	err := s.db.QueryRow(
		`SELECT COALESCE(judge_mode, 'raw'), meta_json, snippets_json, paid_only
		 FROM problems WHERE id = ?`, problemID).
		Scan(&info.JudgeMode, &meta, &snips, &info.PaidOnly)
	if err != nil {
		return info, err
	}
	info.MetaJSON = meta.String
	if snips.Valid && snips.String != "" {
		_ = json.Unmarshal([]byte(snips.String), &info.Snippets)
	}
	return info, nil
}

type Attempt struct {
	ProblemID   int64
	StartedAt   time.Time
	FinishedAt  time.Time
	Language    string
	Passed      bool
	CasesPassed int
	CasesTotal  int
	WallMs      int64
	HintsUsed   int
}

func (s *Store) RecordAttempt(a Attempt) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	iso := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	if _, err := tx.Exec(`
		INSERT INTO attempts (problem_id, started_at, finished_at, language,
		                      passed, cases_passed, cases_total, wall_ms, hints_used)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		a.ProblemID, iso(a.StartedAt), iso(a.FinishedAt), a.Language,
		a.Passed, a.CasesPassed, a.CasesTotal, a.WallMs, a.HintsUsed); err != nil {
		return err
	}

	// Solve credit is granted once per problem — the first green run. Later
	// passes (SRS reviews, re-runs) still log attempts/minutes but never
	// re-increment solved counts or XP.
	var everSolved bool
	if err := tx.QueryRow(`SELECT first_solved_at IS NOT NULL FROM problems
		WHERE id = ?`, a.ProblemID).Scan(&everSolved); err != nil {
		return err
	}

	status := "attempted"
	if a.Passed {
		status = "solved"
	}
	if _, err := tx.Exec(`
		UPDATE problems SET
		  status = CASE WHEN status = 'solved' AND ? = 'attempted' THEN status ELSE ? END,
		  last_attempt_at = ?,
		  first_solved_at = COALESCE(first_solved_at, CASE WHEN ? THEN ? END)
		WHERE id = ?`,
		status, status, iso(a.FinishedAt), a.Passed, iso(a.FinishedAt), a.ProblemID); err != nil {
		return err
	}

	day := a.FinishedAt.Format("2006-01-02")
	solved := 0
	if a.Passed && !everSolved {
		solved = 1
	}
	minutes := int(a.FinishedAt.Sub(a.StartedAt).Minutes())
	if _, err := tx.Exec(`
		INSERT INTO daily_stats (day, solved, attempts, minutes, xp)
		VALUES (?, ?, 1, ?, ?)
		ON CONFLICT(day) DO UPDATE SET
		  solved   = solved + excluded.solved,
		  attempts = attempts + 1,
		  minutes  = minutes + excluded.minutes,
		  xp       = xp + excluded.xp`,
		day, solved, minutes, solved*10); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetReview(problemID int64) (srs.Card, error) {
	c := srs.New()
	var due string
	err := s.db.QueryRow(`
		SELECT ease, interval_days, reps, lapses, due_at
		FROM reviews WHERE problem_id = ?`, problemID).
		Scan(&c.Ease, &c.IntervalDays, &c.Reps, &c.Lapses, &due)
	if err == sql.ErrNoRows {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Due, _ = time.Parse(time.RFC3339, due)
	return c, nil
}

func (s *Store) SaveReview(problemID int64, c srs.Card) error {
	_, err := s.db.Exec(`
		INSERT INTO reviews (problem_id, ease, interval_days, reps, lapses, due_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(problem_id) DO UPDATE SET
		  ease = excluded.ease, interval_days = excluded.interval_days,
		  reps = excluded.reps, lapses = excluded.lapses, due_at = excluded.due_at`,
		problemID, c.Ease, c.IntervalDays, c.Reps, c.Lapses,
		c.Due.UTC().Format(time.RFC3339))
	return err
}

type ProblemList struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	Total       int
	Solved      int
}

// Lists returns every study list with solve progress.
func (s *Store) Lists() ([]ProblemList, error) {
	rows, err := s.db.Query(`
		SELECT l.id, l.slug, l.name, l.description,
		       COUNT(lp.problem_id),
		       COALESCE(SUM(p.status IN ('solved','mastered')), 0)
		FROM lists l
		LEFT JOIN list_problems lp ON lp.list_id = l.id
		LEFT JOIN problems p       ON p.id = lp.problem_id
		GROUP BY l.id ORDER BY l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProblemList
	for rows.Next() {
		var l ProblemList
		if err := rows.Scan(&l.ID, &l.Slug, &l.Name, &l.Description, &l.Total, &l.Solved); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListMembers returns the problem-id set of a list, for filtering.
func (s *Store) ListMembers(listID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(
		`SELECT problem_id FROM list_problems WHERE list_id = ?`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

type TagCount struct {
	Name  string
	Count int
}

// TagCounts returns every tag with its problem count (hidden excluded),
// most common first — powers the tag-browser panel.
func (s *Store) TagCounts() ([]TagCount, error) {
	rows, err := s.db.Query(`
		SELECT t.name, COUNT(*) AS n
		FROM tags t
		JOIN problem_tags pt ON pt.tag_id = t.id
		JOIN problems p      ON p.id = pt.problem_id AND p.hidden = 0
		GROUP BY t.id ORDER BY n DESC, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Name, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

type DashboardStats struct {
	Total      int
	Solved     int
	Attempted  int
	DueReviews int
	StreakDays int
	WeakTags   []string

	// today's daily_stats row (zero if nothing yet)
	TodaySolved, TodayAttempts, TodayMinutes, TodayXP int
	// per-difficulty progress
	DiffTotal, DiffSolved map[string]int
	// attempts per day for the last ActivityDays days, oldest first
	Activity []int
	// actionable queues: reviews that are due, and unsolved picks aimed at
	// the weakest tags (random unsolved when there's no history yet)
	Due       []Problem
	Suggested []Problem

	// lifetime XP across all daily_stats rows — drives the belt rank
	TotalXP int
	// reviews coming due per day: [0] = today (incl. overdue), [6] = +6 days
	Forecast [7]int
	// most recent attempts, newest first
	Recent []AttemptRow
	// personal bests; zero values mean "no history yet"
	Records Records
}

// AttemptRow is one line of the dashboard's recent-activity feed.
type AttemptRow struct {
	FrontendID int64
	Title      string
	Language   string
	Passed     bool
	WallMs     int64
	FinishedAt time.Time
}

// Records are all-time personal bests.
type Records struct {
	FastestMin   float64 // shortest edit-to-green time, minutes
	FastestTitle string
	BestDay      int    // most solves in one day
	BestDayDate  string // YYYY-MM-DD
	LongestRun   int    // longest streak ever, days
	FirstTryPct  int    // % of problems whose first attempt passed (-1 = none)
}

// ActivityDays is the span of the dashboard heat strip (12 weeks).
const ActivityDays = 84

// scanProblemRows collects minimal Problem rows from (id, frontend_id, title,
// difficulty, status) queries — enough for dashboard queues.
func scanProblemRows(rows *sql.Rows) ([]Problem, error) {
	var out []Problem
	for rows.Next() {
		var p Problem
		if err := rows.Scan(&p.ID, &p.FrontendID, &p.Title, &p.Difficulty, &p.Status); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Dashboard() (DashboardStats, error) {
	var st DashboardStats
	err := s.db.QueryRow(`
		SELECT COUNT(*),
		       SUM(status IN ('solved','mastered')),
		       SUM(status = 'attempted')
		FROM problems WHERE hidden = 0`).
		Scan(&st.Total, &nullInt{&st.Solved}, &nullInt{&st.Attempted})
	if err != nil {
		return st, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM reviews WHERE due_at <= ?`, now).
		Scan(&st.DueReviews); err != nil {
		return st, err
	}

	// Streak + heat strip from daily_stats.
	days := map[string]int{}
	rows, err := s.db.Query(`SELECT day, attempts FROM daily_stats`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var d string
		var n int
		_ = rows.Scan(&d, &n)
		days[d] = n
	}
	rows.Close()
	cur := time.Now()
	if days[cur.Format("2006-01-02")] == 0 {
		cur = cur.AddDate(0, 0, -1)
	}
	for days[cur.Format("2006-01-02")] > 0 {
		st.StreakDays++
		cur = cur.AddDate(0, 0, -1)
	}
	st.Activity = make([]int, ActivityDays)
	for i := range st.Activity {
		day := time.Now().AddDate(0, 0, i-ActivityDays+1).Format("2006-01-02")
		st.Activity[i] = days[day]
	}

	today := time.Now().Format("2006-01-02")
	_ = s.db.QueryRow(`SELECT solved, attempts, minutes, xp FROM daily_stats WHERE day = ?`,
		today).Scan(&st.TodaySolved, &st.TodayAttempts, &st.TodayMinutes, &st.TodayXP)

	st.DiffTotal, st.DiffSolved = map[string]int{}, map[string]int{}
	rows, err = s.db.Query(`
		SELECT difficulty, COUNT(*), COALESCE(SUM(status IN ('solved','mastered')), 0)
		FROM problems WHERE hidden = 0 GROUP BY difficulty`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var d string
		var total, solved int
		_ = rows.Scan(&d, &total, &solved)
		st.DiffTotal[d], st.DiffSolved[d] = total, solved
	}
	rows.Close()

	rows, err = s.db.Query(`
		SELECT p.id, p.frontend_id, p.title, p.difficulty, p.status
		FROM reviews r JOIN problems p ON p.id = r.problem_id
		WHERE r.due_at <= ? AND p.hidden = 0
		ORDER BY r.due_at LIMIT 5`, now)
	if err != nil {
		return st, err
	}
	st.Due, err = scanProblemRows(rows)
	rows.Close()
	if err != nil {
		return st, err
	}

	rows, err = s.db.Query(`
		SELECT t.name, AVG(a.passed) AS rate, COUNT(*) AS n
		FROM attempts a
		JOIN problem_tags pt ON pt.problem_id = a.problem_id
		JOIN tags t          ON t.id = pt.tag_id
		GROUP BY t.id HAVING n >= 3
		ORDER BY rate ASC LIMIT 5`)
	if err != nil {
		return st, err
	}
	var weakNames []string
	for rows.Next() {
		var name string
		var rate float64
		var n int
		_ = rows.Scan(&name, &rate, &n)
		st.WeakTags = append(st.WeakTags, fmt.Sprintf("%s (%.0f%%)", name, rate*100))
		weakNames = append(weakNames, name)
	}
	rows.Close()

	// Suggested picks: unsolved problems in the weakest tags; before any
	// history exists, random approachable ones.
	base := `SELECT p.id, p.frontend_id, p.title, p.difficulty, p.status
	         FROM problems p
	         WHERE p.hidden = 0 AND p.status = 'todo'
	           AND p.description_md IS NOT NULL AND p.description_md != ''`
	var args []any
	if len(weakNames) > 0 {
		marks := strings.Repeat("?,", len(weakNames)-1) + "?"
		base += ` AND p.id IN (SELECT pt.problem_id FROM problem_tags pt
		                       JOIN tags t ON t.id = pt.tag_id
		                       WHERE t.name IN (` + marks + `))`
		for _, n := range weakNames {
			args = append(args, n)
		}
	} else {
		base += ` AND p.difficulty IN ('Easy','Medium')`
	}
	rows, err = s.db.Query(base+` ORDER BY RANDOM() LIMIT 5`, args...)
	if err != nil {
		return st, err
	}
	st.Suggested, err = scanProblemRows(rows)
	rows.Close()
	if err != nil {
		return st, err
	}

	// Everything below feeds decorative widgets: failures degrade to empty
	// sections rather than killing the dashboard.
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(xp), 0) FROM daily_stats`).Scan(&st.TotalXP)
	st.Forecast = s.forecast()
	st.Recent, _ = s.RecentAttempts(6)
	st.Records = s.records(days)
	return st, nil
}

// forecast buckets pending reviews into the next 7 local days; anything
// already overdue lands on today.
func (s *Store) forecast() [7]int {
	var f [7]int
	horizon := time.Now().AddDate(0, 0, 7).UTC().Format(time.RFC3339)
	rows, err := s.db.Query(`
		SELECT r.due_at FROM reviews r
		JOIN problems p ON p.id = r.problem_id AND p.hidden = 0
		WHERE r.due_at <= ?`, horizon)
	if err != nil {
		return f
	}
	defer rows.Close()
	today := time.Now()
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		due, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		d := int(due.Local().Sub(midnight).Hours() / 24)
		if d < 0 {
			d = 0
		}
		if d < 7 {
			f[d]++
		}
	}
	return f
}

// RecentAttempts returns the newest n attempts joined with problem info.
func (s *Store) RecentAttempts(n int) ([]AttemptRow, error) {
	rows, err := s.db.Query(`
		SELECT p.frontend_id, p.title, a.language, a.passed, a.wall_ms, a.finished_at
		FROM attempts a JOIN problems p ON p.id = a.problem_id
		ORDER BY a.finished_at DESC, a.id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttemptRow
	for rows.Next() {
		var r AttemptRow
		var raw string
		if err := rows.Scan(&r.FrontendID, &r.Title, &r.Language, &r.Passed, &r.WallMs, &raw); err != nil {
			return out, err
		}
		r.FinishedAt, _ = time.Parse(time.RFC3339, raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

// records computes all-time personal bests. days is the daily_stats
// attempts-per-day map the caller already loaded.
func (s *Store) records(days map[string]int) Records {
	rec := Records{FirstTryPct: -1}
	_ = s.db.QueryRow(`
		SELECT (julianday(a.finished_at) - julianday(a.started_at)) * 1440.0, p.title
		FROM attempts a JOIN problems p ON p.id = a.problem_id
		WHERE a.passed = 1 AND a.finished_at > a.started_at
		ORDER BY 1 ASC LIMIT 1`).Scan(&rec.FastestMin, &rec.FastestTitle)
	_ = s.db.QueryRow(`SELECT day, solved FROM daily_stats
		WHERE solved > 0 ORDER BY solved DESC, day DESC LIMIT 1`).
		Scan(&rec.BestDayDate, &rec.BestDay)

	var firstTry sql.NullFloat64
	_ = s.db.QueryRow(`
		SELECT AVG(passed) FROM attempts a
		WHERE a.id = (SELECT MIN(id) FROM attempts b WHERE b.problem_id = a.problem_id)`).
		Scan(&firstTry)
	if firstTry.Valid {
		rec.FirstTryPct = int(firstTry.Float64*100 + 0.5)
	}

	// longest streak ever: walk the sorted active days
	var active []string
	for d, n := range days {
		if n > 0 {
			active = append(active, d)
		}
	}
	sort.Strings(active)
	run := 0
	var prev time.Time
	for _, d := range active {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if run > 0 && t.Sub(prev) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > rec.LongestRun {
			rec.LongestRun = run
		}
		prev = t
	}
	return rec
}

// nullInt scans a possibly-NULL aggregate into an int.
type nullInt struct{ v *int }

func (n *nullInt) Scan(src any) error {
	switch x := src.(type) {
	case nil:
		*n.v = 0
	case int64:
		*n.v = int(x)
	case float64:
		*n.v = int(x)
	default:
		return fmt.Errorf("nullInt: unsupported type %T", src)
	}
	return nil
}
