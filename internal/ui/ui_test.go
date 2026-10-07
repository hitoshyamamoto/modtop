package ui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/codec"
	"github.com/hitoshyamamoto/modtop/internal/decode"
	"github.com/hitoshyamamoto/modtop/internal/poller"
	"github.com/hitoshyamamoto/modtop/internal/transport"
)

var update = flag.Bool("update", false, "rewrite golden files")

var t0 = time.Date(2026, 10, 7, 14, 3, 22, 0, time.UTC)

type fakeCtl struct{ paused bool }

func (f *fakeCtl) SetPaused(p bool) { f.paused = p }
func (f *fakeCtl) Paused() bool     { return f.paused }

func newTestModel(opts Options) Model {
	if opts.Count == 0 {
		opts.Count = 20
		opts.Start = address.Addr{Table: address.HoldingRegister}
	}
	if opts.Target == "" {
		opts.Target = "10.1.8.99:502"
	}
	if opts.Unit == 0 {
		opts.Unit = 1
	}
	if opts.Interval == 0 {
		opts.Interval = time.Second
	}
	m := NewModel(opts, &fakeCtl{}, nil)
	m.now = func() time.Time { return t0 }
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return mm.(Model)
}

func send(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		mm, _ := m.Update(msg)
		m = mm.(Model)
	}
	return m
}

func key(k string) tea.Msg {
	switch k {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func keys(m Model, ks ...string) Model {
	for _, k := range ks {
		m = send(m, key(k))
	}
	return m
}

// sampleResult reproduces the screen of the specification.
func sampleResult() poller.CycleResult {
	ok := func(v uint16) poller.Cell { return poller.Cell{Raw: v, OK: true, LastOK: t0} }
	cells := make([]poller.Cell, 20)
	for i := range cells {
		cells[i] = ok(0)
	}
	cells[0], cells[1], cells[2], cells[3] = ok(0x4248), ok(0), ok(0x8000), ok(0x44A2)
	cells[4], cells[5] = ok(0x01F4), ok(0xFFF6)
	cells[6] = poller.Cell{Missing: true, Err: &codec.ExceptionError{Function: 3, Code: 2}}
	cells[7] = poller.Cell{Raw: 0x0064, LastOK: t0.Add(-12 * time.Second), Err: &transport.TimeoutError{After: time.Second}}
	return poller.CycleResult{
		Cells:    cells,
		Started:  t0,
		Duration: 38 * time.Millisecond,
		Health:   poller.OK,
		Stats:    poller.Stats{OK: 1243, Timeouts: 2, Exceptions: map[byte]uint64{2: 1}},
	}
}

// sampleModel types rows 1 and 3 as float32 (ABCD and CDAB) and row 6 as
// int16, then selects row 3.
func sampleModel(opts Options) Model {
	m := newTestModel(opts)
	m = send(m, resultMsg(sampleResult()))
	m = keys(m, "t", "t", "t", "t") // row 0: float32
	m = keys(m, "down", "down", "t", "t", "t", "t", "o")
	m = keys(m, "down", "down", "down", "t")
	m = keys(m, "up", "up", "up")
	return m
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/ui -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got\n%s\n--- want\n%s", name, got, want)
	}
	for i, line := range strings.Split(got, "\n") {
		if w := width(line); w > 80 {
			t.Errorf("%s line %d is %d columns wide", name, i+1, w)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("%s contains ANSI escapes without colors", name)
	}
}

func TestGoldenMain(t *testing.T) {
	m := sampleModel(Options{})
	checkGolden(t, "main_80x24", m.render())
	if n := len(strings.Split(m.render(), "\n")); n != 24 {
		t.Errorf("screen has %d lines, want 24", n)
	}
}

func TestGoldenConnecting(t *testing.T) {
	checkGolden(t, "connecting_80x24", newTestModel(Options{}).render())
}

func TestGoldenRTUBits(t *testing.T) {
	m := newTestModel(Options{
		Target: "/dev/ttyUSB0", Serial: &Serial{Baud: 9600, Parity: 'N', StopBits: 1},
		Start: address.Addr{Table: address.Coil}, Count: 10, ForcedPort: true,
	})
	cells := make([]poller.Cell, 10)
	for i := range cells {
		cells[i] = poller.Cell{Raw: uint16(i % 2), OK: true, LastOK: t0}
	}
	m = send(m, resultMsg(poller.CycleResult{Cells: cells, Health: poller.Degraded, Stats: poller.Stats{OK: 3, CRCErrors: 1}}))
	m.ctl.SetPaused(true)
	checkGolden(t, "rtu_bits_80x24", m.render())
}

func TestTooSmall(t *testing.T) {
	m := send(newTestModel(Options{}), tea.WindowSizeMsg{Width: 79, Height: 24})
	if got := m.render(); got != "Terminal too small (minimum 80×24)." {
		t.Errorf("got %q", got)
	}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 23})
	if !strings.HasPrefix(m.render(), "Terminal too small") {
		t.Error("80x23 accepted")
	}
}

