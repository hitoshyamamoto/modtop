package codec

import "fmt"

// RTUExceptionLen is the size of an RTU exception response.
const RTUExceptionLen = 5

// CRC16 computes the CRC-16/MODBUS of b (reflected polynomial 0xA001,
// initial value 0xFFFF).
func CRC16(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, x := range b {
		crc ^= uint16(x)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// appendCRC appends the CRC of b, low byte first.
func appendCRC(b []byte) []byte {
	crc := CRC16(b)
	return append(b, byte(crc), byte(crc>>8))
}

// EncodeRTU builds a Modbus RTU request frame.
func EncodeRTU(r ReadRequest) ([]byte, error) {
	pdu, err := EncodePDU(r)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 0, 1+len(pdu)+2)
	frame = append(frame, r.Unit)
	frame = append(frame, pdu...)
	return appendCRC(frame), nil
}

// RTUExpectedLen returns the size of a normal RTU response to r.
func RTUExpectedLen(r ReadRequest) int {
	return 1 + 1 + 1 + r.DataLen() + 2
}

// IsRTUException reports whether the second byte of an RTU response
// (the function byte) marks an exception.
func IsRTUException(function byte) bool {
	return function&exceptionFlag != 0
}

// DecodeRTU parses a Modbus RTU response frame to request r.
func DecodeRTU(frame []byte, r ReadRequest) (ReadResponse, error) {
	if len(frame) < RTUExceptionLen {
		return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("frame RTU com %d bytes", len(frame))}
	}
	n := len(frame)
	want := CRC16(frame[:n-2])
	got := uint16(frame[n-2]) | uint16(frame[n-1])<<8
	if got != want {
		return ReadResponse{}, &CRCError{Got: got, Want: want}
	}
	if frame[0] != r.Unit {
		return ReadResponse{}, &MismatchError{Field: FieldSlave, Want: int(r.Unit), Got: int(frame[0])}
	}
	return decodePDU(frame[1:n-2], r)
}
