package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type (
	logMsg string

	// A LoggerControl is the user handler for a LoggerModel
	LoggerControl struct {
		ch    chan tea.Msg
		debug bool
	}
)

func NewLoggerControl(debug bool) LoggerControl {
	return LoggerControl{make(chan tea.Msg), debug}
}

// Infof appends a log entry
func (c LoggerControl) Infof(format string, a ...any) {
	c.send(format, a...)
}

// Debugf appends a log entry when in debug mode
func (c LoggerControl) Debugf(format string, a ...any) {
	if c.debug {
		c.send(format, a...)
	}
}

func (c LoggerControl) send(format string, a ...any) {
	c.ch <- logMsg(fmt.Sprintf(format, a...))
}

// Next switch the current model to the next one
func (c LoggerControl) Next(m tea.Model) {
	c.ch <- modelSwitchMsg{m}
}

// End terminates the program, preserving the log entries on screen
func (c LoggerControl) End() {
	c.Next(nil)
	close(c.ch)
}

// A LoggerModel displays a stream of logs
type LoggerModel struct {
	logger LoggerControl
	logs   []string
}

func NewLoggerModel(c LoggerControl) tea.Model {
	m := LoggerModel{logger: c}
	return m
}

func (m LoggerModel) waitForLog() tea.Msg {
	return <-m.logger.ch
}

func (m LoggerModel) Init() tea.Cmd {
	return m.waitForLog
}

func (m LoggerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case logMsg:
		m.logs = append(m.logs, string(msg))
		return m, m.waitForLog
	case modelSwitchMsg:
		if msg.model == nil {
			return m, tea.Quit // quit without clearing screen
		}
		return modelSwitchTo(msg.model), nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			userCancel()
			return modelSwitchTo(clearQuitModel{}), nil
		}
	}
	return m, nil
}

func (m LoggerModel) View() string {
	if m.logger.debug {
		return strings.Join(m.logs, "\n") + "\n"
	}
	if len(m.logs) == 0 {
		return ""
	}
	return m.logs[len(m.logs)-1] + "\n"
}
