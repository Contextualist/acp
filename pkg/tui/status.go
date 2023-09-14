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
	StatusControl[T io.Closer] struct {
		*meteredReadWriteCloser[T]
		chNext chan tea.Msg
	}
)

func NewStatusControl[T io.Closer]() *StatusControl[T] {
	return &StatusControl[T]{
		chNext: make(chan tea.Msg),
	}
}

// Monitor wraps around a read/write stream for obtaining data transfer stats
func (c *StatusControl[T]) Monitor(stream T) T {
	c.meteredReadWriteCloser = newMeteredReadWriteCloser(stream, 300*time.Millisecond)
	return any(c.meteredReadWriteCloser).(T)
}

// Next switches to the next BubbleTea Model and shut down current StatusModel
func (c *StatusControl[_]) Next(m tea.Model) {
	c.chNext <- modelSwitchMsg{m}
	close(c.chNext)
}

type meteredReadWriteCloser[T io.Closer] struct {
	reader      io.ReadCloser
	writer      io.WriteCloser
	rate, total atomic.Uint64
	startTime   time.Time
	ticker      *time.Ticker
}

// NOTE: inner should be io.ReadCloser | io.WriteCloser | io.ReadWriteCloser
func newMeteredReadWriteCloser[T io.Closer](inner T, interval time.Duration) *meteredReadWriteCloser[T] {
	ticker := time.NewTicker(interval)
	m := &meteredReadWriteCloser[T]{
		startTime: time.Now(),
		ticker:    ticker,
	}
	if r, ok := any(inner).(io.ReadCloser); ok {
		m.reader = r
	}
	if w, ok := any(inner).(io.WriteCloser); ok {
		m.writer = w
	}
	if m.reader == nil && m.writer == nil {
		panic("inner is neither io.ReadCloser nor io.WriteCloser")
	}
	go func() {
		for range ticker.C {
			m.rate.Store(uint64(float64(m.total.Load()) / time.Since(m.startTime).Seconds()))
		}
	}()
	return m
}

func (m *meteredReadWriteCloser[_]) Read(p []byte) (n int, err error) {
	n, err = m.reader.Read(p)
	m.total.Add(uint64(n))
	return
}
func (m *meteredReadWriteCloser[_]) Write(p []byte) (n int, err error) {
	n, err = m.writer.Write(p)
	m.total.Add(uint64(n))
	return
}
func (m *meteredReadWriteCloser[_]) Close() error {
	m.ticker.Stop()
	if m.reader != nil {
		return m.reader.Close()
	}
	return m.writer.Close()
}

// A StatusModel displays a updating stats of data transfer
type StatusModel[T io.Closer] struct {
	spinner spinner.Model
	status  *StatusControl[T]
}

func NewStatusModel[T io.Closer](c *StatusControl[T]) tea.Model {
	return StatusModel[T]{
		spinner: spinner.New(spinner.WithSpinner(spinner.Points)),
		status:  c,
	}
}

func (m StatusModel[_]) waitForNext() tea.Msg {
	return <-m.status.chNext
}

func (m StatusModel[_]) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForNext)
}

func (m StatusModel[_]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m StatusModel[_]) View() string {
	rate, total := m.status.rate.Load(), m.status.total.Load()
	return fmt.Sprintf("%s  %6s/s  %6s", m.spinner.View(), humanize.Bytes(rate), humanize.Bytes(total))
}
