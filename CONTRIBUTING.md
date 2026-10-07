# Contributing to modtop

Thank you for helping. modtop aims to do a small thing very well, so read
[GOVERNANCE.md](GOVERNANCE.md) before proposing a feature: proposals need a
real field case and must fit the project's verb (obtain, interpret,
diagnose).

## Development setup

- Go 1.27 or later.
- `socat`, for the RTU tests (they are skipped without it).
- [golangci-lint](https://golangci-lint.run/) v2, for linting.

Try the tool against the built-in simulator:

```sh
go run ./internal/sim/cmd/simserver -addr 127.0.0.1:1502 &
go run ./cmd/modtop 127.0.0.1:1502 -r 40001-40012
```

## Before opening a pull request

Run the same checks as CI:

```sh
go vet ./...
go test -race ./...
golangci-lint run ./...
scripts/check-readonly.sh
```

The UI tests compare screens with golden files in `internal/ui/testdata`.
After an intended layout change, regenerate them with
`go test ./internal/ui -update` and review the diff.

## Rules

- **Tests are required.** Every change comes with tests in the same pull
  request; a bug fix comes with a regression test. Tests must run without
  hardware.
- **Read-only.** Never add a Modbus write function, not even in the codec.
- **English** for code, comments, messages and documentation.
- **Dependencies:** a new dependency needs a written justification in the
  pull request.
- **Commits:** pull requests are squash-merged and the title becomes the
  commit message, so use the [Conventional Commits](https://www.conventionalcommits.org/)
  style (`feat(ui): ...`, `fix(transport): ...`, `docs: ...`).
- **CI actions** are pinned to a full commit SHA with a version comment.
- Update `CHANGELOG.md` for user-visible changes.

## Releasing (maintainers)

1. In `CHANGELOG.md`, rename `[Unreleased]` to the version and date, e.g.
   `## [0.1.0] - 2026-10-20`, and merge that change.
2. Tag the merge commit and push the tag:
   `git tag v0.1.0 && git push origin v0.1.0`.
3. The release workflow runs the tests, builds the binaries, writes
   `SHA256SUMS`, attests the build provenance and publishes the release with
   the changelog section as notes.

## Security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).
