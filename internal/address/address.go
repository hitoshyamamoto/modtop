// Package address converts Modbus addresses between the notations found in
// device manuals (Modicon, base 1, base 0) and the canonical form used
// internally: a table plus the PDU address that goes on the wire.
package address

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Table is one of the four Modbus data tables.
type Table uint8

// The four Modbus tables.
const (
	Coil Table = iota
	DiscreteInput
	InputRegister
	HoldingRegister
)

// String returns the table name shown in the UI, as printed in device manuals.
func (t Table) String() string {
	switch t {
	case Coil:
		return "Coil"
	case DiscreteInput:
		return "Discrete input"
	case InputRegister:
		return "Input register"
	case HoldingRegister:
		return "Holding register"
	}
	return fmt.Sprintf("Table(%d)", uint8(t))
}

// ReadFunction returns the Modbus function code used to read the table.
func (t Table) ReadFunction() byte {
	switch t {
	case Coil:
		return 1
	case DiscreteInput:
		return 2
	case InputRegister:
		return 4
	case HoldingRegister:
		return 3
	}
	return 0
}

// ModiconPrefix returns the leading digit of the table in Modicon notation.
func (t Table) ModiconPrefix() int {
	switch t {
	case Coil:
		return 0
	case DiscreteInput:
		return 1
	case InputRegister:
		return 3
	case HoldingRegister:
		return 4
	}
	return -1
}

// IsBit reports whether the table holds single bits (coils and discrete inputs).
func (t Table) IsBit() bool {
	return t == Coil || t == DiscreteInput
}

// Valid reports whether t is one of the four known tables.
func (t Table) Valid() bool {
	return t <= HoldingRegister
}

func tableFromPrefix(d byte) (Table, bool) {
	switch d {
	case '0':
		return Coil, true
	case '1':
		return DiscreteInput, true
	case '3':
		return InputRegister, true
	case '4':
		return HoldingRegister, true
	}
	return 0, false
}

// Addr is the only address representation used internally.
type Addr struct {
	Table Table
	PDU   uint16 // 0..65535, the address that goes on the wire
}

// Convention is an address notation.
type Convention uint8

// Supported address notations.
const (
	Modicon Convention = iota
	Base1
	Base0
)

// String returns the convention name shown to the user.
func (c Convention) String() string {
	switch c {
	case Modicon:
		return "Modicon"
	case Base1:
		return "base 1"
	case Base0:
		return "base 0"
	}
	return fmt.Sprintf("Convention(%d)", uint8(c))
}

// Next returns the next convention in the UI cycle: Modicon → base 1 → base 0.
func (c Convention) Next() Convention {
	return (c + 1) % 3
}

// MaxRange is the largest number of addresses in a range.
const MaxRange = 250

// Format returns the address in the given notation.
// Modicon uses 5 digits when PDU+1 <= 9999 and 6 digits otherwise.
func Format(a Addr, c Convention) string {
	n := int(a.PDU) + 1
	switch c {
	case Modicon:
		if n <= 9999 {
			return fmt.Sprintf("%d%04d", a.Table.ModiconPrefix(), n)
		}
		return fmt.Sprintf("%d%05d", a.Table.ModiconPrefix(), n)
	case Base1:
		return strconv.Itoa(n)
	default:
		return strconv.Itoa(int(a.PDU))
	}
}

// AmbiguityError is returned when an address typed in base 0 or base 1
// looks like a Modicon address. It guards against the most common
// addressing mistake and must not be relaxed.
type AmbiguityError struct {
	Input      string
	Convention Convention
}

func (e *AmbiguityError) Error() string {
	return fmt.Sprintf("%q looks like a Modicon address, but the current convention is %s.\n"+
		"Use --convention modicon, or check the convention in the device manual.",
		e.Input, e.Convention)
}

// Parse interprets an address typed by the user.
//
// With Modicon the table comes from the leading digit and table is ignored.
// With Base1 and Base0 table is required.
func Parse(s string, c Convention, table *Table) (Addr, error) {
	s = strings.TrimSpace(s)
	if err := checkDigits(s); err != nil {
		return Addr{}, err
	}
	switch c {
	case Modicon:
		return parseModicon(s)
	case Base1, Base0:
		if table == nil || !table.Valid() {
			return Addr{}, errors.New("table not given: base 0 and base 1 need the table (--table holding, input, coil or discrete)")
		}
		return parsePlain(s, c, *table)
	}
	return Addr{}, fmt.Errorf("unknown convention (%d)", uint8(c))
}

