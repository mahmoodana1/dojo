package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestPNG drops a w×h checkerboard PNG at path.
func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{0, 0, 0, 255}
			if (x/4+y/4)%2 == 0 {
				c = color.RGBA{255, 80, 40, 255}
			}
			img.Set(x, y, c)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestAnsiImageDimensions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "img.png")
	writeTestPNG(t, p, 120, 60)
	out, err := ansiImage(p, 40, 0)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	// 120x60 at 40 cols → 20 px tall → 10 half-block rows
	if len(lines) != 10 {
		t.Errorf("rows = %d, want 10", len(lines))
	}
	if n := strings.Count(lines[0], "▀"); n != 40 {
		t.Errorf("cols = %d, want 40", n)
	}
	if !strings.Contains(out, "\x1b[38;2;") || !strings.Contains(out, "\x1b[48;2;") {
		t.Error("output must use truecolor fg+bg escapes")
	}
	if !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Error("each row must reset attributes")
	}
}

func TestAnsiImageNeverUpscales(t *testing.T) {
	p := filepath.Join(t.TempDir(), "small.png")
	writeTestPNG(t, p, 12, 12)
	out, err := ansiImage(p, 60, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Split(out, "\n")[0], "▀"); n != 12 {
		t.Errorf("small image rendered at %d cols, want native 12", n)
	}
}

// The fullscreen viewer passes a row budget; a tall image must shrink to fit
// it instead of overflowing the screen.
func TestAnsiImageHonoursRowBudget(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tall.png")
	writeTestPNG(t, p, 100, 400) // very tall
	out, err := ansiImage(p, 80, 12)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 12 {
		t.Errorf("rows = %d, want <= 12", len(lines))
	}
	// aspect must be preserved: narrow enough to match the row cap
	if n := strings.Count(lines[0], "▀"); n > 12 {
		t.Errorf("cols = %d, too wide for a 1:4 image in 12 rows", n)
	}
}

// Resolution must scale with the space available — that is the entire reason
// the fullscreen viewer looks better than the side pane.
func TestAnsiImageUsesFullWidth(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wide.png")
	writeTestPNG(t, p, 480, 340)
	narrow, err := ansiImage(p, 56, 0)
	if err != nil {
		t.Fatal(err)
	}
	wide, err := ansiImage(p, 160, 0)
	if err != nil {
		t.Fatal(err)
	}
	nc := strings.Count(strings.Split(narrow, "\n")[0], "▀")
	wc := strings.Count(strings.Split(wide, "\n")[0], "▀")
	if nc != 56 || wc != 160 {
		t.Errorf("cols = %d and %d, want 56 and 160 (no internal cap)", nc, wc)
	}
}

// A transparent-background diagram with dark strokes must composite over
// white, or it renders invisible on a dark terminal.
func TestTransparencyCompositesOverWhite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "alpha.png")
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := color.RGBA{0, 0, 0, 0} // fully transparent
			if x == 8 {
				c = color.RGBA{0, 0, 0, 255} // opaque black stroke
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	px := boxScale(mustDecode(t, p), 16, 16)
	if got := px[0][0]; got != [3]uint8{255, 255, 255} {
		t.Errorf("transparent pixel = %v, want white {255,255,255}", got)
	}
	if got := px[0][8]; got != [3]uint8{0, 0, 0} {
		t.Errorf("opaque stroke = %v, want black {0,0,0}", got)
	}
}

func mustDecode(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestTokenizeAndExpand(t *testing.T) {
	// fake image store: two files for slug "two-sum-test"
	root := t.TempDir()
	old := os.Getenv("HOME")
	os.Setenv("HOME", root)
	defer os.Setenv("HOME", old)
	dir := filepath.Join(root, ".local/share/dojo/img/two-sum-test")
	writeTestPNG(t, filepath.Join(dir, "01.png"), 20, 10)
	writeTestPNG(t, filepath.Join(dir, "02.png"), 20, 10)

	md := "intro *[image: ex1]* middle *[image: ]* tail *[image: three]*"
	tok := tokenizeImages(md, "two-sum-test")
	if strings.Count(tok, "DOJOIMG") != 2 {
		t.Fatalf("want 2 tokens (files exist), got %q", tok)
	}
	if !strings.Contains(tok, "*[image: three]*") {
		t.Error("third placeholder has no file and must survive as text")
	}

	out := expandImageTokens(tok, "two-sum-test", 40)
	if strings.Contains(out, "DOJOIMG") {
		t.Error("all tokens must be expanded")
	}
	if strings.Count(out, "▀") == 0 {
		t.Error("expanded output should contain half-block art")
	}

	// missing slug directory: placeholders untouched, nothing crashes
	if got := tokenizeImages(md, "no-such-slug"); got != md {
		t.Errorf("no local images → markdown unchanged, got %q", got)
	}
}
