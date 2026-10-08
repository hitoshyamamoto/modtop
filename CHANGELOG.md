# Changelog

All notable changes to modtop are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Each version uses the sections
Added, Changed, Deprecated, Removed, Fixed and Security, as needed.

## [Unreleased]

## [0.1.2] - 2026-10-08

### Fixed

- RTU: a late answer to a request that had timed out was taken as the
  answer to the next request, shifting every following value by one
  address while showing them as valid. After a timeout or a bad frame,
  the line is now drained before the next request, and the receive loop
  no longer reads past the expected frame.
- TCP: a timeout in the middle of a frame no longer leaves the connection
  out of sync; it is closed and reopened.
- The address ambiguity error now gives the 6-digit Modicon form of the
  address, so following it reads the intended register.
- A serial path that is not a serial device (e.g. `/dev/null`) is reported
  as such instead of as a port in use.
- `ttyUSB0` without `/dev/` is a usage error suggesting the full path.
- Without a terminal, modtop stops before connecting with a clear message.

### Changed

- `-u` accepts 0–255 on Modbus TCP (255 is the value recommended for a
  server addressed directly); RTU keeps 1–247.

## [0.1.1] - 2026-10-08

### Added

- Container image on the GitHub Container Registry
  (`ghcr.io/hitoshyamamoto/modtop`, linux/amd64, arm64, arm/v7) for Modbus
  TCP use, built from the release binaries and with a provenance
  attestation.
- `go install github.com/hitoshyamamoto/modtop/cmd/modtop@<version>` now
  reports the right version in `modtop --version`.
- Documentation: quick start, architecture, assurance case, security model,
  roadmap, governance roles, and how to reproduce a release.
- Version tags are signed (SSH key in `.github/allowed_signers`).

### Changed

- Contributions require a Developer Certificate of Origin sign-off
  (`git commit -s`), checked in CI.

### Fixed

- Release binaries are built from a clean working tree, so they record
  `vcs.modified=false` and can be reproduced from the tagged source.

## [0.1.0] - 2026-10-07

First version.

### Added

- Live view of one range of up to 250 consecutive addresses of one device,
  over Modbus TCP or Modbus RTU (Linux: amd64, arm64, armv7).
- Read functions FC01–FC04 only; no write function exists in the code.
- Address notations Modicon (5 and 6 digits), base 1 and base 0, with
  the translation of the selected address to table, function and wire
  address. Base 0/base 1 input that looks like a Modicon address is
  refused.
- Per-row types uint16, int16, uint32, int32, float32 (bool for bit
  tables) and byte orders ABCD, CDAB, BADC, DCBA, chosen during the
  session; the selected pair is shown in all four orders.
- Frames panel with the last 200 raw frames and exceptions explained.
- Block reads with automatic fallback to one register per request on
  exception 02; `--single`.
- Stale values always shown with their age and reason; connection health
  indicator.
- Serial port protection: refuses a port used by another process
  (`/proc` scan, UUCP lock files, `flock`, `TIOCEXCL`); `--force-port`.
- Clean exit and terminal restoration on `q`, Ctrl+C, SIGTERM, SIGHUP and
  internal errors.
- Release binaries with SHA-256 checksums and build provenance
  attestations.

## Ideas on record

Suggestions outside the current scope, kept here so they are not lost.
Each one needs a real field case before it is considered (see
[GOVERNANCE.md](GOVERNANCE.md)).

- Session profiles in a file (planned theme for 0.2).
- A compact layout for terminals narrower than 80 columns (SSH from a
  phone in portrait).
- Reopening an RTU port after a USB adapter is unplugged and plugged back.
