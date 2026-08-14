package ui

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines the full keyboard shortcuts supported in the TUI
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Top        key.Binding
	Bottom     key.Binding
	NextPane   key.Binding
	PrevPane   key.Binding
	Start      key.Binding
	Stop       key.Binding
	Restart    key.Binding
	DryRun     key.Binding
	Trigger    key.Binding
	AutoScroll key.Binding
	ClearLogs  key.Binding
	ToggleLogs key.Binding
	Filter     key.Binding
	ViewConfig key.Binding
	History    key.Binding
	Help       key.Binding
	Quit       key.Binding
	Escape     key.Binding
}

// DefaultKeyMap returns the configured default key mappings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "bottom"),
		),
		NextPane: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch pane"),
		),
		PrevPane: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev pane"),
		),
		Start: key.NewBinding(
			key.WithKeys("s", "S"),
			key.WithHelp("s", "start"),
		),
		Stop: key.NewBinding(
			key.WithKeys("x", "X"),
			key.WithHelp("x", "stop"),
		),
		Restart: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("r", "restart"),
		),
		DryRun: key.NewBinding(
			key.WithKeys("d", "D"),
			key.WithHelp("d", "dry run"),
		),
		Trigger: key.NewBinding(
			key.WithKeys("t", "T"),
			key.WithHelp("t", "trigger run"),
		),
		AutoScroll: key.NewBinding(
			key.WithKeys("a", "A"),
			key.WithHelp("a", "auto-scroll"),
		),
		ClearLogs: key.NewBinding(
			key.WithKeys("c", "C"),
			key.WithHelp("c", "clear logs"),
		),
		ToggleLogs: key.NewBinding(
			key.WithKeys("l", "L"),
			key.WithHelp("l", "expand logs"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		ViewConfig: key.NewBinding(
			key.WithKeys("v", "V"),
			key.WithHelp("v", "view JSON"),
		),
		History: key.NewBinding(
			key.WithKeys("h", "H"),
			key.WithHelp("h", "run history"),
		),
		Help: key.NewBinding(
			key.WithKeys("?", "f1"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "Q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close/cancel"),
		),
	}
}
