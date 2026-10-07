package codec

import "fmt"

// ExceptionCode is the code carried by a Modbus exception response.
type ExceptionCode byte

// Known exception codes. Written in decimal on purpose (see
// scripts/check-readonly.sh).
const (
	ExcIllegalFunction        ExceptionCode = 1
	ExcIllegalDataAddress     ExceptionCode = 2
	ExcIllegalDataValue       ExceptionCode = 3
	ExcServerDeviceFailure    ExceptionCode = 4
	ExcAcknowledge            ExceptionCode = 5
	ExcServerDeviceBusy       ExceptionCode = 6
	ExcGatewayPathUnavailable ExceptionCode = 10
	ExcGatewayTargetNoReply   ExceptionCode = 11
)

// Name returns the exception name shown in the UI.
func (c ExceptionCode) Name() string {
	switch c {
	case ExcIllegalFunction:
		return "Illegal function"
	case ExcIllegalDataAddress:
		return "Illegal data address"
	case ExcIllegalDataValue:
		return "Illegal data value"
	case ExcServerDeviceFailure:
		return "Server device failure"
	case ExcAcknowledge:
		return "Acknowledge"
	case ExcServerDeviceBusy:
		return "Server device busy"
	case ExcGatewayPathUnavailable:
		return "Gateway path unavailable"
	case ExcGatewayTargetNoReply:
		return "Gateway target device failed to respond"
	}
	return fmt.Sprintf("Unknown exception (0x%02X)", byte(c))
}

// Hint returns advice for the user, or "" when there is none.
func (c ExceptionCode) Hint() string {
	switch c {
	case ExcIllegalFunction:
		return "The device does not support this function on this table. Check that the table is right."
	case ExcIllegalDataAddress:
		return "Usually a wrong table or a base 0/base 1 mix-up. " +
			"Check the manual; restart with the right range or --convention."
	case ExcIllegalDataValue:
		return "The requested quantity may be too large for the device."
	case ExcServerDeviceFailure:
		return "Internal error in the slave."
	case ExcAcknowledge:
		return "Request accepted; processing takes a while."
	case ExcServerDeviceBusy:
		return "Try a longer interval."
	case ExcGatewayPathUnavailable:
		return "The gateway found no route to the slave."
	case ExcGatewayTargetNoReply:
		return "Check the unit ID and the gateway's serial link."
	}
	return ""
}

// ExceptionError is an exception response from the device.
type ExceptionError struct {
	Function byte
	Code     ExceptionCode
}

// Error returns e.g. "exc 02 · illegal data address".
func (e *ExceptionError) Error() string {
	name := []rune(e.Code.Name())
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		name[0] += 'a' - 'A'
	}
	return fmt.Sprintf("exc %02X · %s", byte(e.Code), string(name))
}

// CRCError is an RTU frame whose CRC does not match its contents.
type CRCError struct {
	Got, Want uint16
}

func (e *CRCError) Error() string {
	return "invalid CRC"
}

// Field identifies what did not match in a MismatchError.
type Field string

// Fields checked against the request.
const (
	FieldTransaction Field = "transaction ID"
	FieldUnit        Field = "unit ID"
	FieldSlave       Field = "slave address"
	FieldFunction    Field = "function"
)

// MismatchError is a response that does not belong to the request:
// different transaction ID, unit ID, slave address or function.
type MismatchError struct {
	Field     Field
	Want, Got int
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("response does not match the request: %s %d (expected %d)", e.Field, e.Got, e.Want)
}

// MalformedError is a response with incoherent sizes or fields.
type MalformedError struct {
	Reason string
}

func (e *MalformedError) Error() string {
	return "malformed response: " + e.Reason
}
