package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// LeetCode's own editor stubs, verbatim, for the three families that have no
// class Solution tab at all.
const (
	sqlSnippet    = "# Write your MySQL query statement below\n"
	bashSnippet   = "# Read from the file file.txt and output the tenth line to stdout.\n"
	pandasSnippet = "import pandas as pd\n\ndef selectFirstRows(employees: pd.DataFrame) -> pd.DataFrame:\n    "
	salesMeta     = `{"mysql":["Create table If Not Exists Sales (sale_id int, product_id int, year int, price int)",` +
		`"Create table If Not Exists Product (product_id int, product_name varchar(10))"],"database":true}`
)

func sqlInfo() CodeInfo {
	return CodeInfo{JudgeMode: "raw", MetaJSON: salesMeta,
		Snippets: map[string]string{"mysql": sqlSnippet, "pythondata": pandasSnippet}}
}

var sm = SolutionMeta{FrontendID: 1068, Title: "Product Sales Analysis I",
	Slug: "product-sales-analysis-i", Difficulty: "Easy"}

// The whole point: what dojo writes is what LeetCode shows, so a finished
// solution pastes back into the site with nothing to strip out.
func TestStubIsLeetCodeVerbatim(t *testing.T) {
	got, err := Stub(sm, Registry["mysql"], sqlInfo())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, sqlSnippet) {
		t.Fatalf("stub does not end in LeetCode's snippet:\n%s", got)
	}
	if strings.Contains(got, "def solve(") {
		t.Fatalf("SQL problem got the python I/O scaffold:\n%s", got)
	}
	// the dojo header must be commented in the target language
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			break
		}
		if strings.Contains(line, "leetcode.com") && !strings.HasPrefix(line, "# ") {
			t.Fatalf("header is not a SQL comment: %q", line)
		}
	}
}

func TestStubCommentSyntaxPerLanguage(t *testing.T) {
	info := CodeInfo{Snippets: map[string]string{
		"javascript": "/**\n * @param {number} millis\n */\nvar sleep = async function(millis) {\n    \n};\n"}}
	got, err := Stub(sm, Registry["javascript"], info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "// 1068.") {
		t.Fatalf("javascript header must use //:\n%s", got)
	}
}

// With no snippet fetched, python still gets the synthesised LeetCode
// signature rather than the I/O scaffold.
func TestStubFallsBackToTypedSignature(t *testing.T) {
	info := CodeInfo{JudgeMode: "typed", MetaJSON: twoSumMeta}
	got, err := Stub(sm, Registry["python"], info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "def twoSum(self, nums: List[int], target: int) -> List[int]:") {
		t.Fatalf("missing synthesised signature:\n%s", got)
	}
}

// LeetCode's python3 snippets annotate List[int] without importing typing —
// its judge has the name in scope. The file dojo writes has to stand on its
// own for editors, linters and `python3 solution.py`, so the import goes in.
func TestStubImportsTypingForSnippet(t *testing.T) {
	info := CodeInfo{Snippets: map[string]string{
		"python3": "class Solution:\n    def decompressRLElist(self, nums: List[int]) -> List[int]:\n        \n"}}
	got, err := Stub(sm, Registry["python"], info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "from typing import List\n") {
		t.Fatalf("snippet kept LeetCode's missing import:\n%s", got)
	}
	if strings.Index(got, "from typing import") > strings.Index(got, "class Solution") {
		t.Fatalf("import must precede the class:\n%s", got)
	}
	// The old import-less body is still a stub nobody has worked in.
	old := header(sm, Registry["python"]) + info.Snippets["python3"]
	if !PristineStub(old, sm, Registry["python"], info) {
		t.Fatalf("pre-import stub no longer recognised as pristine:\n%s", old)
	}
}

