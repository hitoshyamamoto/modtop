package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/codec"
)

// DefaultTCPPort is the standard Modbus TCP port.
const DefaultTCPPort = "502"

// Reconnection backoff limits.
const (
	backoffMin = time.Second
	backoffMax = 30 * time.Second
)

// NormalizeTCPAddr turns "host", "host:port", "[v6]" or "[v6]:port" into
// "host:port", adding the default port 502 when missing.
func NormalizeTCPAddr(s string) (string, error) {
	if host, port, err := net.SplitHostPort(s); err == nil {
		if host == "" {
			return "", fmt.Errorf("target %q has no host: use host[:port], e.g. 10.1.8.99:502", s)
		}
		return net.JoinHostPort(host, port), nil
	}
	host := s
	if len(host) > 1 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	} else if net.ParseIP(host) != nil && net.ParseIP(host).To4() == nil {
		return "", fmt.Errorf("target %q: IPv6 addresses go in brackets, e.g. [%s]:502", s, host)
	}
	if host == "" {
		return "", fmt.Errorf("target %q has no host: use host[:port], e.g. 10.1.8.99:502", s)
	}
	return net.JoinHostPort(host, DefaultTCPPort), nil
}

// TCP is a Modbus TCP transport: one connection, one request at a time.
// A late response to an earlier request is recognized by its transaction ID
// and dropped. A timeout that leaves part of a frame unread closes the
// connection, since the stream can no longer be resynchronized.
type TCP struct {
	addr    string
	timeout time.Duration
	log     *FrameLog

	mu        sync.Mutex
	conn      net.Conn
	tid       uint16
	failures  int       // consecutive connection failures
	nextDial  time.Time // earliest time for the next reconnection
	lastError error
}

// DialTCP connects once to addr ("host:port"). The returned error, if any,
// is a *ConnError with a message suitable for the user.
func DialTCP(ctx context.Context, addr string, timeout time.Duration, log *FrameLog) (*TCP, error) {
	t := &TCP{addr: addr, timeout: timeout, log: log}
	if err := t.dial(ctx); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *TCP) dial(ctx context.Context) error {
	d := net.Dialer{Timeout: t.timeout}
	conn, err := d.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		return &ConnError{Msg: describeDialError(err, t.addr), Err: err}
	}
	t.conn = conn
	return nil
}

func describeDialError(err error, addr string) string {
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Sprintf("connection refused at %s", addr)
	case errors.As(err, &dnsErr):
		return fmt.Sprintf("host not found: %s", addr)
	case isTimeout(err):
		return fmt.Sprintf("timed out connecting to %s", addr)
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return fmt.Sprintf("network or host unreachable: %s", addr)
	}
	return fmt.Sprintf("failed to connect to %s: %v", addr, err)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

// Do implements Transport.
func (t *TCP) Do(ctx context.Context, req codec.ReadRequest) (codec.ReadResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return codec.ReadResponse{}, err
	}
	if t.conn == nil {
		if wait := time.Until(t.nextDial); wait > 0 {
			return codec.ReadResponse{}, &ConnError{
				Msg: fmt.Sprintf("no connection to %s; retrying in %ds", t.addr, int(wait.Seconds()+0.999)),
				Err: t.lastError,
			}
		}
		if err := t.dial(ctx); err != nil {
			if ctx.Err() != nil {
				return codec.ReadResponse{}, ctx.Err()
			}
			t.connFailed(err)
			return codec.ReadResponse{}, err
		}
	}

	t.tid++
	if t.tid == 0 {
		t.tid = 1
	}
	frame, err := codec.EncodeTCP(req, t.tid)
	if err != nil {
		return codec.ReadResponse{}, err
	}

	deadline := time.Now().Add(t.timeout)
	_ = t.conn.SetDeadline(deadline)
	conn := t.conn
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	t.log.Add(Frame{At: time.Now(), Dir: TX, Raw: frame, Note: req.String()})
	if _, err := conn.Write(frame); err != nil {
		return codec.ReadResponse{}, t.ioFailed(ctx, err)
	}

	for {
		raw, partial, err := t.readFrame()
		if err != nil {
			if partial {
				// Part of a frame was consumed: the stream is out of sync
				// and only a new connection can recover it.
				t.closeConn()
			}
			return codec.ReadResponse{}, t.ioFailed(ctx, err)
		}
		resp, err := codec.DecodeTCP(raw, req, t.tid)
		var mismatch *codec.MismatchError
		if errors.As(err, &mismatch) && mismatch.Field == codec.FieldTransaction {
			// A late response to an earlier request: drop it and keep reading.
			t.log.Add(Frame{At: time.Now(), Dir: RX, Raw: raw,
				Note: fmt.Sprintf("stale transaction ID %d · discarded", mismatch.Got)})
			continue
		}
		note := okNote(req, req.DataLen())
		if err != nil {
			note = err.Error()
		}
		t.log.Add(Frame{At: time.Now(), Dir: RX, Raw: raw, Note: note})
		if err == nil {
			t.failures = 0
		}
		return resp, err
	}
}

// readFrame reads one MBAP frame from the connection. On error, partial
// reports whether some bytes of the frame were already consumed.
func (t *TCP) readFrame() (frame []byte, partial bool, err error) {
	header := make([]byte, 6, 6+254)
	if n, err := io.ReadFull(t.conn, header); err != nil {
		return nil, n > 0, err
	}
	size, err := codec.TCPFrameLen(header)
	if err != nil {
		// The stream is out of sync; only a new connection can recover.
		t.log.Add(Frame{At: time.Now(), Dir: RX, Raw: header, Note: err.Error()})
		t.closeConn()
		return nil, false, err
	}
	frame = header[:size]
	if _, err := io.ReadFull(t.conn, frame[6:]); err != nil {
		return nil, true, err
	}
	return frame, false, nil
}

// ioFailed classifies a read/write error, logs it and updates the
// connection state.
func (t *TCP) ioFailed(ctx context.Context, err error) error {
	var malformed *codec.MalformedError
	switch {
	case errors.As(err, &malformed):
		return err
	case ctx.Err() != nil:
		return ctx.Err()
	case isTimeout(err):
		// Keep the connection (unless Do already closed it after a partial
		// frame): a late response is dropped by its transaction ID.
		te := &TimeoutError{After: t.timeout}
		t.log.Add(Frame{At: time.Now(), Dir: RX, Note: te.Error()})
		return te
	}
	ce := &ConnError{Msg: fmt.Sprintf("connection lost with %s: %v", t.addr, err), Err: err}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		ce.Msg = fmt.Sprintf("connection closed by the device %s", t.addr)
	}
	t.log.Add(Frame{At: time.Now(), Dir: RX, Note: ce.Msg})
	t.connFailed(ce)
	return ce
}

// connFailed closes the connection and schedules the next attempt with
// exponential backoff: 1 s, 2 s, 4 s ... up to 30 s.
func (t *TCP) connFailed(err error) {
	t.closeConn()
	wait := backoffMax
	if t.failures < 5 {
		wait = min(backoffMin<<t.failures, backoffMax)
	}
	t.failures++
	t.nextDial = time.Now().Add(wait)
	t.lastError = err
}

func (t *TCP) closeConn() {
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
}

// Close closes the connection.
func (t *TCP) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeConn()
	return nil
}
