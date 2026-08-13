# 道場 dojo — offline LeetCode in your terminal

Vim-native TUI for algorithm practice: local SQLite problem bank, in-terminal
markdown rendering, `$EDITOR` handoff, multi-language local judge, and SM-2
spaced repetition. See `docs/ARCHITECTURE.md` for the full design.

## Setup

```sh
cd ~/projects/dojo

# 1. Build the TUI (single static binary, no CGo)
go mod tidy && go build -o dojo ./cmd/dojo

# 2. Seed the problem database (resumable; Ctrl-C safe; ~30 min for full detail fetch)
python3 scripts/ingest.py --limit 300     # start with 300 problems tonight
python3 scripts/ingest.py                 # finish the rest whenever

# 3. Curated study lists (Blind 75, NeetCode 150, Grind 75, topic ladders…)
python3 scripts/seed-lists.py

# 4. Problem diagrams, for offline inline rendering (~1.4k images, ~12 min)
python3 scripts/fetch-images.py

# 5. Run it
./dojo
```

Optional: `pip install markdownify` gives higher-fidelity HTML→markdown
conversion during ingest (a regex fallback is built in).

### Typed judging

95% of free problems (3088/3246) generate the **exact LeetCode Python
signature** — `class Solution` with the real method name and type hints, or
the real class for design problems. The judge builds true arguments
(including `TreeNode`/`ListNode` from LeetCode's flat arrays), calls your
method, and deep-compares the return value. No I/O parsing.

`void` returns compare the mutated first argument; design problems
(LRU Cache, Trie, Min Stack…) replay LeetCode's recorded call sequence.

The rest fall back to a raw-stdin stub: SQL and shell problems (no Python
signature exists on LeetCode either), two exotic types (`NestedInteger`,
`DataFrame`), and ~28 whose examples resist parsing.

After editing `scripts/ingest.py`, rebuild cases offline — no network, and
your solve history is untouched:

```bash
python3 scripts/ingest.py --reconvert
```

### Images

`fetch-images.py` saves each description's diagrams at **full original
quality** to `~/.local/share/dojo/img/<slug>/`, and the TUI draws them inline
as truecolor Unicode half-blocks — no graphics protocol, so it works in any
24-bit terminal and scrolls with the text.

**Press `v` to view diagrams at full quality.** In kitty this hands off to
`icat` and draws the true original pixels; in any other terminal it opens a
fullscreen half-block viewer (`n`/`p` cycle, `esc` close) at roughly 2.7× the
detail pane's resolution.

The launcher prefers kitty for exactly this reason, and `~/.tmux.conf` needs
`set -g allow-passthrough on` so graphics survive tmux. Falling back to
alacritty is fine — you just get half-blocks. Inline previews are always
half-blocks, scaled to the pane and corrected for the font's cell aspect
(JetBrains Mono cells are 2.2:1, not 2:1, so an uncorrected image is 10% too
tall).

Skip the script and descriptions simply keep their `[image: …]` placeholders;
nothing breaks. SVGs stay as placeholders too (Go's stdlib decodes PNG/JPG/GIF
only).

## Hyprland (SUPER+L → workspace 7, fullscreen)

```sh
chmod +x scripts/launch-dojo.sh
cp hypr/dojo.lua ~/.config/hypr/lua/dojo.lua
# then add  require("dojo")  to ~/.config/hypr/hyprland.lua and reload:
Hyprland --verify-config && hyprctl reload
```

## Submitting a solution

There is no submit button — **closing your editor is the submit**:

1. `⏎` open a problem, `i` to start coding (boilerplate is generated for you).
2. Implement the stub. **Python problems get the real LeetCode signature**
   (`class Solution: def twoSum(self, nums: List[int], target: int) -> …`) —
   the judge builds typed arguments and deep-compares your return value, so
   solutions copy-paste to/from leetcode.com. C/C++ (and non-typed problems)
   use raw mode: read stdin, print the answer.
3. Save and close the editor (`:wq`). The judge compiles/runs your code
   against the local test cases automatically. In tmux, closing the `✎` window
   does the same thing.
4. The verdict (per-case ✓/✗, timings, stderr) appears in the description
   panel, your attempt is recorded, and spaced repetition is scheduled.
5. To retry, `i` reopens your existing code (never overwritten); `r` re-judges
   without editing.

## Keys

| Key | Action |
|-----|--------|
| `j` / `k` | move down / up (list or description scroll) |
| `h` / `l` | focus list / description panel |
| `gg` / `G` / `ctrl+d` / `ctrl+u` | vim jumps & half-page scrolls |
| `/` | search: fuzzy on title, plus tokens `d:easy` `t:graph` `s:todo` (mix freely: `/d:h t:dp knapsack`) |
| `1` / `2` / `3` | toggle Easy / Medium / Hard filters |
| `T` | topic browser (tags with counts; `⏎`/`space` toggle, `c` clear, `esc` close) |
| `u` | toggle unsolved-only |
| `o` | cycle sort: problem № → acceptance ↑ (hardest) → acceptance ↓ (easiest) |
| `p` | random unsolved problem honouring active filters (LeetCode "Pick One") |
| `R` or `?` | full keybinding cheat sheet (modal) |
| `dd` | hide problem from rotation |
| `Enter` | open problem (on dashboard: open a study list, or jump straight into a due-review / suggested problem) |
| `esc` | leave the active study list (back to all problems) |
| `i` | edit solution in `$EDITOR` (judged automatically on exit). Inside tmux this opens a separate tmux window so the problem page stays put. |
| `r` | re-run the judge |
| `H` | reveal next hint (counts against your SRS grade) |
| `L` | cycle language (python → cpp → c) |
| `D` / `tab` | toggle daily dashboard |
| `q` | quit |

Solutions live in `~/dojo-solutions/<id>-<slug>/solution.<ext>`; the database
in `~/.local/share/dojo/dojo.db` (override with `DOJO_DB`).

## Dojo Originals

Six of the Blind 75 are premium-only on LeetCode, so dojo ships six original
problems (`scripts/seed-originals.py`) covering the same techniques —
topological sort, union-find ×2, intervals ×2, and a string codec. They appear
as problems 9001–9006, in the **Dojo Originals** list, and in their Blind 75
slots. All are fully typed-judged with hidden cases.
