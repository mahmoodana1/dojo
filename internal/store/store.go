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
	return &Store{db: db}, nil
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
	if a.Passed {
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
	return st, nil
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
