package main

// The tmux helpers behind prefix+t (sensei) and prefix+s (the switcher). The
// key bindings dojo installs call `dojo popup …` through run-shell — the one
// place tmux expands formats like #{window_id} — and that opens the popup,
// whose command is `dojo tutor N` or `dojo switch SESSION`.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mahmood/dojo/internal/app"
	"github.com/mahmood/dojo/internal/store"
	"github.com/mahmood/dojo/internal/tmux"
	"github.com/mahmood/dojo/internal/tutor"
)

// subcommand runs a helper subcommand if args names one.
func subcommand(args []string, st func() *store.Store) (handled bool, err error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "popup": // popup tutor|switch CLIENT TARGET
		if len(args) != 4 {
			return true, errors.New("usage: dojo popup tutor|switch CLIENT TARGET")
		}
		return true, popup(args[1], args[2], args[3], st)
	case "tutor": // tutor N — inside the popup
		fid, err := fidArg(args)
		if err != nil {
			return true, err
		}
		return true, tutorPopup(fid, st())
	case "tutor-run": // tutor-run N — claude itself
		fid, err := fidArg(args)
		if err != nil {
			return true, err
		}
		return true, tutorRun(fid, st())
	case "taskvim": // taskvim — push due reviews into taskvim now
		res, err := app.SyncTaskvim(st(), nil)
		if err == nil {
			fmt.Printf("taskvim: %d review task(s) added, %d removed, %d rescheduled\n",
				res.Added, res.Removed, res.Moved)
		}
		return true, err
	case "switch": // switch SESSION — inside the popup
		if len(args) != 2 {
			return true, errors.New("usage: dojo switch SESSION")
		}
		return true, runSwitcher(args[1], st())
	}
	return false, nil
}

func fidArg(args []string) (int64, error) {
	if len(args) != 2 {
		return 0, fmt.Errorf("usage: dojo %s PROBLEM_NUMBER", args[0])
	}
	return strconv.ParseInt(args[1], 10, 64)
}

func self() string {
	exe, err := os.Executable()
	if err != nil {
		return "dojo"
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		return p
	}
	return exe
}

// popup opens the sensei or switcher popup on a client (CLIENT may be ""
// for the current one). For sensei, TARGET is the window whose problem to
// teach; for the switcher, the session to switch within.
func popup(kind, client, target string, st func() *store.Store) error {
	base := []string{"display-popup", "-E", "-b", "rounded"}
	if client != "" {
		base = append(base, "-c", client)
	}
	switch kind {
	case "tutor":
		v, _ := tmux.Display(target, "#{"+tmux.ProblemOpt+"}")
		fid, _ := strconv.ParseInt(v, 10, 64)
		if fid == 0 {
			msg := []string{"display-message"}
			if client != "" {
				msg = append(msg, "-c", client)
			}
			_, err := tmux.Run(append(msg, "sensei: open a problem first")...)
			return err
		}
		// Toggle: a sensei already open for this problem closes instead.
		if _, err := innerTmux("has-session", "-t", innerSession(fid)); err == nil {
			_, err := innerTmux("kill-session", "-t", innerSession(fid))
			return err
		}
		d, err := st().DetailByFrontendID(fid)
		if err != nil {
			return err
		}
		title := fmt.Sprintf(" 先生 sensei · %d. %s ", d.FrontendID, d.Title)
		args := append(base, "-w", "86%", "-h", "86%", "-T", title,
			"-S", "fg=#7dcfff", "-t", target,
			tmux.Quote(self())+" tutor "+strconv.FormatInt(fid, 10))
		_, err = tmux.Run(args...)
		return err
	case "switch":
		args := append(base, "-w", "72", "-h", "60%", "-T", " solutions ",
			"-S", "fg=#7dcfff", tmux.Quote(self())+" switch "+tmux.Quote(target))
		_, err := tmux.Run(args...)
		return err
	}
	return fmt.Errorf("unknown popup %q", kind)
}

func innerSession(fid int64) string { return fmt.Sprintf("sensei-%d", fid) }

// innerTmux runs a command against the private server sensei popups use.
func innerTmux(args ...string) (string, error) {
	c := exec.Command("tmux", append([]string{"-L", tutor.InnerSocket}, args...)...)
	c.Env = withoutTmux(os.Environ())
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

func withoutTmux(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			out = append(out, kv)
		}
	}
	return out
}

