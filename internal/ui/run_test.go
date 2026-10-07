package ui

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/poller"
)

// fakeSource stands in for the poller.
type fakeSource struct {
	fakeCtl
	results chan poller.CycleResult
	panicIn bool
	stopped atomic.Bool
}

func (f *fakeSource) Run(ctx context.Context) {
	if f.panicIn {
		panic("boom")
	}
	<-ctx.Done()
	f.stopped.Store(true)
}

func (f *fakeSource) Results() <-chan poller.CycleResult { return f.results }

func testRun(t *testing.T, src *fakeSource, input io.Reader) (int, error) {
	t.Helper()
	opts := Options{Target: "x", Unit: 1, Start: address.Addr{Table: address.HoldingRegister}, Count: 4, Interval: time.Second}
	type ret struct {
		code int
		err  error
	}
	done := make(chan ret, 1)
	go func() {
		code, err := run(context.Background(), opts, src, nil,
			tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24))
		done <- ret{code, err}
	}()
	select {
	case r := <-done:
		return r.code, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return")
	}
	return 0, nil
}

func newSource() *fakeSource {
	return &fakeSource{results: make(chan poller.CycleResult, 1)}
}

func TestRunQuitKey(t *testing.T) {
	src := newSource()
	pr, pw := io.Pipe()
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = pw.Write([]byte("q"))
	}()
	code, err := testRun(t, src, pr)
	if code != 0 || err != nil {
		t.Errorf("code %d, err %v", code, err)
	}
	if !src.stopped.Load() {
		t.Error("poller not stopped")
	}
}

func TestRunSignals(t *testing.T) {
	for sig, want := range map[syscall.Signal]int{syscall.SIGTERM: 143, syscall.SIGHUP: 129, syscall.SIGINT: 130} {
		src := newSource()
		pr, _ := io.Pipe()
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = syscall.Kill(syscall.Getpid(), sig)
		}()
		code, err := testRun(t, src, pr)
		if code != want || err != nil {
			t.Errorf("%v: code %d, err %v; want %d", sig, code, err, want)
		}
		if !src.stopped.Load() {
			t.Errorf("%v: poller not stopped", sig)
		}
	}
}

func TestRunPanicInGoroutine(t *testing.T) {
	src := newSource()
	src.panicIn = true
	pr, _ := io.Pipe()
	code, err := testRun(t, src, pr)
	var pe *PanicError
	if code != ExitPanic || !errors.As(err, &pe) || !strings.Contains(err.Error(), "boom") ||
		!strings.Contains(err.Error(), "report it") {
		t.Errorf("code %d, err %v", code, err)
	}
}