func checkDigits(s string) error {
	if s == "" {
		return errors.New("empty address: enter a decimal address, e.g. 40001")
	}
	if len(s) > 1 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return fmt.Errorf("hexadecimal address %q is not accepted: addresses must be decimal, as printed in the manual", s)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return fmt.Errorf("invalid address %q: use decimal digits only", s)
		}
	}
	return nil
}

func parseModicon(s string) (Addr, error) {
	if len(s) != 5 && len(s) != 6 {
		return Addr{}, fmt.Errorf("address %q in Modicon notation must have 5 or 6 digits (e.g. 40001 or 400001)", s)
	}
	t, ok := tableFromPrefix(s[0])
	if !ok {
		return Addr{}, fmt.Errorf("address %q in Modicon notation has an invalid prefix %c: the first digit must be 0 (coil), 1 (discrete input), 3 (input register) or 4 (holding register)", s, s[0])
	}
	n, _ := strconv.Atoi(s[1:]) // digits already validated; at most 5 of them
	maxN := 9999
	if len(s) == 6 {
		maxN = 65536
	}
	if n < 1 {
		return Addr{}, fmt.Errorf("address %q in Modicon notation does not exist: numbering starts at 1 (the first one is %c%0*d)", s, s[0], len(s)-1, 1)
	}
	if n > maxN {
		return Addr{}, fmt.Errorf("address %q in Modicon notation is out of range: with %d digits the number goes from 1 to %d", s, len(s), maxN)
	}
	return Addr{Table: t, PDU: uint16(n - 1)}, nil
}

func parsePlain(s string, c Convention, t Table) (Addr, error) {
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		v = 1 << 32 // too many digits: reported as out of range below
	}
	if (len(s) == 5 || len(s) == 6) && strings.IndexByte("0134", s[0]) >= 0 && v >= 10001 {
		return Addr{}, &AmbiguityError{Input: s, Convention: c}
	}
	if c == Base1 {
		if v < 1 || v > 65536 {
			return Addr{}, fmt.Errorf("address %q out of range: base 1 goes from 1 to 65536", s)
		}
		return Addr{Table: t, PDU: uint16(v - 1)}, nil
	}
	if v > 65535 {
		return Addr{}, fmt.Errorf("address %q out of range: base 0 goes from 0 to 65535", s)
	}
	return Addr{Table: t, PDU: uint16(v)}, nil
}

// ParseRange interprets "START-END" (e.g. "40001-40020"). Both ends must be
// in the same table, with START <= END and at most MaxRange addresses.
func ParseRange(s string, c Convention, table *Table) (start Addr, count int, err error) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, " \t") {
		return Addr{}, 0, fmt.Errorf("invalid range %q: no spaces allowed; the format is START-END, e.g. 40001-40020", s)
	}
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return Addr{}, 0, fmt.Errorf("invalid range %q: use the START-END format with a single hyphen, e.g. 40001-40020", s)
	}
	start, err = Parse(parts[0], c, table)
	if err != nil {
		return Addr{}, 0, fmt.Errorf("range start: %w", err)
	}
	end, err := Parse(parts[1], c, table)
	if err != nil {
		return Addr{}, 0, fmt.Errorf("range end: %w", err)
	}
	if start.Table != end.Table {
		return Addr{}, 0, fmt.Errorf("invalid range %q: the start is a %s and the end is a %s; both ends must be in the same table", s, start.Table, end.Table)
	}
	if start.PDU > end.PDU {
		return Addr{}, 0, fmt.Errorf("invalid range %q: the start is greater than the end; swap them", s)
	}
	count = int(end.PDU) - int(start.PDU) + 1
	if count > MaxRange {
		return Addr{}, 0, fmt.Errorf("range %q has %d addresses; the maximum is %d. Split the reading into smaller ranges", s, count, MaxRange)
	}
	return start, count, nil
}

// Translate returns the translation line shown in the UI footer, e.g.
// "40003 → Holding register #3 → FC03 → on the wire: 2 (0x0002)".
func Translate(a Addr, c Convention) string {
	return fmt.Sprintf("%s → %s #%d → FC%02d → on the wire: %d (0x%04X)",
		Format(a, c), a.Table, int(a.PDU)+1, a.Table.ReadFunction(), a.PDU, a.PDU)
}
