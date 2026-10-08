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

## Coding style

Go code follows [Effective Go](https://go.dev/doc/effective_go) and the
[Go Code Review Comments](https://go.dev/wiki/CodeReviewComments). It must be
formatted with `gofmt` and pass `golangci-lint` with the repository's
[.golangci.yml](.golangci.yml); CI enforces both. Exceptions are rare and
explained in a comment next to them.

## Developer Certificate of Origin

Every commit must be signed off, certifying that you wrote it or otherwise
have the right to submit it under the project's license, as stated in the
[Developer Certificate of Origin 1.1](https://developercertificate.org/).
Add the sign-off with `git commit -s`; it appends a line such as:

```
Signed-off-by: Your Name <you@example.com>
```

The `DCO` check in CI fails when a commit of a pull request has no sign-off.

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
2. Create a signed tag on the merge commit and push it:
   `git tag -s v0.1.2 -m "modtop 0.1.2" && git push origin v0.1.2`.
   Tags are signed with the SSH key listed in
   [.github/allowed_signers](.github/allowed_signers) (git configured with
   `gpg.format=ssh`); check with `git tag -v v0.1.2`.
3. The release workflow runs the tests, builds the binaries, writes
   `SHA256SUMS`, attests the build provenance and publishes the release with
   the changelog section as notes. It then publishes the container image
   `ghcr.io/hitoshyamamoto/modtop` from those binaries (the `image.yml`
   workflow, which can also be run by hand for an existing tag).

## Reproducing a release

Release builds are reproducible: anyone can rebuild them from the tagged
source and get the published checksums. Use the Go version recorded in the
binary (`go version -m modtop-linux-amd64`), then:

```sh
git clone https://github.com/hitoshyamamoto/modtop && cd modtop
git checkout v0.1.2
for t in amd64::amd64 arm64::arm64 arm:7:armv7; do
  IFS=: read -r arch arm name <<< "$t"
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch GOARM=$arm go build -trimpath \
    -ldflags "-s -w -X main.version=v0.1.2" -o "dist/modtop-linux-$name" ./cmd/modtop
done
cd dist
curl -LO https://github.com/hitoshyamamoto/modtop/releases/download/v0.1.2/SHA256SUMS
sha256sum --check SHA256SUMS
```

Write the binaries to `dist/` (ignored by git), as the release workflow
does: a stray file in the working tree marks the build as modified and
changes the result.

v0.1.0 was built while an untracked release-notes file was in the working
tree; to reproduce it, create any untracked file (e.g. `touch notes.md`)
before building.

## Security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).
