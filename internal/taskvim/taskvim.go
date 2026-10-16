// Package taskvim connects dojo to taskvim, the task manager: it edits
// taskvim's data.json directly, which taskvim notices (it watches the file's
// mtime) and reloads.
//
// Two things flow across:
//
//   - A green run ticks off, logging a commit just as completing it in
//     taskvim would:
//     · every open task that names the problem ("LC 42", "leetcode 42",
//     "trapping-rain-water", or exactly its title);
//     · every dojo habit still open this period ("Easy Dojo Problem" ~daily);
//     · one generic open task ("do a medium leetcode problem").
//     A task or habit naming a difficulty only counts problems of that
//     difficulty. With nothing to tick, the solve is still logged under the
//     LeetCode course, once per problem per day, so the heatmap and streak
//     see dojo work.
//   - SRS reviews that are due become tasks in the LeetCode course's Reviews
//     section, linked to the dashboard. Passing the review in dojo completes
//     the task; one that is no longer due is taken back off.
//
// taskvim owns the file. Nothing here creates it: without a data.json the
// bridge does nothing. Fields this package does not know survive a write.
package taskvim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Course  = "LeetCode"
	Reviews = "Reviews"
)

// Problem is what the bridge knows a problem by.
type Problem struct {
	FrontendID int64
	Slug       string
	Title      string
	Difficulty string // Easy, Medium, Hard
}

// Label is how dojo writes a problem into taskvim: "LC 42 · Trapping Rain Water".
func (p Problem) Label() string { return fmt.Sprintf("LC %d · %s", p.FrontendID, p.Title) }

// Review is a problem whose SRS review falls due at Due.
type Review struct {
	Problem
	Due time.Time
}

// Result says what a Sync changed, for the status line.
type Result struct {
	Completed int // open tasks ticked off by the solve
	Logged    bool
	Added     int // review tasks created
	Removed   int // review tasks no longer due
	Moved     int // review tasks whose due date changed
}

// Path is taskvim's data file: $TASKVIM_DATA, else
// $XDG_DATA_HOME/taskvim/data.json, else ~/.local/share/taskvim/data.json.
func Path() string {
	if p := os.Getenv("TASKVIM_DATA"); p != "" {
		return p
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "taskvim", "data.json")
}

// Disabled reports whether the bridge is switched off (DOJO_TASKVIM=0).
func Disabled() bool { return os.Getenv("DOJO_TASKVIM") == "0" }

