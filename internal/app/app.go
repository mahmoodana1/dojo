// Package app is the Bubble Tea root model: routing between screens,
// vim-motion input handling, and view composition. All effects (DB reads,
// judging, editor handoff) run as tea.Cmds so the UI never blocks.
package app

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/mahmood/dojo/internal/runner"
	"github.com/mahmood/dojo/internal/srs"
	"github.com/mahmood/dojo/internal/store"
)

type screen int

const (
	screenDashboard screen = iota
	screenBrowser
)

type panel int

const (
	panelList panel = iota
	panelDetail
)

var (
	styleEasy    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleMedium  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleHard    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleCursor  = lipgloss.NewStyle().Reverse(true)
	stylePass    = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	styleFail    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	styleFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6"))
	styleBlurred = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8"))
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
)

// messages
type problemsMsg []store.Problem
type contentMsg map[int64]store.Content
type previewMsg struct{ seq int }
type statsMsg store.DashboardStats
type listsMsg []store.ProblemList
type membersMsg struct {
	name string
	ids  map[int64]bool
}
type reportMsg runner.Report
type editorDoneMsg struct{ err error }
type tagCountsMsg []store.TagCount
type attemptSavedMsg struct{}
type errMsg struct{ err error }

// tickMsg carries the tick-chain generation so a stale chain (from before a
// screen switch changed the frame rate) dies instead of double-ticking.
type tickMsg struct{ seq int }

// dashItem is one selectable row on the dashboard.
type dashItem struct {
	listIdx int            // -1 = "All problems", >= 0 = index into m.lists
	prob    *store.Problem // non-nil = a jumpable problem (due / suggested)
}

// senseiQuotes greet you on the dashboard — one per visit.
var senseiQuotes = []string{
	"a black belt is a white belt who never quit",
	"slow is smooth, smooth is fast",
	"the best time to plant a tree was twenty years ago; the second best is today",
	"you do not rise to the occasion — you fall to the level of your training",
	"first make it work, then make it right, then make it fast",
	"the master has failed more times than the beginner has tried",
	"one problem a day keeps the recruiter at bay",
	"do not fear the coder who knows 10,000 problems; fear the one who solved one 10,000 times",
	"debugging is like being the detective in a crime movie where you are also the murderer",
	"streaks are built one unglamorous day at a time",
}

type Model struct {
	st   *store.Store
	keys keyMap

	screen screen
	focus  panel
	width  int
	height int

	problems  []store.Problem
	visible   []int // indexes into problems, post-filter
	cursor    int   // index into visible
	filter    textinput.Model
	filtering bool
	pending   string // "d" or "g" for dd / gg

	// structured filters (LeetCode-style browsing)
	diffFilter map[string]bool // Easy/Medium/Hard toggles; empty = all
	tagFilter  map[string]bool // OR-combined, like LeetCode topic filters
	hideSolved bool
	sortMode   int // 0 = number, 1 = acceptance asc, 2 = acceptance desc

	tagPicking bool
	tagCursor  int
	tagCounts  []store.TagCount

	helpOpen bool // Shift+R cheat-sheet modal

	imageOpen bool // v: fullscreen diagram viewer
	imageIdx  int  // which of the problem's diagrams is showing

	detail        *store.ProblemDetail
	revealedHints int
	viewport      viewport.Model

	// preloaded content + render cache: opening a problem never touches the
	// DB, and re-opening never re-renders
	content     map[int64]store.Content
	renderer    *glamour.TermRenderer
	renderCache map[int64]string
	previewSeq  int

	lang      int // index into runner.Order
	report    *runner.Report
	running   bool
	editStart time.Time

	stats  store.DashboardStats
	status string // transient status-bar message
	err    error

	// study lists (dashboard-driven browsing scopes)
	lists       []store.ProblemList
	dashCursor  int            // index into dashItems()
	activeList  string         // name of the list currently scoping the browser
	listMembers map[int64]bool // nil = no list filter
	quote       string         // sensei quote for this dashboard visit

	// animation state (see anim.go)
	dashEnteredAt  time.Time // start of entrance animation
	quoteChangedAt time.Time // start of quote fade-in
	confettiAt     time.Time // zero = no celebration in flight
	tickSeq        int       // generation guard for the tick chain
	statsLoaded    bool      // suppresses confetti on the very first stats load
}

func New(st *store.Store) *Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "fuzzy title, or d:hard t:graph s:todo"
	return &Model{st: st, keys: defaultKeyMap(), filter: ti, screen: screenDashboard,
		diffFilter: map[string]bool{}, tagFilter: map[string]bool{},
		renderCache:   map[int64]string{},
		quote:         senseiQuotes[rand.Intn(len(senseiQuotes))],
		dashEnteredAt: time.Now(), quoteChangedAt: time.Now()}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadProblems(), m.loadContent(), m.loadStats(), m.loadLists(), m.tickCmd())
}

