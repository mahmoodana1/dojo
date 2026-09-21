package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mahmood/dojo/internal/runner"
	"github.com/mahmood/dojo/internal/store"
)

// refreshStubs re-generates the solution files that were written by an older
// dojo and never worked in — the ones still holding a stub that no longer
// matches LeetCode's editor. Files with anything in them are left untouched
// and reported, so this is safe to run at any time.
func refreshStubs(st *store.Store, root string) error {
	problems, err := st.ListProblems(true)
	if err != nil {
		return err
	}
	bySlug := make(map[string]store.Problem, len(problems))
	for _, p := range problems {
		bySlug[p.Slug] = p
	}

	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		fmt.Println("no solutions directory yet:", root)
		return nil
	}
	if err != nil {
		return err
	}

	var changed, kept, unknown int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// directories are <frontendID>-<slug>
		name := e.Name()
		i := strings.Index(name, "-")
		if i < 0 {
			continue
		}
		slug := name[i+1:]
		p, ok := bySlug[slug]
		if !ok {
			unknown++
			fmt.Printf("  ? %-45s not in the database — left alone\n", name)
			continue
		}
		if _, err := strconv.ParseInt(name[:i], 10, 64); err != nil {
			continue
		}

		info, err := st.CodeInfo(p.ID)
		if err != nil {
			return err
		}
		changes, keptHere, err := runner.RefreshSolutionDir(filepath.Join(root, name),
			runner.SolutionMeta{FrontendID: p.FrontendID, Title: p.Title,
				Slug: p.Slug, Difficulty: p.Difficulty}, info)
		if err != nil {
			return err
		}
		kept += keptHere
		sort.Strings(changes)
		for _, c := range changes {
			changed++
			fmt.Printf("  %-45s %s\n", name, c)
		}
	}

	fmt.Printf("\n%d file(s) refreshed, %d with your work in them left untouched",
		changed, kept)
	if unknown > 0 {
		fmt.Printf(", %d unrecognised", unknown)
	}
	fmt.Println()
	return nil
}
