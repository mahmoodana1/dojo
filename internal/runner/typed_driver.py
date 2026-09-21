# dojo typed-judging driver (embedded in the binary, written to tmp at run
# time). Usage: python3 typed_driver.py <solution.py> <metaData JSON>
# stdin: JSON array of arguments. stdout: JSON-encoded return value.
#
# LeetCode encodes linked lists and binary trees as flat arrays; the real
# signature takes objects. This driver inflates arguments on the way in and
# flattens the return value on the way out, so a solution written against the
# LeetCode signature runs unmodified.
import importlib.util
import json
import sys
import typing


class ListNode:
    def __init__(self, val=0, next=None):
        self.val = val
        self.next = next


class TreeNode:
    def __init__(self, val=0, left=None, right=None):
        self.val = val
        self.left = left
        self.right = right


def build_list(vals):
    head = tail = None
    for v in vals or []:
        node = ListNode(v)
        if tail is None:
            head = tail = node
        else:
            tail.next = node
            tail = node
    return head


def build_tree(vals):
    """LeetCode level-order encoding: children are listed only for non-null
    parents, so consume the queue rather than indexing by 2i+1."""
    vals = list(vals or [])
    if not vals or vals[0] is None:
        return None
    root = TreeNode(vals[0])
    queue, i = [root], 1
    while queue and i < len(vals):
        node = queue.pop(0)
        if i < len(vals):
            v = vals[i]
            i += 1
            if v is not None:
                node.left = TreeNode(v)
                queue.append(node.left)
        if i < len(vals):
            v = vals[i]
            i += 1
            if v is not None:
                node.right = TreeNode(v)
                queue.append(node.right)
    return root


def ser_list(node):
    out = []
    seen = set()
    while node is not None:
        if id(node) in seen:      # cycle-safe: never hang the judge
            break
        seen.add(id(node))
        out.append(node.val)
        node = node.next
    return out


def ser_tree(node):
    if node is None:
        return []
    out, queue = [], [node]
    while queue:
        n = queue.pop(0)
        if n is None:
            out.append(None)
            continue
        out.append(n.val)
        queue.append(n.left)
        queue.append(n.right)
    while out and out[-1] is None:   # LeetCode trims trailing nulls
        out.pop()
    return out


def _strip(t):
    return (t or "").strip()


def to_arg(v, t):
    """JSON value -> real argument, following the declared type."""
    t = _strip(t)
    if t.endswith("[]"):
        return [to_arg(x, t[:-2]) for x in (v or [])]
    if t.startswith("list<") and t.endswith(">"):
        return [to_arg(x, t[5:-1]) for x in (v or [])]
    if t == "TreeNode":
        return build_tree(v)
    if t == "ListNode":
        return build_list(v)
    return v


def from_ret(v, t):
    """Return value -> JSON, following the declared type."""
    t = _strip(t)
    if t == "TreeNode":
        return ser_tree(v)
    if t == "ListNode":
        return ser_list(v)
    if v is None:
        return None
    if t.endswith("[]"):
        return [from_ret(x, t[:-2]) for x in v]
    if t.startswith("list<") and t.endswith(">"):
        return [from_ret(x, t[5:-1]) for x in v]
    return v


def run_design(mod, meta, raw):
    """Design problems ("systemdesign"): LeetCode records a call sequence as
    two arrays — operation names and their argument lists — and expects the
    array of return values, with null for the constructor and void methods."""
    ops, argsets = raw[0], raw[1]
    cls = getattr(mod, meta["classname"])
    by_name = {m["name"]: m for m in meta.get("methods") or []}
    ctor = (meta.get("constructor") or {}).get("params") or []

    out, inst = [], None
    for i, (op, a) in enumerate(zip(ops, argsets)):
        if i == 0 or inst is None:      # first entry constructs the object
            args = [to_arg(v, p.get("type", "")) for v, p in zip(a, ctor)]
            inst = cls(*args)
            out.append(None)
            continue
        spec = by_name.get(op, {})
        ptypes = [p.get("type", "") for p in spec.get("params") or []]
        args = [to_arg(v, t) for v, t in zip(a, ptypes)]
        res = getattr(inst, op)(*args)
        rtype = _strip((spec.get("return") or {}).get("type", ""))
        out.append(None if rtype == "void" else from_ret(res, rtype))
    return out


def main():
    src, meta_json = sys.argv[1], sys.argv[2]
    meta = json.loads(meta_json)

    spec = importlib.util.spec_from_file_location("solution", src)
    mod = importlib.util.module_from_spec(spec)
    # Inject the node classes the way LeetCode does, so solutions can use
    # TreeNode/ListNode (including in annotations) without defining them.
    mod.ListNode = ListNode
    mod.TreeNode = TreeNode
    # LeetCode's editor stub is written against a preloaded environment: its
    # `class Solution` annotations say List[int] with no import in sight. dojo
    # writes that stub verbatim so it pastes back unchanged, so the names have
    # to exist here instead.
    for name in ("List", "Optional", "Dict", "Set", "Tuple", "Any", "Union", "Deque"):
        setattr(mod, name, getattr(typing, name, None))
    spec.loader.exec_module(mod)

    raw = json.loads(sys.stdin.read())

    if meta.get("classname"):
        print(json.dumps(run_design(mod, meta, raw)))
        return

    method = meta["name"]
    ptypes = [p.get("type", "") for p in meta.get("params") or []]
    rtype = _strip((meta.get("return") or {}).get("type", ""))
    args = [to_arg(v, t) for v, t in zip(raw, ptypes)]

    result = getattr(mod.Solution(), method)(*args)

    if rtype == "void":
        # in-place problems: the answer is the mutated first argument
        out = from_ret(args[0], ptypes[0]) if args else None
    else:
        out = from_ret(result, rtype)
    print(json.dumps(out))


if __name__ == "__main__":
    main()
