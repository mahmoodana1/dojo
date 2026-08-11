# Dashboard v2 — "always alive" bento grid

Date: 2026-08-11 · Status: approved (user chose: always-alive animation, all four
new widgets, classics + topic-ladder lists, bento card grid)

## Goal

Make the dashboard professional and lively: card-based bento layout, continuous
but tasteful animation, and four new widgets, plus more seeded study lists.

## Structure

- Extract dashboard view code from `internal/app/app.go` into
  `internal/app/dashboard.go`; animation clock in `internal/app/anim.go`.
- Widget = card: titled rounded-border box rendered from `DashboardStats`.
- Grid packs cards into columns: 3 (width ≥ 110), 2 (≥ 76), else 1.
- Keybinds and selectable rows (study lists, due, suggested) unchanged.

## Animation

- 12 FPS `tea.Tick` frame loop only while dashboard is visible; browser keeps
  the 30 s tick.
- Entrance (~600 ms, ease-out cubic): bars sweep, counters count up, heat map
  fades in column by column.
- Ambient: clock with seconds, pulsing streak flame, shimmer on selected row,
  sensei quote cross-fades every ~20 s.
- Event: confetti burst on the "today" card when daily goal is reached.
- Hand-rolled easing, deterministic from elapsed time; no new dependencies.

## Data (store)

`DashboardStats` gains:

- `Forecast [7]int` — SRS reviews due per day for the next 7 days.
- `Recent []AttemptRow` — last 6 attempts (verdict, title, language, wall ms,
  finished at).
- `Records` — fastest passing solve, best day (solves), longest streak ever,
  first-try pass rate.
- `TotalXP int` — lifetime XP (sum of `daily_stats.xp`).

Belt rank from lifetime XP against fixed thresholds:
white → yellow → orange → green → blue → purple → brown → red → black.

## Lists

- `scripts/seed-lists.py`: add NeetCode 150, Grind 75, Top Interview 150
  (embedded slug lists, same pattern as Blind 75).
- Topic ladders: 8 lists (Graphs, DP, Trees, Binary Search, Two Pointers,
  Sliding Window, Backtracking, Heap) generated from tags, easy → hard.
- Study-lists card windows around the cursor to fit ~15 lists.

## Errors & testing

- New store queries degrade gracefully: empty widget, never a crash.
- Tests: belt thresholds, easing math, forecast bucketing, records SQL on a
  seeded temp DB, grid reflow at the three breakpoints.
