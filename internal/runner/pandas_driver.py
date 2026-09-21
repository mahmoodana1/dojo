# dojo pandas judging driver (embedded in the binary, written to tmp at run
# time). Usage: python3 pandas_driver.py <solution.pandas.py> <metaData JSON>
# stdin: the example case text — LeetCode's "<Name> table:" ASCII dumps.
# stdout: the returned DataFrame as TSV (header row, then rows).
#
# LeetCode's metaData for a pandas problem carries the pd.DataFrame
# constructors it hands your function ("employees = pd.DataFrame([], columns=
# [...])"). dojo reads the frame names, columns and dtypes out of those
# declarations, fills the frames with the example rows, and calls your
# function with the frames and values its parameters name — the same binding
# LeetCode does, so the file needs no I/O code.
import ast
import importlib.util
import inspect
import json
import re
import sys

import pandas as pd

# LeetCode declares its frames two ways:
#   name = pd.DataFrame([], columns=[…]).astype({…})
#   name = pd.DataFrame({'col': pd.Series(dtype='int'), …})
DECL_RE = re.compile(r"^\s*(\w+)\s*=\s*pd\.DataFrame", re.M)
COLUMNS_RE = re.compile(r"columns\s*=\s*(\[[^\]]*\])")
ASTYPE_RE = re.compile(r"astype\(\s*(\{.*?\})\s*\)", re.S)
SERIES_RE = re.compile(r"['\"](\w+)['\"]\s*:\s*pd\.Series\(\s*dtype\s*=\s*['\"](\w+)['\"]")

# A heading line names the frame that follows. LeetCode writes it every way
# there is: "Employees table:", "`transactions` table:", or a bare "Visits".
TABLE_HEADING = re.compile(r"^[`*\s]*(\w+)[`*\s]*(?:\s+table)?\s*:?\s*$", re.I)
ASSIGNMENT = re.compile(r"^(\w+)\s*=\s*(.+)$")
LABEL = re.compile(r"^(\w+)\s*:\s*$")


def literal(text: str, fallback=None):
    try:
        return ast.literal_eval(text)
    except (ValueError, SyntaxError):
        return fallback


def declared_frames(meta) -> dict:
    """metaData's pythondata -> {frame name: (columns, dtypes)}, in order.

    Only the names have to be right — the columns come from the example — so
    a declaration dojo cannot fully read still yields a bindable frame."""
    out = {}
    for stmt in meta.get("pythondata") or []:
        m = DECL_RE.search(stmt)
        if not m:
            continue
        cols_match = COLUMNS_RE.search(stmt)
        astype_match = ASTYPE_RE.search(stmt)
        cols = literal(cols_match.group(1), []) if cols_match else []
        dtypes = literal(astype_match.group(1), {}) if astype_match else {}
        series = dict(SERIES_RE.findall(stmt))
        if series:
            cols = cols or list(series)
            dtypes = dtypes or series
        out[m.group(1)] = (cols or [], dtypes or {})
    return out


def parse_input(text: str):
    """LeetCode example input -> ([(table name, columns, rows)], {name: value}).

    Tables arrive as +---+ blocks under a heading; anything else a function
    takes (an `n = 2` line, a `student_data:` list literal) is a plain value."""
    tables, values, name, block = [], {}, None, []
    lines = text.split("\n")

    def flush():
        if block:
            tables.append((name, block[0], block[1:]))

    i = 0
    while i < len(lines):
        line = lines[i].strip()
        i += 1
        if not line or line.startswith("+-"):
            continue
        if line.startswith("|"):
            block.append([c.strip() for c in line.strip("|").split("|")])
            continue

        assign = ASSIGNMENT.match(line)
        if assign:
            v = literal(assign.group(2).strip())
            if v is not None:
                values[assign.group(1)] = v
                continue

        label = LABEL.match(line)
        if label and i < len(lines) and lines[i].strip().startswith(("[", "{")):
            buf = []
            while i < len(lines):
                buf.append(lines[i])
                i += 1
                v = literal("\n".join(buf))
                if v is not None:
                    values[label.group(1)] = v
                    break
            continue

        m = TABLE_HEADING.match(line)
        if m:
            flush()
            name, block[:] = m.group(1), []
    flush()
    return tables, values


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


def build_frames(meta, tables) -> dict:
    declared = declared_frames(meta)
    frames = {name: pd.DataFrame([], columns=cols).astype(dtypes)
              for name, (cols, dtypes) in declared.items()}

    for name, cols, rows in tables:
        key = (name or "").lower()
        target = next((n for n in frames if n.lower() == key), None)
        if target is None:
            # LeetCode usually names the table after the frame, but not
            # always: fall back to whichever frame is still unfilled.
            empty = [n for n, f in frames.items() if f.empty]
            target = empty[0] if empty else (name or key)
        loaded = pd.DataFrame([[cell(v) for v in row] for row in rows],
                              columns=cols)
        dtypes = {c: t for c, t in declared.get(target, ([], {}))[1].items()
                  if c in loaded.columns}
        try:
            loaded = loaded.astype(dtypes)
        except (TypeError, ValueError):
            pass               # keep inferred dtypes rather than fail the case
        frames[target] = loaded
    return frames


def solution_function(mod):
    """The one function the solution defines — LeetCode's stub has exactly
    one, and it is what the judge calls."""
    funcs = [f for _, f in inspect.getmembers(mod, inspect.isfunction)
             if f.__module__ == mod.__name__]
    if not funcs:
        sys.exit("no function defined — implement the one in the stub")
    return funcs[0]


def bind(fn, frames: dict, values: dict) -> list:
    """Match the function's parameters to the example: frames and scalars by
    name (LeetCode's own convention), then by declaration order."""
    pool = dict(frames)
    pool.update(values)
    lower = {k.lower(): v for k, v in pool.items()}
    spare = list(frames.values())
    args = []
    for i, param in enumerate(inspect.signature(fn).parameters):
        if param in pool:
            args.append(pool[param])
        elif param.lower() in lower:
            args.append(lower[param.lower()])
        elif i < len(spare):
            args.append(spare[i])
        else:
            sys.exit(f"the example gives nothing to pass as '{param}'")
    return args


def main() -> None:
    src, meta_json = sys.argv[1], sys.argv[2]
    meta = json.loads(meta_json)
    tables, values = parse_input(sys.stdin.read())
    frames = build_frames(meta, tables)

    spec = importlib.util.spec_from_file_location("solution", src)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)

    fn = solution_function(mod)
    result = fn(*bind(fn, frames, values))

    if not isinstance(result, pd.DataFrame):
        result = pd.DataFrame(result)
    print("\t".join(str(c) for c in result.columns))
    for row in result.itertuples(index=False):
        print("\t".join("null" if pd.isna(v) else str(v) for v in row))


if __name__ == "__main__":
    main()
