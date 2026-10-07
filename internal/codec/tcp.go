package codec

import (
	"encoding/binary"
	"fmt"
)

// MBAPHeaderLen is the size of the MBAP header, unit ID included.
const MBAPHeaderLen = 7

// maxTCPLength is the largest valid MBAP length field (unit ID + 253-byte PDU).
const maxTCPLength = 254

// EncodeTCP builds a Modbus TCP request frame.
func EncodeTCP(r ReadRequest, transaction uint16) ([]byte, error) {
	pdu, err := EncodePDU(r)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, MBAPHeaderLen+len(pdu))
	binary.BigEndian.PutUint16(frame[0:], transaction)
	binary.BigEndian.PutUint16(frame[2:], 0) // protocol ID
	binary.BigEndian.PutUint16(frame[4:], uint16(1+len(pdu)))
	frame[6] = r.Unit
	copy(frame[MBAPHeaderLen:], pdu)
	return frame, nil
}

// TCPFrameLen returns the total frame size announced by an MBAP header.
// header must hold at least the first 6 bytes of the frame.
func TCPFrameLen(header []byte) (int, error) {
	if len(header) < 6 {
		return 0, &MalformedError{Reason: "incomplete MBAP header"}
	}
	length := int(binary.BigEndian.Uint16(header[4:]))
	if length < 2 || length > maxTCPLength {
		return 0, &MalformedError{Reason: fmt.Sprintf("invalid length field %d", length)}
	}
	return 6 + length, nil
}

// DecodeTCP parses a Modbus TCP response frame to request r sent with the
// given transaction ID.
func DecodeTCP(frame []byte, r ReadRequest, transaction uint16) (ReadResponse, error) {
	if len(frame) < MBAPHeaderLen+2 {
		return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("TCP frame of %d bytes", len(frame))}
	}
	if got := binary.BigEndian.Uint16(frame[0:]); got != transaction {
		return ReadResponse{}, &MismatchError{Field: FieldTransaction, Want: int(transaction), Got: int(got)}
	}
	if proto := binary.BigEndian.Uint16(frame[2:]); proto != 0 {
		return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("protocol ID %d (expected 0)", proto)}
	}
	if length := int(binary.BigEndian.Uint16(frame[4:])); length != len(frame)-6 {
		return ReadResponse{}, &MalformedError{Reason: fmt.Sprintf("length field %d, but the frame has %d bytes after it", length, len(frame)-6)}
	}
	if frame[6] != r.Unit {
		return ReadResponse{}, &MismatchError{Field: FieldUnit, Want: int(r.Unit), Got: int(frame[6])}
	}
	return decodePDU(frame[MBAPHeaderLen:], r)
}
