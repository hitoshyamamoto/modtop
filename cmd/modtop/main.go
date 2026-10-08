// Command modtop is an interactive terminal viewer for Modbus devices.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"

	"github.com/hitoshyamamoto/modtop/internal/address"
	"github.com/hitoshyamamoto/modtop/internal/decode"
	"github.com/hitoshyamamoto/modtop/internal/poller"
	"github.com/hitoshyamamoto/modtop/internal/transport"
	"github.com/hitoshyamamoto/modtop/internal/ui"
)

// version is set at build time with -ldflags "-X main.version=...".
// Builds without it (e.g. go install) fall back to the module version.
var version = "dev"

// buildVersion returns the version to report.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

// Exit codes (a contract: see the README).
const (
	exitOK        = 0
	exitUsage     = 1
	exitConnect   = 3
	exitPortInUse = 4
)

const usage = `Usage: modtop <target> -r <range> [options]

Targets:
  <host>[:port]         Modbus TCP (default port 502). IPv6: [::1]:502
  /dev/<port>           Modbus RTU (e.g. /dev/ttyUSB0, /dev/ttyS1)

Required:
  -r, --range RANGE     address range, e.g. 40001-40020

Options:
  -u, --unit N              unit ID: TCP 0–255, RTU slave address 1–247 (default 1)
      --convention C        modicon | base1 | base0 (default modicon)
      --table T             holding | input | coil | discrete
                            (required with base1/base0; not allowed with modicon)
      --order O             ABCD | CDAB | BADC | DCBA (default ABCD)
  -i, --interval DUR        time between cycles (default 1s, minimum 100ms)
  -t, --timeout DUR         timeout per request (default 1s)
      --single              read one register per request
      --baud N              RTU: baud rate (default 9600)
      --parity P            RTU: N | E | O (default E)
      --stopbits N          RTU: 1 | 2 (default 1)
      --force-port          RTU: open the port even if it is in use (dangerous)
  -h, --help
  -V, --version

Durations use Go syntax (500ms, 2s).
`

// config is the validated command line.
type config struct {
	target   string // TCP "host:port" or RTU device path
	rtu      bool
	unit     byte
	start    address.Addr
	count    int
	conv     address.Convention
	order    decode.WordOrder
	interval time.Duration
	timeout  time.Duration
	single   bool
	baud     int
	parity   byte
	stopBits int
	force    bool
}

// usageError is an invalid command line (exit code 1).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

var (
	windowsPort = regexp.MustCompile(`(?i)^com[0-9]+$`)
	bareTTY     = regexp.MustCompile(`^tty[A-Za-z]*[0-9]+$`)
)

