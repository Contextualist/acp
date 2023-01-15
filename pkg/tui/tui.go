package tui

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	programSingleton *tea.Program
	userCancel       context.CancelFunc
)

// RunProgram runs a tea.Program with a tea.Model as the initial model,
// which can switch itself to other model in its Update func.
func RunProgram(model tea.Model, cancel context.CancelFunc) tea.Model {
	programSingleton = tea.NewProgram(model)
	userCancel = cancel
	m, err := programSingleton.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "BubbleTea: %v", err)
		os.Exit(1)
	}
	return m
}

type modelSwitchMsg struct {
	model tea.Model
}

func modelSwitchTo(m tea.Model) tea.Model {
	go func() { programSingleton.Send(m.Init()()) }()
	return m
}
