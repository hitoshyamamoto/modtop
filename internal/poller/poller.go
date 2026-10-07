// Package poller runs the read cycle over the configured range, keeps the
// state of every address and the connection health, and hands results to
// the UI without ever blocking it.
package poller

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/codec"
	"github.com/hitoshyamamoto/modtop/internal/transport"
)

// The frame log is owned by the transport; these aliases let the UI read
// it without importing the transport package.
type (
	FrameLog  = transport.FrameLog
	Frame     = transport.Frame
	Direction = transport.Direction
)

// Frame directions.
const (
	TX = transport.TX
	RX = transport.RX
)

// healthWindow is how many recent cycles the health is computed over.
const healthWindow = 10

// downAfter is how many consecutive cycles without any success mean Down.
const downAfter = 3

// Config describes what to poll.
type Config struct {
	Start    address.Addr
	Count    int
	Unit     byte
	Interval time.Duration
	Single   bool // read one address per request from the start
}

// Cell is the state of one address.
type Cell struct {
	Raw     uint16    // register value; 0 or 1 for bits
	OK      bool      // the LAST read attempt succeeded
	LastOK  time.Time // time of the last successful read (zero if never)
	Err     error     // error of the last attempt, if any
	Missing bool      // exception 02 on an individual read: nonexistent address
}

// Stats are cumulative request counters for the session.
type Stats struct {
	OK, Timeouts, CRCErrors, Malformed uint64
	Exceptions                         map[byte]uint64
	Other                              uint64 // connection, mismatch and echo errors
}

func (s Stats) clone() Stats {
	c := s
	c.Exceptions = make(map[byte]uint64, len(s.Exceptions))
	for k, v := range s.Exceptions {
		c.Exceptions[k] = v
	}
	return c
}

// ExceptionTotal returns the number of exception responses.
func (s Stats) ExceptionTotal() uint64 {
	var n uint64
	for _, v := range s.Exceptions {
		n += v
	}
	return n
}

// Health summarizes the recent cycles.
type Health uint8

// Health states.
const (
	Connecting Health = iota // no cycle completed yet
	OK                       // no failure in the recent cycles
	Degraded                 // some failures, but not Down
	Down                     // the last cycles had no success at all
)

// CycleResult is sent to the UI at the end of every cycle.
type CycleResult struct {
	Cells    []Cell // one per address of the range, in order
	Started  time.Time
	Duration time.Duration
	Stats    Stats
	Health   Health
}

type block struct {
	offset, count int
	individual    bool
}

type outcome struct {
	failure, success bool
}

// Poller reads the range in cycles.
type Poller struct {
	tr      transport.Transport
	cfg     Config
	fc      byte
	results chan CycleResult
	paused  atomic.Bool
	wake    chan struct{}

	// Owned by the Run goroutine.
	cells   []Cell
	blocks  []block
	stats   Stats
	history []outcome
}

// New returns a poller for cfg over tr.
func New(tr transport.Transport, cfg Config) *Poller {
	p := &Poller{
		tr:      tr,
		cfg:     cfg,
		fc:      cfg.Start.Table.ReadFunction(),
		results: make(chan CycleResult, 1),
		wake:    make(chan struct{}, 1),
		cells:   make([]Cell, cfg.Count),
		stats:   Stats{Exceptions: map[byte]uint64{}},
	}
	size := codec.MaxRegisters
	if cfg.Start.Table.IsBit() {
		size = codec.MaxBits
	}
	for off := 0; off < cfg.Count; off += size {
		p.blocks = append(p.blocks, block{offset: off, count: min(size, cfg.Count-off), individual: cfg.Single})
	}
	return p
}

// Results delivers one CycleResult per cycle. Only the most recent result
// is kept: a slow reader never blocks the poller.
func (p *Poller) Results() <-chan CycleResult { return p.results }

// SetPaused stops (or resumes) starting new cycles. A cycle in progress
// always finishes.
func (p *Poller) SetPaused(paused bool) {
	p.paused.Store(paused)
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Paused reports whether polling is paused.
func (p *Poller) Paused() bool { return p.paused.Load() }

// Run polls until ctx is cancelled. The next cycle starts Interval after
// the start of the previous one, or right after it ends if it took longer;
// cycles never overlap.
func (p *Poller) Run(ctx context.Context) {
	next := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		if p.paused.Load() {
			select {
			case <-ctx.Done():
				return
			case <-p.wake:
				continue
			}
		}
		timer.Reset(time.Until(next))
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			continue
		case <-timer.C:
		}
		if p.paused.Load() {
			continue
		}
		start := time.Now()
		res, ok := p.cycle(ctx, start)
		if !ok {
			return
		}
		p.publish(res)
		next = start.Add(p.cfg.Interval)
	}
}

func (p *Poller) publish(res CycleResult) {
	for {
		select {
		case p.results <- res:
			return
		default:
		}
		select {
		case <-p.results: // drop the stale result
		default:
		}
	}
}

