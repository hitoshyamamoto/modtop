// Package sim is a minimal Modbus slave controlled programmatically. It is
// used only by tests and by the development server in cmd/simserver; it is
// never part of the modtop binary.
package sim

import (
	"encoding/binary"
	"sync"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/codec"
)

// Exception codes used by the simulator.
const (
	excIllegalFunction    = byte(codec.ExcIllegalFunction)
	excIllegalDataAddress = byte(codec.ExcIllegalDataAddress)
)

type key struct {
	fc   byte
	addr uint16
}

// Faults are failure modes applied to responses.
type Faults struct {
	BadCRC        bool          // RTU: corrupt the CRC
	Echo          bool          // RTU: echo the request before the response
	Fragments     int           // split the response into this many writes
	FragmentDelay time.Duration // delay between fragments
	StaleTID      bool          // TCP: send a copy with the previous transaction ID first
	CloseConn     bool          // TCP: close the connection instead of answering (one shot)
}

// Device is a simulated Modbus slave. All methods are safe for concurrent use.
type Device struct {
	mu         sync.Mutex
	unit       byte
	values     map[key]uint16
	exceptions map[key]byte
	delays     map[key]time.Duration
	silent     map[key]bool
	skip       int // requests left to ignore
	faults     Faults
	requests   int
}

// NewDevice returns a device answering to the given unit ID.
func NewDevice(unit byte) *Device {
	return &Device{
		unit:       unit,
		values:     map[key]uint16{},
		exceptions: map[key]byte{},
		delays:     map[key]time.Duration{},
		silent:     map[key]bool{},
	}
}

// Set stores a value read by function fc at addr (0/1 for bits).
func (d *Device) Set(fc byte, addr uint16, values ...uint16) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, v := range values {
		d.values[key{fc, addr + uint16(i)}] = v
	}
}

// SetException makes any read by fc that covers addr return code.
func (d *Device) SetException(fc byte, addr uint16, code byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.exceptions[key{fc, addr}] = code
}

// SetMissing marks addr as nonexistent: any read covering it returns
// exception 02, whether it is a block read or an individual one.
func (d *Device) SetMissing(fc byte, addr uint16) {
	d.SetException(fc, addr, excIllegalDataAddress)
}

// SetDelay delays the response to any read by fc that covers addr.
func (d *Device) SetDelay(fc byte, addr uint16, delay time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.delays[key{fc, addr}] = delay
}

// SetSilent makes the device not answer reads by fc that cover addr.
func (d *Device) SetSilent(fc byte, addr uint16, silent bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if silent {
		d.silent[key{fc, addr}] = true
	} else {
		delete(d.silent, key{fc, addr})
	}
}

// SkipNext makes the device ignore the next n requests.
func (d *Device) SkipNext(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.skip = n
}

// SetFaults replaces the active failure modes.
func (d *Device) SetFaults(f Faults) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.faults = f
}

// Requests returns how many requests the device has received.
func (d *Device) Requests() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.requests
}

// takeFaults returns the active faults, clearing the one-shot ones.
func (d *Device) takeFaults() Faults {
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.faults
	d.faults.CloseConn = false
	return f
}

// handle builds the response PDU for a request PDU. reply is false when
// the device must stay silent.
func (d *Device) handle(unit byte, pdu []byte) (resp []byte, delay time.Duration, reply bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requests++
	if unit != d.unit {
		return nil, 0, false
	}
	if d.skip > 0 {
		d.skip--
		return nil, 0, false
	}
	if len(pdu) != 5 {
		return nil, 0, false
	}
	fc := pdu[0]
	addr := binary.BigEndian.Uint16(pdu[1:])
	qty := binary.BigEndian.Uint16(pdu[3:])
	if fc < codec.ReadCoils || fc > codec.ReadInputRegisters {
		return []byte{fc | 0x80, excIllegalFunction}, 0, true
	}
	req := codec.ReadRequest{Unit: unit, Function: fc, Address: addr, Quantity: qty}
	if req.Validate() != nil {
		return []byte{fc | 0x80, byte(codec.ExcIllegalDataValue)}, 0, true
	}
	for i := uint16(0); i < qty; i++ {
		k := key{fc, addr + i}
		delay = max(delay, d.delays[k])
		if d.silent[k] {
			return nil, 0, false
		}
		if code, ok := d.exceptions[k]; ok {
			return []byte{fc | 0x80, code}, delay, true
		}
	}
	if codec.IsBitFunction(fc) {
		data := make([]byte, (int(qty)+7)/8)
		for i := 0; i < int(qty); i++ {
			if d.values[key{fc, addr + uint16(i)}] != 0 {
				data[i/8] |= 1 << (i % 8)
			}
		}
		return append([]byte{fc, byte(len(data))}, data...), delay, true
	}
	resp = []byte{fc, byte(2 * qty)}
	for i := uint16(0); i < qty; i++ {
		resp = binary.BigEndian.AppendUint16(resp, d.values[key{fc, addr + i}])
	}
	return resp, delay, true
}
