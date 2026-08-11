-- dojo schema. Applied idempotently on every launch (CREATE IF NOT EXISTS).
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS problems (
    id              INTEGER PRIMARY KEY,           -- LeetCode internal question id
    frontend_id     INTEGER NOT NULL,              -- the number users know ("1. Two Sum")
    slug            TEXT    NOT NULL UNIQUE,
    title           TEXT    NOT NULL,
    difficulty      TEXT    NOT NULL CHECK (difficulty IN ('Easy','Medium','Hard')),
    description_md  TEXT,                          -- markdown; NULL until detail-fetched or paid-only
    description_html TEXT,                         -- raw API HTML; lets us re-run the converter offline
    hints_json      TEXT    NOT NULL DEFAULT '[]', -- JSON array of hint strings
    meta_json       TEXT,                          -- LeetCode metaData (function signature) for future codegen
    example_tests   TEXT,                          -- raw exampleTestcases blob from the API
    acceptance_rate REAL,
    paid_only       INTEGER NOT NULL DEFAULT 0,
    similar_json    TEXT    NOT NULL DEFAULT '[]', -- slugs of similar problems ("what to grind next")
    judge_mode      TEXT    NOT NULL DEFAULT 'raw', -- 'typed' = signature-aware judging (v2)
    -- local learner state ------------------------------------------------
    status          TEXT    NOT NULL DEFAULT 'todo'
                        CHECK (status IN ('todo','attempted','solved','mastered')),
    hidden          INTEGER NOT NULL DEFAULT 0,    -- `dd` in the browser
    notes_md        TEXT    NOT NULL DEFAULT '',   -- personal notes, editable from the TUI
    first_solved_at TEXT,
    last_attempt_at TEXT
);

CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS problem_tags (
    problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
    PRIMARY KEY (problem_id, tag_id)
);

-- I/O-judged cases: `input` goes to the solution's stdin, `expected` is
-- compared (whitespace-normalised) against its stdout.
CREATE TABLE IF NOT EXISTS test_cases (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    ordinal    INTEGER NOT NULL,
    input      TEXT    NOT NULL,
    expected   TEXT    NOT NULL,
    is_hidden  INTEGER NOT NULL DEFAULT 0,   -- hidden cases: shown as pass/fail only, no diff
    source     TEXT    NOT NULL DEFAULT 'example', -- example | user | community
    -- typed judging (v2): JSON array of arguments + JSON expected value.
    -- NULL on raw-mode rows; raw input/expected stay populated as fallback.
    args_json     TEXT,
    expected_json TEXT,
    UNIQUE (problem_id, ordinal)
);

CREATE TABLE IF NOT EXISTS attempts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    problem_id   INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    started_at   TEXT    NOT NULL,
    finished_at  TEXT    NOT NULL,
    language     TEXT    NOT NULL,
    passed       INTEGER NOT NULL,             -- all cases green
    cases_passed INTEGER NOT NULL,
    cases_total  INTEGER NOT NULL,
    wall_ms      INTEGER NOT NULL,             -- total judge wall time
    hints_used   INTEGER NOT NULL DEFAULT 0,
    big_o_guess  TEXT,                         -- complexity-quiz answer before reveal
    big_o_actual TEXT
);

-- SM-2 spaced-repetition state; a problem enters rotation on first attempt.
CREATE TABLE IF NOT EXISTS reviews (
    problem_id    INTEGER PRIMARY KEY REFERENCES problems(id) ON DELETE CASCADE,
    ease          REAL    NOT NULL DEFAULT 2.5,
    interval_days INTEGER NOT NULL DEFAULT 0,
    reps          INTEGER NOT NULL DEFAULT 0,
    lapses        INTEGER NOT NULL DEFAULT 0,
    due_at        TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS daily_stats (
    day      TEXT PRIMARY KEY,                 -- YYYY-MM-DD
    solved   INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    minutes  INTEGER NOT NULL DEFAULT 0,
    xp       INTEGER NOT NULL DEFAULT 0
);

-- Curated study lists ("First 50", "Blind 75", …). Members reference the same
-- problems table; a list is just a named ordered subset.
CREATE TABLE IF NOT EXISTS lists (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS list_problems (
    list_id    INTEGER NOT NULL REFERENCES lists(id)    ON DELETE CASCADE,
    problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    ordinal    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (list_id, problem_id)
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_problems_status ON problems(status, hidden);
CREATE INDEX IF NOT EXISTS idx_attempts_problem ON attempts(problem_id, finished_at);
CREATE INDEX IF NOT EXISTS idx_reviews_due ON reviews(due_at);
