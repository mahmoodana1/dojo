package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const twoSumMeta = `{"name":"twoSum","params":[{"name":"nums","type":"integer[]"},{"name":"target","type":"integer"}],"return":{"type":"integer[]"}}`

// typedInfo is a problem whose examples parsed into typed arguments.
func typedInfo(metaJSON string) CodeInfo {
	return CodeInfo{JudgeMode: "typed", MetaJSON: metaJSON}
}

func mustMeta(t *testing.T, s string) *Meta {
	t.Helper()
	m, err := ParseMeta(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// runTyped writes code to a temp file and judges it against the given cases.
func runTyped(t *testing.T, meta, code string, cases []Case) Report {
	t.Helper()
	src := filepath.Join(t.TempDir(), "solution.py")
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	return RunAll(context.Background(), Registry["python"], src, cases, typedInfo(meta))
}

// 938. Range Sum of BST — the problem that exposed the missing TreeNode
// support. The solution is written exactly as it would be on LeetCode.
func TestTypedTreeNodeArgument(t *testing.T) {
	const meta = `{"name":"rangeSumBST","params":[{"name":"root","type":"TreeNode"},` +
		`{"name":"low","type":"integer"},{"name":"high","type":"integer"}],` +
		`"return":{"type":"integer"}}`
	code := `class Solution:
    def rangeSumBST(self, root, low, high):
        if root is None:
            return 0
        if root.val < low:
            return self.rangeSumBST(root.right, low, high)
        if root.val > high:
            return self.rangeSumBST(root.left, low, high)
        return (root.val
                + self.rangeSumBST(root.left, low, high)
                + self.rangeSumBST(root.right, low, high))
`
	rep := runTyped(t, meta, code, []Case{
		{ArgsJSON: `[[10,5,15,3,7,null,18], 7, 15]`, ExpectedJSON: `32`},
		{ArgsJSON: `[[10,5,15,3,7,13,18,1,null,6], 6, 10]`, ExpectedJSON: `23`},
	})
	if rep.Passed != 2 {
		t.Fatalf("want 2/2, got %d/%d (%+v)", rep.Passed, rep.Total, rep.Results)
	}
}

// A TreeNode return value must serialise back to LeetCode's level-order
// array with trailing nulls trimmed.
func TestTypedTreeNodeReturn(t *testing.T) {
	const meta = `{"name":"invertTree","params":[{"name":"root","type":"TreeNode"}],` +
		`"return":{"type":"TreeNode"}}`
	code := `class Solution:
    def invertTree(self, root):
        if root:
            root.left, root.right = self.invertTree(root.right), self.invertTree(root.left)
        return root
`
	rep := runTyped(t, meta, code, []Case{
		{ArgsJSON: `[[4,2,7,1,3,6,9]]`, ExpectedJSON: `[4,7,2,9,6,3,1]`},
		{ArgsJSON: `[[2,1,3]]`, ExpectedJSON: `[2,3,1]`},
		{ArgsJSON: `[[]]`, ExpectedJSON: `[]`},
	})
	if rep.Passed != 3 {
		t.Fatalf("want 3/3, got %d/%d (%+v)", rep.Passed, rep.Total, rep.Results)
	}
}

func TestTypedListNode(t *testing.T) {
	const meta = `{"name":"addTwoNumbers","params":[{"name":"l1","type":"ListNode"},` +
		`{"name":"l2","type":"ListNode"}],"return":{"type":"ListNode"}}`
	code := `class Solution:
    def addTwoNumbers(self, l1, l2):
        head = cur = ListNode()
        carry = 0
        while l1 or l2 or carry:
            s = carry + (l1.val if l1 else 0) + (l2.val if l2 else 0)
            carry, d = divmod(s, 10)
            cur.next = ListNode(d)
            cur = cur.next
            l1 = l1.next if l1 else None
            l2 = l2.next if l2 else None
        return head.next
`
	rep := runTyped(t, meta, code, []Case{
		{ArgsJSON: `[[2,4,3],[5,6,4]]`, ExpectedJSON: `[7,0,8]`},
		{ArgsJSON: `[[9,9,9,9],[9,9,9]]`, ExpectedJSON: `[8,9,9,0,1]`},
	})
	if rep.Passed != 2 {
		t.Fatalf("want 2/2, got %d/%d (%+v)", rep.Passed, rep.Total, rep.Results)
	}
}

// void return: the answer is the mutated first argument.
func TestTypedVoidReturnMutatesArg(t *testing.T) {
	const meta = `{"name":"sortColors","params":[{"name":"nums","type":"integer[]"}],` +
		`"return":{"type":"void"}}`
	code := `class Solution:
    def sortColors(self, nums):
        nums.sort()
`
	rep := runTyped(t, meta, code, []Case{
		{ArgsJSON: `[[2,0,2,1,1,0]]`, ExpectedJSON: `[0,0,1,1,2,2]`},
		{ArgsJSON: `[[2,0,1]]`, ExpectedJSON: `[0,1,2]`},
	})
	if rep.Passed != 2 {
		t.Fatalf("want 2/2, got %d/%d (%+v)", rep.Passed, rep.Total, rep.Results)
	}
}

// character[][] grids must survive the round trip unchanged.
func TestTypedCharacterGrid(t *testing.T) {
	const meta = `{"name":"numIslands","params":[{"name":"grid","type":"character[][]"}],` +
		`"return":{"type":"integer"}}`
	code := `class Solution:
    def numIslands(self, grid):
        if not grid:
            return 0
        n = 0
        def sink(r, c):
            if 0 <= r < len(grid) and 0 <= c < len(grid[0]) and grid[r][c] == "1":
                grid[r][c] = "0"
                for dr, dc in ((1,0),(-1,0),(0,1),(0,-1)):
                    sink(r+dr, c+dc)
                return 1
            return 0
        for r in range(len(grid)):
            for c in range(len(grid[0])):
                n += sink(r, c)
        return n
`
	rep := runTyped(t, meta, code, []Case{
		{ArgsJSON: `[[["1","1","0"],["1","1","0"],["0","0","1"]]]`, ExpectedJSON: `2`},
	})
	if rep.Passed != 1 {
		t.Fatalf("want 1/1, got %d/%d (%+v)", rep.Passed, rep.Total, rep.Results)
	}
}

// A solution file written before the problem gained typed judging is a raw
// stdin scaffold, which can never pass the typed driver. Opening it must
// migrate it — without losing anything the user wrote.
func TestMigrateStaleRawStub(t *testing.T) {
	const rawStub = `# 938. Range Sum of BST  [Easy]
#
# dojo judges by I/O: read the raw case text from stdin, print the answer.
import sys


def solve(data: str) -> str:
    # your solution here
    return data


if __name__ == "__main__":
    sys.stdout.write(str(solve(sys.stdin.read().strip())))
`
	const treeMeta = `{"name":"rangeSumBST","params":[{"name":"root","type":"TreeNode"},` +
		`{"name":"low","type":"integer"},{"name":"high","type":"integer"}],` +
		`"return":{"type":"integer"}}`
	sm := SolutionMeta{FrontendID: 938, Title: "Range Sum of BST",
		Slug: "range-sum-of-bst", Difficulty: "Easy"}
	py := Registry["python"]

	t.Run("pristine scaffold is replaced outright", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "0938-range-sum-of-bst")
		os.MkdirAll(dir, 0o755)
		path := filepath.Join(dir, "solution.py")
		os.WriteFile(path, []byte(rawStub), 0o644)

		_, note, err := EnsureSolutionFile(root, sm, py, typedInfo(treeMeta))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), "def rangeSumBST(self, root: Optional[TreeNode]") {
			t.Fatalf("stub was not migrated:\n%s", b)
		}
		if note == "" {
			t.Error("migration should report a note for the status bar")
		}
		if _, err := os.Stat(filepath.Join(dir, "solution.rawmode-backup.py")); err == nil {
			t.Error("a pristine scaffold needs no backup")
		}
	})

	t.Run("edited file is backed up, never lost", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "0938-range-sum-of-bst")
		os.MkdirAll(dir, 0o755)
		path := filepath.Join(dir, "solution.py")
		edited := strings.Replace(rawStub, "    return data", `    return "99"`, 1)
		os.WriteFile(path, []byte(edited), 0o644)

		_, note, err := EnsureSolutionFile(root, sm, py, typedInfo(treeMeta))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), "def rangeSumBST(self, root: Optional[TreeNode]") {
			t.Fatalf("stub was not migrated:\n%s", b)
		}
		backup, err := os.ReadFile(filepath.Join(dir, "solution.rawmode-backup.py"))
		if err != nil {
			t.Fatalf("edited file must be backed up: %v", err)
		}
		if string(backup) != edited {
			t.Error("backup does not match the original byte-for-byte")
		}
		if !strings.Contains(note, "rawmode-backup") {
			t.Errorf("note should name the backup, got %q", note)
		}
	})

	t.Run("real typed work is left completely alone", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "0938-range-sum-of-bst")
		os.MkdirAll(dir, 0o755)
		path := filepath.Join(dir, "solution.py")
		work := "class Solution:\n    def rangeSumBST(self, root, low, high):\n        return 42\n"
		os.WriteFile(path, []byte(work), 0o644)

		_, note, err := EnsureSolutionFile(root, sm, py, typedInfo(treeMeta))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		if string(b) != work {
			t.Fatalf("existing typed solution must never be touched:\n%s", b)
		}
		if note != "" {
			t.Errorf("no migration expected, got note %q", note)
		}
	})
}