// tickCmd keeps time flowing: ~12 FPS while the animated dashboard is
// visible, a lazy 30 s heartbeat everywhere else.
func (m *Model) tickCmd() tea.Cmd {
	seq := m.tickSeq
	d := idleFrame
	if m.screen == screenDashboard {
		d = animFrame
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{seq} })
}

// dashItems flattens every selectable dashboard row: browsing scopes first,
// then the action queues (due reviews, suggested picks).
func (m *Model) dashItems() []dashItem {
	items := []dashItem{{listIdx: -1}}
	for i := range m.lists {
		items = append(items, dashItem{listIdx: i})
	}
	for i := range m.stats.Due {
		items = append(items, dashItem{listIdx: -2, prob: &m.stats.Due[i]})
	}
	for i := range m.stats.Suggested {
		items = append(items, dashItem{listIdx: -2, prob: &m.stats.Suggested[i]})
	}
	return items
}

func (m *Model) clampDash() {
	if n := len(m.dashItems()); m.dashCursor >= n {
		m.dashCursor = n - 1
	}
	if m.dashCursor < 0 {
		m.dashCursor = 0
	}
}

// jumpTo lands the browser on one specific problem, relaxing any filters that
// would hide it.
func (m *Model) jumpTo(id int64) tea.Cmd {
	m.screen = screenBrowser
	m.focus = panelList
	find := func() int {
		for pos, i := range m.visible {
			if m.problems[i].ID == id {
				return pos
			}
		}
		return -1
	}
	pos := find()
	if pos < 0 {
		m.activeList, m.listMembers = "", nil
		m.diffFilter, m.tagFilter = map[string]bool{}, map[string]bool{}
		m.hideSolved, m.filtering = false, false
		m.filter.SetValue("")
		m.applyFilter()
		pos = find()
	}
	if pos >= 0 {
		m.cursor = pos
	}
	if p := m.selected(); p != nil {
		m.setDetail(p)
	}
	return nil
}

func (m *Model) loadLists() tea.Cmd {
	return func() tea.Msg {
		ls, err := m.st.Lists()
		if err != nil {
			return errMsg{err}
		}
		return listsMsg(ls)
	}
}

// --- commands ---------------------------------------------------------------

func (m *Model) loadProblems() tea.Cmd {
	return func() tea.Msg {
		ps, err := m.st.ListProblems(false)
		if err != nil {
			return errMsg{err}
		}
		return problemsMsg(ps)
	}
}

func (m *Model) loadStats() tea.Cmd {
	return func() tea.Msg {
		st, err := m.st.Dashboard()
		if err != nil {
			return errMsg{err}
		}
		return statsMsg(st)
	}
}

func (m *Model) loadContent() tea.Cmd {
	return func() tea.Msg {
		c, err := m.st.AllContent()
		if err != nil {
			return errMsg{err}
		}
		return contentMsg(c)
	}
}

// setDetail opens a problem instantly from the preloaded content map.
func (m *Model) setDetail(p *store.Problem) {
	c := m.content[p.ID]
	m.detail = &store.ProblemDetail{Problem: *p, Description: c.Description,
		Hints: c.Hints, Notes: c.Notes}
	m.revealedHints = 0
	m.report = nil
	m.renderDetail()
	m.viewport.GotoTop()
}

// schedulePreview debounces cursor movement: the detail pane follows the
// cursor once it rests for 120ms, so holding j/k stays smooth.
func (m *Model) schedulePreview() tea.Cmd {
	m.previewSeq++
	seq := m.previewSeq
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return previewMsg{seq} })
}

func solutionsRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "dojo-solutions")
}

