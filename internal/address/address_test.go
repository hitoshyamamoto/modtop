package address

import (
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

func tablePtr(t Table) *Table { return &t }

func TestParseVectors(t *testing.T) {
	type want struct {
		addr      Addr
		err       bool
		ambiguous bool
	}
	tests := []struct {
		in    string
		conv  Convention
		table *Table
		want  want
	}{
		{"40001", Modicon, nil, want{addr: Addr{HoldingRegister, 0}}},
		{"40003", Modicon, nil, want{addr: Addr{HoldingRegister, 2}}},
		{"30010", Modicon, nil, want{addr: Addr{InputRegister, 9}}},
		{"00001", Modicon, nil, want{addr: Addr{Coil, 0}}},
		{"10005", Modicon, nil, want{addr: Addr{DiscreteInput, 4}}},
		{"49999", Modicon, nil, want{addr: Addr{HoldingRegister, 9998}}},
		{"400001", Modicon, nil, want{addr: Addr{HoldingRegister, 0}}},
		{"465536", Modicon, nil, want{addr: Addr{HoldingRegister, 65535}}},
		{"40000", Modicon, nil, want{err: true}},
		{"20001", Modicon, nil, want{err: true}},
		{"4001", Modicon, nil, want{err: true}},
		{"465537", Modicon, nil, want{err: true}},
		{"1", Base1, tablePtr(HoldingRegister), want{addr: Addr{HoldingRegister, 0}}},
		{"0", Base1, tablePtr(HoldingRegister), want{err: true}},
		{"65536", Base1, tablePtr(HoldingRegister), want{addr: Addr{HoldingRegister, 65535}}},
		{"0", Base0, tablePtr(HoldingRegister), want{addr: Addr{HoldingRegister, 0}}},
		{"65536", Base0, tablePtr(HoldingRegister), want{err: true}},
		{"40001", Base0, tablePtr(HoldingRegister), want{err: true, ambiguous: true}},
		{"30001", Base1, tablePtr(InputRegister), want{err: true, ambiguous: true}},
		{"9999", Base0, tablePtr(HoldingRegister), want{addr: Addr{HoldingRegister, 9999}}},
		{"0x10", Base0, tablePtr(HoldingRegister), want{err: true}},
		// Extra cases.
		{" 40001 ", Modicon, nil, want{addr: Addr{HoldingRegister, 0}}},
		{"", Modicon, nil, want{err: true}},
		{"4o001", Modicon, nil, want{err: true}},
		{"-1", Base0, tablePtr(HoldingRegister), want{err: true}},
		{"1", Base0, nil, want{err: true}},
		{"465536", Base1, tablePtr(HoldingRegister), want{err: true, ambiguous: true}},
		{"10000", Base0, tablePtr(Coil), want{addr: Addr{Coil, 10000}}},
		{"00001", Base0, tablePtr(Coil), want{addr: Addr{Coil, 1}}},
		{"99999999999999999999", Base0, tablePtr(Coil), want{err: true}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in, tt.conv, tt.table)
		if tt.want.err {
			if err == nil {
				t.Errorf("Parse(%q, %v) = %+v, want error", tt.in, tt.conv, got)
				continue
			}
			var amb *AmbiguityError
			if isAmb := errors.As(err, &amb); isAmb != tt.want.ambiguous {
				t.Errorf("Parse(%q, %v): ambiguity error = %v, want %v (err: %v)", tt.in, tt.conv, isAmb, tt.want.ambiguous, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q, %v) error: %v", tt.in, tt.conv, err)
			continue
		}
		if got != tt.want.addr {
			t.Errorf("Parse(%q, %v) = %+v, want %+v", tt.in, tt.conv, got, tt.want.addr)
		}
	}
}

func TestAmbiguityMessage(t *testing.T) {
	_, err := Parse("40001", Base0, tablePtr(HoldingRegister))
	want := "\"40001\" looks like a Modicon address, but the current convention is base 0.\n" +
		"If the manual uses Modicon numbering, use --convention modicon.\n" +
		"If it really means base 0 address 40001, write it in 6-digit Modicon form: 440002 (with --convention modicon)."
	if err == nil || err.Error() != want {
		t.Errorf("got %v\nwant %s", err, want)
	}
	// SunSpec: base 0 address 40000 is Modicon 440001, not 40001.
	_, err = Parse("40000", Base0, tablePtr(HoldingRegister))
	if err == nil || !strings.Contains(err.Error(), "6-digit Modicon form: 440001") {
		t.Errorf("SunSpec hint: %v", err)
	}
	_, err = Parse("30001", Base1, tablePtr(InputRegister))
	if err == nil || !strings.Contains(err.Error(), "the current convention is base 1.") ||
		!strings.Contains(err.Error(), "6-digit Modicon form: 330001") {
		t.Errorf("base 1 message: %v", err)
	}
	// The suggested form must parse back to the same register.
	a, err := Parse("440001", Modicon, nil)
	if err != nil || a != (Addr{HoldingRegister, 40000}) {
		t.Errorf("440001 = %+v, %v", a, err)
	}
	// No register exists beyond 65535: only the first hint is given.
	_, err = Parse("465537", Base0, tablePtr(HoldingRegister))
	if err == nil || strings.Contains(err.Error(), "6-digit") {
		t.Errorf("out-of-range hint: %v", err)
	}
}

func TestHexMessage(t *testing.T) {
	_, err := Parse("0x10", Base0, tablePtr(HoldingRegister))
	if err == nil || !strings.Contains(err.Error(), "decimal") {
		t.Errorf("hex error should explain addresses are decimal: %v", err)
	}
}

func TestFormatVectors(t *testing.T) {
	tests := []struct {
		a    Addr
		c    Convention
		want string
	}{
		{Addr{HoldingRegister, 0}, Modicon, "40001"},
		{Addr{HoldingRegister, 9998}, Modicon, "49999"},
		{Addr{HoldingRegister, 9999}, Modicon, "410000"},
		{Addr{Coil, 4}, Modicon, "00005"},
		{Addr{HoldingRegister, 2}, Base1, "3"},
		{Addr{HoldingRegister, 2}, Base0, "2"},
		{Addr{InputRegister, 65535}, Modicon, "365536"},
	}
	for _, tt := range tests {
		if got := Format(tt.a, tt.c); got != tt.want {
			t.Errorf("Format(%+v, %v) = %q, want %q", tt.a, tt.c, got, tt.want)
		}
	}
}

// TestRoundTrip checks Parse(Format(a)) == a. In base 0 and base 1, some
// formatted addresses look like Modicon ones (e.g. "40000"); for those the
// ambiguity rule must win, so the test asserts the ambiguity error instead.
func TestRoundTrip(t *testing.T) {
	pdus := []uint16{0, 1, 9998, 9999, 65534, 65535}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 1000; i++ {
		pdus = append(pdus, uint16(rng.Intn(65536)))
	}
	ambiguous := 0
	for _, table := range []Table{Coil, DiscreteInput, InputRegister, HoldingRegister} {
		for _, c := range []Convention{Modicon, Base1, Base0} {
			for _, pdu := range pdus {
				a := Addr{Table: table, PDU: pdu}
				s := Format(a, c)
				got, err := Parse(s, c, &a.Table)
				if c != Modicon && looksModicon(s) {
					var amb *AmbiguityError
					if !errors.As(err, &amb) {
						t.Fatalf("Parse(%q, %v) = %+v, %v; want ambiguity error", s, c, got, err)
					}
					ambiguous++
					continue
				}
				if err != nil || got != a {
					t.Fatalf("round trip %+v via %v (%q): got %+v, %v", a, c, s, got, err)
				}
			}
		}
	}
	if ambiguous == 0 {
		t.Fatal("expected some ambiguous cases to be exercised")
	}
}

func looksModicon(s string) bool {
	if len(s) != 5 && len(s) != 6 || !strings.ContainsRune("0134", rune(s[0])) {
		return false
	}
	v, _ := strconv.Atoi(s)
	return v >= 10001
}

func TestParseRange(t *testing.T) {
	tests := []struct {
		in        string
		conv      Convention
		table     *Table
		wantStart Addr
		wantCount int
		wantErr   string
	}{
		{in: "40001-40020", conv: Modicon, wantStart: Addr{HoldingRegister, 0}, wantCount: 20},
		{in: "40001-40001", conv: Modicon, wantStart: Addr{HoldingRegister, 0}, wantCount: 1},
		{in: "49990-410010", conv: Modicon, wantStart: Addr{HoldingRegister, 9989}, wantCount: 21},
		{in: "0-249", conv: Base0, table: tablePtr(Coil), wantStart: Addr{Coil, 0}, wantCount: 250},
		{in: " 1-10 ", conv: Base1, table: tablePtr(InputRegister), wantStart: Addr{InputRegister, 0}, wantCount: 10},
		{in: "0-250", conv: Base0, table: tablePtr(Coil), wantErr: "the maximum is 250"},
		{in: "30001-40010", conv: Modicon, wantErr: "same table"},
		{in: "40020-40001", conv: Modicon, wantErr: "greater than the end"},
		{in: "40001", conv: Modicon, wantErr: "START-END"},
		{in: "40001-40002-40003", conv: Modicon, wantErr: "single hyphen"},
		{in: "40001 - 40002", conv: Modicon, wantErr: "no spaces"},
		{in: "40000-40002", conv: Modicon, wantErr: "range start"},
		{in: "40001-4", conv: Modicon, wantErr: "range end"},
		{in: "40001-40002", conv: Base0, table: tablePtr(HoldingRegister), wantErr: "looks like a Modicon address"},
	}
	for _, tt := range tests {
		start, count, err := ParseRange(tt.in, tt.conv, tt.table)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseRange(%q) error = %v, want containing %q", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRange(%q) error: %v", tt.in, err)
			continue
		}
		if start != tt.wantStart || count != tt.wantCount {
			t.Errorf("ParseRange(%q) = %+v, %d; want %+v, %d", tt.in, start, count, tt.wantStart, tt.wantCount)
		}
	}
	// Ambiguity must stay detectable through the range wrapper.
	_, _, err := ParseRange("40001-40002", Base0, tablePtr(HoldingRegister))
	var amb *AmbiguityError
	if !errors.As(err, &amb) {
		t.Errorf("ParseRange should wrap AmbiguityError, got %v", err)
	}
}

func TestTranslate(t *testing.T) {
	got := Translate(Addr{HoldingRegister, 2}, Modicon)
	want := "40003 → Holding register #3 → FC03 → on the wire: 2 (0x0002)"
	if got != want {
		t.Errorf("Translate = %q, want %q", got, want)
	}
	got = Translate(Addr{Coil, 0}, Base0)
	want = "0 → Coil #1 → FC01 → on the wire: 0 (0x0000)"
	if got != want {
		t.Errorf("Translate = %q, want %q", got, want)
	}
}

func TestTableHelpers(t *testing.T) {
	tests := []struct {
		t      Table
		fc     byte
		prefix int
		bit    bool
	}{
		{Coil, 1, 0, true},
		{DiscreteInput, 2, 1, true},
		{InputRegister, 4, 3, false},
		{HoldingRegister, 3, 4, false},
	}
	for _, tt := range tests {
		if tt.t.ReadFunction() != tt.fc || tt.t.ModiconPrefix() != tt.prefix || tt.t.IsBit() != tt.bit {
			t.Errorf("%v helpers wrong", tt.t)
		}
	}
	if Modicon.Next() != Base1 || Base1.Next() != Base0 || Base0.Next() != Modicon {
		t.Error("convention cycle wrong")
	}
}
