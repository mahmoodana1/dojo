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

### The editor stub is LeetCode's own

dojo stores LeetCode's `codeSnippets` — the literal bytes in the site's editor
panel — and writes them into your solution file under a two-line comment
header. Whatever you write pastes back into the site with nothing to strip
out, in every family LeetCode has:

| Problem family | dojo writes | Judged by |
|---|---|---|
| algorithms (3,088) | `class Solution: def twoSum(self, nums: List[int], …)` | typed arguments, deep compare |
| database (105) | `# Write your MySQL query statement below` | your query against a scratch SQLite DB |
| pandas (15) | `import pandas as pd` + the DataFrame signature | frames from metaData, result compared as a table |
| shell (4) | `# Read from the file file.txt and output …` | not locally — solve here, submit on the site |
| javascript (18) | LeetCode's `var fn = function(…)` | not locally |

Backfill the stubs for a database seeded by an older dojo (network, resumable,
skips what it already has), then bring the files already on disk into line:

```bash
python3 scripts/ingest.py --snippets   # fetch the editor stubs
./dojo -refresh-stubs                  # rewrite the ones you never touched
```

`-refresh-stubs` only rewrites files that still hold a stub dojo generated —
compared byte for byte against every stub it has ever written, so a file with
your work in it is reported and left alone. A scaffold in a language LeetCode
does not offer for that problem is removed and replaced with the right one
(`solution.py` → `solution.sql` for a database problem).

The language cycle (`L`) offers exactly the tabs LeetCode offers for that
problem — a database problem gives you MySQL and Pandas, never C.

### Typed judging