func (m *Model) openEditor() tea.Cmd {
	d := m.detail
	if d == nil {
		return nil
	}
	lang := runner.Registry[runner.Order[m.lang]]
	path, err := runner.EnsureSolutionFile(solutionsRoot(), runner.SolutionMeta{
		FrontendID: d.FrontendID, Title: d.Title, Slug: d.Slug, Difficulty: d.Difficulty,
	}, lang, m.typedMeta(d.ID))
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	m.editStart = time.Now()

	// Inside tmux: open the editor in its own window so the problem page
	// stays put — flip back to it anytime. `tmux wait-for` tells us when the
	// editor closes so the auto-judge still fires.
	if os.Getenv("TMUX") != "" {
		signal := fmt.Sprintf("dojo-edit-%d", time.Now().UnixNano())
		winName := fmt.Sprintf("✎%d", d.FrontendID)
		shellCmd := fmt.Sprintf("%s %q; tmux wait-for -S %s", editor, path, signal)
		if err := exec.Command("tmux", "new-window", "-n", winName, shellCmd).Run(); err != nil {
			return func() tea.Msg { return errMsg{fmt.Errorf("tmux new-window: %w", err)} }
		}
		m.status = fmt.Sprintf("editing in tmux window %s — judge runs when it closes", winName)
		return func() tea.Msg {
			_ = exec.Command("tmux", "wait-for", signal).Run()
			return editorDoneMsg{nil}
		}
	}

	// Outside tmux: classic suspend-and-resume handoff.
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// typedMeta returns the parsed signature when a problem is typed-judgeable.
func (m *Model) typedMeta(problemID int64) *runner.Meta {
	mode, metaJSON, err := m.st.JudgeInfo(problemID)
	if err != nil || mode != "typed" {
		return nil
	}
	tm, err := runner.ParseMeta(metaJSON)
	if err != nil {
		return nil
	}
	return tm
}

func (m *Model) runTests() tea.Cmd {
	d := m.detail
	if d == nil {
		return nil
	}
	lang := runner.Registry[runner.Order[m.lang]]
	method := ""
	if tm := m.typedMeta(d.ID); tm != nil {
		method = tm.Name
	}
	m.running = true
	return func() tea.Msg {
		cases, err := m.st.TestCases(d.ID)
		if err != nil {
			return errMsg{err}
		}
		if len(cases) == 0 {
			return errMsg{fmt.Errorf("no test cases for %s (add some, or re-run ingest)", d.Slug)}
		}
		src := filepath.Join(solutionsRoot(),
			fmt.Sprintf("%04d-%s", d.FrontendID, d.Slug), "solution"+lang.Ext)
		if _, err := os.Stat(src); err != nil {
			return errMsg{fmt.Errorf("no solution file yet — press i to start one")}
		}
		rep := runner.RunAll(context.Background(), lang, src, cases, method)
		return reportMsg(rep)
	}
}

func (m *Model) saveAttempt(rep runner.Report) tea.Cmd {
	d := m.detail
	if d == nil {
		return nil
	}
	start := m.editStart
	if start.IsZero() {
		start = time.Now()
	}
	hints := m.revealedHints
	lang := runner.Order[m.lang]
	return func() tea.Msg {
		now := time.Now()
		passed := rep.Total > 0 && rep.Passed == rep.Total && rep.CompileErr == ""
		_ = m.st.RecordAttempt(store.Attempt{
			ProblemID: d.ID, StartedAt: start, FinishedAt: now, Language: lang,
			Passed: passed, CasesPassed: rep.Passed, CasesTotal: rep.Total,
			WallMs: rep.TotalMs, HintsUsed: hints,
		})
		card, err := m.st.GetReview(d.ID)
		if err == nil {
			q := srs.Quality(passed, hints, now.Sub(start).Minutes(), 35)
			_ = m.st.SaveReview(d.ID, srs.Review(card, q, now))
		}
		return attemptSavedMsg{}
	}
}

// --- update -----------------------------------------------------------------

// matchDiff: "e"/"ea"/"easy" match Easy, "m…" Medium, "h…" Hard.
func matchDiff(difficulty, q string) bool {
	return q != "" && strings.HasPrefix(strings.ToLower(difficulty), strings.ToLower(q))
}

func hasAnyTag(tags []string, wanted map[string]bool) bool {
	for _, t := range tags {
		if wanted[t] {
			return true
		}
	}
	return false
}

func keep(cand []int, ps []store.Problem, pred func(store.Problem) bool) []int {
	out := cand[:0]
	for _, i := range cand {
		if pred(ps[i]) {
			out = append(out, i)
		}
	}
	return out
}

// applyFilter runs the full pipeline: structural filters (difficulty toggles,
// tag picker, unsolved-only) -> `/` query tokens (d: t: s: prefixes, rest is
// fuzzy) -> sort. Fuzzy queries keep relevance order; otherwise sortMode wins.
func (m *Model) applyFilter() {
	cand := make([]int, 0, len(m.problems))
	for i, p := range m.problems {
		if m.listMembers != nil && !m.listMembers[p.ID] {
			continue
		}
		if len(m.diffFilter) > 0 && !m.diffFilter[p.Difficulty] {
			continue
		}
		if m.hideSolved && (p.Status == "solved" || p.Status == "mastered") {
			continue
		}
		if len(m.tagFilter) > 0 && !hasAnyTag(p.Tags, m.tagFilter) {
			continue
		}
		cand = append(cand, i)
	}

	var free []string
	for _, tok := range strings.Fields(strings.TrimSpace(m.filter.Value())) {
		switch {
		case strings.HasPrefix(tok, "d:"):
			q := tok[2:]
			cand = keep(cand, m.problems, func(p store.Problem) bool { return matchDiff(p.Difficulty, q) })
		case strings.HasPrefix(tok, "t:"):
			q := strings.ToLower(tok[2:])
			cand = keep(cand, m.problems, func(p store.Problem) bool {
				for _, t := range p.Tags {
					if strings.Contains(strings.ToLower(t), q) {
						return true
					}
				}
				return false
			})
		case strings.HasPrefix(tok, "s:"):
			q := strings.ToLower(tok[2:])
			cand = keep(cand, m.problems, func(p store.Problem) bool { return strings.HasPrefix(p.Status, q) })
		default:
			free = append(free, tok)
		}
	}

	if len(free) > 0 {
		targets := make([]string, len(cand))
		for j, i := range cand {
			targets[j] = m.problems[i].FilterText()
		}
		ranked := make([]int, 0, len(cand))
		for _, match := range fuzzy.Find(strings.Join(free, " "), targets) {
			ranked = append(ranked, cand[match.Index])
		}
		cand = ranked
	} else {
		switch m.sortMode {
		case 1:
			sort.SliceStable(cand, func(a, b int) bool {
				return m.problems[cand[a]].Acceptance < m.problems[cand[b]].Acceptance
			})
		case 2:
			sort.SliceStable(cand, func(a, b int) bool {
				return m.problems[cand[a]].Acceptance > m.problems[cand[b]].Acceptance
			})
		}
	}

	m.visible = cand
	if m.cursor >= len(m.visible) {
		m.cursor = max(0, len(m.visible)-1)
	}
}

// filterSummary is the status-bar readout of every active filter.
func (m *Model) filterSummary() string {
	var parts []string
	if m.activeList != "" {
		parts = append(parts, m.activeList+" (esc to leave)")
	}
	for _, d := range []string{"Easy", "Medium", "Hard"} {
		if m.diffFilter[d] {
			parts = append(parts, diffStyle(d).Render(d[:1]))
		}
	}
	if n := len(m.tagFilter); n > 0 {
		names := make([]string, 0, n)
		for t := range m.tagFilter {
			names = append(names, t)
		}
		sort.Strings(names)
		if len(names) > 2 {
			names = append(names[:2], fmt.Sprintf("+%d", n-2))
		}
		parts = append(parts, strings.Join(names, ","))
	}
	if m.hideSolved {
		parts = append(parts, "unsolved")
	}
	switch m.sortMode {
	case 1:
		parts = append(parts, "acc↑")
	case 2:
		parts = append(parts, "acc↓")
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func (m *Model) selected() *store.Problem {
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return &m.problems[m.visible[m.cursor]]
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.viewport = viewport.New(m.detailWidth()-2, m.height-4)
		// one renderer per width; rendered markdown is width-dependent
		m.renderer, _ = glamour.NewTermRenderer(glamour.WithAutoStyle(),
			glamour.WithWordWrap(m.detailWidth()-6))
		m.renderCache = map[int64]string{}
		m.renderDetail()
		return m, nil

	case problemsMsg:
		m.problems = msg
		m.applyFilter()
		return m, nil

	case statsMsg:
		ns := store.DashboardStats(msg)
		if m.statsLoaded && ns.TodaySolved >= dailyGoal && m.stats.TodaySolved < dailyGoal {
			m.confettiAt = time.Now() // just crossed the goal line 🎉
		}
		m.statsLoaded = true
		m.stats = ns
		m.clampDash()
		return m, nil

	case listsMsg:
		m.lists = msg
		m.clampDash()
		return m, nil

	case tickMsg:
		if msg.seq != m.tickSeq {
			return m, nil // stale chain from before a screen switch
		}
		if m.screen == screenDashboard && time.Since(m.quoteChangedAt) > quotePeriod {
			m.quote = senseiQuotes[rand.Intn(len(senseiQuotes))]
			m.quoteChangedAt = time.Now()
		}
		return m, m.tickCmd()

	case membersMsg:
		m.activeList = msg.name
		m.listMembers = msg.ids
		m.cursor = 0
		m.applyFilter()
		m.screen = screenBrowser
		return m, m.schedulePreview()

	case tagCountsMsg:
		m.tagCounts = msg
		return m, nil

	case contentMsg:
		m.content = msg
		return m, nil

	case previewMsg:
		if msg.seq == m.previewSeq && m.screen == screenBrowser && !m.filtering {
			if p := m.selected(); p != nil && (m.detail == nil || m.detail.ID != p.ID) {
				m.setDetail(p)
			}
		}
		return m, nil

	case imageViewerDoneMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, nil

	case editorDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.status = "editor closed — judging…"
		return m, m.runTests()

	case reportMsg:
		m.running = false
		rep := runner.Report(msg)
		m.report = &rep
		m.status = ""
		m.renderDetail()
		return m, m.saveAttempt(rep)

	case attemptSavedMsg:
		// refresh every dashboard surface: problem statuses, stats/queues,
		// and study-list progress all reflect the attempt immediately
		return m, tea.Batch(m.loadProblems(), m.loadStats(), m.loadLists())

	case errMsg:
		m.running = false
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil

	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filter.SetValue("")
			m.applyFilter()
		case "enter":
			m.filtering = false
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.applyFilter()
			return m, cmd
		}
		return m, nil
	}

	k := msg.String()

	// help modal: any toggle key or esc closes, everything else is swallowed
	if m.helpOpen {
		switch k {
		case "esc", "q", "R", "?":
			m.helpOpen = false
		}
		return m, nil
	}
	if k == "R" || k == "?" {
		m.helpOpen = true
		return m, nil
	}

	// fullscreen diagram viewer captures all keys while open
	if m.imageOpen {
		n := len(localImages(m.detail.Slug))
		switch k {
		case "esc", "q", "v":
			m.imageOpen = false
		case "n", "l", "right", "j":
			if m.imageIdx < n-1 {
				m.imageIdx++
			}
		case "p", "h", "left", "k":
			if m.imageIdx > 0 {
				m.imageIdx--
			}
		}
		return m, nil
	}
	if k == "v" && m.screen == screenBrowser {
		if p := m.selected(); p != nil && (m.detail == nil || m.detail.ID != p.ID) {
			m.setDetail(p)
		}
		if m.detail == nil {
			return m, nil
		}
		// real pixels via kitty when available, half-block viewer otherwise
		if kittyCapable() {
			return m, m.openImageViewer()
		}
		m.imageOpen, m.imageIdx = true, 0
		return m, nil
	}

	// tag-browser modal captures all keys while open
	if m.tagPicking {
		switch k {
		case "esc", "T", "q":
			m.tagPicking = false
		case "j", "down":
			if m.tagCursor < len(m.tagCounts)-1 {
				m.tagCursor++
			}
		case "k", "up":
			if m.tagCursor > 0 {
				m.tagCursor--
			}
		case "enter", " ":
			if m.tagCursor < len(m.tagCounts) {
				name := m.tagCounts[m.tagCursor].Name
				if m.tagFilter[name] {
					delete(m.tagFilter, name)
				} else {
					m.tagFilter[name] = true
				}
				m.applyFilter()
			}
		case "c":
			m.tagFilter = map[string]bool{}
			m.applyFilter()
		}
		return m, nil
	}

	// vim two-key sequences: dd hides, gg jumps to top
	if m.pending != "" {
		pending := m.pending
		m.pending = ""
		switch {
		case pending == "d" && k == "d":
			if p := m.selected(); p != nil {
				id := p.ID
				m.status = fmt.Sprintf("hidden %q — it won't be suggested again", p.Title)
				return m, tea.Sequence(
					func() tea.Msg { _ = m.st.SetHidden(id, true); return nil },
					m.loadProblems(),
				)
			}
		case pending == "g" && k == "g":
			if m.screen == screenDashboard {
				m.dashCursor = 0
				return m, nil
			}
			m.cursor = 0
			return m, m.schedulePreview()
		}
		// fall through: unmatched sequence, treat k normally
	}
	if k == "d" || k == "g" {
		m.pending = k
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Dashboard):
		if m.screen == screenDashboard {
			m.screen = screenBrowser
		} else {
			m.screen = screenDashboard
			m.quote = senseiQuotes[rand.Intn(len(senseiQuotes))]
			m.quoteChangedAt = time.Now()
			m.dashEnteredAt = time.Now() // replay the entrance animation
		}
		m.tickSeq++ // retire the old tick chain, start one at the right rate
		return m, tea.Batch(m.loadStats(), m.tickCmd())
	}

	if m.screen == screenDashboard {
		items := m.dashItems()
		switch {
		case key.Matches(msg, m.keys.Down):
			if m.dashCursor < len(items)-1 {
				m.dashCursor++
			}
		case key.Matches(msg, m.keys.Bottom):
			m.dashCursor = len(items) - 1
		case key.Matches(msg, m.keys.Select):
			it := items[m.dashCursor]
			switch {
			case it.prob != nil: // due review / suggested pick: jump straight in
				return m, m.jumpTo(it.prob.ID)
			case it.listIdx < 0: // "All problems"
				m.activeList = ""
				m.listMembers = nil
				m.applyFilter()
				m.screen = screenBrowser
				return m, m.schedulePreview()
			default:
				l := m.lists[it.listIdx]
				return m, func() tea.Msg {
					ids, err := m.st.ListMembers(l.ID)
					if err != nil {
						return errMsg{err}
					}
					return membersMsg{name: l.Name, ids: ids}
				}
			}
		case key.Matches(msg, m.keys.Up):
			if m.dashCursor > 0 {
				m.dashCursor--
			}
		}
		return m, nil
	}

	// browser screen: structured-filter keys first
	switch k {
	case "1", "2", "3":
		d := map[string]string{"1": "Easy", "2": "Medium", "3": "Hard"}[k]
		if m.diffFilter[d] {
			delete(m.diffFilter, d)
		} else {
			m.diffFilter[d] = true
		}
		m.applyFilter()
		return m, nil
	case "u":
		m.hideSolved = !m.hideSolved
		m.applyFilter()
		return m, nil
	case "o":
		m.sortMode = (m.sortMode + 1) % 3
		m.applyFilter()
		return m, nil
	case "T":
		m.tagPicking = true
		if m.tagCounts == nil {
			return m, func() tea.Msg {
				tc, err := m.st.TagCounts()
				if err != nil {
					return errMsg{err}
				}
				return tagCountsMsg(tc)
			}
		}
		return m, nil
	case "p": // LeetCode "Pick One": random problem honouring active filters
		pool := make([]int, 0, len(m.visible))
		for pos, i := range m.visible {
			if s := m.problems[i].Status; s == "todo" || s == "attempted" {
				pool = append(pool, pos)
			}
		}
		if len(pool) == 0 && len(m.visible) > 0 {
			pool = append(pool, rand.Intn(len(m.visible)))
		}
		if len(pool) > 0 {
			m.cursor = pool[rand.Intn(len(pool))]
			m.focus = panelList
			m.setDetail(&m.problems[m.visible[m.cursor]])
		}
		return m, nil
	case "s": // sync: pull in freshly-ingested problems without restarting
		m.status = "syncing from database…"
		return m, tea.Batch(m.loadProblems(), m.loadContent(), m.loadStats(), m.loadLists())
	case "esc": // drop back to the full problem set
		if m.activeList != "" {
			m.activeList = ""
			m.listMembers = nil
			m.applyFilter()
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink

	case key.Matches(msg, m.keys.Left):
		m.focus = panelList

	case key.Matches(msg, m.keys.Right):
		m.focus = panelDetail

	case key.Matches(msg, m.keys.Down):
		if m.focus == panelDetail {
			m.viewport.LineDown(1)
		} else if m.cursor < len(m.visible)-1 {
			m.cursor++
			return m, m.schedulePreview()
		}

	case key.Matches(msg, m.keys.Up):
		if m.focus == panelDetail {
			m.viewport.LineUp(1)
		} else if m.cursor > 0 {
			m.cursor--
			return m, m.schedulePreview()
		}

	case key.Matches(msg, m.keys.HalfDown):
		m.viewport.HalfViewDown()

	case key.Matches(msg, m.keys.HalfUp):
		m.viewport.HalfViewUp()

	case key.Matches(msg, m.keys.Bottom):
		if m.focus == panelList {
			m.cursor = max(0, len(m.visible)-1)
			return m, m.schedulePreview()
		}
		m.viewport.GotoBottom()

	case key.Matches(msg, m.keys.Select):
		if p := m.selected(); p != nil {
			m.setDetail(p)
			m.focus = panelDetail
		}

	case key.Matches(msg, m.keys.Edit):
		if p := m.selected(); p != nil && (m.detail == nil || m.detail.ID != p.ID) {
			m.setDetail(p)
		}
		if m.detail != nil {
			return m, m.openEditor()
		}

	case key.Matches(msg, m.keys.Run):
		return m, m.runTests()

	case key.Matches(msg, m.keys.Hint):
		if m.detail != nil && m.revealedHints < len(m.detail.Hints) {
			m.revealedHints++
			m.renderDetail()
		}

	case key.Matches(msg, m.keys.CycleLang):
		m.lang = (m.lang + 1) % len(runner.Order)
		m.status = "language: " + runner.Order[m.lang]
	}
	return m, nil
}

// --- view -------------------------------------------------------------------

func (m *Model) listWidth() int   { return max(30, m.width*38/100) }
func (m *Model) detailWidth() int { return max(20, m.width-m.listWidth()) }

func (m *Model) renderDetail() {
	if m.detail == nil || m.width == 0 {
		return
	}
	d := m.detail

	out, cached := m.renderCache[d.ID]
	if !cached || m.revealedHints > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "# %d. %s\n\n**%s** · %s\n\n", d.FrontendID, d.Title,
			d.Difficulty, strings.Join(d.Tags, ", "))
		switch {
		case d.Description != "":
			b.WriteString(d.Description)
		case d.PaidOnly:
			b.WriteString("_Premium-only problem — no description available._\n")
		default:
			b.WriteString("_Description not fetched yet — ingest may still be running. Press s to sync._\n")
		}
		if m.revealedHints > 0 {
			b.WriteString("\n\n---\n\n## Hints\n\n")
			for i := 0; i < m.revealedHints; i++ {
				fmt.Fprintf(&b, "%d. %s\n", i+1, d.Hints[i])
			}
		}
		if n := len(d.Hints) - m.revealedHints; n > 0 {
			fmt.Fprintf(&b, "\n_%d hint(s) available — press H to reveal (counts toward SRS grading)._\n", n)
		}
		out = tokenizeImages(b.String(), d.Slug)
		if m.renderer != nil {
			if rendered, err := m.renderer.Render(out); err == nil {
				out = rendered
			}
		}
		out = expandImageTokens(out, d.Slug, m.detailWidth()-8)
		if m.revealedHints == 0 && d.Description != "" {
			m.renderCache[d.ID] = out
		}
	}

	if m.report != nil {
		out += "\n" + m.renderReport()
	}
	m.viewport.Width = m.detailWidth() - 2
	m.viewport.Height = m.height - 4
	m.viewport.SetContent(out)
}

