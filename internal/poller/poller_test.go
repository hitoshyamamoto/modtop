package poller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/codec"
	"github.com/hitoshyamamoto/modtop/internal/sim"
	"github.com/hitoshyamamoto/modtop/internal/transport"
)

const fc3 = codec.ReadHoldingRegisters

func holding(pdu uint16) address.Addr {
	return address.Addr{Table: address.HoldingRegister, PDU: pdu}
}

// simTransport starts the simulator and a TCP transport connected to it.
func simTransport(t *testing.T, timeout time.Duration) (*sim.Device, transport.Transport) {
	t.Helper()
	dev := sim.NewDevice(1)
	srv, err := sim.ListenTCP("127.0.0.1:0", dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	tr, err := transport.DialTCP(context.Background(), srv.Addr(), timeout, transport.NewFrameLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return dev, tr
}

func runPoller(t *testing.T, p *Poller) (cancel func(), done <-chan struct{}) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	ch := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(ch)
	}()
	t.Cleanup(func() {
		cancelFn()
		<-ch
	})
	return cancelFn, ch
}

func next(t *testing.T, p *Poller) CycleResult {
	t.Helper()
	select {
	case r := <-p.Results():
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("no cycle result")
	}
	return CycleResult{}
}

func TestCycleValues(t *testing.T) {
	dev, tr := simTransport(t, time.Second)
	dev.Set(fc3, 0, 0x4248, 0x0000, 0x8000, 0x44A2)
	p := New(tr, Config{Start: holding(0), Count: 4, Unit: 1, Interval: 50 * time.Millisecond})
	runPoller(t, p)
	r := next(t, p)
	want := []uint16{0x4248, 0, 0x8000, 0x44A2}
	for i, c := range r.Cells {
		if !c.OK || c.Raw != want[i] || c.LastOK.IsZero() {
			t.Errorf("cell %d = %+v", i, c)
		}
	}
	if r.Stats.OK != 1 || r.Health != OK {
		t.Errorf("stats %+v health %v", r.Stats, r.Health)
	}
}

func TestBlocksSplit(t *testing.T) {
	dev, tr := simTransport(t, time.Second)
	for i := uint16(0); i < 250; i++ {
		dev.Set(fc3, i, i)
	}
	p := New(tr, Config{Start: holding(0), Count: 250, Unit: 1, Interval: time.Hour})
	if len(p.blocks) != 2 || p.blocks[0].count != 125 || p.blocks[1].count != 125 {
		t.Fatalf("blocks = %+v", p.blocks)
	}
	runPoller(t, p)
	r := next(t, p)
	if dev.Requests() != 2 || r.Cells[249].Raw != 249 {
		t.Errorf("requests %d, last %d", dev.Requests(), r.Cells[249].Raw)
	}
	bits := New(tr, Config{Start: address.Addr{Table: address.Coil}, Count: 250, Unit: 1})
	if len(bits.blocks) != 1 {
		t.Errorf("bit blocks = %+v", bits.blocks)
	}
}

func TestBlockFallbackAndMissing(t *testing.T) {
	dev, tr := simTransport(t, time.Second)
	dev.Set(fc3, 0, 10, 11, 12, 13, 14, 15)
	dev.SetMissing(fc3, 3)
	p := New(tr, Config{Start: holding(0), Count: 6, Unit: 1, Interval: 20 * time.Millisecond})
	runPoller(t, p)

	r := next(t, p)
	// 1 block request (exception 02) + 6 individual reads.
	if got := dev.Requests(); got != 7 {
		t.Errorf("first cycle requests = %d, want 7", got)
	}
	for i, c := range r.Cells {
		if i == 3 {
			if !c.Missing || c.OK {
				t.Errorf("cell 3 = %+v, want missing", c)
			}
			continue
		}
		if !c.OK || c.Raw != uint16(10+i) {
			t.Errorf("cell %d = %+v", i, c)
		}
	}
	if r.Health != OK {
		t.Errorf("missing addresses must not count as failures: health %v", r.Health)
	}

	// Next cycles: individual mode, never reading the missing address again.
	before := dev.Requests()
	next(t, p)
	if got := dev.Requests() - before; got != 5 {
		t.Errorf("later cycle requests = %d, want 5", got)
	}
}

