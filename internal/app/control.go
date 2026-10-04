package app

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// editRequestMsg asks the browser to open a problem's solution in the editor
// — sent by the switcher (prefix+s) for a solution with no window open.
type editRequestMsg struct{ fid int64 }

// ControlSocket is where a running dojo listens for requests from its tmux
// helpers. One per user; the newest dojo owns it. DOJO_SOCKET overrides it.
func ControlSocket() string {
	if p := os.Getenv("DOJO_SOCKET"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, fmt.Sprintf("dojo-%d.sock", os.Getuid()))
}

// Listen serves the control socket for p until the returned stop is called.
// The protocol is one line per connection: "edit <frontend id>".
func Listen(p *tea.Program) (stop func()) {
	path := ControlSocket()
	_ = os.Remove(path)
	// owner-only from the moment it exists: the fallback dir may be /tmp
	old := syscall.Umask(0o177)
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return func() {}
	}
	_ = os.Chmod(path, 0o600)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				line, _ := bufio.NewReader(conn).ReadString('\n')
				f := strings.Fields(line)
				if len(f) == 2 && f[0] == "edit" {
					if fid, err := strconv.ParseInt(f[1], 10, 64); err == nil {
						p.Send(editRequestMsg{fid})
						fmt.Fprintln(conn, "ok")
						return
					}
				}
				fmt.Fprintln(conn, "error: unknown request")
			}()
		}
	}()
	return func() {
		ln.Close()
		_ = os.Remove(path)
	}
}

// RequestEdit asks the running dojo to open problem fid in the editor.
func RequestEdit(fid int64) error {
	conn, err := net.DialTimeout("unix", ControlSocket(), time.Second)
	if err != nil {
		return fmt.Errorf("dojo is not running")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "edit %d\n", fid)
	reply, _ := bufio.NewReader(conn).ReadString('\n')
	if strings.TrimSpace(reply) != "ok" {
		return fmt.Errorf("dojo: %s", strings.TrimSpace(reply))
	}
	return nil
}
