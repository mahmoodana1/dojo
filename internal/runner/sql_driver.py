# dojo SQL judging driver (embedded in the binary, written to tmp at run time).
# Usage: python3 sql_driver.py <solution.sql> <metaData JSON>
# stdin: the example case text — LeetCode's "<Name> table:" ASCII dumps.
# stdout: the result set as TSV (header row, then rows).
#
# LeetCode's metaData carries the CREATE TABLE statements for a database
# problem, and the statement's example gives the rows. dojo replays both into
# a scratch SQLite database and runs your query against it, so a query written
# for the MySQL tab is checked locally. SQLite is not MySQL: dialect-only
# constructs (DATE_ADD, IF(), stored functions) will fail here even though
# they are correct on the site — that is reported as an error, not a wrong
# answer.
import json
import re
import sqlite3
import sys

# --- schema -------------------------------------------------------------------

TYPE_FIXES = (
    (re.compile(r"\benum\s*\([^)]*\)", re.I), "TEXT"),
    (re.compile(r"\bauto_increment\b", re.I), ""),
    (re.compile(r"\bunsigned\b", re.I), ""),
    (re.compile(r"\bcomment\s+'[^']*'", re.I), ""),
    (re.compile(r"\bengine\s*=\s*\w+", re.I), ""),
)


def sqlite_ddl(stmt: str) -> str:
    for pattern, repl in TYPE_FIXES:
        stmt = pattern.sub(repl, stmt)
    return stmt


# --- example data -------------------------------------------------------------

# A heading line names the table that follows. LeetCode writes it every way
# there is: "Sales table:", "`transactions` table:", or a bare "Visits".
TABLE_HEADING = re.compile(r"^[`*\s]*(\w+)[`*\s]*(?:\s+table)?\s*:?\s*$", re.I)


def parse_input(text: str) -> list[tuple[str | None, list[str], list[list[str]]]]:
    """LeetCode example input -> [(table name, columns, rows)]."""
    out, name, block = [], None, []

    def flush():
        if not block:
            return
        cols, rows = block[0], block[1:]
        out.append((name, cols, rows))

    for line in text.split("\n"):
        line = line.strip()
        if not line or line.startswith("+-"):
            continue
        m = TABLE_HEADING.match(line)
        if m:
            flush()
            name, block[:] = m.group(1), []
            continue
        if line.startswith("|"):
            block.append([c.strip() for c in line.strip("|").split("|")])
    flush()
    return out


def cell(value: str):
    if value.lower() in ("null", "none", ""):
        return None
    try:
        return int(value)
    except ValueError:
        pass
    try:
        return float(value)
    except ValueError:
        return value


# --- query --------------------------------------------------------------------

def strip_hash_comments(sql: str) -> str:
    """MySQL's `#` line comment is a syntax error in SQLite — and LeetCode's
    own stub is exactly one such comment. Quote-aware so a '#' inside a
    string literal survives."""
    out = []
    for line in sql.split("\n"):
        quote = None
        for i, ch in enumerate(line):
            if quote:
                if ch == quote:
                    quote = None
            elif ch in "'\"":
                quote = ch
            elif ch == "#":
                line = line[:i]
                break
        out.append(line)
    return "\n".join(out)


def split_statements(sql: str) -> list[str]:
    stmts, buf, quote = [], [], None
    for ch in sql:
        if quote:
            if ch == quote:
                quote = None
        elif ch in "'\"":
            quote = ch
        elif ch == ";":
            stmts.append("".join(buf))
            buf = []
            continue
        buf.append(ch)
    stmts.append("".join(buf))
    return [s for s in (s.strip() for s in stmts) if s]


def main() -> None:
    src, meta_json = sys.argv[1], sys.argv[2]
    meta = json.loads(meta_json)

    db = sqlite3.connect(":memory:")
    for stmt in meta.get("mysql") or []:
        db.execute(sqlite_ddl(stmt))

    tables = [r[0] for r in
              db.execute("SELECT name FROM sqlite_master WHERE type='table'")]
    filled: set[str] = set()
    for name, cols, rows in parse_input(sys.stdin.read()):
        table = next((t for t in tables if t.lower() == (name or "").lower()), None)
        if table is None:
            # Heading dojo could not match (odd casing, a stray label): the
            # blocks come in schema order, so take the next unfilled table.
            table = next((t for t in tables if t not in filled), None)
            if table is None:
                continue
        filled.add(table)
        placeholders = ",".join("?" * len(cols))
        columns = ",".join(f'"{c}"' for c in cols)
        db.executemany(f'INSERT INTO "{table}" ({columns}) VALUES ({placeholders})',
                       [[cell(v) for v in row] for row in rows])
    db.commit()

    query = strip_hash_comments(open(src).read())
    statements = split_statements(query)
    if not statements:
        sys.exit("empty query — write your SQL in the file")

    cur = None
    for stmt in statements:
        try:
            cur = db.execute(stmt)
        except sqlite3.Error as e:
            sys.exit(f"sqlite could not run this query (dojo judges SQL with "
                     f"SQLite, not MySQL): {e}")
    if cur is None or cur.description is None:
        sys.exit("the last statement returned no result set — end with a SELECT")

    cols = [d[0] for d in cur.description]
    print("\t".join(cols))
    for row in cur.fetchall():
        print("\t".join("null" if v is None else str(v) for v in row))


if __name__ == "__main__":
    main()