func TestPairs(t *testing.T) {
	m := newTestModel(Options{})
	types := func() string {
		var b strings.Builder
		for _, r := range m.rows[:5] {
			if r.cont {
				b.WriteString("+ ")
			} else {
				b.WriteString(r.typ.String() + " ")
			}
		}
		return strings.TrimSpace(b.String())
	}

	m = keys(m, "t")
	if got := types(); got != "int16 uint16 uint16 uint16 uint16" {
		t.Errorf("after t: %s", got)
	}
	m = keys(m, "t")
	if got := types(); got != "uint32 + uint16 uint16 uint16" {
		t.Errorf("uint32: %s", got)
	}
	m = keys(m, "t", "t", "t")
	if got := types(); got != "uint16 uint16 uint16 uint16 uint16" {
		t.Errorf("back to uint16: %s", got)
	}

	// A new pair at row 0 breaks the pair starting at row 1.
	m = keys(m, "down", "t", "t", "up", "t", "t")
	if got := types(); got != "uint32 + uint16 uint16 uint16" {
		t.Errorf("overlapping pair: %s", got)
	}

	// t on the second half undoes the pair.
	m = keys(m, "down", "t")
	if got := types(); got != "uint16 uint16 uint16 uint16 uint16" {
		t.Errorf("t on second half: %s", got)
	}
}

func TestLastRowRefuses32(t *testing.T) {
	m := newTestModel(Options{})
	m = keys(m, "G", "t")
	if m.rows[19].typ != decode.Int16 {
		t.Fatalf("type = %v", m.rows[19].typ)
	}
	m = keys(m, "t")
	if m.rows[19].typ != decode.Uint16 {
		t.Errorf("type = %v, want uint16 after refusal", m.rows[19].typ)
	}
	if !strings.Contains(m.render(), "A 32-bit type needs two registers; this is the last one in the range.") {
		t.Error("refusal message not shown")
	}
}

func TestOrder(t *testing.T) {
	m := newTestModel(Options{Order: decode.CDAB})
	m = keys(m, "o")
	if !strings.Contains(m.render(), "Byte order only applies to 32-bit types.") {
		t.Error("order on 16-bit row should be refused")
	}
	m = keys(m, "t", "t", "t", "t") // float32
	if m.rows[0].order != decode.CDAB {
		t.Errorf("initial order = %v, want --order CDAB", m.rows[0].order)
	}
	m = keys(m, "o", "o", "o")
	if m.rows[0].order != decode.ABCD {
		t.Errorf("order = %v", m.rows[0].order)
	}
	m = keys(m, "down", "o")
	if !strings.Contains(m.render(), "Byte order only applies") {
		t.Error("order on second half should be refused")
	}
}

