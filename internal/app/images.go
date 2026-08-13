// Offline image rendering for problem descriptions. Ingest reduces <img>
// tags to "*[image: alt]*" placeholders; scripts/fetch-images.py saves the
// originals to ~/.local/share/dojo/img/<slug>/<NN>.<ext>. Here the N-th
// placeholder becomes the N-th file, drawn as truecolor Unicode half-blocks —
// no graphics protocol needed, so it works in any terminal and scrolls
// normally inside the viewport.
package app

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	imgPlaceholderRe = regexp.MustCompile(`\*\[image: [^\]]*\]\*`)
	imgTokenRe       = regexp.MustCompile(`DOJOIMG(\d+)X`)
)

// viewImages renders the current problem's diagrams at full terminal size —
// roughly double the pane's resolution, which is the sharpest this terminal
// can show without a graphics protocol.
func (m *Model) viewImages() string {
	if m.detail == nil {
		return ""
	}
	imgs := localImages(m.detail.Slug)
	if len(imgs) == 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styleDim.Render("no diagrams for this problem\n\nesc to close"))
	}
	idx := min(max(m.imageIdx, 0), len(imgs)-1)

	head := fmt.Sprintf("%s  %s",
		styleHeader.Render(fmt.Sprintf("%d. %s", m.detail.FrontendID, m.detail.Title)),
		styleDim.Render(fmt.Sprintf("image %d/%d", idx+1, len(imgs))))
	hint := styleDim.Render("n/p or ←/→ cycle · esc close")

	art, err := ansiImage(imgs[idx], m.width-4, m.height-6)
	if err != nil {
		art = styleFail.Render("cannot render " + filepath.Base(imgs[idx]) + ": " + err.Error())
	}
	body := lipgloss.JoinVertical(lipgloss.Center, head, "", art, "", hint)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func imageRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/dojo/img")
}

// localImages lists a problem's downloaded images in placeholder order.
func localImages(slug string) []string {
	entries, err := os.ReadDir(filepath.Join(imageRoot(), slug))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, filepath.Join(imageRoot(), slug, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// tokenizeImages swaps each "*[image: …]*" placeholder for a stable token
// glamour passes through untouched, but only when the matching file exists
// and is decodable — otherwise the italic placeholder stays.
func tokenizeImages(md, slug string) string {
	imgs := localImages(slug)
	k := 0
	return imgPlaceholderRe.ReplaceAllStringFunc(md, func(orig string) string {
		idx := k
		k++
		if idx >= len(imgs) || !decodableImage(imgs[idx]) {
			return orig
		}
		return fmt.Sprintf("DOJOIMG%dX", idx)
	})
}

func decodableImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// expandImageTokens replaces tokens in glamour's rendered output with the
// half-block art. Failures degrade to a dim note, never a broken pane.
func expandImageTokens(rendered, slug string, maxCols int) string {
	imgs := localImages(slug)
	return imgTokenRe.ReplaceAllStringFunc(rendered, func(tok string) string {
		var idx int
		fmt.Sscanf(tok, "DOJOIMG%dX", &idx)
		if idx >= len(imgs) {
			return styleDim.Render("[image unavailable]")
		}
		art, err := ansiImage(imgs[idx], maxCols, 0)
		if err != nil {
			return styleDim.Render("[image: " + filepath.Base(imgs[idx]) + " — " + err.Error() + "]")
		}
		// align with glamour's two-column document margin
		lines := strings.Split(art, "\n")
		for i, l := range lines {
			lines[i] = "  " + l
		}
		return "\n" + strings.Join(lines, "\n") + "\n"
	})
}

// ansiImage decodes a bitmap and renders it as ▀ half-blocks: each cell
// carries two vertical pixels (foreground = top, background = bottom), which
// is the highest fidelity a terminal without a graphics protocol can reach.
// Resolution is bounded only by the space given, so a wider pane (or the
// fullscreen viewer) is a genuinely sharper image.
func ansiImage(path string, maxCols, maxRows int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}

	sw := src.Bounds().Dx()
	sh := src.Bounds().Dy()
	if sw == 0 || sh == 0 {
		return "", fmt.Errorf("empty image")
	}

	cols := maxCols
	if cols > sw { // never upscale: small diagrams stay crisp at native size
		cols = sw
	}
	if cols < 4 {
		cols = 4
	}
	rows := (sh*cols/sw + 1) / 2
	// honour a row budget (fullscreen viewer) by shrinking width to match
	if maxRows > 0 && rows > maxRows {
		rows = maxRows
		if c := rows * 2 * sw / sh; c < cols {
			cols = max(4, c)
		}
	}

	px := boxScale(src, cols, rows*2)
	sharpen(px, 0.55) // area-averaging softens strokes; restore the edges

	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			tr, tg, tb := px[y*2][x][0], px[y*2][x][1], px[y*2][x][2]
			br, bg, bb := px[y*2+1][x][0], px[y*2+1][x][1], px[y*2+1][x][2]
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		b.WriteString("\x1b[0m\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// sharpen applies an unsharp mask in place. Averaging many source pixels into
// one cell blurs thin diagram strokes into the background; this pulls the
// contrast back so arrows and node outlines stay readable at low resolution.
func sharpen(px [][][3]uint8, amount float64) {
	h := len(px)
	if h < 3 || amount <= 0 {
		return
	}
	w := len(px[0])
	if w < 3 {
		return
	}
	orig := make([][][3]uint8, h)
	for y := range px {
		orig[y] = append([][3]uint8(nil), px[y]...)
	}
	at := func(y, x, c int) float64 {
		y = min(max(y, 0), h-1)
		x = min(max(x, 0), w-1)
		return float64(orig[y][x][c])
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				// 3x3 box blur, then push away from it
				var sum float64
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						sum += at(y+dy, x+dx, c)
					}
				}
				blur := sum / 9
				v := at(y, x, c)
				px[y][x][c] = uint8(min(max(v+amount*(v-blur), 0), 255))
			}
		}
	}
}

// boxScale downsamples with box averaging into a [h][w][3] RGB grid,
// compositing alpha over black.
func boxScale(src image.Image, w, h int) [][][3]uint8 {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	out := make([][][3]uint8, h)
	for y := 0; y < h; y++ {
		out[y] = make([][3]uint8, w)
		sy0 := y * sh / h
		sy1 := (y + 1) * sh / h
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < w; x++ {
			sx0 := x * sw / w
			sx1 := (x + 1) * sw / w
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var rs, gs, bs, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					r, g, b, a := src.At(sb.Min.X+sx, sb.Min.Y+sy).RGBA()
					// Composite over white, not black: these diagrams are
					// drawn for a white page, so a transparent background
					// with dark strokes must stay legible.
					t := 0xffff - a
					rs += uint64((r + t) >> 8)
					gs += uint64((g + t) >> 8)
					bs += uint64((b + t) >> 8)
					n++
				}
			}
			out[y][x] = [3]uint8{uint8(rs / n), uint8(gs / n), uint8(bs / n)}
		}
	}
	return out
}
