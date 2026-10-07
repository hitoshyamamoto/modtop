package codec

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func h(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

func TestEncodeTCPVector(t *testing.T) {
	r := ReadRequest{Unit: 1, Function: ReadHoldingRegisters, Address: 2, Quantity: 2}
	got, err := EncodeTCP(r, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := h("00 01 00 00 00 06 01 03 00 02 00 02"); !bytes.Equal(got, want) {
		t.Errorf("EncodeTCP = % X, want % X", got, want)
	}
}

func TestDecodeTCP(t *testing.T) {
	r := ReadRequest{Unit: 1, Function: ReadHoldingRegisters, Address: 2, Quantity: 2}
	ok := h("00 01 00 00 00 07 01 03 04 80 00 44 A2")
	resp, err := DecodeTCP(ok, r, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp.Values, []uint16{0x8000, 0x44A2}) {
		t.Errorf("values = %04X", resp.Values)
	}

	_, err = DecodeTCP(h("00 01 00 00 00 03 01 83 02"), r, 1)
	var exc *ExceptionError
	if !errors.As(err, &exc) || exc.Code != ExcIllegalDataAddress {
		t.Errorf("exception: got %v", err)
	}
	if err.Error() != "exceção 02 · endereço ilegal" {
		t.Errorf("exception text = %q", err.Error())
	}

	tests := []struct {
		name  string
		frame []byte
		check func(error) bool
	}{
		{"truncated", ok[:10], isMalformed},
		{"extra bytes", append(append([]byte{}, ok...), 0), isMalformed},
		{"short header", ok[:5], isMalformed},
		{"transaction", h("00 02 00 00 00 07 01 03 04 80 00 44 A2"), isMismatch(FieldTransaction)},
		{"protocol", h("00 01 00 01 00 07 01 03 04 80 00 44 A2"), isMalformed},
		{"unit", h("00 01 00 00 00 07 02 03 04 80 00 44 A2"), isMismatch(FieldUnit)},
		{"function", h("00 01 00 00 00 07 01 04 04 80 00 44 A2"), isMismatch(FieldFunction)},
		{"byte count", h("00 01 00 00 00 07 01 03 03 80 00 44 A2"), isMalformed},
		{"long exception", h("00 01 00 00 00 04 01 83 02 00"), isMalformed},
	}
	for _, tt := range tests {
		_, err := DecodeTCP(tt.frame, r, 1)
		if !tt.check(err) {
			t.Errorf("%s: unexpected error %v", tt.name, err)
		}
	}
}

func TestTCPFrameLen(t *testing.T) {
	if n, err := TCPFrameLen(h("00 01 00 00 00 07")); err != nil || n != 13 {
		t.Errorf("TCPFrameLen = %d, %v", n, err)
	}
	for _, hdr := range [][]byte{h("00 01 00 00 00 01"), h("00 01 00 00 01 00"), h("00 01")} {
		if _, err := TCPFrameLen(hdr); !isMalformed(err) {
			t.Errorf("TCPFrameLen(% X) should be malformed, got %v", hdr, err)
		}
	}
}

func TestEncodeRTUVectors(t *testing.T) {
	tests := []struct {
		r    ReadRequest
		want string
	}{
		{ReadRequest{1, ReadHoldingRegisters, 2, 2}, "01 03 00 02 00 02 65 CB"},
		{ReadRequest{1, ReadCoils, 0, 10}, "01 01 00 00 00 0A BC 0D"},
		{ReadRequest{1, ReadInputRegisters, 9, 1}, "01 04 00 09 00 01 E1 C8"},
		{ReadRequest{0x11, ReadHoldingRegisters, 107, 3}, "11 03 00 6B 00 03 76 87"},
	}
	for _, tt := range tests {
		got, err := EncodeRTU(tt.r)
		if err != nil {
			t.Errorf("%v: %v", tt.r, err)
			continue
		}
		if want := h(tt.want); !bytes.Equal(got, want) {
			t.Errorf("EncodeRTU(%v) = % X, want % X", tt.r, got, want)
		}
	}
}

func TestDecodeRTUVectors(t *testing.T) {
	regs := ReadRequest{Unit: 1, Function: ReadHoldingRegisters, Address: 2, Quantity: 2}
	resp, err := DecodeRTU(h("01 03 04 80 00 44 A2 61 4A"), regs)
	if err != nil || !reflect.DeepEqual(resp.Values, []uint16{0x8000, 0x44A2}) {
		t.Errorf("registers: %v, %v", resp.Values, err)
	}
	if n := RTUExpectedLen(regs); n != 9 {
		t.Errorf("RTUExpectedLen = %d, want 9", n)
	}

	_, err = DecodeRTU(h("01 83 02 C0 F1"), regs)
	var exc *ExceptionError
	if !errors.As(err, &exc) || exc.Code != ExcIllegalDataAddress || exc.Function != ReadHoldingRegisters {
		t.Errorf("exception: %v", err)
	}

	coils := ReadRequest{Unit: 1, Function: ReadCoils, Address: 0, Quantity: 10}
	resp, err = DecodeRTU(h("01 01 02 05 01 7B 6C"), coils)
	want := []uint16{1, 0, 1, 0, 0, 0, 0, 0, 1, 0}
	if err != nil || !reflect.DeepEqual(resp.Values, want) {
		t.Errorf("coils: %v, %v; want %v", resp.Values, err, want)
	}
	if n := RTUExpectedLen(coils); n != 7 {
		t.Errorf("RTUExpectedLen(coils) = %d, want 7", n)
	}
}

func TestDecodeRTUErrors(t *testing.T) {
	r := ReadRequest{Unit: 1, Function: ReadHoldingRegisters, Address: 2, Quantity: 2}
	ok := h("01 03 04 80 00 44 A2 61 4A")
	tests := []struct {
		name  string
		frame []byte
		check func(error) bool
	}{
		{"truncated", ok[:4], isMalformed},
		{"truncated with valid crc", rtu("01 03 04 80 00"), isMalformed},
		// Depending on the extra bytes, the frame fails the CRC or the size check.
		{"extra bytes", append(append([]byte{}, ok...), 0), func(err error) bool { return isCRC(err) || isMalformed(err) }},
		{"extra bytes 2", append(append([]byte{}, ok...), 1, 2), func(err error) bool { return isCRC(err) || isMalformed(err) }},
		{"bad crc", h("01 03 04 80 00 44 A2 61 4B"), isCRC},
		{"slave", rtu("02 03 04 80 00 44 A2"), isMismatch(FieldSlave)},
		{"function", rtu("01 04 04 80 00 44 A2"), isMismatch(FieldFunction)},
		{"byte count", rtu("01 03 02 80 00"), isMalformed},
		{"data shorter than count", rtu("01 03 04 80 00 44"), isMalformed},
		{"exception for other function", rtu("01 84 02"), isMismatch(FieldFunction)},
	}
	for _, tt := range tests {
		_, err := DecodeRTU(tt.frame, r)
		if !tt.check(err) {
			t.Errorf("%s: unexpected error %v", tt.name, err)
		}
	}
}

func TestBitPadding(t *testing.T) {
	// Padding bits in the last byte must be ignored.
	r := ReadRequest{Unit: 1, Function: ReadDiscreteInputs, Address: 0, Quantity: 3}
	resp, err := DecodeRTU(rtu("01 02 01 FD"), r)
	if err != nil || !reflect.DeepEqual(resp.Values, []uint16{1, 0, 1}) {
		t.Errorf("got %v, %v", resp.Values, err)
	}
}

func TestQuantityLimits(t *testing.T) {
	tests := []struct {
		r  ReadRequest
		ok bool
	}{
		{ReadRequest{1, ReadCoils, 0, 1}, true},
		{ReadRequest{1, ReadCoils, 0, 2000}, true},
		{ReadRequest{1, ReadCoils, 0, 2001}, false},
		{ReadRequest{1, ReadDiscreteInputs, 0, 0}, false},
		{ReadRequest{1, ReadHoldingRegisters, 0, 125}, true},
		{ReadRequest{1, ReadHoldingRegisters, 0, 126}, false},
		{ReadRequest{1, ReadInputRegisters, 0, 0}, false},
		{ReadRequest{1, ReadInputRegisters, 65535, 1}, true},
		{ReadRequest{1, ReadInputRegisters, 65535, 2}, false},
		{ReadRequest{1, 7, 0, 1}, false},
	}
	for _, tt := range tests {
		_, errTCP := EncodeTCP(tt.r, 1)
		_, errRTU := EncodeRTU(tt.r)
		if (errTCP == nil) != tt.ok || (errRTU == nil) != tt.ok {
			t.Errorf("%+v: tcp err %v, rtu err %v, want ok=%v", tt.r, errTCP, errRTU, tt.ok)
		}
	}
}

func TestExceptionNames(t *testing.T) {
	if got := ExceptionCode(0x0B).Name(); got != "Dispositivo atrás do gateway não respondeu" {
		t.Errorf("0B name = %q", got)
	}
	if got := ExceptionCode(0x2A).Name(); got != "Exceção desconhecida (0x2A)" {
		t.Errorf("unknown name = %q", got)
	}
	if ExceptionCode(0x2A).Hint() != "" {
		t.Error("unknown code should have no hint")
	}
	for c := ExceptionCode(1); c <= 11; c++ {
		if c == 7 || c == 8 || c == 9 {
			continue
		}
		if ExceptionCode(c).Hint() == "" {
			t.Errorf("code %d has no hint", c)
		}
	}
}

func TestRequestString(t *testing.T) {
	r := ReadRequest{Unit: 1, Function: ReadHoldingRegisters, Address: 2, Quantity: 2}
	if got := r.String(); got != "FC03 · PDU 2 · qtd 2" {
		t.Errorf("String = %q", got)
	}
}

// rtu builds an RTU frame from hex with a correct CRC.
func rtu(s string) []byte { return appendCRC(h(s)) }

func isMalformed(err error) bool {
	var e *MalformedError
	return errors.As(err, &e)
}

func isCRC(err error) bool {
	var e *CRCError
	return errors.As(err, &e)
}

func isMismatch(f Field) func(error) bool {
	return func(err error) bool {
		var e *MismatchError
		return errors.As(err, &e) && e.Field == f
	}
}
