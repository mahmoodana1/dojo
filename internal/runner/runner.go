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
	"regexp"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/*.tmpl
var templates embed.FS

//go:embed typed_driver.py
var typedDriver string

//go:embed sql_driver.py
var sqlDriver string

//go:embed pandas_driver.py
var pandasDriver string

// Language describes how to build and run a solution. {{src}} and {{bin}}
// are substituted with the solution path and a temp binary path.
type Language struct {
	Name    string
	Leet    string // LeetCode langSlug: the codeSnippets key, and the tab you paste into
	Ext     string
	Comment string // line-comment marker, for dojo's title/URL header
	// UseSnippet: write LeetCode's editor stub verbatim. False for C/C++,
	// whose only local judge is the I/O contract — LeetCode's `class
	// Solution` has no main(), so a verbatim stub could not even link.
	UseSnippet bool
	Template   string   // file under templates/; empty = snippet-only language
	Compile    []string // empty for interpreted languages
	Run        []string
}

var Registry = map[string]Language{
	"python": {Name: "python", Leet: "python3", Ext: ".py", Comment: "#",
		UseSnippet: true, Template: "python.tmpl",
		Run:        []string{"python3", "{{src}}"}},
	"c": {Name: "c", Leet: "c", Ext: ".c", Comment: "//", Template: "c.tmpl",
		Compile: []string{"gcc", "-O2", "-std=c17", "-o", "{{bin}}", "{{src}}"},
		Run:     []string{"{{bin}}"}},
	"cpp": {Name: "cpp", Leet: "cpp", Ext: ".cpp", Comment: "//", Template: "cpp.tmpl",
		Compile: []string{"g++", "-O2", "-std=c++20", "-o", "{{bin}}", "{{src}}"},
		Run:     []string{"{{bin}}"}},
	// Database and shell problems have no LeetCode C/C++/Python tab at all;
	// their stub comes from codeSnippets and they judge through a driver.
	"mysql":  {Name: "mysql", Leet: "mysql", Ext: ".sql", Comment: "#", UseSnippet: true},
	"pandas": {Name: "pandas", Leet: "pythondata", Ext: ".pandas.py", Comment: "#", UseSnippet: true},
	"bash": {Name: "bash", Leet: "bash", Ext: ".sh", Comment: "#", UseSnippet: true,
		Run: []string{"bash", "{{src}}"}},
	// The JavaScript study plan ("30 Days of JavaScript") has no other tab.
	// dojo writes LeetCode's stub for it but has no JS judge — see Plan.
	"javascript": {Name: "javascript", Leet: "javascript", Ext: ".js", Comment: "//", UseSnippet: true},
}

// Order controls language cycling in the UI. A problem only ever offers the
// languages LeetCode offers for it (see CodeInfo.Languages), so the algo and
// database entries never appear together.
var Order = []string{"python", "cpp", "c", "mysql", "pandas", "bash", "javascript"}

type Case struct {
	Input    string
	Expected string
	Hidden   bool
	// typed judging (v2): when both are set and the language supports it,
	// the case is judged through the typed driver instead of raw stdio.
	ArgsJSON     string
	ExpectedJSON string
}

// Param is one entry of a LeetCode signature.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Method is one member of a design-class problem (LRUCache, MinStack…).
type Method struct {
	Name   string  `json:"name"`
	Params []Param `json:"params"`
	Return struct {
		Type string `json:"type"`
	} `json:"return"`
}

// Meta is the parsed LeetCode metaData. It describes either a single method
// (ClassName empty) or a design class to be instantiated and driven by a
// recorded sequence of calls.
type Meta struct {
	Name        string   `json:"name,omitempty"`
	ClassName   string   `json:"classname,omitempty"`
	Constructor *Method  `json:"constructor,omitempty"`
	Methods     []Method `json:"methods,omitempty"`
	Params      []Param  `json:"params,omitempty"`
	Return      struct {
		Type string `json:"type"`
	} `json:"return"`

	// Non-algorithm problems describe themselves differently: database
	// problems ship table DDL instead of a signature, shell problems ship
	// nothing but a flag.
	MySQL      []string `json:"mysql,omitempty"`
	PythonData []string `json:"pythondata,omitempty"`
	Shell      bool     `json:"shell,omitempty"`
	Manual     bool     `json:"manual,omitempty"`
}

// IsDesign reports whether this is a class-based ("system design") problem.
func (m *Meta) IsDesign() bool { return m.ClassName != "" }

// Kind is the family of problem, which decides both the editor stub and how
// (or whether) dojo can judge it.
type Kind string

const (
	KindFunction Kind = "function" // class Solution { … }
	KindDesign   Kind = "design"   // class LRUCache { … }
	KindSQL      Kind = "sql"      // "Write your MySQL query statement below"
	KindPandas   Kind = "pandas"   // pd.DataFrame in, pd.DataFrame out
	KindShell    Kind = "shell"    // bash one-liners over file.txt
	KindUnknown  Kind = "unknown"
)

func (m *Meta) Kind() Kind {
	switch {
	case m == nil:
		return KindUnknown
	case len(m.MySQL) > 0:
		return KindSQL
	case len(m.PythonData) > 0:
		return KindPandas
	case m.Shell:
		return KindShell
	case m.ClassName != "":
		return KindDesign
	case m.Name != "":
		return KindFunction
	}
	return KindUnknown
}

// ParseMeta decodes LeetCode metaData. Every family is accepted — only a blob
// that names nothing dojo recognises is an error.
func ParseMeta(metaJSON string) (*Meta, error) {
	var m Meta
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		return nil, err
	}
	if m.Kind() == KindUnknown {
		return nil, fmt.Errorf("metaData describes no known problem kind")
	}
	return &m, nil
}

// CodeInfo is everything dojo knows about how a problem is written and
// judged: LeetCode's own editor stubs (codeSnippets, keyed by langSlug), the
// signature metadata, and whether example cases were parsed into typed
// arguments.
type CodeInfo struct {
	JudgeMode string // "typed" | "raw"
	MetaJSON  string
	Snippets  map[string]string
	PaidOnly  bool // premium problem: LeetCode publishes no stub at all
}

// Meta parses MetaJSON, returning nil when absent or unrecognised.
func (c CodeInfo) Meta() *Meta {
	if c.MetaJSON == "" {
		return nil
	}
	m, err := ParseMeta(c.MetaJSON)
	if err != nil {
		return nil
	}
	return m
}

// TypedMeta is the signature to judge against, or nil when this problem does
// not judge typed.
func (c CodeInfo) TypedMeta() *Meta {
	if c.JudgeMode != "typed" {
		return nil
	}
	m := c.Meta()
	if m == nil || (m.Name == "" && !m.IsDesign()) {
		return nil
	}
	return m
}

// Kind reports the problem family, preferring the languages LeetCode actually
// offers (a database problem has no python3 tab) and falling back to metaData
// for problems whose snippets have not been fetched yet.
func (c CodeInfo) Kind() Kind {
	if len(c.Snippets) > 0 {
		switch {
		case c.has("mysql"):
			return KindSQL
		case c.has("pythondata"):
			return KindPandas
		case c.has("bash"):
			return KindShell
		}
	}
	if k := c.Meta().Kind(); k != KindUnknown {
		return k
	}
	return KindFunction
}

func (c CodeInfo) has(langSlug string) bool {
	_, ok := c.Snippets[langSlug]
	return ok
}

// Languages lists the dojo languages this problem can be solved in, in Order.
// It mirrors LeetCode's own language tabs, so a SQL problem never offers C
// and an algorithm problem never offers MySQL.
func (c CodeInfo) Languages() []string {
	var out []string
	kind := c.Kind()
	for _, name := range Order {
		lang := Registry[name]
		if len(c.Snippets) > 0 {
			if c.has(lang.Leet) {
				out = append(out, name)
			}
			continue
		}
		// No snippets fetched: fall back to the family in metaData.
		switch kind {
		case KindSQL:
			if name == "mysql" {
				out = append(out, name)
			}
		case KindPandas:
			if name == "pandas" {
				out = append(out, name)
			}
		case KindShell:
			if name == "bash" {
				out = append(out, name)
			}
		default:
			if lang.Template != "" {
				out = append(out, name)
			}
		}
	}
	if len(out) == 0 {
		out = []string{"python"}
	}
	return out
}

// JudgePlan is how a given (problem, language) pair gets judged.
type JudgePlan int

const (
	JudgeRaw    JudgePlan = iota // stdin -> stdout, string compare
	JudgeTyped                   // real arguments, deep JSON compare
	JudgeSQL                     // query run against a scratch database
	JudgePandas                  // DataFrames in, DataFrame out
	JudgeNone                    // dojo cannot check this one locally
)

// Plan decides how src should be judged, and returns the reason when it
// cannot be. Judging follows what was actually written into the file: a stub
// generated from LeetCode's snippet is never I/O-shaped, so it must not be
// fed to the raw stdin/stdout judge.
func (c CodeInfo) Plan(lang Language) (JudgePlan, string) {
	switch c.Kind() {
	case KindSQL:
		if lang.Name == "mysql" {
			return JudgeSQL, ""
		}
		if lang.Name == "pandas" {
			return JudgePandas, ""
		}
	case KindPandas:
		if lang.Name == "pandas" {
			return JudgePandas, ""
		}
	case KindShell:
		return JudgeNone, "shell problems are judged against LeetCode's own input files — " +
			"solve it here, verify on the site"
	}
	if len(lang.Run) == 0 && lang.Template == "" {
		return JudgeNone, "dojo has no local " + lang.Name +
			" judge — write it here, submit it on LeetCode"
	}
	if lang.Name == "python" {
		if c.TypedMeta() != nil {
			return JudgeTyped, ""
		}
		if c.has("python3") {
			// The file holds LeetCode's `class Solution` stub, but dojo could
			// not turn the examples into typed arguments (in-place mutation,
			// custom output format, interactive judge…).
			return JudgeNone, "LeetCode uses a custom judge for this problem — " +
				"dojo can't check it locally; the examples are in the statement"
		}
	}
	return JudgeRaw, ""
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
	// Note explains a report with no results: the problem exists on LeetCode
	// in a form dojo cannot judge offline.
	Note string
}

const perCaseTimeout = 5 * time.Second

// writeDriver stages an embedded python harness in tmp for this run,
// returning "" if it cannot be written (the caller then judges raw).
func writeDriver(body string) string {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("dojo-driver-%d.py", time.Now().UnixNano()))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return ""
	}
	return path
}

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

// RunAll compiles (if needed) and judges src against every case. How a case
// is judged comes from info: typed cases run through the typed driver against
// the real signature, database problems through the SQL/pandas drivers, and
// everything else through raw stdio.
func RunAll(ctx context.Context, lang Language, src string, cases []Case, info CodeInfo) Report {
	var rep Report
	plan, why := info.Plan(lang)
	if plan == JudgeNone {
		return Report{Note: why}
	}
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

	// Driver-judged plans hand the solution to an embedded python harness
	// instead of running it directly, so the file can hold LeetCode's own
	// signature (or query) with no I/O scaffolding around it.
	var driverPath, metaJSON string
	switch plan {
	case JudgeTyped:
		if b, err := json.Marshal(info.TypedMeta()); err == nil {
			metaJSON = string(b)
			driverPath = writeDriver(typedDriver)
		}
	case JudgeSQL:
		metaJSON = info.MetaJSON
		driverPath = writeDriver(sqlDriver)
	case JudgePandas:
		metaJSON = info.MetaJSON
		driverPath = writeDriver(pandasDriver)
	}
	switch {
	case driverPath != "":
		defer os.Remove(driverPath)
	case len(lang.Run) == 0:
		// A driver-only language whose harness could not be staged has no
		// other way to run: say so rather than pretend.
		return Report{Note: "could not stage the " + lang.Name + " judge in " + os.TempDir()}
	default:
		plan = JudgeRaw // fall back to the language's own I/O contract
	}

	runArgs := expand(lang.Run, src, bin)
	for i, tc := range cases {
		typed := plan == JudgeTyped && tc.ArgsJSON != "" && tc.ExpectedJSON != ""
		table := plan == JudgeSQL || plan == JudgePandas

		cctx, cancel := context.WithTimeout(ctx, perCaseTimeout)
		var cmd *exec.Cmd
		switch {
		case typed:
			cmd = exec.CommandContext(cctx, "python3", driverPath, src, metaJSON)
			cmd.Stdin = strings.NewReader(tc.ArgsJSON)
		case table:
			cmd = exec.CommandContext(cctx, "python3", driverPath, src, metaJSON)
			cmd.Stdin = strings.NewReader(tc.Input)
		default:
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
		} else if table {
			// Result sets are compared as tables: same columns, same rows,
			// order-insensitive — the same latitude LeetCode allows.
			res.Expected = normalize(tc.Expected)
			res.Pass = err == nil && !timedOut && tableEqual(tc.Expected, res.Got)
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
	switch strings.TrimSpace(t) {
	case "integer", "long":
		return "int"
	case "string", "char", "character":
		return "str"
	case "boolean":
		return "bool"
	case "double":
		return "float"
	case "void":
		return "None"
	case "TreeNode":
		return "Optional[TreeNode]"
	case "ListNode":
		return "Optional[ListNode]"
	}
	if inner, ok := strings.CutSuffix(t, "[]"); ok {
		return "List[" + pyType(inner) + "]"
	}
	if inner, ok := strings.CutPrefix(t, "list<"); ok {
		return "List[" + pyType(strings.TrimSuffix(inner, ">")) + "]"
	}
	return "object"
}

// usesType reports whether the signature mentions a node type anywhere,
// including inside list<…> and […] wrappers.
func usesType(m *Meta, name string) bool {
	if strings.Contains(m.Return.Type, name) {
		return true
	}
	for _, p := range m.Params {
		if strings.Contains(p.Type, name) {
			return true
		}
	}
	return false
}

// nodeClassComments mirrors the commented-out definitions LeetCode puts at
// the top of its Python stubs. The judge injects the real classes at run
// time, so these stay comments — exactly as on the site.
func nodeClassComments(m *Meta) string {
	var b strings.Builder
	if usesType(m, "ListNode") {
		b.WriteString(`# Definition for singly-linked list.
# class ListNode:
#     def __init__(self, val=0, next=None):
#         self.val = val
#         self.next = next

`)
	}
	if usesType(m, "TreeNode") {
		b.WriteString(`# Definition for a binary tree node.
# class TreeNode:
#     def __init__(self, val=0, left=None, right=None):
#         self.val = val
#         self.left = left
#         self.right = right

`)
	}
	return b.String()
}

// designPythonStub renders the class skeleton LeetCode shows for
// "system design" problems, including the trailing usage comment.
func designPythonStub(sm SolutionMeta, m *Meta) string {
	sig := func(name string, ps []Param, ret string) string {
		args := []string{"self"}
		for _, p := range ps {
			args = append(args, fmt.Sprintf("%s: %s", p.Name, pyType(p.Type)))
		}
		return fmt.Sprintf("    def %s(%s) -> %s:\n        ...\n",
			name, strings.Join(args, ", "), ret)
	}

	var body strings.Builder
	var ctorParams []Param
	if m.Constructor != nil {
		ctorParams = m.Constructor.Params
	}
	body.WriteString(sig("__init__", ctorParams, "None"))
	for _, meth := range m.Methods {
		body.WriteString("\n" + sig(meth.Name, meth.Params, pyType(meth.Return.Type)))
	}

	// usage comment, exactly as LeetCode renders it
	var usage strings.Builder
	names := make([]string, 0, len(ctorParams))
	for _, p := range ctorParams {
		names = append(names, p.Name)
	}
	fmt.Fprintf(&usage, "\n# Your %s object will be instantiated and called as such:\n", m.ClassName)
	fmt.Fprintf(&usage, "# obj = %s(%s)\n", m.ClassName, strings.Join(names, ", "))
	for i, meth := range m.Methods {
		ns := make([]string, 0, len(meth.Params))
		for _, p := range meth.Params {
			ns = append(ns, p.Name)
		}
		call := fmt.Sprintf("obj.%s(%s)", meth.Name, strings.Join(ns, ", "))
		if meth.Return.Type == "void" || meth.Return.Type == "" {
			fmt.Fprintf(&usage, "# %s\n", call)
		} else {
			fmt.Fprintf(&usage, "# param_%d = %s\n", i+1, call)
		}
	}

	return fmt.Sprintf(`# %d. %s  [%s]
# https://leetcode.com/problems/%s/
#
# dojo typed mode: implement the class below — the judge replays LeetCode's
# recorded call sequence and compares the returned values. No I/O code needed.
from typing import List, Optional


class %s:

%s%s`, sm.FrontendID, sm.Title, sm.Difficulty, sm.Slug,
		m.ClassName, body.String(), usage.String())
}

// typedPythonStub renders a LeetCode-identical class Solution skeleton.
func typedPythonStub(sm SolutionMeta, m *Meta) string {
	if m.IsDesign() {
		return designPythonStub(sm, m)
	}
	params := []string{"self"}
	for _, p := range m.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, pyType(p.Type)))
	}
	body := "        ..."
	if m.Return.Type == "void" {
		body = "        \"\"\"Do not return anything, modify " +
			m.Params[0].Name + " in-place instead.\"\"\""
	}
	return fmt.Sprintf(`# %d. %s  [%s]
# https://leetcode.com/problems/%s/
#
# dojo typed mode: implement the method below — the judge builds the real
# typed arguments and deep-compares your return value. No I/O code needed.
from typing import List, Optional

%s
class Solution:
    def %s(%s) -> %s:
%s
`, sm.FrontendID, sm.Title, sm.Difficulty, sm.Slug,
		nodeClassComments(m),
		m.Name, strings.Join(params, ", "), pyType(m.Return.Type), body)
}

// typingNames are the typing exports LeetCode's judge already has in scope,
// so its python3 snippets annotate `nums: List[int]` without ever importing
// them. typed_driver.py injects the same names for a judged run, but the file
// on disk is also read by editors, linters and `python3 solution.py` — all of
// which rightly call List undefined. dojo writes the import LeetCode omits.
var typingNames = []string{"List", "Optional", "Dict", "Set", "Tuple", "Any", "Union", "Deque"}

// typingUse matches a subscripted typing name — `List[`, never `ListNode`.
var typingUse = regexp.MustCompile(`\b(` + strings.Join(typingNames, "|") + `)\[`)

// withTypingImport prepends the import a LeetCode python3 snippet leaves out.
// A snippet that mentions typing at all is already self-sufficient and is
// returned untouched. The extra line still pastes back into LeetCode fine.
func withTypingImport(snip string) string {
	if strings.Contains(snip, "typing") {
		return snip
	}
	used := make(map[string]bool)
	for _, m := range typingUse.FindAllStringSubmatch(snip, -1) {
		used[m[1]] = true
	}
	var want []string
	for _, name := range typingNames {
		if used[name] {
			want = append(want, name)
		}
	}
	if len(want) == 0 {
		return snip
	}
	return "from typing import " + strings.Join(want, ", ") + "\n\n\n" + snip
}

// header is the two comment lines dojo puts above every stub: which problem
// this is and where it lives. Comments in every language it writes, so the
// whole file still pastes into LeetCode unchanged.
func header(sm SolutionMeta, lang Language) string {
	c := lang.Comment
	if c == "" {
		c = "#"
	}
	return fmt.Sprintf("%s %d. %s  [%s]\n%s https://leetcode.com/problems/%s/\n\n",
		c, sm.FrontendID, sm.Title, sm.Difficulty, c, sm.Slug)
}

// Stub renders the starting file for a problem in one language. LeetCode's
// own editor snippet wins whenever it has been fetched — byte for byte, so
// the solution pastes back into the site with nothing to fix up. dojo only
// synthesises a stub (from the signature metadata, or the language's I/O
// template) when no snippet is on hand.
func Stub(sm SolutionMeta, lang Language, info CodeInfo) (string, error) {
	if snip, ok := info.Snippets[lang.Leet]; ok && lang.UseSnippet && strings.TrimSpace(snip) != "" {
		body := strings.TrimRight(snip, "\n")
		if lang.Name == "python" {
			body = withTypingImport(body)
		}
		return header(sm, lang) + body + "\n", nil
	}
	if lang.Name == "python" {
		if typed := info.TypedMeta(); typed != nil {
			return typedPythonStub(sm, typed), nil
		}
	}
	if info.PaidOnly {
		// Premium problem: there is no signature, description or stub to
		// mirror — an I/O scaffold would only imply a judging contract that
		// does not exist here.
		return header(sm, lang) + fmt.Sprintf(
			"%s premium-only on LeetCode: no stub, statement or tests are published.\n"+
				"%s Write your solution here, run it on the site.\n",
			lang.Comment, lang.Comment), nil
	}
	if lang.Template == "" {
		// A snippet-only language (mysql/pandas/bash) with no snippet yet:
		// an empty, correctly-commented file beats a Python I/O scaffold.
		return header(sm, lang) + fmt.Sprintf(
			"%s run `python3 scripts/ingest.py --snippets` to pull LeetCode's stub for this one\n",
			lang.Comment), nil
	}
	tpl, err := template.ParseFS(templates, "templates/"+lang.Template)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tpl.Execute(&b, sm); err != nil {
		return "", err
	}
	return b.String(), nil
}

// EnsureSolutionFile creates <root>/<frontendID>-<slug>/solution<ext> if it
// does not exist and returns its path, filled with the stub for lang.
// It also returns a note for the UI when an existing file had to be migrated.
func EnsureSolutionFile(root string, meta SolutionMeta, lang Language, info CodeInfo) (string, string, error) {
	dir := filepath.Join(root, fmt.Sprintf("%04d-%s", meta.FrontendID, meta.Slug))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, "solution"+lang.Ext)
	body, err := Stub(meta, lang, info)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(path); err == nil {
		note, err := migrateStaleStub(path, body, lang)
		return path, note, err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", "", err
	}
	return path, dropStaleScaffolds(dir, path, info), nil
}

// LanguageForFile identifies the language of a solution file by extension.
// ".pandas.py" must win over ".py", so the longest extension matches first.
func LanguageForFile(name string) (Language, bool) {
	best, found := Language{}, false
	for _, lang := range Registry {
		if strings.HasSuffix(name, lang.Ext) && len(lang.Ext) > len(best.Ext) {
			best, found = lang, true
		}
	}
	return best, found
}

// PristineStub reports whether src is a stub dojo generated and nobody has
// worked in. It compares against every stub dojo could have written for this
// problem — the current one, the synthesised signature, the language's I/O
// template — so the test is exact rather than a guess about someone's code.
// The one heuristic is for the old raw scaffold, whose header users often
// edited before it was clear the file was a dead end.
func PristineStub(src string, sm SolutionMeta, lang Language, info CodeInfo) bool {
	if isRawModeStub(src) && rawStubIsPristine(src) {
		return true
	}
	for _, candidate := range knownStubs(sm, lang, info) {
		if src == candidate {
			return true
		}
	}
	return false
}

// knownStubs is every file body dojo has ever generated for this problem and
// language, newest first.
func knownStubs(sm SolutionMeta, lang Language, info CodeInfo) []string {
	var out []string
	if body, err := Stub(sm, lang, info); err == nil {
		out = append(out, body)
	}
	if lang.Name == "python" {
		if m := info.Meta(); m != nil && (m.Name != "" || m.IsDesign()) {
			out = append(out, typedPythonStub(sm, m))
		}
		// Before dojo added the typing import, the snippet went out verbatim.
		// Those files are still pristine stubs, and knowing that is what lets
		// RefreshSolutionDir upgrade them instead of treating them as work.
		if snip, ok := info.Snippets[lang.Leet]; ok && lang.UseSnippet && strings.TrimSpace(snip) != "" {
			out = append(out, header(sm, lang)+strings.TrimRight(snip, "\n")+"\n")
		}
	}
	if lang.Template != "" {
		if tpl, err := template.ParseFS(templates, "templates/"+lang.Template); err == nil {
			var b strings.Builder
			if tpl.Execute(&b, sm) == nil {
				out = append(out, b.String())
			}
		}
	}
	return out
}

// RefreshSolutionDir brings one problem's directory in line with what dojo
// would write today: untouched stubs are regenerated, untouched stubs for a
// language LeetCode does not offer are removed, and the problem's primary
// language gets a file if the refresh left none. Anything worked in is
// reported as kept and never modified. Returns one line per change.
func RefreshSolutionDir(dir string, sm SolutionMeta, info CodeInfo) (changes []string, kept int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	langs := info.Languages()
	offered := make(map[string]bool, len(langs))
	for _, name := range langs {
		offered[name] = true
	}

	remaining := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "solution.") ||
			strings.Contains(name, ".rawmode-backup.") {
			continue
		}
		lang, ok := LanguageForFile(name)
		if !ok {
			continue
		}
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !PristineStub(string(b), sm, lang, info) {
			kept++
			remaining++
			continue
		}
		if !offered[lang.Name] {
			if err := os.Remove(path); err == nil {
				changes = append(changes, "removed "+name+" (LeetCode has no "+lang.Name+" tab here)")
			}
			continue
		}
		want, err := Stub(sm, lang, info)
		if err != nil {
			continue
		}
		remaining++
		if string(b) == want {
			continue
		}
		if err := os.WriteFile(path, []byte(want), 0o644); err == nil {
			changes = append(changes, "refreshed "+name)
		}
	}

	// the directory exists because the problem was opened: leave it with a
	// stub in the language LeetCode leads with
	if remaining == 0 && len(langs) > 0 && len(changes) > 0 {
		lang := Registry[langs[0]]
		path := filepath.Join(dir, "solution"+lang.Ext)
		if body, err := Stub(sm, lang, info); err == nil {
			if err := os.WriteFile(path, []byte(body), 0o644); err == nil {
				changes = append(changes, "created solution"+lang.Ext)
			}
		}
	}
	return changes, kept, nil
}

// dropStaleScaffolds removes untouched I/O scaffolds left in the problem's
// directory by an older dojo, once the real stub has been written under a
// different name (a database problem's solution.py next to its solution.sql).
// Only pristine scaffolds in a language LeetCode does not offer here go —
// anything typed into, or any language you might still switch to, stays.
func dropStaleScaffolds(dir, keep string, info CodeInfo) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	offered := make(map[string]bool)
	for _, name := range info.Languages() {
		offered[name] = true
	}
	var dropped []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() || path == keep || !strings.HasPrefix(e.Name(), "solution.") {
			continue
		}
		if lang, ok := LanguageForFile(e.Name()); !ok || offered[lang.Name] {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil || !isRawModeStub(string(b)) || !rawStubIsPristine(string(b)) {
			continue
		}
		if os.Remove(path) == nil {
			dropped = append(dropped, e.Name())
		}
	}
	if len(dropped) == 0 {
		return ""
	}
	return "removed empty " + strings.Join(dropped, ", ") + " — LeetCode has no such tab for this problem"
}

// isRawModeStub reports whether src is dojo's raw-stdin scaffold rather than
// a typed solution. Such a file cannot pass once the problem judges typed —
// the driver calls Solution().<method>(), which the raw stub never defines.
func isRawModeStub(src string) bool {
	return strings.Contains(src, "def solve(data: str) -> str:") &&
		!strings.Contains(src, "class Solution")
}

// rawStubIsPristine reports whether the file still holds the generated body,
// i.e. nothing has been written into it yet.
func rawStubIsPristine(src string) bool {
	return strings.Contains(src, "# your solution here") &&
		strings.Contains(src, "return data")
}

// migrateStaleStub upgrades a solution file that predates a better stub —
// written before the problem gained typed judging, or before LeetCode's own
// snippet was fetched. Pristine scaffolds are replaced outright; anything with
// work in it is copied to solution.rawmode-backup<ext> first, so nothing is
// lost. A file holding real work is never touched.
// Returns a message for the UI when something changed.
func migrateStaleStub(path, want string, lang Language) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	src := string(b)
	if src == want || !isRawModeStub(src) || isRawModeStub(want) {
		return "", nil // already current, real work, or nothing better to offer
	}

	note := ""
	if !rawStubIsPristine(src) {
		backup := strings.TrimSuffix(path, lang.Ext) + ".rawmode-backup" + lang.Ext
		if err := os.WriteFile(backup, b, 0o644); err != nil {
			return "", err
		}
		note = "old raw-mode file saved as " + filepath.Base(backup)
	}
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		return "", err
	}
	if note == "" {
		note = "regenerated stub — now matches LeetCode's editor"
	}
	return note, nil
}
