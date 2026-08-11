// Package runner is the language-agnostic local judge. A language is just a
// compile command (optional) plus a run command; adding a language is one
// Registry entry and one boilerplate template.
package runner

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/*.tmpl
var templates embed.FS

//go:embed typed_driver.py
var typedDriver string

// Language describes how to build and run a solution. {{src}} and {{bin}}
// are substituted with the solution path and a temp binary path.
type Language struct {
	Name     string
	Ext      string
	Template string   // file under templates/
	Compile  []string // empty for interpreted languages
	Run      []string
}

var Registry = map[string]Language{
	"python": {Name: "python", Ext: ".py", Template: "python.tmpl",
		Run: []string{"python3", "{{src}}"}},
	"c": {Name: "c", Ext: ".c", Template: "c.tmpl",
		Compile: []string{"gcc", "-O2", "-std=c17", "-o", "{{bin}}", "{{src}}"},
		Run:     []string{"{{bin}}"}},
	"cpp": {Name: "cpp", Ext: ".cpp", Template: "cpp.tmpl",
		Compile: []string{"g++", "-O2", "-std=c++20", "-o", "{{bin}}", "{{src}}"},
		Run:     []string{"{{bin}}"}},
}

// Order controls language cycling in the UI.
var Order = []string{"python", "cpp", "c"}

type Case struct {
	Input    string
	Expected string
	Hidden   bool
	// typed judging (v2): when both are set and the language supports it,
	// the case is judged through the typed driver instead of raw stdio.
	ArgsJSON     string
	ExpectedJSON string
}

// Meta is the parsed LeetCode metaData: the function signature.
type Meta struct {
	Name   string `json:"name"`
	Params []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"params"`
	Return struct {
		Type string `json:"type"`
	} `json:"return"`
}

func ParseMeta(metaJSON string) (*Meta, error) {
	var m Meta
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		return nil, err
	}
	if m.Name == "" {
		return nil, fmt.Errorf("metaData has no method name")
	}
	return &m, nil
}

type CaseResult struct {
	Index    int
	Pass     bool
	Hidden   bool
	Got      string
	Expected string
	Stderr   string
	Ms       int64
	TimedOut bool
}

type Report struct {
	CompileErr string
	Results    []CaseResult
	Passed     int
	Total      int
	TotalMs    int64
}

const perCaseTimeout = 5 * time.Second

func expand(args []string, src, bin string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		a = strings.ReplaceAll(a, "{{src}}", src)
		a = strings.ReplaceAll(a, "{{bin}}", bin)
		out[i] = a
	}
	return out
}

func normalize(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// jsonEqual deep-compares decoded JSON values; numbers use an epsilon so
// 3, 3.0 and 3.00000001 judge equal.
func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && math.Abs(x-y) < 1e-6
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !jsonEqual(v, y[k]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func jsonEqualStr(a, b string) bool {
	var va, vb any
	if json.Unmarshal([]byte(a), &va) != nil || json.Unmarshal([]byte(b), &vb) != nil {
		return false
	}
	return jsonEqual(va, vb)
}

// RunAll compiles (if needed) and judges src against every case. method is
// the LeetCode method name; when set, typed cases run through the typed
// driver (python only in phase 1) instead of raw stdio.
func RunAll(ctx context.Context, lang Language, src string, cases []Case, method string) Report {
	var rep Report
	rep.Total = len(cases)

	bin := filepath.Join(os.TempDir(), fmt.Sprintf("dojo-%d", time.Now().UnixNano()))
	if len(lang.Compile) > 0 {
		cmd := exec.CommandContext(ctx, lang.Compile[0], expand(lang.Compile[1:], src, bin)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			rep.CompileErr = strings.TrimSpace(string(out))
			if rep.CompileErr == "" {
				rep.CompileErr = err.Error()
			}
			return rep
		}
		defer os.Remove(bin)
	}

	var driverPath string
	if method != "" && lang.Name == "python" {
		driverPath = filepath.Join(os.TempDir(), fmt.Sprintf("dojo-driver-%d.py", time.Now().UnixNano()))
		if err := os.WriteFile(driverPath, []byte(typedDriver), 0o644); err == nil {
			defer os.Remove(driverPath)
		} else {
			driverPath = "" // fall back to raw judging
		}
	}

	runArgs := expand(lang.Run, src, bin)
	for i, tc := range cases {
		typed := driverPath != "" && tc.ArgsJSON != "" && tc.ExpectedJSON != ""

		cctx, cancel := context.WithTimeout(ctx, perCaseTimeout)
		var cmd *exec.Cmd
		if typed {
			cmd = exec.CommandContext(cctx, "python3", driverPath, src, method)
			cmd.Stdin = strings.NewReader(tc.ArgsJSON)
		} else {
			cmd = exec.CommandContext(cctx, runArgs[0], runArgs[1:]...)
			cmd.Stdin = strings.NewReader(tc.Input)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr

		start := time.Now()
		err := cmd.Run()
		elapsed := time.Since(start).Milliseconds()
		timedOut := cctx.Err() == context.DeadlineExceeded
		cancel()

		res := CaseResult{
			Index:    i + 1,
			Hidden:   tc.Hidden,
			Got:      normalize(stdout.String()),
			Stderr:   strings.TrimSpace(stderr.String()),
			Ms:       elapsed,
			TimedOut: timedOut,
		}
		if typed {
			res.Expected = tc.ExpectedJSON
			res.Pass = err == nil && !timedOut && jsonEqualStr(tc.ExpectedJSON, res.Got)
		} else {
			res.Expected = normalize(tc.Expected)
			res.Pass = err == nil && !timedOut && res.Got == res.Expected
		}
		if res.Pass {
			rep.Passed++
		}
		rep.TotalMs += elapsed
		rep.Results = append(rep.Results, res)
	}
	return rep
}

// SolutionMeta is what templates may reference.
type SolutionMeta struct {
	FrontendID int64
	Title      string
	Slug       string
	Difficulty string
}

func pyType(t string) string {
	switch t {
	case "integer", "long":
		return "int"
	case "string", "char":
		return "str"
	case "boolean":
		return "bool"
	case "double":
		return "float"
	case "void":
		return "None"
	}
	if inner, ok := strings.CutSuffix(t, "[]"); ok {
		return "List[" + pyType(inner) + "]"
	}
	if inner, ok := strings.CutPrefix(t, "list<"); ok {
		return "List[" + pyType(strings.TrimSuffix(inner, ">")) + "]"
	}
	return "object"
}

// typedPythonStub renders a LeetCode-identical class Solution skeleton.
func typedPythonStub(sm SolutionMeta, m *Meta) string {
	params := []string{"self"}
	for _, p := range m.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, pyType(p.Type)))
	}
	return fmt.Sprintf(`# %d. %s  [%s]
# https://leetcode.com/problems/%s/
#
# dojo typed mode: implement the method below — the judge builds the real
# typed arguments and deep-compares your return value. No I/O code needed.
from typing import List, Optional


class Solution:
    def %s(%s) -> %s:
        ...
`, sm.FrontendID, sm.Title, sm.Difficulty, sm.Slug,
		m.Name, strings.Join(params, ", "), pyType(m.Return.Type))
}

// EnsureSolutionFile creates <root>/<frontendID>-<slug>/solution<ext> if it
// does not exist and returns its path. With typed metadata and a python
// target it generates a LeetCode-identical signature stub; otherwise the
// language's raw-mode template.
func EnsureSolutionFile(root string, meta SolutionMeta, lang Language, typed *Meta) (string, error) {
	dir := filepath.Join(root, fmt.Sprintf("%04d-%s", meta.FrontendID, meta.Slug))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "solution"+lang.Ext)
	if _, err := os.Stat(path); err == nil {
		return path, nil // never clobber existing work
	}
	if typed != nil && lang.Name == "python" {
		return path, os.WriteFile(path, []byte(typedPythonStub(meta, typed)), 0o644)
	}
	tpl, err := template.ParseFS(templates, "templates/"+lang.Template)
	if err != nil {
		return "", err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return path, tpl.Execute(f, meta)
}
