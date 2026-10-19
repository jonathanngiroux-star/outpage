package main

// tui_tea.go: the Bubble Tea adapter. All logic lives in Model
// (tui_model.go); this file is terminal plumbing only — key events are
// normalized to the same tokens the tests use.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// teaModel adapts Model to bubbletea.
type teaModel struct {
	Model
}

// runTea starts the program; blocks until quit.
func runTea(st *store.Store) int {
	p := tea.NewProgram(teaModel{Model: NewModel(st)}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return 1
	}
	return 0
}

func (t teaModel) Init() tea.Cmd { return nil }

func (t teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height = m.Width, m.Height
		return t, nil
	case tea.KeyMsg:
		switch m.String() {
		case "ctrl+c":
			return t, tea.Quit
		case "enter":
			return t.updated("enter"), nil
		case "tab":
			return t.updated("tab"), nil
		case "shift+tab":
			return t.updated("shift+tab"), nil
		case "backspace":
			return t.updated("backspace"), nil
		case "?":
			return t.updated("?"), nil
		case "b":
			return t.updated("b"), nil
		case "s":
			return t.updated("s"), nil
		default:
			if len(m.Runes) > 0 {
				return t.updated(string(m.Runes)), nil
			}
		}
	}
	return t, nil
}

func (t teaModel) View() string { return t.view() }

// updated applies one normalized input to a copy of the model.
func (t teaModel) updated(input string) teaModel {
	t.Model = t.Model.update(input)
	return t
}
