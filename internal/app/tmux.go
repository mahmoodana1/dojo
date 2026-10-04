package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mahmood/dojo/internal/tmux"
)

// tmuxCtx is where dojo runs when it runs inside tmux: its session and its
// own (browser) window. Solution windows are opened next to it.
type tmuxCtx struct {
	session string // session id, "$3"
	window  string // the browser's window id, "@7"
	exe     string // this binary, for the key bindings to call back into
	problem int64  // frontend id last published on the browser window
}

// setupTmux announces dojo to the tmux session it runs in: the session and
// window options the prefix+t / prefix+s bindings look for, the bindings
// themselves, and a status line that lists open solutions. nil outside tmux.
func setupTmux() *tmuxCtx {
	if !tmux.Inside() {
		return nil
	}
	session, window, err := tmux.Here()
	if err != nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	t := &tmuxCtx{session: session, window: window, exe: exe}
	_, _ = tmux.Run("set-option", "-q", "-t", session, tmux.SessionOpt, "1")
	_ = tmux.SetWindowOpts(window, tmux.HomeOpt, "1")
	_ = installBindings(exe)
	if name, _ := tmux.Display(session, "#{session_name}"); name == "dojo" {
		styleSession(session, window)
	}
	return t
}

// Teardown withdraws what setupTmux published, so the bindings fall back to
// tmux's own behaviour once dojo has exited.
func (t *tmuxCtx) Teardown() {
	if t == nil {
		return
	}
	_, _ = tmux.Run("set-option", "-qu", "-t", t.session, tmux.SessionOpt)
	_ = tmux.SetWindowOpts(t.window, tmux.HomeOpt, "", tmux.ProblemOpt, "", tmux.TitleOpt, "")
}

// publish points the browser window at the problem on screen, so prefix+t
// from the browser opens sensei for it.
func (t *tmuxCtx) publish(fid int64, title string) {
	if t == nil || t.problem == fid {
		return
	}
	t.problem = fid
	_ = tmux.SetWindowOpts(t.window, tmux.ProblemOpt, fmt.Sprint(fid), tmux.TitleOpt, title)
}

// editorWindow finds the open solution window for a problem, "" if none.
func (t *tmuxCtx) editorWindow(fid int64) string {
	ws, err := tmux.Windows(t.session)
	if err != nil {
		return ""
	}
	for _, w := range ws {
		if w.Edit && int64(w.Problem) == fid {
			return w.ID
		}
	}
	return ""
}

// openEditorWindow runs the editor in a new window of dojo's session, tagged
// with the problem so the switcher, the status line and sensei know what it
// is. When the editor exits the window hands focus back to the browser (the
// verdict shows there) and signals `wait-for` so the judge runs.
func (t *tmuxCtx) openEditorWindow(editor, path string, fid int64, title, signal string) error {
	shell := fmt.Sprintf("%s %s; tmux select-window -t %s 2>/dev/null; tmux wait-for -S %s",
		editor, tmux.Quote(path), t.window, signal)
	id, err := tmux.Run("new-window", "-P", "-F", "#{window_id}", "-t", t.session+":",
		"-c", filepath.Dir(path), "-n", windowName(fid, title), shell)
	if err != nil {
		return err
	}
	return tmux.SetWindowOpts(id, tmux.ProblemOpt, fmt.Sprint(fid), tmux.TitleOpt, title,
		tmux.EditOpt, "1")
}

// windowName is the solution window's status-line label: "✎1 Two Sum".
func windowName(fid int64, title string) string {
	const max = 16
	if r := []rune(title); len(r) > max {
		title = strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return fmt.Sprintf("✎%d %s", fid, title)
}

// The prefix keys dojo takes over — only inside a dojo session (or, for t, a
// window that belongs to a problem). Everywhere else each falls back to
// whatever it was bound to before dojo, tmux's default if nothing.
var bindings = []struct {
	key, cond, cmd, fallback string
}{
	{"t", "#{||:#{" + tmux.ProblemOpt + "},#{" + tmux.SessionOpt + "}}",
		"popup tutor '#{client_name}' '#{window_id}'", "clock-mode"},
	{"s", "#{" + tmux.SessionOpt + "}",
		"popup switch '#{client_name}' '#{session_id}'", "choose-tree -Zs"},
}

var listKeyLine = regexp.MustCompile(`^bind-key\s+(?:-r\s+)?-T prefix (\S+)\s+(.*)$`)

func installBindings(exe string) error {
	current := map[string]string{}
	if out, err := tmux.Run("list-keys", "-T", "prefix"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if m := listKeyLine.FindStringSubmatch(line); m != nil {
				current[m[1]] = m[2]
			}
		}
	}
	var conf strings.Builder
	for _, b := range bindings {
		// Remember what the key did before dojo, once: a rebind by a newer
		// dojo must not mistake dojo's own binding for the original.
		origOpt := "@dojo-orig-" + b.key
		fallback, _ := tmux.Run("show-options", "-gqv", origOpt)
		if fallback == "" {
			fallback = b.fallback
			if cur, ok := current[b.key]; ok && !strings.Contains(cur, "dojo") {
				fallback = cur
			}
			_, _ = tmux.Run("set-option", "-gq", origOpt, fallback)
		}
		run := fmt.Sprintf("%s %s", tmux.Quote(exe), b.cmd)
		fmt.Fprintf(&conf, "bind-key -T prefix %s if-shell -F %s { run-shell -b %s } { %s }\n",
			b.key, tmux.Quote(b.cond), confQuote(run), fallback)
	}
	f, err := os.CreateTemp("", "dojo-keys-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(conf.String()); err != nil {
		f.Close()
		return err
	}
	f.Close()
	_, err = tmux.Run("source-file", f.Name())
	return err
}

// confQuote double-quotes s for a tmux config file.
func confQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + r.Replace(s) + `"`
}

// styleSession turns the dojo session's status line into a solution list:
// the browser plus one entry per open solution, the current one highlighted,
// and the two keys that move between them.
func styleSession(session, window string) {
	set := func(opt, val string) { _, _ = tmux.Run("set-option", "-q", "-t", session, opt, val) }
	set("status", "on")
	set("window-status-format", " #W ")
	set("window-status-current-format", " #W ")
	set("window-status-current-style", "fg=#1d2031,bg=#7dcfff,bold")
	set("window-status-style", "fg=#a9b1d6")
	set("window-status-separator", "")
	set("status-left", "#[fg=#7dcfff,bold] 道場 ")
	set("status-left-length", "10")
	set("status-right", "#[fg=#565f89]prefix s switch · prefix t sensei ")
	set("status-right-length", "40")
	_, _ = tmux.Run("rename-window", "-t", window, "browser")
}

type tutorDoneMsg struct{ err error }

// openTutor opens sensei for the problem on screen: as a popup in tmux (the
// same one prefix+t toggles), in place of the TUI outside it.
func (m *Model) openTutor() tea.Cmd {
	d := m.detail
	if d == nil {
		m.status = "open a problem first — sensei teaches one problem at a time"
		return nil
	}
	if m.tm != nil {
		c := exec.Command(m.tm.exe, "popup", "tutor", "", m.tm.window)
		return func() tea.Msg { return tutorDoneMsg{c.Run()} }
	}
	exe, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return tutorDoneMsg{err} }
	}
	c := exec.Command(exe, "tutor-run", fmt.Sprint(d.FrontendID))
	return tea.ExecProcess(c, func(err error) tea.Msg { return tutorDoneMsg{err} })
}
