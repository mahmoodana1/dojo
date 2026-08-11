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


# NeetCode 150 = Blind 75 + these. Premium entries use the same Dojo Originals
# stand-ins as the Blind 75 above.
NEETCODE_EXTRA = [
    # arrays & hashing / two pointers / sliding window
    "valid-sudoku", "two-sum-ii-input-array-is-sorted", "trapping-rain-water",
    "permutation-in-string", "sliding-window-maximum",
    # stack
    "min-stack", "evaluate-reverse-polish-notation", "generate-parentheses",
    "daily-temperatures", "car-fleet", "largest-rectangle-in-histogram",
    # binary search
    "binary-search", "search-a-2d-matrix", "koko-eating-bananas",
    "time-based-key-value-store", "median-of-two-sorted-arrays",
    # linked list
    "copy-list-with-random-pointer", "add-two-numbers",
    "find-the-duplicate-number", "lru-cache", "reverse-nodes-in-k-group",
    # trees
    "diameter-of-binary-tree", "balanced-binary-tree",
    "binary-tree-right-side-view", "count-good-nodes-in-binary-tree",
    # heap
    "kth-largest-element-in-a-stream", "last-stone-weight",
    "k-closest-points-to-origin", "kth-largest-element-in-an-array",
    "task-scheduler", "design-twitter",
    # backtracking
    "subsets", "combination-sum", "permutations", "subsets-ii",
    "combination-sum-ii", "palindrome-partitioning",
    "letter-combinations-of-a-phone-number", "n-queens",
    # graphs
    "max-area-of-island", "surrounded-regions", "rotting-oranges",
    "course-schedule-ii", "redundant-connection", "word-ladder",
    "reconstruct-itinerary", "min-cost-to-connect-all-points",
    "network-delay-time", "swim-in-rising-water",
    "cheapest-flights-within-k-stops",
    # dp
    "min-cost-climbing-stairs", "partition-equal-subset-sum",
    "best-time-to-buy-and-sell-stock-with-cooldown", "coin-change-ii",
    "target-sum", "interleaving-string",
    "longest-increasing-path-in-a-matrix", "distinct-subsequences",
    "edit-distance", "burst-balloons", "regular-expression-matching",
    # greedy
    "jump-game-ii", "gas-station", "hand-of-straights",
    "merge-triplets-to-form-target-triplet", "partition-labels",
    "valid-parenthesis-string",
    # intervals
    "minimum-interval-to-include-each-query",
    # math & geometry / bits
    "happy-number", "plus-one", "powx-n", "multiply-strings",
    "detect-squares", "single-number", "reverse-integer",
]

GRIND_75 = [
    "two-sum", "valid-parentheses", "merge-two-sorted-lists",
    "best-time-to-buy-and-sell-stock", "valid-palindrome", "invert-binary-tree",
    "valid-anagram", "binary-search", "flood-fill",
    "lowest-common-ancestor-of-a-binary-search-tree", "balanced-binary-tree",
    "linked-list-cycle", "implement-queue-using-stacks", "first-bad-version",
    "ransom-note", "climbing-stairs", "longest-palindrome", "reverse-linked-list",
    "majority-element", "add-binary", "diameter-of-binary-tree",
    "middle-of-the-linked-list", "maximum-depth-of-binary-tree",
    "contains-duplicate", "min-stack", "maximum-subarray", "insert-interval",
    "01-matrix", "k-closest-points-to-origin",
    "longest-substring-without-repeating-characters", "3sum",
    "binary-tree-level-order-traversal", "clone-graph",
    "evaluate-reverse-polish-notation", "course-schedule",
    "implement-trie-prefix-tree", "coin-change", "product-of-array-except-self",
    "busy-calendar-check", "validate-binary-search-tree", "number-of-islands",
    "rotting-oranges", "search-in-rotated-sorted-array", "combination-sum",
    "permutations", "merge-intervals", "lowest-common-ancestor-of-a-binary-tree",
    "time-based-key-value-store", "accounts-merge", "sort-colors", "word-break",
    "partition-equal-subset-sum", "string-to-integer-atoi", "spiral-matrix",
    "subsets", "binary-tree-right-side-view", "longest-palindromic-substring",
    "unique-paths", "construct-binary-tree-from-preorder-and-inorder-traversal",
    "container-with-most-water", "letter-combinations-of-a-phone-number",
    "word-search", "find-all-anagrams-in-a-string", "minimum-height-trees",
    "task-scheduler", "kth-smallest-element-in-a-bst", "minimum-window-substring",
    "serialize-and-deserialize-binary-tree", "trapping-rain-water",
    "find-median-from-data-stream", "word-ladder", "basic-calculator",
    "maximum-profit-in-job-scheduling", "merge-k-sorted-lists",
    "largest-rectangle-in-histogram",
]

