// Package decode interprets raw Modbus registers as typed values and
// formats them for display.
package decode

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// DataType is how one or two registers are interpreted.
type DataType uint8

// Supported data types. Bool is only used for bit tables.
const (
	Uint16 DataType = iota
	Int16
	Uint32
	Int32
	Float32
	Bool
)

// String returns the type name shown in the UI.
func (t DataType) String() string {
	switch t {
	case Uint16:
		return "uint16"
	case Int16:
		return "int16"
	case Uint32:
		return "uint32"
	case Int32:
		return "int32"
	case Float32:
		return "float32"
	case Bool:
		return "bool"
	}
	return fmt.Sprintf("DataType(%d)", uint8(t))
}

// Width returns how many registers the type occupies (1 or 2).
func (t DataType) Width() int {
	if t.Is32() {
		return 2
	}
	return 1
}

// Is32 reports whether the type spans two registers.
func (t DataType) Is32() bool {
	return t == Uint32 || t == Int32 || t == Float32
}

// Next returns the next register type in the UI cycle:
// uint16 → int16 → uint32 → int32 → float32 → uint16.
func (t DataType) Next() DataType {
	if t >= Float32 {
		return Uint16
	}
	return t + 1
}

// WordOrder is how the four bytes of a 32-bit value are assembled from
// R0 = A B and R1 = C D before being read as big-endian.
type WordOrder uint8

// The four byte orders.
const (
	ABCD WordOrder = iota
	CDAB
	BADC
	DCBA
)

// Orders lists all byte orders in display order.
var Orders = [4]WordOrder{ABCD, CDAB, BADC, DCBA}

// String returns the order name.
func (o WordOrder) String() string {
	switch o {
	case ABCD:
		return "ABCD"
	case CDAB:
		return "CDAB"
	case BADC:
		return "BADC"
	case DCBA:
		return "DCBA"
	}
	return fmt.Sprintf("WordOrder(%d)", uint8(o))
}

// Next returns the next order in the UI cycle: ABCD → CDAB → BADC → DCBA.
func (o WordOrder) Next() WordOrder {
	return (o + 1) % 4
}

// ParseWordOrder parses an order name such as "CDAB" (case-insensitive).
func ParseWordOrder(s string) (WordOrder, bool) {
	for _, o := range Orders {
		if strings.EqualFold(s, o.String()) {
			return o, true
		}
	}
	return 0, false
}

// Decode16 interprets one register as Uint16 or Int16.
func Decode16(r uint16, t DataType) (float64, error) {
	switch t {
	case Uint16:
		return float64(r), nil
	case Int16:
		return float64(int16(r)), nil
	}
	return 0, fmt.Errorf("decode: %v is not a 16-bit type", t)
}

// Decode32 interprets two registers (R0, R1) as Uint32, Int32 or Float32
// using the given byte order.
func Decode32(r0, r1 uint16, t DataType, o WordOrder) (float64, error) {
	if !t.Is32() {
		return 0, fmt.Errorf("decode: %v is not a 32-bit type", t)
	}
	u := assemble(r0, r1, o)
	switch t {
	case Uint32:
		return float64(u), nil
	case Int32:
		return float64(int32(u)), nil
	default:
		return float64(math.Float32frombits(u)), nil
	}
}

// AllOrders returns the pair interpreted in the four orders, in the order
// ABCD, CDAB, BADC, DCBA. For a type that is not 32-bit it returns NaNs.
func AllOrders(r0, r1 uint16, t DataType) [4]float64 {
	var out [4]float64
	for i, o := range Orders {
		v, err := Decode32(r0, r1, t, o)
		if err != nil {
			v = math.NaN()
		}
		out[i] = v
	}
	return out
}

func assemble(r0, r1 uint16, o WordOrder) uint32 {
	a, b := byte(r0>>8), byte(r0)
	c, d := byte(r1>>8), byte(r1)
	var buf [4]byte
	switch o {
	case CDAB:
		buf = [4]byte{c, d, a, b}
	case BADC:
		buf = [4]byte{b, a, d, c}
	case DCBA:
		buf = [4]byte{d, c, b, a}
	default:
		buf = [4]byte{a, b, c, d}
	}
	return binary.BigEndian.Uint32(buf[:])
}

// Format renders a decoded value for the UI.
//
// Integers are plain decimal and bools are 1/0. Float32 values use up to
// six significant digits with at least one decimal place in fixed notation
// (50.0, 1300.0, 27.31), and scientific notation when |v| >= 1e7 or
// 0 < |v| < 1e-3 (-2.46208e-41). NaN and infinities are printed literally.
func Format(v float64, t DataType) string {
	switch t {
	case Bool:
		if v != 0 {
			return "1"
		}
		return "0"
	case Float32:
		return formatFloat(v)
	}
	return strconv.FormatFloat(v, 'f', 0, 64)
}

func formatFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case v == 0:
		if math.Signbit(v) {
			return "-0.0"
		}
		return "0.0"
	}
	abs := math.Abs(v)
	if abs >= 1e7 || abs < 1e-3 {
		s := strconv.FormatFloat(v, 'e', 5, 32)
		mant, exp, _ := strings.Cut(s, "e")
		mant = strings.TrimRight(mant, "0")
		mant = strings.TrimSuffix(mant, ".")
		return mant + "e" + exp
	}
	// Six significant digits. In [1e6, 1e7) that leaves no room for a
	// decimal place, so the integer part is kept whole and ".0" added.
	decimals := 5 - int(math.Floor(math.Log10(abs)))
	if decimals < 1 {
		decimals = 1
	}
	s := strconv.FormatFloat(v, 'f', decimals, 32)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	return s
}