func TestSingleMode(t *testing.T) {
	dev, tr := simTransport(t, time.Second)
	p := New(tr, Config{Start: holding(0), Count: 4, Unit: 1, Interval: time.Hour, Single: true})
	runPoller(t, p)
	next(t, p)
	if got := dev.Requests(); got != 4 {
		t.Errorf("requests = %d, want 4", got)
	}
}

func TestStaleValueKept(t *testing.T) {
	dev, tr := simTransport(t, 100*time.Millisecond)
	dev.Set(fc3, 0, 42)
	p := New(tr, Config{Start: holding(0), Count: 1, Unit: 1, Interval: 20 * time.Millisecond})
	runPoller(t, p)
	first := next(t, p)
	dev.SetSilent(fc3, 0, true)
	var r CycleResult
	for i := 0; i < 5; i++ {
		r = next(t, p)
		if !r.Cells[0].OK {
			break
		}
	}
	c := r.Cells[0]
	var te *transport.TimeoutError
	if c.OK || c.Raw != 42 || !c.LastOK.Equal(first.Cells[0].LastOK) || !errors.As(c.Err, &te) {
		t.Errorf("cell = %+v", c)
	}
	if ShortReason(c.Err) != "timeout" || r.Stats.Timeouts == 0 {
		t.Errorf("reason %q, stats %+v", ShortReason(c.Err), r.Stats)
	}
}

func TestComputeHealth(t *testing.T) {
	ok := outcome{success: true}
	fail := outcome{failure: true}
	partial := outcome{success: true, failure: true}
	many := func(o outcome, n int) []outcome {
		out := make([]outcome, n)
		for i := range out {
			out[i] = o
		}
		return out
	}
	tests := []struct {
		name string
		h    []outcome
		want Health
	}{
		{"no cycle", nil, Connecting},
		{"one ok", many(ok, 1), OK},
		{"ten ok", many(ok, 10), OK},
		{"one partial", append(many(ok, 9), partial), Degraded},
		{"old failure in window", append([]outcome{fail}, many(ok, 9)...), Degraded},
		{"one failed cycle", append(many(ok, 5), fail), Degraded},
		{"two failed cycles", append(many(ok, 5), fail, fail), Degraded},
		{"three failed cycles", append(many(ok, 5), fail, fail, fail), Down},
		{"first three failed", many(fail, 3), Down},
		{"recovering", append(many(fail, 5), ok), Degraded},
	}
	for _, tt := range tests {
		if got := computeHealth(tt.h); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHealthTransitions(t *testing.T) {
	dev, tr := simTransport(t, 50*time.Millisecond)
	p := New(tr, Config{Start: holding(0), Count: 1, Unit: 1, Interval: 10 * time.Millisecond})
	runPoller(t, p)
	waitHealth(t, p, OK)
	dev.SkipNext(1)
	waitHealth(t, p, Degraded)
	dev.SetSilent(fc3, 0, true)
	waitHealth(t, p, Down)
	dev.SetSilent(fc3, 0, false)
	if r := waitHealth(t, p, Degraded); !r.Cells[0].OK {
		t.Errorf("recovered cell = %+v", r.Cells[0])
	}
}

// waitHealth reads results until one has the wanted health.
func waitHealth(t *testing.T, p *Poller, want Health) CycleResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r := next(t, p); r.Health == want {
			return r
		}
	}
	t.Fatalf("health never became %v", want)
	return CycleResult{}
}

// fakeTransport answers every request after a delay, honoring ctx.
type fakeTransport struct {
	delay time.Duration
	mu    sync.Mutex
	calls []time.Time
}

func (f *fakeTransport) Do(ctx context.Context, req codec.ReadRequest) (codec.ReadResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, time.Now())
	f.mu.Unlock()
	select {
	case <-time.After(f.delay):
		return codec.ReadResponse{Values: make([]uint16, req.Quantity)}, nil
	case <-ctx.Done():
		return codec.ReadResponse{}, ctx.Err()
	}
}

