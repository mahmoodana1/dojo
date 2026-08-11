package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const twoSumMeta = `{"name":"twoSum","params":[{"name":"nums","type":"integer[]"},{"name":"target","type":"integer"}],"return":{"type":"integer[]"}}`

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
	}, "twoSum")
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
