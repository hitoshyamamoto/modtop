package main

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/decode"
)

func TestParseArgsValid(t *testing.T) {
	cfg, err := parseArgs([]string{"10.1.8.99", "-u", "3", "-r", "40001-40020", "--order", "cdab", "-i", "500ms"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.target != "10.1.8.99:502" || cfg.rtu || cfg.unit != 3 || cfg.count != 20 ||
		cfg.start != (address.Addr{Table: address.HoldingRegister}) || cfg.order != decode.CDAB {
		t.Errorf("cfg = %+v", cfg)
	}

	cfg, err = parseArgs([]string{"/dev/ttyUSB0", "--baud", "19200", "--parity", "n", "--stopbits", "2",
		"--convention", "base0", "--table", "input", "-r", "100-109", "--single"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.rtu || cfg.baud != 19200 || cfg.parity != 'N' || cfg.stopBits != 2 || !cfg.single ||
		cfg.start != (address.Addr{Table: address.InputRegister, PDU: 100}) || cfg.count != 10 {
		t.Errorf("cfg = %+v", cfg)
	}

	cfg, err = parseArgs([]string{"[::1]:1502", "-r", "00001-00010"}, &bytes.Buffer{})
	if err != nil || cfg.target != "[::1]:1502" || cfg.start.Table != address.Coil {
		t.Errorf("ipv6: %+v, %v", cfg, err)
	}
}

func TestParseArgsErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"-r", "40001-40002"}, "missing target"},
		{[]string{"a", "b", "-r", "40001-40002"}, "only one target"},
		{[]string{"COM3", "-r", "40001-40002"}, "Windows is not supported in v0.1."},
		{[]string{"10.0.0.1", "--baud", "9600", "-r", "40001-40002"}, "--baud only applies to Modbus RTU"},
		{[]string{"10.0.0.1", "--force-port", "-r", "40001-40002"}, "--force-port only applies"},
		{[]string{"10.0.0.1", "--table", "holding", "-r", "40001-40002"}, "--table is not used with the modicon convention"},
		{[]string{"10.0.0.1", "--convention", "base1", "-r", "1-2"}, "needs --table"},
		{[]string{"10.0.0.1", "--convention", "base2", "-r", "1-2"}, "invalid convention \"base2\""},
		{[]string{"10.0.0.1", "-u", "248", "-r", "40001-40002"}, "out of range 0–247"},
		{[]string{"/dev/ttyS0", "-u", "0", "-r", "40001-40002"}, "broadcast (unit 0) is not allowed for reads"},
		{[]string{"10.0.0.1", "-i", "50ms", "-r", "40001-40002"}, "the minimum is 100ms"},
		{[]string{"10.0.0.1"}, "missing range"},
		{[]string{"10.0.0.1", "-r", "40001-40300"}, "the maximum is 250"},
		{[]string{"10.0.0.1", "--convention", "base0", "--table", "holding", "-r", "40001-40002"}, "looks like a Modicon address"},
		{[]string{"10.0.0.1", "--order", "ACBD", "-r", "40001-40002"}, "invalid order \"ACBD\""},
		{[]string{"/dev/ttyS0", "--parity", "X", "-r", "40001-40002"}, "invalid parity \"X\""},
		{[]string{"/dev/ttyS0", "--stopbits", "3", "-r", "40001-40002"}, "invalid stop bits 3"},
		{[]string{"10.0.0.1", "--bogus", "-r", "40001-40002"}, "bogus"},
		{[]string{"::1", "-r", "40001-40002"}, "brackets"},
	}
	for _, tt := range tests {
		_, err := parseArgs(tt.args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: err = %v, want containing %q", tt.args, err, tt.want)
		}
	}
	// TCP with unit 0 is allowed: the target may be a TCP→RTU gateway.
	if _, err := parseArgs([]string{"10.0.0.1", "-u", "0", "-r", "40001-40002"}, &bytes.Buffer{}); err != nil {
		t.Errorf("TCP unit 0: %v", err)
	}
}

func TestRunExitCodes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "Usage: modtop") {
		t.Errorf("--help: %d %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"-V"}, &out, &errOut); code != 0 || out.String() != "modtop dev\n" {
		t.Errorf("-V: %d %q", code, out.String())
	}
	errOut.Reset()
	if code := run([]string{"10.0.0.1"}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "modtop --help") {
		t.Errorf("usage error: %d %q", code, errOut.String())
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	errOut.Reset()
	if code := run([]string{addr, "-r", "40001-40002"}, &out, &errOut); code != 3 || !strings.Contains(errOut.String(), "connection refused") {
		t.Errorf("refused: %d %q", code, errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"/dev/modtop-missing", "-r", "40001-40002"}, &out, &errOut); code != 3 || !strings.Contains(errOut.String(), "does not exist") {
		t.Errorf("missing port: %d %q", code, errOut.String())
	}
}

func TestBuildVersion(t *testing.T) {
	saved := version
	defer func() { version = saved }()
	version = "v9.9.9"
	if got := buildVersion(); got != "v9.9.9" {
		t.Errorf("ldflags version: got %q", got)
	}
	// Test binaries have no module version, so "dev" is kept.
	version = "dev"
	if got := buildVersion(); got != "dev" {
		t.Errorf("fallback: got %q", got)
	}
}
