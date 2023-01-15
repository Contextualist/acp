package tui

import tea "github.com/charmbracelet/bubbletea"

type clearQuitModel struct{}

func (clearQuitModel) Init() tea.Cmd                           { return func() tea.Msg { return struct{}{} } }
func (m clearQuitModel) Update(_ tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m clearQuitModel) View() string                          { return "" }