// solutionDir is the problem's folder under the solutions root, created if
// need be — sensei runs there, and its conversation is filed under it.
func solutionDir(d *store.ProblemDetail) (string, error) {
	dir := filepath.Join(app.SolutionsRoot(), fmt.Sprintf("%04d-%s", d.FrontendID, d.Slug))
	return dir, os.MkdirAll(dir, 0o755)
}

// tutorPopup is the popup's command: attach to (or start) the problem's
// session on sensei's private tmux server, whose prefix+t closes it.
func tutorPopup(fid int64, st *store.Store) error {
	d, err := st.DetailByFrontendID(fid)
	if err != nil {
		return err
	}
	dir, err := solutionDir(d)
	if err != nil {
		return err
	}
	prefix, _ := tmux.Run("show-options", "-gv", "prefix")
	// The config is executed by tmux, so it lives only in the user's own
	// cache dir (never a shared one like /tmp) and only they can touch it.
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cacheDir := filepath.Join(cache, "dojo")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(cacheDir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(cacheDir, "sensei-*.tmux.conf")
	if err != nil {
		return err
	}
	conf := f.Name()
	_, err = f.WriteString(tutor.InnerConf(prefix))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(conf)
		return err
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	cmd := tmux.Quote(self()) + " tutor-run " + strconv.FormatInt(fid, 10)
	// -f /dev/null keeps the user's ~/.tmux.conf out; the sensei config is
	// sourced explicitly and deleted as soon as tmux has read it.
	argv := []string{"tmux", "-L", tutor.InnerSocket, "-f", "/dev/null",
		"source-file", conf, ";",
		"run-shell", "rm -f " + tmux.Quote(conf), ";",
		"new-session", "-A", "-s", innerSession(fid), "-c", dir, cmd}
	return syscall.Exec(tmuxBin, argv, withoutTmux(os.Environ()))
}

// tutorRun starts claude as sensei for one problem, resuming the problem's
// conversation when there is one. If claude fails to start, the error stays
// on screen instead of the popup vanishing.
func tutorRun(fid int64, st *store.Store) error {
	d, err := st.DetailByFrontendID(fid)
	if err != nil {
		return pause(err)
	}
	dir, err := solutionDir(d)
	if err != nil {
		return pause(err)
	}
	id, resume, err := tutor.SessionID(dir)
	if err != nil {
		return pause(err)
	}
	bin, err := claudeBinary()
	if err != nil {
		return pause(err)
	}
	p := tutor.Problem{FrontendID: int(d.FrontendID), Title: d.Title, Difficulty: d.Difficulty,
		Tags: d.Tags, Description: d.Description, Hints: d.Hints, Dir: dir}
	root := app.SolutionsRoot()
	marker := filepath.Join(root, tutor.TrustedMarker)
	if _, err := os.Stat(marker); err != nil {
		fmt.Printf("\n  First time with sensei: if Claude Code asks whether to trust\n"+
			"  %s, choose \"Yes, I trust this folder\". It only asks once.\n", root)
		time.Sleep(3 * time.Second)
	}
	c := exec.Command(bin, tutor.Args(p, id, resume)...)
	c.Dir = root
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	// as the user's own `claude` wrapper runs it inside tmux, minus anything
	// a parent Claude Code session left in the environment
	c.Env = append(tutor.CleanEnv(withoutTmux(os.Environ())), "FORCE_COLOR=3")
	// Ten seconds in, claude is past its trust question. (On a timer:
	// closing the popup hangs this process up before Run returns.)
	settled := time.AfterFunc(10*time.Second, func() { _ = os.WriteFile(marker, nil, 0o644) })
	if err := c.Run(); err != nil && settled.Stop() {
		return pause(fmt.Errorf("claude exited: %w", err))
	}
	return nil
}

func claudeBinary() (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{".local/bin/claude", ".claude/local/claude"} {
		if _, err := os.Stat(filepath.Join(home, p)); err == nil {
			return filepath.Join(home, p), nil
		}
	}
	return "", errors.New("claude CLI not found on PATH — install Claude Code to use sensei")
}

// pause shows err and waits for enter, so a popup doesn't close on it.
func pause(err error) error {
	fmt.Fprintf(os.Stderr, "\n  sensei: %v\n\n  press enter to close", err)
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	return err
}
