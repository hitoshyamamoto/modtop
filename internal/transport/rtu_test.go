package transport

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/codec"
	"github.com/hitoshyamamoto/modtop/internal/sim"
)

var ptyLine = regexp.MustCompile(`PTY is (\S+)`)

// ptyPair creates two linked pseudo-terminals with socat and returns their
// paths and socat's PID. The test is skipped when socat is not installed.
func ptyPair(t *testing.T) (a, b string, pid int) {
	t.Helper()
	if _, err := exec.LookPath("socat"); err != nil {
		t.Skip("socat não encontrado: instale socat para rodar os testes RTU")
	}
	cmd := exec.Command("socat", "-d", "-d", "pty,raw,echo=0", "pty,raw,echo=0")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	var paths []string
	sc := bufio.NewScanner(stderr)
	for len(paths) < 2 && sc.Scan() {
		if m := ptyLine.FindStringSubmatch(sc.Text()); m != nil {
			paths = append(paths, m[1])
		}
	}
	if len(paths) < 2 {
		t.Fatal("socat did not report two PTYs")
	}
	go func() {
		for sc.Scan() {
		}
	}()
	return paths[0], paths[1], cmd.Process.Pid
}

// serveSim runs the simulator on the given pty.
func serveSim(t *testing.T, path string, dev *sim.Device) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	go func() { _ = sim.ServeRTU(f, dev) }()
}

// testConfig ignores socat, which keeps both pty slaves open, in the
// /proc scan and uses an empty lock directory.
func testConfig(t *testing.T, port string) RTUConfig {
	return RTUConfig{
		Port: port, Baud: 9600, Parity: 'E', StopBits: 1, Timeout: 500 * time.Millisecond,
		procRoot: t.TempDir(), lockDirs: []string{t.TempDir()},
	}
}

func openRTU(t *testing.T, cfg RTUConfig) (*RTU, *FrameLog) {
	t.Helper()
	log := NewFrameLog()
	r, err := OpenRTU(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, log
}

func rtuSetup(t *testing.T) (*sim.Device, *RTU, *FrameLog) {
	a, b, _ := ptyPair(t)
	dev := sim.NewDevice(1)
	serveSim(t, b, dev)
	r, log := openRTU(t, testConfig(t, a))
	return dev, r, log
}

func TestRTUSuccess(t *testing.T) {
	dev, r, log := rtuSetup(t)
	dev.Set(fc3, 2, 0x8000, 0x44A2)
	resp, err := r.Do(context.Background(), req(2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp.Values, []uint16{0x8000, 0x44A2}) {
		t.Errorf("values = %04X", resp.Values)
	}
	frames, _ := log.Snapshot()
	if len(frames) != 2 || frames[1].Note != "FC03 · 4 bytes · ok" {
		t.Errorf("frames = %+v", frames)
	}
	// Coils, a response shorter than the request.
	dev.Set(codec.ReadCoils, 0, 1, 0, 1)
	resp, err = r.Do(context.Background(), codec.ReadRequest{Unit: 1, Function: codec.ReadCoils, Quantity: 3})
	if err != nil || !reflect.DeepEqual(resp.Values, []uint16{1, 0, 1}) {
		t.Errorf("coils = %v, %v", resp.Values, err)
	}
}

func TestRTUException(t *testing.T) {
	dev, r, _ := rtuSetup(t)
	dev.SetMissing(fc3, 6)
	_, err := r.Do(context.Background(), req(6, 1))
	var exc *codec.ExceptionError
	if !errors.As(err, &exc) || exc.Code != codec.ExcIllegalDataAddress {
		t.Fatalf("err = %v", err)
	}
}

func TestRTUBadCRC(t *testing.T) {
	dev, r, log := rtuSetup(t)
	dev.SetFaults(sim.Faults{BadCRC: true})
	_, err := r.Do(context.Background(), req(0, 2))
	var crc *codec.CRCError
	if !errors.As(err, &crc) {
		t.Fatalf("err = %v", err)
	}
	frames, _ := log.Snapshot()
	if got := frames[len(frames)-1].Note; got != "CRC inválido" {
		t.Errorf("note = %q", got)
	}
}

func TestRTUEcho(t *testing.T) {
	dev, r, _ := rtuSetup(t)
	dev.SetFaults(sim.Faults{Echo: true})
	_, err := r.Do(context.Background(), req(0, 1))
	var echo *EchoError
	if !errors.As(err, &echo) || !strings.Contains(err.Error(), "eco") {
		t.Fatalf("err = %v", err)
	}
	// Short responses (coils) must also detect the echo.
	_, err = r.Do(context.Background(), codec.ReadRequest{Unit: 1, Function: codec.ReadCoils, Quantity: 1})
	if !errors.As(err, &echo) {
		t.Fatalf("coils err = %v", err)
	}
}

func TestRTUFragmented(t *testing.T) {
	dev, r, _ := rtuSetup(t)
	dev.Set(fc3, 0, 1, 2, 3, 4, 5)
	dev.SetFaults(sim.Faults{Fragments: 4, FragmentDelay: 40 * time.Millisecond})
	resp, err := r.Do(context.Background(), req(0, 5))
	if err != nil || !reflect.DeepEqual(resp.Values, []uint16{1, 2, 3, 4, 5}) {
		t.Fatalf("got %v, %v", resp.Values, err)
	}
}

func TestRTUTimeout(t *testing.T) {
	dev, r, log := rtuSetup(t)
	dev.SetSilent(fc3, 0, true)
	start := time.Now()
	_, err := r.Do(context.Background(), req(0, 1))
	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("timeout took %v", d)
	}
	frames, _ := log.Snapshot()
	if got := frames[len(frames)-1].Note; got != "timeout (500ms)" {
		t.Errorf("note = %q", got)
	}
	// Recovers on the next request.
	dev.SetSilent(fc3, 0, false)
	if _, err := r.Do(context.Background(), req(0, 1)); err != nil {
		t.Fatalf("after timeout: %v", err)
	}
}

func TestRTUContextCancel(t *testing.T) {
	dev, r, _ := rtuSetup(t)
	dev.SetSilent(fc3, 0, true)
	r.cfg.Timeout = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	if _, err := r.Do(ctx, req(0, 1)); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("cancel took %v", d)
	}
}

