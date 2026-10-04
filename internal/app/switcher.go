package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/mahmood/dojo/internal/store"
	"github.com/mahmood/dojo/internal/tmux"
)

// The switcher (prefix+s in a dojo session) jumps between solutions: the
// browser, every open solution window, and — below them — solutions on disk
// with no window, which the running dojo opens on request.

type switchItem struct {
	window string // tmux window id; "" = a solution with no window open
	fid    int64
	label  string // what is shown and matched
	diff   string
	status string
	active bool
	home   bool
	edited time.Time // closed solutions: last edit
}

func (it switchItem) match() string {
	if it.home {
		return "browser dojo"
	}
	return it.label
}

type switcher struct {
	items   []switchItem
	shown   []int // indexes into items, post-filter
	cursor  int
	input   textinput.Model
	err     error
	width   int
	height  int
	chosen  *switchItem
	session string
}

// RunSwitcher is the switcher popup's program.
func RunSwitcher(session string, st *store.Store) error {
	items, err := switchItems(session, st)
	if err != nil {
		return err
	}
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "type to filter"
	in.Focus()
	s := &switcher{items: items, input: in, session: session}
	s.filter()
	// start on the window you'd most likely want: the one before this one
	for pos, i := range s.shown {
		if !s.items[i].active && s.items[i].window != "" {
			s.cursor = pos
			break
		}
	}
	res, err := tea.NewProgram(s).Run()
	if err != nil {
		return err
	}
	it := res.(*switcher).chosen
	if it == nil {
		return nil
	}
	if it.window != "" {
		_, err = tmux.Run("select-window", "-t", it.window)
		return err
	}
	if err := RequestEdit(it.fid); err != nil {
		_, _ = tmux.Run("display-message", "switcher: "+err.Error())
		return err
	}
	return nil
}

func switchItems(session string, st *store.Store) ([]switchItem, error) {
	ps, err := st.ListProblems(true)
	if err != nil {
		return nil, err
	}
	byFid := make(map[int64]store.Problem, len(ps))
	for _, p := range ps {
		byFid[p.FrontendID] = p
	}
	ws, err := tmux.Windows(session)
	if err != nil {
		return nil, err
	}
	var items []switchItem
	open := map[int64]bool{}
	for _, w := range ws {
		switch {
		case w.Home:
			items = append(items, switchItem{window: w.ID, home: true, active: w.Active,
				label: "道場 browser"})
		case w.Edit:
			fid := int64(w.Problem)
			open[fid] = true
			p := byFid[fid]
			items = append(items, switchItem{window: w.ID, fid: fid, active: w.Active,
				label: fmt.Sprintf("%d. %s", fid, w.Title), diff: p.Difficulty, status: p.Status})
		}
	}
	// home first, then open solutions in window order
	sort.SliceStable(items, func(a, b int) bool { return items[a].home && !items[b].home })

	entries, _ := os.ReadDir(SolutionsRoot())
	var closed []switchItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		i := strings.IndexByte(e.Name(), '-')
		if i < 0 {
			continue
		}
		fid, err := strconv.ParseInt(e.Name()[:i], 10, 64)
		if err != nil || open[fid] {
			continue
		}
		p, ok := byFid[fid]
		if !ok {
			continue
		}
		var edited time.Time
		if info, err := e.Info(); err == nil {
			edited = lastEdit(e.Name(), info.ModTime())
		}
		closed = append(closed, switchItem{fid: fid, label: fmt.Sprintf("%d. %s", fid, p.Title),
			diff: p.Difficulty, status: p.Status, edited: edited})
	}
	sort.SliceStable(closed, func(a, b int) bool { return closed[a].edited.After(closed[b].edited) })
	return append(items, closed...), nil
}

// lastEdit is the newest modification time of the solution files in a
// problem's folder (the folder's own mtime only moves when files come and go).
func lastEdit(dir string, fallback time.Time) time.Time {
	newest := fallback
	path := filepath.Join(SolutionsRoot(), dir)
	entries, err := os.ReadDir(path)
	if err != nil {
		return newest
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "solution.") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

func (s *switcher) filter() {
	q := strings.TrimSpace(s.input.Value())
	s.shown = s.shown[:0]
	if q == "" {
		for i := range s.items {
			s.shown = append(s.shown, i)
		}
	} else {
		targets := make([]string, len(s.items))
		for i, it := range s.items {
			targets[i] = it.match()
		}
		for _, m := range fuzzy.Find(q, targets) {
			s.shown = append(s.shown, m.Index)
		}
	}
	if s.cursor >= len(s.shown) {
		s.cursor = max(0, len(s.shown)-1)
	}
}

func (s *switcher) Init() tea.Cmd { return textinput.Blink }

func (s *switcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		return s, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return s, tea.Quit
		case "enter":
			if s.cursor < len(s.shown) {
				it := s.items[s.shown[s.cursor]]
				s.chosen = &it
			}
			return s, tea.Quit
		case "down", "ctrl+j", "ctrl+n", "tab":
			if s.cursor < len(s.shown)-1 {
				s.cursor++
			}
			return s, nil
		case "up", "ctrl+k", "ctrl+p", "shift+tab":
			if s.cursor > 0 {
				s.cursor--
			}
			return s, nil
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		s.filter()
		return s, cmd
	}
	return s, nil
}

var (
	swSection = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	swActive  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

func (s *switcher) View() string {
	var b strings.Builder
	b.WriteString(s.input.View() + "\n\n")
	h := max(3, s.height-4)
	top := 0
	if s.cursor >= h {
		top = s.cursor - h + 1
	}
	lastOpen := true
	for pos := top; pos < len(s.shown) && pos < top+h; pos++ {
		it := s.items[s.shown[pos]]
		if lastOpen && it.window == "" {
			if pos > 0 {
				b.WriteString(swSection.Render("  ── not open · ⏎ opens in the editor") + "\n")
			}
			lastOpen = false
		}
		line := s.row(it)
		if pos == s.cursor {
			line = styleCursor.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(s.shown) == 0 {
		b.WriteString(styleDim.Render("  nothing matches") + "\n")
	}
	return b.String()
}

func (s *switcher) row(it switchItem) string {
	mark := "  "
	switch {
	case it.active:
		mark = swActive.Render("▶ ")
	case it.window != "":
		mark = "✎ "
	}
	if it.home {
		mark = map[bool]string{true: swActive.Render("▶ "), false: "  "}[it.active]
		return mark + styleHeader.Render(it.label)
	}
	label := it.label
	if w := s.width - 16; w > 8 {
		if r := []rune(label); len(r) > w {
			label = string(r[:w-1]) + "…"
		}
	}
	d := " "
	if it.diff != "" {
		d = diffStyle(it.diff).Render(it.diff[:1])
	}
	line := fmt.Sprintf("%s%s %s %s", mark, statusIcon(it.status), d, label)
	if it.window == "" && !it.edited.IsZero() {
		line += styleDim.Render("  " + ago(it.edited))
	}
	return line
}