const lruMeta = `{"classname":"LRUCache","constructor":{"params":[{"type":"integer","name":"capacity"}]},` +
	`"methods":[{"name":"get","params":[{"type":"integer","name":"key"}],"return":{"type":"integer"}},` +
	`{"name":"put","params":[{"type":"integer","name":"key"},{"type":"integer","name":"value"}],` +
	`"return":{"type":"void"}}],"systemdesign":true}`

// Design problems: LeetCode replays a recorded call sequence and compares the
// array of return values, with null for the constructor and void methods.
func TestTypedDesignClassReplay(t *testing.T) {
	code := `from collections import OrderedDict


class LRUCache:

    def __init__(self, capacity: int):
        self.cap = capacity
        self.d = OrderedDict()

    def get(self, key: int) -> int:
        if key not in self.d:
            return -1
        self.d.move_to_end(key)
        return self.d[key]

    def put(self, key: int, value: int) -> None:
        if key in self.d:
            self.d.move_to_end(key)
        self.d[key] = value
        if len(self.d) > self.cap:
            self.d.popitem(last=False)
`
	rep := runTyped(t, lruMeta, code, []Case{{
		ArgsJSON: `[["LRUCache","put","put","get","put","get","put","get","get","get"],` +
			`[[2],[1,1],[2,2],[1],[3,3],[2],[4,4],[1],[3],[4]]]`,
		ExpectedJSON: `[null,null,null,1,null,-1,null,-1,3,4]`,
	}})
	if rep.Passed != 1 {
		t.Fatalf("want 1/1, got %d/%d (%+v)", rep.Passed, rep.Total, rep.Results)
	}
}

