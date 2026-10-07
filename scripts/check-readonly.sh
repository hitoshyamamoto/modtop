#!/bin/sh
# Fails if any Modbus write function appears in the source tree.
# modtop is read-only by absence: write functions (FC05, FC06, FC15, FC16)
# must not exist anywhere, not even in the codec.
set -eu

status=0

# Write function names, anywhere (including tests).
if grep -rnEi --include='*.go' \
	'FC ?0?(5|6|15|16)\b|write ?(single|multiple)|writecoil|writeregister' .; then
	echo "error: Modbus write function reference found" >&2
	status=1
fi

# Write function code values in non-test code. Exception codes 5 and 6
# are written in decimal on purpose so they never match here.
if grep -rnE --include='*.go' --exclude='*_test.go' \
	'0x0?[56fF]\b|0x10\b' .; then
	echo "error: hex literal matching a Modbus write function code found" >&2
	status=1
fi

exit "$status"
