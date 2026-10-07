package codec

import "testing"

func FuzzDecodeTCP(f *testing.F) {
	f.Add([]byte{0, 1, 0, 0, 0, 7, 1, 3, 4, 0x80, 0, 0x44, 0xA2}, byte(3), uint16(2))
	f.Add([]byte{0, 1, 0, 0, 0, 3, 1, 0x83, 2}, byte(3), uint16(1))
	f.Add([]byte{0, 1, 0, 0, 0, 4, 1, 1, 1, 5}, byte(1), uint16(3))
	f.Fuzz(func(t *testing.T, frame []byte, fc byte, qty uint16) {
		r := ReadRequest{Unit: 1, Function: fc, Quantity: qty}
		resp, err := DecodeTCP(frame, r, 1)
		if err == nil && len(resp.Values) != int(qty) {
			t.Fatalf("got %d values for quantity %d", len(resp.Values), qty)
		}
	})
}

func FuzzDecodeRTU(f *testing.F) {
	f.Add([]byte{1, 3, 4, 0x80, 0, 0x44, 0xA2, 0x61, 0x4A}, byte(3), uint16(2))
	f.Add([]byte{1, 0x83, 2, 0xC0, 0xF1}, byte(3), uint16(1))
	f.Add([]byte{1, 1, 2, 5, 1, 0x7B, 0x6C}, byte(1), uint16(10))
	f.Fuzz(func(t *testing.T, frame []byte, fc byte, qty uint16) {
		r := ReadRequest{Unit: 1, Function: fc, Quantity: qty}
		resp, err := DecodeRTU(frame, r)
		if err == nil && len(resp.Values) != int(qty) {
			t.Fatalf("got %d values for quantity %d", len(resp.Values), qty)
		}
	})
}
