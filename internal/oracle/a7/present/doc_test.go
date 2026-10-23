// Package present freezes A7's inspection, help, completion, stop-guard and
// JSON-envelope surfaces of the default brand. Linux is required for the
// fixture's process identities and Git symlinks. Drivers import only the
// standard library and the oracle harness; production imports are confined
// to adapter_watch_test.go and adapter_envelope_test.go.
//
// Help inventory: testdata/fixtures/help-inventory.go.txt is a one-shot registry
// enumerator, used through a Go overlay as cmd/liza/a7_inventory_test.go.
// Run TestA7HelpInventory on starting commit 96c9ad69a (or a tree verified by
// git diff to have identical cmd/liza production files), setting its explicit
// A7_HELP_INVENTORY_OUTPUT to testdata/fixtures/help-paths.txt. ExecuteC injects
// Cobra's help and completion commands before walking Commands recursively;
// hidden commands and commands built by factories are included. The committed
// path list is never discovered dynamically by the oracle itself.
//
// No classification cases are omitted: every ClassifyError production type
// is exported and constructible from this package at the starting commit.
// classification-cases.txt orders all typed branches, each fallback string
// pattern, operational inner-error branches, an unknown lock type, and a
// successful result with a warning. TestOracleEnvelopeClassification renders
// the real WriteResult, retaining details and warning fields.
//
// Goldens are created only with:
//
//	go run ./internal/testhelpers/oracle/cmd/regen -mode write-new -pkg ./internal/oracle/a7/present/...
//
// The entry point uses the starting production tree. Live tests compare only.
// The environment and relative-clock mask table live in main_test.go; fixture
// timestamps are in 2001 or 2099 and PIDs exceed Linux pid_max. Runtime lock
// files, log.yaml, alerts.log and the home cache are never golden inputs.
//
// Determinism exclusions inherited from the surface inventory: watch's loop
// header and timing, the host process scan of get agents --zombies, and codex
// activation stderr (activation belongs to the installation oracle). Watch
// checks run once and retain emitted order. Fixture agents with live leases
// own running work; one idle agent has a stale heartbeat and no lease.
//
// Negative cases reach specific refusal paths: an existing empty directory
// without Git metadata reaches project_root; task-unknown is absent from an
// otherwise validated project and reaches not_found; config.mode exists, so
// its extra argument reaches field-query arity validation. The stop-guard's
// no-agent case contains a running job, the no-live case contains a completed
// job, the live case contains a running non-await job, and unreadable input is
// invalid JSON under an agent identity. These independent fixtures prevent an
// unrelated missing project or identity from satisfying the refusal proofs.
package present
