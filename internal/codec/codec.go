// Package codec builds Modbus read requests (FC01–FC04) and parses their
// responses, in the TCP (MBAP) and RTU framings. It performs no I/O.
//
// modtop is read-only by absence: this package implements no write function.
package codec

import (
	"encoding/binary"
	"fmt"
)

// Read function codes. These are the only functions modtop implements.
const (
	ReadCoils            byte = 1
	ReadDiscreteInputs   byte = 2
	ReadHoldingRegisters byte = 3
	ReadInputRegisters   byte = 4
)

// exceptionFlag is set in the function byte of an exception response.
const exceptionFlag = 0x80

// Quantity limits per request.
const (
	MaxBits      = 2000
	MaxRegisters = 125
)

// IsBitFunction reports whether fc reads bits (FC01, FC02).
func IsBitFunction(fc byte) bool {
	return fc == ReadCoils || fc == ReadDiscreteInputs
}

// ReadRequest is one read of Quantity consecutive items starting at Address.
type ReadRequest struct {
	Unit     byte // unit ID (TCP) or slave address (RTU)
	Function byte // ReadCoils .. ReadInputRegisters
	Address  uint16
	Quantity uint16
}

// String returns a short description used in the frames panel,
// e.g. "FC03 · PDU 2 · qty 2".
func (r ReadRequest) String() string {
	return fmt.Sprintf("FC%02d · PDU %d · qty %d", r.Function, r.Address, r.Quantity)
}

// Validate checks the function code and the quantity limits.
func (r ReadRequest) Validate() error {
	var maxQty uint16
	switch r.Function {
	case ReadCoils, ReadDiscreteInputs:
		maxQty = MaxBits
	case ReadHoldingRegisters, ReadInputRegisters:
		maxQty = MaxRegisters
	default:
		return fmt.Errorf("codec: function %d not supported (read-only: FC01–FC04)", r.Function)
	}
	if r.Quantity < 1 || r.Quantity > maxQty {
		return fmt.Errorf("codec: quantity %d outside 1–%d for FC%02d", r.Quantity, maxQty, r.Function)
	}
	if int(r.Address)+int(r.Quantity) > 65536 {
		return fmt.Errorf("codec: reading %d items from %d goes past address 65535", r.Quantity, r.Address)
	}
	return nil
}

// DataLen is the byte count of the data in a normal response to r.
func (r ReadRequest) DataLen() int {
	if IsBitFunction(r.Function) {
		return (int(r.Quantity) + 7) / 8
	}
	return 2 * int(r.Quantity)
}

// ReadResponse holds the values read, in increasing address order.
// For registers each value is the 16-bit register; for bits it is 0 or 1.
type ReadResponse struct {
	Values []uint16
}

// EncodePDU builds the request PDU: function, start address, quantity.
func EncodePDU(r ReadRequest) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	pdu := make([]byte, 5)
	pdu[0] = r.Function
	binary.BigEndian.PutUint16(pdu[1:], r.Address)
	binary.BigEndian.PutUint16(pdu[3:], r.Quantity)
	return pdu, nil
}

// decodePDU parses a response PDU for request r.
func decodePDU(pdu []byte, r ReadRequest) (ReadResponse, error) {
	if len(pdu) < 2 {
		return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("PDU of %d bytes", len(pdu))}
	}
	switch pdu[0] {
	case r.Function | exceptionFlag:
		if len(pdu) != 2 {
			return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("exception with %d PDU bytes (expected 2)", len(pdu))}
		}
		return ReadResponse{}, &ExceptionError{Function: r.Function, Code: ExceptionCode(pdu[1])}
	case r.Function:
	default:
		return ReadResponse{}, &MismatchError{Field: FieldFunction, Want: int(r.Function), Got: int(pdu[0])}
	}
	want := r.DataLen()
	if int(pdu[1]) != want {
		return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("byte count %d (expected %d)", pdu[1], want)}
	}
	if len(pdu) != 2+want {
		return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("%d data bytes (expected %d)", len(pdu)-2, want)}
	}
	data := pdu[2:]
	values := make([]uint16, r.Quantity)
	if IsBitFunction(r.Function) {
		for i := range values {
			values[i] = uint16(data[i/8]>>(i%8)) & 1
		}
	} else {
		for i := range values {
			values[i] = binary.BigEndian.Uint16(data[2*i:])
		}
	}
	return ReadResponse{Values: values}, nil
}
