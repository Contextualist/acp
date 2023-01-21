package tui

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	humanize "github.com/dustin/go-humanize"
)

type (
	// A StatusControl is the user handler for a StausModel
	StatusControl struct {
		*meteredReadWriteCloser
		chNext chan tea.Msg
	}
)

func NewStatusControl() *StatusControl {
	return &StatusControl{
		chNext: make(chan tea.Msg),
	}
}

// Monitor wraps around a read/write stream for obtaining data transfer stats
func (c *StatusControl) Monitor(stream io.ReadWriteCloser) io.ReadWriteCloser {
	c.meteredReadWriteCloser = newMeteredReadWriteCloser(stream, 300*time.Millisecond)
	return c.meteredReadWriteCloser
}

// Next switches to the next BubbleTea Model and shut down current StatusModel
func (c *StatusControl) Next(m tea.Model) {
	c.chNext <- modelSwitchMsg{m}
	close(c.chNext)
}

type meteredReadWriteCloser struct {
	io.ReadWriteCloser
	rate, total atomic.Uint64
	startTime   time.Time
	ticker      *time.Ticker
}

func newMeteredReadWriteCloser(inner io.ReadWriteCloser, interval time.Duration) *meteredReadWriteCloser {
	ticker := time.NewTicker(interval)
	m := &meteredReadWriteCloser{
		ReadWriteCloser: inner,
		startTime:       time.Now(),
		ticker:          ticker,
	}
	go func() {
		for range ticker.C {
			m.rate.Store(uint64(float64(m.total.Load()) / time.Since(m.startTime).Seconds()))
		}
	}()
	return m
}

func (m *meteredReadWriteCloser) Read(p []byte) (n int, err error) {
	n, err = m.ReadWriteCloser.Read(p)
	m.total.Add(uint64(n))
	return
}
func (m *meteredReadWriteCloser) Write(p []byte) (n int, err error) {
	n, err = m.ReadWriteCloser.Write(p)
	m.total.Add(uint64(n))
	return
}
func (m *meteredReadWriteCloser) Close() error {
	m.ticker.Stop()
	return m.ReadWriteCloser.Close()
}

// A StatusModel displays a updating stats of data transfer
type StatusModel struct {
	spinner spinner.Model
	status  *StatusControl
}

func NewStatusModel(c *StatusControl) tea.Model {
	return StatusModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Points)),
		status:  c,
	}
}

func (m StatusModel) waitForNext() tea.Msg {
	return <-m.status.chNext
}

func (m StatusModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForNext)
}

func (m StatusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case modelSwitchMsg:
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

func (m StatusModel) View() string {
	rate, total := m.status.rate.Load(), m.status.total.Load()
	return fmt.Sprintf("%s  %6s/s  %6s", m.spinner.View(), humanize.Bytes(rate), humanize.Bytes(total))
}
