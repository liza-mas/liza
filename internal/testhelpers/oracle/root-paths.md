# Exact prompt root substitution

`ReplaceRoot(text, root, token string) string` provides the opt-in C-A8
capability. Resolve the disposable case root with `filepath.EvalSymlinks`, then
substitute that spelling before comparing the prompt:

```go
root, err := filepath.EvalSymlinks(caseRoot)
if err != nil {
    t.Fatal(err)
}
got := oracle.ReplaceRoot(prompt, root, "<ROOT>")
oracle.Check(t, oracle.Prompt, "case.txt", []byte(got))
```

The helper matches only the supplied literal root, including repeated occurrences.
A letter, digit, dot, hyphen or underscore immediately before or after it prevents
a match, so glued names and sibling paths survive. Slash and backslash suffixes
remain unchanged. Empty root is a no-op; empty token removes a matching root.
Another spelling requires another explicit call. The helper does not resolve
symlinks, infer temporary roots or clean paths itself.

Every byte outside matched root spans survives: timestamps with local offsets,
UUIDs, SHAs, other temporary paths, ANSI escapes, tabs, CRLF, invalid UTF-8 and
final-newline differences. Tokens are emitted literally without rescanning.

The original defaults remain distinct:

- **D-3:** `Normaliser.Text` applies the full timestamp, directory, identifier and
  registered-literal normalisation policy. It also rewrites path-tail separators.
  Do not call it for a root-only prompt assertion.
- **D-4:** `Prompt` and `Artifact` forms compare raw bytes. Other kinds retain
  their existing normalisation rules.
- **D-5:** `Check` in live mode compares and never writes; `ORACLE_WRITE=1`
  writes. Root substitution does not change golden I/O or regeneration policy.
  Consumers still create committed goldens through the regeneration entry point
  from the starting commit.

`TestRootOnlyBytes`, `TestRootOnlyAbsentAndEmpty` and
`TestRootOnlyGoldenComparison` exercise the public API with literal expectations.
The comparison test writes only into a disposable golden directory and verifies
that live rejection leaves its original bytes unchanged.

`TestProviderCorrectionA8Start` calls the shared test-only
`runCorrectionAtStart(t, testPattern)`. It exports immutable start
`96c9ad69aee983ce2b929dd681b6ba43bf920535` with Git, overlays only the current
`internal/testhelpers/oracle/**`, and runs selected tests with `-tags oraclestart`
under `ChildEnv(Go=true)`. Source discovery and Go JSON test events require each
selected test to execute and pass; empty selections and wrapper selections are
rejected. A5/A9 wrappers reuse the helper with their own positive/negative test
patterns. All export and overlay writes stay in the helper's temporary directory;
the caller's worktree and live runtime state are untouched.