func TestBitTable(t *testing.T) {
	m := newTestModel(Options{Start: address.Addr{Table: address.Coil}, Count: 5})
	for _, k := range []string{"t", "o"} {
		m = keys(m, k)
		if !strings.Contains(m.render(), "This table holds bits; the type is always bool.") {
			t.Errorf("%s on bits: message missing", k)
		}
		if m.rows[0].typ != decode.Bool {
			t.Errorf("type changed to %v", m.rows[0].typ)
		}
	}
}

func TestConventionCycle(t *testing.T) {
	m := sampleModel(Options{})
	want := []string{"40003 → Holding", "3 → Holding", "2 → Holding", "40003 → Holding"}
	for i, w := range want {
		if i > 0 {
			m = keys(m, "c")
		}
		if !strings.Contains(m.render(), " "+w) {
			t.Errorf("step %d: translation %q not found", i, w)
		}
	}
	m = keys(m, "c")
	if !strings.Contains(m.header(), "· 1–20 ·") {
		t.Errorf("header in base 1: %s", m.header())
	}
}

func TestStaleValues(t *testing.T) {
	m := sampleModel(Options{})
	out := m.render()
	if !strings.Contains(out, "100  12s ago · timeout") {
		t.Error("stale value without age")
	}
	// A value never read shows "—" and the reason.
	r := sampleResult()
	r.Cells[8] = poller.Cell{Err: &transport.TimeoutError{}}
	m = send(m, resultMsg(r))
	if !strings.Contains(m.render(), "—  timeout") {
		t.Error("never-read value should show — and the reason")
	}
	// A pair with one stale half is stale.
	r.Cells[3] = poller.Cell{Raw: 0x44A2, LastOK: t0.Add(-5 * time.Second), Err: &transport.TimeoutError{}}
	m = send(m, resultMsg(r))
	if !strings.Contains(m.render(), "1300.0  5s ago · timeout") {
		t.Errorf("stale pair:\n%s", m.render())
	}
}

func TestNoStaleWithoutMarker(t *testing.T) {
	// Every failed cell with a past value must show its age.
	r := sampleResult()
	for i := range r.Cells {
		r.Cells[i].OK = false
		r.Cells[i].Err = &transport.TimeoutError{}
		r.Cells[i].LastOK = t0.Add(-3 * time.Second)
		r.Cells[i].Missing = false
	}
	m := send(newTestModel(Options{}), resultMsg(r))
	for _, line := range strings.Split(m.render(), "\n")[3:21] {
		if strings.HasPrefix(line, "│ 4") || strings.HasPrefix(line, "│▶4") {
			if !strings.Contains(line, "3s ago · timeout") {
				t.Errorf("unmarked stale value: %q", line)
			}
		}
	}
}

func TestHealthHeader(t *testing.T) {
	m := newTestModel(Options{})
	if !strings.HasSuffix(m.header(), "◌ connecting") {
		t.Errorf("header: %q", m.header())
	}
	for h, want := range map[poller.Health]string{
		poller.OK: "● ok", poller.Degraded: "◐ degraded", poller.Down: "○ no response",
	} {
		m = send(m, resultMsg(poller.CycleResult{Cells: make([]poller.Cell, 20), Health: h}))
		if !strings.HasSuffix(m.header(), want) {
			t.Errorf("%v: header %q", h, m.header())
		}
	}
}

func TestPauseKey(t *testing.T) {
	m := newTestModel(Options{})
	m = keys(m, "p")
	if !m.ctl.Paused() || !strings.Contains(m.header(), "⏸ paused") {
		t.Error("p did not pause")
	}
	m = keys(m, "p")
	if m.ctl.Paused() {
		t.Error("p did not resume")
	}
}

func TestQuitCodes(t *testing.T) {
	m := newTestModel(Options{})
	mm, cmd := m.Update(key("q"))
	if cmd == nil || mm.(Model).QuitCode() != 0 {
		t.Error("q should quit with 0")
	}
	mm, cmd = m.Update(key("ctrl+c"))
	if cmd == nil || mm.(Model).QuitCode() != 130 {
		t.Errorf("ctrl+c should quit with 130, got %d", mm.(Model).QuitCode())
	}
}

