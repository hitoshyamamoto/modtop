package transport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Defaults for the port checks; tests point them elsewhere.
var (
	defaultProcRoot = "/proc"
	defaultSysRoot  = "/sys"
	defaultLockDirs = []string{"/var/lock", "/run/lock"}
)

// PortBusyError means another process is using the serial port.
type PortBusyError struct {
	Port    string
	PID     int    // 0 when unknown
	Process string // process name, when known
	Forced  bool   // --force-port was given but the port is still unavailable
}

func (e *PortBusyError) Error() string {
	who := "another process"
	if e.PID > 0 {
		who = fmt.Sprintf("process %d (%s)", e.PID, e.Process)
	}
	if e.Forced {
		return fmt.Sprintf("Port %s is locked in exclusive mode by %s.\n"+
			"Not even --force-port can open it without root privileges.", e.Port, who)
	}
	return fmt.Sprintf("Port %s is in use by %s.\n"+
		"Modbus RTU allows a single master per bus; using the port now may\n"+
		"corrupt production traffic. Use --force-port only if you are sure.", e.Port, who)
}

// PortOpenError means the serial port could not be opened (missing,
// no permission, not a serial port).
type PortOpenError struct {
	Port string
	Err  error
}

func (e *PortOpenError) Error() string {
	switch {
	case errors.Is(e.Err, unix.ENOENT):
		return fmt.Sprintf("Port %s does not exist. Check that the adapter is connected and the port name (ls /dev/ttyUSB* /dev/ttyACM* /dev/ttyS*).", e.Port)
	case errors.Is(e.Err, unix.EACCES), errors.Is(e.Err, unix.EPERM):
		return fmt.Sprintf("No permission for %s. Add your user to the group that owns the port (usually \"dialout\") or run with sudo.", e.Port)
	case errors.Is(e.Err, unix.ENOTTY):
		return fmt.Sprintf("%s is not a serial port. Check the path.", e.Port)
	}
	return fmt.Sprintf("Failed to open %s: %v. Check the adapter and the port name.", e.Port, e.Err)
}

func (e *PortOpenError) Unwrap() error { return e.Err }

// portLock holds the descriptor that carries our flock on the port.
type portLock struct {
	fd int
}

func (l *portLock) release() {
	if l != nil && l.fd >= 0 {
		_ = unix.Close(l.fd)
		l.fd = -1
	}
}

// lockPort runs the best-effort checks that keep modtop from becoming a
// second master on a bus already driven by another process:
//
//  1. scan /proc/*/fd for another process holding the device open;
//  2. look for a live UUCP lock file (LCK..name);
//  3. take flock(LOCK_EX|LOCK_NB) on our own descriptor.
//
// The fourth layer, TIOCEXCL, is applied by the serial library when it
// opens the port. With force, checks 1–3 do not refuse the port.
func lockPort(path string, force bool, procRoot, sysRoot string, lockDirs []string) (*portLock, error) {
	if err := checkSerialDevice(path, sysRoot); err != nil {
		return nil, err
	}
	if !force {
		if pid, name, ok := findPortUser(path, procRoot); ok {
			return nil, &PortBusyError{Port: path, PID: pid, Process: name}
		}
		if pid, ok := findUUCPLock(path, lockDirs); ok {
			return nil, &PortBusyError{Port: path, PID: pid, Process: processName(procRoot, pid)}
		}
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.EBUSY) {
			pid, name, _ := findPortUser(path, procRoot)
			return nil, &PortBusyError{Port: path, PID: pid, Process: name, Forced: force}
		}
		return nil, &PortOpenError{Port: path, Err: err}
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil && !force {
		_ = unix.Close(fd)
		return nil, &PortBusyError{Port: path}
	}
	return &portLock{fd: fd}, nil
}

// checkSerialDevice tells, without opening it, whether path can be a serial
// port. It runs before the in-use checks so that something like /dev/null,
// which every process holds open, is reported as not a serial port rather
// than as a port in use. Opening a serial port can toggle its modem lines,
// so it is not opened here.
func checkSerialDevice(path, sysRoot string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return &PortOpenError{Port: path, Err: err}
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return &PortOpenError{Port: path, Err: unix.ENOTTY}
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	rdev := uint64(st.Rdev) //nolint:unconvert // Rdev is not uint64 on every platform
	link := filepath.Join(sysRoot, "dev", "char", fmt.Sprintf("%d:%d", unix.Major(rdev), unix.Minor(rdev)))
	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		// No sysfs entry (pseudo-terminals have none): cannot tell, go on.
		return nil
	}
	if !strings.Contains(target+"/", "/tty/") {
		return &PortOpenError{Port: path, Err: unix.ENOTTY}
	}
	return nil
}

// findPortUser returns a process, other than this one, with an open
// descriptor on the same device as path. Unreadable processes are skipped.
func findPortUser(path, procRoot string) (pid int, name string, found bool) {
	target, err := os.Stat(path)
	if err != nil {
		return 0, "", false
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0, "", false
	}
	self := os.Getpid()
	for _, e := range entries {
		p, err := strconv.Atoi(e.Name())
		if err != nil || p == self {
			continue
		}
		fdDir := filepath.Join(procRoot, e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			fi, err := os.Stat(filepath.Join(fdDir, fd.Name()))
			if err == nil && os.SameFile(fi, target) {
				return p, processName(procRoot, p), true
			}
		}
	}
	return 0, "", false
}

func processName(procRoot string, pid int) string {
	b, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm")) //nolint:gosec // fixed /proc path
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(b))
}

// findUUCPLock returns the PID in a UUCP lock file for path, if that
// process is alive.
func findUUCPLock(path string, lockDirs []string) (int, bool) {
	name := "LCK.." + filepath.Base(path)
	for _, dir := range lockDirs {
		b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // fixed lock directories
		if err != nil {
			continue
		}
		pid := parseLockPID(b)
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		if err := unix.Kill(pid, 0); err == nil || errors.Is(err, unix.EPERM) {
			return pid, true
		}
	}
	return 0, false
}

// parseLockPID reads a UUCP lock file: ASCII PID (HDB style) or a 4-byte
// binary integer (old style).
func parseLockPID(b []byte) int {
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
		return pid
	}
	if len(b) == 4 {
		return int(int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
	}
	return 0
}
