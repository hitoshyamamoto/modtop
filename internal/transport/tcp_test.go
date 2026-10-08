package transport

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/codec"
	"github.com/hitoshyamamoto/modtop/internal/sim"
)

const fc3 = codec.ReadHoldingRegisters

func startSim(t *testing.T) (*sim.Device, *sim.TCPServer) {
	t.Helper()
	dev := sim.NewDevice(1)
	srv, err := sim.ListenTCP("127.0.0.1:0", dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return dev, srv
}

func dialSim(t *testing.T, addr string, timeout time.Duration) (*TCP, *FrameLog) {
	t.Helper()
	log := NewFrameLog()
	tr, err := DialTCP(context.Background(), addr, timeout, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr, log
}

func req(addr, qty uint16) codec.ReadRequest {
	return codec.ReadRequest{Unit: 1, Function: fc3, Address: addr, Quantity: qty}
}

func TestTCPSuccess(t *testing.T) {
	dev, srv := startSim(t)
	dev.Set(fc3, 2, 0x8000, 0x44A2)
	tr, log := dialSim(t, srv.Addr(), time.Second)

	resp, err := tr.Do(context.Background(), req(2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp.Values, []uint16{0x8000, 0x44A2}) {
		t.Errorf("values = %04X", resp.Values)
	}
	frames, total := log.Snapshot()
	if total != 2 || frames[0].Dir != TX || frames[1].Dir != RX {
		t.Fatalf("frames = %+v", frames)
	}
	if frames[0].Note != "FC03 · PDU 2 · qty 2" || frames[1].Note != "FC03 · 4 bytes · ok" {
		t.Errorf("notes = %q, %q", frames[0].Note, frames[1].Note)
	}
	// Transaction IDs start at 1 and increment.
	if frames[0].Raw[1] != 1 {
		t.Errorf("first transaction ID = %d", frames[0].Raw[1])
	}
	if _, err := tr.Do(context.Background(), req(2, 2)); err != nil {
		t.Fatal(err)
	}
	frames, _ = log.Snapshot()
	if frames[2].Raw[1] != 2 {
		t.Errorf("second transaction ID = %d", frames[2].Raw[1])
	}
}

func TestTCPTransactionWrap(t *testing.T) {
	_, srv := startSim(t)
	tr, log := dialSim(t, srv.Addr(), time.Second)
	tr.tid = 0xFFFF
	if _, err := tr.Do(context.Background(), req(0, 1)); err != nil {
		t.Fatal(err)
	}
	frames, _ := log.Snapshot()
	if frames[0].Raw[0] != 0 || frames[0].Raw[1] != 1 {
		t.Errorf("transaction ID after 0xFFFF = % X, want 00 01", frames[0].Raw[:2])
	}
}

func TestTCPException(t *testing.T) {
	dev, srv := startSim(t)
	dev.SetMissing(fc3, 6)
	tr, log := dialSim(t, srv.Addr(), time.Second)
	_, err := tr.Do(context.Background(), req(6, 1))
	var exc *codec.ExceptionError
	if !errors.As(err, &exc) || exc.Code != codec.ExcIllegalDataAddress {
		t.Fatalf("err = %v", err)
	}
	frames, _ := log.Snapshot()
	if got := frames[len(frames)-1].Note; got != "exc 02 · illegal data address" {
		t.Errorf("note = %q", got)
	}
}

func TestTCPTimeout(t *testing.T) {
	dev, srv := startSim(t)
	dev.SetSilent(fc3, 0, true)
	tr, log := dialSim(t, srv.Addr(), 100*time.Millisecond)
	start := time.Now()
	_, err := tr.Do(context.Background(), req(0, 1))
	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v, want timeout", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("timeout took %v", d)
	}
	frames, _ := log.Snapshot()
	if got := frames[len(frames)-1].Note; got != "timeout (100ms)" {
		t.Errorf("note = %q", got)
	}
	// The connection survives a timeout.
	dev.SetSilent(fc3, 0, false)
	if _, err := tr.Do(context.Background(), req(0, 1)); err != nil {
		t.Fatalf("after timeout: %v", err)
	}
}

func TestTCPLateResponseDropped(t *testing.T) {
	// A response arriving after its timeout must not be taken as the
	// answer to the next request.
	dev, srv := startSim(t)
	dev.Set(fc3, 0, 111)
	dev.Set(fc3, 1, 222)
	dev.SetDelay(fc3, 0, 300*time.Millisecond)
	tr, _ := dialSim(t, srv.Addr(), 100*time.Millisecond)
	if _, err := tr.Do(context.Background(), req(0, 1)); err == nil {
		t.Fatal("expected timeout")
	}
	tr.timeout = time.Second
	resp, err := tr.Do(context.Background(), req(1, 1))
	if err != nil || resp.Values[0] != 222 {
		t.Fatalf("got %v, %v; want 222", resp.Values, err)
	}
}

func TestTCPStaleTransactionID(t *testing.T) {
	dev, srv := startSim(t)
	dev.Set(fc3, 0, 42)
	dev.SetFaults(sim.Faults{StaleTID: true})
	tr, log := dialSim(t, srv.Addr(), time.Second)
	resp, err := tr.Do(context.Background(), req(0, 1))
	if err != nil || resp.Values[0] != 42 {
		t.Fatalf("got %v, %v", resp.Values, err)
	}
	frames, _ := log.Snapshot()
	if len(frames) != 3 || !strings.Contains(frames[1].Note, "discarded") {
		t.Errorf("frames = %+v", frames)
	}
}

func TestTCPReconnect(t *testing.T) {
	dev, srv := startSim(t)
	dev.Set(fc3, 0, 7)
	tr, _ := dialSim(t, srv.Addr(), time.Second)

	dev.SetFaults(sim.Faults{CloseConn: true})
	_, err := tr.Do(context.Background(), req(0, 1))
	var ce *ConnError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want ConnError", err)
	}
	// During the backoff no new connection is attempted.
	before := dev.Requests()
	if _, err := tr.Do(context.Background(), req(0, 1)); !errors.As(err, &ce) || !strings.Contains(err.Error(), "retrying in") {
		t.Fatalf("during backoff: %v", err)
	}
	if dev.Requests() != before {
		t.Error("request sent during backoff")
	}
	time.Sleep(backoffMin + 100*time.Millisecond)
	resp, err := tr.Do(context.Background(), req(0, 1))
	if err != nil || resp.Values[0] != 7 {
		t.Fatalf("after reconnect: %v, %v", resp.Values, err)
	}
	if tr.failures != 0 {
		t.Errorf("backoff not reset after success: failures = %d", tr.failures)
	}
}

func TestTCPBackoffGrows(t *testing.T) {
	tr := &TCP{addr: "x"}
	var waits []time.Duration
	for i := 0; i < 7; i++ {
		tr.connFailed(errors.New("x"))
		waits = append(waits, time.Until(tr.nextDial).Round(time.Second))
	}
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30}
	for i := range want {
		if waits[i] != want[i]*time.Second {
			t.Errorf("wait %d = %v, want %v", i, waits[i], want[i]*time.Second)
		}
	}
}

