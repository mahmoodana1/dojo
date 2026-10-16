package taskvim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixture = `{
  "version": 7,
  "tasks": [
    {"id": 1, "text": "lc 42 before friday", "project": null, "section": null,
     "due": null, "completed": false, "dash_section": "important", "repeat": null},
    {"id": 2, "text": "Two Sum", "project": 5, "section": null, "due": null, "completed": false},
    {"id": 3, "text": "LC 42 every day", "project": null, "section": null, "due": null,
     "completed": false, "repeat": {"every": 1, "unit": "day", "anchor": "2026-10-01"}},
    {"id": 4, "text": "buy milk", "project": null, "section": null, "due": null, "completed": false}
  ],
  "projects": [{"id": 5, "name": "Algorithms", "color": 2, "archived": false}],
  "sections": [],
  "log": [],
  "dashboard": [1, 2, 4],
  "calendar_events": [],
  "calendar_hidden_feed": ["uid-1"],
  "some_future_field": {"keep": true},
  "daily_goal": 2,
  "next_id": 6,
  "next_color": 3
}`

var (
	now  = time.Date(2026, 10, 16, 21, 30, 0, 0, time.Local)
	trap = Problem{FrontendID: 42, Slug: "trapping-rain-water", Title: "Trapping Rain Water", Difficulty: "Hard"}
	two  = Problem{FrontendID: 1, Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy"}
	lru  = Problem{FrontendID: 146, Slug: "lru-cache", Title: "LRU Cache", Difficulty: "Medium"}
)

func setup(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(p, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type file struct {
	Tasks []struct {
		ID        int64  `json:"id"`
		Text      string `json:"text"`
		Project   *int64 `json:"project"`
		Section   *int64 `json:"section"`
		Due       *string
		Completed bool `json:"completed"`
	} `json:"tasks"`
	Projects []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"projects"`
	Sections []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"sections"`
	Log []struct {
		ID          int64  `json:"id"`
		Text        string `json:"text"`
		ProjectName string `json:"project_name"`
		Date        string `json:"date"`
		Time        string `json:"time"`
		Task        *int64 `json:"task"`
	} `json:"log"`
	Dashboard []int64        `json:"dashboard"`
	NextID    int64          `json:"next_id"`
	Future    map[string]any `json:"some_future_field"`
	Hidden    []string       `json:"calendar_hidden_feed"`
}

func read(t *testing.T, p string) file {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return f
}

func TestSolveTicksOffNamingTasks(t *testing.T) {
	p := setup(t)
	res, err := Sync(p, &trap, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Completed != 2 || res.Logged {
		t.Fatalf("result %+v, want the task and the habit ticked off and no extra commit", res)
	}
	f := read(t, p)
	if !f.Tasks[0].Completed || !f.Tasks[2].Completed || f.Tasks[3].Completed {
		t.Fatalf("tasks 1 and 3 should be done, nothing else: %+v", f.Tasks)
	}
	if len(f.Log) != 2 || f.Log[0].Task == nil || *f.Log[0].Task != 1 ||
		f.Log[0].Date != "2026-10-16" || f.Log[0].Time != "21:30" {
		t.Fatalf("log: %+v", f.Log)
	}
	if f.Log[0].ID < 6 || f.NextID <= f.Log[0].ID {
		t.Fatalf("ids: entry %d, next_id %d", f.Log[0].ID, f.NextID)
	}
	if f.Future["keep"] != true || len(f.Hidden) != 1 {
		t.Fatal("fields dojo does not know were dropped")
	}
}

func TestSolveMatchesExactTitleWithCourseOnCommit(t *testing.T) {
	p := setup(t)
	if _, err := Sync(p, &two, nil, now); err != nil {
		t.Fatal(err)
	}
	f := read(t, p)
	if !f.Tasks[1].Completed || f.Log[0].ProjectName != "Algorithms" {
		t.Fatalf("tasks %+v log %+v", f.Tasks, f.Log)
	}
}

func TestUnaskedSolveLogsOncePerDay(t *testing.T) {
	p := setup(t)
	for i := 0; i < 2; i++ {
		res, err := Sync(p, &lru, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if res.Logged != (i == 0) {
			t.Fatalf("run %d: %+v", i, res)
		}
	}
	f := read(t, p)
	if len(f.Log) != 1 || f.Log[0].Text != "LC 146 · LRU Cache" || f.Log[0].ProjectName != Course {
		t.Fatalf("log %+v", f.Log)
	}
	if len(f.Projects) != 2 || f.Projects[1].Name != Course {
		t.Fatalf("projects %+v", f.Projects)
	}
}

func TestReviewsBecomeTasksAndLeaveWhenDone(t *testing.T) {
	p := setup(t)
	due := []Review{
		{Problem: lru, Due: now.Add(-48 * time.Hour)},
		{Problem: trap, Due: now}, // already on the list as task 1
	}
	res, err := Sync(p, nil, due, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Fatalf("%+v", res)
	}
	f := read(t, p)
	last := f.Tasks[len(f.Tasks)-1]
	if last.Text != "LC 146 · LRU Cache" || last.Due == nil || *last.Due != "2026-10-14" {
		t.Fatalf("review task %+v", last)
	}
	if f.Dashboard[len(f.Dashboard)-1] != last.ID {
		t.Fatalf("not linked to the dashboard: %v", f.Dashboard)
	}

	// Same state again: nothing to write.
	if res, _ := Sync(p, nil, due, now); res != (Result{}) {
		t.Fatalf("second sync changed %+v", res)
	}

	// Passing the review completes the task rather than removing it.
	res, err = Sync(p, &lru, due[1:], now)
	if err != nil || res.Completed != 1 || res.Removed != 0 {
		t.Fatalf("%+v %v", res, err)
	}

	// A review that stops being due without a pass is taken back.
	if _, err := Sync(p, nil, []Review{{Problem: two, Due: now}}, now); err != nil {
		t.Fatal(err)
	}
	if res, _ := Sync(p, nil, nil, now); res.Removed != 0 {
		t.Fatalf("task 2 is the user's, not dojo's: %+v", res)
	}
}

func TestMissingFileIsANoop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope", "data.json")
	if _, err := Sync(p, &trap, []Review{{Problem: lru, Due: now}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("created taskvim's file")
	}
}

func TestNames(t *testing.T) {
	for text, want := range map[string]bool{
		"LC 42":                             true,
		"do leetcode #42 tonight":           true,
		"lc-42":                             true,
		"trapping-rain-water":               true,
		"trapping rain water":               true,
		"LC 420":                            false,
		"trapping-rain-water-ii":            false,
		"read about trapping rain water ii": false,
		"lc 4 and lc 42":                    true,
	} {
		if got := names(text, trap); got != want {
			t.Errorf("names(%q) = %v, want %v", text, got, want)
		}
	}
	if strings.Contains(trap.Label(), "#") {
		t.Error("taskvim reads # as a section")
	}
}

const habits = `{
  "tasks": [
    {"id": 1, "text": "Easy Dojo Problem", "completed": false, "period": "2026-10-16",
     "repeat": {"every": 1, "unit": "day", "anchor": "2026-10-01"}},
    {"id": 2, "text": "Mediume Dojo Problem", "completed": true, "period": "2026-10-10",
     "repeat": {"every": 3, "unit": "day", "anchor": "2026-10-16"}},
    {"id": 3, "text": "Do an easy Dojo Problem 1", "completed": false},
    {"id": 4, "text": "Do an easy Dojo Problem 2", "completed": false},
    {"id": 5, "text": "dojo: anything", "completed": true},
    {"id": 6, "text": "fix the dojo pane", "completed": false,
     "repeat": {"every": 1, "unit": "week", "anchor": "2026-10-01"}, "period": "2026-10-12"}
  ],
  "projects": [], "sections": [], "log": [], "dashboard": [1, 2, 3, 4],
  "daily_goal": 2, "next_id": 7, "next_color": 0
}`

func done(t *testing.T, p string) map[int64]bool {
	t.Helper()
	b, _ := os.ReadFile(p)
	var f struct {
		Tasks []struct {
			ID        int64  `json:"id"`
			Completed bool   `json:"completed"`
			Period    string `json:"period"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	out := map[int64]bool{}
	for _, x := range f.Tasks {
		// a habit done in an earlier period is open again
		out[x.ID] = x.Completed && (x.Period == "" || x.Period == "2026-10-16")
	}
	return out
}

func TestSolveTicksHabitsAndOneGenericTask(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(p, []byte(habits), 0o600); err != nil {
		t.Fatal(err)
	}

	// An easy problem: the easy habit and one easy one-off; not the medium
	// habit, and not "fix the dojo pane", which asks for no problem.
	res, err := Sync(p, &two, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	got := done(t, p)
	if !got[1] || got[2] || !got[3] || got[4] || got[6] || res.Completed != 2 || res.Logged {
		t.Fatalf("after easy: %v %+v", got, res)
	}

	// A medium problem: the medium habit, whose period rolled over since it
	// was last done.
	if _, err := Sync(p, &lru, nil, now); err != nil {
		t.Fatal(err)
	}
	if got := done(t, p); !got[2] || got[4] {
		t.Fatalf("after medium: %v", got)
	}

	// Another easy one: the habit is done for today; the second one-off goes.
	if _, err := Sync(p, &two, nil, now); err != nil {
		t.Fatal(err)
	}
	if got := done(t, p); !got[4] {
		t.Fatalf("after second easy: %v", got)
	}
}

func TestPeriodStartMatchesTaskvim(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }
	rep := func(every int, unit, anchor string) obj {
		return obj{"every": json.Number(fmt.Sprint(every)), "unit": unit, "anchor": anchor}
	}
	for _, c := range []struct {
		r           obj
		today, want string
	}{ // cases from taskvim's src/due.rs
		{rep(1, "day", "2026-08-01"), "2026-08-27", "2026-08-27"},
		{rep(3, "day", "2026-08-20"), "2026-08-22", "2026-08-20"},
		{rep(3, "day", "2026-08-20"), "2026-08-23", "2026-08-23"},
		{rep(3, "day", "2026-08-20"), "2026-08-28", "2026-08-26"},
		{rep(1, "week", "2026-08-01"), "2026-08-26", "2026-08-24"},
		{rep(1, "week", "2026-08-01"), "2026-08-31", "2026-08-31"},
		{rep(2, "week", "2026-08-05"), "2026-08-10", "2026-08-03"},
		{rep(2, "week", "2026-08-05"), "2026-08-17", "2026-08-17"},
		{rep(1, "month", "2026-01-15"), "2026-02-15", "2026-02-01"},
		{rep(3, "month", "2026-01-10"), "2026-03-31", "2026-01-01"},
		{rep(3, "month", "2026-01-10"), "2026-07-01", "2026-07-01"},
		{rep(3, "day", "2026-08-20"), "2026-08-18", "2026-08-17"}, // before the anchor
	} {
		if got := periodStart(d(c.today), c.r).Format("2006-01-02"); got != c.want {
			t.Errorf("%v on %s: %s, want %s", c.r, c.today, got, c.want)
		}
	}
}
