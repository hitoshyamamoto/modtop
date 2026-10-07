package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/hitoshyamamoto/modtop/internal/poller"
)

// ExitPanic is the exit code after an internal error (a recovered panic).
const ExitPanic = 70

// signalCodes maps the signals that end a session to their exit codes
// (128 + signal number).
var signalCodes = map[os.Signal]int{
	syscall.SIGINT:  130,
	syscall.SIGTERM: 143,
	syscall.SIGHUP:  129,
}

// Source is what the UI needs from the poller.
type Source interface {
	Controller
	Run(ctx context.Context)
	Results() <-chan poller.CycleResult
}

// PanicError reports a recovered panic; the terminal has been restored.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("internal error: %v\n\n%s\n"+
		"This is a bug in modtop. Please report it at https://github.com/hitoshyamamoto/modtop/issues\n"+
		"with the message above and the command line you used.", e.Value, e.Stack)
}

// Run shows the UI until the user quits or a signal arrives, then stops
// the poller and returns the process exit code. The caller closes the
// transport. The terminal is restored on every path, panics included.
func Run(ctx context.Context, opts Options, src Source, log *poller.FrameLog) (int, error) {
	return run(ctx, opts, src, log)
}

func run(ctx context.Context, opts Options, src Source, log *poller.FrameLog, progOpts ...tea.ProgramOption) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The goroutines below may kill the program, which is only safe once
	// it is running: they wait for the model's Init to signal it.
	started := make(chan struct{})
	model := NewModel(opts, src, log)
	model.started = started
	prog := tea.NewProgram(model, append([]tea.ProgramOption{tea.WithoutSignalHandler()}, progOpts...)...)

	var (
		mu       sync.Mutex
		panicErr *PanicError
		sigCode  int
	)
	// guard waits for the program to start, runs fn and turns a panic
	// into a clean shutdown.
	guard := func(fn func()) {
		select {
		case <-started:
		case <-ctx.Done():
			return
		}
		defer func() {
			if r := recover(); r != nil {
				mu.Lock()
				if panicErr == nil {
					panicErr = &PanicError{Value: r, Stack: debug.Stack()}
				}
				mu.Unlock()
				cancel()
				prog.Kill()
			}
		}()
		fn()
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		guard(func() { src.Run(ctx) })
	}()
	go func() {
		defer wg.Done()
		guard(func() {
			for {
				select {
				case r := <-src.Results():
					prog.Send(resultMsg(r))
				case <-ctx.Done():
					return
				}
			}
		})
	}()
	go func() {
		defer wg.Done()
		guard(func() {
			select {
			case s := <-sigs:
				mu.Lock()
				sigCode = signalCodes[s]
				mu.Unlock()
				prog.Send(quitMsg{code: signalCodes[s]})
			case <-ctx.Done():
			}
		})
	}()

	final, err := prog.Run()
	cancel()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	switch {
	case panicErr != nil:
		return ExitPanic, panicErr
	case errors.Is(err, tea.ErrProgramPanic):
		// Bubble Tea already printed the panic and its stack.
		return ExitPanic, errors.New("internal error (see above). This is a bug in modtop. " +
			"Please report it at https://github.com/hitoshyamamoto/modtop/issues")
	case sigCode != 0:
		return sigCode, nil
	case err != nil:
		return 1, err
	}
	m, _ := final.(Model)
	return m.QuitCode(), nil
}