func TestTCPContextCancel(t *testing.T) {
	dev, srv := startSim(t)
	dev.SetSilent(fc3, 0, true)
	tr, _ := dialSim(t, srv.Addr(), 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := tr.Do(ctx, req(0, 1))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("cancel took %v", d)
	}
}

func TestDialErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	_, err = DialTCP(context.Background(), addr, time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "connection refused at "+addr) {
		t.Errorf("refused: %v", err)
	}
	_, err = DialTCP(context.Background(), "host.invalid:502", time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "host not found") {
		t.Errorf("not found: %v", err)
	}
}

func TestNormalizeTCPAddr(t *testing.T) {
	tests := []struct{ in, want string }{
		{"10.1.8.99", "10.1.8.99:502"},
		{"10.1.8.99:1502", "10.1.8.99:1502"},
		{"gateway.local", "gateway.local:502"},
		{"[::1]:502", "[::1]:502"},
		{"[::1]", "[::1]:502"},
		{"::1", ""},
		{":502", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got, err := NormalizeTCPAddr(tt.in)
		if tt.want == "" {
			if err == nil {
				t.Errorf("NormalizeTCPAddr(%q) = %q, want error", tt.in, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("NormalizeTCPAddr(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestFrameLogRing(t *testing.T) {
	log := NewFrameLog()
	for i := 0; i < FrameLogSize+5; i++ {
		log.Add(Frame{Raw: []byte{byte(i)}})
	}
	frames, total := log.Snapshot()
	if total != FrameLogSize+5 || len(frames) != FrameLogSize {
		t.Fatalf("total %d, len %d", total, len(frames))
	}
	if frames[0].Raw[0] != 5 || frames[len(frames)-1].Raw[0] != byte(FrameLogSize+4) {
		t.Errorf("order wrong: first %d last %d", frames[0].Raw[0], frames[len(frames)-1].Raw[0])
	}
	var nilLog *FrameLog
	nilLog.Add(Frame{}) // must not panic
}

// TestTCPPartialFrameTimeoutResyncs is the regression test for a timeout in
// the middle of a frame leaving the stream out of sync.
func TestTCPPartialFrameTimeoutResyncs(t *testing.T) {
	dev, srv := startSim(t)
	dev.Set(fc3, 0, 1, 2)
	dev.SetFaults(sim.Faults{Fragments: 3, FragmentDelay: 300 * time.Millisecond})
	tr, _ := dialSim(t, srv.Addr(), 200*time.Millisecond)

	_, err := tr.Do(context.Background(), req(0, 2))
	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("first request: err = %v, want timeout", err)
	}
	dev.SetFaults(sim.Faults{})
	resp, err := tr.Do(context.Background(), req(0, 2))
	if err != nil || !reflect.DeepEqual(resp.Values, []uint16{1, 2}) {
		t.Fatalf("after a partial frame: %v, %v; want [1 2]", resp.Values, err)
	}
}