func TestScrolling(t *testing.T) {
	m := newTestModel(Options{Count: 100, Start: address.Addr{Table: address.HoldingRegister}})
	m = keys(m, "G")
	if m.sel != 99 || m.top != 100-m.visibleRows() {
		t.Errorf("sel %d top %d", m.sel, m.top)
	}
	if !strings.Contains(m.render(), "▶40100") {
		t.Error("selected row not visible")
	}
	m = keys(m, "g")
	if m.sel != 0 || m.top != 0 {
		t.Errorf("sel %d top %d", m.sel, m.top)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.sel != m.visibleRows() {
		t.Errorf("pgdown sel = %d", m.sel)
	}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 30})
	if m.sel < m.top || m.sel >= m.top+m.visibleRows() {
		t.Error("selection hidden after resize")
	}
}

func TestStatusExplainsError(t *testing.T) {
	m := sampleModel(Options{})
	m = keys(m, "down", "down", "down", "down") // row 6, missing
	out := statusText(m)
	if !strings.Contains(out, "Illegal data address (exception 02).") || !strings.Contains(out, "will not be read again") {
		t.Errorf("missing explanation: %q", out)
	}
}

func TestNoResponseHint(t *testing.T) {
	m := newTestModel(Options{Target: "/dev/ttyUSB0", Serial: &Serial{Baud: 9600, Parity: 'E', StopBits: 1}})
	failed := poller.CycleResult{Cells: make([]poller.Cell, 20), Health: poller.Down}
	for i := range failed.Cells {
		failed.Cells[i].Err = &transport.TimeoutError{}
	}
	for i := 0; i < noResponseCycles; i++ {
		m = send(m, resultMsg(failed))
	}
	if strings.Contains(m.render(), "No response") {
		t.Error("hint shown too early")
	}
	m = send(m, resultMsg(failed))
	out := statusText(m)
	want := "No response. Check baud (9600), parity (E), stop bits (1) and unit ID (1). " +
		"Many devices use parity N: try --parity N."
	if out != want {
		t.Errorf("hint = %q", out)
	}
}

func TestColorsOnlyWhenEnabled(t *testing.T) {
	m := sampleModel(Options{Color: true})
	out := m.render()
	if !strings.Contains(out, "\x1b[") {
		t.Error("no styling with colors enabled")
	}
	if strings.Contains(m.ordersLine(), "[CDAB") {
		t.Error("brackets should only be used without colors")
	}
	for i, line := range strings.Split(out, "\n") {
		if w := width(line); w > 80 {
			t.Errorf("line %d is %d columns wide", i+1, w)
		}
	}
}

func TestExceptionExplanationsFit(t *testing.T) {
	m := newTestModel(Options{})
	for code := codec.ExceptionCode(1); code <= 11; code++ {
		r := sampleResult()
		r.Cells[0] = poller.Cell{Err: &codec.ExceptionError{Function: 3, Code: code}}
		m = send(m, resultMsg(r))
		text := strings.ReplaceAll(m.explainSelected(), "\n", " ")
		lines := wrap(text, m.width-2, 3)
		if len(lines) > 2 {
			t.Errorf("exception %d explanation needs %d lines: %q", code, len(lines), text)
		}
	}
}

func TestWrap(t *testing.T) {
	got := wrap("aaa bbb ccc ddd", 7, 2)
	if len(got) != 2 || got[0] != "aaa bbb" || got[1] != "ccc ddd" {
		t.Errorf("wrap = %q", got)
	}
	got = wrap("aaa bbb ccc ddd eee", 7, 2)
	if len(got) != 2 || got[1] != "ccc ..." {
		t.Errorf("wrap overflow = %q", got)
	}
}

// statusText joins the two status lines.
func statusText(m Model) string {
	s := m.statusLines()
	return strings.TrimSpace(strings.TrimSpace(s[0]) + " " + strings.TrimSpace(s[1]))
}
