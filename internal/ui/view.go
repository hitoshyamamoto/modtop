package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/codec"
	"github.com/hitoshyamamoto/modtop/internal/decode"
	"github.com/hitoshyamamoto/modtop/internal/poller"
)

// Fixed screen lines around the register list: header, box top, column
// titles, box bottom, translation, orders, stats, two status lines and
// the shortcuts line.
const chromeLines = 10

// Column widths inside the box (after the selection marker).
const (
	colAddr  = 11
	colRaw   = 9
	colType  = 10
	colValue = 21
)

var (
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleReverse = lipgloss.NewStyle().Reverse(true)
	styleFaint   = lipgloss.NewStyle().Faint(true)
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleBad     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// paint applies a style only when colors are enabled.
func (m Model) paint(s lipgloss.Style, text string) string {
	if !m.opts.Color {
		return text
	}
	return s.Render(text)
}

func (m Model) visibleRows() int {
	return m.height - chromeLines
}

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return "Terminal too small (minimum 80×24)."
	}
	var lines []string
	lines = append(lines, m.header())
	lines = append(lines, "┌"+strings.Repeat("─", m.width-2)+"┐")
	lines = append(lines, m.boxLine(" "+pad("Address", colAddr)+pad("Raw", colRaw)+pad("Type", colType)+"Value", false, false))
	vis := m.visibleRows()
	for i := m.top; i < m.top+vis; i++ {
		if i < len(m.rows) {
			lines = append(lines, m.rowLine(i))
		} else {
			lines = append(lines, m.boxLine("", false, false))
		}
	}
	lines = append(lines, "└"+strings.Repeat("─", m.width-2)+"┘")
	lines = append(lines, " "+address.Translate(m.addr(m.sel), m.conv))
	lines = append(lines, m.ordersLine())
	lines = append(lines, m.statsLine())
	status := m.statusLines()
	lines = append(lines, status[0], status[1])
	lines = append(lines, " ↑↓ move  t type  o order  c convention  f frames  p pause  ? help  q quit")
	for i, l := range lines {
		lines[i] = truncate(l, m.width)
	}
	return strings.Join(lines, "\n")
}

func (m Model) header() string {
	end := m.addr(len(m.rows) - 1)
	var right []string
	if m.opts.ForcedPort {
		right = append(right, m.paint(styleBad, "⚠ FORCED PORT"))
	}
	if m.ctl != nil && m.ctl.Paused() {
		right = append(right, "⏸ paused")
	}
	right = append(right, m.healthText())
	r := strings.Join(right, "  ")

	// From the fullest to the most compact form, so the status on the
	// right always fits.
	rng := address.Format(m.opts.Start, m.conv) + "–" + address.Format(end, m.conv)
	base := fmt.Sprintf("%s · unit %d · %s", m.opts.Target, m.opts.Unit, rng)
	room := m.width - width(r) - 1
	left := ""
	for _, cand := range []string{
		fmt.Sprintf(" modtop · %s · %s · %v", base, m.conv, m.opts.Interval),
		fmt.Sprintf(" %s · %s · %v", base, m.conv, m.opts.Interval),
		fmt.Sprintf(" %s · %s", base, m.conv),
		" " + base,
	} {
		left = cand
		if width(cand) <= room {
			break
		}
	}
	left = truncate(left, room)
	gap := m.width - width(left) - width(r)
	return m.paint(styleBold, left) + strings.Repeat(" ", gap) + r
}

func (m Model) healthText() string {
	if !m.hasResult {
		return "◌ connecting"
	}
	switch m.result.Health {
	case poller.OK:
		return m.paint(styleOK, "● ok")
	case poller.Degraded:
		return m.paint(styleWarn, "◐ degraded")
	case poller.Down:
		return m.paint(styleBad, "○ no response")
	}
	return "◌ connecting"
}

// boxLine wraps plain inner content in the box borders, padded to the
// width. Selected rows are shown in reverse video and stale rows faint.
func (m Model) boxLine(inner string, selected, stale bool) string {
	inner = pad(truncate(inner, m.width-2), m.width-2)
	switch {
	case selected:
		inner = m.paint(styleReverse, inner)
	case stale:
		inner = m.paint(styleFaint, inner)
	}
	return "│" + inner + "│"
}

func (m Model) cell(i int) (poller.Cell, bool) {
	if !m.hasResult || i >= len(m.result.Cells) {
		return poller.Cell{}, false
	}
	return m.result.Cells[i], true
}

func (m Model) rowLine(i int) string {
	r := m.rows[i]
	marker := " "
	if i == m.sel {
		marker = "▶"
	}
	c, have := m.cell(i)

	raw := "—"
	if have && !c.LastOK.IsZero() && !c.Missing {
		if m.opts.Start.Table.IsBit() {
			raw = fmt.Sprintf("%d", c.Raw)
		} else {
			raw = fmt.Sprintf("0x%04X", c.Raw)
		}
	}
	typ := r.typ.String()
	if r.cont {
		typ = ""
	}

	value, stale := m.valueText(i)
	pair := ""
	if r.cont {
		value, stale = "·", false
		pair = "┘"
	} else if r.typ.Is32() {
		pair = "┐ " + r.order.String()
	}
	if pair != "" {
		if width(value) < colValue {
			value = pad(value, colValue)
		} else {
			value += " "
		}
		value += pair
	}
	return m.boxLine(marker+pad(address.Format(m.addr(i), m.conv), colAddr)+pad(raw, colRaw)+pad(typ, colType)+value, i == m.sel, stale)
}

