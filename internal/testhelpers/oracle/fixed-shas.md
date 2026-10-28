# Fixed-recipe SHA preservation

`(*Normaliser).KeepSHA(sha string) error` opts one normaliser into retaining an
exact fixed-recipe SHA spelling. Register all fixed SHAs before comparisons:

```go
n := oracle.NewNormaliser()
for _, sha := range fixtureCommitSHAs {
    if err := n.KeepSHA(sha); err != nil {
        t.Fatal(err)
    }
}
// Apply A9's lease/time/equality-preserving volatile-identity preprocessing first.
n.CheckState(t, "claim.yaml", preprocessedBefore, preprocessedAfter)
n.Check(t, oracle.JSON, "launch.json", preprocessedLaunch)
n.Check(t, oracle.Human, "agent.txt", preprocessedOutput)
```

Only complete 40- or 64-character ASCII hex strings are accepted. Invalid input
returns a value-free error without changing registrations. Registration is
idempotent and local to the instance; a zero-value `Normaliser` also accepts it.
Case is exact: registering lowercase does not retain the uppercase spelling.
Register before use; concurrent read-only use after registration is supported,
while concurrent registration or other configuration mutation is not.

Retention applies only at `Text`'s final hex-ID replacement. A match in the
allowlist is emitted verbatim, without a private sentinel or token round trip.
Unknown identifiers, including a SHA changed by one hex digit, still become
`<ID>`. `Dir`, `Literal`, timestamp and UUID processing retain their order.
An explicit `Literal` substitution takes precedence: register fixed SHAs with
`KeepSHA`, never `Literal`. A9 performs its own preprocessing before the harness
so lease derivations and volatile-identity equality tokens survive along with
the fixed Git SHAs.

Use the configured instance's `StateForm`, `Form(JSON/Human)`, `CheckState` and
`Check` methods. Package-level `Check` and `CheckState` create a fresh unconfigured
normaliser and therefore still normalise the same SHAs to `<ID>`.

The D-3–D-5 defaults remain unchanged when no SHA is registered: timestamps,
paths, UUIDs and hex IDs normalise as before; JSON keys sort and numbers/codes
survive; human token and row order survive whitespace/ANSI removal; state is a
before/after pair. Prompt and artifact bytes remain raw. Live checks never write;
`ORACLE_WRITE=1` writes normalised bytes. Consumers still generate committed
goldens through the regeneration entry point from the starting baseline.

`TestFixedSHAPreservation` uses independently fixed SHA-1/SHA-256 empty-input
recipes and tests exact spellings, repetition, validation, explicit substitution,
instance isolation and concurrent readers. `TestFixedSHAKinds` asserts complete
literal state/JSON/human forms after A9-style preprocessing.
`TestFixedSHAGoldenComparison` uses public write/live methods in a disposable
golden directory, checks exact stored bytes, rejects a one-digit change to either
SHA for each kind without rewriting the golden, and checks default helpers.
`TestProviderCorrectionA9Start` uses the shared `runCorrectionAtStart` helper to
require these three tests to execute and pass against immutable starting commit
`96c9ad69aee983ce2b929dd681b6ba43bf920535`, overlaid only with the current harness.

Validation runs in the assigned worktree after `make sync-embedded`. Pre-commit
covers all changed Go, Markdown and JSON files, including `check-json`, Go
format/import checks, vet, staticcheck, testhelper and module checks, duplicate
detection and the applicable text/conflict hooks. Canonical harness, race, oracle
and sample-regeneration commands are unchanged; the reviewed acceptance contract
defers the shared `make lint`, `make test` and `make test-e2e` commands.
