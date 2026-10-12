// The dashboard: an animated bento grid of cards. Selectable rows (study
// lists, due reviews, suggested picks) keep the exact dashItems() order the
// key handler navigates, wherever their cards land in the grid.
package app

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"github.com/mahmood/dojo/internal/store"
)

// dailyGoal is the number of solves that fills the "today" bar.
const dailyGoal = 3

// --- belts -------------------------------------------------------------------

type beltRank struct {
	name  string
	color lipgloss.Color
	minXP int
}

// beltLadder is the lifetime-XP progression. Thresholds roughly double so
// each belt is a real grind.
var beltLadder = []beltRank{
	{"white", colText, 0},
	{"yellow", colSand, 100},
	{"orange", colWarm, 300},
	{"green", colGreen, 700},
	{"blue", colBlue, 1400},
	{"purple", lipgloss.Color("#cba6f7"), 2500},
	{"brown", lipgloss.Color("#c2956e"), 4000},
	{"red", colRose, 6000},
	{"black", colMuted, 9000},
}

// beltForXP returns the current belt, the next one (nil at black), and the
// 1-based level.
func beltForXP(xp int) (beltRank, *beltRank, int) {
	cur := 0
	for i, b := range beltLadder {
		if xp >= b.minXP {
			cur = i
		}
	}
	if cur+1 < len(beltLadder) {
		next := beltLadder[cur+1]
		return beltLadder[cur], &next, cur + 1
	}
	return beltLadder[cur], nil, cur + 1
}

// --- small render helpers ----------------------------------------------------

// padTo pads (or ANSI-aware truncates) a styled string to exactly w cells.
func padTo(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = truncate.StringWithTail(s, uint(w), "…")
	}
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// card draws a borderless panel w cells wide: the title and a faint rule,
// the body, then a blank row. It is exactly as tall as the old rounded box
// was, so the grid keeps its shape, and the last 3 cells of every row are a
// gutter so neighbouring columns never touch.
func card(title string, w int, _ lipgloss.Color, body []string) string {
	iw := max(1, w-3)
	rule := max(0, iw-lipgloss.Width(title)-1)
	var b strings.Builder
	b.WriteString(padTo(styleSub.Render(title)+" "+styleFaint.Render(strings.Repeat("─", rule)), w) + "\n")
	for _, line := range body {
		b.WriteString(padTo(padTo(line, iw)+"   ", w) + "\n")
	}
	b.WriteString(strings.Repeat(" ", max(0, w)))
	return b.String()
}

// bar is a thin progress line: filled in the given style, the rest faint.
func bar(frac float64, width int, on lipgloss.Style) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	width = max(0, width)
	filled := int(frac*float64(width) + 0.5)
	return on.Render(strings.Repeat("━", filled)) + styleFaint.Render(strings.Repeat("─", width-filled))
}

// hints renders "key what   key what": keys grey, meanings one step fainter.
func hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, styleSub.Render(pairs[i])+styleDim.Render(" "+pairs[i+1]))
	}
	return strings.Join(parts, "   ")
}

