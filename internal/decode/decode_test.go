package decode

import (
	"math"
	"testing"
)

// sameFloat32 compares by bits after converting to float32, so NaN and
// tiny subnormals are checked exactly.
func sameFloat32(got, want float64) bool {
	return math.Float32bits(float32(got)) == math.Float32bits(float32(want))
}

func TestDecode32Vectors(t *testing.T) {
	tests := []struct {
		r0, r1 uint16
		order  WordOrder
		f32    float64
		u32    float64
		i32    float64
	}{
		{0x8000, 0x44A2, ABCD, -2.4620814e-41, 2147501218, -2147466078},
		{0x8000, 0x44A2, CDAB, 1300.0, 1151500288, 1151500288},
		{0x8000, 0x44A2, BADC, 1.1813153e-38, 8430148, 8430148},
		{0x8000, 0x44A2, DCBA, -2.6563218e-18, 2722365568, -1572601728},
		{0x4248, 0x0000, ABCD, 50.0, 1112014848, 1112014848},
		{0x4248, 0x0000, CDAB, 2.3777232e-41, 16968, 16968},
		{0x4248, 0x0000, BADC, 198656.0, 1212284928, 1212284928},
		{0x4248, 0x0000, DCBA, 2.5921219e-41, 18498, 18498},
	}
	for _, tt := range tests {
		f, err := Decode32(tt.r0, tt.r1, Float32, tt.order)
		if err != nil || !sameFloat32(f, tt.f32) {
			t.Errorf("%04X %04X %v float32 = %g, %v; want %g", tt.r0, tt.r1, tt.order, f, err, tt.f32)
		}
		u, err := Decode32(tt.r0, tt.r1, Uint32, tt.order)
		if err != nil || u != tt.u32 {
			t.Errorf("%04X %04X %v uint32 = %v, %v; want %v", tt.r0, tt.r1, tt.order, u, err, tt.u32)
		}
		i, err := Decode32(tt.r0, tt.r1, Int32, tt.order)
		if err != nil || i != tt.i32 {
			t.Errorf("%04X %04X %v int32 = %v, %v; want %v", tt.r0, tt.r1, tt.order, i, err, tt.i32)
		}
	}
}

func TestDecode32Extra(t *testing.T) {
	check := func(r0, r1 uint16, typ DataType, o WordOrder, want float64) {
		t.Helper()
		got, err := Decode32(r0, r1, typ, o)
		if err != nil || got != want {
			t.Errorf("%04X %04X %v %v = %v, %v; want %v", r0, r1, typ, o, got, err, want)
		}
	}
	check(0x0001, 0x86A0, Uint32, ABCD, 100000)
	check(0x0001, 0x86A0, Uint32, CDAB, 2258632705)
	check(0x0001, 0x86A0, Int32, CDAB, -2036334591)
	check(0xFFFF, 0xFFFE, Int32, ABCD, -2)
	check(0xFFFF, 0xFFFE, Uint32, ABCD, 4294967294)

	f, err := Decode32(0xFFFF, 0xFFFE, Float32, ABCD)
	if err != nil || !math.IsNaN(f) {
		t.Errorf("FFFF FFFE float32 = %v, %v; want NaN", f, err)
	}
	if _, err := Decode32(1, 2, Uint16, ABCD); err == nil {
		t.Error("Decode32 with a 16-bit type should fail")
	}
}

func TestDecode16(t *testing.T) {
	if v, err := Decode16(0xFFF6, Int16); err != nil || v != -10 {
		t.Errorf("int16 = %v, %v", v, err)
	}
	if v, err := Decode16(0xFFF6, Uint16); err != nil || v != 65526 {
		t.Errorf("uint16 = %v, %v", v, err)
	}
	if _, err := Decode16(1, Float32); err == nil {
		t.Error("Decode16 with a 32-bit type should fail")
	}
}

func TestAllOrders(t *testing.T) {
	got := AllOrders(0x8000, 0x44A2, Float32)
	want := [4]float64{-2.4620814e-41, 1300.0, 1.1813153e-38, -2.6563218e-18}
	for i := range want {
		if !sameFloat32(got[i], want[i]) {
			t.Errorf("AllOrders[%v] = %g, want %g", Orders[i], got[i], want[i])
		}
	}
	for _, v := range AllOrders(1, 2, Int16) {
		if !math.IsNaN(v) {
			t.Error("AllOrders with a 16-bit type should return NaN")
		}
	}
}

func TestFormat(t *testing.T) {
	f32 := func(v float32) float64 { return float64(v) }
	tests := []struct {
		v    float64
		t    DataType
		want string
	}{
		{f32(50), Float32, "50.0"},
		{f32(1300), Float32, "1300.0"},
		{f32(27.31), Float32, "27.31"},
		{f32(198656), Float32, "198656.0"},
		{f32(-2.4620814e-41), Float32, "-2.46208e-41"},
		{f32(1.1813153e-38), Float32, "1.18132e-38"},
		{f32(-2.6563218e-18), Float32, "-2.65632e-18"},
		{f32(1e7), Float32, "1e+07"},
		{f32(1234567), Float32, "1234567.0"},
		{f32(0.001), Float32, "0.001"},
		{f32(0.0009), Float32, "9e-04"},
		{f32(-0.5), Float32, "-0.5"},
		{f32(123.456789), Float32, "123.457"},
		{0, Float32, "0.0"},
		{math.NaN(), Float32, "NaN"},
		{math.Inf(1), Float32, "+Inf"},
		{math.Inf(-1), Float32, "-Inf"},
		{65526, Uint16, "65526"},
		{-10, Int16, "-10"},
		{4294967294, Uint32, "4294967294"},
		{-2147466078, Int32, "-2147466078"},
		{1, Bool, "1"},
		{0, Bool, "0"},
	}
	for _, tt := range tests {
		if got := Format(tt.v, tt.t); got != tt.want {
			t.Errorf("Format(%v, %v) = %q, want %q", tt.v, tt.t, got, tt.want)
		}
	}
}

func TestCycles(t *testing.T) {
	seq := []DataType{Uint16, Int16, Uint32, Int32, Float32, Uint16}
	for i := 0; i < len(seq)-1; i++ {
		if seq[i].Next() != seq[i+1] {
			t.Errorf("%v.Next() = %v, want %v", seq[i], seq[i].Next(), seq[i+1])
		}
	}
	if DCBA.Next() != ABCD || ABCD.Next() != CDAB {
		t.Error("order cycle wrong")
	}
	if Float32.Width() != 2 || Uint16.Width() != 1 || Bool.Width() != 1 {
		t.Error("width wrong")
	}
	if o, ok := ParseWordOrder("cdab"); !ok || o != CDAB {
		t.Error("ParseWordOrder(cdab) failed")
	}
	if _, ok := ParseWordOrder("ACBD"); ok {
		t.Error("ParseWordOrder(ACBD) should fail")
	}
}
