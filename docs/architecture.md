# Architecture

modtop is a single Go binary. This document describes its main parts, how
they depend on each other, and the properties the design keeps.

## Packages

| Package | Role | I/O |
|---|---|---|
| `cmd/modtop` | command line: flags, validation, exit codes; wires the parts together | starts everything |
| `internal/ui` | the terminal interface (Bubble Tea): list, translation, byte orders, frames panel, help | terminal only |
| `internal/poller` | the read cycle: splits the range into blocks, keeps per-address state, connection health and statistics | through a transport |
| `internal/transport` | sends one request at a time over TCP or a serial line (RTU); serial port protection; the frame log | network, serial port, `/proc`, lock files |
| `internal/codec` | builds read requests (FC01–FC04) and parses responses, TCP and RTU framing, CRC | none |
| `internal/decode` | interprets registers as uint16/int16/uint32/int32/float32 in the four byte orders; formats values | none |
| `internal/address` | the canonical address (table + wire address) and the Modicon, base 1 and base 0 notations | none |
| `internal/sim` | a programmable Modbus slave, used only by tests and the development server | test only |

Everything is under `internal/`: modtop offers no library API.

## Dependency rule

```
cmd/modtop ──► ui ──► poller ──► transport ──► codec
                         │           │
                         └──► decode, address (pure, no I/O)
```

- `codec`, `decode` and `address` do no I/O and import no other package of
  the project, so they are tested exhaustively with fixed vectors and fuzzing.
- No package imports `ui`. The UI never calls the transport.
- `sim` imports only `codec` and never ends up in the modtop binary.

## Data flow

1. `cmd/modtop` validates the command line, checks that it runs in a
   terminal and opens the transport. A failure here ends the program with
   exit code 1, 3 or 4, before the terminal interface starts.
2. The poller runs in its own goroutine. Each cycle reads the range in
   blocks (or one address at a time) through `Transport.Do`, updates the
   cells and sends a `CycleResult` on a channel that keeps only the latest
   result, so a slow UI never stalls polling.
3. The transport records every raw frame, sent and received, with a short
   note in a ring buffer of 200 frames (`FrameLog`), which the UI reads to
   draw the frames panel.
4. The UI redraws only when a result, a key or a resize arrives.

## Concurrency

- One goroutine polls; the Bubble Tea program owns the UI state; small
  goroutines forward results and signals.
- The transport handles one request at a time. The frame log is guarded by
  a mutex. Results cross goroutines only through channels.
- Every goroutine started by modtop is guarded: a panic stops the program,
  restores the terminal and exits with code 70.
- The test suite runs with the race detector in CI; the fuzzers run
  without it (fuzzing and the race detector are not combined).

## Properties the design keeps

- **Read-only by absence.** Only functions 01–04 exist; a CI check fails if
  a write function code appears in the sources.
- **No stale value without its age.** A cell whose last read failed keeps
  its last good value but is always shown with its age and the reason.
- **No answer attributed to the wrong request.** TCP matches each response
  by its transaction ID and reconnects when a timeout leaves part of a
  frame unread. RTU has no transaction ID: after a timeout or a bad frame,
  the next request first drains the line for up to one timeout, and a
  response is never read past its expected size (except to recognize an
  echo of the request).
- **One master per serial bus.** A path that is not a serial device is
  rejected first (from sysfs, without opening it). A serial port used by
  another process is refused unless the user forces it, and a forced port
  is flagged for the whole session.
- **Zero configuration.** No configuration file, environment variable
  (other than `NO_COLOR`) or network access besides the target.

## Testing strategy

- Fixed vectors from the specification for addressing, decoding and
  framing, in both directions.
- Fuzzing of the TCP and RTU response parsers, which must never panic.
- Transports tested against the simulator: TCP on `127.0.0.1`, RTU over a
  pair of pseudo-terminals created by `socat`.
- The UI tested as a model (keys and results in, state out) and through
  golden files of the rendered screen at 80×24.
- CI enforces at least 80% statement coverage.
