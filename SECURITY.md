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

## Security model: what you can and cannot expect

You can expect that modtop:

- only sends read requests (functions 01–04); no write function exists in
  the code;
- talks only to the target you give it, and stores nothing on disk;
- validates every response before using it, and never crashes or hangs on
  malformed data (the parsers are fuzzed);
- refuses a serial port that another process is using, unless you pass
  `--force-port`;
- never shows a value from a failed read as current;
- restores your terminal on every exit path.

You cannot expect that modtop:

- protects the traffic: Modbus has no encryption or authentication, so
  anyone on the network path can read or alter it (use an SSH tunnel for
  remote access);
- detects every process using a serial port: the detection is best effort,
  and it does not work across a container boundary;
- validates what a device reports: values are shown as received.

The reasoning behind these claims is in
[docs/assurance-case.md](docs/assurance-case.md).

## How we handle a report

1. Acknowledge the report within 5 business days.
2. Reproduce it and assess the impact within 14 days, keeping the reporter
   informed.
3. Prepare the fix in a private fork of a GitHub security advisory, with a
   regression test.
4. Release the fix and publish the advisory, with a CVE when one applies.
   The changelog lists the fix under "Security".
5. Credit the reporter in the advisory and the changelog, unless they ask
   to stay anonymous.

