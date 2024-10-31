package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type (
	auxLoggerControl struct {
		ch         chan tea.Msg
		chFinalLog chan string
		maxRows    int
	}
	auxLoggerQuitMsg struct{}
)

func newAuxLoggerControl(maxRows int) auxLoggerControl {
	return auxLoggerControl{make(chan tea.Msg), make(chan string), maxRows}
}

// Logf logs a message
func (c auxLoggerControl) Logf(format string, a ...any) {
	c.ch <- logMsg(fmt.Sprintf(format, a...))
}

// epilogue shuts down the logger and returns the current displayed log
func (c auxLoggerControl) epilogue() string {
	c.ch <- auxLoggerQuitMsg{}
	close(c.ch)
	return <-c.chFinalLog
}

// auxLoggerModel is a model that displays a rolling log alongside other models.
// i.e. it is intended to be used as an embedded model
type auxLoggerModel struct {
	logger  auxLoggerControl
	logs    []string
	logRepr string
}

func newAuxLoggerModel(c auxLoggerControl) tea.Model {
	return auxLoggerModel{logger: c}
}

func (m auxLoggerModel) waitForLog() tea.Msg {
	return <-m.logger.ch
}

func (m auxLoggerModel) Init() tea.Cmd {
	return m.waitForLog
}

func (m auxLoggerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case logMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > m.logger.maxRows {
			m.logs = m.logs[1:]
		}
		m.logRepr = strings.Join(m.logs, "\n")
		return m, m.waitForLog
	case auxLoggerQuitMsg:
		final := m.logRepr
		if final != "" {
			heading := "Skipped errors:"
			if len(m.logs) == m.logger.maxRows {
				heading += fmt.Sprintf(" (showing only last %d entries)", m.logger.maxRows)
			}
			final = heading + "\n" + final
		}
		m.logger.chFinalLog <- final
		close(m.logger.chFinalLog)
		return m, nil
	}
	return m, nil
}

func (m auxLoggerModel) View() string {
	return m.logRepr
}
