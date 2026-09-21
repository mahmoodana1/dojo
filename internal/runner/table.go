package runner

import (
	"sort"
	"strconv"
	"strings"
)

// Database problems are judged on result sets, not text. LeetCode prints them
// as ASCII tables in the statement; the SQL and pandas drivers print them as
// TSV. Both parse into (columns, rows) here, and comparison follows LeetCode's
// own rules: the same columns and the same rows, in any order, unless the
// problem asked for an ordering.

type table struct {
	cols []string
	rows [][]string
}

// parseTable reads either LeetCode's ASCII table or a driver's TSV dump.
func parseTable(s string) (table, bool) {
	var t table
	var raw [][]string
	ascii := strings.Contains(s, "|")
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "+-") || strings.HasPrefix(line, "+=") {
			continue
		}
		if ascii {
			if !strings.HasPrefix(line, "|") {
				continue // prose around the table ("Output:", explanations)
			}
			line = strings.Trim(line, "|")
			raw = append(raw, strings.Split(line, "|"))
			continue
		}
		raw = append(raw, strings.Split(line, "\t"))
	}
	if len(raw) == 0 {
		return t, false
	}
	for _, c := range raw[0] {
		t.cols = append(t.cols, strings.ToLower(strings.TrimSpace(c)))
	}
	for _, r := range raw[1:] {
		if len(r) != len(t.cols) {
			return t, false
		}
		row := make([]string, len(r))
		for i, c := range r {
			row[i] = cellValue(c)
		}
		t.rows = append(t.rows, row)
	}
	return t, true
}

// cellValue canonicalises one cell so 5000, 5000.00 and " 5000 " compare
// equal, and every spelling of an empty cell becomes "null".
func cellValue(c string) string {
	c = strings.TrimSpace(c)
	switch strings.ToLower(c) {
	case "", "null", "none", "nan", "nat", "<na>":
		return "null"
	}
	if f, err := strconv.ParseFloat(c, 64); err == nil {
		return strconv.FormatFloat(f, 'f', 6, 64)
	}
	return c
}

// tableEqual compares a solution's result set against the expected one.
// Columns must match by name but may be in any order; rows are compared as a
// multiset, since LeetCode returns database results "in any order".
func tableEqual(expected, got string) bool {
	want, ok1 := parseTable(expected)
	have, ok2 := parseTable(got)
	if !ok1 || !ok2 || len(want.cols) != len(have.cols) {
		return false
	}

	// map each expected column to its position in the produced table
	order := make([]int, len(want.cols))
	used := make([]bool, len(have.cols))
	for i, name := range want.cols {
		order[i] = -1
		for j, h := range have.cols {
			if !used[j] && h == name {
				order[i], used[j] = j, true
				break
			}
		}
		if order[i] < 0 {
			return false
		}
	}

	if len(want.rows) != len(have.rows) {
		return false
	}
	wantKeys := make([]string, 0, len(want.rows))
	haveKeys := make([]string, 0, len(have.rows))
	for _, r := range want.rows {
		wantKeys = append(wantKeys, strings.Join(r, "\x00"))
	}
	for _, r := range have.rows {
		reordered := make([]string, len(order))
		for i, j := range order {
			reordered[i] = r[j]
		}
		haveKeys = append(haveKeys, strings.Join(reordered, "\x00"))
	}
	sort.Strings(wantKeys)
	sort.Strings(haveKeys)
	for i := range wantKeys {
		if wantKeys[i] != haveKeys[i] {
			return false
		}
	}
	return true
}
