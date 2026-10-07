// Package ui is the terminal interface. It never calls the transport: it
// only receives cycle results from the poller and reads the frame log.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/decode"
	"github.com/hitoshyamamoto/modtop/internal/poller"
)

// Minimum terminal size.
const (
	minWidth  = 80
	minHeight = 24
)

// noResponseCycles is how many cycles without any answer, on RTU, before
// the serial settings hint is shown.
const noResponseCycles = 5

// Serial describes the serial line, for the header and the no-response hint.
type Serial struct {
	Baud     int
	Parity   byte
	StopBits int
}

// Options is the session configuration shown by the UI.
type Options struct {
	Target     string // "10.1.8.99:502" or "/dev/ttyUSB0"
	Serial     *Serial
	Unit       byte
	Start      address.Addr
	Count      int
	Convention address.Convention
	Interval   time.Duration
	Order      decode.WordOrder // initial order of new pairs
	ForcedPort bool
	Color      bool
}

// Controller pauses and resumes polling.
type Controller interface {
	SetPaused(bool)
	Paused() bool
}

// row is the per-address interpretation chosen during the session.
type row struct {
	typ   decode.DataType
	order decode.WordOrder
	cont  bool // second register of a 32-bit pair
}

// Model is the UI state.
type Model struct {
	opts  Options
	ctl   Controller
	log   *poller.FrameLog
	now   func() time.Time
	width int

	height int
	rows   []row
	sel    int
	top    int
	conv   address.Convention
	status string

	result    poller.CycleResult
	hasResult bool
	silent    int // consecutive cycles in which nothing answered

	quitCode int
}

// NewModel returns the initial state.
func NewModel(opts Options, ctl Controller, log *poller.FrameLog) Model {
	m := Model{
		opts: opts,
		ctl:  ctl,
		log:  log,
		now:  time.Now,
		rows: make([]row, opts.Count),
		conv: opts.Convention,
	}
	def := decode.Uint16
	if opts.Start.Table.IsBit() {
		def = decode.Bool
	}
	for i := range m.rows {
		m.rows[i] = row{typ: def, order: opts.Order}
	}
	return m
}

// resultMsg carries a cycle result from the poller.
type resultMsg poller.CycleResult

// quitMsg asks the UI to exit with the given process exit code.
type quitMsg struct{ code int }

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()
	case resultMsg:
		m.applyResult(poller.CycleResult(msg))
	case quitMsg:
		m.quitCode = msg.code
		return m, tea.Quit
	case tea.KeyPressMsg:
		return m.key(msg.String())
	}
	return m, nil
}

func (m *Model) applyResult(r poller.CycleResult) {
	m.result = r
	m.hasResult = true
	answered := false
	for _, c := range r.Cells {
		if c.OK || c.Missing {
			answered = true
			break
		}
	}
	if answered {
		m.silent = 0
	} else {
		m.silent++
	}
}

func (m Model) key(k string) (tea.Model, tea.Cmd) {
	m.status = ""
	switch k {
	case "q", "ctrl+c":
		if k == "ctrl+c" {
			m.quitCode = 130
		}
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.visibleRows())
	case "pgdown":
		m.move(m.visibleRows())
	case "home", "g":
		m.move(-len(m.rows))
	case "end", "G":
		m.move(len(m.rows))
	case "t":
		m.cycleType()
	case "o":
		m.cycleOrder()
	case "c":
		m.conv = m.conv.Next()
	case "p":
		if m.ctl != nil {
			m.ctl.SetPaused(!m.ctl.Paused())
		}
	}
	return m, nil
}

func (m *Model) move(delta int) {
	m.sel = max(0, min(len(m.rows)-1, m.sel+delta))
	m.scroll()
}

// scroll keeps the selected row visible.
func (m *Model) scroll() {
	vis := m.visibleRows()
	if vis < 1 {
		return
	}
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+vis {
		m.top = m.sel - vis + 1
	}
	m.top = max(0, min(m.top, len(m.rows)-vis))
}

const msgBits = "This table holds bits; the type is always bool."

// cycleType applies the "t" key: uint16 → int16 → uint32 → int32 →
// float32 → uint16. A 32-bit type turns the next row into the second
// half of a pair.
func (m *Model) cycleType() {
	if m.opts.Start.Table.IsBit() {
		m.status = msgBits
		return
	}
	i := m.sel
	if m.rows[i].cont {
		// Second half of a pair: undo the pair.
		m.rows[i-1] = row{typ: decode.Uint16, order: m.rows[i-1].order}
		m.rows[i] = row{typ: decode.Uint16, order: m.opts.Order}
		return
	}
	cur := m.rows[i]
	next := cur.typ.Next()
	if next.Is32() && i == len(m.rows)-1 {
		m.status = "A 32-bit type needs two registers; this is the last one in the range."
		next = decode.Uint16
	}
	if next.Is32() {
		if !cur.typ.Is32() {
			// Row i+1 becomes the second half; if it started another
			// pair, that pair is undone.
			if nxt := m.rows[i+1]; nxt.typ.Is32() && !nxt.cont {
				m.rows[i+2] = row{typ: decode.Uint16, order: m.opts.Order}
			}
			m.rows[i+1] = row{typ: decode.Uint16, order: m.opts.Order, cont: true}
			cur.order = m.opts.Order
		}
		cur.typ = next
		m.rows[i] = cur
		return
	}
	if cur.typ.Is32() {
		m.rows[i+1] = row{typ: decode.Uint16, order: m.opts.Order}
	}
	cur.typ = next
	m.rows[i] = cur
}

func (m *Model) cycleOrder() {
	if m.opts.Start.Table.IsBit() {
		m.status = msgBits
		return
	}
	r := &m.rows[m.sel]
	if !r.typ.Is32() || r.cont {
		m.status = "Byte order only applies to 32-bit types."
		return
	}
	r.order = r.order.Next()
}

// addr returns the address of row i.
func (m Model) addr(i int) address.Addr {
	return address.Addr{Table: m.opts.Start.Table, PDU: m.opts.Start.PDU + uint16(i)}
}

// QuitCode returns the exit code chosen when the UI quit.
func (m Model) QuitCode() int { return m.quitCode }