// parseArgs validates the command line. It returns (nil, nil) when help
// or version was printed.
func parseArgs(args []string, stdout io.Writer) (*config, error) {
	fs := pflag.NewFlagSet("modtop", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.SortFlags = false
	var (
		rangeStr   = fs.StringP("range", "r", "", "")
		unit       = fs.IntP("unit", "u", 1, "")
		convStr    = fs.String("convention", "modicon", "")
		tableStr   = fs.String("table", "", "")
		orderStr   = fs.String("order", "ABCD", "")
		interval   = fs.DurationP("interval", "i", time.Second, "")
		timeout    = fs.DurationP("timeout", "t", time.Second, "")
		single     = fs.Bool("single", false, "")
		baud       = fs.Int("baud", 9600, "")
		parityStr  = fs.String("parity", "E", "")
		stopBits   = fs.Int("stopbits", 1, "")
		force      = fs.Bool("force-port", false, "")
		help       = fs.BoolP("help", "h", false, "")
		showVer    = fs.BoolP("version", "V", false, "")
		rtuOnly    = []string{"baud", "parity", "stopbits", "force-port"}
		cfg        config
		tableGiven bool
	)
	if err := fs.Parse(args); err != nil {
		return nil, usagef("%v", err)
	}
	if *help {
		_, _ = io.WriteString(stdout, usage)
		return nil, nil
	}
	if *showVer {
		_, _ = fmt.Fprintf(stdout, "modtop %s\n", buildVersion())
		return nil, nil
	}

	switch fs.NArg() {
	case 0:
		return nil, usagef("missing target: give a host (Modbus TCP) or a serial port /dev/... (Modbus RTU)")
	case 1:
	default:
		return nil, usagef("only one target per session; got: %s", strings.Join(fs.Args(), " "))
	}
	target := fs.Arg(0)
	if windowsPort.MatchString(target) {
		return nil, usagef("Windows is not supported in v0.1.")
	}
	if bareTTY.MatchString(target) {
		return nil, usagef("%q is not a host; for a serial port give its full path, e.g. /dev/%s", target, target)
	}
	cfg.rtu = strings.HasPrefix(target, "/")
	if cfg.rtu {
		cfg.target = target
	} else {
		for _, name := range rtuOnly {
			if fs.Changed(name) {
				return nil, usagef("--%s only applies to Modbus RTU (a /dev/... target), but %q is a TCP target", name, target)
			}
		}
		addr, err := transport.NormalizeTCPAddr(target)
		if err != nil {
			return nil, usagef("%v", err)
		}
		cfg.target = addr
	}

	switch strings.ToLower(*convStr) {
	case "modicon":
		cfg.conv = address.Modicon
	case "base1":
		cfg.conv = address.Base1
	case "base0":
		cfg.conv = address.Base0
	default:
		return nil, usagef("invalid convention %q: use modicon, base1 or base0", *convStr)
	}
	var table address.Table
	tableGiven = fs.Changed("table")
	if tableGiven {
		switch strings.ToLower(*tableStr) {
		case "holding":
			table = address.HoldingRegister
		case "input":
			table = address.InputRegister
		case "coil":
			table = address.Coil
		case "discrete":
			table = address.DiscreteInput
		default:
			return nil, usagef("invalid table %q: use holding, input, coil or discrete", *tableStr)
		}
	}
	if cfg.conv == address.Modicon && tableGiven {
		return nil, usagef("--table is not used with the modicon convention: the table comes from the first digit of the address (4xxxx = holding)")
	}
	if cfg.conv != address.Modicon && !tableGiven {
		return nil, usagef("--convention %s needs --table (holding, input, coil or discrete)", *convStr)
	}

	o, ok := decode.ParseWordOrder(*orderStr)
	if !ok {
		return nil, usagef("invalid order %q: use ABCD, CDAB, BADC or DCBA", *orderStr)
	}
	cfg.order = o

	// On TCP the unit ID may be anything up to 255: a server addressed
	// directly often expects 255 (0xFF), the value the Modbus TCP
	// implementation guide recommends; a gateway uses it to route.
	switch {
	case cfg.rtu && *unit == 0:
		return nil, usagef("broadcast (unit 0) is not allowed for reads; use the slave address (1–247)")
	case cfg.rtu && *unit > 247:
		return nil, usagef("slave address %d out of range: Modbus RTU uses 1–247", *unit)
	case *unit < 0 || *unit > 255:
		return nil, usagef("unit ID %d out of range 0–255", *unit)
	}
	cfg.unit = byte(*unit)

	if *interval < 100*time.Millisecond {
		return nil, usagef("interval %v is too short: the minimum is 100ms", *interval)
	}
	if *timeout <= 0 {
		return nil, usagef("invalid timeout %v: use a positive value, e.g. 1s", *timeout)
	}
	cfg.interval, cfg.timeout, cfg.single = *interval, *timeout, *single

	if *rangeStr == "" {
		return nil, usagef("missing range: use -r, e.g. -r 40001-40020")
	}
	var tp *address.Table
	if tableGiven {
		tp = &table
	}
	start, count, err := address.ParseRange(*rangeStr, cfg.conv, tp)
	if err != nil {
		return nil, usagef("%v", err)
	}
	cfg.start, cfg.count = start, count

	if *baud <= 0 {
		return nil, usagef("invalid baud rate %d", *baud)
	}
	switch p := strings.ToUpper(*parityStr); p {
	case "N", "E", "O":
		cfg.parity = p[0]
	default:
		return nil, usagef("invalid parity %q: use N, E or O", *parityStr)
	}
	if *stopBits != 1 && *stopBits != 2 {
		return nil, usagef("invalid stop bits %d: use 1 or 2", *stopBits)
	}
	cfg.baud, cfg.stopBits, cfg.force = *baud, *stopBits, *force
	return &cfg, nil
}

// hasTerminal reports whether modtop can run its interactive interface:
// output must go to a terminal, and input must come from one (stdin, or
// /dev/tty, which the interface opens when stdin is redirected). Tests
// replace it.
var hasTerminal = func() bool {
	if !isTerminal(os.Stdout.Fd()) {
		return false
	}
	if isTerminal(os.Stdin.Fd()) {
		return true
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = tty.Close()
	return true
}

func isTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
	return err == nil
}

// connect opens the transport and maps failures to exit codes.
func connect(cfg *config, log *transport.FrameLog) (transport.Transport, int, error) {
	if cfg.rtu {
		tr, err := transport.OpenRTU(transport.RTUConfig{
			Port: cfg.target, Baud: cfg.baud, Parity: cfg.parity, StopBits: cfg.stopBits,
			Timeout: cfg.timeout, Force: cfg.force,
		}, log)
		var busy *transport.PortBusyError
		if errors.As(err, &busy) {
			return nil, exitPortInUse, err
		}
		if err != nil {
			return nil, exitConnect, err
		}
		return tr, exitOK, nil
	}
	tr, err := transport.DialTCP(context.Background(), cfg.target, cfg.timeout, log)
	if err != nil {
		return nil, exitConnect, err
	}
	return tr, exitOK, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseArgs(args, stdout)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "modtop: %v\nRun modtop --help to see the options.\n", err)
		return exitUsage
	}
	if cfg == nil {
		return exitOK
	}
	if !hasTerminal() {
		_, _ = fmt.Fprintln(stderr, "modtop: modtop is interactive and needs a terminal; run it from a terminal (over SSH, use ssh -t).")
		return exitUsage
	}

	log := transport.NewFrameLog()
	tr, code, err := connect(cfg, log)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "modtop: %v\n", err)
		return code
	}
	defer func() { _ = tr.Close() }()

	p := poller.New(tr, poller.Config{
		Start: cfg.start, Count: cfg.count, Unit: cfg.unit, Interval: cfg.interval, Single: cfg.single,
	})
	opts := ui.Options{
		Target:     cfg.target,
		Unit:       cfg.unit,
		Start:      cfg.start,
		Count:      cfg.count,
		Convention: cfg.conv,
		Interval:   cfg.interval,
		Order:      cfg.order,
		ForcedPort: cfg.force,
		Color:      os.Getenv("NO_COLOR") == "",
	}
	if cfg.rtu {
		opts.Serial = &ui.Serial{Baud: cfg.baud, Parity: cfg.parity, StopBits: cfg.stopBits}
	}
	code, err = ui.Run(context.Background(), opts, p, log)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "modtop: %v\n", err)
	}
	return code
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
