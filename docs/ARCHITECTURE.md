# dojo — Architecture

An offline-first, Vim-native algorithm practice environment. One binary, one
SQLite file, your `$EDITOR`, your window manager.

## 1. Stack decision

**Go + Bubble Tea** (with Lip Gloss, Glamour, bubbles, sahilm/fuzzy,
modernc.org/sqlite).

| Option | Verdict |
|--------|---------|
| Go + Bubble Tea | **Chosen.** Elm architecture = the modular state machine we need; `tea.ExecProcess` makes `$EDITOR` handoff a one-liner; Glamour renders problem markdown natively; modernc SQLite = zero CGo, single static binary. |
| Rust + Ratatui | Fastest, but iteration speed is worse and none of this is performance-bound (the judge shells out anyway). |
| Pure Neovim plugin | Tempting for a Vim purist, but couples the judge/DB lifecycle to editor sessions and makes the "TUI as OS-level app on workspace 7" model awkward. Instead, dojo *uses* Neovim as its editor. |

### State management

Strict Elm architecture: one `Model`, all mutations in `Update`, all effects
(DB reads, judging, editor) as `tea.Cmd` returning typed messages
(`problemsMsg`, `reportMsg`, `editorDoneMsg`, …). Adding a feature = new
message type + handler; no shared mutable state, no locks. Screens are an enum
(`dashboard`, `browser`) routed in `Update`/`View` — a future `stats` or
`review-queue` screen is one more enum value.

Layering (no cycles):

```
cmd/dojo → internal/app → internal/store → internal/srs
                        ↘ internal/runner ↗   (store reuses runner.Case)
```

`store` is the only SQL owner. `runner` knows nothing about problems — it
judges (source file, cases) pairs. `srs` is pure math.

## 2. Data pipeline

`scripts/ingest.py` (stdlib-only, no pip deps) seeds the DB in two passes:

1. **Catalogue** — paginated `problemsetQuestionList` GraphQL query: id,
   title, slug, difficulty, acceptance rate, paid flag, topic tags. ~30
   requests for the full ~3,300-problem set.
2. **Details** — per-slug `question` query: content (HTML → markdown), hints,
   `metaData` (function signatures), `codeSnippets` (the editor panel itself,
   per language), `exampleTestcases`, similar problems. Skips paid-only.

`--snippets` is a third, cheap pass that backfills `codeSnippets` alone, for
databases seeded before dojo stored them. Snippets are what make a stub
identical to the site's: `metaData` describes an algorithm signature well, but
says nothing about a shell problem beyond `{"shell": true}`.

Politeness and robustness: ~2 req/s with jitter, per-problem commits, resume
by re-running (rows with `description_md IS NULL` are the work queue), errors
logged and skipped. Alternative seed: any open-source LeetCode SQLite/JSON
dump can be mapped onto the same schema.

**Test-case reality check:** the API's `exampleTestcases` has inputs but *no
expected outputs*. Ingest therefore regex-parses the `Input:`/`Output:` pairs
from each description's Example blocks into `test_cases (source='example')`.
Sources are tiered: `example` (parsed), `user` (authored in-app — these are
your "hidden" cases), `community` (imported dumps).

### Schema (see `internal/store/schema.sql`)

Required fields plus deliberate additions:

- `meta_json` — LeetCode's function-signature metadata; enables future
  signature-aware boilerplate/harness codegen (v2).
- `similar_json` — powers "grind next" suggestions.
- `notes_md` — per-problem personal notes (your own editorial: pattern name,
  the trick, what you missed).
- `acceptance_rate` — proxy for real difficulty within a band.
- `attempts.big_o_guess / big_o_actual` — complexity-quiz feature.
- `test_cases.is_hidden` — hidden cases show pass/fail only, no diff.
- `reviews` — SM-2 state (`ease`, `interval_days`, `reps`, `lapses`, `due_at`).
- `daily_stats` — streaks/XP without scanning `attempts`.

## 3. TUI mechanics

Split-pane browser: problem list (38%) + Glamour-rendered description (62%),
borders indicate focus, status bar with counts/lang/errors/keymap.

Vim grammar: `j/k` (list or scroll, focus-dependent), `h/l` panel focus,
`gg/G/ctrl+d/ctrl+u`, `/` fuzzy filter-as-you-type over title+tags+difficulty,
`dd` hide (two-key pending-state machine, extensible to `dm`/`ds` marks),
`Enter` open, `i` edit, `r` judge, `H` reveal hint, `L` cycle language,
`D` dashboard, `q` quit.

**Editor handoff:** `i` materialises
`~/dojo-solutions/<id>-<slug>/solution.<ext>` (never clobbers existing work),
suspends the TUI via `tea.ExecProcess($EDITOR file)`, and on editor exit the
judge runs automatically — write, `:wq`, verdict. No context switch.

