package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mahmood/dojo/internal/app"
	"github.com/mahmood/dojo/internal/store"
)

func main() {
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

	p := tea.NewProgram(app.New(st), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
