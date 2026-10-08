# modtop

[![CI](https://github.com/hitoshyamamoto/modtop/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/hitoshyamamoto/modtop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/hitoshyamamoto/modtop?include_prereleases)](https://github.com/hitoshyamamoto/modtop/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/hitoshyamamoto/modtop)](go.mod)
[![License](https://img.shields.io/github/license/hitoshyamamoto/modtop)](LICENSE)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/15299/badge)](https://www.bestpractices.dev/projects/15299)

An interactive terminal tool to **see and understand what a Modbus device is saying**, live, including over SSH on headless field gateways.

It is meant for the field engineer or technician commissioning or troubleshooting a meter, an inverter or a PLC: it lists a range of registers, translates addresses between the notations found in manuals, shows a 32-bit value in all four byte orders at once, and shows the raw frames on the wire.

```
 modtop · 10.1.8.99:502 · unit 1 · 40001–40020 · Modicon · 1s               ● ok
┌──────────────────────────────────────────────────────────────────────────────┐
│ Address    Raw      Type      Value                                          │
│ 40001      0x4248   float32   50.0                 ┐ ABCD                    │
│ 40002      0x0000             ·                    ┘                         │
│▶40003      0x8000   float32   1300.0               ┐ CDAB                    │
│ 40004      0x44A2             ·                    ┘                         │
│ 40005      0x01F4   uint16    500                                            │
│ 40006      0xFFF6   int16     -10                                            │
│ 40007      —        uint16    — (exc 02)                                     │
│ 40008      0x0064   uint16    100  12s ago · timeout                         │
└──────────────────────────────────────────────────────────────────────────────┘
 40003 → Holding register #3 → FC03 → on the wire: 2 (0x0002)
 ABCD -2.46208e-41 · [CDAB 1300.0] · BADC 1.18132e-38 · DCBA -2.65632e-18
 ok 1243 · timeouts 2 · exceptions 1 · cycle 38 ms
 ↑↓ move  t type  o order  c convention  f frames  p pause  ? help  q quit
```

modtop is **read-only**: the Modbus write functions are not implemented anywhere in the code. It reads one device and one range of up to 250 addresses per session, over Modbus TCP or Modbus RTU, on Linux (amd64, arm64, armv7).

## Quick start

On a Linux x86-64 machine that can reach a Modbus TCP device:

```sh
curl -LO https://github.com/hitoshyamamoto/modtop/releases/latest/download/modtop-linux-amd64
chmod +x modtop-linux-amd64
./modtop-linux-amd64 10.1.8.99 -u 1 -r 40001-40020
```

Use your device's address, unit ID and register range from its manual. In
the list, select a register and press `t` to change its type, `f` to see
the raw frames, `?` for help and `q` to quit.

## Installation

Download the binary for your platform from the [releases page](https://github.com/hitoshyamamoto/modtop/releases), check it and make it executable:

```sh
curl -LO https://github.com/hitoshyamamoto/modtop/releases/latest/download/modtop-linux-arm64
curl -LO https://github.com/hitoshyamamoto/modtop/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
chmod +x modtop-linux-arm64
sudo mv modtop-linux-arm64 /usr/local/bin/modtop
```

The binaries are `modtop-linux-amd64`, `modtop-linux-arm64` and `modtop-linux-armv7`. They are static, need no configuration file, and run on a clean machine.

Each release also carries a build provenance attestation. With the GitHub CLI you can check that a binary was built by this repository's release workflow:

```sh
gh attestation verify modtop-linux-arm64 --repo hitoshyamamoto/modtop
```

### Container image

For Modbus TCP, modtop is also published as a multi-architecture container image (linux/amd64, arm64, arm/v7) on the GitHub Container Registry. The image holds the same release binary and nothing else.

```sh
docker run -it --rm ghcr.io/hitoshyamamoto/modtop:0.1.2 10.1.8.99 -u 1 -r 40001-40020
```

- Add `-v /etc/localtime:/etc/localtime:ro` to show the frame times in local time instead of UTC.
- To reach a device on the host's network namespace (e.g. an SSH tunnel on `127.0.0.1`), add `--network host`.
- Check the image's provenance with `gh attestation verify oci://ghcr.io/hitoshyamamoto/modtop:0.1.2 --repo hitoshyamamoto/modtop`.

**For Modbus RTU, use the native binary.** Inside a container, modtop cannot reliably see whether another process on the host is using the serial port, so the one-master-per-bus protection described under [Safety](#safety) would not hold.

### From source

With Go 1.27 or later, install the latest release into `$(go env GOPATH)/bin`
(or `$GOBIN`):

```sh
go install github.com/hitoshyamamoto/modtop/cmd/modtop@latest
```

To uninstall, delete that file: `rm "$(go env GOPATH)/bin/modtop"`.

To build from a checkout:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" ./cmd/modtop
```

Release builds are reproducible; see "Reproducing a release" in
[CONTRIBUTING.md](CONTRIBUTING.md).

### Verifying a release

- Checksums: `sha256sum --check --ignore-missing SHA256SUMS`, as above.
- Provenance: `gh attestation verify <binary> --repo hitoshyamamoto/modtop`
  (and `oci://ghcr.io/hitoshyamamoto/modtop:<version>` for the image).
- Version tags, from v0.1.1 on, are signed with the SSH key in
  [.github/allowed_signers](.github/allowed_signers):

  ```sh
  git clone https://github.com/hitoshyamamoto/modtop && cd modtop
  git -c gpg.ssh.allowedSignersFile=.github/allowed_signers tag -v v0.1.1
  ```

## Usage

Three common ways to use it:

**Modbus TCP, directly:**

```sh
modtop 10.1.8.99 -u 1 -r 40001-40020
```

**Modbus RTU on a gateway, over SSH** (run modtop on the gateway itself):

```sh
ssh user@gateway
modtop /dev/ttyS1 --baud 9600 --parity N -u 1 -r 40001-40020
```

**Through an SSH tunnel** to a TCP device behind the gateway:

```sh
ssh -N -L 1502:<internal-ip>:502 user@gateway
modtop 127.0.0.1:1502 -u 1 -r 40001-40020
```

Run `modtop --help` for all options. The main ones:

| Option | Meaning |
|---|---|
| `-r, --range` | address range, e.g. `40001-40020` (required, up to 250 addresses) |
| `-u, --unit` | unit ID (default 1): 0–255 on TCP, slave address 1–247 on RTU |
| `--convention` | `modicon` (default), `base1` or `base0` |
| `--table` | `holding`, `input`, `coil` or `discrete`; required with `base1`/`base0` |
| `--order` | initial byte order of 32-bit pairs: `ABCD` (default), `CDAB`, `BADC`, `DCBA` |
| `-i, --interval` | time between cycles (default `1s`, minimum `100ms`) |
| `-t, --timeout` | timeout per request (default `1s`) |
| `--single` | read one register per request |
| `--baud`, `--parity`, `--stopbits` | RTU line settings (default 9600, `E`, 1; 8 data bits) |
| `--force-port` | RTU: open the port even if another process uses it (dangerous) |

Keys: `↑`/`↓` (or `k`/`j`) move, `PgUp`/`PgDn` page, `Home`/`End` (or `g`/`G`) jump, `t` cycles the type of the row, `o` cycles the byte order of a pair, `c` cycles the address convention shown, `f` opens the frames panel, `p` pauses, `?` shows the help, `q` quits.

A value whose last read failed is never shown as current: it carries its age and the reason (`100  12s ago · timeout`).

### Exit codes

| Code | Meaning |
|---|---|
| 0 | normal exit |
| 1 | usage error (invalid options or range) |
| 2 | reserved |
| 3 | initial connection failed (TCP refused, serial port missing or no permission) |
| 4 | serial port in use |
| 70 | internal error (a bug; please report it) |
| 129, 130, 143 | ended by SIGHUP (e.g. SSH drop), Ctrl+C, SIGTERM |

## Finding the word order

A 32-bit value (`uint32`, `int32`, `float32`) spans two registers, and devices disagree on the order of their four bytes. Select the first register of the pair and press `t` until the type matches the manual. The line below the list then shows the value in all four orders:

```
 ABCD -2.46208e-41 · [CDAB 1300.0] · BADC 1.18132e-38 · DCBA -2.65632e-18
```

Pick the one that makes physical sense (1300.0 W, not -2.46208e-41) and press `o` until that order is active. If none of the four makes sense, the pair is probably shifted by one register: move one row up or down and try again. See [docs/addressing.md](docs/addressing.md).

## Safety

- **Read-only.** Only functions 01–04 exist in the code; the write functions are not implemented, and CI fails if one appears.
- **One master per RTU bus.** Modbus RTU allows a single master. Before opening a serial port, modtop refuses it (exit code 4) when another process has it open, when a live UUCP lock file exists, or when an exclusive lock cannot be taken; it then marks the port exclusive (`TIOCEXCL`). `--force-port` skips these checks and shows `⚠ FORCED PORT` in the header for the whole session.
- **Limitation:** the in-use detection is best effort. When the other process belongs to another user and modtop does not run as root, it may not be visible.

## Troubleshooting

| Symptom | What to check |
|---|---|
| No response on RTU | Baud, parity and stop bits must match the device. Many devices use parity N (`--parity N`), not the standard E. Check the unit ID and the A/B wiring. |
| Frequent timeouts, or `late response · discarded` in the frames panel | The device answers more slowly than the timeout. Raise it, e.g. `-t 2s`; on RTU, answers later than about twice the timeout cannot be told apart from the next one. |
| No response on TCP through a gateway | The unit ID is the address of the device behind the gateway. |
| No response from a Modbus TCP device addressed directly | Many such devices ignore the unit ID, but some answer only to 255 (`-u 255`), the value the Modbus TCP implementation guide recommends, or only to 1. |
| Exception 02 (illegal data address) | Wrong table (3xxxx vs 4xxxx) or a base 0/base 1 mix-up. Compare with the neighbors in the list and check the convention in the manual. |
| `The RS-485 adapter is echoing…` | The adapter returns what it transmits. It is not supported in this version; check whether its echo can be disabled. |
| Zeros where you expected values | Some devices answer block reads with 0 for registers that do not exist. Use `--single` to read one register at a time. |
| `No permission for /dev/ttyUSB0` | Add your user to the group that owns the port, usually `dialout` (`sudo usermod -aG dialout $USER`, then log in again), or run with sudo. |
| `Port … is in use by process …` | Another program (often the gateway's own runtime) drives the bus. Stop it first; use `--force-port` only if you are sure. |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md), [GOVERNANCE.md](GOVERNANCE.md) and the [ROADMAP.md](ROADMAP.md). How modtop is built: [docs/architecture.md](docs/architecture.md). Security issues: [SECURITY.md](SECURITY.md); security reasoning: [docs/assurance-case.md](docs/assurance-case.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
