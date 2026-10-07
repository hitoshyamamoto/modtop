<!-- PRs are squash-merged: the PR title becomes the commit message (conventional style: feat/fix/docs/chore/...). -->

## What

<!-- What changes and why. -->

## How it was tested

<!-- Tests added or updated; manual checks, if any. -->

## Checklist

- [ ] `go vet ./...` and `go test -race ./...` pass
- [ ] Tests cover the change (bug fixes include a regression test)
- [ ] No Modbus write function was added (`scripts/check-readonly.sh`)
- [ ] `CHANGELOG.md` updated for user-visible changes
