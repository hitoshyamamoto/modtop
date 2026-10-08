package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"go.bug.st/serial"

	"github.com/hitoshyamamoto/modtop/internal/codec"
)

// RTUConfig describes a serial line. Data bits are always 8.
type RTUConfig struct {
	Port     string
	Baud     int
	Parity   byte // 'N', 'E' or 'O'
	StopBits int  // 1 or 2
	Timeout  time.Duration
	Force    bool // open even if the port seems to be in use

	procRoot string   // tests only
	sysRoot  string   // tests only
	lockDirs []string // tests only
}

// EchoError means the adapter echoes what is transmitted.
type EchoError struct{}

func (e *EchoError) Error() string {
	return "The RS-485 adapter is echoing what is transmitted.\n" +
		"This adapter is not supported in v0.1. Check whether its echo can be disabled."
}

// readSlice bounds each blocking read so cancellation is noticed quickly.
const readSlice = 50 * time.Millisecond

// RTU is a Modbus RTU transport over a local serial port. RTU frames carry
// no transaction ID, so a request that follows a failed one first lets the
// line settle (see settle), and reads never go past the expected frame.
type RTU struct {
	cfg    RTUConfig
	port   serial.Port
	lock   *portLock
	log    *FrameLog
	silent time.Duration // 3.5 character times
	lastIO time.Time

	// unsettled is set after a request whose answer may still be on the
	// line (timeout or bad frame); the next request first drains it.
	unsettled bool
}

// OpenRTU checks that the port is free, opens and configures it.
// Errors are *PortBusyError, *PortOpenError or a configuration error.
func OpenRTU(cfg RTUConfig, log *FrameLog) (*RTU, error) {
	mode := &serial.Mode{BaudRate: cfg.Baud, DataBits: 8}
	switch cfg.Parity {
	case 'N':
		mode.Parity = serial.NoParity
	case 'E':
		mode.Parity = serial.EvenParity
	case 'O':
		mode.Parity = serial.OddParity
	default:
		return nil, fmt.Errorf("invalid parity %q: use N, E or O", cfg.Parity)
	}
	switch cfg.StopBits {
	case 1:
		mode.StopBits = serial.OneStopBit
	case 2:
		mode.StopBits = serial.TwoStopBits
	default:
		return nil, fmt.Errorf("invalid stop bits %d: use 1 or 2", cfg.StopBits)
	}
	if cfg.procRoot == "" {
		cfg.procRoot = defaultProcRoot
	}
	if cfg.sysRoot == "" {
		cfg.sysRoot = defaultSysRoot
	}
	if cfg.lockDirs == nil {
		cfg.lockDirs = defaultLockDirs
	}

	lock, err := lockPort(cfg.Port, cfg.Force, cfg.procRoot, cfg.sysRoot, cfg.lockDirs)
	if err != nil {
		return nil, err
	}
	port, err := serial.Open(cfg.Port, mode)
	if err != nil {
		lock.release()
		var pe *serial.PortError
		if errors.As(err, &pe) && pe.Code() == serial.PortBusy {
			pid, name, _ := findPortUser(cfg.Port, cfg.procRoot)
			return nil, &PortBusyError{Port: cfg.Port, PID: pid, Process: name, Forced: cfg.Force}
		}
		return nil, &PortOpenError{Port: cfg.Port, Err: err}
	}
	return &RTU{cfg: cfg, port: port, lock: lock, log: log, silent: silentInterval(cfg)}, nil
}

// silentInterval is 3.5 character times, fixed at 1.75 ms above 19200 baud.
func silentInterval(cfg RTUConfig) time.Duration {
	if cfg.Baud > 19200 {
		return 1750 * time.Microsecond
	}
	bits := 1 + 8 + cfg.StopBits
	if cfg.Parity != 'N' {
		bits++
	}
	return time.Duration(float64(time.Second) * 3.5 * float64(bits) / float64(cfg.Baud))
}