func (m *Model) renderReport() string {
	rep := m.report
	var b strings.Builder
	if rep.CompileErr != "" {
		b.WriteString(styleFail.Render("COMPILE ERROR") + "\n" + rep.CompileErr + "\n")
		return b.String()
	}
	verdict := stylePass.Render("PASS")
	if rep.Passed != rep.Total {
		verdict = styleFail.Render("FAIL")
	}
	fmt.Fprintf(&b, "%s  %d/%d cases  ·  %d ms total\n", verdict, rep.Passed, rep.Total, rep.TotalMs)
	for _, r := range rep.Results {
		mark := stylePass.Render("✓")
		if !r.Pass {
			mark = styleFail.Render("✗")
		}
		fmt.Fprintf(&b, " %s case %d (%d ms)", mark, r.Index, r.Ms)
		if r.TimedOut {
			b.WriteString("  TIMEOUT")
		}
		b.WriteString("\n")
		if !r.Pass && !r.Hidden && !r.TimedOut {
			fmt.Fprintf(&b, "   expected: %s\n   got:      %s\n",
				firstLine(r.Expected), firstLine(r.Got))
			if r.Stderr != "" {
				fmt.Fprintf(&b, "   stderr:   %s\n", firstLine(r.Stderr))
			}
		}
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func statusIcon(status string) string {
	switch status {
	case "solved", "mastered":
		return stylePass.Render("●")
	case "attempted":
		return styleMedium.Render("◐")
	default:
		return styleDim.Render("○")
	}
}

func diffStyle(d string) lipgloss.Style {
	switch d {
	case "Easy":
		return styleEasy
	case "Medium":
		return styleMedium
	default:
		return styleHard
	}
}

func (m *Model) viewList() string {
	h := m.height - 4
	if h < 1 {
		h = 1
	}
	// keep cursor in window
	top := 0
	if m.cursor >= h {
		top = m.cursor - h + 1
	}
	var rows []string
	for i := top; i < len(m.visible) && i < top+h; i++ {
		p := m.problems[m.visible[i]]
		title := p.Title
		maxTitle := m.listWidth() - 16
		if r := []rune(title); len(r) > maxTitle && maxTitle > 1 {
			title = string(r[:maxTitle-1]) + "…"
		}
		line := fmt.Sprintf("%s %4d %s %s", statusIcon(p.Status), p.FrontendID,
			diffStyle(p.Difficulty).Render(fmt.Sprintf("%-1.1s", p.Difficulty)), title)
		if i == m.cursor && m.focus == panelList {
			line = styleCursor.Render(line)
		}
		rows = append(rows, line)
	}
	if len(rows) == 0 {
		rows = append(rows, styleDim.Render("no problems — run scripts/ingest.py"))
	}
	body := strings.Join(rows, "\n")
	style := styleBlurred
	if m.focus == panelList {
		style = styleFocused
	}
	return style.Width(m.listWidth() - 2).Height(m.height - 3).Render(body)
}

func (m *Model) viewDetail() string {
	style := styleBlurred
	if m.focus == panelDetail {
		style = styleFocused
	}
	content := m.viewport.View()
	if m.detail == nil {
		content = styleDim.Render("\n  ⏎ open a problem · i start coding\n  save & close your editor to submit — the judge runs automatically\n  R for all keys")
	}
	if m.running {
		content = styleMedium.Render("  judging…") + "\n" + content
	}
	return style.Width(m.detailWidth() - 2).Height(m.height - 3).Render(content)
}

func (m *Model) viewTagPicker() string {
	h := max(5, m.height-8)
	top := 0
	if m.tagCursor >= h {
		top = m.tagCursor - h + 1
	}
	var rows []string
	rows = append(rows, styleHeader.Render("topics")+styleDim.Render("  ⏎/space toggle · c clear · esc close"))
	for i := top; i < len(m.tagCounts) && i < top+h; i++ {
		tc := m.tagCounts[i]
		mark := styleDim.Render("[ ]")
		if m.tagFilter[tc.Name] {
			mark = stylePass.Render("[x]")
		}
		line := fmt.Sprintf(" %s %-28s %4d", mark, tc.Name, tc.Count)
		if i == m.tagCursor {
			line = styleCursor.Render(line)
		}
		rows = append(rows, line)
	}
	box := styleFocused.Padding(0, 1).Render(strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) viewHelp() string {
	type row struct{ k, d string }
	sections := []struct {
		title string
		rows  []row
	}{
		{"submit a solution", []row{
			{"1.", "⏎ open a problem, then i to start coding"},
			{"2.", "implement the stub — python gets the real LeetCode"},
			{"", "   signature; the judge feeds typed args automatically"},
			{"3.", "save & close the editor (:wq) — the judge runs by itself"},
			{"", "   in tmux: closing the ✎ window is the submit"},
			{"4.", "verdict, per-case ✓/✗ and timings appear in the panel"},
			{"5.", "failed? i to reopen your code, :wq to resubmit (or r)"},
		}},
		{"navigate", []row{
			{"j / k", "move (list, or scroll description)"},
			{"h / l", "focus list / description panel"},
			{"gg / G", "jump to top / bottom"},
			{"ctrl+d / ctrl+u", "half-page scroll"},
			{"⏎", "open problem (dashboard: open list / jump to pick)"},
		}},
		{"filter & find", []row{
			{"/", "search — fuzzy title + d:easy t:graph s:todo"},
			{"1 / 2 / 3", "toggle Easy / Medium / Hard"},
			{"T", "topic browser (⏎/space toggle, c clear)"},
			{"u", "unsolved only"},
			{"o", "sort: number → acceptance ↑ → acceptance ↓"},
			{"p", "pick a random problem (honours filters)"},
			{"esc", "leave the active study list"},
			{"dd", "hide problem from rotation"},
		}},
		{"solve", []row{
			{"i", "edit in $EDITOR — in tmux, opens its own window"},
			{"r", "run the judge"},
			{"v", "view diagrams fullscreen (sharpest; n/p to cycle)"},
			{"H", "reveal next hint (lowers your SRS grade)"},
			{"L", "cycle language: python → cpp → c"},
		}},
		{"other", []row{
			{"D / tab", "toggle dashboard"},
			{"s", "sync freshly-downloaded problems"},
			{"R / ?", "this cheat sheet"},
			{"q", "quit"},
		}},
	}

	var b strings.Builder
	b.WriteString(styleHeader.Render("dojo — all keys") + "\n")
	for _, sec := range sections {
		b.WriteString("\n" + styleMedium.Render(sec.title) + "\n")
		for _, r := range sec.rows {
			b.WriteString("  " + styleHeader.Render(fmt.Sprintf("%-16s", r.k)) + r.d + "\n")
		}
	}
	b.WriteString("\n" + styleDim.Render("esc / R to close"))
	box := styleFocused.Padding(1, 2).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.helpOpen {
		return m.viewHelp()
	}
	if m.imageOpen {
		return m.viewImages()
	}
	if m.screen == screenDashboard {
		return m.viewDashboard()
	}
	if m.tagPicking {
		return m.viewTagPicker()
	}

	panes := lipgloss.JoinHorizontal(lipgloss.Top, m.viewList(), m.viewDetail())

	bar := fmt.Sprintf(" %d/%d  lang:%s ", len(m.visible), len(m.problems), runner.Order[m.lang])
	if fs := m.filterSummary(); fs != "" {
		bar += " " + styleHeader.Render(fs)
	}
	if m.filtering {
		bar += " " + m.filter.View()
	} else if m.err != nil {
		bar += styleFail.Render(" " + m.err.Error())
	} else if m.status != "" {
		bar += styleMedium.Render(" " + m.status)
	} else {
		bar += styleDim.Render(" ⏎ open · i edit · r run · / search · R help")
	}
	return lipgloss.JoinVertical(lipgloss.Left, panes, bar)
}
