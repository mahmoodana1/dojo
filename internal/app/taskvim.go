package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mahmood/dojo/internal/store"
	"github.com/mahmood/dojo/internal/taskvim"
)

// taskvimMsg reports what a sync with taskvim changed.
type taskvimMsg struct {
	res taskvim.Result
	err error
}

// SyncTaskvim pushes a solve (nil for none) and the reviews due by tonight
// into taskvim's data file. See package taskvim.
func SyncTaskvim(st *store.Store, solved *store.Problem) (taskvim.Result, error) {
	now := time.Now()
	y, mo, d := now.Date()
	due, err := st.ReviewsDueBy(time.Date(y, mo, d+1, 0, 0, 0, 0, now.Location()))
	if err != nil {
		return taskvim.Result{}, err
	}
	rs := make([]taskvim.Review, len(due))
	for i, r := range due {
		rs[i] = taskvim.Review{Problem: tvProblem(r.Problem), Due: r.Due}
	}
	var sp *taskvim.Problem
	if solved != nil {
		p := tvProblem(*solved)
		sp = &p
	}
	return taskvim.Sync(taskvim.Path(), sp, rs, now)
}

func tvProblem(p store.Problem) taskvim.Problem {
	return taskvim.Problem{FrontendID: p.FrontendID, Slug: p.Slug, Title: p.Title,
		Difficulty: p.Difficulty}
}

func (m *Model) syncTaskvim() tea.Cmd {
	return func() tea.Msg {
		res, err := SyncTaskvim(m.st, nil)
		return taskvimMsg{res, err}
	}
}

// summary is the status-bar line for a sync; "" when nothing worth saying.
func (msg taskvimMsg) summary() string {
	if msg.err != nil {
		return "taskvim: " + msg.err.Error()
	}
	r := msg.res
	var parts []string
	if r.Completed > 0 {
		parts = append(parts, fmt.Sprintf("ticked off %d task%s", r.Completed, plural(r.Completed)))
	}
	if r.Logged {
		parts = append(parts, "logged a commit")
	}
	if r.Added > 0 {
		parts = append(parts, fmt.Sprintf("%d review%s added", r.Added, plural(r.Added)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "taskvim: " + strings.Join(parts, " · ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