func TestRTUSecondOpenRefused(t *testing.T) {
	a, _, _ := ptyPair(t)
	openRTU(t, testConfig(t, a))
	_, err := OpenRTU(testConfig(t, a), nil)
	var busy *PortBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("second open: err = %v, want PortBusyError", err)
	}
}

func TestPortScanFindsOtherProcess(t *testing.T) {
	// socat keeps the pty open, so the real /proc scan must report it.
	a, _, pid := ptyPair(t)
	cfg := testConfig(t, a)
	cfg.procRoot = defaultProcRoot
	_, err := OpenRTU(cfg, nil)
	var busy *PortBusyError
	if !errors.As(err, &busy) || busy.PID != pid || busy.Process != "socat" {
		t.Fatalf("err = %v, want busy by socat (%d)", err, pid)
	}
	want := "A porta " + a + " está em uso pelo processo " + strconv.Itoa(pid) + " (socat)."
	if !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), "--force-port") {
		t.Errorf("message = %q", err.Error())
	}
	// --force-port skips the check.
	cfg.Force = true
	r, err := OpenRTU(cfg, nil)
	if err != nil {
		t.Fatalf("forced open: %v", err)
	}
	_ = r.Close()
}

func TestPortScanFakeProc(t *testing.T) {
	a, _, _ := ptyPair(t)
	proc := t.TempDir()
	fdDir := filepath.Join(proc, "4242", "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, filepath.Join(fdDir, "3")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "4242", "comm"), []byte("gatewayd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, name, ok := findPortUser(a, proc)
	if !ok || pid != 4242 || name != "gatewayd" {
		t.Errorf("findPortUser = %d, %q, %v", pid, name, ok)
	}
	if _, _, ok := findPortUser("/dev/null", proc); ok {
		t.Error("unrelated device reported as in use")
	}
}

func TestUUCPLock(t *testing.T) {
	a, _, socatPID := ptyPair(t)
	dir := t.TempDir()
	lock := filepath.Join(dir, "LCK.."+filepath.Base(a))

	// Live process: refused.
	if err := os.WriteFile(lock, []byte("     "+strconv.Itoa(socatPID)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t, a)
	cfg.lockDirs = []string{dir}
	_, err := OpenRTU(cfg, nil)
	var busy *PortBusyError
	if !errors.As(err, &busy) || busy.PID != socatPID {
		t.Fatalf("live lock: err = %v", err)
	}

	// Stale lock (dead process): accepted.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte(strconv.Itoa(dead.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRTU(cfg, nil)
	if err != nil {
		t.Fatalf("stale lock: %v", err)
	}
	_ = r.Close()

	if got := parseLockPID([]byte{0x39, 0x30, 0, 0}); got != 12345 {
		t.Errorf("binary lock pid = %d", got)
	}
}

func TestOpenErrors(t *testing.T) {
	cfg := RTUConfig{Port: "/dev/modtop-does-not-exist", Baud: 9600, Parity: 'E', StopBits: 1,
		procRoot: t.TempDir(), lockDirs: []string{t.TempDir()}}
	_, err := OpenRTU(cfg, nil)
	var oe *PortOpenError
	if !errors.As(err, &oe) || !strings.Contains(err.Error(), "não existe") {
		t.Errorf("missing port: %v", err)
	}
	cfg.Port = "/dev/null"
	if _, err := OpenRTU(cfg, nil); !errors.As(err, &oe) {
		t.Errorf("not a tty: %v", err)
	}
	cfg.Parity = 'X'
	if _, err := OpenRTU(cfg, nil); err == nil {
		t.Error("bad parity accepted")
	}
	perm := (&PortOpenError{Port: "/dev/ttyUSB0", Err: syscall.EACCES}).Error()
	if perm != "Sem permissão para /dev/ttyUSB0. Adicione seu usuário ao grupo dono da porta (geralmente \"dialout\") ou rode com sudo." {
		t.Errorf("permission message = %q", perm)
	}
}

func TestSilentInterval(t *testing.T) {
	tests := []struct {
		baud   int
		parity byte
		stop   int
		want   time.Duration
	}{
		{9600, 'E', 1, 4010416 * time.Nanosecond}, // 3.5 × 11 bits / 9600
		{9600, 'N', 2, 4010416 * time.Nanosecond},
		{19200, 'E', 1, 2005208 * time.Nanosecond},
		{38400, 'E', 1, 1750 * time.Microsecond},
	}
	for _, tt := range tests {
		got := silentInterval(RTUConfig{Baud: tt.baud, Parity: tt.parity, StopBits: tt.stop})
		if d := got - tt.want; d < -time.Microsecond || d > time.Microsecond {
			t.Errorf("%d %c %d: %v, want %v", tt.baud, tt.parity, tt.stop, got, tt.want)
		}
	}
}