// valueText returns the decoded value of row i (a pair start reads two
// cells) and whether it is stale. Stale values carry their age and the
// reason; values never read show "—" and the reason.
func (m Model) valueText(i int) (string, bool) {
	r := m.rows[i]
	c, have := m.cell(i)
	if !have {
		return "—", false
	}
	cells := []poller.Cell{c}
	if r.typ.Is32() {
		c2, _ := m.cell(i + 1)
		cells = append(cells, c2)
	}
	var (
		failed  *poller.Cell
		oldest  time.Time
		missing bool
		never   bool
	)
	for k := range cells {
		cc := &cells[k]
		if cc.Missing {
			missing = true
		}
		if cc.LastOK.IsZero() {
			never = true
		} else if oldest.IsZero() || cc.LastOK.Before(oldest) {
			oldest = cc.LastOK
		}
		if !cc.OK && failed == nil {
			failed = cc
		}
	}
	switch {
	case missing:
		return "— (exc 02)", false
	case failed == nil:
		return m.decoded(r, cells), false
	case never:
		return "—  " + poller.ShortReason(failed.Err), false
	}
	return m.decoded(r, cells) + "  " + age(m.now().Sub(oldest)) + " · " + poller.ShortReason(failed.Err), true
}

func (m Model) decoded(r row, cells []poller.Cell) string {
	if r.typ == decode.Bool {
		return decode.Format(float64(cells[0].Raw), decode.Bool)
	}
	var (
		v   float64
		err error
	)
	if r.typ.Is32() {
		v, err = decode.Decode32(cells[0].Raw, cells[1].Raw, r.typ, r.order)
	} else {
		v, err = decode.Decode16(cells[0].Raw, r.typ)
	}
	if err != nil {
		return "?"
	}
	return decode.Format(v, r.typ)
}

// age formats how long ago a value was read: "12s ago", "3min ago", "2h ago".
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dmin ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// ordersLine shows the selected pair in the four byte orders, the active
// one highlighted and, without colors, in brackets.
func (m Model) ordersLine() string {
	r := m.rows[m.sel]
	if !r.typ.Is32() || r.cont {
		return ""
	}
	c0, ok0 := m.cell(m.sel)
	c1, ok1 := m.cell(m.sel + 1)
	if !ok0 || !ok1 || c0.LastOK.IsZero() || c1.LastOK.IsZero() {
		return ""
	}
	vals := decode.AllOrders(c0.Raw, c1.Raw, r.typ)
	parts := make([]string, len(vals))
	for k, o := range decode.Orders {
		text := o.String() + " " + decode.Format(vals[k], r.typ)
		if o == r.order {
			if m.opts.Color {
				text = styleReverse.Render(text)
			} else {
				text = "[" + text + "]"
			}
		}
		parts[k] = text
	}
	return " " + strings.Join(parts, " · ")
}

func (m Model) statsLine() string {
	if !m.hasResult {
		return ""
	}
	s := m.result.Stats
	line := fmt.Sprintf(" ok %d · timeouts %d · exceptions %d", s.OK, s.Timeouts, s.ExceptionTotal())
	if s.CRCErrors > 0 {
		line += fmt.Sprintf(" · CRC %d", s.CRCErrors)
	}
	if s.Malformed > 0 {
		line += fmt.Sprintf(" · malformed %d", s.Malformed)
	}
	if s.Other > 0 {
		line += fmt.Sprintf(" · other %d", s.Other)
	}
	return line + fmt.Sprintf(" · cycle %d ms", m.result.Duration.Milliseconds())
}

// statusLines returns two lines: a message from the last key, or the
// serial settings hint when nothing answers, or an explanation of the
// selected address's error.
func (m Model) statusLines() [2]string {
	text := m.status
	if text == "" && m.opts.Serial != nil && m.silent > noResponseCycles {
		s := m.opts.Serial
		text = fmt.Sprintf("No response. Check baud (%d), parity (%c), stop bits (%d) and unit ID (%d).\n"+
			"Many devices use parity N: try --parity N.", s.Baud, s.Parity, s.StopBits, m.opts.Unit)
	}
	if text == "" {
		text = m.explainSelected()
	}
	var out [2]string
	for k, l := range wrap(strings.ReplaceAll(text, "\n", " "), m.width-2, 2) {
		out[k] = " " + l
	}
	return out
}

// wrap splits text into at most maxLines lines of up to n columns,
// breaking at spaces. Text that still does not fit is cut with "...".
func wrap(text string, n, maxLines int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case width(line)+1+width(word) <= n:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) > maxLines {
		last := lines[maxLines-1] + " " + strings.Join(lines[maxLines:], " ")
		lines = append(lines[:maxLines-1], truncate(last, n))
	}
	return lines
}

// explainSelected describes the error of the selected address, if any.
func (m Model) explainSelected() string {
	c, have := m.cell(m.sel)
	if !have || c.Err == nil || c.OK {
		return ""
	}
	var exc *codec.ExceptionError
	if !errors.As(c.Err, &exc) {
		return c.Err.Error()
	}
	hint := exc.Code.Hint()
	if c.Missing {
		hint = "The register does not exist on this device; it will not be read again this session."
	}
	return fmt.Sprintf("%s (exception %02X). %s", exc.Code.Name(), byte(exc.Code), hint)
}

// width is the display width of s (styled or not).
func width(s string) int {
	return lipgloss.Width(s)
}

// pad right-pads plain text with spaces to n columns.
func pad(s string, n int) string {
	if w := width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// truncate cuts plain text to n columns, ending with "...". Every glyph
// the UI uses is one column wide. Styled text is built to fit and is
// returned unchanged.
func truncate(s string, n int) string {
	if width(s) <= n || strings.Contains(s, "\x1b") {
		return s
	}
	r := []rune(s)
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}
