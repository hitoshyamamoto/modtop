package sim

import (
	"io"
	"time"

	"github.com/hitoshyamamoto/modtop/internal/codec"
)

// rtuRequestLen is the size of every read request frame (FC01–FC04).
const rtuRequestLen = 8

// ServeRTU answers RTU read requests read from rw until it returns an error.
// Frames with a bad CRC are ignored, like a real slave would.
func ServeRTU(rw io.ReadWriter, dev *Device) error {
	frame := make([]byte, rtuRequestLen)
	for {
		if _, err := io.ReadFull(rw, frame); err != nil {
			return err
		}
		n := len(frame)
		if codec.CRC16(frame[:n-2]) != uint16(frame[n-2])|uint16(frame[n-1])<<8 {
			continue
		}
		resp, delay, reply := dev.handle(frame[0], frame[1:n-2])
		faults := dev.takeFaults()
		if !reply {
			continue
		}
		time.Sleep(delay)
		out := append([]byte{frame[0]}, resp...)
		crc := codec.CRC16(out)
		if faults.BadCRC {
			crc ^= 1
		}
		out = append(out, byte(crc), byte(crc>>8))
		if faults.Echo {
			out = append(append([]byte{}, frame...), out...)
		}
		if err := writeFragments(rw, out, faults.Fragments, faults.FragmentDelay); err != nil {
			return err
		}
	}
}

func writeFragments(w io.Writer, b []byte, parts int, delay time.Duration) error {
	if parts < 2 {
		_, err := w.Write(b)
		return err
	}
	size := (len(b) + parts - 1) / parts
	for len(b) > 0 {
		n := min(size, len(b))
		if _, err := w.Write(b[:n]); err != nil {
			return err
		}
		b = b[n:]
		if len(b) > 0 {
			time.Sleep(delay)
		}
	}
	return nil
}