// ago humanizes a past timestamp: now / 37m / 5h / 3d.
func ago(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func greeting(now time.Time) string {
	switch h := now.Hour(); {
	case h < 5:
		return "burning the midnight oil"
	case h < 12:
		return "good morning"
	case h < 18:
		return "good afternoon"
	default:
		return "good evening"
	}
}

// --- cards -------------------------------------------------------------------

func (m *Model) cardBelt(w int, e float64) string {
	cur, next, lv := beltForXP(m.stats.TotalXP)
	bs := lipgloss.NewStyle().Foreground(cur.color).Bold(true)
	xp := countUp(m.stats.TotalXP, e)
	lines := []string{
		bs.Render(cur.name+" belt") + styleDim.Render(fmt.Sprintf("  level %d", lv)),
	}
	if next != nil {
		span := next.minXP - cur.minXP
		frac := float64(xp-cur.minXP) / float64(span)
		lines = append(lines,
			fmt.Sprintf("%s  %d xp", bar(frac*e, w-16, lipgloss.NewStyle().Foreground(cur.color)), xp),
			styleDim.Render(fmt.Sprintf("%d xp → %s belt", next.minXP-m.stats.TotalXP, next.name)))
	} else {
		lines = append(lines, stylePass.Render(fmt.Sprintf("%d xp — mastery. teach others.", xp)))
	}
	return card("belt", w, cur.color, lines)
}

func (m *Model) cardToday(w int, e float64, now time.Time) string {
	s := m.stats
	goal := fmt.Sprintf("%d/%d", s.TodaySolved, dailyGoal)
	if s.TodaySolved >= dailyGoal {
		goal = stylePass.Render(goal + " — goal met ✓")
	}
	// The streak breathes gently between warm and sand while the dashboard is open.
	streakStyle := lipgloss.NewStyle().Foreground(colWarm)
	if pulse(time.Since(m.dashEnteredAt), 1200*time.Millisecond) < 0.5 {
		streakStyle = lipgloss.NewStyle().Foreground(colSand)
	}
	lines := []string{
		fmt.Sprintf("%s  %s", bar(float64(min(s.TodaySolved, dailyGoal))/dailyGoal*e, w-14, stylePass), goal),
		fmt.Sprintf("%s %s",
			streakStyle.Render(fmt.Sprintf("%d-day streak", countUp(s.StreakDays, e))),
			styleDim.Render(fmt.Sprintf("· %d att · %dm today",
				countUp(s.TodayAttempts, e), countUp(s.TodayMinutes, e)))),
		fmt.Sprintf("solved %s of %s · %s xp today",
			stylePass.Render(fmt.Sprint(countUp(s.Solved, e))),
			fmt.Sprint(s.Total), styleMedium.Render(fmt.Sprint(countUp(s.TodayXP, e)))),
	}
	if since := time.Since(m.confettiAt); !m.confettiAt.IsZero() && since < confettiDur {
		lines = append(lines, confettiLine(w-4, int(since/animFrame)), confettiLine(w-4, int(since/animFrame)+7))
	}
	return card("today", w, lipgloss.Color("6"), lines)
}

// confettiLine scatters deterministic sparkles for the goal celebration.
func confettiLine(w, seed int) string {
	rng := rand.New(rand.NewSource(int64(seed)))
	glyphs := []string{"✦", "✶", "*", "·", "❋"}
	colors := []lipgloss.Color{colBlue, colWarm, colTeal, colRose, colGreen, colSand}
	cells := make([]string, w)
	for i := range cells {
		cells[i] = " "
	}
	for i := 0; i < w/3; i++ {
		st := lipgloss.NewStyle().Foreground(colors[rng.Intn(len(colors))])
		cells[rng.Intn(w)] = st.Render(glyphs[rng.Intn(len(glyphs))])
	}
	return strings.Join(cells, "")
}

func (m *Model) cardProgress(w int, e float64) string {
	s := m.stats
	var lines []string
	for _, d := range []string{"Easy", "Medium", "Hard"} {
		frac := 0.0
		if s.DiffTotal[d] > 0 {
			frac = float64(s.DiffSolved[d]) / float64(s.DiffTotal[d])
		}
		lines = append(lines, fmt.Sprintf("%s %s %d/%d",
			diffStyle(d).Render(fmt.Sprintf("%-6s", d)),
			bar(frac*e, w-22, diffStyle(d)), s.DiffSolved[d], s.DiffTotal[d]))
	}
	if len(s.WeakTags) > 0 {
		lines = append(lines, styleDim.Render("weak: ")+styleHard.Render(strings.Join(s.WeakTags, " · ")))
	}
	return card("progress", w, lipgloss.Color("3"), lines)
}

func (m *Model) cardHeatmap(w int, e float64) string {
	cell := func(n int) string {
		switch {
		// Steps toward the shared pastel green rather than a neon ramp.
		case n <= 0:
			return styleFaint.Render("·")
		case n == 1:
			return lipgloss.NewStyle().Foreground(lipgloss.Color("#466048")).Render("■")
		case n <= 3:
			return lipgloss.NewStyle().Foreground(lipgloss.Color("#648c62")).Render("■")
		case n <= 5:
			return lipgloss.NewStyle().Foreground(lipgloss.Color("#85b981")).Render("■")
		default:
			return styleEasy.Render("■")
		}
	}
	weeks := len(m.stats.Activity) / 7
	maxWeeks := (w - 5) / 2
	start := 0
	if weeks > maxWeeks {
		start = weeks - maxWeeks
	}
	var lines []string
	for r := 0; r < 7; r++ {
		var b strings.Builder
		for c := start; c < weeks; c++ {
			if stagger(e, c-start, weeks-start) < 1 {
				b.WriteString(styleFaint.Render("·") + " ")
				continue
			}
			b.WriteString(cell(m.stats.Activity[c*7+r]) + " ")
		}
		lines = append(lines, b.String())
	}
	lines = append(lines, styleDim.Render("last 12 weeks"))
	return card("heatmap", w, lipgloss.Color("2"), lines)
}

// cardLists renders the selectable study-list rows, windowed around the
// cursor so a dozen lists stay compact. row() is the shared cursor-aware
// row builder from viewDashboard.
func (m *Model) cardLists(w int, e float64, row func(string) string) string {
	s := m.stats
	type entry struct {
		name          string
		solved, total int
	}
	entries := []entry{{"All problems", s.Solved, s.Total}}
	for _, pl := range m.lists {
		entries = append(entries, entry{pl.Name, pl.Solved, pl.Total})
	}
	const window = 9
	start := 0
	if m.dashCursor < len(entries) && len(entries) > window { // cursor inside this card
		start = max(0, min(m.dashCursor-window/2, len(entries)-window))
	}
	var lines []string
	if start > 0 {
		lines = append(lines, styleDim.Render(fmt.Sprintf("↑ %d more", start)))
	}
	nameW := max(8, w-16-11)
	// row() must fire once per entry, in order, to keep dashCursor indexes
	// aligned across cards — entries outside the window are consumed too.
	for i, en := range entries {
		frac := 0.0
		if en.total > 0 {
			frac = float64(en.solved) / float64(en.total)
		}
		line := row(fmt.Sprintf("%s %s %d/%d",
			padTo(truncate.StringWithTail(en.name, uint(nameW), "…"), nameW),
			bar(frac*stagger(e, max(0, i-start), window), 10, stylePass), en.solved, en.total))
		if i >= start && i < start+window {
			lines = append(lines, line)
		}
	}
	if rest := len(entries) - start - window; rest > 0 {
		lines = append(lines, styleDim.Render(fmt.Sprintf("↓ %d more", rest)))
	}
	return card("study lists", w, lipgloss.Color("6"), lines)
}

func (m *Model) cardForecast(w int, e float64) string {
	f := m.stats.Forecast
	peak := 1
	total := 0
	for _, n := range f {
		total += n
		if n > peak {
			peak = n
		}
	}
	ramp := []rune("▁▂▃▄▅▆▇█")
	var bars, labels strings.Builder
	day := time.Now()
	for i, n := range f {
		lvl := 0
		if n > 0 {
			lvl = 1 + int(float64(n)/float64(peak)*e*6.99)
			if lvl > 7 {
				lvl = 7
			}
		}
		st := styleFaint
		if n > 0 {
			st = styleMedium
		}
		bars.WriteString(st.Render(strings.Repeat(string(ramp[lvl]), 2)) + " ")
		labels.WriteString(strings.ToLower(day.AddDate(0, 0, i).Format("Mon")[:2]) + " ")
	}
	summary := "review queue is clear ✓"
	if total > 0 {
		summary = fmt.Sprintf("%d review(s) in the next 7 days", countUp(total, e))
	}
	return card("forecast", w, lipgloss.Color("3"),
		[]string{bars.String(), styleDim.Render(labels.String()), styleDim.Render(summary)})
}

// cardQueue renders due-review and suggested rows — all selectable.
func (m *Model) cardQueue(w int, probRow func(string, *store.Problem) string) string {
	s := m.stats
	var lines []string
	lines = append(lines, styleHeader.Render("due for review")+styleDim.Render("  srs says now"))
	if len(s.Due) == 0 {
		lines = append(lines, styleDim.Render("nothing due — the queue is clear ✓"))
	}
	for i := range s.Due {
		lines = append(lines, probRow(styleMedium.Render("↻"), &s.Due[i]))
	}
	lines = append(lines, "")
	lines = append(lines, styleHeader.Render("sensei suggests")+styleDim.Render("  fresh picks"))
	if len(s.Suggested) == 0 {
		lines = append(lines, styleDim.Render("nothing left — solve everything again, faster"))
	}
	for i := range s.Suggested {
		lines = append(lines, probRow(stylePass.Render("★"), &s.Suggested[i]))
	}
	return card("train now", w, lipgloss.Color("5"), lines)
}

func (m *Model) cardRecent(w int) string {
	var lines []string
	if len(m.stats.Recent) == 0 {
		lines = append(lines, styleDim.Render("no attempts yet — press i on a problem"))
	}
	for _, a := range m.stats.Recent {
		mark := stylePass.Render("✓")
		if !a.Passed {
			mark = styleFail.Render("✗")
		}
		title := truncate.StringWithTail(fmt.Sprintf("%d. %s", a.FrontendID, a.Title), uint(max(8, w-18)), "…")
		lines = append(lines, fmt.Sprintf("%s %s %s %s", mark, title,
			styleDim.Render(a.Language), styleDim.Render(ago(a.FinishedAt))))
	}
	return card("recent", w, lipgloss.Color("4"), lines)
}

func (m *Model) cardTrophies(w int, e float64) string {
	r := m.stats.Records
	// Labels share one column so the values line up as a table.
	label := func(l string) string { return styleDim.Render(fmt.Sprintf("%-16s", l)) }
	var lines []string
	if r.FastestTitle != "" {
		lines = append(lines, label("fastest green")+
			stylePass.Render(fmt.Sprintf("%.0fm", r.FastestMin))+styleDim.Render(" · ")+
			truncate.StringWithTail(r.FastestTitle, uint(max(6, w-30)), "…"))
	}
	if r.BestDay > 0 {
		lines = append(lines, label("best day")+
			stylePass.Render(fmt.Sprintf("%d solve(s)", countUp(r.BestDay, e)))+styleDim.Render(" · "+r.BestDayDate))
	}
	if r.LongestRun > 0 {
		lines = append(lines, label("longest streak")+
			lipgloss.NewStyle().Foreground(colWarm).Render(fmt.Sprintf("%d day(s)", countUp(r.LongestRun, e))))
	}
	if r.FirstTryPct >= 0 {
		lines = append(lines, label("first-try rate")+
			styleMedium.Render(fmt.Sprintf("%d%%", countUp(r.FirstTryPct, e))))
	}
	if len(lines) == 0 {
		lines = append(lines, styleDim.Render("no records yet — go set some"))
	}
	return card("trophies", w, colSand, lines)
}

// --- assembly ----------------------------------------------------------------

// quoteStyle fades the sensei quote in after each rotation.
func (m *Model) quoteStyle() lipgloss.Style {
	t := float64(time.Since(m.quoteChangedAt)) / float64(quoteFadeDur)
	shades := []string{"236", "238", "240", "243", "246"}
	i := int(easeOutCubic(t) * float64(len(shades)-1))
	if i < 0 {
		i = 0
	}
	if i >= len(shades) {
		i = len(shades) - 1
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(shades[i])).Italic(true)
}

func (m *Model) viewDashboard() string {
	now := time.Now()
	e := entrance(time.Since(m.dashEnteredAt))
	cur, _, _ := beltForXP(m.stats.TotalXP)

	// header: identity left, live clock right, quote underneath
	beltTag := lipgloss.NewStyle().Foreground(cur.color).Bold(true).Render(cur.name + " belt")
	left := styleHeader.Render("道場 dojo") + styleDim.Render("   ") + beltTag +
		styleDim.Render(" — "+greeting(now))
	clock := styleHeader.Render(now.Format("Mon 02 Jan · 15:04:05"))
	gap := max(1, m.width-4-lipgloss.Width(left)-lipgloss.Width(clock))
	header := left + strings.Repeat(" ", gap) + clock + "\n" +
		m.quoteStyle().Render("“"+m.quote+"”") + "\n"

	// shimmer: the selected row's marker cycles through cyan shades
	shimmerShades := []string{"#fab387", "#f9c8a8", "#fab387", "#e89a6e"}
	frame := int(time.Since(m.dashEnteredAt) / (250 * time.Millisecond))
	markerStyle := lipgloss.NewStyle().Foreground(
		lipgloss.Color(shimmerShades[frame%len(shimmerShades)])).Bold(true)

	// row/probRow are shared by every selectable card and MUST be called in
	// dashItems() order: All + lists, then due, then suggested.
	idx := 0
	row := func(line string) string {
		selected := idx == m.dashCursor
		idx++
		if selected {
			return markerStyle.Render("▸ ") + styleCursor.Render(line)
		}
		return "  " + line
	}
	// responsive grid: 3 / 2 / 1 columns
	cols := 1
	switch {
	case m.width >= 110:
		cols = 3
	case m.width >= 76:
		cols = 2
	}
	colW := (m.width - 4 - (cols - 1)) / cols

	titleW := uint(max(10, colW-18))
	probRow := func(icon string, p *store.Problem) string {
		return row(fmt.Sprintf("%s %s %s %s", icon,
			styleDim.Render(fmt.Sprintf("%4d", p.FrontendID)),
			padTo(truncate.StringWithTail(p.Title, titleW, "…"), int(titleW)),
			diffStyle(p.Difficulty).Render(p.Difficulty[:1])))
	}

	// build in dashItems order first (lists, then queue) so cursor indexes
	// line up; the rest are static and can render in any order.
	lists := m.cardLists(colW, e, row)
	queue := m.cardQueue(colW, probRow)
	belt := m.cardBelt(colW, e)
	today := m.cardToday(colW, e, now)
	progress := m.cardProgress(colW, e)
	heatmap := m.cardHeatmap(colW, e)
	forecast := m.cardForecast(colW, e)
	recent := m.cardRecent(colW)
	trophies := m.cardTrophies(colW, e)

	var columns [][]string
	switch cols {
	case 3:
		columns = [][]string{
			{belt, today, progress, heatmap},
			{lists, forecast},
			{queue, recent, trophies},
		}
	case 2:
		columns = [][]string{
			{belt, today, progress, heatmap, forecast},
			{lists, queue, recent, trophies},
		}
	default:
		columns = [][]string{{belt, today, lists, queue, forecast, recent, trophies, progress, heatmap}}
	}
	rendered := make([]string, len(columns))
	for i, c := range columns {
		rendered[i] = strings.Join(c, "\n")
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	hint := hints("j/k", "move", "⏎", "open", "gg/G", "jump", "D", "browse", "R", "help", "q", "quit")
	return lipgloss.NewStyle().Padding(1, 2).Render(header + "\n" + body + "\n" + hint)
}