**In tmux** the editor gets its own window instead (`internal/app/tmux.go`),
tagged with window options (`@dojo-problem`, `@dojo-edit`, `@dojo-title`) so
other tools can tell what it is. The browser publishes the problem on screen in
its own window's `@dojo-problem`. Several solutions can be open at once, so
everything about a solve is keyed by problem, not by "the one on screen":
`editorDoneMsg`/`reportMsg` carry the problem id, and edit start times,
revealed hints and last verdicts are per-problem maps. On startup dojo binds
`prefix s`/`prefix t` to `run-shell` calls back into its own binary (`dojo
popup …`, since `run-shell` is where tmux expands formats like
`#{window_id}`). Each binding is conditional on the dojo options and falls
back to the key's previous binding. A unix socket (`ControlSocket`) lets the
switcher ask the running browser to open a solution.

**Sensei** (`internal/tutor`, `cmd/dojo/sensei.go`): `dojo tutor N` runs in
the popup and attaches to session `sensei-N` on a private tmux server
(`-L dojo-sensei`). Popups get no key bindings of their own, so that inner
server is what makes `prefix t` work as a toggle: it kills the session. With
`destroy-unattached`, closing the popup any other way does the same, so claude
never outlives the popup. `dojo tutor-run N` starts `claude --model
claude-sonnet-5-5` with a teacher system prompt, read-only tools, and
`--setting-sources project,local --strict-mcp-config`. It resumes the
problem's session id from `.sensei-session` when Claude has a transcript for
it. The cwd is always the solutions root, so Claude Code's trust question
comes up once rather than per problem.

The file's contents are LeetCode's `codeSnippets` entry for the chosen
language, verbatim, under a two-line comment header — so the finished solution
pastes into the site unchanged. dojo only synthesises a stub when no snippet
has been fetched: from `meta_json` for python, else the language's I/O
template. `L` cycles the languages LeetCode actually offers for that problem
(`CodeInfo.Languages`), so a database problem shows MySQL and Pandas, not C.

## 4. Local judge

`CodeInfo.Plan` picks the judge from what was written into the file — a stub
generated from a LeetCode snippet is never I/O-shaped, so it must never reach
the stdio judge:

| Plan | Applies to | Mechanism |
|---|---|---|
| `JudgeTyped` | algorithm problems with parsed example arguments | `typed_driver.py` builds real arguments, deep-compares the return |
| `JudgeSQL` | database problems | `sql_driver.py` replays the metaData DDL + example rows into in-memory SQLite, runs your query |
| `JudgePandas` | pandas tabs | `pandas_driver.py` builds the declared frames, calls your function by parameter name |
| `JudgeRaw` | C/C++ and untyped fallbacks | stdin → stdout, whitespace-normalised compare |
| `JudgeNone` | shell, javascript, custom-checker problems | reports why, records no attempt |

Result sets compare as tables (`table.go`): same column names in any order,
rows as a multiset, numbers within 1e-6 — the latitude LeetCode allows. The
scratch database is SQLite, so MySQL-only syntax surfaces as an error rather
than a wrong answer.

A language is still one `Registry` entry (optional compile argv + run argv
with `{{src}}`/`{{bin}}` placeholders) plus, for I/O languages, one template
file. Per-case: 5 s timeout (`context.WithTimeout` kills the process),
wall-time in ms, captured stderr, compile errors as their own verdict.

## 5. Hyprland integration

`hypr/dojo.lua` + `scripts/launch-dojo.sh`: SUPER+L runs the launcher, which
focuses the existing dojo window (workspace 7) or execs
`[workspace 7] alacritty --class dojo -e dojo`; window rules on
`class:^(dojo)$` pin workspace 7 + fullscreen. Matches the Lua config wrapper
in `~/.config/hypr/lua/`.

## 6. Progression design

- **SM-2, but graded by evidence** (`internal/srs`): the review grade is
  derived from the attempt itself — fail = lapse (due tomorrow); pass grades
  5/4/3 by hints used and time vs. a 35-minute budget. No self-assessment
  dishonesty.
- **Daily dashboard** on launch: streak, solved/attempted totals, reviews due,
  and weak tags (lowest pass-rate tags with ≥3 attempts) as grind suggestions.
- **Hints cost you**: revealing a hint is tracked and lowers the SRS grade,
  so the "just peek" temptation has a price the scheduler remembers.

### Roadmap (designed-for, not yet built)

- **Belt ranks** (it's a dojo): white → black, from mastered-problem count
  weighted by difficulty; belt shown in the status bar.
- **Complexity quiz**: after a pass, prompt for Big-O before revealing the
  editorial answer; schema already stores guess vs. actual per attempt.
- **Ghost racing**: on review solves, race the wall-clock of your previous
  attempt on the same problem.
- **Boss battle**: weekly timed Hard, no hints allowed, in your weakest tag.
- **Contribution heatmap** on the dashboard from `daily_stats`.
- **Signature-aware codegen** from `meta_json` (LeetCode-identical stubs).
- **Watch mode**: fsnotify on the solutions dir → judge on every save, not
  just editor exit.
- **FTS5** full-text search over descriptions for `/` on large banks.
