package ui

import (
	"fmt"
	"strings"

	"github.com/hitoshyamamoto/modtop/internal/poller"
)

// frameRows is the height of the frames panel: the lower half of the
// register area (one more line is used by its title bar).
func (m Model) frameRows() int {
	if !m.frames {
		return 0
	}
	return m.visibleRows() / 2
}

// listRows is how many register rows are visible.
func (m Model) listRows() int {
	if !m.frames {
		return m.visibleRows()
	}
	return m.visibleRows() - m.frameRows() - 1
}

// scrollFrames moves the frames panel by delta (negative is older).
// Scrolling freezes the panel; reaching the newest frame resumes it.
func (m *Model) scrollFrames(delta int) {
	if m.log == nil {
		return
	}
	frames, total := m.log.Snapshot()
	if len(frames) == 0 {
		return
	}
	end := m.frameEnd
	if end == 0 {
		end = total
	}
	first := total - uint64(len(frames)) + 1
	lowest := min(total, first+uint64(m.frameRows())-1)
	next := int64(end) + int64(delta)
	switch {
	case next >= int64(total):
		m.frameEnd = 0
	case next < int64(lowest):
		m.frameEnd = lowest
	default:
		m.frameEnd = uint64(next)
	}
}

// frameLines renders the panel title bar and its rows.
func (m Model) frameLines() []string {
	rows := m.frameRows()
	var frames []poller.Frame
	var total uint64
	if m.log != nil {
		frames, total = m.log.Snapshot()
	}
	title := "─ frames · live "
	end := total
	if m.frameEnd != 0 {
		title = "─ frames · frozen (↓ to the newest resumes) "
		end = min(m.frameEnd, total)
	}
	out := []string{"├" + title + strings.Repeat("─", max(0, m.width-2-width(title))) + "┤"}

	// Frames with sequence numbers up to end, newest at the bottom.
	first := total - uint64(len(frames)) + 1
	var shown []poller.Frame
	if end >= first && len(frames) > 0 {
		upto := int(end - first + 1)
		shown = frames[max(0, upto-rows):upto]
	}

	// " 15:04:05.000  TX  " is 19 columns, followed by the hex column and
	// the note. The hex column fits a whole TCP request (12 bytes) and
	// grows with the terminal; a line whose note does not fit gives up
	// part of its hex instead of the note.
	inner := m.width - 2
	hexW := max(35, inner-19-2-30)
	for i := 0; i < rows; i++ {
		if i >= len(shown) {
			out = append(out, m.boxLine("", false, false))
			continue
		}
		f := shown[i]
		lineHexW := max(11, min(hexW, inner-19-2-width(f.Note)))
		hex := pad(truncate(hexBytes(f.Raw), lineHexW), lineHexW)
		line := fmt.Sprintf(" %s  %s  %s  %s", f.At.Format("15:04:05.000"), f.Dir, hex, f.Note)
		out = append(out, m.boxLine(line, false, false))
	}
	return out
}

func hexBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, " ")
}

// helpText is the help screen. Every line fits in 80 columns.
const helpText = `KEYS

  ↑ ↓  k j        move (scroll the frames panel when it is open)
  PgUp PgDn       page
  Home End  g G   first / last address
  t               cycle the type of the row
  o               cycle the byte order of a 32-bit pair
  c               cycle the address convention: Modicon → base 1 → base 0
  f               open/close the frames panel
  p               pause/resume polling
  ?               open/close this help
  Esc             close the panel or the help
  q  Ctrl+C       quit

MODBUS ADDRESSING

The same register can appear in three forms in manuals:

  Modicon   40001   the first digit tells the table (4 = holding register)
  Base 1    1       numbering that starts at 1
  Base 0    0       the address actually sent in the frame

Tables: 0xxxx coils (FC01) · 1xxxx discrete inputs (FC02)
        3xxxx input registers (FC04) · 4xxxx holding registers (FC03)

Common mistakes:
  • Off-by-one: mixing up base 0 and base 1. Look at the neighbors in the list.
  • Wrong table: 3xxxx and 4xxxx are different tables.
  • Word order: an absurd float32 may just be the byte order (key o).
  • Block reads: some devices return 0 for nonexistent registers.
    When in doubt, use --single.`

func helpLines() []string { return strings.Split(helpText, "\n") }

// renderHelp draws the help screen, scrolled to helpTop.
func (m Model) renderHelp() string {
	lines := helpLines()
	body := m.height - 2
	top := min(m.helpTop, max(0, len(lines)-body))
	out := []string{m.paint(styleBold, " modtop · help")}
	for i := top; i < top+body; i++ {
		if i < len(lines) {
			out = append(out, " "+lines[i])
		} else {
			out = append(out, "")
		}
	}
	out = append(out, " ↑↓ scroll  ? or Esc close  q quit")
	for i, l := range out {
		out[i] = truncate(l, m.width)
	}
	return strings.Join(out, "\n")
}