// cycle reads every block once. It returns false if ctx was cancelled.
func (p *Poller) cycle(ctx context.Context, start time.Time) (CycleResult, bool) {
	var oc outcome
	requests := 0
	for i := range p.blocks {
		b := &p.blocks[i]
		if !b.individual {
			requests++
			err := p.readBlock(ctx, b, &oc)
			if ctx.Err() != nil {
				return CycleResult{}, false
			}
			if !isIllegalAddress(err) {
				continue
			}
			// The device refuses the block: read it one address at a time
			// for the rest of the session.
			b.individual = true
		}
		for off := b.offset; off < b.offset+b.count; off++ {
			if p.cells[off].Missing {
				continue
			}
			requests++
			p.readOne(ctx, off, &oc)
			if ctx.Err() != nil {
				return CycleResult{}, false
			}
		}
	}
	if requests == 0 {
		oc.success = true // every address is known to be missing
	}
	p.history = append(p.history, oc)
	if len(p.history) > healthWindow {
		p.history = p.history[1:]
	}
	return CycleResult{
		Cells:    append([]Cell(nil), p.cells...),
		Started:  start,
		Duration: time.Since(start),
		Stats:    p.stats.clone(),
		Health:   computeHealth(p.history),
	}, true
}

func (p *Poller) request(offset, count int) codec.ReadRequest {
	return codec.ReadRequest{
		Unit:     p.cfg.Unit,
		Function: p.fc,
		Address:  p.cfg.Start.PDU + uint16(offset),
		Quantity: uint16(count),
	}
}

func (p *Poller) readBlock(ctx context.Context, b *block, oc *outcome) error {
	resp, err := p.tr.Do(ctx, p.request(b.offset, b.count))
	if ctx.Err() != nil {
		return err
	}
	p.count(err)
	now := time.Now()
	if err == nil {
		oc.success = true
		for i, v := range resp.Values {
			p.cells[b.offset+i] = Cell{Raw: v, OK: true, LastOK: now}
		}
		return nil
	}
	if !isIllegalAddress(err) {
		oc.failure = true
		for i := b.offset; i < b.offset+b.count; i++ {
			p.cells[i].OK = false
			p.cells[i].Err = err
		}
	}
	return err
}

func (p *Poller) readOne(ctx context.Context, off int, oc *outcome) {
	resp, err := p.tr.Do(ctx, p.request(off, 1))
	if ctx.Err() != nil {
		return
	}
	p.count(err)
	c := &p.cells[off]
	switch {
	case err == nil:
		oc.success = true
		*c = Cell{Raw: resp.Values[0], OK: true, LastOK: time.Now()}
	case isIllegalAddress(err):
		c.OK = false
		c.Err = err
		c.Missing = true
	default:
		oc.failure = true
		c.OK = false
		c.Err = err
	}
}

func (p *Poller) count(err error) {
	var (
		exc *codec.ExceptionError
		te  *transport.TimeoutError
		crc *codec.CRCError
		mal *codec.MalformedError
	)
	switch {
	case err == nil:
		p.stats.OK++
	case errors.As(err, &exc):
		p.stats.Exceptions[byte(exc.Code)]++
	case errors.As(err, &te):
		p.stats.Timeouts++
	case errors.As(err, &crc):
		p.stats.CRCErrors++
	case errors.As(err, &mal):
		p.stats.Malformed++
	default:
		p.stats.Other++
	}
}

func isIllegalAddress(err error) bool {
	var exc *codec.ExceptionError
	return errors.As(err, &exc) && exc.Code == codec.ExcIllegalDataAddress
}

// computeHealth applies, in order: Connecting (no cycle yet), Down (the
// last 3 cycles had no success), OK (no failure in the window), Degraded.
func computeHealth(h []outcome) Health {
	if len(h) == 0 {
		return Connecting
	}
	if len(h) >= downAfter {
		down := true
		for _, o := range h[len(h)-downAfter:] {
			if o.success {
				down = false
				break
			}
		}
		if down {
			return Down
		}
	}
	for _, o := range h {
		if o.failure {
			return Degraded
		}
	}
	return OK
}

// ShortReason returns a short description of a read error for the value
// column, e.g. "timeout" or "exc 02".
func ShortReason(err error) string {
	var (
		exc  *codec.ExceptionError
		te   *transport.TimeoutError
		crc  *codec.CRCError
		mal  *codec.MalformedError
		mis  *codec.MismatchError
		echo *transport.EchoError
		conn *transport.ConnError
	)
	switch {
	case err == nil:
		return ""
	case errors.As(err, &exc):
		return fmt.Sprintf("exc %02X", byte(exc.Code))
	case errors.As(err, &te):
		return "timeout"
	case errors.As(err, &crc):
		return "CRC inválido"
	case errors.As(err, &mal):
		return "resposta malformada"
	case errors.As(err, &mis):
		return "resposta trocada"
	case errors.As(err, &echo):
		return "eco"
	case errors.As(err, &conn):
		return "sem conexão"
	}
	return "erro"
}
