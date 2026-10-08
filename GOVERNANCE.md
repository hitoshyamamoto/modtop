# modtop governance

## The verb

modtop exists to **see and understand what a Modbus device is saying**.
That verb breaks down into three:

- **Obtain:** read data in more situations.
- **Interpret:** turn bytes into meaning.
- **Diagnose:** explain why something does not work.

Every feature must serve at least one of them.

## Feature acceptance criteria

### 0. Demand (prerequisite)
Is there a real field case, described concretely (equipment, situation,
current friction)? Speculative proposals are not evaluated.

### 1. Verb (veto)
- Sentence test: can the feature be described as "obtain, interpret or
  diagnose better, or in more situations"? If it needs another verb
  (store, notify, control, publish, schedule), it is rejected.
- Separate project test: would it make sense as a standalone tool?
  If so, it is another project.
- Audience test: is the user the same engineer diagnosing a device?
  If not, it is another product.

Writing is only accepted in diagnostic form: isolated, explicit,
confirmed and logged. Batch, scheduled, looped or conditional writes are
control and are out of scope.

### 2. Zero configuration (negotiable with mitigation)
The basic uses keep working on a clean machine, with only the binary,
without a configuration file, environment variable or internet access.
Features with risk or cost are off by default.

### 3. Maintainable by one person (negotiable with mitigation)
Two years from now, can a bug in this feature be fixed in an afternoon,
with only tests, documentation and code, without the original hardware?
- Testable in CI without hardware.
- New dependencies justified in writing.
- Platform-specific code isolated.

### 4. Contracts (veto)
No existing contract is broken (see the inventory below).

### 5. Operational safety (veto, unless mitigated)
If the feature transmits something new on the bus, increases the
communication load, can cause a physical effect or opens a network port,
it only goes in as optional, explicit, confirmed and logged, with the
default unchanged.

## Contract inventory

| Contract | Rule |
|---|---|
| CLI (flags, semantics, defaults, exit codes) | additions only; changes require deprecation |
| Safety (read-only by default; never contend for a serial port) | never changes |
| Keyboard shortcuts | weak contract: changes only with a changelog notice |
| Visual layout and colors | not a contract |

New contracts (profile format, CSV/JSON output, recordings) join this
inventory when they are created.

## Deprecation

1. Version N marks the item as deprecated; it keeps working and emits a
   warning with the alternative.
2. The warning stays for at least two minor versions.
3. File formats get an automatic migration tool.
4. Removal only in a major version, documented in the changelog.

## Release discipline

One theme per version. A version never mixes themes.

## Proposal template

    ## Real case
    ## Verb (obtain / interpret / diagnose)
    ## Basic usage (unchanged? optional?)
    ## Maintenance (how it is tested without hardware; new dependencies)
    ## Contracts affected
    ## Operational safety

## Decision making

modtop is maintained by one person, who makes the final decisions.
Proposals and changes are discussed in public, in GitHub issues and pull
requests, and judged against the criteria above. Every change lands through
a pull request that must pass the required CI checks; there is no direct
push to `main`. Disagreements are discussed in the issue or pull request
thread; the maintainer decides and records the reason there.

## Roles and responsibilities

| Role | Responsibilities | Held by |
|---|---|---|
| Maintainer | Triage issues, review and merge pull requests, keep the documentation current, apply this governance | [@hitoshyamamoto](https://github.com/hitoshyamamoto) |
| Release manager | Prepare the changelog, sign and push version tags, check the published release | [@hitoshyamamoto](https://github.com/hitoshyamamoto) |
| Security responder | Handle vulnerability reports as described in [SECURITY.md](SECURITY.md) | [@hitoshyamamoto](https://github.com/hitoshyamamoto) |
| Code of conduct enforcement | Apply [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) | [@hitoshyamamoto](https://github.com/hitoshyamamoto) |

## Continuity

Today the project has a bus factor of 1: only the maintainer can merge
changes and publish releases. What limits the damage if the maintainer
becomes unavailable:

- The Apache License 2.0 lets anyone fork and continue the project.
- Everything needed to build, test and release is in the repository. The
  release workflow needs no stored secrets: it uses the workflow token and
  keyless signing.
- The release steps are documented in [CONTRIBUTING.md](CONTRIBUTING.md).

What is missing: a second person with the access to merge and release
within a week. Adding a co-maintainer is the planned remedy.

