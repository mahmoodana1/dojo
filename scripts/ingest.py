#!/usr/bin/env python3
"""Seed the dojo SQLite database from LeetCode's public GraphQL API.

Dependency-free (stdlib only). Polite by design: ~2 requests/second with
jitter, resumable (re-running skips problems that already have a description),
and safe to Ctrl-C at any point thanks to per-problem commits.

Usage:
    python3 scripts/ingest.py                 # full catalogue, then details
    python3 scripts/ingest.py --limit 200     # only detail-fetch 200 problems
    python3 scripts/ingest.py --db /path/x.db

Paid-only problems are stored as metadata rows (no description) so the list
is complete but locked content is never fetched.
"""

from __future__ import annotations

import argparse
import html
import json
import random
import re
import sqlite3
import sys
import time
import urllib.request
from pathlib import Path

GRAPHQL = "https://leetcode.com/graphql"
HEADERS = {
    "Content-Type": "application/json",
    "Referer": "https://leetcode.com/problemset/",
    "User-Agent": "dojo-ingest/0.1 (personal offline practice tool)",
}

LIST_QUERY = """
query problemsetQuestionList($skip: Int!, $limit: Int!) {
  problemsetQuestionList: questionList(
    categorySlug: "" skip: $skip limit: $limit filters: {}
  ) {
    total: totalNum
    questions: data {
      questionId frontendQuestionId: questionFrontendId
      title titleSlug difficulty paidOnly: isPaidOnly acRate
      topicTags { name slug }
    }
  }
}
"""

DETAIL_QUERY = """
query questionDetail($titleSlug: String!) {
  question(titleSlug: $titleSlug) {
    questionId content hints metaData exampleTestcases similarQuestions
  }
}
"""


def gql(query: str, variables: dict) -> dict:
    body = json.dumps({"query": query, "variables": variables}).encode()
    req = urllib.request.Request(GRAPHQL, data=body, headers=HEADERS)
    with urllib.request.urlopen(req, timeout=30) as resp:
        payload = json.loads(resp.read())
    if "errors" in payload:
        raise RuntimeError(payload["errors"])
    return payload["data"]


# --- HTML -> markdown ---------------------------------------------------------
# LeetCode `content` is HTML. A real parser (stdlib HTMLParser) handles the
# nesting/attributes that regexes silently mangle: nested lists, <strong
# class="example">, trailing-space bold, &nbsp;, sup/sub, <pre> blocks.

from html.parser import HTMLParser


