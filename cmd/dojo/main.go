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

	var st *store.Store
	open := func() *store.Store {
		if st == nil {
			st = openStore()
		}
		return st
	}
	defer func() {
		if st != nil {
			st.Close()
		}
	}()

	if handled, err := subcommand(flag.Args(), open); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "dojo:", err)
			if st != nil {
				st.Close()
			}
			os.Exit(1)
		}
		return
	}
	open()

	if *refresh {
		if err := refreshStubs(st, app.SolutionsRoot()); err != nil {
			fmt.Fprintln(os.Stderr, "refresh stubs:", err)
			os.Exit(1)
		}
		return
	}

	m := app.New(st)
	m.AttachTmux()
	defer m.DetachTmux()
	p := tea.NewProgram(m, tea.WithAltScreen())
	stop := app.Listen(p)
	defer stop()
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func openStore() *store.Store {
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
	return st
}

func runSwitcher(session string, st *store.Store) error { return app.RunSwitcher(session, st) }