TOP_INTERVIEW_150 = [
    "merge-sorted-array", "remove-element", "remove-duplicates-from-sorted-array",
    "remove-duplicates-from-sorted-array-ii", "majority-element", "rotate-array",
    "best-time-to-buy-and-sell-stock", "best-time-to-buy-and-sell-stock-ii",
    "jump-game", "jump-game-ii", "h-index", "insert-delete-getrandom-o1",
    "product-of-array-except-self", "gas-station", "candy", "trapping-rain-water",
    "roman-to-integer", "integer-to-roman", "length-of-last-word",
    "longest-common-prefix", "reverse-words-in-a-string", "zigzag-conversion",
    "find-the-index-of-the-first-occurrence-in-a-string", "text-justification",
    "valid-palindrome", "is-subsequence", "two-sum-ii-input-array-is-sorted",
    "container-with-most-water", "3sum", "minimum-size-subarray-sum",
    "longest-substring-without-repeating-characters",
    "substring-with-concatenation-of-all-words", "minimum-window-substring",
    "valid-sudoku", "spiral-matrix", "rotate-image", "set-matrix-zeroes",
    "game-of-life", "ransom-note", "isomorphic-strings", "word-pattern",
    "valid-anagram", "group-anagrams", "two-sum", "happy-number",
    "contains-duplicate-ii", "longest-consecutive-sequence", "summary-ranges",
    "merge-intervals", "insert-interval",
    "minimum-number-of-arrows-to-burst-balloons", "valid-parentheses",
    "simplify-path", "min-stack", "evaluate-reverse-polish-notation",
    "basic-calculator", "linked-list-cycle", "add-two-numbers",
    "merge-two-sorted-lists", "copy-list-with-random-pointer",
    "reverse-linked-list-ii", "reverse-nodes-in-k-group",
    "remove-nth-node-from-end-of-list", "remove-duplicates-from-sorted-list-ii",
    "rotate-list", "partition-list", "lru-cache", "maximum-depth-of-binary-tree",
    "same-tree", "invert-binary-tree", "symmetric-tree",
    "construct-binary-tree-from-preorder-and-inorder-traversal",
    "construct-binary-tree-from-inorder-and-postorder-traversal",
    "populating-next-right-pointers-in-each-node-ii",
    "flatten-binary-tree-to-linked-list", "path-sum", "sum-root-to-leaf-numbers",
    "binary-tree-maximum-path-sum", "binary-search-tree-iterator",
    "count-complete-tree-nodes", "lowest-common-ancestor-of-a-binary-tree",
    "binary-tree-right-side-view", "average-of-levels-in-binary-tree",
    "binary-tree-level-order-traversal",
    "binary-tree-zigzag-level-order-traversal",
    "minimum-absolute-difference-in-bst", "kth-smallest-element-in-a-bst",
    "validate-binary-search-tree", "number-of-islands", "surrounded-regions",
    "clone-graph", "evaluate-division", "course-schedule", "course-schedule-ii",
    "snakes-and-ladders", "minimum-genetic-mutation", "word-ladder",
    "implement-trie-prefix-tree", "design-add-and-search-words-data-structure",
    "word-search-ii", "letter-combinations-of-a-phone-number", "combinations",
    "permutations", "combination-sum", "n-queens-ii", "generate-parentheses",
    "word-search", "convert-sorted-array-to-binary-search-tree", "sort-list",
    "construct-quad-tree", "merge-k-sorted-lists", "maximum-subarray",
    "maximum-sum-circular-subarray", "search-insert-position",
    "search-a-2d-matrix", "find-peak-element", "search-in-rotated-sorted-array",
    "find-first-and-last-position-of-element-in-sorted-array",
    "find-minimum-in-rotated-sorted-array", "median-of-two-sorted-arrays",
    "kth-largest-element-in-an-array", "ipo", "find-k-pairs-with-smallest-sums",
    "find-median-from-data-stream", "add-binary", "reverse-bits",
    "number-of-1-bits", "single-number", "single-number-ii",
    "bitwise-and-of-numbers-range", "palindrome-number", "plus-one",
    "factorial-trailing-zeroes", "sqrtx", "powx-n", "max-points-on-a-line",
    "climbing-stairs", "house-robber", "word-break", "coin-change",
    "longest-increasing-subsequence", "triangle", "minimum-path-sum",
    "unique-paths-ii", "longest-palindromic-substring", "interleaving-string",
    "edit-distance", "best-time-to-buy-and-sell-stock-iii",
    "best-time-to-buy-and-sell-stock-iv", "maximal-square",
]

