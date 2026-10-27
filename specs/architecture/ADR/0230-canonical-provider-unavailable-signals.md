# 230 - Canonical Provider-Unavailable Signals

## Status

ACCEPTED.

## Context

A provider-unavailable signal stops supervisors, spawns and orchestrator wakes
of one provider until its environment is repaired. The record was named by the
classifier's label after an alias rule (`codex-acp` and `acpx-*` fold to the
tool they wrap) and matched by file name, so equality followed that rule and
the launch tool that failed was lost. The goal's behaviour change C3 asks that
a signal raised for `codex` stops a `codex-acp` supervisor and the reverse,
following the policy-time provider keys of
[ADR-0229](0229-policy-time-canonical-provider-keys.md); quota signals are
unchanged.

## Decision

### D1 - A record keeps the launch tool

`providersignal.ProviderUnavailable` carries `Tool` beside `Provider`. The
classifier sets `Tool` to the tool it was asked about, so the diagnostic
label (`codex` serves both Codex tools) is never taken for the tool that
failed. A record whose tool equals its label reads as it always did; any other
adds a `tool:` line.

### D2 - Match by C2 provider keys at evaluation time

`CheckProviderUnavailableSignal(root, tool, keys)` enumerates the records and
reports a match when `ProviderKeys.Same(record tool, tool)`. The keys are a
lazy argument: derived once, only when a record exists, from the catalog-derived
registry under the configuration of the state the caller decides on. Nothing
caches or stores a key, so a configuration change takes effect at the next
check. A record without a `tool:` line (written by an earlier version) is
identified by its file name and read through the same keys.
Unreadable records still block, identified by file name.

Consumers: the supervisor admission check (last published snapshot, read only
when a record exists), the orchestrator's final wake revalidation (the state it
revalidates), and the spawn gate (the runtime configuration it already reads,
now before the check and after the quota gate, which still refuses first).
Refusal text, alert text and sequencing are unchanged; the spawn alert names
the requested tool.

### D3 - Storage and presentation are separate

The record file is named by the percent-encoded launch tool, so tools with
distinct keys never overwrite each other, ordinary names equal their legacy
file names, and a configured tool name cannot address a path outside the signal
directory. Presentation enumerates records and reads the display provider (the
`provider:` line of a record that has a `tool:` line, otherwise the file name),
collapses records that display the same provider into one row and clears every
one by the path it enumerated. Provider values, row order, labels and failure
attribution of existing fixtures are unchanged; a custom shared key never
appears in them.

## Consequences

Quota signals keep their spelled alias normalisation. A C3 golden of the A9
oracle (`i21-unavailable-codex-run`) changes from not blocked to blocked, with
D24/D56 ledger records. Signal files written by this version are not read by
older versions as alias records (a `codex-acp` raise is stored under its own
file), which is accepted: the files are transient and cleared by resume.