class _MDConverter(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.chunks: list[str] = []
        self.lists: list = []  # 'ul' | next-index int for ol
        self.in_pre = False

    def _emit(self, s: str) -> None:
        self.chunks.append(s)

    def _close_inline(self, mark: str) -> None:
        # move trailing whitespace outside the marker: '**Follow-up: **' is
        # not valid markdown bold; '**Follow-up:** ' is.
        ws = ""
        while self.chunks and self.chunks[-1] != "":
            last = self.chunks[-1]
            stripped = last.rstrip(" \t")
            if stripped == last:
                break
            self.chunks.pop()
            ws = last[len(stripped):] + ws
            if stripped:
                self.chunks.append(stripped)
                break
        self._emit(mark + ws)

    def handle_starttag(self, tag, attrs):
        if tag == "pre":
            self.in_pre = True
            self._emit("\n\n```\n")
        elif self.in_pre:
            return
        elif tag == "code":
            self._emit("`")
        elif tag in ("b", "strong"):
            self._emit("**")
        elif tag in ("i", "em"):
            self._emit("*")
        elif tag == "sup":
            self._emit("^")
        elif tag == "sub":
            self._emit("_")
        elif tag == "br":
            self._emit("\n")
        elif tag == "p":
            self._emit("\n\n")
        elif tag == "ul":
            self.lists.append("ul")
            self._emit("\n")
        elif tag == "ol":
            self.lists.append(1)
            self._emit("\n")
        elif tag == "li":
            indent = "  " * max(0, len(self.lists) - 1)
            if self.lists and self.lists[-1] != "ul":
                n = self.lists[-1]
                self.lists[-1] = n + 1
                self._emit(f"\n{indent}{n}. ")
            else:
                self._emit(f"\n{indent}- ")
        elif tag == "img":
            alt = dict(attrs).get("alt") or "diagram"
            self._emit(f"*[image: {alt}]*")

    def handle_endtag(self, tag):
        if tag == "pre":
            self.in_pre = False
            self._emit("\n```\n\n")
        elif self.in_pre:
            return
        elif tag == "code":
            self._close_inline("`")
        elif tag in ("b", "strong"):
            self._close_inline("**")
        elif tag in ("i", "em"):
            self._close_inline("*")
        elif tag in ("ul", "ol"):
            if self.lists:
                self.lists.pop()
            self._emit("\n")
        elif tag == "p":
            self._emit("\n\n")

    def handle_data(self, data):
        data = data.replace("\xa0", " ")
        if self.in_pre:
            self._emit(data)
            return
        collapsed = re.sub(r"[ \t\r\n]+", " ", data)
        if collapsed == " " and (not self.chunks or self.chunks[-1].endswith("\n")):
            return  # inter-tag whitespace at start of line: drop
        self._emit(collapsed)


def html_to_md(s: str) -> str:
    p = _MDConverter()
    p.feed(s or "")
    p.close()
    md = "".join(p.chunks)
    md = md.replace("\xa0", " ")           # belt-and-braces: no nbsp anywhere
    md = md.replace("****", "")            # empty bold pairs
    md = re.sub(r"```\n+", "```\n", md)    # tight code fences
    md = re.sub(r"\n+```", "\n```", md)
    md = re.sub(r"[ \t]+\n", "\n", md)     # trailing line whitespace
    md = re.sub(r"\n{3,}", "\n\n", md)
    return md.strip()


# Best-effort: pull Input/Output pairs out of the "Example N:" blocks so the
# judge has seed cases. Inputs keep LeetCode's `name = value` form; the
# solution template's parse step deals with that per problem.
EXAMPLE_RE = re.compile(
    r"Input:?\s*(?P<inp>.*?)\s*Output:?\s*(?P<out>.*?)\s*(?:```|\*\*|Explanation|Example|Constraints|$)",
    re.S,
)


def parse_examples(description_md: str) -> list[tuple[str, str]]:
    cases = []
    for m in EXAMPLE_RE.finditer(description_md):
        inp, out = m.group("inp").strip().strip("`"), m.group("out").strip().strip("`")
        if inp and out and len(inp) < 4000:
            cases.append((inp, out))
    return cases


# --- typed judging (v2) -------------------------------------------------------
# A problem qualifies for signature-aware judging when its metaData describes
# a plain-typed method (no ListNode/TreeNode/design-class shapes — phase 2)
# and its exampleTestcases blob parses into one JSON value per parameter.

PLAIN_TYPES = {"integer", "long", "string", "boolean", "double", "char"}


def is_plain_type(t: str) -> bool:
    t = t.strip()
    while t.endswith("[]"):
        t = t[:-2]
    if t.startswith("list<") and t.endswith(">"):
        return is_plain_type(t[5:-1])
    return t in PLAIN_TYPES


def typed_signature(meta_json: str | None):
    """Return (method_name, param_count) if typed judging applies, else None."""
    try:
        meta = json.loads(meta_json or "")
    except (TypeError, json.JSONDecodeError):
        return None
    if not isinstance(meta, dict) or "classname" in meta or not meta.get("name"):
        return None
    params = meta.get("params") or []
    types = [p.get("type", "") for p in params]
    types.append((meta.get("return") or {}).get("type", ""))
    if not params or not all(is_plain_type(t) for t in types):
        return None
    return meta["name"], len(params)


def build_cases(db: sqlite3.Connection, pid: int, md: str,
                meta_json: str | None, example_tests: str | None) -> None:
    """(Re)build example cases: raw rows always, typed columns when possible."""
    db.execute("DELETE FROM test_cases WHERE problem_id=? AND source='example'", (pid,))
    raw_pairs = parse_examples(md)

    groups: list[list] = []
    sig = typed_signature(meta_json)
    if sig and example_tests:
        _, k = sig
        lines = [l for l in example_tests.split("\n") if l.strip()]
        if lines and len(lines) % k == 0:
            try:
                vals = [json.loads(l) for l in lines]
                groups = [vals[i:i + k] for i in range(0, len(vals), k)]
            except json.JSONDecodeError:
                groups = []

    typed = 0
    for i in range(max(len(raw_pairs), len(groups))):
        inp, out = raw_pairs[i] if i < len(raw_pairs) else ("", "")
        args_json = expected_json = None
        if i < len(groups) and out:
            try:
                expected_json = json.dumps(json.loads(out))
                args_json = json.dumps(groups[i])
            except json.JSONDecodeError:
                pass
        if not inp and args_json is None:
            continue
        db.execute(
            """INSERT OR IGNORE INTO test_cases
               (problem_id, ordinal, input, expected, source, args_json, expected_json)
               VALUES (?,?,?,?, 'example', ?, ?)""",
            (pid, i, inp or args_json or "", out, args_json, expected_json))
        if args_json is not None:
            typed += 1
    db.execute("UPDATE problems SET judge_mode=? WHERE id=?",
               ("typed" if typed else "raw", pid))


def seed_catalogue(db: sqlite3.Connection) -> int:
    skip, total, page = 0, 1, 100
    while skip < total:
        data = gql(LIST_QUERY, {"skip": skip, "limit": page})["problemsetQuestionList"]
        total = data["total"]
        for q in data["questions"]:
            db.execute(
                """INSERT INTO problems (id, frontend_id, slug, title, difficulty,
                                         acceptance_rate, paid_only)
                   VALUES (?,?,?,?,?,?,?)
                   ON CONFLICT(slug) DO UPDATE SET acceptance_rate = excluded.acceptance_rate""",
                (int(q["questionId"]), int(q["frontendQuestionId"]), q["titleSlug"],
                 q["title"], q["difficulty"], q["acRate"], int(q["paidOnly"])),
            )
            for t in q["topicTags"]:
                db.execute("INSERT OR IGNORE INTO tags (slug, name) VALUES (?,?)", (t["slug"], t["name"]))
                db.execute(
                    """INSERT OR IGNORE INTO problem_tags (problem_id, tag_id)
                       SELECT ?, id FROM tags WHERE slug = ?""",
                    (int(q["questionId"]), t["slug"]),
                )
        db.commit()
        skip += page
        print(f"\rcatalogue: {min(skip, total)}/{total}", end="", flush=True)
        time.sleep(0.5 + random.random() * 0.3)
    print()
    return total


def fetch_details(db: sqlite3.Connection, limit: int | None) -> None:
    rows = db.execute(
        """SELECT slug FROM problems
           WHERE description_md IS NULL AND paid_only = 0
           ORDER BY frontend_id"""
    ).fetchall()
    if limit:
        rows = rows[:limit]
    for i, (slug,) in enumerate(rows, 1):
        try:
            q = gql(DETAIL_QUERY, {"titleSlug": slug})["question"]
        except Exception as e:  # noqa: BLE001 — log and keep grinding
            print(f"\n  ! {slug}: {e}", file=sys.stderr)
            time.sleep(5)
            continue
        md = html_to_md(q["content"] or "")
        db.execute(
            """UPDATE problems SET description_md=?, description_html=?, hints_json=?,
                                   meta_json=?, example_tests=?, similar_json=? WHERE slug=?""",
            (md, q["content"], json.dumps(q["hints"] or []), q["metaData"],
             q["exampleTestcases"], q["similarQuestions"] or "[]", slug),
        )
        pid = int(q["questionId"])
        build_cases(db, pid, md, q["metaData"], q["exampleTestcases"])
        db.commit()
        print(f"\rdetails: {i}/{len(rows)}  {slug[:50]:<50}", end="", flush=True)
        time.sleep(0.4 + random.random() * 0.4)
    print()


def reconvert(db: sqlite3.Connection) -> None:
    """Regenerate markdown + example test cases from stored HTML — offline."""
    rows = db.execute(
        """SELECT id, description_html, meta_json, example_tests
           FROM problems WHERE description_html IS NOT NULL"""
    ).fetchall()
    for pid, html_src, meta_json, example_tests in rows:
        md = html_to_md(html_src)
        db.execute("UPDATE problems SET description_md=? WHERE id=?", (md, pid))
        build_cases(db, pid, md, meta_json, example_tests)
        db.commit()  # per-row: keeps write locks tiny while ingest runs
    typed = db.execute("SELECT COUNT(*) FROM problems WHERE judge_mode='typed'").fetchone()[0]
    print(f"reconverted {len(rows)} problems from stored HTML ({typed} typed-judgeable)")


def migrate(db: sqlite3.Connection) -> None:
    for ddl in (
        "ALTER TABLE problems ADD COLUMN description_html TEXT",
        "ALTER TABLE problems ADD COLUMN judge_mode TEXT NOT NULL DEFAULT 'raw'",
        "ALTER TABLE test_cases ADD COLUMN args_json TEXT",
        "ALTER TABLE test_cases ADD COLUMN expected_json TEXT",
    ):
        try:
            db.execute(ddl)
        except sqlite3.OperationalError:
            pass  # column already exists


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=str(Path.home() / ".local/share/dojo/dojo.db"))
    ap.add_argument("--limit", type=int, default=None, help="max detail fetches this run")
    ap.add_argument("--skip-catalogue", action="store_true")
    ap.add_argument("--reconvert", action="store_true",
                    help="rebuild markdown/test cases from stored HTML, no network")
    args = ap.parse_args()

    Path(args.db).parent.mkdir(parents=True, exist_ok=True)
    db = sqlite3.connect(args.db)
    schema = (Path(__file__).parent.parent / "internal/store/schema.sql").read_text()
    db.executescript(schema)
    migrate(db)

    if args.reconvert:
        reconvert(db)
        return

    if not args.skip_catalogue:
        total = seed_catalogue(db)
        print(f"catalogue complete: {total} problems")
    fetch_details(db, args.limit)
    print("done. re-run any time to resume.")


if __name__ == "__main__":
    main()