// A wrong design implementation must actually fail, or the replay harness is
// not really checking anything.
func TestTypedDesignClassCatchesWrongAnswer(t *testing.T) {
	code := `class LRUCache:
    def __init__(self, capacity: int):
        self.d = {}                      # no eviction, no LRU ordering

    def get(self, key: int) -> int:
        return self.d.get(key, -1)

    def put(self, key: int, value: int) -> None:
        self.d[key] = value
`
	rep := runTyped(t, lruMeta, code, []Case{{
		ArgsJSON: `[["LRUCache","put","put","get","put","get","put","get","get","get"],` +
			`[[2],[1,1],[2,2],[1],[3,3],[2],[4,4],[1],[3],[4]]]`,
		ExpectedJSON: `[null,null,null,1,null,-1,null,-1,3,4]`,
	}})
	if rep.Passed != 0 {
		t.Fatalf("a cache with no eviction must fail, got %d/%d", rep.Passed, rep.Total)
	}
}

func TestDesignPythonStub(t *testing.T) {
	stub := typedPythonStub(SolutionMeta{FrontendID: 146, Title: "LRU Cache",
		Slug: "lru-cache", Difficulty: "Medium"}, mustMeta(t, lruMeta))
	for _, want := range []string{
		"class LRUCache:",
		"def __init__(self, capacity: int) -> None:",
		"def get(self, key: int) -> int:",
		"def put(self, key: int, value: int) -> None:",
		"# Your LRUCache object will be instantiated and called as such:",
		"# obj = LRUCache(capacity)",
		"# param_1 = obj.get(key)",
		"# obj.put(key, value)",
	} {
		if !strings.Contains(stub, want) {
			t.Fatalf("design stub missing %q:\n%s", want, stub)
		}
	}
}

