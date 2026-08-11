#!/usr/bin/env python3
"""Seed the six "Dojo Originals" — original problems written for dojo that
cover the same techniques as the six premium-only Blind 75 entries
(alien-dictionary, graph-valid-tree, number-of-connected-components,
meeting-rooms, meeting-rooms-ii, encode-and-decode-strings).

All statements, examples and test data are original. Every test case is
verified against a reference solution before anything touches the database.
Offline, idempotent: originals are rebuilt from scratch on each run.

    python3 scripts/seed-originals.py [--db PATH]
"""

from __future__ import annotations

import argparse
import heapq
import json
import sqlite3
from pathlib import Path

# ids far above LeetCode's range so a future catalogue sync can never collide.
BASE_ID = 900000
BASE_FRONTEND = 9000


# --- reference solutions (used only to verify the test data) ------------------

def ref_rune_order(words):
    letters = set("".join(words))
    succ = {c: set() for c in letters}
    indeg = {c: 0 for c in letters}
    for a, b in zip(words, words[1:]):
        if a.startswith(b) and len(a) > len(b):
            return ""
        for x, y in zip(a, b):
            if x != y:
                if y not in succ[x]:
                    succ[x].add(y)
                    indeg[y] += 1
                break
    heap = sorted(c for c in letters if indeg[c] == 0)
    out = []
    while heap:
        c = heapq.heappop(heap)
        out.append(c)
        for n in succ[c]:
            indeg[n] -= 1
            if indeg[n] == 0:
                heapq.heappush(heap, n)
    return "".join(out) if len(out) == len(letters) else ""


def ref_valid_tree(n, edges):
    if len(edges) != n - 1:
        return False
    parent = list(range(n))

    def find(x):
        while parent[x] != x:
            parent[x] = parent[parent[x]]
            x = parent[x]
        return x

    for a, b in edges:
        ra, rb = find(a), find(b)
        if ra == rb:
            return False
        parent[ra] = rb
    return True


def ref_count_clusters(n, edges):
    parent = list(range(n))

    def find(x):
        while parent[x] != x:
            parent[x] = parent[parent[x]]
            x = parent[x]
        return x

    count = n
    for a, b in edges:
        ra, rb = find(a), find(b)
        if ra != rb:
            parent[ra] = rb
            count -= 1
    return count


def ref_can_attend(intervals):
    ivs = sorted(intervals)
    return all(a[1] <= b[0] for a, b in zip(ivs, ivs[1:]))


def ref_min_rooms(intervals):
    events = sorted([(s, 1) for s, _ in intervals] + [(e, -1) for _, e in intervals])
    best = cur = 0
    for _, d in events:
        cur += d
        best = max(best, cur)
    return best


def ref_pack_unpack(strs):
    packed = "".join(f"{len(s)}#{s}" for s in strs)
    out, i = [], 0
    while i < len(packed):
        j = packed.index("#", i)
        n = int(packed[i:j])
        out.append(packed[j + 1:j + 1 + n])
        i = j + 1 + n
    return out


# --- the problems -------------------------------------------------------------
# Each: (slug, title, difficulty, tags, description_md, hints, meta, cases, ref)
# cases: list of (args, expected, is_hidden)