// Only names the snippet actually subscripts get imported, and ListNode must
// not be mistaken for List. A snippet that imports typing itself is untouched.
func TestTypingImportIsScopedToWhatIsUsed(t *testing.T) {
	got := withTypingImport("# class ListNode:\nclass Solution:\n" +
		"    def f(self, head: Optional[ListNode]) -> Optional[ListNode]:\n        ...\n")
	if !strings.HasPrefix(got, "from typing import Optional\n") {
		t.Fatalf("want Optional only, got:\n%s", got)
	}
	already := "from typing import List\nclass Solution:\n    def f(self, n: List[int]): ...\n"
	if withTypingImport(already) != already {
		t.Fatalf("snippet that imports typing itself was rewritten")
	}
	none := "class Solution:\n    def f(self, n: int) -> int:\n        ...\n"
	if withTypingImport(none) != none {
		t.Fatalf("snippet with no typing use gained an import")
	}
}

// A premium problem has no published stub to mirror; the file should say
// that rather than imply an I/O judging contract that does not exist.
func TestPaidOnlyStub(t *testing.T) {
	got, err := Stub(sm, Registry["python"], CodeInfo{PaidOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "def solve(") {
		t.Fatalf("premium problem got the I/O scaffold:\n%s", got)
	}
	if !strings.Contains(got, "premium-only") {
		t.Fatalf("premium problem should say so:\n%s", got)
	}
}

func TestLanguagesMirrorLeetCodeTabs(t *testing.T) {
	cases := []struct {
		name string
		info CodeInfo
		want []string
	}{
		{"database", sqlInfo(), []string{"mysql", "pandas"}},
		{"shell", CodeInfo{Snippets: map[string]string{"bash": bashSnippet}}, []string{"bash"}},
		{"algorithm", CodeInfo{Snippets: map[string]string{
			"python3": "class Solution:", "cpp": "class Solution {", "c": "int f();",
			"java": "class Solution {}"}}, []string{"python", "cpp", "c"}},
		{"javascript only", CodeInfo{Snippets: map[string]string{"javascript": "var f;"}},
			[]string{"javascript"}},
		// snippets not fetched yet: fall back to the family in metaData
		{"database, no snippets", CodeInfo{MetaJSON: salesMeta}, []string{"mysql"}},
		{"nothing known", CodeInfo{}, []string{"python", "cpp", "c"}},
	}
	for _, c := range cases {
		got := c.info.Languages()
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: languages = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPlanPicksTheJudge(t *testing.T) {
	cases := []struct {
		name string
		info CodeInfo
		lang string
		want JudgePlan
	}{
		{"sql", sqlInfo(), "mysql", JudgeSQL},
		{"pandas tab of a sql problem", sqlInfo(), "pandas", JudgePandas},
		{"typed python", CodeInfo{JudgeMode: "typed", MetaJSON: twoSumMeta}, "python", JudgeTyped},
		{"shell", CodeInfo{Snippets: map[string]string{"bash": bashSnippet}}, "bash", JudgeNone},
		{"javascript", CodeInfo{Snippets: map[string]string{"javascript": "var f;"}},
			"javascript", JudgeNone},
		// LeetCode stub written, but the examples never became typed
		// arguments (in-place mutation): judging it raw would be nonsense.
		{"custom judge", CodeInfo{JudgeMode: "raw", MetaJSON: twoSumMeta,
			Snippets: map[string]string{"python3": "class Solution:"}}, "python", JudgeNone},
		{"plain io fallback", CodeInfo{}, "python", JudgeRaw},
	}
	for _, c := range cases {
		got, why := c.info.Plan(Registry[c.lang])
		if got != c.want {
			t.Errorf("%s: plan = %v, want %v", c.name, got, c.want)
		}
		if got == JudgeNone && why == "" {
			t.Errorf("%s: JudgeNone with no explanation", c.name)
		}
	}
}

// 1068 end to end: LeetCode's stub, a real query, the statement's example.
func TestSQLJudging(t *testing.T) {
	const input = `Sales table:
+---------+------------+------+-------+
| sale_id | product_id | year | price |
+---------+------------+------+-------+
| 1       | 100        | 2008 | 5000  |
| 2       | 100        | 2009 | 5000  |
| 7       | 200        | 2011 | 9000  |
+---------+------------+------+-------+
Product table:
+------------+--------------+
| product_id | product_name |
+------------+--------------+
| 100        | Nokia        |
| 200        | Apple        |
+------------+--------------+`
	const expected = `+--------------+-------+-------+
| product_name | year  | price |
+--------------+-------+-------+
| Nokia        | 2008  | 5000  |
| Nokia        | 2009  | 5000  |
| Apple        | 2011  | 9000  |
+--------------+-------+-------+`

	run := func(query string) Report {
		src := filepath.Join(t.TempDir(), "solution.sql")
		if err := os.WriteFile(src, []byte(sqlSnippet+query), 0o644); err != nil {
			t.Fatal(err)
		}
		return RunAll(context.Background(), Registry["mysql"], src,
			[]Case{{Input: input, Expected: expected}}, sqlInfo())
	}

	rep := run(`SELECT p.product_name, s.year, s.price
	            FROM Sales s JOIN Product p ON p.product_id = s.product_id;`)
	if rep.Passed != 1 {
		t.Fatalf("correct query rejected: %+v", rep.Results)
	}

	rep = run(`SELECT p.product_name, s.year FROM Sales s
	           JOIN Product p ON p.product_id = s.product_id;`)
	if rep.Passed != 0 {
		t.Fatal("query with a missing column was accepted")
	}

	rep = run(`SELECT DATE_ADD(NOW(), INTERVAL 1 DAY);`)
	if rep.Passed != 0 || !strings.Contains(rep.Results[0].Stderr, "SQLite") {
		t.Fatalf("MySQL-only syntax should fail loudly: %+v", rep.Results)
	}
}

// The pandas tab of a database problem: frames built from metaData, filled
// from the statement's example, handed to the function by parameter name.
func TestPandasJudging(t *testing.T) {
	if err := exec.Command("python3", "-c", "import pandas").Run(); err != nil {
		t.Skip("pandas not installed")
	}
	const meta = `{"pythondata":["employees = pd.DataFrame([], columns=['employee_id', 'name'])` +
		`.astype({'employee_id':'Int64', 'name':'object'})"],"database":true,"name":"selectFirstRows"}`
	const input = `Employees table:
+-------------+-------+
| employee_id | name  |
+-------------+-------+
| 3           | Bob   |
| 90          | Alice |
| 9           | Tati  |
| 60          | John  |
+-------------+-------+`
	const expected = `+-------------+-------+
| employee_id | name  |
+-------------+-------+
| 3           | Bob   |
| 90          | Alice |
| 9           | Tati  |
+-------------+-------+`

	info := CodeInfo{JudgeMode: "raw", MetaJSON: meta,
		Snippets: map[string]string{"pythondata": pandasSnippet}}
	src := filepath.Join(t.TempDir(), "solution.pandas.py")
	code := "import pandas as pd\n\n" +
		"def selectFirstRows(employees: pd.DataFrame) -> pd.DataFrame:\n    return employees.head(3)\n"
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := RunAll(context.Background(), Registry["pandas"], src,
		[]Case{{Input: input, Expected: expected}}, info)
	if rep.Passed != 1 {
		t.Fatalf("correct pandas solution rejected: %+v", rep.Results)
	}
}

// Not every pandas problem is "one frame in, one frame out": some take a
// scalar the example gives as `n = 2`, some take a list literal instead of a
// table. The parameters bind by name either way.
func TestPandasBindsNonFrameArguments(t *testing.T) {
	if err := exec.Command("python3", "-c", "import pandas").Run(); err != nil {
		t.Skip("pandas not installed")
	}
	cases := []struct {
		name, meta, code, input, expected string
	}{
		{
			name: "scalar parameter",
			meta: `{"pythondata":["Employee = pd.DataFrame([], columns=['id', 'salary'])"],"name":"nth"}`,
			code: "import pandas as pd\n\ndef nth(employee: pd.DataFrame, N: int) -> pd.DataFrame:\n" +
				"    s = employee['salary'].drop_duplicates().nlargest(N)\n" +
				"    return pd.DataFrame({'nth': [s.iloc[N - 1]]})\n",
			input: "Employee table:\n+----+--------+\n| id | salary |\n+----+--------+\n" +
				"| 1  | 100    |\n| 2  | 200    |\n| 3  | 300    |\n+----+--------+\nN = 2",
			expected: "+-----+\n| nth |\n+-----+\n| 200 |\n+-----+",
		},
		{
			name: "list literal instead of a table",
			meta: `{"pythondata":[],"name":"createDataframe"}`,
			code: "import pandas as pd\n\ndef createDataframe(student_data: list) -> pd.DataFrame:\n" +
				"    return pd.DataFrame(student_data, columns=['student_id', 'age'])\n",
			input:    "student_data:\n[\n  [1, 15],\n  [2, 11]\n]",
			expected: "+------------+-----+\n| student_id | age |\n+------------+-----+\n| 1          | 15  |\n| 2          | 11  |\n+------------+-----+",
		},
	}
	for _, c := range cases {
		src := filepath.Join(t.TempDir(), "solution.pandas.py")
		if err := os.WriteFile(src, []byte(c.code), 0o644); err != nil {
			t.Fatal(err)
		}
		rep := RunAll(context.Background(), Registry["pandas"], src,
			[]Case{{Input: c.input, Expected: c.expected}},
			CodeInfo{MetaJSON: c.meta, Snippets: map[string]string{"pythondata": pandasSnippet}})
		if rep.Passed != 1 {
			t.Errorf("%s: %+v", c.name, rep.Results)
		}
	}
}

func TestTableEqual(t *testing.T) {
	const want = `+------+-------+
| name | score |
+------+-------+
| a    | 5000  |
| b    | 10    |
+------+-------+`
	cases := []struct {
		name string
		got  string
		ok   bool
	}{
		{"same order", "name\tscore\na\t5000\nb\t10", true},
		{"rows in any order", "name\tscore\nb\t10\na\t5000", true},
		{"columns in any order", "score\tname\n5000\ta\n10\tb", true},
		{"decimal spelling", "name\tscore\na\t5000.00\nb\t10.0", true},
		{"missing row", "name\tscore\na\t5000", false},
		{"extra row", "name\tscore\na\t5000\nb\t10\nc\t1", false},
		{"wrong value", "name\tscore\na\t5001\nb\t10", false},
		{"wrong column name", "name\tpoints\na\t5000\nb\t10", false},
		{"empty output", "", false},
	}
	for _, c := range cases {
		if got := tableEqual(want, c.got); got != c.ok {
			t.Errorf("%s: tableEqual = %v, want %v", c.name, got, c.ok)
		}
	}
	// a result set with no rows still has to match on columns
	if !tableEqual("+---+\n| a |\n+---+", "a") {
		t.Error("empty result sets should compare equal")
	}
}

// The python scaffold an older dojo wrote for a database problem is cleared
// away when the real SQL stub lands — but only while it is still empty.
func TestStaleScaffoldCleanup(t *testing.T) {
	py, mysql := Registry["python"], Registry["mysql"]

	t.Run("pristine scaffold is removed", func(t *testing.T) {
		root := t.TempDir()
		stale, _, err := EnsureSolutionFile(root, sm, py, CodeInfo{})
		if err != nil {
			t.Fatal(err)
		}
		_, note, err := EnsureSolutionFile(root, sm, mysql, sqlInfo())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(note, "solution.py") {
			t.Errorf("cleanup not reported, note = %q", note)
		}
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Error("empty python scaffold survived")
		}
	})

	t.Run("work is never removed", func(t *testing.T) {
		root := t.TempDir()
		stale, _, err := EnsureSolutionFile(root, sm, py, CodeInfo{})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stale, []byte(pythonScaffoldWithWork), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := EnsureSolutionFile(root, sm, mysql, sqlInfo()); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(stale); err != nil || !strings.Contains(string(b), "my hard work") {
			t.Fatal("a file with work in it was removed")
		}
	})

	t.Run("finished solution is not rewritten", func(t *testing.T) {
		root := t.TempDir()
		path, _, err := EnsureSolutionFile(root, sm, mysql, sqlInfo())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("SELECT 1;"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, note, _ := EnsureSolutionFile(root, sm, mysql, sqlInfo()); note != "" {
			t.Errorf("finished solution reported a migration: %q", note)
		}
		if b, _ := os.ReadFile(path); string(b) != "SELECT 1;" {
			t.Error("finished SQL solution was overwritten")
		}
	})
}

// `dojo -refresh-stubs`: solution files written by an older dojo and never
// worked in are brought up to date; anything else is left exactly as it is.
func TestRefreshSolutionDir(t *testing.T) {
	root := t.TempDir()
	py, cpp := Registry["python"], Registry["cpp"]

	// as an older dojo left it: an I/O scaffold for what is really a SQL
	// problem, plus an untouched C++ template
	if _, _, err := EnsureSolutionFile(root, sm, py, CodeInfo{}); err != nil {
		t.Fatal(err)
	}
	cppPath, _, err := EnsureSolutionFile(root, sm, cpp, CodeInfo{})
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(cppPath)
	changes, kept, err := RefreshSolutionDir(dir, sm, sqlInfo())
	if err != nil {
		t.Fatal(err)
	}
	if kept != 0 {
		t.Errorf("nothing had work in it, but %d file(s) reported kept", kept)
	}
	joined := strings.Join(changes, "; ")
	if !strings.Contains(joined, "removed solution.py") ||
		!strings.Contains(joined, "created solution.sql") {
		t.Fatalf("unexpected changes: %v", changes)
	}
	if _, err := os.Stat(filepath.Join(dir, "solution.py")); !os.IsNotExist(err) {
		t.Error("the python scaffold should be gone")
	}
	body, err := os.ReadFile(filepath.Join(dir, "solution.sql"))
	if err != nil || !strings.HasSuffix(string(body), sqlSnippet) {
		t.Fatalf("solution.sql missing or wrong: %v\n%s", err, body)
	}
	// C++ is not a LeetCode tab for a database problem either, so its
	// untouched template goes too
	if _, err := os.Stat(cppPath); !os.IsNotExist(err) {
		t.Error("the C++ template should have been removed as well")
	}

	// a second run is a no-op
	if changes, _, err := RefreshSolutionDir(dir, sm, sqlInfo()); err != nil || len(changes) != 0 {
		t.Errorf("refresh is not idempotent: %v %v", changes, err)
	}

	// work is kept, byte for byte
	work := filepath.Join(dir, "solution.sql")
	const query = "SELECT 1;\n"
	if err := os.WriteFile(work, []byte(query), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, kept, err = RefreshSolutionDir(dir, sm, sqlInfo())
	if err != nil {
		t.Fatal(err)
	}
	if kept != 1 || len(changes) != 0 {
		t.Errorf("a written solution was touched: kept=%d changes=%v", kept, changes)
	}
	if b, _ := os.ReadFile(work); string(b) != query {
		t.Error("a written solution was modified")
	}
}

// C/C++ keep dojo's I/O template even when LeetCode has a tab for them:
// their only local judge is stdin/stdout, and `class Solution` has no main().
func TestCppKeepsIOTemplate(t *testing.T) {
	info := CodeInfo{JudgeMode: "typed", MetaJSON: twoSumMeta, Snippets: map[string]string{
		"python3": "class Solution:\n    def twoSum(self):\n        ",
		"cpp":     "class Solution {\npublic:\n    vector<int> twoSum() {\n        \n    }\n};",
	}}
	got, err := Stub(sm, Registry["cpp"], info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "int main()") {
		t.Fatalf("C++ stub must stay runnable:\n%s", got)
	}
	if plan, _ := info.Plan(Registry["cpp"]); plan != JudgeRaw {
		t.Fatalf("C++ should judge raw, got %v", plan)
	}
}

const pythonScaffoldWithWork = `import sys


def solve(data: str) -> str:
    return "my hard work"


if __name__ == "__main__":
    sys.stdout.write(str(solve(sys.stdin.read().strip())))
`
