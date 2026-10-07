package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/hitoshyamamoto/modtop/internal/poller"
)

// Run shows the UI until the user quits, then stops the poller and
// returns the process exit code. The caller closes the transport.
func Run(ctx context.Context, opts Options, p *poller.Poller, log *poller.FrameLog) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	prog := tea.NewProgram(NewModel(opts, p, log))

	pollerDone := make(chan struct{})
	go func() {
		defer close(pollerDone)
		p.Run(ctx)
	}()
	go func() {
		for {
			select {
			case r := <-p.Results():
				prog.Send(resultMsg(r))
			case <-ctx.Done():
				return
			}
		}
	}()

	final, err := prog.Run()
	cancel()
	<-pollerDone
	if err != nil {
		return 1, err
	}
	return final.(Model).QuitCode(), nil
}