# Topic ladders: tag name -> list title. 40 problems each, easy -> hard.
LADDERS = {
    "Graph Theory": "Graphs ladder",
    "Dynamic Programming": "DP ladder",
    "Tree": "Trees ladder",
    "Binary Search": "Binary Search ladder",
    "Two Pointers": "Two Pointers ladder",
    "Sliding Window": "Sliding Window ladder",
    "Backtracking": "Backtracking ladder",
    "Heap (Priority Queue)": "Heap ladder",
}


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

    def slug_list(list_slug: str, name: str, desc: str, slugs: list[str]) -> None:
        marks = ",".join("?" * len(slugs))
        n = rebuild(db, list_slug, name, desc,
                    f"slug IN ({marks}) AND paid_only = 0", tuple(slugs))
        missing = [s for s in slugs if not db.execute(
            "SELECT 1 FROM problems WHERE slug=? AND paid_only=0", (s,)).fetchone()]
        print(f"{name}: {n} problems" +
              (f" ({len(missing)} unavailable: {missing})" if missing else ""))

    slug_list("neetcode-150", "NeetCode 150",
              "Blind 75 grown to the full NeetCode roadmap.",
              BLIND_75 + NEETCODE_EXTRA)
    slug_list("grind-75", "Grind 75",
              "Tech-interview essentials, beginner-friendly order.", GRIND_75)
    slug_list("top-interview-150", "Top Interview 150",
              "LeetCode's official interview study plan.", TOP_INTERVIEW_150)

    for tag, title in LADDERS.items():
        n = rebuild(db, title.lower().replace(" ", "-"), title,
                    f"40 {tag} problems, easy to hard.",
                    """id IN (SELECT p.id FROM problems p
                              JOIN problem_tags pt ON pt.problem_id = p.id
                              JOIN tags t          ON t.id = pt.tag_id
                              WHERE t.name = ? AND p.paid_only = 0
                                AND p.description_md IS NOT NULL
                              ORDER BY CASE p.difficulty WHEN 'Easy' THEN 0
                                       WHEN 'Medium' THEN 1 ELSE 2 END,
                                       p.acceptance_rate DESC
                              LIMIT 40)""", (tag,))
        print(f"{title}: {n} problems")

    db.commit()
    print("lists seeded. press s (or restart) in dojo to see them.")


if __name__ == "__main__":
    main()
