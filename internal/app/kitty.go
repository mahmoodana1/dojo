// Terminal capability probing.
//
// Two things the renderer needs that ANSI cannot express: the pixel size of a
// character cell (so half-block art keeps the source aspect ratio) and whether
// the terminal can draw real images. Both come from a TIOCGWINSZ ioctl plus
// environment sniffing, and both degrade to safe defaults when the terminal
// will not say.
package app

import (
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

// cellPixels reports the pixel width and height of one character cell.
// Zero,zero means the terminal did not report a pixel size. tmux forwards
// the real values, so this works inside a session too.
func cellPixels() (w, h int) {
	for _, f := range []*os.File{os.Stdout, os.Stderr, os.Stdin} {
		ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
		if err != nil || ws.Col == 0 || ws.Row == 0 {
			continue
		}
		if ws.Xpixel == 0 || ws.Ypixel == 0 {
			continue
		}
		return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
	}
	return 0, 0
}

// cellAspect is cell height ÷ cell width. Half-block art stacks two pixels
// per cell, so a "pixel" is square only when this is exactly 2. JetBrains
// Mono measures 2.2, which stretches an uncorrected image 10% vertically.
func cellAspect() float64 {
	if w, h := cellPixels(); w > 0 && h > 0 {
		return float64(h) / float64(w)
	}
	return 2.0 // conservative default: assume square half-block pixels
}

// kittyCapable reports whether real images can be drawn via kitty's icat.
//
// Placing graphics escapes directly is not viable here: inside tmux the
// escape reaches kitty but the cursor kitty sees is not where tmux's logical
// cursor is, so images land in the wrong place. icat owns that problem and
// solves it correctly, including tmux passthrough.
func kittyCapable() bool {
	// Explicit override wins. The launcher sets this on the tmux session
	// because a tmux server started from another terminal keeps its own
	// stale environment, so KITTY_WINDOW_ID cannot be trusted to appear.
	switch strings.ToLower(os.Getenv("DOJO_GRAPHICS")) {
	case "none", "off":
		return false
	case "kitty":
		return hasIcat()
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("KITTY_PID") != "" {
		return hasIcat()
	}
	term := os.Getenv("TERM") + " " + os.Getenv("TERM_PROGRAM")
	return strings.Contains(strings.ToLower(term), "kitty") && hasIcat()
}

func hasIcat() bool {
	_, err := exec.LookPath("kitty")
	return err == nil
}
