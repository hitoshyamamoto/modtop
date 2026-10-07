# Security policy

## Reporting a vulnerability

Please do not open a public issue. Report it privately through
[GitHub private vulnerability reporting](https://github.com/hitoshyamamoto/modtop/security/advisories/new).

Include a description, the steps to reproduce, the affected version
(`modtop --version`) and, if you have one, a suggested fix. You can expect
an acknowledgement within 5 business days and an assessment within 14 days.

## Supported versions

Only the latest release receives security fixes.

## Scope

modtop is a read-only diagnostic tool. Issues of particular interest:

- any way to make modtop transmit something other than a read request
  (FC01–FC04);
- any way to make it open a serial port that another process is using
  without `--force-port`;
- crashes or hangs caused by responses from a device (the response parsers
  are fuzzed in CI);
- terminal escape sequences from device data reaching the screen.

Known limitation: detecting that another process uses a serial port is best
effort. When that process belongs to another user and modtop does not run as
root, it may not be visible.