func (f *fakeTransport) Close() error { return nil }

func (f *fakeTransport) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestSlowCycleNoOverlap(t *testing.T) {
	f := &fakeTransport{delay: 80 * time.Millisecond}
	p := New(f, Config{Start: holding(0), Count: 1, Unit: 1, Interval: 30 * time.Millisecond})
	runPoller(t, p)
	var results []CycleResult
	for i := 0; i < 4; i++ {
		results = append(results, next(t, p))
	}
	for i := 1; i < len(results); i++ {
		prevEnd := results[i-1].Started.Add(results[i-1].Duration)
		if results[i].Started.Before(prevEnd) {
			t.Errorf("cycle %d started before cycle %d ended", i, i-1)
		}
		if gap := results[i].Started.Sub(prevEnd); gap > 30*time.Millisecond {
			t.Errorf("cycle %d started %v after the previous one ended, want immediately", i, gap)
		}
	}
}

func TestInterval(t *testing.T) {
	f := &fakeTransport{}
	p := New(f, Config{Start: holding(0), Count: 1, Unit: 1, Interval: 100 * time.Millisecond})
	runPoller(t, p)
	a := next(t, p)
	b := next(t, p)
	if d := b.Started.Sub(a.Started); d < 95*time.Millisecond || d > 200*time.Millisecond {
		t.Errorf("interval between starts = %v, want ~100ms", d)
	}
}

func TestPause(t *testing.T) {
	f := &fakeTransport{}
	p := New(f, Config{Start: holding(0), Count: 1, Unit: 1, Interval: 20 * time.Millisecond})
	runPoller(t, p)
	next(t, p)
	p.SetPaused(true)
	if !p.Paused() {
		t.Fatal("Paused() = false")
	}
	time.Sleep(50 * time.Millisecond) // let an in-flight cycle finish
	before := f.count()
	time.Sleep(150 * time.Millisecond)
	if got := f.count(); got != before {
		t.Errorf("%d requests while paused", got-before)
	}
	p.SetPaused(false)
	select {
	case <-p.Results(): // drain a result published before the pause, if any
	default:
	}
	next(t, p)
	if f.count() == before {
		t.Error("no request after resume")
	}
}

func TestCancel(t *testing.T) {
	const timeout = 500 * time.Millisecond
	dev, tr := simTransport(t, timeout)
	dev.SetSilent(fc3, 0, true)
	p := New(tr, Config{Start: holding(0), Count: 1, Unit: 1, Interval: 10 * time.Millisecond})
	cancel, done := runPoller(t, p)
	time.Sleep(100 * time.Millisecond) // a request is in flight
	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(timeout + 100*time.Millisecond):
		t.Fatal("poller did not stop within timeout + 100ms")
	}
	if d := time.Since(start); d > timeout+100*time.Millisecond {
		t.Errorf("stop took %v", d)
	}
}

func TestSlowReaderNeverBlocks(t *testing.T) {
	f := &fakeTransport{}
	p := New(f, Config{Start: holding(0), Count: 1, Unit: 1, Interval: 5 * time.Millisecond})
	runPoller(t, p)
	time.Sleep(100 * time.Millisecond) // nobody reads results
	if f.count() < 5 {
		t.Errorf("poller stalled: %d requests", f.count())
	}
	r := next(t, p)
	if time.Since(r.Started) > 50*time.Millisecond {
		t.Errorf("result is stale: %v old", time.Since(r.Started))
	}
}

func TestShortReason(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{&codec.ExceptionError{Code: 2}, "exc 02"},
		{&transport.TimeoutError{}, "timeout"},
		{&codec.CRCError{}, "invalid CRC"},
		{&codec.MalformedError{}, "malformed response"},
		{&transport.ConnError{}, "no connection"},
		{&transport.EchoError{}, "echo"},
		{errors.New("x"), "error"},
	}
	for _, tt := range tests {
		if got := ShortReason(tt.err); got != tt.want {
			t.Errorf("ShortReason(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}
