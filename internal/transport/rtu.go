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
	lockDirs []string // tests only
}

// EchoError means the adapter echoes what is transmitted.
type EchoError struct{}

func (e *EchoError) Error() string {
	return "O adaptador RS-485 está devolvendo o eco do que foi transmitido.\n" +
		"Esse adaptador não é suportado na v0.1. Verifique se ele tem modo de eco desativável."
}

// readSlice bounds each blocking read so cancellation is noticed quickly.
const readSlice = 50 * time.Millisecond

// RTU is a Modbus RTU transport over a local serial port.
type RTU struct {
	cfg    RTUConfig
	port   serial.Port
	lock   *portLock
	log    *FrameLog
	silent time.Duration // 3.5 character times
	lastIO time.Time
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
		return nil, fmt.Errorf("paridade %q inválida: use N, E ou O", cfg.Parity)
	}
	switch cfg.StopBits {
	case 1:
		mode.StopBits = serial.OneStopBit
	case 2:
		mode.StopBits = serial.TwoStopBits
	default:
		return nil, fmt.Errorf("stop bits %d inválido: use 1 ou 2", cfg.StopBits)
	}
	if cfg.procRoot == "" {
		cfg.procRoot = defaultProcRoot
	}
	if cfg.lockDirs == nil {
		cfg.lockDirs = defaultLockDirs
	}

	lock, err := lockPort(cfg.Port, cfg.Force, cfg.procRoot, cfg.lockDirs)
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
	if wait := r.silent - time.Since(r.lastIO); wait > 0 {
		time.Sleep(wait)
	}
	_ = r.port.ResetInputBuffer()

	r.log.Add(Frame{At: time.Now(), Dir: TX, Raw: frame, Note: req.String()})
	if _, err := r.port.Write(frame); err != nil {
		return codec.ReadResponse{}, r.portFailed("escrever em", err)
	}

	raw, err := r.readResponse(ctx, req, frame)
	r.lastIO = time.Now()
	if err != nil {
		return codec.ReadResponse{}, err
	}
	resp, err := codec.DecodeRTU(raw, req)
	note := okNote(req, req.DataLen())
	if err != nil {
		note = err.Error()
	}
	r.log.Add(Frame{At: time.Now(), Dir: RX, Raw: raw, Note: note})
	return resp, err
}

// readResponse reads until the expected response size or the timeout.
// It does not rely on inter-character silence, which Linux scheduling and
// USB adapters make unreliable.
func (r *RTU) readResponse(ctx context.Context, req codec.ReadRequest, sent []byte) ([]byte, error) {
	deadline := time.Now().Add(r.cfg.Timeout)
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	for {
		if len(buf) >= len(sent) && bytes.Equal(buf[:len(sent)], sent) {
			e := &EchoError{}
			r.log.Add(Frame{At: time.Now(), Dir: RX, Raw: buf, Note: "eco do pedido"})
			return nil, e
		}
		need := codec.RTUExpectedLen(req)
		if len(buf) >= 2 && codec.IsRTUException(buf[1]) {
			need = codec.RTUExceptionLen
		}
		if bytes.HasPrefix(sent, buf) {
			// Could still be an echo of the request: read enough to tell.
			need = max(need, len(sent))
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
				note = fmt.Sprintf("%s · %d de %d bytes", note, len(buf), need)
			}
			r.log.Add(Frame{At: time.Now(), Dir: RX, Raw: buf, Note: note})
			return nil, te
		}
		_ = r.port.SetReadTimeout(min(remaining, readSlice))
		n, err := r.port.Read(tmp[:need-len(buf)])
		if err != nil {
			return nil, r.portFailed("ler de", err)
		}
		buf = append(buf, tmp[:n]...)
	}
}

func (r *RTU) portFailed(op string, err error) error {
	ce := &ConnError{Msg: fmt.Sprintf("falha ao %s %s: %v. Verifique se o adaptador continua conectado.", op, r.cfg.Port, err), Err: err}
	r.log.Add(Frame{At: time.Now(), Dir: RX, Note: ce.Msg})
	return ce
}

// Close closes the port and releases the lock.
func (r *RTU) Close() error {
	err := r.port.Close()
	r.lock.release()
	return err
}
