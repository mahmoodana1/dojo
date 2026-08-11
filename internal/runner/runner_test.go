package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunAllPython(t *testing.T) {
	src := filepath.Join(t.TempDir(), "solution.py")
	code := "import sys\nprint(sys.stdin.read().strip().upper())\n"
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := RunAll(context.Background(), Registry["python"], src, []Case{
		{Input: "abc", Expected: "ABC"},
		{Input: "dojo", Expected: "WRONG"},
	}, "")
	if rep.CompileErr != "" {
		t.Fatalf("unexpected compile error: %s", rep.CompileErr)
	}
	if rep.Passed != 1 || rep.Total != 2 {
		t.Fatalf("want 1/2 passed, got %d/%d", rep.Passed, rep.Total)
	}
	if !rep.Results[0].Pass || rep.Results[1].Pass {
		t.Fatalf("wrong per-case verdicts: %+v", rep.Results)
	}
}

func TestRunAllCCompiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "solution.c")
	code := `#include <stdio.h>
int main(void){int a,b;scanf("%d %d",&a,&b);printf("%d\n",a+b);return 0;}`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := RunAll(context.Background(), Registry["c"], src, []Case{
		{Input: "2 3", Expected: "5"},
	}, "")
	if rep.CompileErr != "" {
		t.Fatalf("compile error: %s", rep.CompileErr)
	}
	if rep.Passed != 1 {
		t.Fatalf("want pass, got %+v", rep.Results)
	}
}

func TestEnsureSolutionFileDoesNotClobber(t *testing.T) {
	root := t.TempDir()
	meta := SolutionMeta{FrontendID: 1, Title: "Two Sum", Slug: "two-sum", Difficulty: "Easy"}
	p1, err := EnsureSolutionFile(root, meta, Registry["python"], nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1, []byte("my work"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := EnsureSolutionFile(root, meta, Registry["python"], nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p2)
	if string(got) != "my work" {
		t.Fatal("EnsureSolutionFile overwrote an existing solution")
	}
}
