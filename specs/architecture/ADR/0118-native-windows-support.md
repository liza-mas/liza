# ADR-0118: Native Windows Support

## Status

ACCEPTED

## Context

v0.8.0 dropped native Windows support (commit `788a9f4`): the CLI fails fast on
`GOOS=windows` and directs users to WSL2, Windows artifacts were removed from
GoReleaser and the Makefile, and the docs were updated to match. No ADR recorded
the reasoning, so the constraint that produced it has to be reconstructed from
the change itself: the suite could not pass on Windows, the platform had no CI
coverage, and WSL2 made the problem go away for the cost of one sentence in the
README.

That trade is sound right up until a user cannot use WSL2. On a managed
enterprise machine it may be unavailable, and a developer working on a Windows
host with their tools, credentials and repositories outside WSL pays a real cost
for the bridge: paths, line endings and Git configuration all differ across it.

The relevant question is not whether Windows is convenient to support, but
whether Liza's design actually depends on POSIX. It largely does not. The binary
is Go; the state, worktree and review machinery is filesystem and Git. Two
genuine dependencies remain: the hooks Liza deploys are POSIX shell, and
contract activation uses symlinks.

## Decision

Support Windows natively as a first-class platform, on two explicit conditions.

**Git for Windows is required, with its `bash.exe` ahead of the WSL launcher on
PATH.** The hooks stay POSIX shell — one implementation, not two. Porting them to
PowerShell would double the surface that governs agent behaviour and split the
security-sensitive init gate into two dialects that would drift.

**Symlink creation requires Developer Mode or an elevated shell**, as upstream
already documents. Where the privilege is absent, the code falls back rather than
failing: managed git hooks install a wrapper script that execs the dispatcher,
and contract activation reports what could not be linked.

Two rules follow, and they are the substance of this decision:

- **No skip stands in for a missing capability.** Where a POSIX mechanism has a
  Windows equivalent, the equivalent is implemented: the executable bit becomes
  `bash -n`, a read-only directory becomes an explicit deny entry in the DACL, a
  shell stub gains a `.cmd` wrapper so `exec.LookPath` can find it. Where a test
  depends on a session privilege rather than a platform, it probes for the
  capability and runs wherever it is held. A test is skipped only where the
  concept genuinely does not exist on Windows — setuid stripping, for one — and
  the skip says so.

- **CI runs on windows-latest.** Without it the rest is a claim rather than a
  property, and every future change can regress it silently.

## Consequences

Positive:

- Windows becomes usable without WSL2, and the constraints are stated up front
  rather than discovered.
- The portability work found four user-visible defects that WSL2 had been hiding,
  none of which were test-only: the session-init gate compared native Read paths
  against a forward-slashed project root, so those reads could not clear it;
  Bash reads now require forward slashes so the hook validates the same command
  Bash executes. The session-context hook emitted half-native, half-POSIX paths;
  managed git hooks reinstalled themselves on every run and lost the hook name,
  so the post-checkout short-circuit never fired; and `liza update` had no way to
  replace a running executable.
- The `.cmd` wrapper, the DACL helper and the capability probes are reusable, so
  the next Windows-shaped problem starts from a helper rather than a skip.

Negative:

- A third platform to keep green, and a CI job whose runtime is paid on every
  push.
- Git for Windows is a hard dependency for hook execution. A user who has it but
  whose PATH resolves `bash` to the WSL launcher gets failures that read like
  missing files; this is documented in TROUBLESHOOTING, but it is a sharp edge.
- Symlink behaviour is genuinely two-shaped: a symlink where the privilege is
  held, a wrapper where it is not. Both are supported and tested, which is more
  surface than one.

## Alternatives Considered

**Keep WSL2 as the only Windows path.** Cheapest, and it is what upstream chose.
Rejected because it fails exactly where it is most needed: a locked-down
enterprise machine, which is where a Windows developer is most likely to be.

**Port the hooks to PowerShell.** Would remove the Git for Windows dependency and
give Windows a native feel. Rejected: it doubles the contract-enforcement surface
and invites drift between two implementations of the same gate, at the cost of a
dependency most Windows developers already have.

**Copy contract files instead of symlinking.** Would sidestep the privilege
requirement entirely. Implemented, then reverted: copies break the readlink-based
conflict detection that makes `setup` idempotent, and they go stale silently when
the canonical contract changes — the failure mode the symlink exists to prevent.
The privilege requirement is the honest cost, and the wrapper fallback already
covers the case that matters most.
