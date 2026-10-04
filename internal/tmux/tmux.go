// Package tmux wraps the handful of tmux commands dojo uses to live inside a
// session: one window per open solution, window options that say which
// problem a window belongs to, and the popups (sensei, switcher) bound to the
// prefix table.
package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Window options dojo sets. A window carrying ProblemOpt is "about" that
// problem (frontend id): the dojo browser window points it at the problem on
// screen, solution windows at their own problem.
const (
	ProblemOpt = "@dojo-problem" // frontend id the window is about
	TitleOpt   = "@dojo-title"   // that problem's title, for the switcher
	EditOpt    = "@dojo-edit"    // set on solution (editor) windows only
	HomeOpt    = "@dojo-home"    // set on the window running the dojo TUI
	SessionOpt = "@dojo"         // set on the session dojo runs in
)

// Inside reports whether this process runs inside a tmux pane.
func Inside() bool { return os.Getenv("TMUX") != "" }

// Run executes one tmux command and returns its trimmed stdout.
func Run(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("tmux %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Display expands a format against a target (pane, window or session).
func Display(target, format string) (string, error) {
	return Run("display-message", "-p", "-t", target, format)
}

// Here is the session and window ids of the pane this process runs in.
func Here() (session, window string, err error) {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return "", "", fmt.Errorf("not inside a tmux pane")
	}
	out, err := Display(pane, "#{session_id} #{window_id}")
	if err != nil {
		return "", "", err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return "", "", fmt.Errorf("tmux: unexpected %q", out)
	}
	return f[0], f[1], nil
}

// Window is one tmux window as dojo sees it.
type Window struct {
	ID      string
	Index   int
	Name    string
	Active  bool
	Problem int    // ProblemOpt, 0 if unset
	Title   string // TitleOpt
	Edit    bool   // a solution window
	Home    bool   // the dojo browser
}

const sep = "\x1f"

// Windows lists a session's windows with dojo's options.
func Windows(session string) ([]Window, error) {
	format := strings.Join([]string{"#{window_id}", "#{window_index}", "#{window_name}",
		"#{window_active}", "#{" + ProblemOpt + "}", "#{" + TitleOpt + "}",
		"#{" + EditOpt + "}", "#{" + HomeOpt + "}"}, sep)
	out, err := Run("list-windows", "-t", session, "-F", format)
	if err != nil {
		return nil, err
	}
	var ws []Window
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, sep)
		if len(f) != 8 {
			continue
		}
		w := Window{ID: f[0], Name: f[2], Active: f[3] == "1", Title: f[5],
			Edit: f[6] != "", Home: f[7] != ""}
		fmt.Sscan(f[1], &w.Index)
		fmt.Sscan(f[4], &w.Problem)
		ws = append(ws, w)
	}
	return ws, nil
}

// SetWindowOpts sets (or, for an empty value, unsets) window options in one
// tmux invocation.
func SetWindowOpts(window string, kv ...string) error {
	var args []string
	for i := 0; i+1 < len(kv); i += 2 {
		if len(args) > 0 {
			args = append(args, ";")
		}
		if kv[i+1] == "" {
			args = append(args, "set-option", "-wqu", "-t", window, kv[i])
		} else {
			args = append(args, "set-option", "-wq", "-t", window, kv[i], kv[i+1])
		}
	}
	if len(args) == 0 {
		return nil
	}
	_, err := Run(args...)
	return err
}

// Quote makes s a single shell word for commands tmux hands to /bin/sh.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