PROBLEMS = [
    (
        "rune-order", "Order of the Runes", "Hard",
        ["Graph", "Topological Sort", "String"],
        """An archive of scrolls is written in a runic alphabet whose letter
order is unknown. You are given `words`, a list of words that the archivists
guarantee is sorted according to that unknown alphabet.

Recover the alphabet: return a string containing every distinct rune that
appears in `words`, arranged in an order consistent with how the words are
sorted. If several orders are consistent, return the **lexicographically
smallest** one (by ordinary a-z comparison). If no order can explain the
sorting — a contradiction, or a longer word placed before its own prefix —
return `""`.

**Example 1:**

```
Input: words = ["wrt","wrf","er","ett","rftt"]
Output: "wertf"
```

**Example 2:**

```
Input: words = ["ca","cb"]
Output: "abc"
Explanation: Only a < b is forced; c is unconstrained, so the
lexicographically smallest consistent order is "abc".
```

**Example 3:**

```
Input: words = ["abc","ab"]
Output: ""
Explanation: A word may not precede its own prefix.
```

**Constraints:**

- `1 <= words.length <= 100`
- `1 <= words[i].length <= 100`
- `words[i]` consists of lowercase English letters.
""",
        ["Adjacent word pairs are the only source of information: the first "
         "differing character gives one ordering constraint.",
         "This is topological sorting. For the lexicographically smallest "
         "result, run Kahn's algorithm with a min-heap instead of a queue.",
         "Two failure modes: a cycle among the constraints, and a word "
         "appearing after a longer word it prefixes."],
        {"name": "runeOrder",
         "params": [{"name": "words", "type": "list<string>"}],
         "return": {"type": "string"}},
        [
            ((["wrt", "wrf", "er", "ett", "rftt"],), "wertf", 0),
            ((["ca", "cb"],), "abc", 0),
            ((["abc", "ab"],), "", 0),
            ((["z", "x", "z"],), "", 1),
            ((["dojo"],), "djo", 1),
        ],
        ref_rune_order,
    ),
    (
        "valid-wiring-tree", "Valid Wiring Tree", "Medium",
        ["Graph", "Union Find", "Depth-First Search"],
        """A data centre has `n` machines labelled `0` to `n - 1`, and
`cables[i] = [a, b]` means machines `a` and `b` are wired together
directly.

Return `true` if the wiring forms a **tree**: every machine reachable from
every other, with no redundant cable (no cycles).

**Example 1:**

```
Input: n = 5, cables = [[0,1],[0,2],[0,3],[1,4]]
Output: true
```

**Example 2:**

```
Input: n = 5, cables = [[0,1],[1,2],[2,3],[1,3],[1,4]]
Output: false
Explanation: Cables 1-2, 2-3, 1-3 form a loop.
```

**Constraints:**

- `1 <= n <= 2000`
- `0 <= cables.length <= 5000`
- `cables[i].length == 2`, `0 <= a, b < n`, `a != b`
""",
        ["A tree on n nodes has exactly n - 1 edges. Check that first — "
         "then one property (connected OR acyclic) implies the other.",
         "Union-Find: if a cable joins two machines already in the same "
         "set, that cable closes a loop."],
        {"name": "validTree",
         "params": [{"name": "n", "type": "integer"},
                    {"name": "cables", "type": "integer[][]"}],
         "return": {"type": "boolean"}},
        [
            ((5, [[0, 1], [0, 2], [0, 3], [1, 4]]), True, 0),
            ((5, [[0, 1], [1, 2], [2, 3], [1, 3], [1, 4]]), False, 0),
            ((4, [[0, 1], [2, 3]]), False, 1),
            ((1, []), True, 1),
        ],
        ref_valid_tree,
    ),
    (
        "count-server-clusters", "Count Server Clusters", "Medium",
        ["Graph", "Union Find", "Breadth-First Search"],
        """A network has `n` servers labelled `0` to `n - 1`, and
`links[i] = [a, b]` means servers `a` and `b` can talk to each other
directly. Communication is transitive.

Return the number of **clusters** — groups of servers that can all reach
each other, directly or indirectly.

**Example 1:**

```
Input: n = 5, links = [[0,1],[1,2],[3,4]]
Output: 2
```

**Example 2:**

```
Input: n = 4, links = []
Output: 4
```

**Constraints:**

- `1 <= n <= 2000`
- `0 <= links.length <= 5000`
- `links[i].length == 2`, `0 <= a, b < n`, `a != b`
""",
        ["Start with n clusters; every link that joins two different "
         "clusters reduces the count by one.",
         "Union-Find with path compression makes that near-O(1) per link."],
        {"name": "countClusters",
         "params": [{"name": "n", "type": "integer"},
                    {"name": "links", "type": "integer[][]"}],
         "return": {"type": "integer"}},
        [
            ((5, [[0, 1], [1, 2], [3, 4]]), 2, 0),
            ((4, []), 4, 0),
            ((5, [[0, 1], [1, 2], [2, 3], [3, 4]]), 1, 1),
            ((6, [[0, 5], [5, 1], [2, 4]]), 3, 1),
        ],
        ref_count_clusters,
    ),
    (
        "busy-calendar-check", "Busy Calendar Check", "Easy",
        ["Array", "Sorting", "Intervals"],
        """You are given `meetings`, where `meetings[i] = [start, end)` is a
half-open time interval. Return `true` if one person could attend every
meeting — that is, no two meetings overlap. A meeting may begin at the
exact minute another ends.

**Example 1:**

```
Input: meetings = [[0,30],[5,10],[15,20]]
Output: false
```

**Example 2:**

```
Input: meetings = [[7,10],[2,4]]
Output: true
```

**Example 3:**

```
Input: meetings = [[1,2],[2,3]]
Output: true
Explanation: Back-to-back is fine — [1,2) ends as [2,3) begins.
```

**Constraints:**

- `0 <= meetings.length <= 10^4`
- `0 <= start < end <= 10^6`
""",
        ["Sort by start time; then only neighbours can conflict.",
         "After sorting, a conflict is exactly: previous end > next start."],
        {"name": "canAttendAll",
         "params": [{"name": "meetings", "type": "integer[][]"}],
         "return": {"type": "boolean"}},
        [
            (([[0, 30], [5, 10], [15, 20]],), False, 0),
            (([[7, 10], [2, 4]],), True, 0),
            (([[1, 2], [2, 3]],), True, 0),
            (([],), True, 1),
            (([[5, 8], [9, 12], [1, 5]],), True, 1),
        ],
        ref_can_attend,
    ),
    (
        "conference-room-planner", "Conference Room Planner", "Medium",
        ["Array", "Sorting", "Heap (Priority Queue)", "Intervals"],
        """You are given `meetings`, where `meetings[i] = [start, end)` is a
half-open time interval. Return the **minimum number of rooms** needed so
that every meeting takes place, with no two meetings sharing a room at the
same moment. A room is free again at the minute its meeting ends.

**Example 1:**

```
Input: meetings = [[0,30],[5,10],[15,20]]
Output: 2
```

**Example 2:**

```
Input: meetings = [[7,10],[2,4]]
Output: 1
```

**Example 3:**

```
Input: meetings = [[1,4],[2,5],[3,6]]
Output: 3
Explanation: At minute 3 all three meetings are in progress.
```

**Constraints:**

- `1 <= meetings.length <= 10^4`
- `0 <= start < end <= 10^6`
""",
        ["The answer is the maximum number of meetings in progress at any "
         "single moment.",
         "Sweep line: sort the start times and end times; walk them "
         "together, +1 on a start, -1 on an end (ends first on a tie).",
         "Alternative: sort by start and keep a min-heap of end times — "
         "the heap's peak size is the answer."],
        {"name": "minRooms",
         "params": [{"name": "meetings", "type": "integer[][]"}],
         "return": {"type": "integer"}},
        [
            (([[0, 30], [5, 10], [15, 20]],), 2, 0),
            (([[7, 10], [2, 4]],), 1, 0),
            (([[1, 4], [2, 5], [3, 6]],), 3, 0),
            (([[1, 2], [2, 3], [3, 4]],), 1, 1),
            (([[1, 10], [2, 3], [4, 5], [6, 7]],), 2, 1),
        ],
        ref_min_rooms,
    ),
    (
        "pack-unpack-strings", "Pack and Unpack Strings", "Medium",
        ["String", "Design"],
        """Design a codec that packs a list of strings into **one** string
for transmission, and unpacks that string back into the original list —
exactly, for *any* input, including empty strings and strings containing
any character your format might use as a delimiter.

Implement two methods and wire them into the judged entry point:

```python
class Solution:
    def encode(self, strs: List[str]) -> str: ...
    def decode(self, s: str) -> List[str]: ...

    def packUnpack(self, strs: List[str]) -> List[str]:
        return self.decode(self.encode(strs))
```

The judge calls `packUnpack` and checks it returns its input unchanged —
any self-consistent format passes, but naive delimiter-joining will break
on the tricky cases.

**Example 1:**

```
Input: strs = ["hello","world"]
Output: ["hello","world"]
```

**Example 2:**

```
Input: strs = ["#","","5#ambush"]
Output: ["#","","5#ambush"]
Explanation: Empty strings and delimiter look-alikes must survive the
round trip.
```

**Constraints:**

- `0 <= strs.length <= 200`
- `0 <= strs[i].length <= 200`
- `strs[i]` may contain any printable ASCII character.
""",
        ["Any single-character separator can appear inside the strings — "
         "escaping is fiddly. Is there a format that never needs it?",
         "Length-prefixing: emit `len(s)` + a marker + `s` for each "
         "string. Decoding reads the length, then skips exactly that many "
         "characters, so the content is never inspected."],
        {"name": "packUnpack",
         "params": [{"name": "strs", "type": "list<string>"}],
         "return": {"type": "list<string>"}},
        [
            ((["hello", "world"],), ["hello", "world"], 0),
            ((["#", "", "5#ambush"],), ["#", "", "5#ambush"], 0),
            (([],), [], 1),
            (([""],), [""], 1),
            ((["12#34", "#", "##", "plain"],), ["12#34", "#", "##", "plain"], 1),
        ],
        ref_pack_unpack,
    ),
]


