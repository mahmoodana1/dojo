#!/usr/bin/env python3
"""Seed curated study lists from the problems already in the database.

Offline — no network. Safe to re-run: lists are rebuilt from scratch each time
(membership only; solve progress lives on the problems themselves).

    python3 scripts/seed-lists.py [--db PATH]
"""

from __future__ import annotations

import argparse
import sqlite3
from pathlib import Path

# The Blind 75, by slug. The six premium-only entries are replaced by the
# "Dojo Originals" (seed-originals.py) — original problems covering the same
# techniques. Unknown slugs are reported, not fatal.
BLIND_75 = [
    # arrays & hashing
    "two-sum", "best-time-to-buy-and-sell-stock", "contains-duplicate",
    "product-of-array-except-self", "maximum-subarray", "maximum-product-subarray",
    "find-minimum-in-rotated-sorted-array", "search-in-rotated-sorted-array",
    "3sum", "container-with-most-water",
    # bits
    "sum-of-two-integers", "number-of-1-bits", "counting-bits",
    "missing-number", "reverse-bits",
    # dp
    "climbing-stairs", "coin-change", "longest-increasing-subsequence",
    "longest-common-subsequence", "word-break", "combination-sum-iv",
    "house-robber", "house-robber-ii", "decode-ways", "unique-paths", "jump-game",
    # graphs
    "clone-graph", "course-schedule", "pacific-atlantic-water-flow",
    "number-of-islands", "longest-consecutive-sequence", "rune-order",
    "valid-wiring-tree", "count-server-clusters",
    # intervals
    "insert-interval", "merge-intervals", "non-overlapping-intervals",
    "busy-calendar-check", "conference-room-planner",
    # linked lists
    "reverse-linked-list", "linked-list-cycle", "merge-two-sorted-lists",
    "merge-k-sorted-lists", "remove-nth-node-from-end-of-list", "reorder-list",
    # matrix
    "set-matrix-zeroes", "spiral-matrix", "rotate-image", "word-search",
    # strings
    "longest-substring-without-repeating-characters",
    "longest-repeating-character-replacement", "minimum-window-substring",
    "valid-anagram", "group-anagrams", "valid-parentheses", "valid-palindrome",
    "longest-palindromic-substring", "palindromic-substrings",
    "pack-unpack-strings",
    # trees
    "maximum-depth-of-binary-tree", "same-tree", "invert-binary-tree",
    "binary-tree-maximum-path-sum", "binary-tree-level-order-traversal",
    "serialize-and-deserialize-binary-tree", "subtree-of-another-tree",
    "construct-binary-tree-from-preorder-and-inorder-traversal",
    "validate-binary-search-tree", "kth-smallest-element-in-a-bst",
    "lowest-common-ancestor-of-a-binary-search-tree", "implement-trie-prefix-tree",
    "design-add-and-search-words-data-structure", "word-search-ii",
    # heap
    "top-k-frequent-elements", "find-median-from-data-stream",
]


def rebuild(db: sqlite3.Connection, slug: str, name: str, desc: str,
            member_sql: str, params: tuple = ()) -> int:
    db.execute(
        """INSERT INTO lists (slug, name, description) VALUES (?,?,?)
           ON CONFLICT(slug) DO UPDATE SET name=excluded.name,
                                           description=excluded.description""",
        (slug, name, desc))
    (list_id,) = db.execute("SELECT id FROM lists WHERE slug=?", (slug,)).fetchone()
    db.execute("DELETE FROM list_problems WHERE list_id=?", (list_id,))
    db.execute(
        f"""INSERT INTO list_problems (list_id, problem_id, ordinal)
            SELECT ?, id, ROW_NUMBER() OVER (ORDER BY frontend_id)
            FROM problems WHERE {member_sql}""",
        (list_id, *params))
    (n,) = db.execute(
        "SELECT COUNT(*) FROM list_problems WHERE list_id=?", (list_id,)).fetchone()
    return n


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=str(Path.home() / ".local/share/dojo/dojo.db"))
    args = ap.parse_args()
    db = sqlite3.connect(args.db, timeout=30)
    schema = (Path(__file__).parent.parent / "internal/store/schema.sql").read_text()
    db.executescript(schema)

    n = rebuild(db, "first-50", "First 50",
                "The classic opening stretch: problems 1-50.",
                "frontend_id BETWEEN 1 AND 50 AND paid_only = 0")
    print(f"First 50: {n} problems")

    n = rebuild(db, "basic-100", "Basic 100",
                "The 100 gentlest Easy problems by acceptance rate — warm-up territory.",
                """id IN (SELECT id FROM problems
                         WHERE difficulty = 'Easy' AND paid_only = 0
                           AND acceptance_rate IS NOT NULL
                         ORDER BY acceptance_rate DESC LIMIT 100)""")
    print(f"Basic 100: {n} problems")

    n = rebuild(db, "dojo-originals", "Dojo Originals",
                "Original problems standing in for premium-only classics.",
                "id BETWEEN 900001 AND 900999")
    print(f"Dojo Originals: {n} problems")

    marks = ",".join("?" * len(BLIND_75))
    n = rebuild(db, "blind-75", "Blind 75",
                "The canonical interview-prep list.",
                f"slug IN ({marks}) AND paid_only = 0", tuple(BLIND_75))
    missing = [s for s in BLIND_75 if not db.execute(
        "SELECT 1 FROM problems WHERE slug=? AND paid_only=0", (s,)).fetchone()]
    print(f"Blind 75: {n} problems" +
          (f" ({len(missing)} unavailable: premium or renamed: {missing})" if missing else ""))

    db.commit()
    print("lists seeded. press s (or restart) in dojo to see them.")


if __name__ == "__main__":
    main()