// Sync applies a solve (nil for none) and reconciles the review tasks against
// due, the reviews due by the end of today. It does nothing when taskvim has
// no data file.
func Sync(path string, solved *Problem, due []Review, now time.Time) (Result, error) {
	var res Result
	if Disabled() || path == "" {
		return res, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	d, err := parse(raw)
	if err != nil {
		return res, fmt.Errorf("taskvim data: %w", err)
	}

	if solved != nil {
		res.Completed, res.Logged = d.solve(*solved, now)
	}
	res.Added, res.Removed, res.Moved = d.reconcile(due, solved, now)

	if res == (Result{}) {
		return res, nil
	}
	out, err := d.encode()
	if err != nil {
		return res, err
	}
	return res, writeAtomic(path, out)
}

// --- the file ----------------------------------------------------------------

type obj = map[string]any

// data holds the top level as raw JSON so unknown keys pass through, and
// decodes only the collections the bridge edits.
type data struct {
	top       map[string]json.RawMessage
	tasks     []obj
	projects  []obj
	sections  []obj
	log       []obj
	dashboard []any
	nextID    int64
	nextColor int64
}

func parse(raw []byte) (*data, error) {
	d := &data{}
	if err := json.Unmarshal(raw, &d.top); err != nil {
		return nil, err
	}
	for key, dst := range map[string]any{
		"tasks": &d.tasks, "projects": &d.projects, "sections": &d.sections,
		"log": &d.log, "dashboard": &d.dashboard,
	} {
		v, ok := d.top[key]
		if !ok {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(v))
		dec.UseNumber() // ids stay integers on the way back out
		if err := dec.Decode(dst); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	_ = json.Unmarshal(d.top["next_id"], &d.nextID)
	_ = json.Unmarshal(d.top["next_color"], &d.nextColor)
	// Never trust next_id alone: taskvim repairs a stale one on load, and so
	// must anything else that hands out ids.
	for _, list := range [][]obj{d.tasks, d.projects, d.sections, d.log} {
		for _, o := range list {
			if id := num(o["id"]); id >= d.nextID {
				d.nextID = id + 1
			}
		}
	}
	return d, nil
}

func (d *data) encode() ([]byte, error) {
	set := func(key string, v any) error {
		b, err := marshal(v)
		if err == nil {
			d.top[key] = b
		}
		return err
	}
	for key, v := range map[string]any{
		"tasks": d.tasks, "projects": d.projects, "sections": d.sections,
		"log": d.log, "dashboard": d.dashboard,
		"next_id": d.nextID, "next_color": d.nextColor,
	} {
		if err := set(key, v); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d.top); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func marshal(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// writeAtomic replaces the file in one rename, so taskvim (or Syncthing)
// never reads it half-written.
func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".data.json.dojo-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp.Name(), fi.Mode().Perm())
	}
	return os.Rename(tmp.Name(), path)
}

func num(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func str(v any) string { s, _ := v.(string); return s }

func (d *data) newID() int64 {
	id := d.nextID
	d.nextID++
	return id
}

// --- matching ----------------------------------------------------------------

var lcRef = regexp.MustCompile(`(?i)\b(?:lc|leetcode)\s*[-#.:]?\s*(\d+)\b`)

// names reports whether task text refers to p.
func names(text string, p Problem) bool {
	for _, m := range lcRef.FindAllStringSubmatch(text, -1) {
		if n, _ := strconv.ParseInt(m[1], 10, 64); n == p.FrontendID {
			return true
		}
	}
	low := strings.ToLower(strings.TrimSpace(text))
	if p.Title != "" && low == strings.ToLower(p.Title) {
		return true
	}
	if p.Slug != "" && strings.Contains(p.Slug, "-") {
		re := regexp.MustCompile(`(^|[^a-z0-9-])` + regexp.QuoteMeta(p.Slug) + `($|[^a-z0-9-])`)
		return re.MatchString(low)
	}
	return false
}

var (
	dojoWord = regexp.MustCompile(`(?i)\b(?:dojo|leetcode|lc)\b`)
	probWord = regexp.MustCompile(`(?i)\bproblems?\b`)
	diffWord = regexp.MustCompile(`(?i)\b(easy|med[a-z]*|hard)\b`)
)

// generic reports whether text asks for some dojo problem rather than a
// particular one ("Easy Dojo Problem", "a medium leetcode"), and if it names
// a difficulty, that it is p's. "fix the dojo pane" is not one.
func generic(text string, p Problem) bool {
	if !dojoWord.MatchString(text) || lcRef.MatchString(text) {
		return false
	}
	if !probWord.MatchString(text) && !diffWord.MatchString(text) {
		return false
	}
	for _, m := range diffWord.FindAllStringSubmatch(text, -1) {
		if p.Difficulty != "" && strings.EqualFold(m[1][:3], p.Difficulty[:3]) {
			return true
		}
	}
	return !diffWord.MatchString(text)
}

// open reports whether t can be ticked off: a pending one-off task.
func open(t obj) bool {
	done, _ := t["completed"].(bool)
	return !done && t["repeat"] == nil
}

// habitOpen reports whether t is a habit not yet done in the period holding
// today, returning that period's start.
func habitOpen(t obj, today time.Time) (string, bool) {
	r, ok := t["repeat"].(obj)
	if !ok {
		return "", false
	}
	start := periodStart(today, r).Format("2006-01-02")
	done, _ := t["completed"].(bool)
	return start, !done || str(t["period"]) != start
}

// periodStart is taskvim's due::period_start: the start of the grid cell
// today falls in, the grid being laid from the repeat's anchor in steps of
// `every` days, Monday-weeks or months.
func periodStart(today time.Time, r obj) time.Time {
	day := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	unit := str(r["unit"])
	align := func(t time.Time) time.Time {
		switch unit {
		case "week":
			return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
		case "month":
			return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
		return t
	}
	target := align(day(today))
	base := target
	if a, err := time.Parse("2006-01-02", str(r["anchor"])); err == nil {
		base = align(a)
	}
	every := num(r["every"])
	if every < 1 {
		every = 1
	}
	var between int64
	switch unit {
	case "month":
		between = int64(target.Year()-base.Year())*12 + int64(target.Month()-base.Month())
	case "week":
		between = int64(target.Sub(base).Hours()/24) / 7
	default:
		between = int64(target.Sub(base).Hours() / 24)
	}
	steps := between / every
	if between%every != 0 && between < 0 { // floor, like div_euclid
		steps--
	}
	n := int(steps * every)
	switch unit {
	case "month":
		return base.AddDate(0, n, 0)
	case "week":
		return base.AddDate(0, 0, 7*n)
	}
	return base.AddDate(0, 0, n)
}

// --- courses -----------------------------------------------------------------

func (d *data) project(name string) obj {
	for _, p := range d.projects {
		if str(p["name"]) == name {
			return p
		}
	}
	return nil
}

func (d *data) section(pid int64, name string) obj {
	for _, s := range d.sections {
		if num(s["project"]) == pid && str(s["name"]) == name {
			return s
		}
	}
	return nil
}

// course returns the LeetCode course, creating it (and un-archiving nothing:
// an archived one is still used, just out of sight) on first need.
func (d *data) course() obj {
	if p := d.project(Course); p != nil {
		return p
	}
	p := obj{
		"id": d.newID(), "name": Course,
		"color": d.nextColor % 6, "archived": false,
	}
	d.nextColor++
	d.projects = append(d.projects, p)
	return p
}

func (d *data) reviewSection(pid int64) obj {
	if s := d.section(pid, Reviews); s != nil {
		return s
	}
	s := obj{"id": d.newID(), "project": pid, "name": Reviews}
	d.sections = append(d.sections, s)
	return s
}

// commit logs a completion the way taskvim's toggle_complete does.
func (d *data) commit(text string, project, section obj, task any, now time.Time) {
	e := obj{
		"id": d.newID(), "text": text,
		"project": nil, "project_name": "", "color": 0,
		"section": nil, "section_name": "",
		"date": now.Format("2006-01-02"), "time": now.Format("15:04"),
		"task": task,
	}
	if project != nil {
		e["project"], e["project_name"], e["color"] = project["id"], project["name"], project["color"]
	}
	if section != nil {
		e["section"], e["section_name"] = section["id"], section["name"]
	}
	d.log = append(d.log, e)
}

func (d *data) byID(list []obj, id any) obj {
	if id == nil {
		return nil
	}
	for _, o := range list {
		if num(o["id"]) == num(id) {
			return o
		}
	}
	return nil
}

// --- the two flows -----------------------------------------------------------

func (d *data) solve(p Problem, now time.Time) (completed int, logged bool) {
	tick := func(t obj) {
		t["completed"] = true
		d.commit(str(t["text"]), d.byID(d.projects, t["project"]),
			d.byID(d.sections, t["section"]), t["id"], now)
		completed++
	}
	var loose obj // first generic one-off; only one per solve
	for _, t := range d.tasks {
		text := str(t["text"])
		if period, ok := habitOpen(t, now); ok {
			if names(text, p) || generic(text, p) {
				t["period"], t["due"] = period, nil
				tick(t)
			}
			continue
		}
		if !open(t) {
			continue
		}
		if names(text, p) {
			tick(t)
		} else if loose == nil && generic(text, p) {
			loose = t
		}
	}
	if loose != nil {
		tick(loose)
	}
	if completed > 0 {
		return completed, false
	}

	// No task asked for it: credit the work anyway, once a day.
	c := d.course()
	label, today := p.Label(), now.Format("2006-01-02")
	for _, e := range d.log {
		if num(e["project"]) == num(c["id"]) && str(e["text"]) == label && str(e["date"]) == today {
			return 0, false
		}
	}
	d.commit(label, c, nil, nil, now)
	return 0, true
}

// reconcile makes the Reviews section hold exactly one open task per due
// review. Tasks dojo did not write (anywhere else) are only ever read, to see
// that a review is already on the list.
func (d *data) reconcile(due []Review, solved *Problem, now time.Time) (added, removed, moved int) {
	// The problem just passed has been rescheduled; whatever the caller's due
	// list says, it is not due now.
	var pending []Review
	for _, r := range due {
		if solved == nil || r.FrontendID != solved.FrontendID {
			pending = append(pending, r)
		}
	}

	c := d.project(Course)
	var sec obj
	if c != nil {
		sec = d.section(num(c["id"]), Reviews)
	}

	// Take back review tasks whose problem is no longer due.
	if sec != nil {
		dropped := map[int64]bool{}
		kept := d.tasks[:0]
		for _, t := range d.tasks {
			if open(t) && num(t["section"]) == num(sec["id"]) && !anyNamed(str(t["text"]), pending) {
				if lcRef.MatchString(str(t["text"])) { // written by dojo
					dropped[num(t["id"])] = true
					removed++
					continue
				}
			}
			kept = append(kept, t)
		}
		d.tasks = kept
		if len(dropped) > 0 {
			dash := d.dashboard[:0]
			for _, id := range d.dashboard {
				if !dropped[num(id)] {
					dash = append(dash, id)
				}
			}
			d.dashboard = dash
		}
	}

	for _, r := range pending {
		day := r.Due.Local().Format("2006-01-02")
		if t := d.openNaming(r.Problem); t != nil {
			if sec != nil && num(t["section"]) == num(sec["id"]) && str(t["due"]) != day {
				t["due"] = day
				moved++
			}
			continue
		}
		if c == nil {
			c = d.course()
		}
		if sec == nil {
			sec = d.reviewSection(num(c["id"]))
		}
		id := d.newID()
		d.tasks = append(d.tasks, obj{
			"id": id, "text": r.Label(),
			"project": c["id"], "section": sec["id"],
			"due": day, "start_time": nil, "end_time": nil,
			"completed": false, "dash_section": "general",
			"repeat": nil, "period": nil, "relink": false,
		})
		d.dashboard = append(d.dashboard, json.Number(strconv.FormatInt(id, 10)))
		added++
	}
	return added, removed, moved
}

func anyNamed(text string, rs []Review) bool {
	for _, r := range rs {
		if names(text, r.Problem) {
			return true
		}
	}
	return false
}

func (d *data) openNaming(p Problem) obj {
	for _, t := range d.tasks {
		if open(t) && names(str(t["text"]), p) {
			return t
		}
	}
	return nil
}
