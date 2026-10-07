// Package transport sends Modbus read requests over TCP or a serial line
// (RTU), one at a time, and records every raw frame in a FrameLog.
package transport

import (
	"context"
	"fmt"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/codec"
)

// Transport performs one read request at a time.
type Transport interface {
	// Do sends a read request and returns the values read, or a
	// classifiable error (codec errors, *TimeoutError, *ConnError,
	// *EchoError, or the context error).
	Do(ctx context.Context, req codec.ReadRequest) (codec.ReadResponse, error)
	Close() error
}

// TimeoutError means no complete response arrived within the timeout.
type TimeoutError struct {
	After time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timeout (%v)", e.After)
}

// ConnError is a connection-level failure (TCP connect, reset, EOF, or
// waiting before the next reconnection attempt).
type ConnError struct {
	Msg string
	Err error
}

func (e *ConnError) Error() string { return e.Msg }

func (e *ConnError) Unwrap() error { return e.Err }

// okNote describes a successful response in the frames panel,
// e.g. "FC03 · 4 bytes · ok".
func okNote(req codec.ReadRequest, dataBytes int) string {
	return fmt.Sprintf("FC%02d · %d bytes · ok", req.Function, dataBytes)
}