Algorithm problems build true arguments (including `TreeNode`/`ListNode` from
LeetCode's flat arrays), call your method, and deep-compare the return value.
No I/O parsing. `void` returns compare the mutated first argument; design
problems (LRU Cache, Trie, Min Stack…) replay LeetCode's recorded call
sequence.

Database problems judge on result sets: same columns, same rows, any order —
the latitude LeetCode allows. The scratch database is SQLite, so a query that
leans on MySQL-only syntax (`DATE_ADD`, `IF()`, stored functions) reports an
error rather than a wrong answer.

A handful of problems have no local verdict at all and say so instead of
failing you: shell and JavaScript problems, and the ~30 where LeetCode runs a
custom checker (in-place mutation, interactive judges).

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
2. Implement the stub. **It is LeetCode's own editor panel**, verbatim —
   `class Solution: def twoSum(self, nums: List[int], target: int) -> …` for
   algorithms, `# Write your MySQL query statement below` for database
   problems — so solutions copy-paste to/from leetcode.com unchanged. C/C++
   use raw mode: read stdin, print the answer.
3. Save and close the editor (`:wq`). The judge compiles/runs your code
   against the local test cases automatically. In tmux, closing the `✎` window
   does the same thing.
4. The verdict (per-case ✓/✗, timings, stderr) appears in the description
   panel, your attempt is recorded, and spaced repetition is scheduled.
5. To retry, `i` reopens your existing code (never overwritten); `r` re-judges
   without editing.

## Several solutions at once (tmux)

Inside tmux, each solution gets its own window (`✎1 Two Sum`), at most one per
problem — `i` on a problem that already has one just switches to it. You can
keep browsing while editors are open; whichever editor you close, *that*
problem is judged and the browser jumps to its verdict.

| Key | Action |
|-----|--------|
| `prefix s` | switcher popup: the browser, every open solution, then recent solutions with no window (`⏎` on one of those asks dojo to open it). Type to filter. |
| `prefix t` | toggle **sensei** for the problem you're on (see below) |

The dojo session's status line lists the browser and each open solution, with
the current one highlighted. Both prefix keys are dojo's only while dojo is
running and only in its session; elsewhere they keep whatever they did before
(tmux's `choose-tree` and `clock-mode` by default).

## Sensei — a tutor per problem

`t` in the browser, or `prefix t` from any of the problem's windows, opens a
floating popup with a Claude **Haiku 5.5** session that teaches the problem
without solving it. It reads your current solution and the judge's latest
verdict (`.last-run.txt`), points at the line and the input that break your
code, and climbs a hint ladder one rung at a time. It never writes the
solution, however you ask.

- **Only alive while open.** `prefix t` inside the popup (or `prefix q`, or
  `/exit`) closes it, and that ends the process. Nothing keeps running.
- **Saved per problem.** Each problem has its own conversation (id in
  `.sensei-session` in the solution folder). Reopening resumes it.
- **Isolated.** It runs with read-only tools (Read/Grep/Glob), its own system
  prompt, and none of your Claude Code plugins, hooks or MCP servers.

It needs the `claude` CLI, logged in. The first time, Claude Code may ask
whether to trust your solutions folder: choose *Yes*. It asks once, because
every sensei runs from the solutions root.

## taskvim

dojo writes into [taskvim](../taskvim)'s `data.json`, and taskvim picks the
change up live. Nothing happens until that file exists.

**A green run ticks off taskvim tasks**, each logged as a commit, the same as
completing it there:

- every open task that names the problem: `LC 42`, `leetcode 42`,
  `trapping-rain-water`, or exactly `Trapping Rain Water`
- every dojo habit not yet done this period, e.g. `Easy Dojo Problem ~daily`
- one generic task, e.g. `do a medium leetcode problem`

"Dojo habit" and "generic task" mean text with *dojo*, *leetcode* or *lc* plus
*problem* or a difficulty. If it names a difficulty, only problems of that
difficulty count. If the solve ticks nothing, it is still logged under a
**LeetCode** course (once per problem per day), so taskvim's heatmap and
streak count dojo work.

**Due reviews become tasks** in LeetCode › Reviews, linked to the dashboard
and dated when they fell due. Passing one in dojo completes it. If one stops
being due, dojo removes it. This happens on launch and after every run, and
`dojo taskvim` runs it on demand.

`DOJO_TASKVIM=0` turns the bridge off. `TASKVIM_DATA` points it at another file.

## Keys

| Key | Action |
|-----|--------|
| `j` / `k` | move down / up (list or description scroll) |
| `h` / `l` | focus list / description panel |
| `gg` / `G` / `ctrl+d` / `ctrl+u` | vim jumps & half pages (list or description) |
| `/` | search: fuzzy on title, plus tokens `d:easy` `t:graph` `s:todo` (mix freely: `/d:h t:dp knapsack`) |
| `1` / `2` / `3` | toggle Easy / Medium / Hard filters |
| `T` | topic browser (tags with counts; `⏎`/`space` toggle, `c` clear, `esc` close) |
| `u` | toggle unsolved-only |
| `o` | cycle sort: problem № → acceptance ↑ (hardest) → acceptance ↓ (easiest) |
| `p` | random unsolved problem honouring active filters (LeetCode "Pick One") |
| `R` or `?` | full keybinding cheat sheet (modal) |
| `dd` | hide problem from rotation (`U` undoes) |
| `t` | sensei: a tutor for this problem (see above) |
| `Enter` | open problem (on dashboard: open a study list, or jump straight into a due-review / suggested problem) |
| `esc` | leave the active study list (back to all problems) |
| `i` | edit solution in `$EDITOR` (judged automatically on exit). Inside tmux this opens a separate tmux window so the problem page stays put. |
| `r` | re-run the judge |
| `H` | reveal next hint (counts against your SRS grade) |
| `L` | cycle language — only the tabs LeetCode offers for this problem (python → cpp → c, or mysql → pandas, or bash) |
| `D` / `tab` | toggle daily dashboard |
| `q` | quit |

Solutions live in `~/dojo-solutions/<id>-<slug>/solution.<ext>`; the database
in `~/.local/share/dojo/dojo.db` (override with `DOJO_SOLUTIONS` and `DOJO_DB`).

## Dojo Originals

Six of the Blind 75 are premium-only on LeetCode, so dojo ships six original
problems (`scripts/seed-originals.py`) covering the same techniques —
topological sort, union-find ×2, intervals ×2, and a string codec. They appear
as problems 9001–9006, in the **Dojo Originals** list, and in their Blind 75
slots. All are fully typed-judged with hidden cases.
