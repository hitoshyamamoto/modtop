# Assurance case

This document argues why modtop meets its security requirements, which are
stated in [SECURITY.md](../SECURITY.md#security-model-what-you-can-and-cannot-expect).
It is a self-assessment by the maintainer, not an independent audit.

## What is protected

| Asset | Why it matters |
|---|---|
| The industrial bus and the devices on it | A wrong frame or a second master can disturb production. |
| The machine modtop runs on (often a field gateway) | modtop must not crash, hang or harm it. |
| The user's terminal | It must be left usable on every exit path. |
| The integrity of what users download | A tampered binary would run on industrial networks. |

## Threat model

| Source | What it can do |
|---|---|
| A faulty or malicious device (or anything on the network path) | Send malformed, truncated, oversized, mismatched or slow responses; never answer. |
| Another process on the same serial port | Drive the same RS-485 bus at the same time. |
| The local user | Pass any command line, including mistakes. The local user is trusted not to attack their own machine. |
| A compromised dependency or build pipeline | Ship modified code in a release. |

Out of scope: an attacker who controls the machine modtop runs on, and the
lack of authentication and encryption in Modbus itself (see residual risks).

## Trust boundaries

1. **Network and serial line → codec.** Every byte from a device is
   untrusted until the codec has validated it.
2. **Command line → configuration.** Arguments are validated before any
   connection is made.
3. **The serial port.** A resource shared with other processes; modtop must
   not take it over silently.
4. **Source → release artifacts.** The path from the repository to the
   binaries and the container image.

## Secure design principles applied

- **Fail-safe defaults.** Read-only by absence of code; a serial port in use
  is refused by default; invalid input is rejected, never guessed.
- **Least privilege.** modtop needs no root (only access to the serial
  port); the container image runs as an unprivileged user on an empty base.
- **Complete mediation.** Every response is checked (size, byte count, CRC,
  transaction, unit, slave and function) before any value is used.
- **Economy of mechanism.** A small code base with five direct
  dependencies and no configuration surface.
- **Defense in depth** for the serial port: a `/proc` scan, UUCP lock files,
  `flock` and `TIOCEXCL`.
- **Psychological acceptability.** Every error says what happened, the
  likely cause and what to do; stale values are never shown as current.

## Common weaknesses countered

| Weakness | Countermeasure | Evidence |
|---|---|---|
| Improper input validation (CWE-20) | Allowlist validation of all arguments and of every response field | `internal/address`, `internal/codec`, `cmd/modtop`; tests with invalid input |
| Out-of-bounds read/write (CWE-125, CWE-787) | Memory-safe language, no cgo, no `unsafe`; sizes checked before slicing | fuzzing of both response parsers in CI |
| Uncontrolled resource consumption (CWE-400) | Bounded frame size (MBAP length ≤ 254), a 200-frame ring buffer, at most 250 addresses, per-request timeouts | `internal/codec/tcp.go`, `internal/transport/framelog.go` |
| Race conditions (CWE-362) | One request at a time, channels between goroutines | the race detector on every CI run |
| Terminal escape injection (CWE-150) | Device data is shown only as numbers and hex, never as text | `internal/ui/view.go` |
| Vulnerable dependencies (CWE-1395) | `govulncheck` on every change; Dependabot alerts and updates | `.github/workflows/ci.yml`, `.github/dependabot.yml` |
| Supply chain tampering | SHA-pinned CI actions; reproducible builds; SHA-256 checksums; Sigstore provenance attestations for binaries and image; signed tags | `.github/workflows/release.yml`, `CONTRIBUTING.md` |

## Residual risks

- **Port-in-use detection is best effort.** A process owned by another user
  may be invisible to modtop when it does not run as root, and inside a
  container the checks do not see the host. The README says so and
  recommends the native binary for RTU.
- **Modbus has no security.** Requests and responses travel in clear text
  and are not authenticated; modtop can only be as trustworthy as the
  network path. An SSH tunnel is documented for remote access.
- **Single maintainer.** There is no independent review of changes (bus
  factor 1); see [GOVERNANCE.md](../GOVERNANCE.md#continuity).