// The generated stub must match LeetCode's, including the commented-out node
// class definition and the Optional[...] annotation.
func TestTypedStubForTreeProblem(t *testing.T) {
	const meta = `{"name":"rangeSumBST","params":[{"name":"root","type":"TreeNode"},` +
		`{"name":"low","type":"integer"},{"name":"high","type":"integer"}],` +
		`"return":{"type":"integer"}}`
	stub := typedPythonStub(SolutionMeta{FrontendID: 938, Title: "Range Sum of BST",
		Slug: "range-sum-of-bst", Difficulty: "Easy"}, mustMeta(t, meta))
	for _, want := range []string{
		"def rangeSumBST(self, root: Optional[TreeNode], low: int, high: int) -> int:",
		"# Definition for a binary tree node.",
		"#     def __init__(self, val=0, left=None, right=None):",
	} {
		if !strings.Contains(stub, want) {
			t.Fatalf("stub missing %q:\n%s", want, stub)
		}
	}
	if strings.Contains(stub, "def solve(data: str)") {
		t.Error("tree problem must not fall back to the raw I/O stub")
	}
}

func TestTypedJudgingEndToEnd(t *testing.T) {
	src := filepath.Join(t.TempDir(), "solution.py")
	code := `class Solution:
    def twoSum(self, nums, target):
        seen = {}
        for i, n in enumerate(nums):
            if target - n in seen:
                return [seen[target - n], i]
            seen[n] = i
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := RunAll(context.Background(), Registry["python"], src, []Case{
		{ArgsJSON: `[[2,7,11,15], 9]`, ExpectedJSON: `[0, 1]`},
		{ArgsJSON: `[[3,2,4], 6]`, ExpectedJSON: `[1, 2]`},
		{ArgsJSON: `[[3,3], 6]`, ExpectedJSON: `[0, 1]`},
		{ArgsJSON: `[[1,2], 99]`, ExpectedJSON: `[0, 1]`}, // returns null -> fail
	}, typedInfo(twoSumMeta))
	if rep.Passed != 3 || rep.Total != 4 {
		t.Fatalf("want 3/4 passed, got %d/%d (%+v)", rep.Passed, rep.Total, rep.Results)
	}
}

func TestJSONEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`[0,1]`, `[0, 1]`, true},
		{`3`, `3.0`, true},
		{`3.0000001`, `3`, true},
		{`"abc"`, `"abc"`, true},
		{`true`, `true`, true},
		{`[[1,2],[3,4]]`, `[[1,2],[3,4]]`, true},
		{`[0,1]`, `[1,0]`, false},
		{`"3"`, `3`, false},
		{`null`, `0`, false},
	}
	for _, c := range cases {
		if got := jsonEqualStr(c.a, c.b); got != c.want {
			t.Errorf("jsonEqualStr(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestTypedPythonStub(t *testing.T) {
	m, err := ParseMeta(twoSumMeta)
	if err != nil {
		t.Fatal(err)
	}
	stub := typedPythonStub(SolutionMeta{FrontendID: 1, Title: "Two Sum",
		Slug: "two-sum", Difficulty: "Easy"}, m)
	want := "def twoSum(self, nums: List[int], target: int) -> List[int]:"
	if !strings.Contains(stub, want) {
		t.Fatalf("stub missing signature %q:\n%s", want, stub)
	}
}