def verify() -> None:
    for slug, *_, cases, ref in [(p[0], p[7], p[8]) for p in PROBLEMS]:
        for i, (args, expected, _) in enumerate(cases, 1):
            got = ref(*[json.loads(json.dumps(a)) for a in args])
            if json.loads(json.dumps(got)) != expected:
                raise SystemExit(
                    f"self-test FAILED: {slug} case {i}: got {got!r}, "
                    f"expected {expected!r}")
    print(f"self-test: all cases verified against reference solutions")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=str(Path.home() / ".local/share/dojo/dojo.db"))
    args = ap.parse_args()
    verify()

    db = sqlite3.connect(args.db, timeout=30)
    for i, (slug, title, diff, tags, md, hints, meta, cases, _ref) in enumerate(PROBLEMS):
        pid = BASE_ID + i + 1
        db.execute(
            """INSERT INTO problems (id, frontend_id, slug, title, difficulty,
                                     description_md, hints_json, meta_json,
                                     paid_only, judge_mode)
               VALUES (?,?,?,?,?,?,?,?,0,'typed')
               ON CONFLICT(slug) DO UPDATE SET
                   title=excluded.title, difficulty=excluded.difficulty,
                   description_md=excluded.description_md,
                   hints_json=excluded.hints_json, meta_json=excluded.meta_json,
                   judge_mode='typed'""",
            (pid, BASE_FRONTEND + i + 1, slug, title, diff, md,
             json.dumps(hints), json.dumps(meta)))
        (pid,) = db.execute("SELECT id FROM problems WHERE slug=?", (slug,)).fetchone()

        for tag in tags:
            tslug = tag.lower().replace(" ", "-").replace("(", "").replace(")", "")
            db.execute("INSERT OR IGNORE INTO tags (slug, name) VALUES (?,?)",
                       (tslug, tag))
            db.execute(
                """INSERT OR IGNORE INTO problem_tags (problem_id, tag_id)
                   SELECT ?, id FROM tags WHERE slug=?""", (pid, tslug))

        db.execute("DELETE FROM test_cases WHERE problem_id=?", (pid,))
        for ordinal, (cargs, expected, hidden) in enumerate(cases, 1):
            db.execute(
                """INSERT INTO test_cases (problem_id, ordinal, input, expected,
                                           is_hidden, source, args_json, expected_json)
                   VALUES (?,?,?,?,?,'original',?,?)""",
                (pid, ordinal,
                 "\n".join(json.dumps(a) for a in cargs), json.dumps(expected),
                 hidden, json.dumps(list(cargs)), json.dumps(expected)))
        print(f"{title} ({diff}): {len(cases)} cases")

    db.commit()
    print("originals seeded. now run seed-lists.py to refresh list membership.")


if __name__ == "__main__":
    main()
