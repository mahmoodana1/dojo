package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mahmood/dojo/internal/app"
	"github.com/mahmood/dojo/internal/store"
)

func main() {
	refresh := flag.Bool("refresh-stubs", false,
		"regenerate untouched solution stubs to match LeetCode's editor, then exit")
	flag.Parse()

	dbPath := os.Getenv("DOJO_DB")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot resolve home dir:", err)
			os.Exit(1)
		}
		dbPath = filepath.Join(home, ".local/share/dojo/dojo.db")
	}

	st, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer st.Close()

	if *refresh {
		if err := refreshStubs(st, app.SolutionsRoot()); err != nil {
			fmt.Fprintln(os.Stderr, "refresh stubs:", err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(app.New(st), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