// Do implements Transport.
func (r *RTU) Do(ctx context.Context, req codec.ReadRequest) (codec.ReadResponse, error) {
	if err := ctx.Err(); err != nil {
		return codec.ReadResponse{}, err
	}
	frame, err := codec.EncodeRTU(req)
	if err != nil {
		return codec.ReadResponse{}, err
	}
	if r.unsettled {
		if err := r.settle(ctx); err != nil {
			return codec.ReadResponse{}, err
		}
	}
	if wait := r.silent - time.Since(r.lastIO); wait > 0 {
		time.Sleep(wait)
	}
	_ = r.port.ResetInputBuffer()

	r.log.Add(Frame{At: time.Now(), Dir: TX, Raw: frame, Note: req.String()})
	if _, err := r.port.Write(frame); err != nil {
		return codec.ReadResponse{}, r.portFailed("write to", err)
	}

	raw, err := r.readResponse(ctx, req, frame)
	r.lastIO = time.Now()
	if err != nil {
		r.unsettled = true
		return codec.ReadResponse{}, err
	}
	resp, err := codec.DecodeRTU(raw, req)
	note := okNote(req, req.DataLen())
	if err != nil {
		note = err.Error()
	}
	r.log.Add(Frame{At: time.Now(), Dir: RX, Raw: raw, Note: note})
	// After a bad frame the line may still carry stray bytes. An exception
	// is a clean, complete answer.
	var exc *codec.ExceptionError
	r.unsettled = err != nil && !errors.As(err, &exc)
	return resp, err
}

// maxDrained bounds what settle keeps for the frame log.
const maxDrained = 512

// settle runs before a request that follows a failed one. RTU has no
// transaction ID: a late answer to the previous request, arriving after
// its timeout, would otherwise be read as the answer to this request (the
// CRC and byte count cannot tell them apart when both requests have the
// same size). It reads and discards for up to one timeout, stopping early
// once bytes have arrived and the line has been quiet for readSlice.
// Answers later than that are not caught; a slow device needs a longer -t.
func (r *RTU) settle(ctx context.Context) error {
	deadline := time.Now().Add(r.cfg.Timeout)
	var drained []byte
	var last time.Time
	tmp := make([]byte, 256)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := time.Now()
		if !now.Before(deadline) || (!last.IsZero() && now.Sub(last) >= readSlice) {
			break
		}
		_ = r.port.SetReadTimeout(min(deadline.Sub(now), readSlice))
		n, err := r.port.Read(tmp)
		if err != nil {
			return r.portFailed("read from", err)
		}
		if n > 0 {
			last = time.Now()
			if len(drained) < maxDrained {
				drained = append(drained, tmp[:min(n, maxDrained-len(drained))]...)
			}
		}
	}
	if !last.IsZero() {
		r.log.Add(Frame{At: time.Now(), Dir: RX, Raw: drained, Note: "late response · discarded"})
	}
	r.unsettled = false
	r.lastIO = time.Now()
	return nil
}

// readResponse reads until the expected response size or the timeout.
// It does not rely on inter-character silence, which Linux scheduling and
// USB adapters make unreliable. It never reads past the expected frame,
// except while the bytes received equal the request, to recognize an echo.
func (r *RTU) readResponse(ctx context.Context, req codec.ReadRequest, sent []byte) ([]byte, error) {
	deadline := time.Now().Add(r.cfg.Timeout)
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	for {
		if len(buf) >= len(sent) && bytes.Equal(buf[:len(sent)], sent) {
			e := &EchoError{}
			r.log.Add(Frame{At: time.Now(), Dir: RX, Raw: buf, Note: "request echo"})
			return nil, e
		}
		need := codec.RTUExpectedLen(req)
		if len(buf) >= 2 && codec.IsRTUException(buf[1]) {
			need = codec.RTUExceptionLen
		}
		if len(buf) >= need && len(buf) < len(sent) && bytes.HasPrefix(sent, buf) {
			// A whole frame identical to the start of the request may be an
			// echo of it: read the rest of the request's length to tell.
			// Never read past the expected frame otherwise, or the start of
			// the next frame would be consumed.
			need = len(sent)
		}
		if len(buf) >= need {
			return buf, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			te := &TimeoutError{After: r.cfg.Timeout}
			note := te.Error()
			if len(buf) > 0 {
				note = fmt.Sprintf("%s · %d of %d bytes", note, len(buf), need)
			}
			r.log.Add(Frame{At: time.Now(), Dir: RX, Raw: buf, Note: note})
			return nil, te
		}
		_ = r.port.SetReadTimeout(min(remaining, readSlice))
		n, err := r.port.Read(tmp[:need-len(buf)])
		if err != nil {
			return nil, r.portFailed("read from", err)
		}
		buf = append(buf, tmp[:n]...)
	}
}

func (r *RTU) portFailed(op string, err error) error {
	ce := &ConnError{Msg: fmt.Sprintf("failed to %s %s: %v. Check that the adapter is still connected.", op, r.cfg.Port, err), Err: err}
	r.log.Add(Frame{At: time.Now(), Dir: RX, Note: ce.Msg})
	return ce
}

// Close closes the port and releases the lock.
func (r *RTU) Close() error {
	err := r.port.Close()
	r.lock.release()
	return err
}
