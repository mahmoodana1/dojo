package app

import "github.com/charmbracelet/bubbles/key"

// Multi-key vim sequences (dd, gg) are handled with a pending-key state in
// Update; everything single-key lives here.
type keyMap struct {
	Up, Down    key.Binding
	Left, Right key.Binding
	Bottom      key.Binding
	HalfDown    key.Binding
	HalfUp      key.Binding
	Filter      key.Binding
	Select      key.Binding
	Edit        key.Binding
	Run         key.Binding
	Hint        key.Binding
	CycleLang   key.Binding
	Dashboard   key.Binding
	Quit        key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("k", "up")),
		Down:      key.NewBinding(key.WithKeys("j", "down")),
		Left:      key.NewBinding(key.WithKeys("h")),
		Right:     key.NewBinding(key.WithKeys("l")),
		Bottom:    key.NewBinding(key.WithKeys("G")),
		HalfDown:  key.NewBinding(key.WithKeys("ctrl+d")),
		HalfUp:    key.NewBinding(key.WithKeys("ctrl+u")),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Select:    key.NewBinding(key.WithKeys("enter")),
		Edit:      key.NewBinding(key.WithKeys("i")),
		Run:       key.NewBinding(key.WithKeys("r")),
		Hint:      key.NewBinding(key.WithKeys("H")),
		CycleLang: key.NewBinding(key.WithKeys("L")),
		Dashboard: key.NewBinding(key.WithKeys("D", "tab")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c")),
	}
}
